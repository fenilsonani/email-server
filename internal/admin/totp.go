package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"net/http"
	"time"

	"github.com/fenilsonani/email-server/internal/audit"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// dbQueryTimeout is the timeout for database queries
const dbQueryTimeout = 5 * time.Second

const (
	totpIssuer          = "MailServer Admin"
	trustedDeviceCookie = "totp_trusted"
	trustedDeviceDays   = 30
)

// TwoFactorStatus represents a user's 2FA status
type TwoFactorStatus struct {
	Enabled   bool
	Secret    string
	CreatedAt time.Time
}

// getTwoFactorStatus gets the 2FA status for a user
func (s *Server) getTwoFactorStatus(userID int64) (*TwoFactorStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	var secret sql.NullString
	var enabled int
	err := s.db.QueryRowContext(ctx,
		"SELECT COALESCE(totp_secret, ''), COALESCE(totp_enabled, 0) FROM users WHERE id = ?",
		userID,
	).Scan(&secret, &enabled)
	if err != nil {
		return nil, err
	}
	return &TwoFactorStatus{
		Enabled: enabled == 1,
		Secret:  secret.String,
	}, nil
}

// generateTOTPSecret generates a new TOTP secret for a user
// SECURITY: Uses SHA256 instead of deprecated SHA1
func (s *Server) generateTOTPSecret(username string) (*otp.Key, error) {
	return totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: username,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA256,
	})
}

// generateQRCodeBase64 generates a QR code image as a base64-encoded PNG
func generateQRCodeBase64(key *otp.Key) (string, error) {
	img, err := key.Image(200, 200)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// validateTOTPCode validates a TOTP code against the user's secret
// SECURITY: Tries SHA256 first (new), then falls back to SHA1 (legacy) for backward compatibility
func (s *Server) validateTOTPCode(secret, code string) bool {
	// Try SHA256 first (for new enrollments)
	valid, err := totp.ValidateCustom(code, secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA256,
	})
	if err == nil && valid {
		return true
	}

	// Fall back to SHA1 for legacy enrollments
	valid, err = totp.ValidateCustom(code, secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return err == nil && valid
}

// generateDeviceToken generates a secure random token for trusted device
func generateDeviceToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// createTrustedDevice creates a trusted device entry
func (s *Server) createTrustedDevice(userID int64, r *http.Request) (string, error) {
	token, err := generateDeviceToken()
	if err != nil {
		return "", err
	}

	expiresAt := time.Now().AddDate(0, 0, trustedDeviceDays)
	deviceName := r.UserAgent()
	if len(deviceName) > 200 {
		deviceName = deviceName[:200]
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbQueryTimeout)
	defer cancel()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO totp_trusted_devices (user_id, device_token, device_name, ip_address, user_agent, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		userID, token, deviceName, getIP(r), r.UserAgent(), expiresAt,
	)
	if err != nil {
		return "", err
	}

	return token, nil
}

// checkTrustedDevice checks if a device token is valid and not expired
func (s *Server) checkTrustedDevice(userID int64, token string) bool {
	if token == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM totp_trusted_devices
		WHERE user_id = ? AND device_token = ? AND expires_at > datetime('now')`,
		userID, token,
	).Scan(&count)

	if err != nil || count == 0 {
		return false
	}

	// Update last used time (non-critical, log errors but don't fail)
	updateCtx, updateCancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer updateCancel()

	if _, err := s.db.ExecContext(updateCtx, `
		UPDATE totp_trusted_devices SET last_used_at = datetime('now')
		WHERE user_id = ? AND device_token = ?`,
		userID, token,
	); err != nil {
		s.logger.Debug("Failed to update trusted device last_used_at", "error", err.Error())
	}

	return true
}

// removeTrustedDevice removes a trusted device
func (s *Server) removeTrustedDevice(userID int64, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		"DELETE FROM totp_trusted_devices WHERE user_id = ? AND device_token = ?",
		userID, token,
	)
	return err
}

// cleanupExpiredTrustedDevices removes expired trusted devices
func (s *Server) cleanupExpiredTrustedDevices() {
	ctx, cancel := context.WithTimeout(context.Background(), dbQueryTimeout)
	defer cancel()

	if _, err := s.db.ExecContext(ctx, "DELETE FROM totp_trusted_devices WHERE expires_at < datetime('now')"); err != nil {
		s.logger.Debug("Failed to cleanup expired trusted devices", "error", err.Error())
	}
}

// getSessionUserID gets the user ID from the current session
func (s *Server) getSessionUserID(r *http.Request) (int64, bool) {
	cookie, err := r.Cookie("admin_session")
	if err != nil {
		return 0, false
	}
	return s.validateSession(cookie.Value)
}

// handle2FASetup handles the 2FA setup page
func (s *Server) handle2FASetup(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	// Get username for this user
	ctx, cancel := context.WithTimeout(r.Context(), dbQueryTimeout)
	defer cancel()

	var username string
	err := s.db.QueryRowContext(ctx, "SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	if err != nil {
		s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
			"Title": "Two-Factor Setup",
			"Error": "Failed to get user information",
		})
		return
	}

	status, err := s.getTwoFactorStatus(userID)
	if err != nil {
		s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
			"Title": "Two-Factor Setup",
			"Error": "Failed to get 2FA status",
		})
		return
	}

	if r.Method == http.MethodGet {
		if status.Enabled {
			// Show disable option
			s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
				"Title":     "Two-Factor Authentication",
				"Enabled":   true,
				"CSRFToken": w.Header().Get("X-CSRF-Token"),
			})
			return
		}

		// Generate new secret
		key, err := s.generateTOTPSecret(username)
		if err != nil {
			s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
				"Title": "Two-Factor Setup",
				"Error": "Failed to generate 2FA secret",
			})
			return
		}

		// Generate QR code as base64
		qrCodeBase64, err := generateQRCodeBase64(key)
		if err != nil {
			s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
				"Title": "Two-Factor Setup",
				"Error": "Failed to generate QR code",
			})
			return
		}

		// Store secret temporarily (not enabled yet)
		storeCtx, storeCancel := context.WithTimeout(r.Context(), dbQueryTimeout)
		_, err = s.db.ExecContext(storeCtx,
			"UPDATE users SET totp_secret = ? WHERE id = ?",
			key.Secret(), userID,
		)
		storeCancel()
		if err != nil {
			s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
				"Title": "Two-Factor Setup",
				"Error": "Failed to save 2FA secret",
			})
			return
		}

		s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
			"Title":       "Two-Factor Setup",
			"Enabled":     false,
			"Secret":      key.Secret(),
			"QRCode":      qrCodeBase64,
			"AccountName": username,
			"CSRFToken":   w.Header().Get("X-CSRF-Token"),
		})
		return
	}

	// POST - enable or disable 2FA
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	action := r.FormValue("action")
	code := r.FormValue("code")

	if action == "disable" {
		// Verify code before disabling
		if !s.validateTOTPCode(status.Secret, code) {
			s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
				"Title":     "Two-Factor Authentication",
				"Enabled":   true,
				"Error":     "Invalid verification code",
				"CSRFToken": w.Header().Get("X-CSRF-Token"),
			})
			return
		}

		// Disable 2FA
		disableCtx, disableCancel := context.WithTimeout(r.Context(), dbQueryTimeout)
		_, err = s.db.ExecContext(disableCtx,
			"UPDATE users SET totp_enabled = 0, totp_secret = NULL WHERE id = ?",
			userID,
		)
		disableCancel()
		if err != nil {
			s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
				"Title":     "Two-Factor Authentication",
				"Enabled":   true,
				"Error":     "Failed to disable 2FA",
				"CSRFToken": w.Header().Get("X-CSRF-Token"),
			})
			return
		}

		// Remove all trusted devices
		cleanupCtx, cleanupCancel := context.WithTimeout(r.Context(), dbQueryTimeout)
		s.db.ExecContext(cleanupCtx, "DELETE FROM totp_trusted_devices WHERE user_id = ?", userID)
		cleanupCancel()

		s.auditLogger.Log(r.Context(), username, audit.EventConfigChange, "2FA disabled", nil, getIP(r))
		http.Redirect(w, r, "/admin/2fa/setup?disabled=1", http.StatusSeeOther)
		return
	}

	// Enable 2FA - verify code first
	secretCtx, secretCancel := context.WithTimeout(r.Context(), dbQueryTimeout)
	var secret string
	err = s.db.QueryRowContext(secretCtx, "SELECT totp_secret FROM users WHERE id = ?", userID).Scan(&secret)
	secretCancel()
	if err != nil || secret == "" {
		s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
			"Title": "Two-Factor Setup",
			"Error": "Please refresh and try again",
		})
		return
	}

	if !s.validateTOTPCode(secret, code) {
		// Generate QR code from existing secret
		key, err := otp.NewKeyFromURL("otpauth://totp/" + totpIssuer + ":" + username + "?secret=" + secret + "&issuer=" + totpIssuer)
		var qrCodeBase64 string
		if err == nil {
			qrCodeBase64, _ = generateQRCodeBase64(key)
		}
		s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
			"Title":       "Two-Factor Setup",
			"Enabled":     false,
			"Secret":      secret,
			"QRCode":      qrCodeBase64,
			"AccountName": username,
			"Error":       "Invalid verification code. Please try again.",
			"CSRFToken":   w.Header().Get("X-CSRF-Token"),
		})
		return
	}

	// Enable 2FA
	enableCtx, enableCancel := context.WithTimeout(r.Context(), dbQueryTimeout)
	_, err = s.db.ExecContext(enableCtx, "UPDATE users SET totp_enabled = 1 WHERE id = ?", userID)
	enableCancel()
	if err != nil {
		s.renderTemplate(w, "2fa_setup.html", map[string]interface{}{
			"Title": "Two-Factor Setup",
			"Error": "Failed to enable 2FA",
		})
		return
	}

	// Create trusted device for current session
	token, err := s.createTrustedDevice(userID, r)
	if err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     trustedDeviceCookie,
			Value:    token,
			Path:     "/admin",
			HttpOnly: true,
			Secure:   isSecureContext(r),
			SameSite: http.SameSiteStrictMode,
			MaxAge:   trustedDeviceDays * 24 * 60 * 60,
		})
	}

	s.auditLogger.Log(r.Context(), username, audit.EventConfigChange, "2FA enabled", nil, getIP(r))
	http.Redirect(w, r, "/admin/2fa/setup?enabled=1", http.StatusSeeOther)
}

// handle2FAVerify handles the 2FA verification page (during login)
func (s *Server) handle2FAVerify(w http.ResponseWriter, r *http.Request) {
	// Get pending 2FA session from cookie
	cookie, err := r.Cookie("pending_2fa")
	if err != nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	var pending struct {
		UserID   int64  `json:"user_id"`
		Username string `json:"username"`
		Expires  int64  `json:"expires"`
	}
	data, err := base64.URLEncoding.DecodeString(cookie.Value)
	if err != nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	if err := json.Unmarshal(data, &pending); err != nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	// Check if pending session expired (5 minutes)
	if time.Now().Unix() > pending.Expires {
		http.SetCookie(w, &http.Cookie{
			Name:   "pending_2fa",
			Value:  "",
			Path:   "/admin",
			MaxAge: -1,
		})
		http.Redirect(w, r, "/admin/login?expired=1", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodGet {
		s.renderTemplate(w, "2fa_verify.html", map[string]interface{}{
			"Title":     "Two-Factor Verification",
			"Username":  pending.Username,
			"CSRFToken": w.Header().Get("X-CSRF-Token"),
		})
		return
	}

	// POST - verify code
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	code := r.FormValue("code")
	remember := r.FormValue("remember") == "on"

	// Get user's TOTP secret
	status, err := s.getTwoFactorStatus(pending.UserID)
	if err != nil || !status.Enabled {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	clientIP := s.rateLimiter.GetClientIP(r)

	if !s.validateTOTPCode(status.Secret, code) {
		s.rateLimiter.RecordFailure(clientIP)
		s.auditLogger.Log(r.Context(), pending.Username, audit.EventLoginFailure, "2FA verification failed", nil, clientIP)

		s.renderTemplate(w, "2fa_verify.html", map[string]interface{}{
			"Title":     "Two-Factor Verification",
			"Username":  pending.Username,
			"Error":     "Invalid verification code",
			"CSRFToken": w.Header().Get("X-CSRF-Token"),
		})
		return
	}

	// Success - clear pending 2FA cookie
	http.SetCookie(w, &http.Cookie{
		Name:   "pending_2fa",
		Value:  "",
		Path:   "/admin",
		MaxAge: -1,
	})

	// Create trusted device if "remember" is checked
	if remember {
		token, err := s.createTrustedDevice(pending.UserID, r)
		if err == nil {
			http.SetCookie(w, &http.Cookie{
				Name:     trustedDeviceCookie,
				Value:    token,
				Path:     "/admin",
				HttpOnly: true,
				Secure:   isSecureContext(r),
				SameSite: http.SameSiteStrictMode,
				MaxAge:   trustedDeviceDays * 24 * 60 * 60,
			})
		}
	}

	// Create session
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

	s.rateLimiter.RecordSuccess(clientIP)
	s.logger.Info("Admin login successful (2FA)", "ip", clientIP, "username", pending.Username)
	s.auditLogger.Log(r.Context(), pending.Username, audit.EventLoginSuccess, "2FA verified", nil, clientIP)

	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

// setPending2FA sets a pending 2FA verification cookie
func (s *Server) setPending2FA(w http.ResponseWriter, r *http.Request, userID int64, username string) {
	pending := struct {
		UserID   int64  `json:"user_id"`
		Username string `json:"username"`
		Expires  int64  `json:"expires"`
	}{
		UserID:   userID,
		Username: username,
		Expires:  time.Now().Add(5 * time.Minute).Unix(),
	}

	data, _ := json.Marshal(pending)
	encoded := base64.URLEncoding.EncodeToString(data)

	http.SetCookie(w, &http.Cookie{
		Name:     "pending_2fa",
		Value:    encoded,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   isSecureContext(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   300, // 5 minutes
	})
}

// needs2FAVerification checks if a user needs to verify 2FA
func (s *Server) needs2FAVerification(r *http.Request, userID int64) bool {
	status, err := s.getTwoFactorStatus(userID)
	if err != nil || !status.Enabled {
		return false
	}

	// Check for trusted device cookie
	cookie, err := r.Cookie(trustedDeviceCookie)
	if err != nil {
		return true
	}

	return !s.checkTrustedDevice(userID, cookie.Value)
}
