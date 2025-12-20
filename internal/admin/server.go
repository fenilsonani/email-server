package admin

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"net/http"
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
	templates     *template.Template
	httpServer    *http.Server
}

// NewServer creates a new admin server
func NewServer(cfg *config.Config, db *sql.DB, authenticator *auth.Authenticator, store *maildir.Store, sieveStore *sieve.Store, logger *logging.Logger) (*Server, error) {
	// Parse templates
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	s := &Server{
		config:        cfg,
		db:            db,
		authenticator: authenticator,
		store:         store,
		sieveStore:    sieveStore,
		logger:        logger,
		templates:     tmpl,
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
