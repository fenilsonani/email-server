package userportal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

const (
	sessionCookieName = "account_session"
	sessionDuration   = 8 * time.Hour
)

// createSession creates a new session for the user
func (s *Server) createSession(ctx context.Context, userID int64, r *http.Request) (string, error) {
	token := generateToken()
	expiresAt := time.Now().Add(sessionDuration)

	ipAddress := getClientIP(r)
	userAgent := r.UserAgent()
	if len(userAgent) > 255 {
		userAgent = userAgent[:255]
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_sessions (token, user_id, expires_at, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?)
	`, token, userID, expiresAt, ipAddress, userAgent)

	if err != nil {
		return "", err
	}

	return token, nil
}

// validateSession checks if a session token is valid and returns the user ID
func (s *Server) validateSession(token string) (int64, bool) {
	var userID int64
	var expiresAt time.Time

	err := s.db.QueryRow(`
		SELECT user_id, expires_at FROM user_sessions WHERE token = ?
	`, token).Scan(&userID, &expiresAt)

	if err != nil {
		return 0, false
	}

	if time.Now().After(expiresAt) {
		// Session expired, clean it up
		s.db.Exec(`DELETE FROM user_sessions WHERE token = ?`, token)
		return 0, false
	}

	return userID, true
}

// deleteSession removes a session
func (s *Server) deleteSession(token string) {
	s.db.Exec(`DELETE FROM user_sessions WHERE token = ?`, token)
}

// invalidateUserSessions removes all sessions for a user
func (s *Server) invalidateUserSessions(userID int64) {
	s.db.Exec(`DELETE FROM user_sessions WHERE user_id = ?`, userID)
}

// cleanExpiredSessions removes expired sessions
func (s *Server) cleanExpiredSessions() {
	s.db.Exec(`DELETE FROM user_sessions WHERE expires_at < ?`, time.Now())
}

// setSessionCookie sets the session cookie on the response
func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/account",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
}

// clearSessionCookie removes the session cookie
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/account",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// generateToken creates a cryptographically secure random token
func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxied requests)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain
		if idx := len(xff); idx > 0 {
			for i, c := range xff {
				if c == ',' {
					return xff[:i]
				}
			}
			return xff
		}
	}

	// Check X-Real-IP header
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return xrip
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}
