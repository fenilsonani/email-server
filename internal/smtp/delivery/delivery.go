// Package delivery implements outbound email delivery with circuit breakers and retry logic.
package delivery

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/metrics"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/resilience"
	"github.com/fenilsonani/email-server/internal/security"
	"github.com/fenilsonani/email-server/internal/tracing"
)

// Common errors
var (
	ErrPermanentFailure = errors.New("permanent delivery failure")
	ErrTemporaryFailure = errors.New("temporary delivery failure")
	ErrCircuitOpen      = errors.New("circuit breaker open for domain")
	ErrAllMXFailed      = errors.New("all MX servers failed")
	ErrMessageTooLarge  = errors.New("message too large")
	ErrInvalidRecipient = errors.New("invalid recipient")
)

// Config configures the delivery engine.
type Config struct {
	// Workers is the number of concurrent delivery workers.
	Workers int
	// Hostname is the HELO/EHLO hostname.
	Hostname string
	// ConnectTimeout is the TCP connection timeout.
	ConnectTimeout time.Duration
	// CommandTimeout is the SMTP command timeout.
	CommandTimeout time.Duration
	// MaxMessageSize is the maximum message size in bytes.
	MaxMessageSize int64
	// RequireTLS requires TLS for outbound delivery.
	RequireTLS bool
	// VerifyTLS verifies TLS certificates.
	VerifyTLS bool
	// QueuePath is the base path for queued message files (for safe cleanup verification)
	QueuePath string
	// RelayHost is an optional smarthost for all outbound mail (host:port).
	RelayHost string

	// MTA-STS configuration
	MTASTSEnabled bool // Enable MTA-STS policy checking

	// DANE configuration
	DANEEnabled   bool   // Enable DANE/TLSA checking
	DANEDNSServer string // DNS server for TLSA lookups
}

// DefaultConfig returns sensible default configuration.
func DefaultConfig() Config {
	return Config{
		Workers:        4,
		Hostname:       "localhost",
		ConnectTimeout: 30 * time.Second,
		CommandTimeout: 5 * time.Minute,
		MaxMessageSize: 25 * 1024 * 1024, // 25MB
		RequireTLS:     false,
		VerifyTLS:      true,
	}
}

// Engine handles outbound email delivery.
type Engine struct {
	config     Config
	queue      *queue.RedisQueue
	mxResolver *MXResolver
	dkimPool   *security.DKIMSignerPool
	breakers   *resilience.BreakerRegistry
	logger     *logging.Logger
	bounceGen  *BounceGenerator
	db         *sql.DB

	// Security resolvers
	stsResolver  *STSResolver  // MTA-STS policy resolver
	daneResolver *DANEResolver // DANE/TLSA resolver

	// Observability
	tracer      *tracing.Tracer
	domainStats *metrics.DomainStats

	// Deduplication
	dedupTracker *queue.DeliveryTracker

	// External event handler for webhooks, suppression, etc.
	// Stored as atomic.Value for safe concurrent access since Start()
	// may be called before SetEventHandler().
	eventHandler atomic.Value // stores DeliveryEventHandler

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	mu           sync.RWMutex
	totalSent    int64
	totalFailed  int64
	totalRetried int64
	totalBounced int64
}

// DeliveryEvent contains information about a delivery outcome for external consumers.
type DeliveryEvent struct {
	// SMTPMessageID is the Message-ID header value (without angle brackets).
	SMTPMessageID string
	// Recipients is the list of recipient addresses.
	Recipients []string
	// Sender is the envelope sender.
	Sender string
	// Status is "delivered", "bounced", or "failed".
	Status string
	// SMTPCode is the SMTP response code (e.g., 250, 550).
	SMTPCode int
	// ErrorMessage is the error/bounce reason (empty on success).
	ErrorMessage string
	// Domain is the recipient domain.
	Domain string
	// Attempt is the delivery attempt number.
	Attempt int
}

// DeliveryEventHandler is called by the delivery engine when a message
// reaches a terminal state. Implementations must be safe for concurrent use.
type DeliveryEventHandler func(ctx context.Context, event DeliveryEvent)

// EngineOption configures the delivery engine.
type EngineOption func(*Engine)

// WithTracer sets the tracer for the delivery engine.
func WithTracer(t *tracing.Tracer) EngineOption {
	return func(e *Engine) {
		e.tracer = t
	}
}

// WithDomainStats sets the domain stats tracker for the delivery engine.
func WithDomainStats(ds *metrics.DomainStats) EngineOption {
	return func(e *Engine) {
		e.domainStats = ds
	}
}

// WithDedupTracker sets the deduplication tracker for the delivery engine.
func WithDedupTracker(dt *queue.DeliveryTracker) EngineOption {
	return func(e *Engine) {
		e.dedupTracker = dt
	}
}

// WithEventHandler sets a callback for delivery events (delivered, bounced, failed).
func WithEventHandler(h DeliveryEventHandler) EngineOption {
	return func(e *Engine) {
		e.eventHandler.Store(h)
	}
}

// SetEventHandler sets the delivery event handler. Safe to call concurrently
// with running workers (e.g., when the API server initializes after Start()).
func (e *Engine) SetEventHandler(h DeliveryEventHandler) {
	e.eventHandler.Store(h)
}

// NewEngine creates a new delivery engine.
func NewEngine(cfg Config, q *queue.RedisQueue, dkim *security.DKIMSignerPool, logger *logging.Logger, db *sql.DB, opts ...EngineOption) *Engine {
	ctx, cancel := context.WithCancel(context.Background())

	e := &Engine{
		config:     cfg,
		queue:      q,
		mxResolver: NewMXResolver(DefaultMXResolverConfig()),
		dkimPool:   dkim,
		breakers: resilience.NewBreakerRegistry(func(key string) resilience.Config {
			return resilience.Config{
				Name:             "smtp:" + key,
				FailureThreshold: 5,
				SuccessThreshold: 2,
				Timeout:          5 * time.Minute,
				HalfOpenMaxCalls: 2,
				ExecutionTimeout: 2 * time.Minute,
			}
		}),
		logger:    logger.Delivery(),
		bounceGen: NewBounceGenerator(cfg.Hostname),
		db:        db,
		ctx:       ctx,
		cancel:    cancel,
	}

	// Apply options
	for _, opt := range opts {
		opt(e)
	}

	// Initialize MTA-STS resolver if enabled
	if cfg.MTASTSEnabled {
		e.stsResolver = NewSTSResolver(DefaultSTSResolverConfig())
	}

	// Initialize DANE resolver if enabled
	if cfg.DANEEnabled {
		daneCfg := DefaultDANEResolverConfig()
		if cfg.DANEDNSServer != "" {
			daneCfg.DNSServer = cfg.DANEDNSServer
		}
		e.daneResolver = NewDANEResolver(daneCfg)
	}

	return e
}

// Start starts the delivery workers.
func (e *Engine) Start() {
	e.logger.Info("Starting delivery engine", "workers", e.config.Workers)

	// Pre-open circuit breakers for domains with recent consecutive failures
	// to avoid hammering known-broken servers on restart
	e.warmupCircuitBreakers()

	for i := 0; i < e.config.Workers; i++ {
		e.wg.Add(1)
		go e.worker(i)
	}

	// Start stale message recovery
	e.wg.Add(1)
	go e.recoveryWorker()
}

// Stop gracefully stops the delivery engine.
func (e *Engine) Stop() {
	e.logger.Info("Stopping delivery engine")
	e.cancel()
	e.wg.Wait()
	e.logger.Info("Delivery engine stopped")
}

// Enqueue adds a message for delivery.
func (e *Engine) Enqueue(ctx context.Context, sender string, recipients []string, messagePath string) error {
	// Validate message file exists and get size
	info, err := os.Stat(messagePath)
	if err != nil {
		return fmt.Errorf("message file not found: %w", err)
	}

	if info.Size() > e.config.MaxMessageSize {
		return ErrMessageTooLarge
	}

	// Group recipients by domain
	byDomain := make(map[string][]string)
	for _, rcpt := range recipients {
		domain := extractDomain(rcpt)
		if domain == "" {
			e.logger.WarnContext(ctx, "Invalid recipient address", "recipient", rcpt)
			continue
		}
		byDomain[domain] = append(byDomain[domain], rcpt)
	}

	// Create one queue message per domain
	for domain, rcpts := range byDomain {
		msg := &queue.Message{
			Sender:      sender,
			Recipients:  rcpts,
			MessagePath: messagePath,
			Size:        info.Size(),
			Domain:      domain,
		}

		if err := e.queue.Enqueue(ctx, msg); err != nil {
			return fmt.Errorf("failed to enqueue for domain %s: %w", domain, err)
		}

		e.logger.InfoContext(ctx, "Message enqueued",
			"domain", domain,
			"recipients", len(rcpts),
			"size", info.Size(),
		)
	}

	return nil
}

// worker is a delivery worker goroutine.
func (e *Engine) worker(id int) {
	defer e.wg.Done()

	e.logger.Debug("Delivery worker started", "worker_id", id)

	for {
		select {
		case <-e.ctx.Done():
			e.logger.Debug("Delivery worker stopping", "worker_id", id)
			return
		default:
		}

		// Try to get a message
		msg, err := e.queue.Dequeue(e.ctx)
		if err != nil {
			if !errors.Is(err, queue.ErrQueueClosed) {
				e.logger.Error("Failed to dequeue message", "error", err.Error(), "worker_id", id)
			}
			time.Sleep(time.Second)
			continue
		}

		if msg == nil {
			// No messages ready, wait a bit
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Deliver the message
		e.deliverMessage(msg)
	}
}

// deliverMessage attempts to deliver a single message.
func (e *Engine) deliverMessage(msg *queue.Message) {
	ctx := logging.WithMessageID(e.ctx, msg.ID)
	logger := e.logger.WithFields("message_id", msg.ID, "domain", msg.Domain)

	// Start tracing span
	var span *tracing.Span
	if e.tracer != nil {
		ctx, span = e.tracer.StartSpan(ctx, "deliver_message")
		span.SetTag("message_id", msg.ID)
		span.SetTag("domain", msg.Domain)
		span.SetTag("attempt", fmt.Sprintf("%d", msg.Attempts))
		defer span.Finish()
	}

	// Track delivery timing for metrics
	startTime := time.Now()
	var deliverySuccess bool
	defer func() {
		duration := time.Since(startTime)
		if e.domainStats != nil {
			e.domainStats.RecordDelivery(msg.Domain, deliverySuccess, duration)
		}
	}()

	// Extract SMTP Message-ID for deduplication
	smtpMessageID := e.extractMessageID(msg.MessagePath)
	workerID := fmt.Sprintf("worker-%d", time.Now().UnixNano())

	// Check deduplication before delivery
	if e.dedupTracker != nil && smtpMessageID != "" {
		if err := e.dedupTracker.StartDelivery(ctx, smtpMessageID, msg.ID, workerID, msg.Recipients); err != nil {
			if errors.Is(err, queue.ErrAlreadyDelivered) {
				logger.InfoContext(ctx, "Message already delivered (dedup), skipping",
					"smtp_message_id", smtpMessageID)
				e.queue.Complete(ctx, msg.ID)
				deliverySuccess = true
				if span != nil {
					span.SetTag("dedup", "skipped")
				}
				return
			}
			logger.WarnContext(ctx, "Dedup check failed, continuing delivery",
				"error", err.Error())
		}
	}

	logger.InfoContext(ctx, "Attempting delivery",
		"attempt", msg.Attempts,
		"recipients", len(msg.Recipients),
	)

	// Check circuit breaker for this domain
	breaker := e.breakers.Get(msg.Domain)
	if breaker == nil {
		err := fmt.Errorf("invalid domain: %q", msg.Domain)
		logger.ErrorContext(ctx, "No circuit breaker available (empty domain?), failing delivery", err)
		e.queue.Fail(ctx, msg.ID, err.Error())
		e.mu.Lock()
		e.totalFailed++
		e.mu.Unlock()
		for _, rcpt := range msg.Recipients {
			e.logDeliveryWithTrace(ctx, msg.ID, msg.Sender, rcpt, "rejected", 0, err.Error(), msg.Domain, msg.Attempts, startTime, "")
		}
		if e.dedupTracker != nil && smtpMessageID != "" {
			e.dedupTracker.MarkFailed(ctx, smtpMessageID, err.Error())
		}
		e.updateSentEmailStatus(ctx, smtpMessageID, "failed", "", err.Error(), msg.Attempts)
		e.fireEvent(ctx, DeliveryEvent{
			SMTPMessageID: smtpMessageID, Recipients: msg.Recipients, Sender: msg.Sender,
			Status: "failed", ErrorMessage: err.Error(), Domain: msg.Domain, Attempt: msg.Attempts,
		})
		if span != nil {
			span.SetError(err)
		}
		return
	}

	// Record circuit breaker state
	cbState := breaker.State()
	if span != nil {
		span.SetTag("circuit_breaker_state", cbState.String())
	}

	if cbState == resilience.StateOpen {
		logger.WarnContext(ctx, "Circuit breaker open, deferring")
		e.queue.Retry(ctx, msg.ID, ErrCircuitOpen)
		e.mu.Lock()
		e.totalRetried++
		e.mu.Unlock()
		// Log deferred status for each recipient
		for _, rcpt := range msg.Recipients {
			e.logDeliveryWithTrace(ctx, msg.ID, msg.Sender, rcpt, "deferred", 0, "circuit breaker open", msg.Domain, msg.Attempts, startTime, cbState.String())
		}
		if span != nil {
			span.SetTag("result", "deferred_circuit_open")
		}
		return
	}

	// Attempt delivery through circuit breaker
	err := breaker.Execute(ctx, func(ctx context.Context) error {
		return e.attemptDelivery(ctx, msg)
	})

	if err != nil {
		smtpCode := extractSMTPCode(err)
		// Determine if permanent or temporary
		if isPermanentError(err) {
			logger.ErrorContext(ctx, "Permanent delivery failure", err)
			e.queue.Fail(ctx, msg.ID, err.Error())
			e.mu.Lock()
			e.totalFailed++
			e.mu.Unlock()

			// Log rejected status for each recipient
			for _, rcpt := range msg.Recipients {
				e.logDeliveryWithTrace(ctx, msg.ID, msg.Sender, rcpt, "rejected", smtpCode, err.Error(), msg.Domain, msg.Attempts, startTime, cbState.String())
			}

			// Mark as failed in dedup tracker
			if e.dedupTracker != nil && smtpMessageID != "" {
				e.dedupTracker.MarkFailed(ctx, smtpMessageID, err.Error())
			}

			// Generate and send bounce message
			if ShouldBounce(msg.Sender) {
				if bounceErr := e.sendBounce(ctx, msg, err); bounceErr != nil {
					logger.WarnContext(ctx, "Failed to send bounce message",
						"error", bounceErr.Error())
				} else {
					e.mu.Lock()
					e.totalBounced++
					e.mu.Unlock()
					// Log bounce status
					e.logDeliveryWithTrace(ctx, msg.ID, "", msg.Sender, "bounced", 0, "", msg.Domain, msg.Attempts, startTime, "")
				}
			}

			// Update sent_emails status for API send logs
			e.updateSentEmailStatus(ctx, smtpMessageID, "bounced", "", err.Error(), msg.Attempts)
			e.fireEvent(ctx, DeliveryEvent{
				SMTPMessageID: smtpMessageID, Recipients: msg.Recipients, Sender: msg.Sender,
				Status: "bounced", SMTPCode: smtpCode, ErrorMessage: err.Error(),
				Domain: msg.Domain, Attempt: msg.Attempts,
			})

			// Clean up the original message file
			if err := e.cleanupMessageFile(msg.MessagePath); err != nil {
				logger.WarnContext(ctx, "Failed to cleanup message file after failure",
					"path", msg.MessagePath,
					"error", err.Error())
			}

			if span != nil {
				span.SetError(err)
				span.SetTag("result", "permanent_failure")
			}
		} else {
			logger.WarnContext(ctx, "Temporary delivery failure, will retry", "error", err.Error())
			e.queue.Retry(ctx, msg.ID, err)
			e.mu.Lock()
			e.totalRetried++
			e.mu.Unlock()
			// Log deferred status for each recipient
			for _, rcpt := range msg.Recipients {
				e.logDeliveryWithTrace(ctx, msg.ID, msg.Sender, rcpt, "deferred", smtpCode, err.Error(), msg.Domain, msg.Attempts, startTime, cbState.String())
			}
			// Record deferred attempt in delivery timeline (don't change sent_emails status)
			if smtpMessageID != "" {
				e.recordDeliveryAttempt(ctx, "<"+smtpMessageID+">", "deferred", "", err.Error(), msg.Attempts)
			}
			if span != nil {
				span.SetTag("result", "temporary_failure")
			}
		}
		return
	}

	// Success!
	deliverySuccess = true
	logger.InfoContext(ctx, "Message delivered successfully")
	e.queue.Complete(ctx, msg.ID)
	e.mu.Lock()
	e.totalSent++
	e.mu.Unlock()

	// Mark as delivered in dedup tracker
	if e.dedupTracker != nil && smtpMessageID != "" {
		e.dedupTracker.MarkDelivered(ctx, smtpMessageID, "250 OK")
	}

	// Log delivered status for each recipient
	for _, rcpt := range msg.Recipients {
		e.logDeliveryWithTrace(ctx, msg.ID, msg.Sender, rcpt, "delivered", 250, "", msg.Domain, msg.Attempts, startTime, cbState.String())
	}

	// Update sent_emails status for API send logs
	e.updateSentEmailStatus(ctx, smtpMessageID, "delivered", "250 OK", "", msg.Attempts)
	e.fireEvent(ctx, DeliveryEvent{
		SMTPMessageID: smtpMessageID, Recipients: msg.Recipients, Sender: msg.Sender,
		Status: "delivered", SMTPCode: 250, Domain: msg.Domain, Attempt: msg.Attempts,
	})

	// Clean up the message file from disk
	if err := e.cleanupMessageFile(msg.MessagePath); err != nil {
		logger.WarnContext(ctx, "Failed to cleanup message file",
			"path", msg.MessagePath,
			"error", err.Error())
	}

	if span != nil {
		span.SetTag("result", "success")
	}
}

// sendBounce generates and sends a bounce message back to the sender.
func (e *Engine) sendBounce(ctx context.Context, msg *queue.Message, failureErr error) error {
	// Generate bounce message
	bounceData, err := e.bounceGen.Generate(msg, failureErr)
	if err != nil {
		return fmt.Errorf("failed to generate bounce: %w", err)
	}

	// Create temporary file for bounce message
	tmpFile, err := os.CreateTemp(e.config.QueuePath, "bounce-*.eml")
	if err != nil {
		return fmt.Errorf("failed to create bounce temp file: %w", err)
	}
	bouncePath := tmpFile.Name()

	if _, err := tmpFile.Write(bounceData); err != nil {
		tmpFile.Close()
		os.Remove(bouncePath)
		return fmt.Errorf("failed to write bounce message: %w", err)
	}
	tmpFile.Close()

	// Enqueue bounce for delivery (null sender as per RFC)
	bounceMsg := &queue.Message{
		Sender:      "", // Null sender for bounces
		Recipients:  []string{msg.Sender},
		MessagePath: bouncePath,
		Size:        int64(len(bounceData)),
		Domain:      extractDomain(msg.Sender),
	}

	if err := e.queue.Enqueue(ctx, bounceMsg); err != nil {
		os.Remove(bouncePath)
		return fmt.Errorf("failed to enqueue bounce: %w", err)
	}

	e.logger.InfoContext(ctx, "Bounce message queued",
		"original_message_id", msg.ID,
		"bounce_recipient", msg.Sender,
	)

	return nil
}

// cleanupMessageFile safely removes a message file after delivery
func (e *Engine) cleanupMessageFile(path string) error {
	if path == "" {
		return nil
	}

	// Safety check: only delete files within expected paths
	// This prevents accidental deletion of arbitrary files
	if e.config.QueuePath != "" && !strings.HasPrefix(path, e.config.QueuePath) {
		e.logger.Warn("Refusing to delete file outside queue path",
			"path", path,
			"queue_path", e.config.QueuePath)
		return nil
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove message file: %w", err)
	}
	return nil
}

// attemptDelivery tries to deliver to MX servers or relay host.
func (e *Engine) attemptDelivery(ctx context.Context, msg *queue.Message) error {
	// Read and sign the message
	messageData, err := e.readAndSignMessage(ctx, msg)
	if err != nil {
		return fmt.Errorf("failed to prepare message: %w", err)
	}

	// Use relay host if configured
	if e.config.RelayHost != "" {
		e.logger.DebugContext(ctx, "Using relay host", "relay", e.config.RelayHost)
		return e.deliverToRelay(ctx, msg, messageData)
	}

	// Resolve MX records
	mxHosts, err := e.mxResolver.LookupWithFallback(ctx, msg.Domain)
	if err != nil {
		return fmt.Errorf("MX lookup failed: %w", err)
	}

	// Try each MX host in preference order
	var lastErr error
	for _, mx := range mxHosts {
		for _, addr := range mx.Addresses {
			lastErr = e.deliverToHost(ctx, addr, mx.Host, msg, messageData)
			if lastErr == nil {
				return nil // Success
			}

			// Check if permanent error
			if isPermanentError(lastErr) {
				return lastErr
			}

			e.logger.DebugContext(ctx, "MX attempt failed, trying next",
				"host", mx.Host,
				"addr", addr,
				"error", lastErr.Error(),
			)
		}
	}

	return fmt.Errorf("%w: %v", ErrAllMXFailed, lastErr)
}

// deliverToRelay sends mail through the configured relay host.
func (e *Engine) deliverToRelay(ctx context.Context, msg *queue.Message, data []byte) error {
	host, port, err := net.SplitHostPort(e.config.RelayHost)
	if err != nil {
		// Assume port 25 if not specified
		host = e.config.RelayHost
		port = "25"
	}
	// Connect with timeout
	dialer := &net.Dialer{
		Timeout: e.config.ConnectTimeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return fmt.Errorf("relay connection failed: %w", err)
	}
	defer conn.Close()

	// Set overall deadline from context or config timeout
	deadline := time.Now().Add(e.config.CommandTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	conn.SetDeadline(deadline)

	// Create SMTP client
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer func() {
		client.Quit()
		client.Close()
	}()

	// Say hello
	if err := client.Hello(e.config.Hostname); err != nil {
		return fmt.Errorf("HELO failed: %w", err)
	}

	// Set sender
	if err := client.Mail(msg.Sender); err != nil {
		return classifyError(err)
	}

	// Set recipients - track successes
	successfulRecipients := 0
	var lastRcptErr error
	for _, rcpt := range msg.Recipients {
		if err := client.Rcpt(rcpt); err != nil {
			lastRcptErr = err
			e.logger.WarnContext(ctx, "RCPT failed",
				"recipient", rcpt,
				"error", err.Error(),
			)
		} else {
			successfulRecipients++
		}
	}

	// If no recipients accepted, fail
	if successfulRecipients == 0 {
		if lastRcptErr != nil {
			return classifyError(lastRcptErr)
		}
		return fmt.Errorf("%w: no recipients accepted", ErrInvalidRecipient)
	}

	// Send data
	w, err := client.Data()
	if err != nil {
		return classifyError(err)
	}

	_, err = w.Write(data)
	if err != nil {
		w.Close()
		return fmt.Errorf("data write failed: %w", err)
	}

	if err := w.Close(); err != nil {
		return classifyError(err)
	}

	return nil
}

// readAndSignMessage reads the message and applies DKIM signature.
func (e *Engine) readAndSignMessage(ctx context.Context, msg *queue.Message) ([]byte, error) {
	// Read original message
	data, err := os.ReadFile(msg.MessagePath)
	if err != nil {
		return nil, err
	}

	// Sign with DKIM if available
	if e.dkimPool != nil {
		senderDomain := extractDomain(msg.Sender)
		signer := e.dkimPool.GetSigner(senderDomain)
		if signer != nil {
			var signed bytes.Buffer
			if err := signer.Sign(&signed, bytes.NewReader(data)); err != nil {
				e.logger.WarnContext(ctx, "DKIM signing failed", "error", err.Error())
				// Continue without DKIM
			} else {
				data = signed.Bytes()
			}
		}
	}

	return data, nil
}

// deliverToHost delivers to a specific SMTP server.
func (e *Engine) deliverToHost(ctx context.Context, addr, hostname string, msg *queue.Message, data []byte) error {
	return e.deliverToHostWithTLS(ctx, addr, hostname, msg, data, true)
}

// deliverToHostWithTLS delivers to a specific SMTP server with optional TLS.
func (e *Engine) deliverToHostWithTLS(ctx context.Context, addr, hostname string, msg *queue.Message, data []byte, tryTLS bool) error {
	// Check MTA-STS policy for target domain
	var stsPolicy *STSPolicy
	if e.stsResolver != nil && tryTLS {
		var err error
		stsPolicy, err = e.stsResolver.GetPolicy(ctx, msg.Domain)
		if err != nil {
			e.logger.WarnContext(ctx, "MTA-STS policy fetch failed",
				"domain", msg.Domain,
				"error", err.Error(),
			)
			// Continue without MTA-STS - policy fetch failure shouldn't block delivery
		} else if stsPolicy != nil {
			e.logger.DebugContext(ctx, "MTA-STS policy found",
				"domain", msg.Domain,
				"mode", stsPolicy.Mode,
			)
		}
	}

	// Lookup DANE/TLSA records for this host
	var tlsaRecords []TLSARecord
	var tlsaDNSSECValid bool
	if e.daneResolver != nil && tryTLS {
		var err error
		tlsaRecords, tlsaDNSSECValid, err = e.daneResolver.LookupTLSA(ctx, hostname, 25)
		if err != nil {
			e.logger.WarnContext(ctx, "DANE TLSA lookup failed",
				"host", hostname,
				"error", err.Error(),
			)
		} else if len(tlsaRecords) > 0 {
			e.logger.DebugContext(ctx, "DANE TLSA records found",
				"host", hostname,
				"count", len(tlsaRecords),
				"dnssec_valid", tlsaDNSSECValid,
			)
		}
	}
	if len(tlsaRecords) > 0 && !tlsaDNSSECValid {
		e.logger.WarnContext(ctx, "Ignoring DANE TLSA records without DNSSEC validation",
			"host", hostname,
			"count", len(tlsaRecords),
		)
	}
	useDANE := shouldUseDANE(tlsaRecords, tlsaDNSSECValid)

	// Connect with timeout
	dialer := &net.Dialer{
		Timeout: e.config.ConnectTimeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(addr, "25"))
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer conn.Close()

	// Set overall deadline from context or config timeout
	deadline := time.Now().Add(e.config.CommandTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	conn.SetDeadline(deadline)

	// Create SMTP client
	client, err := smtp.NewClient(conn, hostname)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer func() {
		client.Quit()
		client.Close()
	}()

	// Say hello
	if err := client.Hello(e.config.Hostname); err != nil {
		return fmt.Errorf("HELO failed: %w", err)
	}

	// Try STARTTLS if enabled and this is our first attempt
	if tryTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{
				ServerName: hostname,
				MinVersion: tls.VersionTLS12,
			}

			// If we have DNSSEC-validated DANE/TLSA records, use custom certificate verification.
			if useDANE {
				tlsConfig.InsecureSkipVerify = true // We'll verify manually with DANE
				tlsConfig.VerifyConnection = e.daneVerifyConnection(ctx, hostname, tlsaRecords)
			} else {
				// No DANE, use standard verification
				tlsConfig.InsecureSkipVerify = !e.config.VerifyTLS
			}

			if err := client.StartTLS(tlsConfig); err != nil {
				if reason := tlsRequirementReason(stsPolicy, useDANE, e.config.RequireTLS); reason != "" {
					return fmt.Errorf("%s but STARTTLS failed: %w", reason, err)
				}
				// SECURITY WARNING: TLS downgrade attack possible here
				e.logger.WarnContext(ctx, "SECURITY: STARTTLS failed, falling back to plaintext - potential downgrade attack",
					"host", hostname,
					"error", err.Error(),
					"recommendation", "set RequireTLS=true for secure delivery",
				)
				// Close current connection and retry without TLS
				client.Quit()
				client.Close()
				conn.Close()
				return e.deliverToHostWithTLS(ctx, addr, hostname, msg, data, false)
			}

			// MTA-STS: Validate that the MX hostname is in the policy's allowed list
			if stsPolicy != nil && stsPolicy.ShouldEnforceTLS() {
				if !stsPolicy.ValidateMX(hostname) {
					return fmt.Errorf("MTA-STS policy violation: MX host %s not in allowed list", hostname)
				}
			}
		} else {
			// No STARTTLS support
			if reason := tlsRequirementReason(stsPolicy, useDANE, e.config.RequireTLS); reason != "" {
				return fmt.Errorf("%s but server doesn't support STARTTLS", reason)
			}
		}
	}

	// Set sender
	if err := client.Mail(msg.Sender); err != nil {
		return classifyError(err)
	}

	// Set recipients - track successes
	successfulRecipients := 0
	var lastRcptErr error
	for _, rcpt := range msg.Recipients {
		if err := client.Rcpt(rcpt); err != nil {
			lastRcptErr = err
			e.logger.WarnContext(ctx, "RCPT failed",
				"recipient", rcpt,
				"error", err.Error(),
			)
		} else {
			successfulRecipients++
		}
	}

	// If no recipients accepted, fail
	if successfulRecipients == 0 {
		if lastRcptErr != nil {
			return classifyError(lastRcptErr)
		}
		return fmt.Errorf("%w: no recipients accepted", ErrInvalidRecipient)
	}

	// Send data
	w, err := client.Data()
	if err != nil {
		return classifyError(err)
	}

	_, err = w.Write(data)
	if err != nil {
		w.Close()
		return fmt.Errorf("data write failed: %w", err)
	}

	if err := w.Close(); err != nil {
		return classifyError(err)
	}

	return nil
}

func (e *Engine) daneVerifyConnection(ctx context.Context, hostname string, tlsaRecords []TLSARecord) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return fmt.Errorf("no certificates presented")
		}

		result := ValidateCertificate(cs.PeerCertificates[0], cs.PeerCertificates[1:], tlsaRecords)
		if !result.Valid {
			return fmt.Errorf("DANE validation failed: %w", result.Error)
		}

		e.logger.DebugContext(ctx, "DANE validation passed",
			"host", hostname,
			"usage", result.UsedRecord.Usage.String(),
		)
		return nil
	}
}

func shouldUseDANE(tlsaRecords []TLSARecord, dnssecValid bool) bool {
	return len(tlsaRecords) > 0 && dnssecValid
}

func tlsRequirementReason(stsPolicy *STSPolicy, useDANE, requireTLS bool) string {
	if useDANE {
		return "DANE requires TLS"
	}
	if stsPolicy != nil && stsPolicy.ShouldEnforceTLS() {
		return "MTA-STS enforces TLS"
	}
	if requireTLS {
		return "STARTTLS required"
	}
	return ""
}

// recoveryWorker periodically recovers stale messages and cleans up orphaned files.
func (e *Engine) recoveryWorker() {
	defer e.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			recovered, err := e.queue.RecoverStale(e.ctx, 10*time.Minute)
			if err != nil {
				e.logger.Error("Stale recovery failed", "error", err.Error())
			} else if recovered > 0 {
				e.logger.Info("Recovered stale messages", "count", recovered)
			}

			// Clean up orphaned .eml files older than retry_max_age (7 days).
			// Files this old have either been delivered (file should have been
			// deleted) or aged out of the retry window. Safe to remove.
			if cleaned := e.cleanupOrphanedFiles(); cleaned > 0 {
				e.logger.Info("Cleaned up orphaned message files", "count", cleaned)
			}
		}
	}
}

// warmupCircuitBreakers checks delivery_log for domains with recent consecutive
// failures and pre-opens their circuit breakers. This prevents the server from
// hammering known-broken MX servers immediately after a restart.
//
// The live breaker opens after 5 consecutive failures and resets its failure
// count on any success. To reconstruct this, we look at the most recent
// delivery attempts per domain (deduplicated by message_id+attempt_number to
// avoid per-recipient row inflation) and count consecutive trailing failures.
func (e *Engine) warmupCircuitBreakers() {
	if e.db == nil {
		return
	}

	oneHourAgo := time.Now().Add(-1 * time.Hour)

	// Get all domains with any recent failure activity
	domainRows, err := e.db.QueryContext(e.ctx, `
		SELECT DISTINCT domain FROM delivery_log
		WHERE status IN ('rejected', 'bounced', 'deferred')
		  AND domain IS NOT NULL
		  AND created_at > ?
	`, oneHourAgo)
	if err != nil {
		e.logger.Warn("Failed to query delivery_log for circuit breaker warmup",
			"error", err.Error())
		return
	}

	var domains []string
	for domainRows.Next() {
		var d string
		if domainRows.Scan(&d) == nil {
			domains = append(domains, d)
		}
	}
	domainRows.Close()

	warmed := 0
	for _, domain := range domains {
		// Get the most recent delivery attempts for this domain, deduplicated
		// by (message_id, attempt_number) to avoid per-recipient inflation.
		// Order by most recent first so we can count consecutive trailing failures.
		rows, err := e.db.QueryContext(e.ctx, `
			SELECT status FROM (
				SELECT MAX(status) as status, MAX(created_at) as latest
				FROM delivery_log
				WHERE domain = ? AND created_at > ?
				GROUP BY message_id, attempt_number
				ORDER BY latest DESC
				LIMIT 10
			) sub ORDER BY latest DESC
		`, domain, oneHourAgo)
		if err != nil {
			e.logger.Debug("Failed to check recent attempts for domain",
				"domain", domain, "error", err.Error())
			continue
		}

		// Count consecutive trailing failures (stop at first success)
		consecutiveFailures := 0
		for rows.Next() {
			var status string
			if rows.Scan(&status) != nil {
				break
			}
			if status == "delivered" {
				break // Success resets the count, just like the live breaker
			}
			consecutiveFailures++
		}
		rows.Close()

		if consecutiveFailures >= 5 {
			breaker := e.breakers.Get(domain)
			if breaker != nil {
				breaker.ForceOpen()
				warmed++
				e.logger.Info("Pre-opened circuit breaker for failing domain",
					"domain", domain, "consecutive_failures", consecutiveFailures)
			}
		}
	}

	if warmed > 0 {
		e.logger.Info("Circuit breaker warmup complete", "pre_opened", warmed)
	}
}

// orphanFileMinAge is the minimum age before a file is considered for orphan
// cleanup. Files younger than this are likely still being processed.
const orphanFileMinAge = 1 * time.Hour

// cleanupOrphanedFiles removes .eml files from the queue directory that are
// not referenced by any active queue entry in Redis. Only files older than
// orphanFileMinAge are considered, to avoid racing with in-flight enqueues.
func (e *Engine) cleanupOrphanedFiles() int {
	if e.config.QueuePath == "" {
		return 0
	}

	// Get all file paths still referenced by pending/processing messages
	if e.queue == nil {
		return 0
	}
	activePaths, err := e.queue.ActiveMessagePaths(e.ctx)
	if err != nil {
		e.logger.Warn("Failed to query active message paths for orphan cleanup",
			"error", err.Error())
		return 0
	}

	entries, err := os.ReadDir(e.config.QueuePath)
	if err != nil {
		e.logger.Warn("Failed to read queue directory for orphan cleanup",
			"error", err.Error(), "path", e.config.QueuePath)
		return 0
	}

	cutoff := time.Now().Add(-orphanFileMinAge)
	cleaned := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".eml") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Skip recent files to avoid racing with in-flight enqueues
		if !info.ModTime().Before(cutoff) {
			continue
		}

		path := filepath.Join(e.config.QueuePath, entry.Name())

		// Only delete if NOT referenced by any active queue entry
		if activePaths[path] {
			continue
		}

		if err := os.Remove(path); err != nil {
			e.logger.Warn("Failed to remove orphaned file",
				"path", path, "error", err.Error())
		} else {
			cleaned++
		}
	}

	return cleaned
}

// Stats returns delivery statistics.
func (e *Engine) Stats() EngineStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	queueStats, _ := e.queue.Stats(e.ctx)

	return EngineStats{
		TotalSent:    e.totalSent,
		TotalFailed:  e.totalFailed,
		TotalRetried: e.totalRetried,
		TotalBounced: e.totalBounced,
		QueueStats:   queueStats,
		MXCacheStats: e.mxResolver.CacheStats(),
	}
}

// EngineStats contains delivery engine statistics.
type EngineStats struct {
	TotalSent    int64
	TotalFailed  int64
	TotalRetried int64
	TotalBounced int64
	QueueStats   *queue.QueueStats
	MXCacheStats MXCacheStats
}

// Helper functions

// extractDomain extracts the domain from an email address.
func extractDomain(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}

// isPermanentError determines if an error is permanent (5xx) vs temporary (4xx).
func isPermanentError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Check for permanent SMTP codes (5xx)
	if strings.Contains(errStr, "550") ||
		strings.Contains(errStr, "551") ||
		strings.Contains(errStr, "552") ||
		strings.Contains(errStr, "553") ||
		strings.Contains(errStr, "554") {
		return true
	}

	// Specific permanent errors
	if errors.Is(err, ErrPermanentFailure) ||
		errors.Is(err, ErrInvalidRecipient) ||
		errors.Is(err, ErrMessageTooLarge) {
		return true
	}

	return false
}

// classifyError classifies an SMTP error as permanent or temporary.
func classifyError(err error) error {
	if err == nil {
		return nil
	}

	errStr := err.Error()

	// 5xx errors are permanent
	if strings.HasPrefix(errStr, "5") ||
		strings.Contains(errStr, " 5") {
		return fmt.Errorf("%w: %v", ErrPermanentFailure, err)
	}

	// 4xx errors are temporary
	return fmt.Errorf("%w: %v", ErrTemporaryFailure, err)
}

// logDelivery logs a delivery event to the database
func (e *Engine) logDelivery(ctx context.Context, messageID, sender, recipient, status string, smtpCode int, errorMsg string) {
	if e.db == nil {
		return // Graceful degradation if no database configured
	}

	var errMsgPtr *string
	if errorMsg != "" {
		errMsgPtr = &errorMsg
	}

	var smtpCodePtr *int
	if smtpCode > 0 {
		smtpCodePtr = &smtpCode
	}

	_, err := e.db.ExecContext(ctx,
		`INSERT INTO delivery_log (message_id, sender, recipient, status, smtp_code, error_message)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		messageID, sender, recipient, status, smtpCodePtr, errMsgPtr,
	)
	if err != nil {
		e.logger.WarnContext(ctx, "Failed to log delivery event",
			"error", err.Error(),
			"message_id", messageID,
		)
	}
}

// extractSMTPCode extracts SMTP status code from error string
func extractSMTPCode(err error) int {
	if err == nil {
		return 0
	}
	errStr := err.Error()

	// Look for 3-digit SMTP codes
	codes := []int{550, 551, 552, 553, 554, 421, 450, 451, 452}
	for _, code := range codes {
		if strings.Contains(errStr, fmt.Sprintf("%d", code)) {
			return code
		}
	}
	return 0
}

// extractMessageID extracts the Message-ID header from an email file.
func (e *Engine) extractMessageID(messagePath string) string {
	if messagePath == "" {
		return ""
	}

	file, err := os.Open(messagePath)
	if err != nil {
		return ""
	}
	defer file.Close()

	reader := textproto.NewReader(bufio.NewReader(file))
	header, err := reader.ReadMIMEHeader()
	if err != nil {
		return ""
	}

	messageID := header.Get("Message-ID")
	// Clean up angle brackets if present
	messageID = strings.TrimPrefix(messageID, "<")
	messageID = strings.TrimSuffix(messageID, ">")
	return messageID
}

// fireEvent dispatches a delivery event to the registered handler asynchronously.
// The event is passed by value so the handler is not tied to the caller's lifetime.
func (e *Engine) fireEvent(ctx context.Context, event DeliveryEvent) {
	h, _ := e.eventHandler.Load().(DeliveryEventHandler)
	if h != nil {
		go func(ev DeliveryEvent) {
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("Panic in delivery event handler",
						"error", fmt.Sprintf("%v", r),
						"message_id", ev.SMTPMessageID)
				}
			}()
			h(context.Background(), ev)
		}(event)
	}
}

// maxBounceReasonLen limits stored bounce reasons to prevent storage abuse
// from malicious SMTP servers sending excessively long error responses.
const maxBounceReasonLen = 1024

// truncateString safely truncates a string to maxLen bytes.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// updateSentEmailStatus syncs the delivery result back to the sent_emails table
// and records a delivery attempt. This ensures the transactional API's send logs
// reflect actual delivery status instead of staying permanently "queued".
func (e *Engine) updateSentEmailStatus(ctx context.Context, smtpMessageID, status, smtpResponse, bounceReason string, attempt int) {
	if e.db == nil || smtpMessageID == "" {
		return
	}

	// Truncate externally-sourced strings to prevent storage abuse
	bounceReason = truncateString(bounceReason, maxBounceReasonLen)
	smtpResponse = truncateString(smtpResponse, maxBounceReasonLen)

	// sent_emails.message_id stores the full angle-bracket form: <id@domain>
	// extractMessageID strips them, so we re-add for matching
	fullMessageID := "<" + smtpMessageID + ">"
	now := time.Now()

	var err error
	switch status {
	case "delivered":
		_, err = e.db.ExecContext(ctx,
			`UPDATE sent_emails SET status = 'delivered', delivered_at = ?, smtp_response = ? WHERE message_id = ? AND status IN ('queued', 'sending')`,
			now, smtpResponse, fullMessageID,
		)
	case "bounced":
		_, err = e.db.ExecContext(ctx,
			`UPDATE sent_emails SET status = 'bounced', bounced_at = ?, bounce_reason = ? WHERE message_id = ? AND status IN ('queued', 'sending')`,
			now, bounceReason, fullMessageID,
		)
	case "failed":
		_, err = e.db.ExecContext(ctx,
			`UPDATE sent_emails SET status = 'failed', bounce_reason = ? WHERE message_id = ? AND status IN ('queued', 'sending')`,
			bounceReason, fullMessageID,
		)
	}

	if err != nil {
		e.logger.WarnContext(ctx, "Failed to update sent_emails status",
			"error", err.Error(),
			"smtp_message_id", smtpMessageID,
			"status", status,
		)
	}

	// Record delivery attempt for the timeline view
	e.recordDeliveryAttempt(ctx, fullMessageID, status, smtpResponse, bounceReason, attempt)
}

// recordDeliveryAttempt inserts a row into delivery_attempts linked to the sent_email.
func (e *Engine) recordDeliveryAttempt(ctx context.Context, fullMessageID, status, smtpResponse, errorMessage string, attempt int) {
	if e.db == nil || fullMessageID == "" || fullMessageID == "<>" {
		return
	}

	// Map delivery status to the delivery_attempts CHECK constraint:
	// ('pending', 'sent', 'deferred', 'failed', 'bounced')
	var attemptStatus string
	switch status {
	case "delivered":
		attemptStatus = "sent"
	case "bounced":
		attemptStatus = "bounced"
	case "failed":
		attemptStatus = "failed"
	case "deferred":
		attemptStatus = "deferred"
	default:
		// Unknown status — skip to avoid CHECK constraint violation
		e.logger.WarnContext(ctx, "Unknown delivery status for attempt recording",
			"status", status, "message_id", fullMessageID)
		return
	}

	// Truncate externally-sourced strings
	smtpResponse = truncateString(smtpResponse, maxBounceReasonLen)
	errorMessage = truncateString(errorMessage, maxBounceReasonLen)

	var smtpResp, errMsg *string
	if smtpResponse != "" {
		smtpResp = &smtpResponse
	}
	if errorMessage != "" {
		errMsg = &errorMessage
	}

	_, err := e.db.ExecContext(ctx,
		`INSERT INTO delivery_attempts (sent_email_id, attempt_number, attempted_at, status, smtp_response, error_message)
		 SELECT id, ?, ?, ?, ?, ? FROM sent_emails WHERE message_id = ?`,
		attempt, time.Now(), attemptStatus, smtpResp, errMsg, fullMessageID,
	)
	if err != nil {
		e.logger.WarnContext(ctx, "Failed to record delivery attempt",
			"error", err.Error(),
			"message_id", fullMessageID,
			"attempt", attempt,
		)
	}
}

// logDeliveryWithTrace logs a delivery event with tracing and observability data.
func (e *Engine) logDeliveryWithTrace(ctx context.Context, messageID, sender, recipient, status string, smtpCode int, errorMsg, domain string, attempt int, startTime time.Time, cbState string) {
	if e.db == nil {
		return // Graceful degradation if no database configured
	}

	var errMsgPtr *string
	if errorMsg != "" {
		errMsgPtr = &errorMsg
	}

	var smtpCodePtr *int
	if smtpCode > 0 {
		smtpCodePtr = &smtpCode
	}

	// Get trace ID from context
	traceID := tracing.GetTraceID(ctx)
	var traceIDPtr *string
	if traceID != "" {
		traceIDPtr = &traceID
	}

	// Calculate duration
	durationMs := int(time.Since(startTime).Milliseconds())

	var domainPtr *string
	if domain != "" {
		domainPtr = &domain
	}

	var cbStatePtr *string
	if cbState != "" {
		cbStatePtr = &cbState
	}

	_, err := e.db.ExecContext(ctx,
		`INSERT INTO delivery_log (message_id, sender, recipient, status, smtp_code, error_message, trace_id, domain, attempt_number, delivery_duration_ms, circuit_breaker_state)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		messageID, sender, recipient, status, smtpCodePtr, errMsgPtr, traceIDPtr, domainPtr, attempt, durationMs, cbStatePtr,
	)
	if err != nil {
		e.logger.WarnContext(ctx, "Failed to log delivery event",
			"error", err.Error(),
			"message_id", messageID,
		)
	}
}
