package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/smtp/delivery"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server handles the transactional email API
type Server struct {
	config       *config.Config
	db           *sql.DB
	queue        *queue.RedisQueue
	delivery     *delivery.Engine
	logger       *logging.Logger
	httpServer   *http.Server
	shutdownOnce sync.Once
	queuePath    string

	// New services for production features
	idempotency *IdempotencyStore   // Redis-backed idempotency store
	suppression *SuppressionService // Email suppression list
	scheduler   *Scheduler          // Scheduled email processor
}

// NewServer creates a new API server
func NewServer(
	cfg *config.Config,
	db *sql.DB,
	q *queue.RedisQueue,
	deliveryEngine *delivery.Engine,
	logger *logging.Logger,
) (*Server, error) {
	// Create queue directory for API messages
	queuePath := filepath.Join(cfg.Storage.DataDir, "queue")
	if err := os.MkdirAll(queuePath, 0750); err != nil {
		return nil, fmt.Errorf("failed to create queue directory: %w", err)
	}

	s := &Server{
		config:    cfg,
		db:        db,
		queue:     q,
		delivery:  deliveryEngine,
		logger:    logger,
		queuePath: queuePath,
	}

	// Initialize suppression service
	s.suppression = NewSuppressionService(db)

	// Initialize idempotency store if Redis queue is available
	if q != nil && q.Client() != nil {
		s.idempotency = NewIdempotencyStore(q.Client(), cfg.Queue.Prefix)
		logger.Info("Idempotency store initialized with Redis backend")
	}

	// Initialize scheduler for scheduled emails
	s.scheduler = NewScheduler(db, s, 30*time.Second)

	return s, nil
}

// Start starts the API server
func (s *Server) Start(listen string) error {
	mux := http.NewServeMux()

	// Health endpoints (no auth)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/healthz", s.handleHealth)

	// Prometheus metrics endpoint (no auth)
	mux.Handle("/metrics", promhttp.Handler())

	// API v1 routes (with auth)
	mux.Handle("/api/v1/send", s.authMiddleware(http.HandlerFunc(s.handleSendEmail)))
	mux.Handle("/api/v1/send/template", s.authMiddleware(http.HandlerFunc(s.handleSendTemplate)))
	mux.Handle("/api/v1/send/batch", s.authMiddleware(http.HandlerFunc(s.handleSendBatch)))

	// Templates
	mux.Handle("/api/v1/templates", s.authMiddleware(http.HandlerFunc(s.handleTemplates)))
	mux.Handle("/api/v1/templates/", s.authMiddleware(http.HandlerFunc(s.handleTemplateBySlug)))

	// Emails (status, history)
	mux.Handle("/api/v1/emails", s.authMiddleware(http.HandlerFunc(s.handleListEmails)))
	mux.Handle("/api/v1/emails/", s.authMiddleware(http.HandlerFunc(s.handleGetEmail)))

	// Stats
	mux.Handle("/api/v1/stats", s.authMiddleware(http.HandlerFunc(s.handleStats)))

	// Suppression list endpoints
	mux.Handle("/api/v1/suppressions", s.authMiddleware(http.HandlerFunc(s.handleSuppressions)))
	mux.Handle("/api/v1/suppressions/", s.authMiddleware(http.HandlerFunc(s.handleSuppressionByEmail)))

	// Webhooks
	mux.Handle("/api/v1/webhooks", s.authMiddleware(http.HandlerFunc(s.handleWebhooks)))

	// Tracking endpoints (no auth - public tracking pixels/links)
	mux.HandleFunc("/t/o/", s.handleTrackOpen)
	mux.HandleFunc("/t/c/", s.handleTrackClick)

	// Build middleware chain
	handler := s.corsMiddleware(mux)
	handler = s.requestIDMiddleware(handler) // Add request ID middleware
	handler = s.metricsMiddleware(handler)   // Add metrics middleware
	handler = s.requestLoggingMiddleware(handler)

	s.httpServer = &http.Server{
		Addr:              listen,
		Handler:           handler,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.logger.Info("Starting API server", "listen", listen)

	// Start the scheduler for scheduled emails
	if s.scheduler != nil {
		s.scheduler.Start()
		s.logger.Info("Email scheduler started")
	}

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("API server error: %w", err)
		}
		return nil
	case sig := <-sigChan:
		s.logger.Info("API server received shutdown signal", "signal", sig.String())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return s.Shutdown(shutdownCtx)
	}
}

// Shutdown gracefully stops the API server
func (s *Server) Shutdown(ctx context.Context) error {
	var err error
	s.shutdownOnce.Do(func() {
		s.logger.Info("Shutting down API server")

		// Stop the scheduler first
		if s.scheduler != nil {
			s.scheduler.Stop()
			s.logger.Info("Email scheduler stopped")
		}

		if s.httpServer != nil {
			if shutdownErr := s.httpServer.Shutdown(ctx); shutdownErr != nil {
				s.logger.Error("Error shutting down API server", "error", shutdownErr.Error())
				err = shutdownErr
			}
		}
		s.logger.Info("API server shutdown complete")
	})
	return err
}

// handleHealth returns server health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]string{
		"status":  "healthy",
		"service": "transactional-api",
	}, http.StatusOK)
}

// getDomainID retrieves the domain ID for the API key
func (s *Server) getDomainID(ctx context.Context) (int64, error) {
	apiKey := getAPIKeyFromContext(ctx)
	if apiKey == nil {
		return 0, errors.New("no API key in context")
	}
	return apiKey.DomainID, nil
}

// canSendFromDomain checks if the API key can send from a given email domain
func (s *Server) canSendFromDomain(ctx context.Context, email string) (bool, error) {
	apiKey := getAPIKeyFromContext(ctx)
	if apiKey == nil {
		return false, errors.New("no API key in context")
	}

	// Extract domain from email
	parts := splitEmail(email)
	if len(parts) != 2 {
		return false, errors.New("invalid email format")
	}
	emailDomain := parts[1]

	// Check if this domain belongs to the API key's domain
	var domainName string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM domains WHERE id = ?`, apiKey.DomainID).Scan(&domainName)
	if err != nil {
		return false, err
	}

	return emailDomain == domainName, nil
}

// splitEmail splits an email into local part and domain
func splitEmail(email string) []string {
	for i := len(email) - 1; i >= 0; i-- {
		if email[i] == '@' {
			return []string{email[:i], email[i+1:]}
		}
	}
	return []string{email}
}
