package userportal

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

type contextKey string

const userIDContextKey contextKey = "userID"
const domainContextKey contextKey = "domain"

// DomainInfo holds information about the current request's domain
type DomainInfo struct {
	ID           int64
	Name         string
	MailHostname string
	IsPrimary    bool
}

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
		return nil
	}

	domain.IsPrimary = isPrimaryInt == 1
	return &domain
}

// getDomain retrieves the domain from the request context
func getDomain(r *http.Request) *DomainInfo {
	if domain, ok := r.Context().Value(domainContextKey).(*DomainInfo); ok {
		return domain
	}
	return nil
}

// withUserAuth wraps handlers requiring user authentication
func (s *Server) withUserAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Detect domain from host
		domain := s.detectDomain(r)

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Redirect(w, r, "/account/login", http.StatusSeeOther)
			return
		}

		userID, valid := s.validateSession(cookie.Value)
		if !valid {
			clearSessionCookie(w)
			http.Redirect(w, r, "/account/login", http.StatusSeeOther)
			return
		}

		// Check user is still active and belongs to the current domain
		var isActive bool
		var userDomainID int64
		err = s.db.QueryRowContext(r.Context(),
			"SELECT is_active, domain_id FROM users WHERE id = ?",
			userID).Scan(&isActive, &userDomainID)
		if err != nil || !isActive {
			clearSessionCookie(w)
			http.Redirect(w, r, "/account/login", http.StatusSeeOther)
			return
		}

		// If we detected a domain, verify user belongs to it
		if domain != nil && userDomainID != domain.ID {
			clearSessionCookie(w)
			http.Redirect(w, r, "/account/login", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), userIDContextKey, userID)
		if domain != nil {
			ctx = context.WithValue(ctx, domainContextKey, domain)
		}
		next(w, r.WithContext(ctx))
	}
}

// getUserID retrieves the user ID from the request context
func getUserID(r *http.Request) int64 {
	if id, ok := r.Context().Value(userIDContextKey).(int64); ok {
		return id
	}
	return 0
}

// CSRF token handling
var (
	csrfTokens   = make(map[string]time.Time)
	csrfTokensMu sync.RWMutex
)

// withCSRF wraps a handler with CSRF protection
func (s *Server) withCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF for GET/HEAD/OPTIONS
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			// Generate token for forms
			token := generateToken()
			csrfTokensMu.Lock()
			csrfTokens[token] = time.Now().Add(1 * time.Hour)
			csrfTokensMu.Unlock()

			w.Header().Set("X-CSRF-Token", token)
			next(w, r)
			return
		}

		// Validate CSRF token for state-changing requests
		token := r.FormValue("csrf_token")
		if token == "" {
			token = r.Header.Get("X-CSRF-Token")
		}

		if len(token) != 64 {
			http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
			return
		}

		csrfTokensMu.RLock()
		expiry, exists := csrfTokens[token]
		csrfTokensMu.RUnlock()

		if !exists || time.Now().After(expiry) {
			http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
			return
		}

		// Keep token valid for reuse (don't delete on use)
		w.Header().Set("X-CSRF-Token", token)

		next(w, r)
	}
}

// RateLimiter implements login rate limiting
type RateLimiter struct {
	attempts    map[string][]time.Time
	blocked     map[string]time.Time
	mu          sync.RWMutex
	maxAttempts int
	window      time.Duration
	blockTime   time.Duration
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(maxAttempts int, window, blockTime time.Duration) *RateLimiter {
	rl := &RateLimiter{
		attempts:    make(map[string][]time.Time),
		blocked:     make(map[string]time.Time),
		maxAttempts: maxAttempts,
		window:      window,
		blockTime:   blockTime,
	}

	// Start cleanup goroutine
	go rl.cleanup()

	return rl
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()

		// Clean up expired blocks
		for ip, blockedUntil := range rl.blocked {
			if now.After(blockedUntil) {
				delete(rl.blocked, ip)
			}
		}

		// Clean up old attempts
		for ip, attempts := range rl.attempts {
			var valid []time.Time
			for _, t := range attempts {
				if now.Sub(t) < rl.window {
					valid = append(valid, t)
				}
			}
			if len(valid) == 0 {
				delete(rl.attempts, ip)
			} else {
				rl.attempts[ip] = valid
			}
		}

		rl.mu.Unlock()
	}
}

// IsBlocked checks if an IP is currently blocked
func (rl *RateLimiter) IsBlocked(ip string) bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	if blockedUntil, exists := rl.blocked[ip]; exists {
		return time.Now().Before(blockedUntil)
	}
	return false
}

// RecordFailure records a failed login attempt
func (rl *RateLimiter) RecordFailure(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Clean old attempts for this IP
	var valid []time.Time
	for _, t := range rl.attempts[ip] {
		if now.Sub(t) < rl.window {
			valid = append(valid, t)
		}
	}
	valid = append(valid, now)
	rl.attempts[ip] = valid

	// Check if should block
	if len(valid) >= rl.maxAttempts {
		rl.blocked[ip] = now.Add(rl.blockTime)
		delete(rl.attempts, ip)
		return true
	}

	return false
}

// RecordSuccess clears failed attempts for an IP
func (rl *RateLimiter) RecordSuccess(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, ip)
}

// RemainingAttempts returns how many attempts remain before blocking
func (rl *RateLimiter) RemainingAttempts(ip string) int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	now := time.Now()
	var count int
	for _, t := range rl.attempts[ip] {
		if now.Sub(t) < rl.window {
			count++
		}
	}
	return rl.maxAttempts - count
}
