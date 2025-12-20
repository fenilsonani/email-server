// Package delivery implements outbound email delivery with circuit breakers and retry logic.
package delivery

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/resilience"
	"github.com/fenilsonani/email-server/internal/security"
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

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	mu            sync.RWMutex
	totalSent     int64
	totalFailed   int64
	totalRetried  int64
}

// NewEngine creates a new delivery engine.
func NewEngine(cfg Config, q *queue.RedisQueue, dkim *security.DKIMSignerPool, logger *logging.Logger) *Engine {
	ctx, cancel := context.WithCancel(context.Background())

	return &Engine{
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
		logger: logger.Delivery(),
		ctx:    ctx,
		cancel: cancel,
	}
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

	logger.InfoContext(ctx, "Attempting delivery",
		"attempt", msg.Attempts,
		"recipients", len(msg.Recipients),
	)

	// Check circuit breaker for this domain
	breaker := e.breakers.Get(msg.Domain)
	if breaker.State() == resilience.StateOpen {
		logger.WarnContext(ctx, "Circuit breaker open, deferring")
		e.queue.Retry(ctx, msg.ID, ErrCircuitOpen)
		e.mu.Lock()
		e.totalRetried++
		e.mu.Unlock()
		return
	}

	// Attempt delivery through circuit breaker
	err := breaker.Execute(ctx, func(ctx context.Context) error {
		return e.attemptDelivery(ctx, msg)
	})

	if err != nil {
		// Determine if permanent or temporary
		if isPermanentError(err) {
			logger.ErrorContext(ctx, "Permanent delivery failure", err)
			e.queue.Fail(ctx, msg.ID, err.Error())
			e.mu.Lock()
			e.totalFailed++
			e.mu.Unlock()

			// TODO: Generate bounce message

			// Clean up the original message file
			if err := e.cleanupMessageFile(msg.MessagePath); err != nil {
				logger.WarnContext(ctx, "Failed to cleanup message file after failure",
					"path", msg.MessagePath,
					"error", err.Error())
			}
		} else {
			logger.WarnContext(ctx, "Temporary delivery failure, will retry", "error", err.Error())
			e.queue.Retry(ctx, msg.ID, err)
			e.mu.Lock()
			e.totalRetried++
			e.mu.Unlock()
		}
		return
	}

	// Success!
	logger.InfoContext(ctx, "Message delivered successfully")
	e.queue.Complete(ctx, msg.ID)
	e.mu.Lock()
	e.totalSent++
	e.mu.Unlock()

	// Clean up the message file from disk
	if err := e.cleanupMessageFile(msg.MessagePath); err != nil {
		logger.WarnContext(ctx, "Failed to cleanup message file",
			"path", msg.MessagePath,
			"error", err.Error())
	}
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

// attemptDelivery tries to deliver to MX servers.
func (e *Engine) attemptDelivery(ctx context.Context, msg *queue.Message) error {
	// Read and sign the message
	messageData, err := e.readAndSignMessage(ctx, msg)
	if err != nil {
		return fmt.Errorf("failed to prepare message: %w", err)
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
	// Connect with timeout
	dialer := &net.Dialer{
		Timeout: e.config.ConnectTimeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(addr, "25"))
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer conn.Close()

	// Set overall deadline
	conn.SetDeadline(time.Now().Add(e.config.CommandTimeout))

	// Create SMTP client
	client, err := smtp.NewClient(conn, hostname)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer client.Close()

	// Say hello
	if err := client.Hello(e.config.Hostname); err != nil {
		return fmt.Errorf("HELO failed: %w", err)
	}

	// Try STARTTLS
	if ok, _ := client.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{
			ServerName:         hostname,
			InsecureSkipVerify: !e.config.VerifyTLS,
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			if e.config.RequireTLS {
				return fmt.Errorf("STARTTLS required but failed: %w", err)
			}
			// Continue without TLS
			e.logger.DebugContext(ctx, "STARTTLS failed, continuing without TLS",
				"host", hostname,
				"error", err.Error(),
			)
		}
	} else if e.config.RequireTLS {
		return fmt.Errorf("STARTTLS required but not supported by server")
	}

	// Set sender
	if err := client.Mail(msg.Sender); err != nil {
		return classifyError(err)
	}

	// Set recipients
	for _, rcpt := range msg.Recipients {
		if err := client.Rcpt(rcpt); err != nil {
			// Log but continue - partial delivery may still work
			e.logger.WarnContext(ctx, "RCPT failed",
				"recipient", rcpt,
				"error", err.Error(),
			)
		}
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

	// Quit
	client.Quit()

	return nil
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
		QueueStats:   queueStats,
		MXCacheStats: e.mxResolver.CacheStats(),
	}
}

// EngineStats contains delivery engine statistics.
type EngineStats struct {
	TotalSent    int64
	TotalFailed  int64
	TotalRetried int64
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
