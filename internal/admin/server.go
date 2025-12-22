package admin

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/sieve"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
)

//go:embed templates/*.html
var templatesFS embed.FS

// Server handles the admin web interface
type Server struct {
	config        *config.Config
	db            *sql.DB
	authenticator *auth.Authenticator
	store         *maildir.Store
	sieveStore    *sieve.Store
	queue         *queue.RedisQueue
	logger        *logging.Logger
	templates     map[string]*template.Template
	httpServer    *http.Server
	shutdownOnce  sync.Once
	rateLimiter   *RateLimiter
	startTime     time.Time
}

// NewServer creates a new admin server
func NewServer(cfg *config.Config, db *sql.DB, authenticator *auth.Authenticator, store *maildir.Store, sieveStore *sieve.Store, q *queue.RedisQueue, logger *logging.Logger) (*Server, error) {
	// Read base template content
	baseContent, err := templatesFS.ReadFile("templates/base.html")
	if err != nil {
		return nil, fmt.Errorf("failed to read base template: %w", err)
	}
	baseStr := string(baseContent)

	// Create template map
	templates := make(map[string]*template.Template)

	// Pages that use the base layout
	pages := []string{
		"dashboard.html",
		"users.html",
		"user_form.html",
		"user_edit.html",
		"domains.html",
		"domain_form.html",
		"sieve.html",
		"auth_logs.html",
		"delivery_logs.html",
		"queue.html",
		"dns_check.html",
		"test_email.html",
	}

	for _, page := range pages {
		// Read page template content
		pageContent, err := templatesFS.ReadFile("templates/" + page)
		if err != nil {
			return nil, fmt.Errorf("failed to read template %s: %w", page, err)
		}

		// Replace the placeholder in base template with page content
		combined := strings.Replace(baseStr, "<!-- CONTENT_PLACEHOLDER -->", string(pageContent), 1)

		// Parse the combined template
		tmpl, err := template.New(page).Parse(combined)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template %s: %w", page, err)
		}

		templates[page] = tmpl
	}

	// Login page is standalone (no base layout)
	loginContent, err := templatesFS.ReadFile("templates/login.html")
	if err != nil {
		return nil, fmt.Errorf("failed to read login template: %w", err)
	}
	loginTmpl, err := template.New("login.html").Parse(string(loginContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse login template: %w", err)
	}
	templates["login.html"] = loginTmpl

	s := &Server{
		config:        cfg,
		db:            db,
		authenticator: authenticator,
		store:         store,
		sieveStore:    sieveStore,
		queue:         q,
		logger:        logger,
		templates:     templates,
		rateLimiter:   DefaultRateLimiter(),
		startTime:     time.Now(),
	}

	return s, nil
}

// Start starts the admin server
func (s *Server) Start(listen string) error {
	mux := http.NewServeMux()

	// Routes
	mux.HandleFunc("/admin/", s.withAuth(s.handleDashboard))
	mux.HandleFunc("/admin/login", s.handleLogin)
	mux.HandleFunc("/admin/logout", s.handleLogout)
	mux.HandleFunc("/admin/users", s.withAuth(s.handleUsers))
	mux.HandleFunc("/admin/users/add", s.withAuth(s.handleUserAdd))
	mux.HandleFunc("/admin/users/edit/", s.withAuth(s.handleUserEdit))
	mux.HandleFunc("/admin/users/delete/", s.withAuth(s.handleUserDelete))
	mux.HandleFunc("/admin/domains", s.withAuth(s.handleDomains))
	mux.HandleFunc("/admin/domains/add", s.withAuth(s.handleDomainAdd))
	mux.HandleFunc("/admin/domains/delete/", s.withAuth(s.handleDomainDelete))
	mux.HandleFunc("/admin/sieve/", s.withAuth(s.handleSieve))
	mux.HandleFunc("/admin/logs/auth", s.withAuth(s.handleAuthLogs))
	mux.HandleFunc("/admin/logs/delivery", s.withAuth(s.handleDeliveryLogs))
	mux.HandleFunc("/admin/queue", s.withAuth(s.handleQueue))
	mux.HandleFunc("/admin/queue/retry/", s.withAuth(s.handleQueueRetry))
	mux.HandleFunc("/admin/queue/delete/", s.withAuth(s.handleQueueDelete))
	mux.HandleFunc("/admin/api/stats", s.withAuth(s.handleAPIStats))
	mux.HandleFunc("/admin/tools/dns", s.withAuth(s.handleDNSCheck))
	mux.HandleFunc("/admin/tools/test-email", s.withAuth(s.handleTestEmail))

	// Health check endpoint (no auth required)
	// Use Go 1.22 method-based routing for clarity
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)

	// Build middleware chain (order matters: innermost first, then wrapping outward)
	// The execution order will be: logging -> security headers -> panic recovery -> CSRF -> routes
	handler := s.withCSRF(mux)
	handler = s.withPanicRecovery(handler)
	handler = s.withSecurityHeaders(handler)
	handler = s.withRequestLogging(handler)

	s.httpServer = &http.Server{
		Addr:         listen,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
		// Enhance with better defaults for reliability
		MaxHeaderBytes:    1 << 20, // 1 MB
		ReadHeaderTimeout: 10 * time.Second,
	}

	s.logger.Info("Starting admin server", "listen", listen)

	// Start cleanup goroutine
	CleanupExpiredSessions()

	// Start server in a goroutine for graceful shutdown
	serverErr := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case sig := <-sigChan:
		s.logger.Info("Received shutdown signal", "signal", sig.String())

		// Create shutdown context with timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		return s.Shutdown(shutdownCtx)
	}
}

// Shutdown gracefully stops the admin server
func (s *Server) Shutdown(ctx context.Context) error {
	var err error
	s.shutdownOnce.Do(func() {
		s.logger.Info("Shutting down admin server")

		// Shutdown HTTP server
		if s.httpServer != nil {
			if shutdownErr := s.httpServer.Shutdown(ctx); shutdownErr != nil {
				s.logger.Error("Error shutting down HTTP server", "error", shutdownErr.Error())
				err = shutdownErr
			}
		}

		// Clean up sessions
		s.logger.Info("Cleaning up sessions")
		sessionsMu.Lock()
		for token := range sessions {
			delete(sessions, token)
		}
		sessionsMu.Unlock()

		// Clean up CSRF tokens
		csrfTokensMu.Lock()
		for token := range csrfTokens {
			delete(csrfTokens, token)
		}
		csrfTokensMu.Unlock()

		// Note: Database connection is managed by the caller, not closed here
		s.logger.Info("Admin server shutdown complete")
	})
	return err
}

// Stats holds dashboard statistics
type Stats struct {
	TotalUsers     int
	TotalDomains   int
	TotalMessages  int
	QueuePending   int
	QueueFailed    int
	ServerUptime   string
	RecentActivity []ActivityItem
}

// ActivityItem represents a recent activity entry
type ActivityItem struct {
	Time        time.Time
	Type        string
	Description string
	Status      string
}

// getStats retrieves dashboard statistics
func (s *Server) getStats(ctx context.Context) (*Stats, error) {
	stats := &Stats{}

	// Count users
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&stats.TotalUsers)
	if err != nil {
		return nil, err
	}

	// Count domains
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM domains").Scan(&stats.TotalDomains)
	if err != nil {
		return nil, err
	}

	// Get queue stats if available
	if s.queue != nil {
		queueStats, err := s.queue.Stats(ctx)
		if err == nil {
			stats.QueuePending = int(queueStats.Pending)
			stats.QueueFailed = int(queueStats.Failed)
		}
	}

	// Get recent auth activity
	rows, err := s.db.QueryContext(ctx, `
		SELECT username, remote_addr, protocol, success, created_at
		FROM auth_log
		ORDER BY created_at DESC
		LIMIT 10
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var item ActivityItem
			var username, remoteAddr, protocol string
			var success bool
			var createdAt time.Time
			if err := rows.Scan(&username, &remoteAddr, &protocol, &success, &createdAt); err == nil {
				item.Time = createdAt
				item.Type = "auth"
				if success {
					item.Status = "success"
					item.Description = fmt.Sprintf("%s logged in via %s from %s", username, protocol, remoteAddr)
				} else {
					item.Status = "failed"
					item.Description = fmt.Sprintf("Failed login for %s via %s from %s", username, protocol, remoteAddr)
				}
				stats.RecentActivity = append(stats.RecentActivity, item)
			}
		}
	}

	return stats, nil
}

// renderTemplate renders a template with the given data
func (s *Server) renderTemplate(w http.ResponseWriter, name string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}

	// Add common data
	data["CSRFToken"] = w.Header().Get("X-CSRF-Token")

	tmpl, ok := s.templates[name]
	if !ok {
		s.logger.Error("Template not found", "template", name)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Set content type before executing template
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Execute template with proper error handling
	// We can't set headers after writing body, so we need to handle errors carefully
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		s.logger.Error("Failed to render template", "template", name, "error", err.Error())
		// If headers already sent, we can't send error page
		// Log the error and let connection close
		return
	}
}
