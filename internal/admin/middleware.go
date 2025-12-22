package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
)

// Session represents an admin session
type session struct {
	userID    int64
	createdAt time.Time
	expiresAt time.Time
}

var (
	sessions   = make(map[string]*session)
	sessionsMu sync.RWMutex
)

// createSession creates a new session and returns the token
func (s *Server) createSession(userID int64) string {
	token := generateToken()

	sessionsMu.Lock()
	sessions[token] = &session{
		userID:    userID,
		createdAt: time.Now(),
		expiresAt: time.Now().Add(7 * 24 * time.Hour), // 7 days session
	}
	sessionsMu.Unlock()

	return token
}

// validateSession checks if a session token is valid
func (s *Server) validateSession(token string) (int64, bool) {
	// Validate token format: must be valid hex and minimum length
	if !isValidToken(token) {
		return 0, false
	}

	sessionsMu.RLock()
	sess, exists := sessions[token]
	sessionsMu.RUnlock()

	if !exists {
		return 0, false
	}

	// Check expiration with proper locking
	now := time.Now()
	if now.After(sess.expiresAt) {
		sessionsMu.Lock()
		// Double-check after acquiring write lock (may have been deleted)
		if s, ok := sessions[token]; ok && now.After(s.expiresAt) {
			delete(sessions, token)
		}
		sessionsMu.Unlock()
		return 0, false
	}

	return sess.userID, true
}

// withAuth wraps a handler with authentication check
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		// Check user is still admin
		var isAdmin bool
		err = s.db.QueryRowContext(r.Context(), "SELECT is_admin FROM users WHERE id = ?", userID).Scan(&isAdmin)
		if err != nil || !isAdmin {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		next(w, r)
	}
}

// CSRF token handling
var (
	csrfTokens   = make(map[string]time.Time)
	csrfTokensMu sync.RWMutex
)

// withCSRF wraps a handler with CSRF protection
func (s *Server) withCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF for GET/HEAD/OPTIONS
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			// Generate token for forms
			token := generateToken()
			csrfTokensMu.Lock()
			csrfTokens[token] = time.Now().Add(1 * time.Hour)
			csrfTokensMu.Unlock()

			w.Header().Set("X-CSRF-Token", token)
			next.ServeHTTP(w, r)
			return
		}

		// Validate CSRF token for state-changing requests
		token := r.FormValue("csrf_token")
		if token == "" {
			token = r.Header.Get("X-CSRF-Token")
		}

		// Validate token format
		if !isValidToken(token) {
			http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
			return
		}

		csrfTokensMu.RLock()
		expiry, exists := csrfTokens[token]
		csrfTokensMu.RUnlock()

		now := time.Now()
		if !exists || now.After(expiry) {
			http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
			return
		}

		// Remove used token with proper locking
		csrfTokensMu.Lock()
		// Double-check it still exists
		if exp, ok := csrfTokens[token]; ok && !now.After(exp) {
			delete(csrfTokens, token)
		}
		csrfTokensMu.Unlock()

		// Generate new token for response
		newToken := generateToken()
		csrfTokensMu.Lock()
		csrfTokens[newToken] = time.Now().Add(1 * time.Hour)
		csrfTokensMu.Unlock()
		w.Header().Set("X-CSRF-Token", newToken)

		next.ServeHTTP(w, r)
	})
}

// generateToken generates a cryptographically secure token
func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(b)
}

// CleanupExpiredSessions removes expired sessions periodically
func CleanupExpiredSessions() {
	ticker := time.NewTicker(15 * time.Minute)
	go func() {
		for range ticker.C {
			now := time.Now()

			// Clean sessions
			sessionsMu.Lock()
			for token, sess := range sessions {
				if now.After(sess.expiresAt) {
					delete(sessions, token)
				}
			}
			sessionsMu.Unlock()

			// Clean CSRF tokens
			csrfTokensMu.Lock()
			for token, expiry := range csrfTokens {
				if now.After(expiry) {
					delete(csrfTokens, token)
				}
			}
			csrfTokensMu.Unlock()
		}
	}()
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
				// Log the panic with stack trace
				stack := debug.Stack()
				s.logger.Error(
					"Panic recovered in HTTP handler",
					"error", fmt.Sprintf("%v", err),
					"path", r.URL.Path,
					"method", r.Method,
					"remote_addr", r.RemoteAddr,
					"stack", string(stack),
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
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// XSS protection (legacy, but still useful for older browsers)
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Referrer policy - don't leak URLs to external sites
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy - restrict resource loading
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline'; "+ // Allow inline scripts for simple UI
				"style-src 'self' 'unsafe-inline'; "+ // Allow inline styles
				"img-src 'self' data:; "+
				"font-src 'self'; "+
				"form-action 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'")

		// Permissions Policy - disable unnecessary browser features
		w.Header().Set("Permissions-Policy",
			"accelerometer=(), camera=(), geolocation=(), gyroscope=(), "+
				"magnetometer=(), microphone=(), payment=(), usb=()")

		// Cache control for admin pages - don't cache sensitive data
		if r.URL.Path != "/admin/login" {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
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
