package userportal

import (
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/audit"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/logging"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Server handles the user self-service portal
type Server struct {
	db            *sql.DB
	authenticator *auth.Authenticator
	auditLogger   *audit.Logger
	logger        *logging.Logger
	templates     map[string]*template.Template
	rateLimiter   *RateLimiter
}

// NewServer creates a new user portal server
func NewServer(db *sql.DB, authenticator *auth.Authenticator, auditLogger *audit.Logger, logger *logging.Logger) (*Server, error) {
	s := &Server{
		db:            db,
		authenticator: authenticator,
		auditLogger:   auditLogger,
		logger:        logger,
		templates:     make(map[string]*template.Template),
		rateLimiter:   NewRateLimiter(5, 15*time.Minute, 30*time.Minute),
	}

	if err := s.loadTemplates(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Server) loadTemplates() error {
	baseContent, err := templatesFS.ReadFile("templates/base.html")
	if err != nil {
		return fmt.Errorf("failed to read base template: %w", err)
	}
	baseStr := string(baseContent)

	funcMap := template.FuncMap{
		"safeHTML": func(str string) template.HTML { return template.HTML(str) },
	}

	pages := []string{
		"login.html",
		"dashboard.html",
		"password.html",
		"profile.html",
		"forwarding.html",
		"vacation.html",
	}

	for _, page := range pages {
		pageContent, err := templatesFS.ReadFile("templates/" + page)
		if err != nil {
			return fmt.Errorf("failed to read template %s: %w", page, err)
		}

		combined := strings.Replace(baseStr, "<!-- CONTENT_PLACEHOLDER -->", string(pageContent), 1)

		tmpl, err := template.New(page).Funcs(funcMap).Parse(combined)
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", page, err)
		}

		s.templates[page] = tmpl
	}

	return nil
}

// RegisterRoutes registers all user portal routes on the given mux
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Static files
	staticHTTP := http.FileServer(http.FS(staticFS))
	mux.Handle("/account/static/", http.StripPrefix("/account/", staticHTTP))

	// Public routes (no auth)
	mux.HandleFunc("/account/login", s.withCSRF(s.handleLogin))

	// Protected routes (auth required)
	mux.HandleFunc("/account/", s.withUserAuth(s.handleDashboard))
	mux.HandleFunc("/account/logout", s.withUserAuth(s.handleLogout))
	mux.HandleFunc("/account/password", s.withUserAuth(s.withCSRF(s.handlePassword)))
	mux.HandleFunc("/account/profile", s.withUserAuth(s.withCSRF(s.handleProfile)))
	mux.HandleFunc("/account/forwarding", s.withUserAuth(s.withCSRF(s.handleForwarding)))
	mux.HandleFunc("/account/vacation", s.withUserAuth(s.withCSRF(s.handleVacation)))
}

func (s *Server) renderTemplate(w http.ResponseWriter, name string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}

	data["CSRFToken"] = w.Header().Get("X-CSRF-Token")

	tmpl, ok := s.templates[name]
	if !ok {
		s.logger.Error("Template not found", "template", name)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		s.logger.Error("Failed to render template", "template", name, "error", err.Error())
		return
	}
}
