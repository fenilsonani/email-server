package userportal

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/audit"
)

// UserInfo holds user information for display
type UserInfo struct {
	ID          int64
	Email       string
	DisplayName string
	QuotaBytes  int64
	UsedBytes   int64
	IsActive    bool
}

// handleLogin handles user login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Account Login",
		})
		return
	}

	// POST - handle login
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	clientIP := getClientIP(r)

	// Check rate limiting
	if s.rateLimiter.IsBlocked(clientIP) {
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Account Login",
			"Error": "Too many failed attempts. Please try again later.",
		})
		return
	}

	// Authenticate user
	user, err := s.authenticator.Authenticate(r.Context(), email, password)
	if err != nil {
		blocked := s.rateLimiter.RecordFailure(clientIP)
		remaining := s.rateLimiter.RemainingAttempts(clientIP)

		s.auditLogger.Log(r.Context(), email, audit.EventLoginFailure, email, map[string]interface{}{
			"portal":             "user",
			"remaining_attempts": remaining,
		}, clientIP)

		errorMsg := "Invalid email or password"
		if remaining > 0 && remaining < 3 {
			errorMsg = "Invalid credentials. " + string(rune('0'+remaining)) + " attempts remaining"
		} else if blocked {
			errorMsg = "Too many failed attempts. Account temporarily locked."
		}

		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Account Login",
			"Error": errorMsg,
			"Email": email,
		})
		return
	}

	// Create session
	token, err := s.createSession(r.Context(), user.ID, r)
	if err != nil {
		s.logger.Error("Failed to create session", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	s.rateLimiter.RecordSuccess(clientIP)
	setSessionCookie(w, token)

	s.auditLogger.Log(r.Context(), email, audit.EventLoginSuccess, email, map[string]interface{}{
		"portal": "user",
	}, clientIP)

	http.Redirect(w, r, "/account/", http.StatusSeeOther)
}

// handleLogout handles user logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.deleteSession(cookie.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/account/login", http.StatusSeeOther)
}

// handleDashboard shows the user dashboard
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/account/" {
		http.Redirect(w, r, "/account/", http.StatusSeeOther)
		return
	}

	userID := getUserID(r)
	user, err := s.getUserInfo(r.Context(), userID)
	if err != nil {
		s.logger.Error("Failed to get user info", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get forwarding status
	var forwardingActive bool
	s.db.QueryRowContext(r.Context(), "SELECT is_active FROM user_forwarding WHERE user_id = ?", userID).Scan(&forwardingActive)

	// Get vacation status
	var vacationActive bool
	s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) > 0 FROM sieve_scripts
		WHERE user_id = ? AND name = 'vacation' AND is_active = TRUE
	`, userID).Scan(&vacationActive)

	s.renderTemplate(w, "dashboard.html", map[string]interface{}{
		"Title":            "My Account",
		"User":             user,
		"ForwardingActive": forwardingActive,
		"VacationActive":   vacationActive,
	})
}

// handlePassword handles password change
func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	user, err := s.getUserInfo(r.Context(), userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodGet {
		s.renderTemplate(w, "password.html", map[string]interface{}{
			"Title": "Change Password",
			"User":  user,
		})
		return
	}

	// POST - handle password change
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	currentPassword := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")
	confirmPassword := r.FormValue("confirm_password")

	// Validate new passwords match
	if newPassword != confirmPassword {
		s.renderTemplate(w, "password.html", map[string]interface{}{
			"Title": "Change Password",
			"User":  user,
			"Error": "New passwords do not match",
		})
		return
	}

	// Validate new password length
	if len(newPassword) < 8 || len(newPassword) > 128 {
		s.renderTemplate(w, "password.html", map[string]interface{}{
			"Title": "Change Password",
			"User":  user,
			"Error": "Password must be 8-128 characters",
		})
		return
	}

	// Verify current password
	_, err = s.authenticator.Authenticate(r.Context(), user.Email, currentPassword)
	if err != nil {
		s.renderTemplate(w, "password.html", map[string]interface{}{
			"Title": "Change Password",
			"User":  user,
			"Error": "Current password is incorrect",
		})
		return
	}

	// Update password
	if err := s.authenticator.UpdatePassword(r.Context(), userID, newPassword); err != nil {
		s.logger.Error("Failed to update password", "error", err)
		s.renderTemplate(w, "password.html", map[string]interface{}{
			"Title": "Change Password",
			"User":  user,
			"Error": "Failed to update password. Please try again.",
		})
		return
	}

	// Invalidate all sessions for this user
	s.invalidateUserSessions(userID)

	// Audit log
	clientIP := getClientIP(r)
	s.auditLogger.Log(r.Context(), user.Email, audit.EventPasswordChange, user.Email, map[string]interface{}{
		"portal":    "user",
		"self_service": true,
	}, clientIP)

	// Redirect to login with success message
	http.Redirect(w, r, "/account/login?msg=password_changed", http.StatusSeeOther)
}

// handleProfile handles profile editing
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	user, err := s.getUserInfo(r.Context(), userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodGet {
		s.renderTemplate(w, "profile.html", map[string]interface{}{
			"Title": "Edit Profile",
			"User":  user,
		})
		return
	}

	// POST - update profile
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	displayName := strings.TrimSpace(r.FormValue("display_name"))
	if len(displayName) > 255 {
		displayName = displayName[:255]
	}

	_, err = s.db.ExecContext(r.Context(), `
		UPDATE users SET display_name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, displayName, userID)

	if err != nil {
		s.logger.Error("Failed to update profile", "error", err)
		s.renderTemplate(w, "profile.html", map[string]interface{}{
			"Title": "Edit Profile",
			"User":  user,
			"Error": "Failed to update profile",
		})
		return
	}

	user.DisplayName = displayName
	s.renderTemplate(w, "profile.html", map[string]interface{}{
		"Title":   "Edit Profile",
		"User":    user,
		"Success": "Profile updated successfully",
	})
}

// handleForwarding handles email forwarding settings
func (s *Server) handleForwarding(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	user, err := s.getUserInfo(r.Context(), userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get current forwarding settings
	var forwardTo sql.NullString
	var keepCopy, isActive bool
	err = s.db.QueryRowContext(r.Context(), `
		SELECT forward_to, keep_copy, is_active FROM user_forwarding WHERE user_id = ?
	`, userID).Scan(&forwardTo, &keepCopy, &isActive)
	if err == sql.ErrNoRows {
		keepCopy = true // Default
	}

	if r.Method == http.MethodGet {
		s.renderTemplate(w, "forwarding.html", map[string]interface{}{
			"Title":     "Email Forwarding",
			"User":      user,
			"ForwardTo": forwardTo.String,
			"KeepCopy":  keepCopy,
			"IsActive":  isActive,
		})
		return
	}

	// POST - update forwarding
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	newForwardTo := strings.TrimSpace(r.FormValue("forward_to"))
	newKeepCopy := r.FormValue("keep_copy") == "on"
	newIsActive := r.FormValue("is_active") == "on"

	// Validate email if forwarding is active
	if newIsActive && newForwardTo == "" {
		s.renderTemplate(w, "forwarding.html", map[string]interface{}{
			"Title":     "Email Forwarding",
			"User":      user,
			"ForwardTo": newForwardTo,
			"KeepCopy":  newKeepCopy,
			"IsActive":  newIsActive,
			"Error":     "Please enter a forwarding address",
		})
		return
	}

	// Upsert forwarding settings
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO user_forwarding (user_id, forward_to, keep_copy, is_active, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			forward_to = excluded.forward_to,
			keep_copy = excluded.keep_copy,
			is_active = excluded.is_active,
			updated_at = CURRENT_TIMESTAMP
	`, userID, newForwardTo, newKeepCopy, newIsActive)

	if err != nil {
		s.logger.Error("Failed to update forwarding", "error", err)
		s.renderTemplate(w, "forwarding.html", map[string]interface{}{
			"Title":     "Email Forwarding",
			"User":      user,
			"ForwardTo": newForwardTo,
			"KeepCopy":  newKeepCopy,
			"IsActive":  newIsActive,
			"Error":     "Failed to update forwarding settings",
		})
		return
	}

	s.renderTemplate(w, "forwarding.html", map[string]interface{}{
		"Title":     "Email Forwarding",
		"User":      user,
		"ForwardTo": newForwardTo,
		"KeepCopy":  newKeepCopy,
		"IsActive":  newIsActive,
		"Success":   "Forwarding settings updated",
	})
}

// handleVacation handles vacation responder settings
func (s *Server) handleVacation(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	user, err := s.getUserInfo(r.Context(), userID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get current vacation settings from vacation_responses table
	var subject, message string
	var startDate, endDate sql.NullTime
	var isActive bool

	err = s.db.QueryRowContext(r.Context(), `
		SELECT subject, message, start_date, end_date, is_active
		FROM vacation_responses WHERE user_id = ?
	`, userID).Scan(&subject, &message, &startDate, &endDate, &isActive)
	if err == sql.ErrNoRows {
		// No existing vacation settings
	}

	if r.Method == http.MethodGet {
		s.renderTemplate(w, "vacation.html", map[string]interface{}{
			"Title":     "Vacation Responder",
			"User":      user,
			"Subject":   subject,
			"Message":   message,
			"StartDate": formatDate(startDate),
			"EndDate":   formatDate(endDate),
			"IsActive":  isActive,
		})
		return
	}

	// POST - update vacation settings
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	newSubject := strings.TrimSpace(r.FormValue("subject"))
	newMessage := strings.TrimSpace(r.FormValue("message"))
	newStartDateStr := r.FormValue("start_date")
	newEndDateStr := r.FormValue("end_date")
	newIsActive := r.FormValue("is_active") == "on"

	// Parse dates
	var newStartDate, newEndDate sql.NullTime
	if newStartDateStr != "" {
		if t, err := time.Parse("2006-01-02", newStartDateStr); err == nil {
			newStartDate = sql.NullTime{Time: t, Valid: true}
		}
	}
	if newEndDateStr != "" {
		if t, err := time.Parse("2006-01-02", newEndDateStr); err == nil {
			newEndDate = sql.NullTime{Time: t, Valid: true}
		}
	}

	// Validate if active
	if newIsActive && (newSubject == "" || newMessage == "") {
		s.renderTemplate(w, "vacation.html", map[string]interface{}{
			"Title":     "Vacation Responder",
			"User":      user,
			"Subject":   newSubject,
			"Message":   newMessage,
			"StartDate": newStartDateStr,
			"EndDate":   newEndDateStr,
			"IsActive":  newIsActive,
			"Error":     "Subject and message are required when vacation responder is active",
		})
		return
	}

	// Upsert vacation settings
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO vacation_responses (user_id, subject, message, start_date, end_date, is_active)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			subject = excluded.subject,
			message = excluded.message,
			start_date = excluded.start_date,
			end_date = excluded.end_date,
			is_active = excluded.is_active
	`, userID, newSubject, newMessage, newStartDate, newEndDate, newIsActive)

	if err != nil {
		s.logger.Error("Failed to update vacation", "error", err)
		s.renderTemplate(w, "vacation.html", map[string]interface{}{
			"Title":     "Vacation Responder",
			"User":      user,
			"Subject":   newSubject,
			"Message":   newMessage,
			"StartDate": newStartDateStr,
			"EndDate":   newEndDateStr,
			"IsActive":  newIsActive,
			"Error":     "Failed to update vacation settings",
		})
		return
	}

	s.renderTemplate(w, "vacation.html", map[string]interface{}{
		"Title":     "Vacation Responder",
		"User":      user,
		"Subject":   newSubject,
		"Message":   newMessage,
		"StartDate": newStartDateStr,
		"EndDate":   newEndDateStr,
		"IsActive":  newIsActive,
		"Success":   "Vacation responder updated",
	})
}

// getUserInfo retrieves user information by ID
func (s *Server) getUserInfo(ctx context.Context, userID int64) (*UserInfo, error) {
	var user UserInfo
	var domainName string

	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, d.name, u.display_name, u.quota_bytes, u.used_bytes, u.is_active
		FROM users u
		JOIN domains d ON u.domain_id = d.id
		WHERE u.id = ?
	`, userID).Scan(&user.ID, &user.Email, &domainName, &user.DisplayName, &user.QuotaBytes, &user.UsedBytes, &user.IsActive)

	if err != nil {
		return nil, err
	}

	user.Email = user.Email + "@" + domainName
	return &user, nil
}

func formatDate(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format("2006-01-02")
}
