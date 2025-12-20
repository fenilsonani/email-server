package admin

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
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
		expiresAt: time.Now().Add(24 * time.Hour),
	}
	sessionsMu.Unlock()

	return token
}

// validateSession checks if a session token is valid
func (s *Server) validateSession(token string) (int64, bool) {
	sessionsMu.RLock()
	sess, exists := sessions[token]
	sessionsMu.RUnlock()

	if !exists {
		return 0, false
	}

	if time.Now().After(sess.expiresAt) {
		sessionsMu.Lock()
		delete(sessions, token)
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

		csrfTokensMu.RLock()
		expiry, exists := csrfTokens[token]
		csrfTokensMu.RUnlock()

		if !exists || time.Now().After(expiry) {
			http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
			return
		}

		// Remove used token
		csrfTokensMu.Lock()
		delete(csrfTokens, token)
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
