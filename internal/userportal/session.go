package userportal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
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

	ipAddress := s.getClientIP(r)
	userAgent := normalizeUserAgent(r.UserAgent())

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
func (s *Server) validateSession(r *http.Request, token string) (int64, bool) {
	var userID int64
	var expiresAt time.Time
	var ipAddress, userAgent string

	err := s.db.QueryRowContext(r.Context(), `
		SELECT user_id, expires_at, COALESCE(ip_address, ''), COALESCE(user_agent, '')
		FROM user_sessions WHERE token = ?
	`, token).Scan(&userID, &expiresAt, &ipAddress, &userAgent)

	if err != nil {
		return 0, false
	}

	if time.Now().After(expiresAt) {
		// Session expired, clean it up
		s.db.Exec(`DELETE FROM user_sessions WHERE token = ?`, token)
		return 0, false
	}

	currentIP := s.getClientIP(r)
	if !sessionIPMatches(ipAddress, currentIP) {
		s.db.Exec(`DELETE FROM user_sessions WHERE token = ?`, token)
		return 0, false
	}

	currentUserAgent := normalizeUserAgent(r.UserAgent())
	if userAgent != "" && currentUserAgent != "" && userAgent != currentUserAgent {
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
// SECURITY: Panics if crypto/rand fails to ensure we never generate weak tokens
func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Panic is appropriate here - if crypto/rand fails, the system
		// is in a critical state and should not continue generating tokens
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// getClientIP extracts the client IP from the request.
// Proxy headers are only trusted when the direct peer is a trusted proxy.
func (s *Server) getClientIP(r *http.Request) string {
	directIP := normalizeIP(r.RemoteAddr)

	if s.trustedProxies[directIP] {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ips := strings.Split(xff, ",")
			if len(ips) > 0 {
				clientIP := normalizeIP(strings.TrimSpace(ips[0]))
				if clientIP != "" {
					return clientIP
				}
			}
		}

		if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
			clientIP := normalizeIP(strings.TrimSpace(xrip))
			if clientIP != "" {
				return clientIP
			}
		}
	}

	return directIP
}

func sessionIPMatches(storedIP, currentIP string) bool {
	if storedIP == "" || currentIP == "" {
		return true
	}

	if storedIP == currentIP {
		return true
	}

	return normalizeIP(storedIP) == currentIP
}

func normalizeIP(value string) string {
	if value == "" {
		return ""
	}

	if ip := net.ParseIP(value); ip != nil {
		return ip.String()
	}

	host, _, err := net.SplitHostPort(value)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil {
			return ip.String()
		}
		return ""
	}

	return ""
}

func normalizeUserAgent(userAgent string) string {
	if len(userAgent) > 255 {
		return userAgent[:255]
	}
	return userAgent
}
