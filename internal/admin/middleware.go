package admin

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// Session represents an admin session
type session struct {
	userID    int64
	createdAt time.Time
	expiresAt time.Time
}

// In-memory cache for sessions (backed by database)
var (
	sessionCache   = make(map[string]*session)
	sessionCacheMu sync.RWMutex
)

// InitSessionsTable creates the sessions table if it doesn't exist
func InitSessionsTable(db *sql.DB) error {
	// Create table
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS admin_sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL
		)
	`)
	if err != nil {
		return err
	}

	// Create indexes separately (some drivers don't support multiple statements)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_admin_sessions_user ON admin_sessions(user_id)`)
	_, _ = db.Exec(`CREATE INDEX IF NOT EXISTS idx_admin_sessions_expires ON admin_sessions(expires_at)`)

	return nil
}

// createSession creates a new session and returns the token
func (s *Server) createSession(userID int64) string {
	token := generateToken()
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	// Store in database
	_, err := s.db.Exec(
		`INSERT INTO admin_sessions (token, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		token, userID, now, expiresAt,
	)
	if err != nil {
		s.logger.Error("Failed to create session in database", "error", err.Error())
		return ""
	}

	// Cache in memory with size limit
	sessionCacheMu.Lock()
	// Enforce max cache size to prevent memory exhaustion
	if len(sessionCache) >= maxSessionCache {
		// Evict expired sessions first
		for t, sess := range sessionCache {
			if now.After(sess.expiresAt) || len(sessionCache) >= maxSessionCache {
				delete(sessionCache, t)
				if len(sessionCache) < maxSessionCache*9/10 { // Keep 90% capacity
					break
				}
			}
		}
	}
	sessionCache[token] = &session{
		userID:    userID,
		createdAt: now,
		expiresAt: expiresAt,
	}
	sessionCacheMu.Unlock()

	return token
}

// invalidateUserSessions removes all sessions for a specific user
// Called when a user is deleted to prevent orphaned sessions
func (s *Server) invalidateUserSessions(userID int64) {
	// Delete from database
	_, _ = s.db.Exec(`DELETE FROM admin_sessions WHERE user_id = ?`, userID)

	// Clear from cache
	sessionCacheMu.Lock()
	defer sessionCacheMu.Unlock()
	for token, sess := range sessionCache {
		if sess.userID == userID {
			delete(sessionCache, token)
		}
	}
}

// DomainInfo holds information about the current request's domain
type DomainInfo struct {
	ID           int64
	Name         string
	MailHostname string
	IsPrimary    bool
}

// AdminUser represents the authenticated admin user with role and permissions
type AdminUser struct {
	ID          int64
	Email       string
	Role        string   // "super_admin", "domain_admin", "support"
	Permissions []string // ["users.read", "users.create", ...]
	DomainIDs   []int64  // scoped domain IDs (nil = all domains)
}

// HasPermission checks if the admin user has a specific permission
func (u *AdminUser) HasPermission(perm string) bool {
	for _, p := range u.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

// HasDomainAccess checks if the admin user has access to a specific domain.
// Only super_admin and support (non-domain-scoped roles) get global access.
// domain_admin must have explicit domain assignments.
func (u *AdminUser) HasDomainAccess(domainID int64) bool {
	// Only non-domain-scoped roles get global access
	if u.DomainIDs == nil {
		return true
	}
	for _, id := range u.DomainIDs {
		if id == domainID {
			return true
		}
	}
	return false
}

type adminUserContextKeyType string

const adminUserContextKey adminUserContextKeyType = "admin_user"

// GetAdminUser retrieves the admin user from the request context
func GetAdminUser(r *http.Request) *AdminUser {
	if user, ok := r.Context().Value(adminUserContextKey).(*AdminUser); ok {
		return user
	}
	return nil
}

type domainContextKeyType string

const domainContextKey domainContextKeyType = "domain"

// detectDomain detects the domain based on the Host header
func (s *Server) detectDomain(r *http.Request) *DomainInfo {
	host := r.Host

	// Strip port if present
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Look up domain by mail_hostname
	var domain DomainInfo
	var isPrimaryInt int
	err := s.db.QueryRowContext(r.Context(),
		`SELECT id, name, COALESCE(mail_hostname, 'mail.' || name), COALESCE(is_primary, 0)
		 FROM domains WHERE mail_hostname = ? OR 'mail.' || name = ?`,
		host, host,
	).Scan(&domain.ID, &domain.Name, &domain.MailHostname, &isPrimaryInt)

	if err != nil {
		// Check if this is the primary hostname from config
		if host == s.config.Server.Hostname {
			// Return primary domain
			s.db.QueryRowContext(r.Context(),
				`SELECT id, name, COALESCE(mail_hostname, 'mail.' || name), 1
				 FROM domains WHERE is_primary = 1 OR name = ? LIMIT 1`,
				s.config.Server.Domain,
			).Scan(&domain.ID, &domain.Name, &domain.MailHostname, &isPrimaryInt)
			domain.IsPrimary = true
			return &domain
		}
		return nil
	}

	domain.IsPrimary = isPrimaryInt == 1
	return &domain
}

// GetDomainFromContext retrieves the domain from the request context
func GetDomainFromContext(r *http.Request) *DomainInfo {
	if domain, ok := r.Context().Value(domainContextKey).(*DomainInfo); ok {
		return domain
	}
	return nil
}

// withDomainDetection adds domain info to the request context
func (s *Server) withDomainDetection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		domain := s.detectDomain(r)
		if domain != nil {
			ctx := context.WithValue(r.Context(), domainContextKey, domain)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// withPrimaryDomainOnly restricts access to primary domain only
func (s *Server) withPrimaryDomainOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		domain := s.detectDomain(r)

		// Allow if domain is primary OR if accessing via the configured hostname
		if domain != nil && domain.IsPrimary {
			next.ServeHTTP(w, r)
			return
		}

		// Also allow if Host matches the configured server hostname
		host := r.Host
		if idx := strings.Index(host, ":"); idx != -1 {
			host = host[:idx]
		}
		if host == s.config.Server.Hostname {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "Admin access only available on primary domain", http.StatusForbidden)
	})
}

// validateSession checks if a session token is valid
func (s *Server) validateSession(token string) (int64, bool) {
	// Validate token format: must be valid hex and minimum length
	if !isValidToken(token) {
		return 0, false
	}

	now := time.Now()

	// Check cache first
	sessionCacheMu.RLock()
	sess, exists := sessionCache[token]
	sessionCacheMu.RUnlock()

	if exists {
		if now.After(sess.expiresAt) {
			// Expired - remove from cache and DB
			s.deleteSession(token)
			return 0, false
		}
		return sess.userID, true
	}

	// Not in cache, check database
	var userID int64
	var expiresAt time.Time
	var createdAt time.Time
	err := s.db.QueryRow(
		`SELECT user_id, created_at, expires_at FROM admin_sessions WHERE token = ?`,
		token,
	).Scan(&userID, &createdAt, &expiresAt)

	if err != nil {
		return 0, false
	}

	if now.After(expiresAt) {
		// Expired - remove from DB
		s.deleteSession(token)
		return 0, false
	}

	// Cache the valid session
	sessionCacheMu.Lock()
	sessionCache[token] = &session{
		userID:    userID,
		createdAt: createdAt,
		expiresAt: expiresAt,
	}
	sessionCacheMu.Unlock()

	return userID, true
}

// deleteSession removes a session from both cache and database
func (s *Server) deleteSession(token string) {
	sessionCacheMu.Lock()
	delete(sessionCache, token)
	sessionCacheMu.Unlock()

	_, _ = s.db.Exec(`DELETE FROM admin_sessions WHERE token = ?`, token)
}

// withAuth wraps a handler with authentication check and loads role/permissions into context
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check primary domain access for admin routes
		host := r.Host
		if idx := strings.Index(host, ":"); idx != -1 {
			host = host[:idx]
		}

		// Allow if Host matches the configured server hostname
		isPrimary := host == s.config.Server.Hostname
		if !isPrimary {
			// Check if this is a primary domain via database
			domain := s.detectDomain(r)
			isPrimary = domain != nil && domain.IsPrimary
		}

		if !isPrimary {
			http.Error(w, "Admin access only available on primary domain", http.StatusForbidden)
			return
		}

		cookie, err := r.Cookie("admin_session")
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		userID, valid := s.validateSession(cookie.Value)
		if !valid {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		// Load admin user with role and permissions
		adminUser, err := s.loadAdminUser(r.Context(), userID)
		if err != nil || adminUser == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		// Store admin user in context
		ctx := context.WithValue(r.Context(), adminUserContextKey, adminUser)
		next(w, r.WithContext(ctx))
	}
}

// loadAdminUser loads the admin user's role and permissions from the database.
// Falls back to is_admin check for backward compatibility if no roles are assigned.
func (s *Server) loadAdminUser(ctx context.Context, userID int64) (*AdminUser, error) {
	var username, domain string
	var isAdmin bool
	err := s.db.QueryRowContext(ctx,
		`SELECT u.username, d.name, u.is_admin FROM users u
		 JOIN domains d ON u.domain_id = d.id WHERE u.id = ?`, userID,
	).Scan(&username, &domain, &isAdmin)
	if err != nil {
		return nil, err
	}

	user := &AdminUser{
		ID:    userID,
		Email: username + "@" + domain,
	}

	// Try to load role from user_roles table
	var roleName string
	err = s.db.QueryRowContext(ctx,
		`SELECT r.name FROM user_roles ur
		 JOIN roles r ON ur.role_id = r.id
		 WHERE ur.user_id = ? LIMIT 1`, userID,
	).Scan(&roleName)

	if err == sql.ErrNoRows {
		// No role assigned - fall back to is_admin boolean
		if !isAdmin {
			return nil, nil // Not an admin
		}
		// Legacy admin without role assignment - treat as super_admin
		user.Role = "super_admin"
	} else if err != nil {
		// Table might not exist yet (pre-migration) - fall back to is_admin
		if !isAdmin {
			return nil, nil
		}
		user.Role = "super_admin"
	} else {
		user.Role = roleName
	}

	// Load permissions for the role
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.name FROM permissions p
		 JOIN role_permissions rp ON rp.permission_id = p.id
		 JOIN roles r ON rp.role_id = r.id
		 WHERE r.name = ?`, user.Role,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var perm string
			if rows.Scan(&perm) == nil {
				user.Permissions = append(user.Permissions, perm)
			}
		}
	}

	// If no permissions loaded (pre-migration), grant all for super_admin
	if len(user.Permissions) == 0 && user.Role == "super_admin" {
		user.Permissions = []string{
			"users.create", "users.read", "users.update", "users.delete", "users.password",
			"domains.create", "domains.read", "domains.update", "domains.delete",
			"aliases.manage", "lists.manage", "logs.view", "audit.view",
			"settings.manage", "features.manage", "queue.manage",
		}
	}

	// Load scoped domain IDs for domain_admin.
	// Initialize to empty slice (not nil) so HasDomainAccess denies by default.
	if user.Role == "domain_admin" {
		user.DomainIDs = []int64{}
		domainRows, err := s.db.QueryContext(ctx,
			`SELECT domain_id FROM user_roles WHERE user_id = ? AND domain_id IS NOT NULL`, userID,
		)
		if err == nil {
			defer domainRows.Close()
			for domainRows.Next() {
				var domainID int64
				if domainRows.Scan(&domainID) == nil {
					user.DomainIDs = append(user.DomainIDs, domainID)
				}
			}
		}
	}

	return user, nil
}

// requirePermission returns a middleware that checks if the admin user has a specific permission
func (s *Server) requirePermission(perm string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := GetAdminUser(r)
		if user == nil || !user.HasPermission(perm) {
			http.Error(w, "Forbidden - insufficient permissions", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// withAPIAuth wraps a handler with JSON API authentication check
func (s *Server) withAPIAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("admin_session")
		if err != nil {
			s.jsonError(w, http.StatusUnauthorized, "Authentication required")
			return
		}

		userID, valid := s.validateSession(cookie.Value)
		if !valid {
			s.jsonError(w, http.StatusUnauthorized, "Session expired")
			return
		}

		// Check user is still admin
		var isAdmin bool
		err = s.db.QueryRowContext(r.Context(), "SELECT is_admin FROM users WHERE id = ?", userID).Scan(&isAdmin)
		if err != nil || !isAdmin {
			s.jsonError(w, http.StatusForbidden, "Admin access required")
			return
		}

		next(w, r)
	}
}

// CSRF token handling with bounded cache
const (
	maxCSRFTokens   = 10000 // Maximum CSRF tokens in cache
	maxSessionCache = 10000 // Maximum sessions in cache
	maxCSRFFormBody = 10 << 20
)

var (
	csrfTokens   = make(map[string]csrfTokenState)
	csrfTokensMu sync.RWMutex
)

type csrfTokenState struct {
	Expiry  time.Time
	Binding string
}

// withCSRF wraps a handler with CSRF protection
func (s *Server) withCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF for user portal (has its own CSRF handling)
		if strings.HasPrefix(r.URL.Path, "/account/") || r.URL.Path == "/account" {
			next.ServeHTTP(w, r)
			return
		}

		// Skip CSRF for GET/HEAD/OPTIONS
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			// Generate token for forms
			token := generateToken()
			csrfTokensMu.Lock()
			// Enforce max cache size to prevent memory exhaustion
			if len(csrfTokens) >= maxCSRFTokens {
				// Evict oldest tokens (simple eviction: remove expired first)
				now := time.Now()
				for t, state := range csrfTokens {
					if now.After(state.Expiry) || len(csrfTokens) >= maxCSRFTokens {
						delete(csrfTokens, t)
						if len(csrfTokens) < maxCSRFTokens*9/10 { // Keep 90% capacity
							break
						}
					}
				}
			}
			csrfTokens[token] = csrfTokenState{
				Expiry:  time.Now().Add(1 * time.Hour),
				Binding: s.csrfBinding(r),
			}
			csrfTokensMu.Unlock()

			w.Header().Set("X-CSRF-Token", token)
			next.ServeHTTP(w, r)
			return
		}

		// Validate CSRF token for state-changing requests.
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			token = r.URL.Query().Get("csrf_token")
		}
		if token == "" && requestHasFormBody(r) {
			if err := parseFormWithLimit(w, r, maxCSRFFormBody); err != nil {
				status := formErrorStatus(err)
				http.Error(w, "Invalid or expired CSRF token", status)
				return
			}
			token = r.PostForm.Get("csrf_token")
		}

		// Validate token format
		if !isValidToken(token) {
			http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
			return
		}

		csrfTokensMu.RLock()
		state, exists := csrfTokens[token]
		csrfTokensMu.RUnlock()

		now := time.Now()
		if !exists || now.After(state.Expiry) {
			http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
			return
		}

		if state.Binding != s.csrfBinding(r) {
			http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
			return
		}

		// Keep token valid for reuse (for multi-step wizards and AJAX calls)
		// Token will naturally expire after 1 hour
		w.Header().Set("X-CSRF-Token", token)

		next.ServeHTTP(w, r)
	})
}

func requestHasFormBody(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	return strings.HasPrefix(contentType, "application/x-www-form-urlencoded") ||
		strings.HasPrefix(contentType, "multipart/form-data") ||
		strings.HasPrefix(contentType, "text/plain")
}

// generateToken generates a cryptographically secure token
// Panics if crypto/rand fails - security must not be compromised
func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Never fall back to weak tokens - this is a critical security function
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// sessionCleanupStop is used to stop the session cleanup goroutine
var (
	sessionCleanupStop   chan struct{}
	sessionCleanupStopMu sync.Mutex
)

// CleanupExpiredSessions removes expired sessions periodically
func CleanupExpiredSessions(db *sql.DB) {
	sessionCleanupStopMu.Lock()
	sessionCleanupStop = make(chan struct{})
	stopCh := sessionCleanupStop
	sessionCleanupStopMu.Unlock()

	ticker := time.NewTicker(15 * time.Minute)

	go func() {
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				now := time.Now()

				// Clean expired sessions from database
				if db != nil {
					if _, err := db.Exec(`DELETE FROM admin_sessions WHERE expires_at < ?`, now); err != nil {
						// Log error but continue - non-critical
					}
				}

				// Clean session cache
				sessionCacheMu.Lock()
				for token, sess := range sessionCache {
					if now.After(sess.expiresAt) {
						delete(sessionCache, token)
					}
				}
				sessionCacheMu.Unlock()

				// Clean CSRF tokens
				csrfTokensMu.Lock()
				for token, state := range csrfTokens {
					if now.After(state.Expiry) {
						delete(csrfTokens, token)
					}
				}
				csrfTokensMu.Unlock()
			}
		}
	}()
}

// StopSessionCleanup stops the session cleanup goroutine
func StopSessionCleanup() {
	sessionCleanupStopMu.Lock()
	defer sessionCleanupStopMu.Unlock()
	if sessionCleanupStop != nil {
		close(sessionCleanupStop)
		sessionCleanupStop = nil
	}
}

// isValidToken validates token format (must be hex and minimum 32 chars)
func isValidToken(token string) bool {
	// Minimum length check (32 hex chars = 16 bytes)
	if len(token) < 32 {
		return false
	}
	// Maximum length check to prevent DoS
	if len(token) > 128 {
		return false
	}
	// Validate hex encoding
	_, err := hex.DecodeString(token)
	return err == nil
}

func (s *Server) csrfBinding(r *http.Request) string {
	if cookie, err := r.Cookie("admin_session"); err == nil && cookie.Value != "" {
		return "session:" + cookie.Value
	}

	if s != nil && s.rateLimiter != nil {
		return "client:" + s.rateLimiter.GetClientIP(r)
	}

	return "client:" + r.RemoteAddr
}

// withTimeout adds a timeout to requests
func (s *Server) withTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			// Create a channel to signal completion
			done := make(chan struct{})

			// Run the handler in a goroutine
			go func() {
				defer close(done)
				next.ServeHTTP(w, r.WithContext(ctx))
			}()

			// Wait for either completion or timeout
			select {
			case <-done:
				// Request completed successfully
				return
			case <-ctx.Done():
				// Timeout occurred
				if ctx.Err() == context.DeadlineExceeded {
					http.Error(w, "Request timeout", http.StatusGatewayTimeout)
				}
				return
			}
		})
	}
}

// withPanicRecovery adds panic recovery to prevent crashes
func (s *Server) withPanicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// Log the panic with truncated stack trace to prevent info disclosure
				stack := debug.Stack()
				// Limit stack trace to 2KB to prevent logging sensitive details
				const maxStackSize = 2048
				stackStr := string(stack)
				if len(stackStr) > maxStackSize {
					stackStr = stackStr[:maxStackSize] + "\n... (truncated)"
				}
				s.logger.Error(
					"Panic recovered in HTTP handler",
					"error", fmt.Sprintf("%v", err),
					"path", r.URL.Path,
					"method", r.Method,
					"remote_addr", r.RemoteAddr,
					"stack", stackStr,
				)

				// Return 500 error to client
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// withRequestLogging logs all HTTP requests
func (s *Server) withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapper := &responseWriterWrapper{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Log request
		s.logger.Info(
			"HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)

		next.ServeHTTP(wrapper, r)

		// Log response
		duration := time.Since(start)
		s.logger.Info(
			"HTTP response",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapper.statusCode,
			"duration_ms", duration.Milliseconds(),
		)
	})
}

// withSecurityHeaders adds security headers to all responses
func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Dev mode: allow CORS from Next.js dev server
		if s.config.Admin.DevMode {
			origin := r.Header.Get("Origin")
			if origin == "http://localhost:3000" || origin == "http://localhost:3001" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusOK)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
		}

		// CRITICAL: Block all cross-origin requests - admin panel should NEVER be accessed from external sites
		w.Header().Set("Access-Control-Allow-Origin", "") // Explicitly NO CORS
		w.Header().Set("Access-Control-Allow-Methods", "")
		w.Header().Set("Access-Control-Allow-Headers", "")

		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// XSS protection (legacy, but still useful for older browsers)
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Referrer policy - don't leak URLs to external sites
		w.Header().Set("Referrer-Policy", "no-referrer")

		// Content Security Policy - restrict resource loading
		// Note: 'unsafe-inline' + 'unsafe-eval' needed for Next.js SPA
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data: blob:; "+
				"font-src 'self' data:; "+
				"connect-src 'self'; "+
				"form-action 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"object-src 'none'; "+
				"upgrade-insecure-requests")

		// Permissions Policy - disable unnecessary browser features
		w.Header().Set("Permissions-Policy",
			"accelerometer=(), camera=(), geolocation=(), gyroscope=(), "+
				"magnetometer=(), microphone=(), payment=(), usb=(), "+
				"interest-cohort=()") // Block FLoC tracking

		// Cache control for admin pages - don't cache sensitive data
		// Skip for static assets (SPA handler sets its own cache headers)
		if r.URL.Path != "/admin/login" && !strings.HasPrefix(r.URL.Path, "/admin/_next/") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}

		next.ServeHTTP(w, r)
	})
}

// withBodySizeLimit limits request body size to prevent DoS via large payloads.
// Backup restore has its own limit via multipart form parsing.
func (s *Server) withBodySizeLimit(next http.Handler) http.Handler {
	const maxBodySize = 10 << 20 // 10 MB
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.ContentLength != 0 {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		}
		next.ServeHTTP(w, r)
	})
}

// responseWriterWrapper wraps http.ResponseWriter to capture status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// getSessionUser returns the email of the currently logged-in admin user
// Returns "unknown" if the session is invalid or user not found
func getSessionUser(r *http.Request) string {
	cookie, err := r.Cookie("admin_session")
	if err != nil {
		return "unknown"
	}

	if !isValidToken(cookie.Value) {
		return "unknown"
	}

	sessionCacheMu.RLock()
	sess, exists := sessionCache[cookie.Value]
	sessionCacheMu.RUnlock()

	if !exists || time.Now().After(sess.expiresAt) {
		return "unknown"
	}

	return fmt.Sprintf("user:%d", sess.userID)
}

// isSecureContext returns true if the request came over HTTPS
// Checks both direct TLS and X-Forwarded-Proto header (for reverse proxy)
func isSecureContext(r *http.Request) bool {
	// Direct TLS connection
	if r.TLS != nil {
		return true
	}
	// Check if behind HTTPS proxy
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		return true
	}
	// In production with nginx, always return true
	// This is safer than potentially setting Secure=false
	return true
}
