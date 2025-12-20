package smtp

import (
	"context"
	"io"
	"log"
	"strings"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/storage"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
)

// Backend implements the go-smtp Backend interface
type Backend struct {
	config        *config.Config
	authenticator *auth.Authenticator
	store         *maildir.Store
}

// NewBackend creates a new SMTP backend
func NewBackend(cfg *config.Config, authenticator *auth.Authenticator, store *maildir.Store) *Backend {
	return &Backend{
		config:        cfg,
		authenticator: authenticator,
		store:         store,
	}
}

// NewSession is called when a new SMTP connection is established
func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &Session{
		backend:      b,
		conn:         c,
		isSubmission: false,
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
}

// AuthPlain handles PLAIN authentication
func (s *Session) AuthPlain(username, password string) error {
	user, err := s.backend.authenticator.Authenticate(context.Background(), username, password)
	if err != nil {
		return smtp.ErrAuthFailed
	}
	s.user = user
	return nil
}

// Mail is called when the MAIL FROM command is received
func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	// For submission (authenticated), allow any sender the user owns
	if s.isSubmission && s.user != nil {
		// Verify the from address belongs to the user
		fromLocal, fromDomain := parseAddress(from)
		if fromLocal != s.user.Username || fromDomain != s.user.Domain {
			// Allow if user has explicit permission (e.g., alias)
			// For now, be permissive for personal use
			log.Printf("SMTP: User %s sending as %s", s.user.Email, from)
		}
	}

	s.from = from
	return nil
}

// Rcpt is called when the RCPT TO command is received
func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	// For MX (port 25), only accept mail for local domains
	// For submission (authenticated), allow sending to any address

	if s.isSubmission {
		// Authenticated user can send anywhere
		s.rcpts = append(s.rcpts, to)
		return nil
	}

	// MX mode - verify recipient is local
	valid, err := s.backend.authenticator.ValidateAddress(context.Background(), to)
	if err != nil {
		log.Printf("SMTP: Error validating address %s: %v", to, err)
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 3, 0},
			Message:      "Temporary failure, please try again",
		}
	}

	if !valid {
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

	// Read message data
	data, err := io.ReadAll(io.LimitReader(r, int64(s.backend.config.Security.MaxMessageSize)))
	if err != nil {
		return err
	}

	if s.isSubmission {
		// Queue for outbound delivery
		return s.handleOutbound(data)
	}

	// Local delivery
	return s.handleInbound(data)
}

// handleInbound delivers mail to local mailboxes
func (s *Session) handleInbound(data []byte) error {
	ctx := context.Background()

	for _, rcpt := range s.rcpts {
		// Check for alias
		userID, external, err := s.backend.authenticator.ResolveAlias(ctx, rcpt)
		if err != nil {
			log.Printf("SMTP: Error resolving alias %s: %v", rcpt, err)
			continue
		}

		if external != nil {
			// Forward to external address - queue for outbound
			log.Printf("SMTP: Forwarding to external address %s", *external)
			// TODO: Queue for outbound delivery
			continue
		}

		// Get user for direct delivery
		var user *auth.User
		if userID != nil {
			user, err = s.backend.authenticator.LookupUserByID(ctx, *userID)
		} else {
			user, err = s.backend.authenticator.LookupUser(ctx, rcpt)
		}

		if err != nil {
			log.Printf("SMTP: User not found for %s: %v", rcpt, err)
			continue
		}

		// Get INBOX mailbox
		inbox, err := s.backend.store.GetMailbox(ctx, user.ID, "INBOX")
		if err != nil {
			log.Printf("SMTP: INBOX not found for %s: %v", rcpt, err)
			continue
		}

		// Deliver message
		_, err = s.backend.store.AppendMessage(ctx, inbox.ID, nil, time.Now(),
			strings.NewReader(string(data)))
		if err != nil {
			log.Printf("SMTP: Failed to deliver to %s: %v", rcpt, err)
			continue
		}

		log.Printf("SMTP: Delivered message to %s", rcpt)
	}

	return nil
}

// handleOutbound queues mail for external delivery
func (s *Session) handleOutbound(data []byte) error {
	// For now, just log - actual relay will be implemented later
	log.Printf("SMTP: Queuing outbound message from %s to %v", s.from, s.rcpts)

	// In a full implementation, you would:
	// 1. Sign with DKIM
	// 2. Queue the message for delivery
	// 3. Attempt delivery via MX lookup
	// 4. Handle retries and bounces

	// Store in user's Sent folder
	if s.user != nil {
		ctx := context.Background()

		sent, err := s.backend.store.GetMailbox(ctx, s.user.ID, "Sent")
		if err == nil {
			flags := []storage.Flag{storage.FlagSeen}
			s.backend.store.AppendMessage(ctx, sent.ID, flags, time.Now(),
				strings.NewReader(string(data)))
		}
	}

	return nil
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
