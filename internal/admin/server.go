package admin

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
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
	logger        *logging.Logger
	templates     map[string]*template.Template
	httpServer    *http.Server
}

// NewServer creates a new admin server
func NewServer(cfg *config.Config, db *sql.DB, authenticator *auth.Authenticator, store *maildir.Store, sieveStore *sieve.Store, logger *logging.Logger) (*Server, error) {
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
		logger:        logger,
		templates:     templates,
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
	mux.HandleFunc("/admin/api/stats", s.withAuth(s.handleAPIStats))

	s.httpServer = &http.Server{
		Addr:         listen,
		Handler:      s.withCSRF(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	s.logger.Info("Starting admin server", "listen", listen)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the admin server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
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

	// Execute template by its name
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		s.logger.Error("Failed to render template", "template", name, "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
