package admin

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/fenilsonani/email-server/internal/audit"
)

// handleAPILogin handles JSON login requests
func (s *Server) handleAPILogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	clientIP := s.rateLimiter.GetClientIP(r)

	// Check if IP is blocked
	if s.rateLimiter.IsBlocked(clientIP) {
		s.jsonError(w, http.StatusTooManyRequests, "Too many failed attempts. Please try again later.")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" {
		s.jsonError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	user, err := s.authenticator.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		blocked := s.rateLimiter.RecordFailure(clientIP)
		remaining := s.rateLimiter.RemainingAttempts(clientIP)

		s.logger.Warn("Failed API login attempt", "ip", clientIP, "username", req.Username, "remaining_attempts", remaining, "blocked", blocked)
		s.auditLogger.Log(r.Context(), req.Username, audit.EventLoginFailure, req.Username, map[string]interface{}{
			"remaining_attempts": remaining,
			"blocked":            blocked,
			"via":                "api",
		}, clientIP)

		s.jsonError(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}

	// Check if user is admin
	var isAdmin bool
	err = s.db.QueryRowContext(r.Context(), "SELECT is_admin FROM users WHERE id = ?", user.ID).Scan(&isAdmin)
	if err != nil || !isAdmin {
		s.rateLimiter.RecordFailure(clientIP)
		s.jsonError(w, http.StatusForbidden, "Admin access required")
		return
	}

	// Check if 2FA is required
	if s.needs2FAVerification(r, user.ID) {
		// Set pending 2FA cookie
		s.setPending2FA(w, r, user.ID, req.Username)
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"needs_2fa": true,
			"username":  req.Username,
		})
		return
	}

	// Success
	s.rateLimiter.RecordSuccess(clientIP)
	token := s.createSession(user.ID)
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   isSecureContext(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})

	s.logger.Info("Admin API login successful", "ip", clientIP, "username", req.Username)
	s.auditLogger.Log(r.Context(), req.Username, audit.EventLoginSuccess, req.Username, map[string]interface{}{"via": "api"}, clientIP)

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"needs_2fa": false,
		"username":  req.Username,
	})
}

// handleAPILogout handles JSON logout requests
func (s *Server) handleAPILogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	cookie, err := r.Cookie("admin_session")
	if err == nil {
		s.deleteSession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		MaxAge:   -1,
	})

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"logged_out": true,
	})
}

// handleAPISession checks the current session status
func (s *Server) handleAPISession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	cookie, err := r.Cookie("admin_session")
	if err != nil {
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
		})
		return
	}

	userID, valid := s.validateSession(cookie.Value)
	if !valid {
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
		})
		return
	}

	// Get username
	var username, domain string
	err = s.db.QueryRowContext(r.Context(),
		`SELECT u.username, d.name FROM users u JOIN domains d ON u.domain_id = d.id WHERE u.id = ?`,
		userID,
	).Scan(&username, &domain)
	if err != nil {
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
		})
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"username":      username,
		"email":         username + "@" + domain,
		"user_id":       userID,
	})
}

// handleAPICSRF returns a CSRF token for the SPA
func (s *Server) handleAPICSRF(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// The CSRF middleware already generates a token on GET requests
	// and sets it in the X-CSRF-Token header
	token := w.Header().Get("X-CSRF-Token")
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"token": token,
	})
}

// handleAPI2FAVerify handles 2FA verification via JSON
func (s *Server) handleAPI2FAVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Check if IP is blocked from too many failed attempts
	clientIP := s.rateLimiter.GetClientIP(r)
	if s.rateLimiter.IsBlocked(clientIP) {
		s.jsonError(w, http.StatusTooManyRequests, "Too many failed attempts. Please try again later.")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get pending 2FA session from cookie
	pendingCookie, err := r.Cookie("pending_2fa")
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, "No pending 2FA session")
		return
	}

	// Decode pending 2FA data (same format as setPending2FA)
	var pending struct {
		UserID   int64  `json:"user_id"`
		Username string `json:"username"`
		Expires  int64  `json:"expires"`
	}
	data, err := base64.URLEncoding.DecodeString(pendingCookie.Value)
	if err != nil {
		s.jsonError(w, http.StatusUnauthorized, "Invalid 2FA session")
		return
	}
	if err := json.Unmarshal(data, &pending); err != nil {
		s.jsonError(w, http.StatusUnauthorized, "Invalid 2FA session")
		return
	}

	// Check if pending session expired
	if time.Now().Unix() > pending.Expires {
		http.SetCookie(w, &http.Cookie{
			Name:   "pending_2fa",
			Value:  "",
			Path:   "/admin",
			MaxAge: -1,
		})
		s.jsonError(w, http.StatusUnauthorized, "2FA session expired")
		return
	}

	// Get user's TOTP secret and verify
	status, err := s.getTwoFactorStatus(pending.UserID)
	if err != nil || !status.Enabled {
		s.jsonError(w, http.StatusUnauthorized, "2FA not configured")
		return
	}

	if !s.validateTOTPCode(status.Secret, req.Code) {
		clientIP = s.rateLimiter.GetClientIP(r)
		s.rateLimiter.RecordFailure(clientIP)
		s.auditLogger.Log(r.Context(), pending.Username, audit.EventLoginFailure, "2FA verification failed", map[string]interface{}{"via": "api"}, clientIP)
		s.jsonError(w, http.StatusUnauthorized, "Invalid 2FA code")
		return
	}

	// Clear pending 2FA cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "pending_2fa",
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		MaxAge:   -1,
	})

	// Create full session
	clientIP = s.rateLimiter.GetClientIP(r)
	s.rateLimiter.RecordSuccess(clientIP)
	token := s.createSession(pending.UserID)
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   isSecureContext(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400,
	})

	s.logger.Info("Admin API 2FA verified", "username", pending.Username)
	s.auditLogger.Log(r.Context(), pending.Username, audit.EventLoginSuccess, pending.Username, map[string]interface{}{"via": "api", "2fa": true}, clientIP)

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"verified": true,
		"username": pending.Username,
	})
}
