package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/features"
	"github.com/fenilsonani/email-server/internal/greylist"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/metrics"
	"github.com/fenilsonani/email-server/internal/sieve"
	"github.com/fenilsonani/email-server/internal/smtp/delivery"
	"github.com/fenilsonani/email-server/internal/storage"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
)

// Buffer pools for reducing allocations in hot paths
var (
	// idBufferPool for generateID - 16 bytes for random ID
	idBufferPool = sync.Pool{
		New: func() any {
			b := make([]byte, 16)
			return &b
		},
	}

	// bufioReaderPool for parseMessageForSieve
	bufioReaderPool = sync.Pool{
		New: func() any {
			return bufio.NewReader(nil)
		},
	}

	// bytesBufferPool for vacation messages
	bytesBufferPool = sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}
)

// LocalDeliveryNotifier is called when a message is delivered locally
type LocalDeliveryNotifier func(username, mailbox string)

// UserRateLimiter tracks email sending rate per user
type UserRateLimiter struct {
	mu       sync.RWMutex
	counters map[int64]*userSendCounter
	// Limits
	maxPerHour int
	maxPerDay  int
	stopCleanup chan struct{}
}

type userSendCounter struct {
	hourCount  int
	dayCount   int
	hourReset  time.Time
	dayReset   time.Time
	lastAccess time.Time // Track last access for cleanup
}

// NewUserRateLimiter creates a rate limiter for user sending
func NewUserRateLimiter(maxPerHour, maxPerDay int) *UserRateLimiter {
	rl := &UserRateLimiter{
		counters:    make(map[int64]*userSendCounter),
		maxPerHour:  maxPerHour,
		maxPerDay:   maxPerDay,
		stopCleanup: make(chan struct{}),
	}
	// Start cleanup goroutine to prevent unbounded memory growth
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop periodically removes stale entries from the rate limiter
func (rl *UserRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCleanup:
			return
		}
	}
}

// cleanup removes entries that haven't been accessed in over 48 hours
func (rl *UserRateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-48 * time.Hour)
	for userID, counter := range rl.counters {
		if counter.lastAccess.Before(cutoff) {
			delete(rl.counters, userID)
		}
	}
}

// Stop stops the cleanup goroutine
func (rl *UserRateLimiter) Stop() {
	close(rl.stopCleanup)
}

// CheckAndIncrement checks if user can send and increments counter
func (rl *UserRateLimiter) CheckAndIncrement(userID int64) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	counter, exists := rl.counters[userID]

	if !exists {
		rl.counters[userID] = &userSendCounter{
			hourCount:  1,
			dayCount:   1,
			hourReset:  now.Add(time.Hour),
			dayReset:   now.Add(24 * time.Hour),
			lastAccess: now,
		}
		return nil
	}

	// Update last access time for cleanup tracking
	counter.lastAccess = now

	// Reset counters if window expired
	if now.After(counter.hourReset) {
		counter.hourCount = 0
		counter.hourReset = now.Add(time.Hour)
	}
	if now.After(counter.dayReset) {
		counter.dayCount = 0
		counter.dayReset = now.Add(24 * time.Hour)
	}

	// Check limits
	if counter.hourCount >= rl.maxPerHour {
		return fmt.Errorf("hourly sending limit exceeded (%d/hour)", rl.maxPerHour)
	}
	if counter.dayCount >= rl.maxPerDay {
		return fmt.Errorf("daily sending limit exceeded (%d/day)", rl.maxPerDay)
	}

	// Increment
	counter.hourCount++
	counter.dayCount++
	return nil
}

// Backend implements the go-smtp Backend interface
type Backend struct {
	config          *config.Config
	authenticator   *auth.Authenticator
	store           *maildir.Store
	deliveryEngine  *delivery.Engine
	logger          *logging.Logger
	queuePath       string // Path to store queued message files
	onLocalDelivery LocalDeliveryNotifier
	sieveExecutor   *sieve.Executor
	greylister      *greylist.Greylister
	userRateLimiter *UserRateLimiter // Per-user sending rate limiter
	vacationSem     chan struct{}    // Semaphore to limit concurrent vacation responses
	featuresStore   *features.Store  // Store for unique features (Screener, Aliases, etc.)
}

// NewBackend creates a new SMTP backend
func NewBackend(cfg *config.Config, authenticator *auth.Authenticator, store *maildir.Store, deliveryEngine *delivery.Engine, logger *logging.Logger) (*Backend, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if authenticator == nil {
		return nil, fmt.Errorf("authenticator cannot be nil")
	}
	if store == nil {
		return nil, fmt.Errorf("store cannot be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}

	queuePath := filepath.Join(cfg.Storage.DataDir, "queue")
	if err := os.MkdirAll(queuePath, 0750); err != nil { // Restrict permissions - no world access
		return nil, fmt.Errorf("failed to create queue directory: %w", err)
	}

	return &Backend{
		config:          cfg,
		authenticator:   authenticator,
		store:           store,
		deliveryEngine:  deliveryEngine,
		logger:          logger.SMTP(),
		queuePath:       queuePath,
		userRateLimiter: NewUserRateLimiter(100, 1000), // 100/hour, 1000/day per user
		vacationSem:     make(chan struct{}, 10),       // Limit to 10 concurrent vacation responses
	}, nil
}

// SetLocalDeliveryNotifier sets the callback for local delivery notifications
func (b *Backend) SetLocalDeliveryNotifier(notifier LocalDeliveryNotifier) {
	b.onLocalDelivery = notifier
}

// SetGreylister sets the greylisting handler
func (b *Backend) SetGreylister(gl *greylist.Greylister) {
	b.greylister = gl
}

// SetSieveExecutor sets the Sieve script executor for mail filtering
func (b *Backend) SetSieveExecutor(executor *sieve.Executor) {
	b.sieveExecutor = executor
}

// SetFeaturesStore sets the features store for Screener, Aliases, etc.
func (b *Backend) SetFeaturesStore(store *features.Store) {
	b.featuresStore = store
}

// NewSession is called when a new SMTP connection is established
func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	if b == nil {
		return nil, fmt.Errorf("backend is nil")
	}
	if c == nil {
		return nil, fmt.Errorf("connection is nil")
	}

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

// AuthMechanisms returns the list of supported authentication mechanisms
func (s *Session) AuthMechanisms() []string {
	return []string{sasl.Plain}
}

// Auth handles SASL authentication
func (s *Session) Auth(mech string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(identity, username, password string) error {
		user, err := s.backend.authenticator.Authenticate(s.ctx, username, password)
		if err != nil {
			s.backend.logger.WarnContext(s.ctx, "Authentication failed",
				"username", username,
				"remote_addr", s.remoteAddr,
			)
			// Log failed auth attempt
			s.backend.authenticator.LogAuthAttempt(s.ctx, nil, username, s.remoteAddr, "smtp", false, err.Error())
			metrics.RecordAuth(false, "smtp")
			return smtp.ErrAuthFailed
		}

		s.user = user
		s.ctx = logging.WithUserID(s.ctx, user.ID)
		s.backend.logger.InfoContext(s.ctx, "User authenticated",
			"username", username,
		)
		// Log successful auth attempt
		s.backend.authenticator.LogAuthAttempt(s.ctx, &user.ID, username, s.remoteAddr, "smtp", true, "")
		metrics.RecordAuth(true, "smtp")
		return nil
	}), nil
}

// Mail is called when the MAIL FROM command is received
func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	// Submission requires authentication
	if s.isSubmission && s.user == nil {
		return &smtp.SMTPError{
			Code:         530,
			EnhancedCode: smtp.EnhancedCode{5, 7, 0},
			Message:      "Authentication required",
		}
	}

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
		// Submission requires authentication - reject if not authenticated
		if s.user == nil {
			return &smtp.SMTPError{
				Code:         530,
				EnhancedCode: smtp.EnhancedCode{5, 7, 0},
				Message:      "Authentication required",
			}
		}
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

	// Check greylisting for inbound mail
	if s.backend.greylister != nil && s.backend.greylister.IsEnabled() {
		allow, firstTime, err := s.backend.greylister.Check(s.ctx, s.remoteAddr, s.from, to)
		if err != nil {
			s.backend.logger.WarnContext(s.ctx, "Greylist check failed",
				"error", err.Error(),
				"sender", s.from,
				"recipient", to,
			)
			// On error, allow the message (fail open)
		} else if !allow {
			if firstTime {
				metrics.GreylistChecks.WithLabelValues("deferred_new").Inc()
				s.backend.logger.InfoContext(s.ctx, "Greylisted new sender",
					"sender_ip", s.remoteAddr,
					"sender", s.from,
					"recipient", to,
				)
			} else {
				metrics.GreylistChecks.WithLabelValues("deferred_retry").Inc()
			}
			return &smtp.SMTPError{
				Code:         451,
				EnhancedCode: smtp.EnhancedCode{4, 7, 1},
				Message:      "Greylisting in effect, please retry in a few minutes",
			}
		} else {
			metrics.GreylistChecks.WithLabelValues("passed").Inc()
		}
	}

	s.rcpts = append(s.rcpts, to)
	return nil
}

// Data is called when the DATA command is received
func (s *Session) Data(r io.Reader) error {
	// Defensive nil checks
	if s == nil || s.backend == nil {
		return fmt.Errorf("session or backend is nil")
	}
	if r == nil {
		return &smtp.SMTPError{
			Code:         451,
			EnhancedCode: smtp.EnhancedCode{4, 3, 0},
			Message:      "Invalid data reader",
		}
	}

	if len(s.rcpts) == 0 {
		return &smtp.SMTPError{
			Code:         503,
			EnhancedCode: smtp.EnhancedCode{5, 5, 1},
			Message:      "No recipients specified",
		}
	}

	// Check for context cancellation
	if err := s.ctx.Err(); err != nil {
		return fmt.Errorf("operation cancelled: %w", err)
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

	// Record message received
	metrics.MessagesReceived.Inc()

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

	// Check for context cancellation
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("operation cancelled: %w", err)
	}

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
			if err := s.backend.deliveryEngine.Enqueue(ctx, s.from, []string{*external}, messagePath); err != nil {
				// Clean up the orphaned queue file
				if cleanupErr := os.Remove(messagePath); cleanupErr != nil {
					s.backend.logger.WarnContext(ctx, "Failed to cleanup queue file after enqueue failure",
						"path", messagePath,
						"error", cleanupErr.Error(),
					)
				}
				return err
			}
			return nil
		}
		s.backend.logger.WarnContext(ctx, "External forwarding not available - delivery engine not configured",
			"external_addr", *external,
		)
		return nil
	}

	// Check for features-based email alias (masked/disposable addresses)
	var aliasID int64
	if s.backend.featuresStore != nil && userID == nil {
		alias, err := s.backend.featuresStore.GetAliasByAddress(ctx, rcpt)
		if err == nil && alias != nil {
			if !alias.IsActive {
				s.backend.logger.InfoContext(ctx, "Email alias is disabled, rejecting",
					"alias", rcpt,
				)
				return &smtp.SMTPError{
					Code:         550,
					EnhancedCode: smtp.EnhancedCode{5, 1, 1},
					Message:      "Mailbox disabled",
				}
			}
			// Resolve to the alias owner
			userID = &alias.UserID
			aliasID = alias.ID
			s.backend.logger.InfoContext(ctx, "Email delivered via alias",
				"alias", rcpt,
				"user_id", alias.UserID,
			)
		}
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

	// Track alias usage (increment count) if delivered via alias
	if aliasID > 0 && s.backend.featuresStore != nil {
		if err := s.backend.featuresStore.IncrementAliasCount(ctx, aliasID); err != nil {
			s.backend.logger.WarnContext(ctx, "Failed to increment alias count",
				"alias_id", aliasID,
				"error", err.Error(),
			)
		}
	}

	// Check Screener (first-contact filtering) if enabled
	targetMailbox := "INBOX"
	screenerHandled := false
	if s.backend.featuresStore != nil {
		prefs, err := s.backend.featuresStore.GetPreferences(ctx, user.ID)
		if err == nil && prefs.ScreenerEnabled {
			// Get sender email from the from address
			senderEmail := s.from
			status, err := s.backend.featuresStore.GetScreenerStatus(ctx, user.ID, senderEmail)
			if err != nil {
				s.backend.logger.WarnContext(ctx, "Screener lookup failed, delivering normally",
					"error", err.Error(),
					"sender", senderEmail,
				)
			} else {
				switch status {
				case features.ScreenerBlocked:
					// Blocked senders go to Trash
					s.backend.logger.InfoContext(ctx, "Sender blocked by Screener, delivering to Trash",
						"sender", senderEmail,
						"recipient", rcpt,
					)
					targetMailbox = "Trash"
					screenerHandled = true
				case features.ScreenerPending:
					// Unknown/pending senders go to Screener mailbox
					s.backend.logger.InfoContext(ctx, "Unknown sender held in Screener",
						"sender", senderEmail,
						"recipient", rcpt,
					)
					targetMailbox = "Screener"
					screenerHandled = true
				case features.ScreenerApproved:
					// Approved senders go through normal flow
					s.backend.logger.DebugContext(ctx, "Sender approved by Screener",
						"sender", senderEmail,
					)
				}
			}
		}
	}

	// Parse message for Sieve and zone detection
	var msg *sieve.Message
	if s.backend.sieveExecutor != nil || s.backend.featuresStore != nil {
		msg = s.parseMessageForSieve(data)
	}

	// Execute Sieve filtering if available (skip if Screener already handled routing)
	if s.backend.sieveExecutor != nil && msg != nil {
		result, err := s.backend.sieveExecutor.Execute(ctx, user.ID, msg)
		if err != nil {
			s.backend.logger.WarnContext(ctx, "Sieve execution failed, delivering to INBOX",
				"error", err.Error(),
			)
		} else if result != nil {
			// Handle discard
			if result.Discarded {
				s.backend.logger.InfoContext(ctx, "Message discarded by Sieve",
					"recipient", rcpt,
				)
				return nil
			}

			// Handle reject
			if result.Rejected {
				s.backend.logger.InfoContext(ctx, "Message rejected by Sieve",
					"recipient", rcpt,
					"reason", result.RejectMsg,
				)
				return fmt.Errorf("message rejected: %s", result.RejectMsg)
			}

			// Handle redirect
			if result.Redirected && len(result.RedirectTo) > 0 {
				if s.backend.deliveryEngine != nil {
					messagePath, err := s.saveMessageToQueue(data)
					if err != nil {
						return fmt.Errorf("failed to save message for redirect: %w", err)
					}
					if err := s.backend.deliveryEngine.Enqueue(ctx, s.from, result.RedirectTo, messagePath); err != nil {
						s.backend.logger.ErrorContext(ctx, "Failed to enqueue redirected message", err)
						// Clean up the orphaned queue file
						if cleanupErr := os.Remove(messagePath); cleanupErr != nil {
							s.backend.logger.WarnContext(ctx, "Failed to cleanup queue file after enqueue failure",
								"path", messagePath,
								"error", cleanupErr.Error(),
							)
						}
					} else {
						s.backend.logger.InfoContext(ctx, "Message redirected by Sieve",
							"recipient", rcpt,
							"redirect_to", result.RedirectTo,
						)
					}
				}
				if !result.Keep {
					return nil
				}
			}

			// Handle fileinto (only if Screener hasn't overridden)
			if result.Filed && result.FileInto != "" && !screenerHandled {
				targetMailbox = result.FileInto
			}

			// Handle vacation response
			if result.Vacation && result.VacationTo != "" {
				// Launch vacation response with semaphore to limit concurrent goroutines
				select {
				case s.backend.vacationSem <- struct{}{}:
					// Acquired semaphore slot
					go func() {
						defer func() {
							<-s.backend.vacationSem // Release semaphore
							if r := recover(); r != nil {
								s.backend.logger.ErrorContext(ctx, "Panic in vacation response goroutine", fmt.Errorf("panic: %v", r))
							}
						}()
						s.sendVacationResponse(ctx, result, user)
					}()
				default:
					// Semaphore full - skip vacation response to avoid backpressure
					s.backend.logger.WarnContext(ctx, "Vacation response skipped - too many pending",
						"recipient", rcpt,
					)
				}
			}
		}
	}

	// Get target mailbox (INBOX or fileinto folder)
	mailbox, err := s.backend.store.GetMailbox(ctx, user.ID, targetMailbox)
	if err != nil {
		// If target folder doesn't exist, try to create it, or fall back to INBOX
		if targetMailbox != "INBOX" {
			s.backend.logger.WarnContext(ctx, "Sieve target folder not found, creating",
				"folder", targetMailbox,
			)
			mailbox, err = s.backend.store.CreateMailbox(ctx, user.ID, targetMailbox, "")
			if err != nil {
				s.backend.logger.WarnContext(ctx, "Failed to create Sieve target folder, using INBOX",
					"folder", targetMailbox,
					"error", err.Error(),
				)
				mailbox, err = s.backend.store.GetMailbox(ctx, user.ID, "INBOX")
				if err != nil {
					return fmt.Errorf("INBOX not found: %w", err)
				}
				targetMailbox = "INBOX"
			}
		} else {
			return fmt.Errorf("INBOX not found: %w", err)
		}
	}

	// Check quota before delivery
	messageSize := int64(len(data))
	if user.QuotaBytes > 0 {
		if user.UsedBytes+messageSize > user.QuotaBytes {
			s.backend.logger.WarnContext(ctx, "Quota exceeded for user",
				"recipient", rcpt,
				"quota", user.QuotaBytes,
				"used", user.UsedBytes,
				"message_size", messageSize,
			)
			metrics.QuotaExceeded.Inc()
			return &smtp.SMTPError{
				Code:         452,
				EnhancedCode: smtp.EnhancedCode{4, 2, 2},
				Message:      "Mailbox quota exceeded",
			}
		}
	}

	// Parse message for zone detection (reuse Sieve parsed message if available)
	var msgHeaders map[string][]string
	var msgSubject string
	if msg != nil {
		msgHeaders = msg.Headers
		msgSubject = msg.Subject
	} else {
		// Parse headers if we didn't go through Sieve
		parsedMsg := s.parseMessageForSieve(data)
		msgHeaders = parsedMsg.Headers
		msgSubject = parsedMsg.Subject
	}

	// Detect zone for the message
	zone := s.detectMessageZone(ctx, user.ID, s.from, msgHeaders, msgSubject)

	// Deliver message (use bytes.NewReader to avoid string allocation)
	deliveredMsg, err := s.backend.store.AppendMessage(ctx, mailbox.ID, nil, time.Now(),
		bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to append message: %w", err)
	}

	// Update message zone if not default
	if zone != "inbox" && deliveredMsg != nil {
		if err := s.backend.store.UpdateMessageZone(ctx, deliveredMsg.ID, zone); err != nil {
			s.backend.logger.WarnContext(ctx, "Failed to update message zone",
				"message_id", deliveredMsg.ID,
				"zone", zone,
				"error", err.Error(),
			)
		} else {
			s.backend.logger.DebugContext(ctx, "Message zone set",
				"message_id", deliveredMsg.ID,
				"zone", zone,
			)
		}
	}

	// Update used bytes after successful delivery
	if err := s.backend.authenticator.UpdateUsedBytes(ctx, user.ID, messageSize); err != nil {
		s.backend.logger.WarnContext(ctx, "Failed to update used bytes",
			"user_id", user.ID,
			"delta", messageSize,
			"error", err.Error(),
		)
		// Don't fail the delivery, just log the warning
	}

	// Notify IMAP clients about new message (for IDLE support) - async for speed
	if s.backend.onLocalDelivery != nil {
		// Launch notification in goroutine with panic recovery
		go func() {
			defer func() {
				if r := recover(); r != nil {
					s.backend.logger.ErrorContext(ctx, "Panic in local delivery notification goroutine", fmt.Errorf("panic: %v", r))
				}
			}()
			s.backend.onLocalDelivery(user.Email, targetMailbox)
		}()
	}

	return nil
}

// handleOutbound queues mail for external delivery
func (s *Session) handleOutbound(data []byte) error {
	// Check for context cancellation
	if err := s.ctx.Err(); err != nil {
		return fmt.Errorf("operation cancelled: %w", err)
	}

	// Check per-user rate limit for authenticated users
	if s.user != nil && s.backend.userRateLimiter != nil {
		if err := s.backend.userRateLimiter.CheckAndIncrement(s.user.ID); err != nil {
			s.backend.logger.WarnContext(s.ctx, "User rate limit exceeded",
				"user_id", s.user.ID,
				"email", s.user.Email,
				"error", err.Error(),
			)
			return &smtp.SMTPError{
				Code:         452,
				EnhancedCode: smtp.EnhancedCode{4, 7, 1},
				Message:      "Too many messages sent, please try again later",
			}
		}
	}

	// Separate local and external recipients
	// Check against all managed domains in the database, not just the primary domain
	var localRcpts, externalRcpts []string

	for _, rcpt := range s.rcpts {
		_, domain := parseAddress(rcpt)
		// Check if domain exists in database (managed domain)
		_, err := s.backend.authenticator.GetDomainID(s.ctx, domain)
		if err == nil {
			// Domain exists in database - it's a local recipient
			localRcpts = append(localRcpts, rcpt)
		} else {
			// Domain not found or inactive - external recipient
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
			// Check for context cancellation in loop
			if err := s.ctx.Err(); err != nil {
				return fmt.Errorf("operation cancelled during local delivery: %w", err)
			}

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

		// Check for context cancellation before queueing
		if err := s.ctx.Err(); err != nil {
			return fmt.Errorf("operation cancelled before queueing: %w", err)
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
			// Clean up the orphaned queue file
			if cleanupErr := os.Remove(messagePath); cleanupErr != nil {
				s.backend.logger.WarnContext(s.ctx, "Failed to cleanup queue file after enqueue failure",
					"path", messagePath,
					"error", cleanupErr.Error(),
				)
			}
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
			// Use bytes.NewReader to avoid string allocation
			_, err = s.backend.store.AppendMessage(ctx, sent.ID, flags, time.Now(),
				bytes.NewReader(data))
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
	// Defensive nil checks
	if s == nil || s.backend == nil {
		return "", fmt.Errorf("session or backend is nil")
	}
	if len(data) == 0 {
		return "", fmt.Errorf("message data is empty")
	}

	// Generate unique filename
	filename := fmt.Sprintf("%d-%s.eml", time.Now().UnixNano(), generateID())
	path := filepath.Join(s.backend.queuePath, filename)

	// Write file atomically using a temp file with secure permissions
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil { // Owner-only access for email data
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, path); err != nil {
		// Clean up temp file on failure
		os.Remove(tempPath)
		return "", fmt.Errorf("failed to rename temp file: %w", err)
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

// parseAddress extracts local part and domain from an email address.
// Optimized to avoid allocations when possible.
func parseAddress(addr string) (local, domain string) {
	// Handle <addr> format without allocation
	if len(addr) > 0 && addr[0] == '<' {
		addr = addr[1:]
	}
	if len(addr) > 0 && addr[len(addr)-1] == '>' {
		addr = addr[:len(addr)-1]
	}

	// Find @ without SplitN allocation
	atIdx := strings.IndexByte(addr, '@')
	if atIdx >= 0 {
		localPart := addr[:atIdx]
		domainPart := addr[atIdx+1:]
		// Only allocate if not already lowercase
		return toLowerIfNeeded(localPart), toLowerIfNeeded(domainPart)
	}
	return addr, ""
}

// toLowerIfNeeded returns lowercase string, avoiding allocation if already lowercase.
func toLowerIfNeeded(s string) string {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			// Found uppercase, need to allocate
			return strings.ToLower(s)
		}
	}
	return s // Already lowercase, no allocation
}

// generateID generates a cryptographically secure unique ID.
// Uses buffer pool to reduce allocations.
func generateID() string {
	bufPtr := idBufferPool.Get().(*[]byte)
	b := *bufPtr
	defer idBufferPool.Put(bufPtr)

	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if crypto/rand fails (should never happen)
		return fmt.Sprintf("%d-%x", time.Now().UnixNano(), time.Now().UnixNano()%0xFFFFFF)
	}
	return hex.EncodeToString(b)
}

// detectMessageZone determines the zone for a message based on headers and sender
// Returns: "priority", "feed", "paper_trail", or "inbox"
func (s *Session) detectMessageZone(ctx context.Context, userID int64, senderEmail string, headers map[string][]string, subject string) string {
	// Check if zones are enabled in preferences
	if s.backend.featuresStore == nil {
		return "inbox"
	}
	prefs, err := s.backend.featuresStore.GetPreferences(ctx, userID)
	if err != nil || prefs == nil || !prefs.ZonesEnabled {
		return "inbox"
	}

	// 1. Check if sender is VIP -> priority
	isVIP, _ := s.backend.featuresStore.IsVIP(ctx, userID, senderEmail)
	if isVIP {
		return "priority"
	}

	// 2. Check for newsletter indicators -> feed
	// Check List-Unsubscribe header
	if _, hasListUnsub := headers["List-Unsubscribe"]; hasListUnsub {
		return "feed"
	}
	// Check Precedence header (bulk or list)
	if prec, hasPrecedence := headers["Precedence"]; hasPrecedence {
		for _, p := range prec {
			p = strings.ToLower(p)
			if p == "bulk" || p == "list" {
				return "feed"
			}
		}
	}
	// Check for common newsletter From patterns
	fromLower := strings.ToLower(senderEmail)
	newsletterPatterns := []string{"newsletter", "updates@", "digest@", "noreply@", "no-reply@", "marketing@"}
	for _, pattern := range newsletterPatterns {
		if strings.Contains(fromLower, pattern) {
			return "feed"
		}
	}

	// 3. Check for receipt/transactional indicators -> paper_trail
	subjectLower := strings.ToLower(subject)
	receiptKeywords := []string{"receipt", "order confirmation", "shipping", "invoice", "payment", "subscription", "your order", "order #"}
	for _, kw := range receiptKeywords {
		if strings.Contains(subjectLower, kw) {
			return "paper_trail"
		}
	}
	// Check From patterns for receipts
	receiptFromPatterns := []string{"orders@", "receipts@", "shipping@", "billing@", "payments@"}
	for _, pattern := range receiptFromPatterns {
		if strings.Contains(fromLower, pattern) {
			return "paper_trail"
		}
	}

	return "inbox"
}

// parseMessageForSieve parses raw email data into a Sieve message structure.
// Uses pooled bufio.Reader to reduce allocations.
func (s *Session) parseMessageForSieve(data []byte) *sieve.Message {
	msg := &sieve.Message{
		Headers: make(map[string][]string),
		Size:    int64(len(data)),
	}

	// Get pooled bufio.Reader
	reader := bufioReaderPool.Get().(*bufio.Reader)
	reader.Reset(bytes.NewReader(data))
	defer bufioReaderPool.Put(reader)

	// Parse headers using textproto
	tp := textproto.NewReader(reader)
	headers, err := tp.ReadMIMEHeader()
	if err != nil && len(headers) == 0 {
		// Failed to parse headers, return minimal message
		msg.From = s.from
		return msg
	}

	// Copy headers
	for key, values := range headers {
		msg.Headers[key] = values
	}

	// Extract common headers
	if from := headers.Get("From"); from != "" {
		msg.From = from
	} else {
		msg.From = s.from
	}

	if to := headers.Get("To"); to != "" {
		msg.To = strings.Split(to, ",")
		for i := range msg.To {
			msg.To[i] = strings.TrimSpace(msg.To[i])
		}
	}

	if subject := headers.Get("Subject"); subject != "" {
		msg.Subject = subject
	}

	return msg
}

// sendVacationResponse sends an automatic vacation reply.
// Uses pooled bytes.Buffer to reduce allocations.
func (s *Session) sendVacationResponse(ctx context.Context, result *sieve.Result, user *auth.User) {
	// Defensive nil checks
	if s == nil || s.backend == nil {
		return
	}
	if result == nil || user == nil {
		s.backend.logger.WarnContext(ctx, "Cannot send vacation response - nil result or user")
		return
	}
	if s.backend.deliveryEngine == nil {
		s.backend.logger.WarnContext(ctx, "Cannot send vacation response - delivery engine not configured")
		return
	}

	// Build vacation message
	// Extract sender domain for Message-ID (multi-domain support)
	_, senderDomain := parseAddress(user.Email)
	if senderDomain == "" {
		senderDomain = s.backend.config.Server.Domain // fallback
	}

	// Get pooled buffer
	msg := bytesBufferPool.Get().(*bytes.Buffer)
	msg.Reset()
	defer bytesBufferPool.Put(msg)

	msg.WriteString(fmt.Sprintf("From: %s\r\n", user.Email))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", result.VacationTo))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", result.VacationSubject))
	msg.WriteString("Auto-Submitted: auto-replied\r\n")
	msg.WriteString("X-Auto-Response-Suppress: All\r\n")
	msg.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	msg.WriteString(fmt.Sprintf("Message-ID: <%s@%s>\r\n", generateID(), senderDomain))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(result.VacationBody)

	// Save and enqueue - copy bytes since buffer will be reused
	msgData := make([]byte, msg.Len())
	copy(msgData, msg.Bytes())
	messagePath, err := s.saveMessageToQueue(msgData)
	if err != nil {
		s.backend.logger.ErrorContext(ctx, "Failed to save vacation response", err)
		return
	}

	if err := s.backend.deliveryEngine.Enqueue(ctx, user.Email, []string{result.VacationTo}, messagePath); err != nil {
		s.backend.logger.ErrorContext(ctx, "Failed to enqueue vacation response", err)
		// Clean up the orphaned queue file
		if cleanupErr := os.Remove(messagePath); cleanupErr != nil {
			s.backend.logger.WarnContext(ctx, "Failed to cleanup queue file after vacation enqueue failure",
				"path", messagePath,
				"error", cleanupErr.Error(),
			)
		}
		return
	}

	s.backend.logger.InfoContext(ctx, "Vacation response queued",
		"to", result.VacationTo,
	)
}
