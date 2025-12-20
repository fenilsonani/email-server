package smtp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/smtp/delivery"
	"github.com/fenilsonani/email-server/internal/storage"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
)

// Backend implements the go-smtp Backend interface
type Backend struct {
	config         *config.Config
	authenticator  *auth.Authenticator
	store          *maildir.Store
	deliveryEngine *delivery.Engine
	logger         *logging.Logger
	queuePath      string // Path to store queued message files
}

// NewBackend creates a new SMTP backend
func NewBackend(cfg *config.Config, authenticator *auth.Authenticator, store *maildir.Store, deliveryEngine *delivery.Engine, logger *logging.Logger) *Backend {
	queuePath := filepath.Join(cfg.Storage.DataDir, "queue")
	os.MkdirAll(queuePath, 0755)

	return &Backend{
		config:         cfg,
		authenticator:  authenticator,
		store:          store,
		deliveryEngine: deliveryEngine,
		logger:         logger.SMTP(),
		queuePath:      queuePath,
	}
}

// NewSession is called when a new SMTP connection is established
func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	remoteAddr := ""
	if c.Conn() != nil {
		remoteAddr = c.Conn().RemoteAddr().String()
	}

	return &Session{
		backend:      b,
		conn:         c,
		isSubmission: false,
		remoteAddr:   remoteAddr,
		ctx:          logging.WithRemoteAddr(context.Background(), remoteAddr),
	}, nil
}

// Session implements the go-smtp Session interface
type Session struct {
	backend      *Backend
	conn         *smtp.Conn
	user         *auth.User
	from         string
	rcpts        []string
	isSubmission bool
	remoteAddr   string
	ctx          context.Context
}

// AuthPlain handles PLAIN authentication
func (s *Session) AuthPlain(username, password string) error {
	user, err := s.backend.authenticator.Authenticate(s.ctx, username, password)
	if err != nil {
		s.backend.logger.WarnContext(s.ctx, "Authentication failed",
			"username", username,
			"remote_addr", s.remoteAddr,
		)
		return smtp.ErrAuthFailed
	}

	s.user = user
	s.ctx = logging.WithUserID(s.ctx, user.ID)
	s.backend.logger.InfoContext(s.ctx, "User authenticated",
		"username", username,
	)
	return nil
}

// Mail is called when the MAIL FROM command is received
func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	// For submission (authenticated), validate sender
	if s.isSubmission && s.user != nil {
		fromLocal, fromDomain := parseAddress(from)
		if fromLocal != s.user.Username || fromDomain != s.user.Domain {
			// Check if user has permission to send as this address
			// For single-domain personal use, we're permissive but log it
			s.backend.logger.WarnContext(s.ctx, "User sending as different address",
				"user_email", s.user.Email,
				"from", from,
			)
		}
	}

	s.from = from
	return nil
}

// Rcpt is called when the RCPT TO command is received
func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	if s.isSubmission {
		// Authenticated user can send anywhere
		s.rcpts = append(s.rcpts, to)
		return nil
	}

	// MX mode - verify recipient is local
	valid, err := s.backend.authenticator.ValidateAddress(s.ctx, to)
	if err != nil {
		s.backend.logger.ErrorContext(s.ctx, "Error validating recipient", err,
			"recipient", to,
		)
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 3, 0},
			Message:      "Temporary failure, please try again",
		}
	}

	if !valid {
		s.backend.logger.InfoContext(s.ctx, "Rejected unknown recipient",
			"recipient", to,
		)
		return &smtp.SMTPError{
			Code:         550,
			EnhancedCode: smtp.EnhancedCode{5, 1, 1},
			Message:      "User not found",
		}
	}

	s.rcpts = append(s.rcpts, to)
	return nil
}

// Data is called when the DATA command is received
func (s *Session) Data(r io.Reader) error {
	if len(s.rcpts) == 0 {
		return &smtp.SMTPError{
			Code:         503,
			EnhancedCode: smtp.EnhancedCode{5, 5, 1},
			Message:      "No recipients specified",
		}
	}

	// Read message data with size limit
	data, err := io.ReadAll(io.LimitReader(r, int64(s.backend.config.Security.MaxMessageSize)))
	if err != nil {
		s.backend.logger.ErrorContext(s.ctx, "Failed to read message data", err)
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 3, 0},
			Message:      "Error reading message data",
		}
	}

	if s.isSubmission {
		return s.handleOutbound(data)
	}

	return s.handleInbound(data)
}

// handleInbound delivers mail to local mailboxes
func (s *Session) handleInbound(data []byte) error {
	var deliveryErrors []error
	successCount := 0

	for _, rcpt := range s.rcpts {
		err := s.deliverToLocalRecipient(rcpt, data)
		if err != nil {
			deliveryErrors = append(deliveryErrors, fmt.Errorf("%s: %w", rcpt, err))
			s.backend.logger.ErrorContext(s.ctx, "Local delivery failed", err,
				"recipient", rcpt,
			)
		} else {
			successCount++
			s.backend.logger.InfoContext(s.ctx, "Message delivered locally",
				"recipient", rcpt,
			)
		}
	}

	// If no deliveries succeeded, return error
	if successCount == 0 && len(deliveryErrors) > 0 {
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 0, 0},
			Message:      "Temporary delivery failure for all recipients",
		}
	}

	// Partial success is still success from SMTP perspective
	// Failed recipients will be handled via DSN if needed
	return nil
}

// deliverToLocalRecipient delivers to a single local recipient
func (s *Session) deliverToLocalRecipient(rcpt string, data []byte) error {
	ctx := s.ctx

	// Check for alias
	userID, external, err := s.backend.authenticator.ResolveAlias(ctx, rcpt)
	if err != nil {
		return fmt.Errorf("failed to resolve alias: %w", err)
	}

	// Handle external forwarding
	if external != nil {
		if s.backend.deliveryEngine != nil {
			// Queue for outbound delivery
			messagePath, err := s.saveMessageToQueue(data)
			if err != nil {
				return fmt.Errorf("failed to save message for forwarding: %w", err)
			}
			return s.backend.deliveryEngine.Enqueue(ctx, s.from, []string{*external}, messagePath)
		}
		s.backend.logger.WarnContext(ctx, "External forwarding not available - delivery engine not configured",
			"external_addr", *external,
		)
		return nil
	}

	// Get user for direct delivery
	var user *auth.User
	if userID != nil {
		user, err = s.backend.authenticator.LookupUserByID(ctx, *userID)
	} else {
		user, err = s.backend.authenticator.LookupUser(ctx, rcpt)
	}

	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}

	// Get INBOX mailbox
	inbox, err := s.backend.store.GetMailbox(ctx, user.ID, "INBOX")
	if err != nil {
		return fmt.Errorf("INBOX not found: %w", err)
	}

	// Deliver message
	_, err = s.backend.store.AppendMessage(ctx, inbox.ID, nil, time.Now(),
		strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("failed to append message: %w", err)
	}

	return nil
}

// handleOutbound queues mail for external delivery
func (s *Session) handleOutbound(data []byte) error {
	// Separate local and external recipients
	var localRcpts, externalRcpts []string
	localDomain := s.backend.config.Server.Domain

	for _, rcpt := range s.rcpts {
		_, domain := parseAddress(rcpt)
		if domain == localDomain {
			localRcpts = append(localRcpts, rcpt)
		} else {
			externalRcpts = append(externalRcpts, rcpt)
		}
	}

	var lastError error

	// Deliver to local recipients
	if len(localRcpts) > 0 {
		s.backend.logger.InfoContext(s.ctx, "Delivering to local recipients",
			"count", len(localRcpts),
		)
		for _, rcpt := range localRcpts {
			if err := s.deliverToLocalRecipient(rcpt, data); err != nil {
				s.backend.logger.ErrorContext(s.ctx, "Local delivery failed", err,
					"recipient", rcpt,
				)
				lastError = err
			}
		}
	}

	// Queue external recipients for delivery
	if len(externalRcpts) > 0 {
		if s.backend.deliveryEngine == nil {
			s.backend.logger.ErrorContext(s.ctx, "Delivery engine not configured", nil)
			return &smtp.SMTPError{
				Code:         451,
				EnhancedCode: smtp.EnhancedCode{4, 3, 0},
				Message:      "Mail delivery temporarily unavailable",
			}
		}

		// Save message to queue directory
		messagePath, err := s.saveMessageToQueue(data)
		if err != nil {
			s.backend.logger.ErrorContext(s.ctx, "Failed to save message to queue", err)
			return &smtp.SMTPError{
				Code:         451,
				EnhancedCode: smtp.EnhancedCode{4, 3, 0},
				Message:      "Temporary failure saving message",
			}
		}

		// Enqueue for delivery
		if err := s.backend.deliveryEngine.Enqueue(s.ctx, s.from, externalRcpts, messagePath); err != nil {
			s.backend.logger.ErrorContext(s.ctx, "Failed to enqueue message for delivery", err)
			return &smtp.SMTPError{
				Code:         451,
				EnhancedCode: smtp.EnhancedCode{4, 3, 0},
				Message:      "Temporary failure queuing message",
			}
		}

		s.backend.logger.InfoContext(s.ctx, "Message queued for external delivery",
			"from", s.from,
			"recipients", len(externalRcpts),
		)
	}

	// Store in user's Sent folder
	if s.user != nil {
		ctx := s.ctx
		sent, err := s.backend.store.GetMailbox(ctx, s.user.ID, "Sent")
		if err == nil {
			flags := []storage.Flag{storage.FlagSeen}
			_, err = s.backend.store.AppendMessage(ctx, sent.ID, flags, time.Now(),
				strings.NewReader(string(data)))
			if err != nil {
				s.backend.logger.WarnContext(ctx, "Failed to save to Sent folder", "error", err.Error())
			}
		}
	}

	if lastError != nil && len(externalRcpts) == 0 {
		// Only local delivery and it failed
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 0, 0},
			Message:      "Delivery failed",
		}
	}

	return nil
}

// saveMessageToQueue saves a message to the queue directory
func (s *Session) saveMessageToQueue(data []byte) (string, error) {
	// Generate unique filename
	filename := fmt.Sprintf("%d-%s.eml", time.Now().UnixNano(), generateID())
	path := filepath.Join(s.backend.queuePath, filename)

	// Write file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}

	return path, nil
}

// Reset is called after a successful DATA command or RSET
func (s *Session) Reset() {
	s.from = ""
	s.rcpts = nil
}

// Logout is called when the connection is closed
func (s *Session) Logout() error {
	return nil
}

// parseAddress extracts local part and domain from an email address
func parseAddress(addr string) (local, domain string) {
	// Handle <addr> format
	addr = strings.TrimPrefix(addr, "<")
	addr = strings.TrimSuffix(addr, ">")

	parts := strings.SplitN(addr, "@", 2)
	if len(parts) == 2 {
		return strings.ToLower(parts[0]), strings.ToLower(parts[1])
	}
	return addr, ""
}

// generateID generates a cryptographically secure unique ID
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if crypto/rand fails (should never happen)
		return fmt.Sprintf("%d-%x", time.Now().UnixNano(), time.Now().UnixNano()%0xFFFFFF)
	}
	return hex.EncodeToString(b)
}
