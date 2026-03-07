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

	"github.com/fenilsonani/email-server/internal/audit"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/features"
	"github.com/fenilsonani/email-server/internal/lists"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/sieve"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
	"github.com/fenilsonani/email-server/internal/userportal"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server handles the admin web interface
type Server struct {
	config        *config.Config
	db            *sql.DB
	authenticator *auth.Authenticator
	store         *maildir.Store
	sieveStore    *sieve.Store
	featuresStore *features.Store
	listsStore    *lists.Store
	queue         *queue.RedisQueue
	logger        *logging.Logger
	auditLogger   *audit.Logger
	templates     map[string]*template.Template
	httpServer         *http.Server
	shutdownOnce       sync.Once
	rateLimiter        *RateLimiter
	previewRateLimiter *PreviewRateLimiter
	startTime          time.Time
	dnsChecker         *DNSChecker
}

// NewServer creates a new admin server
func NewServer(cfg *config.Config, db *sql.DB, authenticator *auth.Authenticator, store *maildir.Store, sieveStore *sieve.Store, q *queue.RedisQueue, logger *logging.Logger) (*Server, error) {
	// Read base template content
	baseContent, err := templatesFS.ReadFile("templates/base.html")
	if err != nil {
		return nil, fmt.Errorf("failed to read base template: %w", err)
	}
	baseStr := string(baseContent)

	// Create template function map
	funcMap := template.FuncMap{
		"sub":      func(a, b int) int { return a - b },
		"subtract": func(a, b int) int { return a - b },
		"add":      func(a, b int) int { return a + b },
		"untilStep": func(start, stop, step int) []int {
			result := []int{}
			for i := start; i < stop; i += step {
				result = append(result, i)
			}
			return result
		},
		// SECURITY: safeHTML function removed - bypasses HTML escaping and could enable XSS
		// If you need to render trusted HTML, use a proper sanitizer library instead
	}

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
		"logs.html",
		"auth_logs.html",
		"delivery_logs.html",
		"audit_logs.html",
		"queue.html",
		"dns_check.html",
		"test_email.html",
		"system.html",
		"update.html",
		"2fa_setup.html",
		"email_preview.html",
		"features.html",
		"features_screener.html",
		"features_aliases.html",
		"features_alias_form.html",
		"features_vip.html",
		"features_vip_form.html",
		"features_preferences.html",
		"features_scheduled.html",
		"features_scheduled_form.html",
		"features_snoozed.html",
		"lists.html",
		"list_form.html",
		"list_members.html",
		"list_moderation.html",
		"list_archives.html",
		"doctor.html",
	}

	for _, page := range pages {
		// Read page template content
		pageContent, err := templatesFS.ReadFile("templates/" + page)
		if err != nil {
			return nil, fmt.Errorf("failed to read template %s: %w", page, err)
		}

		// Replace the placeholder in base template with page content
		combined := strings.Replace(baseStr, "<!-- CONTENT_PLACEHOLDER -->", string(pageContent), 1)

		// Parse the combined template with function map
		tmpl, err := template.New(page).Funcs(funcMap).Parse(combined)
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
	loginTmpl, err := template.New("login.html").Funcs(funcMap).Parse(string(loginContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse login template: %w", err)
	}
	templates["login.html"] = loginTmpl

	// 2FA verify page is standalone (no base layout)
	verifyContent, err := templatesFS.ReadFile("templates/2fa_verify.html")
	if err != nil {
		return nil, fmt.Errorf("failed to read 2fa_verify template: %w", err)
	}
	verifyTmpl, err := template.New("2fa_verify.html").Funcs(funcMap).Parse(string(verifyContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse 2fa_verify template: %w", err)
	}
	templates["2fa_verify.html"] = verifyTmpl

	// Initialize audit logger
	auditLog, err := audit.NewLogger(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create audit logger: %w", err)
	}

	// Initialize sessions table for persistent sessions
	if err := InitSessionsTable(db); err != nil {
		return nil, fmt.Errorf("failed to create sessions table: %w", err)
	}

	s := &Server{
		config:        cfg,
		db:            db,
		authenticator: authenticator,
		store:         store,
		sieveStore:    sieveStore,
		queue:         q,
		logger:        logger,
		auditLogger:        auditLog,
		templates:          templates,
		rateLimiter:        DefaultRateLimiter(),
		previewRateLimiter: NewPreviewRateLimiter(),
		startTime:          time.Now(),
		dnsChecker:         NewDNSChecker(db, cfg, logger),
	}

	return s, nil
}

// SetFeaturesStore sets the features store for unique feature APIs
func (s *Server) SetFeaturesStore(store *features.Store) {
	s.featuresStore = store
}

// SetListsStore sets the lists store for mailing list management
func (s *Server) SetListsStore(store *lists.Store) {
	s.listsStore = store
}

// Start starts the admin server
func (s *Server) Start(listen string) error {
	mux := http.NewServeMux()

	// Health check endpoints (no auth required)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)

	// Prometheus metrics endpoint (no auth for scraping)
	mux.Handle("/metrics", promhttp.Handler())

	// Static files (CSS, JS)
	staticHTTP := http.FileServer(http.FS(staticFS))
	mux.Handle("/static/", http.StripPrefix("/", staticHTTP))

	// === JSON API routes (for Next.js SPA) ===
	// Auth (no withAPIAuth — handles own auth)
	mux.HandleFunc("/admin/api/auth/login", s.handleAPILogin)
	mux.HandleFunc("/admin/api/auth/logout", s.handleAPILogout)
	mux.HandleFunc("/admin/api/auth/session", s.handleAPISession)
	mux.HandleFunc("/admin/api/auth/csrf", s.handleAPICSRF)
	mux.HandleFunc("/admin/api/auth/2fa", s.handleAPI2FAVerify)
	// Dashboard
	mux.HandleFunc("/admin/api/v1/stats", s.withAPIAuth(s.handleAPIGetStats))
	// Users
	mux.HandleFunc("/admin/api/v1/users", s.withAPIAuth(s.handleAPIUsers))
	mux.HandleFunc("/admin/api/v1/users/", s.withAPIAuth(s.handleAPIUserByID))
	// Domains
	mux.HandleFunc("/admin/api/v1/domains", s.withAPIAuth(s.handleAPIDomains))
	mux.HandleFunc("/admin/api/v1/domains/", s.withAPIAuth(s.handleAPIDomainByID))
	mux.HandleFunc("/admin/api/v1/domains-list", s.withAPIAuth(s.handleAPIGetAvailableDomains))
	// Logs
	mux.HandleFunc("/admin/api/v1/logs/auth", s.withAPIAuth(s.handleAPIGetLogs))
	mux.HandleFunc("/admin/api/v1/logs/delivery", s.withAPIAuth(s.handleAPIGetLogs))
	mux.HandleFunc("/admin/api/v1/logs/audit", s.withAPIAuth(s.handleAPIGetLogs))
	// Queue
	mux.HandleFunc("/admin/api/v1/queue", s.withAPIAuth(s.handleAPIGetQueue))
	// Features
	mux.HandleFunc("/admin/api/v1/features", s.withAPIAuth(s.handleAPIGetFeatures))
	// Lists
	mux.HandleFunc("/admin/api/v1/lists", s.withAPIAuth(s.handleAPIListsCollection))
	// System
	mux.HandleFunc("/admin/api/v1/system", s.withAPIAuth(s.handleAPIGetSystem))

	// --- v2 API routes ---
	// Features: Screener
	mux.HandleFunc("/admin/api/v1/features/screener", s.withAPIAuth(s.handleAPIScreener))
	mux.HandleFunc("/admin/api/v1/features/screener/", s.withAPIAuth(s.handleAPIScreenerByID))
	// Features: Aliases
	mux.HandleFunc("/admin/api/v1/features/aliases", s.withAPIAuth(s.handleAPIAliases))
	mux.HandleFunc("/admin/api/v1/features/aliases/", s.withAPIAuth(s.handleAPIAliasByID))
	// Features: VIP
	mux.HandleFunc("/admin/api/v1/features/vip", s.withAPIAuth(s.handleAPIVIP))
	mux.HandleFunc("/admin/api/v1/features/vip/", s.withAPIAuth(s.handleAPIVIPByID))
	// Features: Preferences
	mux.HandleFunc("/admin/api/v1/features/preferences", s.withAPIAuth(s.handleAPIPreferences))
	// Features: Scheduled & Snoozed
	mux.HandleFunc("/admin/api/v1/features/scheduled", s.withAPIAuth(s.handleAPIScheduled))
	mux.HandleFunc("/admin/api/v1/features/snoozed", s.withAPIAuth(s.handleAPISnoozed))
	// Lists: CRUD + sub-resources (members, moderation, archives)
	mux.HandleFunc("/admin/api/v1/lists/", s.withAPIAuth(s.handleAPIListByID))
	// Note: POST /admin/api/v1/lists is handled by handleAPIListsCollection above
	// Queue: retry/delete by ID
	mux.HandleFunc("/admin/api/v1/queue/", s.withAPIAuth(s.handleAPIQueueByID))
	// Sieve
	mux.HandleFunc("/admin/api/v1/sieve", s.withAPIAuth(s.handleAPISieve))
	mux.HandleFunc("/admin/api/v1/sieve/validate", s.withAPIAuth(s.handleAPISieveValidate))
	// System: Backup
	mux.HandleFunc("/admin/api/v1/system/backup/status", s.withAPIAuth(s.handleAPIBackupStatus))
	mux.HandleFunc("/admin/api/v1/system/backup", s.withAPIAuth(s.handleAPIBackupTrigger))
	mux.HandleFunc("/admin/api/v1/system/backup/history", s.withAPIAuth(s.handleAPIBackupHistory))
	mux.HandleFunc("/admin/api/v1/system/restore", s.withAPIAuth(s.handleAPIRestore))
	// System: Certificates
	mux.HandleFunc("/admin/api/v1/system/certificates", s.withAPIAuth(s.handleAPICertificates))
	mux.HandleFunc("/admin/api/v1/system/certificates/renew", s.withAPIAuth(s.handleAPICertificatesRenew))
	// System: 2FA
	mux.HandleFunc("/admin/api/v1/system/2fa/status", s.withAPIAuth(s.handleAPI2FAStatus))
	mux.HandleFunc("/admin/api/v1/system/2fa/setup", s.withAPIAuth(s.handleAPI2FASetup))
	mux.HandleFunc("/admin/api/v1/system/2fa/verify", s.withAPIAuth(s.handleAPI2FAVerifyCode))
	mux.HandleFunc("/admin/api/v1/system/2fa/disable", s.withAPIAuth(s.handleAPI2FADisable))
	// System: Updates
	mux.HandleFunc("/admin/api/v1/system/check-update", s.withAPIAuth(s.handleAPICheckUpdate))
	// System: DKIM Auto-Rotate
	mux.HandleFunc("/admin/api/v1/system/dkim-autorotate", s.withAPIAuth(s.handleAPIDKIMAutoRotate))
	mux.HandleFunc("/admin/api/v1/system/dkim-autorotate/rotate-now", s.withAPIAuth(s.handleAPIDKIMAutoRotate))
	// Tools
	mux.HandleFunc("/admin/api/v1/tools/doctor", s.withAPIAuth(s.handleAPIToolsDoctor))
	mux.HandleFunc("/admin/api/v1/tools/test-email", s.withAPIAuth(s.handleAPIToolsTestEmail))
	mux.HandleFunc("/admin/api/v1/tools/dns-check", s.withAPIAuth(s.handleAPIToolsDNSCheck))

	// SPA catch-all: serve Next.js static export for all /admin/ paths
	// (API routes above are more specific and match first)
	// No withAuth — the SPA handles auth client-side via /admin/api/auth/session
	// StripPrefix removes /admin so the SPA handler sees paths relative to root
	mux.Handle("/admin/", http.StripPrefix("/admin", s.serveSPA()))

	// User portal (separate auth from admin)
	userPortal, err := userportal.NewServer(s.db, s.authenticator, s.auditLogger, s.logger)
	if err != nil {
		s.logger.Error("Failed to initialize user portal", "error", err.Error())
	} else {
		userPortal.RegisterRoutes(mux)
		s.logger.Info("User portal routes registered at /account/")
	}

	// Build middleware chain (order matters: innermost first, then wrapping outward)
	// The execution order will be: logging -> security headers -> panic recovery -> domain detection -> CSRF -> routes
	handler := s.withCSRF(mux)
	handler = s.withDomainDetection(handler)
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

	// Start DNS checker background service
	s.dnsChecker.Start()

	// Start cleanup goroutine
	CleanupExpiredSessions(s.db)

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

		// Stop DNS checker
		if s.dnsChecker != nil {
			s.dnsChecker.Stop()
		}

		// Shutdown HTTP server
		if s.httpServer != nil {
			if shutdownErr := s.httpServer.Shutdown(ctx); shutdownErr != nil {
				s.logger.Error("Error shutting down HTTP server", "error", shutdownErr.Error())
				err = shutdownErr
			}
		}

		// Clean up session cache (sessions persist in database)
		s.logger.Info("Cleaning up session cache")
		sessionCacheMu.Lock()
		for token := range sessionCache {
			delete(sessionCache, token)
		}
		sessionCacheMu.Unlock()

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
	TotalUsers        int
	TotalDomains      int
	TotalMessages     int
	QueuePending      int
	QueueFailed       int
	TotalLists        int
	TotalListMembers  int
	PendingModeration int
	ServerUptime      string
	RecentActivity    []ActivityItem
}

// ActivityItem represents a recent activity entry
type ActivityItem struct {
	Time        time.Time `json:"time"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
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

	// Get mailing list stats (non-critical, log errors but continue)
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mailing_lists WHERE is_active = 1").Scan(&stats.TotalLists); err != nil {
		s.logger.Debug("Failed to get mailing list count", "error", err.Error())
	}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM list_members").Scan(&stats.TotalListMembers); err != nil {
		s.logger.Debug("Failed to get list members count", "error", err.Error())
	}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM list_moderation_queue WHERE status = 'pending'").Scan(&stats.PendingModeration); err != nil {
		s.logger.Debug("Failed to get pending moderation count", "error", err.Error())
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
