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
	"strings"
	"sync"
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
	ErrPermanentFailure  = errors.New("permanent delivery failure")
	ErrTemporaryFailure  = errors.New("temporary delivery failure")
	ErrCircuitOpen       = errors.New("circuit breaker open for domain")
	ErrAllMXFailed       = errors.New("all MX servers failed")
	ErrMessageTooLarge   = errors.New("message too large")
	ErrInvalidRecipient  = errors.New("invalid recipient")
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
	config         Config
	queue          *queue.RedisQueue
	mxResolver     *MXResolver
	dkimPool       *security.DKIMSignerPool
	breakers       *resilience.BreakerRegistry
	logger         *logging.Logger
	bounceGen      *BounceGenerator
	db             *sql.DB

	// Security resolvers
	stsResolver  *STSResolver  // MTA-STS policy resolver
	daneResolver *DANEResolver // DANE/TLSA resolver

	// Observability
	tracer      *tracing.Tracer
	domainStats *metrics.DomainStats

	// Deduplication
	dedupTracker *queue.DeliveryTracker

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	mu            sync.RWMutex
	totalSent     int64
	totalFailed   int64
	totalRetried  int64
	totalBounced  int64
}

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

			// If we have DANE/TLSA records, use custom certificate verification
			if len(tlsaRecords) > 0 {
				tlsConfig.InsecureSkipVerify = true // We'll verify manually with DANE
				tlsConfig.VerifyConnection = e.daneVerifyConnection(ctx, hostname, tlsaRecords)
			} else {
				// No DANE, use standard verification
				tlsConfig.InsecureSkipVerify = !e.config.VerifyTLS
			}

			if err := client.StartTLS(tlsConfig); err != nil {
				// Check MTA-STS policy - if enforcing, TLS failure is fatal
				if stsPolicy != nil && stsPolicy.ShouldEnforceTLS() {
					return fmt.Errorf("MTA-STS enforced TLS but STARTTLS failed: %w", err)
				}
				if e.config.RequireTLS {
					return fmt.Errorf("STARTTLS required but failed: %w", err)
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
			if stsPolicy != nil && stsPolicy.ShouldEnforceTLS() {
				return fmt.Errorf("MTA-STS enforces TLS but server doesn't support STARTTLS")
			}
			if e.config.RequireTLS {
				return fmt.Errorf("STARTTLS required but not supported by server")
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

// recoveryWorker periodically recovers stale messages.
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
		}
	}
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
