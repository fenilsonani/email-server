package admin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fenilsonani/email-server/internal/audit"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/safecast"
	"github.com/fenilsonani/email-server/internal/security"
	"github.com/fenilsonani/email-server/internal/validation"
)

// PaginationParams holds pagination parameters from request
type PaginationParams struct {
	Page     int
	PageSize int
	Offset   int
}

// getPaginationParams extracts and validates pagination parameters from request
func getPaginationParams(r *http.Request) PaginationParams {
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	pageSize := 50
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 && parsed <= 200 {
			pageSize = parsed
		}
	}

	return PaginationParams{
		Page:     page,
		PageSize: pageSize,
		Offset:   (page - 1) * pageSize,
	}
}

// buildDateFilter creates a WHERE clause for date range filtering.
// Returns the clause string (including leading " WHERE") and the args.
func buildDateFilter(column, from, to string) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	if from != "" {
		conditions = append(conditions, column+" >= ?")
		args = append(args, from)
	}
	if to != "" {
		conditions = append(conditions, column+" <= ?")
		args = append(args, to)
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

// handleDashboard shows the main dashboard
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/admin/" {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}

	stats, err := s.getStats(r.Context())
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get stats", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Title": "Dashboard",
		"Stats": stats,
	}

	s.renderTemplate(w, "dashboard.html", data)
}

// handleLogin handles admin login with rate limiting
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	clientIP := s.rateLimiter.GetClientIP(r)

	// Check if IP is blocked
	if s.rateLimiter.IsBlocked(clientIP) {
		blockedUntil := s.rateLimiter.BlockedUntil(clientIP)
		remaining := time.Until(blockedUntil).Round(time.Minute)
		s.logger.Warn("Blocked login attempt", "ip", clientIP, "blocked_for", remaining.String())
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Admin Login",
			"Error": "Too many failed attempts. Please try again in " + remaining.String(),
		})
		return
	}

	if r.Method == http.MethodGet {
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Admin Login",
		})
		return
	}

	// POST - handle login
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Bad request", formErrorStatus(err))
		return
	}

	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")

	user, err := s.authenticator.Authenticate(r.Context(), username, password)
	if err != nil {
		// Record failed attempt
		blocked := s.rateLimiter.RecordFailure(clientIP)
		remaining := s.rateLimiter.RemainingAttempts(clientIP)

		s.logger.Warn("Failed login attempt",
			"ip", clientIP,
			"username", username,
			"remaining_attempts", remaining,
			"blocked", blocked)

		// Audit log failed login
		s.auditLogger.Log(r.Context(), username, audit.EventLoginFailure, username, map[string]interface{}{
			"remaining_attempts": remaining,
			"blocked":            blocked,
		}, clientIP)

		errorMsg := "Invalid credentials"
		if remaining > 0 && remaining < 3 {
			errorMsg = "Invalid credentials. " + strconv.Itoa(remaining) + " attempts remaining"
		} else if blocked {
			errorMsg = "Too many failed attempts. Account temporarily locked"
		}

		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Admin Login",
			"Error": errorMsg,
		})
		return
	}

	// Check if user has admin role (via roles table or legacy is_admin flag)
	adminUser, loadErr := s.loadAdminUser(r.Context(), user.ID)
	if loadErr != nil || adminUser == nil {
		s.rateLimiter.RecordFailure(clientIP)
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Admin Login",
			"Error": "Access denied - admin rights required",
		})
		return
	}

	// Check if 2FA is required
	if s.needs2FAVerification(r, user.ID) {
		// Set pending 2FA session and redirect to verification
		s.setPending2FA(w, r, user.ID, username)
		http.Redirect(w, r, "/admin/2fa/verify", http.StatusSeeOther)
		return
	}

	// Success - clear rate limit for this IP
	s.rateLimiter.RecordSuccess(clientIP)

	// Create session
	token := s.createSession(user.ID)
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   isSecureContext(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24 hours - reduced from 7 days for security
	})

	s.logger.Info("Admin login successful", "ip", clientIP, "username", username)

	// Audit log successful login
	s.auditLogger.Log(r.Context(), username, audit.EventLoginSuccess, username, nil, clientIP)

	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

// handleLogout handles admin logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// handleUsers shows user list
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	adminUser := GetAdminUser(r)

	// Get pagination parameters
	pagination := getPaginationParams(r)

	// Get filter parameters
	filterUsername := r.URL.Query().Get("username")
	filterDomain := r.URL.Query().Get("domain")
	filterRole := r.URL.Query().Get("role")

	// Build query with filters
	query := `SELECT u.id, u.username, d.name as domain, u.is_admin, u.created_at,
		COALESCE((SELECT r.name FROM user_roles ur JOIN roles r ON ur.role_id = r.id WHERE ur.user_id = u.id LIMIT 1), '') as role_name
		FROM users u
		JOIN domains d ON u.domain_id = d.id WHERE 1=1`
	countQuery := `SELECT COUNT(*) FROM users u
		JOIN domains d ON u.domain_id = d.id WHERE 1=1`
	args := []interface{}{}

	// Domain scoping for domain_admin
	if adminUser != nil && adminUser.Role == "domain_admin" {
		if len(adminUser.DomainIDs) > 0 {
			placeholders := make([]string, len(adminUser.DomainIDs))
			for i, id := range adminUser.DomainIDs {
				placeholders[i] = "?"
				args = append(args, id)
			}
			domainFilter := " AND u.domain_id IN (" + strings.Join(placeholders, ",") + ")" // #nosec G202 -- parameterized placeholders only
			query += domainFilter
			countQuery += domainFilter
		} else {
			// Empty DomainIDs means no domains assigned — return no results
			query += " AND 1=0"
			countQuery += " AND 1=0"
		}
	}

	// Add filters to query
	if filterUsername != "" {
		query += " AND u.username LIKE ?"
		countQuery += " AND u.username LIKE ?"
		args = append(args, "%"+filterUsername+"%")
	}
	if filterDomain != "" {
		query += " AND d.name = ?"
		countQuery += " AND d.name = ?"
		args = append(args, filterDomain)
	}
	if filterRole != "" {
		if filterRole == "admin" {
			query += " AND u.is_admin = 1"
			countQuery += " AND u.is_admin = 1"
		} else {
			query += " AND EXISTS (SELECT 1 FROM user_roles ur2 JOIN roles r2 ON ur2.role_id = r2.id WHERE ur2.user_id = u.id AND r2.name = ?)"
			countQuery += " AND EXISTS (SELECT 1 FROM user_roles ur2 JOIN roles r2 ON ur2.role_id = r2.id WHERE ur2.user_id = u.id AND r2.name = ?)"
			args = append(args, filterRole)
		}
	}

	// Get total count
	var totalCount int
	err := s.db.QueryRowContext(r.Context(), countQuery, args...).Scan(&totalCount)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to count users", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Add order and pagination
	query += " ORDER BY d.name, u.username LIMIT ? OFFSET ?"
	args = append(args, pagination.PageSize, pagination.Offset)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get users", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type User struct {
		ID        int64
		Username  string
		Domain    string
		Email     string
		IsAdmin   bool
		Role      string
		CreatedAt time.Time
	}

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Domain, &u.IsAdmin, &u.CreatedAt, &u.Role); err != nil {
			s.logger.ErrorContext(r.Context(), "Failed to scan user row", err)
			continue
		}
		u.Email = u.Username + "@" + u.Domain
		// If no role but is_admin, show as super_admin
		if u.Role == "" && u.IsAdmin {
			u.Role = "super_admin"
		}
		users = append(users, u)
	}

	// Get domains list for filter dropdown
	domainRows, err := s.db.QueryContext(r.Context(), "SELECT name FROM domains ORDER BY name")
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get domains", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer domainRows.Close()

	var domains []string
	for domainRows.Next() {
		var domain string
		if err := domainRows.Scan(&domain); err == nil {
			domains = append(domains, domain)
		}
	}

	// Calculate pagination info
	totalPages := (totalCount + pagination.PageSize - 1) / pagination.PageSize
	if totalPages == 0 {
		totalPages = 1
	}

	s.renderTemplate(w, "users.html", map[string]interface{}{
		"Title":          "Users",
		"Users":          users,
		"Domains":        domains,
		"FilterUsername": filterUsername,
		"FilterDomain":   filterDomain,
		"FilterRole":     filterRole,
		"AdminUser":      adminUser,
		"CurrentPage":    pagination.Page,
		"TotalPages":     totalPages,
		"TotalCount":     totalCount,
	})
}

// handleUserAdd handles adding a new user
func (s *Server) handleUserAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Get domains for dropdown
		rows, err := s.db.QueryContext(r.Context(), "SELECT id, name FROM domains ORDER BY name")
		if err != nil {
			s.logger.ErrorContext(r.Context(), "Failed to query domains", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type Domain struct {
			ID   int64
			Name string
		}
		var domains []Domain
		for rows.Next() {
			var d Domain
			if err := rows.Scan(&d.ID, &d.Name); err != nil {
				s.logger.ErrorContext(r.Context(), "Failed to scan domain row", err)
				continue
			}
			domains = append(domains, d)
		}

		s.renderTemplate(w, "user_form.html", map[string]interface{}{
			"Title":   "Add User",
			"Domains": domains,
		})
		return
	}

	// POST - create user
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Bad request", formErrorStatus(err))
		return
	}

	username := r.PostForm.Get("username")
	password := r.PostForm.Get("password")
	domainIDStr := r.PostForm.Get("domain_id")
	role := r.PostForm.Get("role")

	// Validate domain_id parsing
	domainID, err := strconv.ParseInt(domainIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid domain ID format", http.StatusBadRequest)
		return
	}

	// Validate username
	if err := validation.Username(username); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate password
	if err := validation.Password(password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if domainID == 0 {
		http.Error(w, "Domain is required", http.StatusBadRequest)
		return
	}

	// Check domain access for domain_admin
	currentAdmin := GetAdminUser(r)
	if currentAdmin != nil && !currentAdmin.HasDomainAccess(domainID) {
		http.Error(w, "Access denied - you cannot create users in this domain", http.StatusForbidden)
		return
	}

	user, err := s.authenticator.CreateUser(r.Context(), username, password, domainID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to create user", err)
		http.Error(w, "Failed to create user. Please check server logs for details.", http.StatusInternalServerError)
		return
	}

	// Initialize default mailboxes for the new user
	if err := s.store.InitializeUserMailboxes(r.Context(), user.ID); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to initialize mailboxes", err)
	}

	// Assign role if specified (only super_admin can assign roles)
	if role != "" && role != "none" {
		if currentAdmin != nil && currentAdmin.Role == "super_admin" {
			_ = s.assignUserRole(r.Context(), user.ID, role, domainID)
			if role == "super_admin" || role == "domain_admin" || role == "support" {
				_, _ = s.db.ExecContext(r.Context(), "UPDATE users SET is_admin = TRUE WHERE id = ?", user.ID)
			}
		}
	}

	// Audit log
	auditUser := getSessionUser(r)
	_ = s.auditLogger.Log(r.Context(), auditUser, audit.EventUserCreate, user.Username, map[string]interface{}{
		"domain_id": domainID,
		"role":      role,
	}, getIP(r))

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// handleUserEdit handles editing a user
func (s *Server) handleUserEdit(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.NotFound(w, r)
		return
	}
	userID, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID format", http.StatusBadRequest)
		return
	}

	currentAdmin := GetAdminUser(r)

	if r.Method == http.MethodGet {
		var username, domain string
		var isAdmin bool
		var editDomainID int64
		err := s.db.QueryRowContext(r.Context(),
			`SELECT u.username, d.name, u.is_admin, u.domain_id FROM users u
			 JOIN domains d ON u.domain_id = d.id WHERE u.id = ?`, userID).
			Scan(&username, &domain, &isAdmin, &editDomainID)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Check domain access for domain_admin
		if currentAdmin != nil && !currentAdmin.HasDomainAccess(editDomainID) {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		// Get current role
		var currentRole string
		roleErr := s.db.QueryRowContext(r.Context(),
			`SELECT r.name FROM user_roles ur JOIN roles r ON ur.role_id = r.id WHERE ur.user_id = ? LIMIT 1`, userID,
		).Scan(&currentRole)
		if roleErr != nil && isAdmin {
			currentRole = "super_admin"
		}

		// Get scoped domains for domain_admin role
		var scopedDomains []string
		if currentRole == "domain_admin" {
			dRows, _ := s.db.QueryContext(r.Context(),
				`SELECT d.name FROM user_roles ur JOIN domains d ON ur.domain_id = d.id WHERE ur.user_id = ? AND ur.domain_id IS NOT NULL`, userID)
			if dRows != nil {
				defer dRows.Close()
				for dRows.Next() {
					var dName string
					if dRows.Scan(&dName) == nil {
						scopedDomains = append(scopedDomains, dName)
					}
				}
			}
		}

		// Get all domains for role assignment dropdown
		var allDomains []struct {
			ID   int64
			Name string
		}
		if currentAdmin != nil && currentAdmin.Role == "super_admin" {
			dRows, _ := s.db.QueryContext(r.Context(), "SELECT id, name FROM domains ORDER BY name")
			if dRows != nil {
				defer dRows.Close()
				for dRows.Next() {
					var d struct {
						ID   int64
						Name string
					}
					if dRows.Scan(&d.ID, &d.Name) == nil {
						allDomains = append(allDomains, d)
					}
				}
			}
		}

		s.renderTemplate(w, "user_edit.html", map[string]interface{}{
			"Title":         "Edit User",
			"UserID":        userID,
			"Username":      username,
			"Email":         username + "@" + domain,
			"IsAdmin":       isAdmin,
			"CurrentRole":   currentRole,
			"ScopedDomains": scopedDomains,
			"AllDomains":    allDomains,
			"AdminUser":     currentAdmin,
		})
		return
	}

	// POST - update user
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Bad request", formErrorStatus(err))
		return
	}

	password := r.PostForm.Get("password")
	role := r.PostForm.Get("role")

	// Check domain access
	var editDomainID int64
	if err := s.db.QueryRowContext(r.Context(), "SELECT domain_id FROM users WHERE id = ?", userID).Scan(&editDomainID); err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	if currentAdmin != nil && !currentAdmin.HasDomainAccess(editDomainID) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Update role (only super_admin can change roles)
	if currentAdmin != nil && currentAdmin.Role == "super_admin" {
		tx, txErr := s.db.BeginTx(r.Context(), nil)
		if txErr != nil {
			s.logger.Error("Failed to begin transaction", "error", txErr.Error())
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer func() {
			if txErr != nil {
				_ = tx.Rollback()
			}
		}()

		// Clear existing roles
		if _, txErr = tx.ExecContext(r.Context(), "DELETE FROM user_roles WHERE user_id = ?", userID); txErr != nil {
			s.logger.Error("Failed to clear roles", "error", txErr.Error())
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if role != "" && role != "none" {
			// Get role ID
			var roleID int64
			if txErr = tx.QueryRowContext(r.Context(), "SELECT id FROM roles WHERE name = ?", role).Scan(&roleID); txErr != nil {
				s.logger.Warn("Role not found", "role", role, "error", txErr.Error())
				http.Error(w, "Invalid role", http.StatusBadRequest)
				return
			}

			// Assign scoped domains for domain_admin
			if role == "domain_admin" {
				if domainIDs, ok := r.Form["role_domains"]; ok {
					for _, idStr := range domainIDs {
						if dID, parseErr := strconv.ParseInt(idStr, 10, 64); parseErr == nil {
							_, txErr = tx.ExecContext(r.Context(),
								"INSERT OR IGNORE INTO user_roles (user_id, role_id, domain_id) VALUES (?, ?, ?)",
								userID, roleID, dID)
							if txErr != nil {
								s.logger.Error("Failed to assign domain role", "error", txErr.Error())
								http.Error(w, "Internal server error", http.StatusInternalServerError)
								return
							}
						}
					}
				}
			} else {
				if _, txErr = tx.ExecContext(r.Context(),
					"INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?)",
					userID, roleID); txErr != nil {
					s.logger.Error("Failed to assign role", "error", txErr.Error())
					http.Error(w, "Internal server error", http.StatusInternalServerError)
					return
				}
			}

			// Sync is_admin flag for backward compat
			_, _ = tx.ExecContext(r.Context(), "UPDATE users SET is_admin = TRUE WHERE id = ?", userID)
		} else {
			_, _ = tx.ExecContext(r.Context(), "UPDATE users SET is_admin = FALSE WHERE id = ?", userID)
		}

		if txErr = tx.Commit(); txErr != nil {
			s.logger.Error("Failed to commit role update", "error", txErr.Error())
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	// Update password if provided
	if password != "" {
		if err := validation.Password(password); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.authenticator.UpdatePassword(r.Context(), userID, password); err != nil {
			http.Error(w, "Failed to update password", http.StatusInternalServerError)
			return
		}
		s.invalidateUserSessions(userID)
		auditUser := getSessionUser(r)
		_ = s.auditLogger.Log(r.Context(), auditUser, audit.EventPasswordChange, strconv.FormatInt(userID, 10), nil, getIP(r))
	}

	// Audit log user update
	auditUser := getSessionUser(r)
	_ = s.auditLogger.Log(r.Context(), auditUser, audit.EventUserUpdate, strconv.FormatInt(userID, 10), map[string]interface{}{
		"role": role,
	}, getIP(r))

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// handleUserDelete handles deleting a user
func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract user ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.NotFound(w, r)
		return
	}
	userID, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID format", http.StatusBadRequest)
		return
	}

	var deleteErr error
	_, deleteErr = s.db.ExecContext(r.Context(), "DELETE FROM users WHERE id = ?", userID)
	if deleteErr != nil {
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	// Invalidate all sessions for the deleted user
	s.invalidateUserSessions(userID)

	// Audit log
	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventUserDelete, strconv.FormatInt(userID, 10), nil, getIP(r))

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// assignUserRole assigns a role to a user in the user_roles table
func (s *Server) assignUserRole(ctx context.Context, userID int64, roleName string, domainID int64) error {
	var roleID int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM roles WHERE name = ?", roleName).Scan(&roleID)
	if err != nil {
		s.logger.Warn("Failed to find role for assignment", "role", roleName, "error", err.Error())
		return fmt.Errorf("role %q not found: %w", roleName, err)
	}

	if roleName == "domain_admin" && domainID > 0 {
		_, err = s.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO user_roles (user_id, role_id, domain_id) VALUES (?, ?, ?)",
			userID, roleID, domainID)
	} else {
		_, err = s.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?)",
			userID, roleID)
	}
	if err != nil {
		s.logger.Error("Failed to assign user role", "user_id", userID, "role", roleName, "error", err.Error())
		return fmt.Errorf("failed to assign role: %w", err)
	}
	return nil
}

// handleDomains shows domain list
func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	// Get pagination parameters
	pagination := getPaginationParams(r)

	// Get filter parameters
	filterName := r.URL.Query().Get("name")

	// Build query with filters
	// Use hardcoded defaults for is_verified and verification_token
	// In case the migration 015 columns don't exist yet
	query := `SELECT d.id, d.name, d.created_at, COALESCE(d.dkim_selector, 'mail'),
			d.dkim_private_key IS NOT NULL AND LENGTH(d.dkim_private_key) > 0,
			d.dkim_key_file,
			(SELECT COUNT(*) FROM users WHERE domain_id = d.id) as user_count,
			COALESCE(d.dns_status, 'pending'),
			COALESCE(d.dns_mx_verified, 0),
			COALESCE(d.dns_spf_verified, 0),
			COALESCE(d.dns_dkim_verified, 0),
			COALESCE(d.dns_dmarc_verified, 0),
			COALESCE(d.dns_mail_hostname_verified, 0),
			d.dns_last_checked,
			COALESCE(d.mail_hostname, 'mail.' || d.name),
			COALESCE(d.is_primary, 0),
			1 as is_verified,
			'' as verification_token
		FROM domains d WHERE 1=1`
	countQuery := `SELECT COUNT(*) FROM domains WHERE 1=1`
	args := []interface{}{}

	// Add filter to query
	if filterName != "" {
		query += " AND d.name LIKE ?"
		countQuery += " AND name LIKE ?"
		args = append(args, "%"+filterName+"%")
	}

	// Get total count
	var totalCount int
	err := s.db.QueryRowContext(r.Context(), countQuery, args...).Scan(&totalCount)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to count domains", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Add order and pagination
	query += " ORDER BY d.name LIMIT ? OFFSET ?"
	paginationArgs := []interface{}{pagination.PageSize, pagination.Offset}

	rows, err := s.db.QueryContext(r.Context(), query, append(args, paginationArgs...)...)
	if err != nil && strings.Contains(err.Error(), "no such column") {
		// Fall back to query without is_verified and verification_token columns
		query := `SELECT d.id, d.name, d.created_at, COALESCE(d.dkim_selector, 'mail'),
			d.dkim_private_key IS NOT NULL AND LENGTH(d.dkim_private_key) > 0,
			d.dkim_key_file,
			(SELECT COUNT(*) FROM users WHERE domain_id = d.id) as user_count,
			COALESCE(d.dns_status, 'pending'),
			COALESCE(d.dns_mx_verified, 0),
			COALESCE(d.dns_spf_verified, 0),
			COALESCE(d.dns_dkim_verified, 0),
			COALESCE(d.dns_dmarc_verified, 0),
			COALESCE(d.dns_mail_hostname_verified, 0),
			d.dns_last_checked,
			COALESCE(d.mail_hostname, 'mail.' || d.name),
			COALESCE(d.is_primary, 0),
			1,
			''
		FROM domains d WHERE 1=1`

		if filterName != "" {
			query += " AND d.name LIKE ?"
		}
		query += " ORDER BY d.name LIMIT ? OFFSET ?"

		rows, err = s.db.QueryContext(r.Context(), query, append(args, paginationArgs...)...)
		if err != nil {
			s.logger.ErrorContext(r.Context(), "Failed to get domains", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	} else if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get domains", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Domain struct {
		ID                      int64
		Name                    string
		UserCount               int
		CreatedAt               time.Time
		DKIMSelector            string
		HasDKIMKey              bool
		DNSStatus               string
		DNSMXVerified           bool
		DNSSPFVerified          bool
		DNSDKIMVerified         bool
		DNSDMARCVerified        bool
		DNSMailHostnameVerified bool
		DNSLastChecked          sql.NullTime
		DNSVerifiedCount        int
		MailHostname            string
		IsPrimary               bool
		IsVerified              bool
		VerificationToken       string
	}

	// Get DKIM key directory for file-based check
	dkimPath := s.getDKIMPath()

	var domains []Domain
	for rows.Next() {
		var d Domain
		var selector string
		var hasDBKey bool
		var keyFile sql.NullString
		var mxVerified, spfVerified, dkimVerified, dmarcVerified, mailHostnameVerified int
		var isPrimaryInt, isVerifiedInt int
		if err := rows.Scan(&d.ID, &d.Name, &d.CreatedAt, &selector, &hasDBKey, &keyFile, &d.UserCount,
			&d.DNSStatus, &mxVerified, &spfVerified, &dkimVerified, &dmarcVerified, &mailHostnameVerified,
			&d.DNSLastChecked, &d.MailHostname, &isPrimaryInt, &isVerifiedInt, &d.VerificationToken); err != nil {
			s.logger.ErrorContext(r.Context(), "Failed to scan domain row", err)
			continue
		}
		d.IsPrimary = isPrimaryInt == 1
		d.IsVerified = isVerifiedInt == 1
		d.DKIMSelector = selector
		d.DNSMXVerified = mxVerified == 1
		d.DNSSPFVerified = spfVerified == 1
		d.DNSDKIMVerified = dkimVerified == 1
		d.DNSDMARCVerified = dmarcVerified == 1
		d.DNSMailHostnameVerified = mailHostnameVerified == 1
		d.DNSVerifiedCount = mxVerified + spfVerified + dkimVerified + dmarcVerified

		// Check if key exists either in database or as file
		d.HasDKIMKey = hasDBKey
		if !d.HasDKIMKey {
			// Check for file-based key
			if keyFile.Valid && keyFile.String != "" {
				if _, err := os.Stat(keyFile.String); err == nil {
					d.HasDKIMKey = true
				}
			} else {
				// Check default file location
				keyPath := dkimPath + "/" + d.Name + ".key"
				if _, err := os.Stat(keyPath); err == nil {
					d.HasDKIMKey = true
				}
			}
		}

		domains = append(domains, d)
	}

	// Check for error message in query params
	errorMsg := r.URL.Query().Get("error")

	// Calculate pagination info
	totalPages := (totalCount + pagination.PageSize - 1) / pagination.PageSize
	if totalPages == 0 {
		totalPages = 1
	}

	s.renderTemplate(w, "domains.html", map[string]interface{}{
		"Title":       "Domains",
		"Domains":     domains,
		"Error":       errorMsg,
		"FilterName":  filterName,
		"CurrentPage": pagination.Page,
		"TotalPages":  totalPages,
		"TotalCount":  totalCount,
	})
}

// handleSieve handles Sieve script management
func (s *Server) handleSieve(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.NotFound(w, r)
		return
	}
	userID, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID format", http.StatusBadRequest)
		return
	}

	if s.sieveStore == nil {
		http.Error(w, "Sieve not configured", http.StatusServiceUnavailable)
		return
	}

	if r.Method == http.MethodGet {
		scripts, _ := s.sieveStore.ListScripts(r.Context(), userID)

		s.renderTemplate(w, "sieve.html", map[string]interface{}{
			"Title":   "Sieve Scripts",
			"UserID":  userID,
			"Scripts": scripts,
		})
		return
	}

	// POST - save script
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Bad request", formErrorStatus(err))
		return
	}

	name := r.PostForm.Get("name")
	content := r.PostForm.Get("content")
	action := r.PostForm.Get("action")

	switch action {
	case "create":
		_, err := s.sieveStore.CreateScript(r.Context(), userID, name, content)
		if err != nil {
			http.Error(w, "Failed to create script. Check server logs for details.", http.StatusBadRequest)
			return
		}
	case "update":
		err := s.sieveStore.UpdateScript(r.Context(), userID, name, content)
		if err != nil {
			http.Error(w, "Failed to update script. Check server logs for details.", http.StatusBadRequest)
			return
		}
	case "delete":
		err := s.sieveStore.DeleteScript(r.Context(), userID, name)
		if err != nil {
			http.Error(w, "Failed to delete script", http.StatusInternalServerError)
			return
		}
	case "activate":
		err := s.sieveStore.SetActiveScript(r.Context(), userID, name)
		if err != nil {
			http.Error(w, "Failed to activate script", http.StatusInternalServerError)
			return
		}
	}

	http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
}

// handleLogs shows the logs landing page
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "logs.html", map[string]interface{}{
		"Title": "Logs",
	})
}

// handleAuthLogs shows authentication logs
func (s *Server) handleAuthLogs(w http.ResponseWriter, r *http.Request) {
	// Get pagination parameters
	pagination := getPaginationParams(r)

	// Get filter parameters
	filterUsername := r.URL.Query().Get("username")
	filterProtocol := r.URL.Query().Get("protocol")
	filterStatus := r.URL.Query().Get("status")
	filterDateFrom := r.URL.Query().Get("date_from")
	filterDateTo := r.URL.Query().Get("date_to")

	// Build query with filters
	query := `SELECT id, username, remote_addr, protocol, success, failure_reason, created_at
		FROM auth_log WHERE 1=1`
	countQuery := `SELECT COUNT(*) FROM auth_log WHERE 1=1`
	args := []interface{}{}

	// Add filters to query
	if filterUsername != "" {
		query += " AND username LIKE ?"
		countQuery += " AND username LIKE ?"
		args = append(args, "%"+filterUsername+"%")
	}
	if filterProtocol != "" {
		query += " AND protocol = ?"
		countQuery += " AND protocol = ?"
		args = append(args, filterProtocol)
	}
	if filterStatus == "success" {
		query += " AND success = 1"
		countQuery += " AND success = 1"
	} else if filterStatus == "failed" {
		query += " AND success = 0"
		countQuery += " AND success = 0"
	}
	if filterDateFrom != "" {
		query += " AND created_at >= ?"
		countQuery += " AND created_at >= ?"
		args = append(args, filterDateFrom)
	}
	if filterDateTo != "" {
		query += " AND created_at <= ?"
		countQuery += " AND created_at <= ?"
		args = append(args, filterDateTo+" 23:59:59")
	}

	// Get total count
	var totalCount int
	err := s.db.QueryRowContext(r.Context(), countQuery, args...).Scan(&totalCount)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to count auth logs", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Add order and pagination
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, pagination.PageSize, pagination.Offset)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get auth logs", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type LogEntry struct {
		ID            int64
		Username      string
		RemoteAddr    string
		Protocol      string
		Success       bool
		FailureReason *string
		CreatedAt     time.Time
	}

	var logs []LogEntry
	for rows.Next() {
		var l LogEntry
		if err := rows.Scan(&l.ID, &l.Username, &l.RemoteAddr, &l.Protocol, &l.Success, &l.FailureReason, &l.CreatedAt); err != nil {
			s.logger.ErrorContext(r.Context(), "Failed to scan auth log row", err)
			continue
		}
		logs = append(logs, l)
	}

	// Calculate pagination info
	totalPages := (totalCount + pagination.PageSize - 1) / pagination.PageSize
	if totalPages == 0 {
		totalPages = 1
	}

	s.renderTemplate(w, "auth_logs.html", map[string]interface{}{
		"Title":          "Authentication Logs",
		"Logs":           logs,
		"FilterUsername": filterUsername,
		"FilterProtocol": filterProtocol,
		"FilterStatus":   filterStatus,
		"FilterDateFrom": filterDateFrom,
		"FilterDateTo":   filterDateTo,
		"CurrentPage":    pagination.Page,
		"TotalPages":     totalPages,
		"TotalCount":     totalCount,
	})
}

// handleDeliveryLogs shows delivery logs
func (s *Server) handleDeliveryLogs(w http.ResponseWriter, r *http.Request) {
	// Get pagination parameters
	pagination := getPaginationParams(r)

	// Get filter parameters
	filterSender := r.URL.Query().Get("sender")
	filterRecipient := r.URL.Query().Get("recipient")
	filterStatus := r.URL.Query().Get("status")
	filterDateFrom := r.URL.Query().Get("date_from")
	filterDateTo := r.URL.Query().Get("date_to")

	// Build query with filters
	query := `SELECT id, message_id, sender, recipient, status, smtp_code, error_message, created_at
		FROM delivery_log WHERE 1=1`
	countQuery := `SELECT COUNT(*) FROM delivery_log WHERE 1=1`
	args := []interface{}{}

	// Add filters to query
	if filterSender != "" {
		query += " AND sender LIKE ?"
		countQuery += " AND sender LIKE ?"
		args = append(args, "%"+filterSender+"%")
	}
	if filterRecipient != "" {
		query += " AND recipient LIKE ?"
		countQuery += " AND recipient LIKE ?"
		args = append(args, "%"+filterRecipient+"%")
	}
	if filterStatus != "" {
		query += " AND status = ?"
		countQuery += " AND status = ?"
		args = append(args, filterStatus)
	}
	if filterDateFrom != "" {
		query += " AND created_at >= ?"
		countQuery += " AND created_at >= ?"
		args = append(args, filterDateFrom)
	}
	if filterDateTo != "" {
		query += " AND created_at <= ?"
		countQuery += " AND created_at <= ?"
		args = append(args, filterDateTo+" 23:59:59")
	}

	// Get total count
	var totalCount int
	err := s.db.QueryRowContext(r.Context(), countQuery, args...).Scan(&totalCount)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to count delivery logs", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Add order and pagination
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, pagination.PageSize, pagination.Offset)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get delivery logs", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type LogEntry struct {
		ID           int64
		MessageID    *string
		Sender       string
		Recipient    string
		Status       string
		SMTPCode     *int
		ErrorMessage *string
		CreatedAt    time.Time
	}

	var logs []LogEntry
	for rows.Next() {
		var l LogEntry
		if err := rows.Scan(&l.ID, &l.MessageID, &l.Sender, &l.Recipient, &l.Status, &l.SMTPCode, &l.ErrorMessage, &l.CreatedAt); err != nil {
			s.logger.ErrorContext(r.Context(), "Failed to scan delivery log row", err)
			continue
		}
		logs = append(logs, l)
	}

	// Calculate pagination info
	totalPages := (totalCount + pagination.PageSize - 1) / pagination.PageSize
	if totalPages == 0 {
		totalPages = 1
	}

	s.renderTemplate(w, "delivery_logs.html", map[string]interface{}{
		"Title":           "Delivery Logs",
		"Logs":            logs,
		"FilterSender":    filterSender,
		"FilterRecipient": filterRecipient,
		"FilterStatus":    filterStatus,
		"FilterDateFrom":  filterDateFrom,
		"FilterDateTo":    filterDateTo,
		"CurrentPage":     pagination.Page,
		"TotalPages":      totalPages,
		"TotalCount":      totalCount,
	})
}

// handleDomainAdd handles adding a new domain
func (s *Server) handleDomainAdd(w http.ResponseWriter, r *http.Request) {
	// Redirect GET to domains page (form is now a modal on that page)
	if r.Method == http.MethodGet {
		http.Redirect(w, r, "/admin/domains", http.StatusSeeOther)
		return
	}

	// POST - create domain
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Redirect(w, r, "/admin/domains?error=Bad+request", http.StatusSeeOther)
		return
	}

	name := strings.TrimSpace(strings.ToLower(r.PostForm.Get("name")))

	// Validate domain name
	if err := validation.Domain(name); err != nil {
		http.Redirect(w, r, "/admin/domains?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	// Check if domain already exists
	var exists int
	err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM domains WHERE name = ?", name).Scan(&exists)
	if err == nil && exists > 0 {
		http.Redirect(w, r, "/admin/domains?error=Domain+already+exists", http.StatusSeeOther)
		return
	}

	// Get DKIM configuration from form
	generateDKIM := r.PostForm.Get("generate_dkim") == "on"
	dkimSelector := r.PostForm.Get("dkim_selector")
	if dkimSelector == "" {
		dkimSelector = "mail"
	}
	dkimBits := 2048
	if r.PostForm.Get("dkim_bits") == "4096" {
		dkimBits = 4096
	}
	dkimStorage := r.PostForm.Get("dkim_storage")
	if dkimStorage == "" {
		dkimStorage = "database"
	}

	// Generate verification token for domain ownership verification
	// SECURITY: Requires DNS TXT record verification before domain can send emails
	verificationToken := generateDomainVerificationToken()

	// Insert domain with auto-generated mail hostname
	mailHostname := "mail." + name
	// Try to insert with verification columns, fall back if they don't exist
	_, err = s.db.ExecContext(r.Context(),
		`INSERT INTO domains (name, dkim_selector, mail_hostname, verification_token, is_verified)
		 VALUES (?, ?, ?, ?, FALSE)`,
		name, dkimSelector, mailHostname, verificationToken,
	)
	// If insert fails due to missing columns, try without them
	if err != nil && strings.Contains(err.Error(), "no such column") {
		_, err = s.db.ExecContext(r.Context(),
			`INSERT INTO domains (name, dkim_selector, mail_hostname)
			 VALUES (?, ?, ?)`,
			name, dkimSelector, mailHostname,
		)
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to create domain", err)
		http.Redirect(w, r, "/admin/domains?error=Failed+to+create+domain", http.StatusSeeOther)
		return
	}

	// Generate DKIM key if requested
	if generateDKIM {
		dkimPath := s.getDKIMPath()
		store := security.NewKeyStore(dkimStorage, dkimPath, s.db)

		_, err = security.GenerateAndSaveKey(r.Context(), store, name, dkimSelector, dkimBits)
		if err != nil {
			s.logger.ErrorContext(r.Context(), "Failed to generate DKIM key", err)
			// Domain was created but DKIM failed - log but don't fail the request
		}
	}

	// Audit log
	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventDomainCreate, name, map[string]interface{}{
		"generate_dkim": generateDKIM,
		"dkim_selector": dkimSelector,
	}, getIP(r))

	http.Redirect(w, r, "/admin/domains", http.StatusSeeOther)
}

// handleDomainDelete handles deleting a domain
func (s *Server) handleDomainDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.NotFound(w, r)
		return
	}
	domainID, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		http.Error(w, "Invalid domain ID format", http.StatusBadRequest)
		return
	}

	// Delete domain (users will be cascade deleted due to foreign key)
	var deleteDomainErr error
	_, deleteDomainErr = s.db.ExecContext(r.Context(), "DELETE FROM domains WHERE id = ?", domainID)
	if deleteDomainErr != nil {
		s.logger.ErrorContext(r.Context(), "Failed to delete domain", deleteDomainErr)
		http.Error(w, "Failed to delete domain", http.StatusInternalServerError)
		return
	}

	// Audit log
	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventDomainDelete, strconv.FormatInt(domainID, 10), nil, getIP(r))

	http.Redirect(w, r, "/admin/domains", http.StatusSeeOther)
}

// handleDomainVerifyOwnership verifies domain ownership via TXT record
// SECURITY: Ensures only domain owners can add domains to the system
func (s *Server) handleDomainVerifyOwnership(w http.ResponseWriter, r *http.Request) {
	// Extract domain ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.NotFound(w, r)
		return
	}
	domainID, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		http.Error(w, "Invalid domain ID format", http.StatusBadRequest)
		return
	}

	// Get domain info including verification token
	var domainName, verificationToken string
	// Try to fetch verification token if the column exists
	err = s.db.QueryRowContext(r.Context(),
		"SELECT name, COALESCE(verification_token, '') FROM domains WHERE id = ?",
		domainID).Scan(&domainName, &verificationToken)
	// If it fails due to missing column, try without it
	if err != nil && strings.Contains(err.Error(), "no such column") {
		err = s.db.QueryRowContext(r.Context(),
			"SELECT name FROM domains WHERE id = ?",
			domainID).Scan(&domainName)
		verificationToken = ""
	}
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Look up TXT record at _mailserver-verify.domain.com
	verifyDomain := "_mailserver-verify." + domainName
	txtRecords, err := net.LookupTXT(verifyDomain)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         false,
			"verified":        false,
			"message":         "TXT record not found",
			"expected_record": verifyDomain,
			"expected_value":  verificationToken,
			"instructions":    "Add a TXT record with the name '_mailserver-verify' and the value shown above",
		})
		return
	}

	// Check if any TXT record matches our verification token
	verified := false
	for _, txt := range txtRecords {
		if strings.TrimSpace(txt) == verificationToken {
			verified = true
			break
		}
	}

	if verified {
		// Update domain as verified (only if the column exists in the database)
		_, err = s.db.ExecContext(r.Context(),
			"UPDATE domains SET is_verified = TRUE, verified_at = ? WHERE id = ?",
			time.Now(), domainID)
		// Ignore error if column doesn't exist (older database schema)
		if err != nil && !strings.Contains(err.Error(), "no such column") {
			s.logger.ErrorContext(r.Context(), "Failed to update domain verification", err)
		}

		// Audit log
		adminUser := getSessionUser(r)
		s.auditLogger.Log(r.Context(), adminUser, audit.EventDomainCreate, domainName, map[string]interface{}{
			"action": "ownership_verified",
		}, getIP(r))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"verified": true,
			"message":  "Domain ownership verified successfully",
		})
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         false,
			"verified":        false,
			"message":         "TXT record found but value doesn't match",
			"expected_record": verifyDomain,
			"expected_value":  verificationToken,
			"found_values":    txtRecords,
		})
	}
}

// handleDKIMGenerate generates a DKIM key for a domain
func (s *Server) handleDKIMGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 {
		http.NotFound(w, r)
		return
	}
	domainID, err := strconv.ParseInt(parts[5], 10, 64)
	if err != nil {
		http.Error(w, "Invalid domain ID format", http.StatusBadRequest)
		return
	}

	// Get domain name
	var domainName string
	err = s.db.QueryRowContext(r.Context(), "SELECT name FROM domains WHERE id = ?", domainID).Scan(&domainName)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	// Parse form values
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Bad request", formErrorStatus(err))
		return
	}

	selector := r.PostForm.Get("selector")
	if selector == "" {
		selector = "mail"
	}

	bitsStr := r.PostForm.Get("bits")
	bits := 2048
	if bitsStr == "4096" {
		bits = 4096
	}

	storageType := r.PostForm.Get("storage")
	if storageType == "" {
		storageType = "database"
	}

	// Get DKIM key directory
	dkimPath := s.getDKIMPath()

	// Create key store
	store := security.NewKeyStore(storageType, dkimPath, s.db)

	// Generate key
	_, err = security.GenerateAndSaveKey(r.Context(), store, domainName, selector, bits)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to generate DKIM key", err)
		http.Error(w, "Failed to generate DKIM key. Check server logs.", http.StatusInternalServerError)
		return
	}

	// Audit log
	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventConfigChange, domainName, map[string]interface{}{
		"action":   "dkim_generate",
		"selector": selector,
		"bits":     bits,
	}, getIP(r))

	http.Redirect(w, r, "/admin/domains", http.StatusSeeOther)
}

// handleDKIMShow returns DKIM DNS record as JSON
func (s *Server) handleDKIMShow(w http.ResponseWriter, r *http.Request) {
	// Extract domain ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 {
		http.NotFound(w, r)
		return
	}
	domainID, err := strconv.ParseInt(parts[5], 10, 64)
	if err != nil {
		http.Error(w, "Invalid domain ID format", http.StatusBadRequest)
		return
	}

	// Get domain info
	var domainName, selector string
	var storageType sql.NullString
	err = s.db.QueryRowContext(r.Context(),
		"SELECT name, COALESCE(dkim_selector, 'mail'), dkim_storage_type FROM domains WHERE id = ?",
		domainID).Scan(&domainName, &selector, &storageType)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	storage := "file"
	if storageType.Valid && storageType.String != "" {
		storage = storageType.String
	}

	// Get DKIM key directory
	dkimPath := s.getDKIMPath()

	// Create key store
	store := security.NewKeyStore(storage, dkimPath, s.db)

	// Get key metadata
	meta, err := store.GetKeyMetadata(r.Context(), domainName)
	if err != nil || !meta.HasKey {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"hasKey": false,
			"domain": domainName,
		})
		return
	}

	// Get DNS record
	recordName, recordValue, err := security.GetDNSRecord(r.Context(), store, domainName)
	if err != nil {
		http.Error(w, "Failed to get DNS record", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"hasKey":      true,
		"domain":      domainName,
		"selector":    meta.Selector,
		"algorithm":   meta.Algorithm,
		"createdAt":   meta.CreatedAt.Format("2006-01-02 15:04:05"),
		"storageType": meta.StorageType,
		"recordName":  recordName,
		"recordValue": recordValue,
	})
}

// handleDKIMRotate rotates the DKIM key for a domain
func (s *Server) handleDKIMRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract domain ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 {
		http.NotFound(w, r)
		return
	}
	domainID, err := strconv.ParseInt(parts[5], 10, 64)
	if err != nil {
		http.Error(w, "Invalid domain ID format", http.StatusBadRequest)
		return
	}

	// Get domain info
	var domainName string
	var storageType sql.NullString
	err = s.db.QueryRowContext(r.Context(),
		"SELECT name, dkim_storage_type FROM domains WHERE id = ?",
		domainID).Scan(&domainName, &storageType)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	storage := "file"
	if storageType.Valid && storageType.String != "" {
		storage = storageType.String
	}

	// Get DKIM key directory
	dkimPath := s.getDKIMPath()

	// Create key store
	store := security.NewKeyStore(storage, dkimPath, s.db)

	// Rotate key
	newSelector, _, err := security.RotateKey(r.Context(), store, domainName, 2048)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to rotate DKIM key", err)
		http.Error(w, "Failed to rotate DKIM key. Check server logs.", http.StatusInternalServerError)
		return
	}

	// Audit log
	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventConfigChange, domainName, map[string]interface{}{
		"action":      "dkim_rotate",
		"newSelector": newSelector,
	}, getIP(r))

	http.Redirect(w, r, "/admin/domains", http.StatusSeeOther)
}

// handleDomainDNS shows all DNS records for a domain
func (s *Server) handleDomainDNS(w http.ResponseWriter, r *http.Request) {
	// Extract domain ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.NotFound(w, r)
		return
	}
	domainID, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		http.Error(w, "Invalid domain ID format", http.StatusBadRequest)
		return
	}

	// Get domain info
	var domainName, selector string
	var storageType sql.NullString
	err = s.db.QueryRowContext(r.Context(),
		"SELECT name, COALESCE(dkim_selector, 'mail'), dkim_storage_type FROM domains WHERE id = ?",
		domainID).Scan(&domainName, &selector, &storageType)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	storage := "file"
	if storageType.Valid && storageType.String != "" {
		storage = storageType.String
	}

	// Get DKIM key directory
	dkimPath := s.getDKIMPath()

	// Create key store
	store := security.NewKeyStore(storage, dkimPath, s.db)

	// Get all DNS records
	records, err := security.GetAllDNSRecords(r.Context(), store, domainName, s.config.Server.Hostname)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get DNS records", err)
	}

	// Get DKIM record name
	dkimRecordName := selector + "._domainkey." + domainName

	// Get mail hostname
	mailHostname := "mail." + domainName

	// Get server IP for A record suggestion
	var serverIP string
	if ips, err := net.LookupIP(s.config.Server.Hostname); err == nil && len(ips) > 0 {
		serverIP = ips[0].String()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"domain":         domainName,
		"hostname":       s.config.Server.Hostname,
		"mxRecord":       records.MX,
		"spfRecord":      records.SPF,
		"dmarcRecord":    records.DMARC,
		"dkimRecord":     records.DKIM,
		"dkimRecordName": dkimRecordName,
		"selector":       selector,
		"mailHostname":   mailHostname,
		"serverIP":       serverIP,
	})
}

// handleDNSVerify performs actual DNS lookups to verify domain configuration
func (s *Server) handleDNSVerify(w http.ResponseWriter, r *http.Request) {
	// Extract domain ID from path: /admin/domains/dns/verify/{id}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	domainID, err := strconv.ParseInt(parts[5], 10, 64)
	if err != nil {
		http.Error(w, "Invalid domain ID format", http.StatusBadRequest)
		return
	}

	// Get domain info
	var domainName, selector string
	var storageType sql.NullString
	err = s.db.QueryRowContext(r.Context(),
		"SELECT name, COALESCE(dkim_selector, 'mail'), dkim_storage_type FROM domains WHERE id = ?",
		domainID).Scan(&domainName, &selector, &storageType)
	if err != nil {
		http.Error(w, "Domain not found", http.StatusNotFound)
		return
	}

	storage := "file"
	if storageType.Valid && storageType.String != "" {
		storage = storageType.String
	}

	// Get expected values
	dkimPath := s.getDKIMPath()
	store := security.NewKeyStore(storage, dkimPath, s.db)
	records, _ := security.GetAllDNSRecords(r.Context(), store, domainName, s.config.Server.Hostname)

	// Verify MX record
	mxResult := s.verifyMXRecord(domainName, s.config.Server.Hostname)

	// Verify SPF record
	spfResult := s.verifySPFRecord(domainName)

	// Verify DKIM record
	dkimResult := s.verifyDKIMRecord(domainName, selector, records.DKIM)

	// Verify DMARC record
	dmarcResult := s.verifyDMARCRecord(domainName)

	// Verify mail hostname A record
	mailHostnameResult := s.verifyMailHostnameRecord(domainName)

	// Persist results to database
	var mxVerified, spfVerified, dkimVerified, dmarcVerified, mailHostnameVerified int
	if mxResult["ok"].(bool) {
		mxVerified = 1
	}
	if spfResult["ok"].(bool) {
		spfVerified = 1
	}
	if dkimResult["ok"].(bool) {
		dkimVerified = 1
	}
	if dmarcResult["ok"].(bool) {
		dmarcVerified = 1
	}
	if mailHostnameResult["ok"].(bool) {
		mailHostnameVerified = 1
	}

	// Calculate overall status (mail hostname is optional for core email functionality)
	var dnsStatus string
	if mxVerified == 1 && spfVerified == 1 && dkimVerified == 1 && dmarcVerified == 1 {
		dnsStatus = "ready"
	} else if mxVerified == 1 || spfVerified == 1 {
		dnsStatus = "partial"
	} else {
		dnsStatus = "pending"
	}

	// Update database
	_, err = s.db.ExecContext(r.Context(),
		`UPDATE domains SET
			dns_mx_verified = ?,
			dns_spf_verified = ?,
			dns_dkim_verified = ?,
			dns_dmarc_verified = ?,
			dns_mail_hostname_verified = ?,
			dns_status = ?,
			dns_last_checked = ?
		WHERE id = ?`,
		mxVerified, spfVerified, dkimVerified, dmarcVerified, mailHostnameVerified, dnsStatus, time.Now(), domainID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to update DNS status", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"mx":           mxResult,
		"spf":          spfResult,
		"dkim":         dkimResult,
		"dmarc":        dmarcResult,
		"mailHostname": mailHostnameResult,
		"domain":       domainName,
		"dnsStatus":    dnsStatus,
	})
}

// verifyMXRecord checks if MX record points to the correct hostname
func (s *Server) verifyMXRecord(domain, expectedHost string) map[string]interface{} {
	result := map[string]interface{}{
		"ok":    false,
		"found": "",
	}

	mxRecords, err := net.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		return result
	}

	// Valid MX targets: server hostname OR mail.<domain>
	validHosts := []string{
		expectedHost,
		"mail." + domain,
	}

	// Check if any MX record matches valid hosts
	var foundHosts []string
	for _, mx := range mxRecords {
		host := strings.TrimSuffix(mx.Host, ".")
		foundHosts = append(foundHosts, host)
		for _, validHost := range validHosts {
			if strings.EqualFold(host, validHost) || strings.EqualFold(host, validHost+".") {
				result["ok"] = true
			}
		}
	}
	result["found"] = strings.Join(foundHosts, ", ")

	return result
}

// verifySPFRecord checks if SPF record exists and contains expected values
func (s *Server) verifySPFRecord(domain string) map[string]interface{} {
	result := map[string]interface{}{
		"ok":    false,
		"found": "",
	}

	txtRecords, err := net.LookupTXT(domain)
	if err != nil {
		return result
	}

	for _, txt := range txtRecords {
		if strings.HasPrefix(txt, "v=spf1") {
			result["found"] = txt
			// Check if it has mx or the hostname
			if strings.Contains(txt, "mx") || strings.Contains(txt, s.config.Server.Hostname) {
				result["ok"] = true
			}
			return result
		}
	}

	return result
}

// verifyDKIMRecord checks if DKIM record matches the expected public key
func (s *Server) verifyDKIMRecord(domain, selector, expectedDKIM string) map[string]interface{} {
	result := map[string]interface{}{
		"ok":    false,
		"found": "",
	}

	// No DKIM key generated yet
	if expectedDKIM == "" || strings.HasPrefix(expectedDKIM, "No DKIM") {
		result["found"] = "No DKIM key generated"
		return result
	}

	dkimDomain := selector + "._domainkey." + domain
	txtRecords, err := net.LookupTXT(dkimDomain)
	if err != nil {
		return result
	}

	for _, txt := range txtRecords {
		if strings.Contains(txt, "v=DKIM1") {
			result["found"] = txt
			// Check if the public key portion matches
			// Extract just the key part for comparison
			if strings.Contains(txt, "p=") && strings.Contains(expectedDKIM, "p=") {
				// Get the p= value from both
				foundKey := extractDKIMKey(txt)
				expectedKey := extractDKIMKey(expectedDKIM)
				if foundKey != "" && foundKey == expectedKey {
					result["ok"] = true
				}
			}
			return result
		}
	}

	return result
}

// extractDKIMKey extracts the public key value from a DKIM record
func extractDKIMKey(record string) string {
	// Remove spaces and newlines
	record = strings.ReplaceAll(record, " ", "")
	record = strings.ReplaceAll(record, "\n", "")
	record = strings.ReplaceAll(record, "\t", "")

	// Find p= and extract the key
	pIdx := strings.Index(record, "p=")
	if pIdx == -1 {
		return ""
	}

	key := record[pIdx+2:]
	// Key ends at ; or end of string
	if semiIdx := strings.Index(key, ";"); semiIdx != -1 {
		key = key[:semiIdx]
	}

	return key
}

// verifyDMARCRecord checks if DMARC record exists
func (s *Server) verifyDMARCRecord(domain string) map[string]interface{} {
	result := map[string]interface{}{
		"ok":    false,
		"found": "",
	}

	dmarcDomain := "_dmarc." + domain
	txtRecords, err := net.LookupTXT(dmarcDomain)
	if err != nil {
		return result
	}

	for _, txt := range txtRecords {
		if strings.HasPrefix(txt, "v=DMARC1") {
			result["found"] = txt
			result["ok"] = true
			return result
		}
	}

	return result
}

// verifyMailHostnameRecord checks if mail.{domain} A record resolves to an IP
func (s *Server) verifyMailHostnameRecord(domain string) map[string]interface{} {
	result := map[string]interface{}{
		"ok":    false,
		"found": "",
	}

	mailHostname := "mail." + domain
	ips, err := net.LookupIP(mailHostname)
	if err != nil || len(ips) == 0 {
		return result
	}

	// Format IPs found
	var ipStrings []string
	for _, ip := range ips {
		ipStrings = append(ipStrings, ip.String())
	}
	result["found"] = strings.Join(ipStrings, ", ")
	result["ok"] = true

	return result
}

// getDKIMPath returns the path for DKIM keys
func (s *Server) getDKIMPath() string {
	if s.config.Storage.DataDir != "" {
		return s.config.Storage.DataDir + "/dkim"
	}
	if s.config.Storage.MaildirPath != "" {
		return s.config.Storage.MaildirPath + "/../dkim"
	}
	return "/etc/mailserver/dkim"
}

// handleAPIStats returns stats as JSON for AJAX updates
func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.getStats(r.Context())
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get queue stats
	queueStats, err := s.queue.Stats(r.Context())
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get queue stats", err)
		// Continue with nil queue stats rather than failing entirely
	}

	// Get recent auth logs (last 5)
	type RecentAuth struct {
		Username string    `json:"username"`
		Success  bool      `json:"success"`
		Time     time.Time `json:"time"`
	}
	var recentAuth []RecentAuth
	authRows, err := s.db.QueryContext(r.Context(), `
		SELECT username, success, created_at
		FROM auth_log
		ORDER BY created_at DESC
		LIMIT 5
	`)
	if err == nil {
		defer authRows.Close()
		for authRows.Next() {
			var ra RecentAuth
			if err := authRows.Scan(&ra.Username, &ra.Success, &ra.Time); err == nil {
				recentAuth = append(recentAuth, ra)
			}
		}
	}

	// Get recent delivery logs (last 5)
	type RecentDelivery struct {
		From   string    `json:"from"`
		To     string    `json:"to"`
		Status string    `json:"status"`
		Time   time.Time `json:"time"`
	}
	var recentDelivery []RecentDelivery
	deliveryRows, err := s.db.QueryContext(r.Context(), `
		SELECT sender, recipient, status, created_at
		FROM delivery_log
		ORDER BY created_at DESC
		LIMIT 5
	`)
	if err == nil {
		defer deliveryRows.Close()
		for deliveryRows.Next() {
			var rd RecentDelivery
			if err := deliveryRows.Scan(&rd.From, &rd.To, &rd.Status, &rd.Time); err == nil {
				recentDelivery = append(recentDelivery, rd)
			}
		}
	}

	// Calculate uptime
	uptime := time.Since(s.startTime).Round(time.Second).String()

	w.Header().Set("Content-Type", "application/json")
	// Use proper JSON encoding to prevent injection
	response := struct {
		Users          int               `json:"users"`
		Domains        int               `json:"domains"`
		Messages       int               `json:"messages"`
		Queue          *queue.QueueStats `json:"queue"`
		RecentAuth     []RecentAuth      `json:"recent_auth"`
		RecentDelivery []RecentDelivery  `json:"recent_delivery"`
		Uptime         string            `json:"uptime"`
	}{
		Users:          stats.TotalUsers,
		Domains:        stats.TotalDomains,
		Messages:       stats.TotalMessages,
		Queue:          queueStats,
		RecentAuth:     recentAuth,
		RecentDelivery: recentDelivery,
		Uptime:         uptime,
	}
	json.NewEncoder(w).Encode(response)
}

// QueueMessage represents a message in the queue for display
type QueueMessage struct {
	ID          string
	Sender      string
	Recipients  string
	Status      string
	Attempts    int
	MaxAttempts int
	LastError   string
	NextAttempt time.Time
	CreatedAt   time.Time
}

// handleQueue shows the email queue
func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	if s.queue == nil {
		s.renderTemplate(w, "queue.html", map[string]interface{}{
			"Title": "Email Queue",
			"Error": "Queue not configured - Redis not available",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get queue statistics
	stats, err := s.queue.Stats(ctx)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get queue stats", err)
		s.renderTemplate(w, "queue.html", map[string]interface{}{
			"Title": "Email Queue",
			"Error": "Failed to get queue stats. Check server logs for details.",
		})
		return
	}

	// Get pending messages
	pendingMsgs, err := s.queue.ListPending(ctx, 50)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get pending messages", err)
	}
	pendingMessages := convertQueueMessages(pendingMsgs)

	// Get failed messages
	failedMsgs, err := s.queue.ListFailed(ctx, 50)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get failed messages", err)
	}
	failedMessages := convertQueueMessages(failedMsgs)

	// Get recently sent messages
	sentMsgs, err := s.queue.ListSent(ctx, 20)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get sent messages", err)
	}
	sentMessages := convertQueueMessages(sentMsgs)

	s.renderTemplate(w, "queue.html", map[string]interface{}{
		"Title":           "Email Queue",
		"Stats":           stats,
		"PendingMessages": pendingMessages,
		"FailedMessages":  failedMessages,
		"SentMessages":    sentMessages,
	})
}

// convertQueueMessages converts queue.Message to QueueMessage for display
func convertQueueMessages(msgs []*queue.Message) []QueueMessage {
	if msgs == nil {
		return nil
	}
	result := make([]QueueMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		result = append(result, QueueMessage{
			ID:          msg.ID,
			Sender:      msg.Sender,
			Recipients:  strings.Join(msg.Recipients, ", "),
			Status:      string(msg.Status),
			Attempts:    msg.Attempts,
			MaxAttempts: msg.MaxAttempts,
			LastError:   msg.LastError,
			NextAttempt: msg.NextAttempt,
			CreatedAt:   msg.CreatedAt,
		})
	}
	return result
}

// handleQueueRetry retries a failed message
func (s *Server) handleQueueRetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.queue == nil {
		http.Error(w, "Queue not configured", http.StatusServiceUnavailable)
		return
	}

	// Extract message ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.NotFound(w, r)
		return
	}
	msgID := parts[4]

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get the message and reschedule it for immediate retry
	msg, err := s.queue.GetMessage(ctx, msgID)
	if err != nil {
		http.Error(w, "Message not found: "+err.Error(), http.StatusNotFound)
		return
	}

	// Reset attempts and schedule for immediate retry
	msg.Attempts = 0
	msg.NextAttempt = time.Now()

	// Re-enqueue the message
	if err := s.queue.Enqueue(ctx, msg); err != nil {
		http.Error(w, "Failed to reschedule message. Check server logs for details.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/queue", http.StatusSeeOther)
}

// handleQueueDelete deletes a message from the queue
func (s *Server) handleQueueDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.queue == nil {
		http.Error(w, "Queue not configured", http.StatusServiceUnavailable)
		return
	}

	// Extract message ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.NotFound(w, r)
		return
	}
	msgID := parts[4]

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Mark the message as permanently failed (removes from active queues)
	if err := s.queue.Fail(ctx, msgID, "Manually deleted by admin"); err != nil {
		http.Error(w, "Failed to delete message. Check server logs for details.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/queue", http.StatusSeeOther)
}

// handleEmailPreview shows a preview of an email from the queue
func (s *Server) handleEmailPreview(w http.ResponseWriter, r *http.Request) {
	// Extract message ID from path: /admin/email/preview/{messageID}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}
	messageID := parts[4]

	// Get session ID for rate limiting
	cookie, err := r.Cookie("admin_session")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Rate limit: 10 previews per minute per session
	if !s.previewRateLimiter.Allow(cookie.Value) {
		http.Error(w, "Rate limit exceeded. Please wait before previewing more emails.", http.StatusTooManyRequests)
		return
	}

	// Get message from queue with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	msg, err := s.queue.GetMessage(ctx, messageID)
	if err != nil {
		http.Error(w, "Message not found", http.StatusNotFound)
		return
	}

	// Size check (prevent loading huge emails)
	if msg.Size > 512*1024 { // 512KB limit
		http.Error(w, "Message too large to preview (max 512KB)", http.StatusRequestEntityTooLarge)
		return
	}

	// Read message from file
	messageFile, err := os.Open(msg.MessagePath)
	if err != nil {
		http.Error(w, "Failed to read message file", http.StatusInternalServerError)
		return
	}
	defer messageFile.Close()

	// Parse email
	parsed, err := parseMessageContent(messageFile)
	if err != nil {
		http.Error(w, "Failed to parse message", http.StatusInternalServerError)
		return
	}

	// Sanitize HTML if present
	if parsed.HTMLBody != "" {
		parsed.HTMLBody = sanitizeHTML(parsed.HTMLBody)
	}

	// Audit log
	userID, _ := s.getSessionUserID(r)
	var username string
	s.db.QueryRowContext(ctx, "SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	s.auditLogger.Log(ctx, username, audit.EventConfigChange,
		"Email preview: "+messageID, nil, getIP(r))

	// Render template
	s.renderTemplate(w, "email_preview.html", map[string]interface{}{
		"Title":      "Email Preview",
		"MessageID":  messageID,
		"Sender":     msg.Sender,
		"Recipients": strings.Join(msg.Recipients, ", "),
		"Parsed":     parsed,
	})
}

// HealthStatus represents the health check response
type HealthStatus struct {
	Status    string            `json:"status"`
	Timestamp string            `json:"timestamp"`
	Uptime    string            `json:"uptime"`
	Services  map[string]string `json:"services"`
}

// handleHealth returns basic health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := HealthStatus{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Uptime:    time.Since(s.startTime).Round(time.Second).String(),
		Services:  make(map[string]string),
	}

	// Check database
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := s.db.PingContext(ctx); err != nil {
		status.Status = "degraded"
		status.Services["database"] = "error: " + err.Error()
	} else {
		status.Services["database"] = "ok"
	}

	// Check Redis queue if available
	if s.queue != nil {
		if _, err := s.queue.Stats(ctx); err != nil {
			status.Status = "degraded"
			status.Services["queue"] = "error: " + err.Error()
		} else {
			status.Services["queue"] = "ok"
		}
	} else {
		status.Services["queue"] = "not configured"
	}

	w.Header().Set("Content-Type", "application/json")
	if status.Status != "ok" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(status)
}

// handleReady returns readiness status for orchestration
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// Check database connection
	if err := s.db.PingContext(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready: database unavailable"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ready"))
}

// DNSCheckResult represents DNS check results
type DNSCheckResult struct {
	RecordType string `json:"record_type"`
	Status     string `json:"status"`
	Expected   string `json:"expected"`
	Actual     string `json:"actual"`
	Message    string `json:"message"`
}

// handleDNSCheck performs DNS verification for a domain
func (s *Server) handleDNSCheck(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		// Show form
		s.renderTemplate(w, "dns_check.html", map[string]interface{}{
			"Title": "DNS Check",
		})
		return
	}

	// Perform DNS checks using the dns package
	mailServer := s.config.Server.Hostname

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Use net package for DNS lookups
	results := []DNSCheckResult{}

	// Check MX records
	mxRecords, err := net.LookupMX(domain)
	if err != nil {
		results = append(results, DNSCheckResult{
			RecordType: "MX",
			Status:     "fail",
			Expected:   mailServer,
			Actual:     "",
			Message:    "No MX records found: " + err.Error(),
		})
	} else {
		found := false
		var actualMX string
		for _, mx := range mxRecords {
			actualMX += mx.Host + " "
			if strings.TrimSuffix(mx.Host, ".") == mailServer {
				found = true
			}
		}
		if found {
			results = append(results, DNSCheckResult{
				RecordType: "MX",
				Status:     "pass",
				Expected:   mailServer,
				Actual:     strings.TrimSpace(actualMX),
				Message:    "MX record correctly points to mail server",
			})
		} else {
			results = append(results, DNSCheckResult{
				RecordType: "MX",
				Status:     "fail",
				Expected:   mailServer,
				Actual:     strings.TrimSpace(actualMX),
				Message:    "MX record does not point to expected mail server",
			})
		}
	}

	// Check SPF record
	txtRecords, err := net.LookupTXT(domain)
	if err != nil {
		results = append(results, DNSCheckResult{
			RecordType: "SPF",
			Status:     "fail",
			Expected:   "v=spf1 ...",
			Actual:     "",
			Message:    "No TXT records found: " + err.Error(),
		})
	} else {
		foundSPF := false
		var spfRecord string
		for _, txt := range txtRecords {
			if strings.HasPrefix(txt, "v=spf1") {
				foundSPF = true
				spfRecord = txt
				break
			}
		}
		if foundSPF {
			results = append(results, DNSCheckResult{
				RecordType: "SPF",
				Status:     "pass",
				Expected:   "v=spf1 ...",
				Actual:     spfRecord,
				Message:    "SPF record found",
			})
		} else {
			results = append(results, DNSCheckResult{
				RecordType: "SPF",
				Status:     "fail",
				Expected:   "v=spf1 mx -all",
				Actual:     "",
				Message:    "No SPF record found",
			})
		}
	}

	// Check DKIM record
	dkimDomain := "mail._domainkey." + domain
	dkimRecords, err := net.LookupTXT(dkimDomain)
	if err != nil {
		results = append(results, DNSCheckResult{
			RecordType: "DKIM",
			Status:     "fail",
			Expected:   "v=DKIM1; ...",
			Actual:     "",
			Message:    "No DKIM record found at " + dkimDomain,
		})
	} else {
		foundDKIM := false
		var dkimRecord string
		for _, txt := range dkimRecords {
			if strings.Contains(txt, "DKIM1") || strings.Contains(txt, "p=") {
				foundDKIM = true
				dkimRecord = txt
				break
			}
		}
		if foundDKIM {
			results = append(results, DNSCheckResult{
				RecordType: "DKIM",
				Status:     "pass",
				Expected:   "v=DKIM1; ...",
				Actual:     dkimRecord[:min(len(dkimRecord), 50)] + "...",
				Message:    "DKIM record found",
			})
		} else {
			results = append(results, DNSCheckResult{
				RecordType: "DKIM",
				Status:     "fail",
				Expected:   "v=DKIM1; k=rsa; p=...",
				Actual:     "",
				Message:    "Invalid DKIM record",
			})
		}
	}

	// Check DMARC record
	dmarcDomain := "_dmarc." + domain
	dmarcRecords, err := net.LookupTXT(dmarcDomain)
	if err != nil {
		results = append(results, DNSCheckResult{
			RecordType: "DMARC",
			Status:     "warning",
			Expected:   "v=DMARC1; ...",
			Actual:     "",
			Message:    "No DMARC record found (recommended)",
		})
	} else {
		foundDMARC := false
		var dmarcRecord string
		for _, txt := range dmarcRecords {
			if strings.HasPrefix(txt, "v=DMARC1") {
				foundDMARC = true
				dmarcRecord = txt
				break
			}
		}
		if foundDMARC {
			results = append(results, DNSCheckResult{
				RecordType: "DMARC",
				Status:     "pass",
				Expected:   "v=DMARC1; ...",
				Actual:     dmarcRecord,
				Message:    "DMARC record found",
			})
		} else {
			results = append(results, DNSCheckResult{
				RecordType: "DMARC",
				Status:     "warning",
				Expected:   "v=DMARC1; p=quarantine; ...",
				Actual:     "",
				Message:    "Invalid DMARC record",
			})
		}
	}

	_ = ctx // Use context for future improvements

	s.renderTemplate(w, "dns_check.html", map[string]interface{}{
		"Title":   "DNS Check",
		"Domain":  domain,
		"Results": results,
	})
}

// handleTestEmail sends a test email
func (s *Server) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.renderTemplate(w, "test_email.html", map[string]interface{}{
			"Title": "Send Test Email",
		})
		return
	}

	// POST - send test email
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Bad request", formErrorStatus(err))
		return
	}

	recipient := r.PostForm.Get("recipient")
	if recipient == "" {
		s.renderTemplate(w, "test_email.html", map[string]interface{}{
			"Title": "Send Test Email",
			"Error": "Recipient email is required",
		})
		return
	}

	// Create a simple test message
	// Get first active domain for multi-domain support
	var testDomain string
	err := s.db.QueryRowContext(r.Context(),
		"SELECT name FROM domains WHERE is_active = TRUE ORDER BY id LIMIT 1",
	).Scan(&testDomain)
	if err != nil || testDomain == "" {
		testDomain = s.config.Server.Domain // fallback to config
	}

	from := "postmaster@" + testDomain
	subject := "Test Email from " + s.config.Server.Hostname
	body := "This is a test email sent from the mail server admin panel.\n\n" +
		"Server: " + s.config.Server.Hostname + "\n" +
		"Time: " + time.Now().Format(time.RFC1123) + "\n\n" +
		"If you received this email, your mail server is working correctly!"

	messageID := generateMessageID(testDomain)
	msg := "From: " + from + "\r\n" +
		"To: " + recipient + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Message-ID: <" + messageID + ">\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		body

	// Queue the message if queue is available
	if s.queue != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		// Write message to temp file in maildir
		tmpDir := s.config.Storage.MaildirPath
		if tmpDir == "" {
			tmpDir = "/tmp"
		}
		tmpFile, err := os.CreateTemp(tmpDir, "test-email-*.eml")
		if err != nil {
			s.renderTemplate(w, "test_email.html", map[string]interface{}{
				"Title": "Send Test Email",
				"Error": "Failed to create message file. Check server logs for details.",
			})
			return
		}
		tmpFile.WriteString(msg)
		tmpFile.Close()

		// Extract domain from recipient
		parts := strings.Split(recipient, "@")
		recipientDomain := ""
		if len(parts) == 2 {
			recipientDomain = parts[1]
		}

		// Queue the message
		queueMsg := &queue.Message{
			Sender:      from,
			Recipients:  []string{recipient},
			MessagePath: tmpFile.Name(),
			Size:        int64(len(msg)),
			Domain:      recipientDomain,
		}

		if err := s.queue.Enqueue(ctx, queueMsg); err != nil {
			os.Remove(tmpFile.Name())
			s.renderTemplate(w, "test_email.html", map[string]interface{}{
				"Title": "Send Test Email",
				"Error": "Failed to queue message. Check server logs for details.",
			})
			return
		}

		s.renderTemplate(w, "test_email.html", map[string]interface{}{
			"Title":   "Send Test Email",
			"Success": "Test email queued for delivery to " + recipient,
		})
	} else {
		s.renderTemplate(w, "test_email.html", map[string]interface{}{
			"Title": "Send Test Email",
			"Error": "Queue not configured - cannot send email",
		})
	}
}

// generateMessageID creates a unique message ID
func generateMessageID(domain string) string {
	return time.Now().Format("20060102150405") + "." + strconv.FormatInt(time.Now().UnixNano(), 36) + "@" + domain
}

// generateDomainVerificationToken creates a random token for domain ownership verification
// The token should be added as a TXT record: _mailserver-verify.domain.com
func generateDomainVerificationToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based token if crypto/rand fails
		return "mailserver-verify=" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "mailserver-verify=" + hex.EncodeToString(b)
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// handleAuditLogs shows the audit log
func (s *Server) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if s.auditLogger == nil {
		s.renderTemplate(w, "audit_logs.html", map[string]interface{}{
			"Title": "Audit Logs",
			"Error": "Audit logging not configured",
		})
		return
	}

	// Parse filter parameters
	filter := audit.QueryFilter{
		Limit: 100,
	}

	if actor := r.URL.Query().Get("actor"); actor != "" {
		filter.Actor = actor
	}
	if action := r.URL.Query().Get("action"); action != "" {
		filter.Action = audit.EventType(action)
	}
	if target := r.URL.Query().Get("target"); target != "" {
		filter.Target = target
	}

	events, err := s.auditLogger.Query(r.Context(), filter)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to query audit logs", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Convert to display format
	type AuditLogEntry struct {
		ID        int64
		Timestamp time.Time
		Actor     string
		Action    string
		Target    string
		Details   string
		IPAddress string
	}

	var logs []AuditLogEntry
	for _, e := range events {
		logs = append(logs, AuditLogEntry{
			ID:        e.ID,
			Timestamp: e.Timestamp,
			Actor:     e.Actor,
			Action:    string(e.Action),
			Target:    e.Target,
			Details:   e.Details,
			IPAddress: e.IPAddress,
		})
	}

	data := map[string]interface{}{
		"Title":        "Audit Logs",
		"Logs":         logs,
		"FilterActor":  r.URL.Query().Get("actor"),
		"FilterAction": r.URL.Query().Get("action"),
	}

	s.renderTemplate(w, "audit_logs.html", data)
}

// handleSystem shows the system management page
func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	// Get system stats
	var domainCount, userCount, emailCount int64

	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM domains").Scan(&domainCount)
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users").Scan(&userCount)

	// Get data directory size
	dataDir := s.config.Storage.DataDir
	var dataSizeStr string
	if size, err := getDirSize(dataDir); err == nil {
		dataSizeStr = formatBytes(size)
	} else {
		dataSizeStr = "Unknown"
	}

	// Get DKIM auto-rotate setting from database or config
	var autoRotateDays int = 90 // default

	// Check for recent backups
	backupDir := filepath.Join(dataDir, "backups")
	var lastBackup string
	if entries, err := os.ReadDir(backupDir); err == nil && len(entries) > 0 {
		// Find most recent
		var newest time.Time
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil {
				if info.ModTime().After(newest) {
					newest = info.ModTime()
					lastBackup = newest.Format("Jan 02, 2006 15:04")
				}
			}
		}
	}
	if lastBackup == "" {
		lastBackup = "Never"
	}

	data := map[string]interface{}{
		"Title":           "System",
		"DomainCount":     domainCount,
		"UserCount":       userCount,
		"EmailCount":      emailCount,
		"DataSize":        dataSizeStr,
		"DataDir":         dataDir,
		"LastBackup":      lastBackup,
		"AutoRotateDays":  autoRotateDays,
		"ServerUptime":    time.Since(s.startTime).Round(time.Second).String(),
		"ServerStartTime": s.startTime.Format("Jan 02, 2006 15:04:05"),
	}

	s.renderTemplate(w, "system.html", data)
}

// handleBackup creates and downloads a backup
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dataDir := s.config.Storage.DataDir

	// Create backup in temp file
	tempFile, err := os.CreateTemp("", "mailserver-backup-*.tar.gz")
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to create temp file for backup", err)
		http.Error(w, "Failed to create backup", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Create tar.gz
	gzWriter := gzip.NewWriter(tempFile)
	tarWriter := tar.NewWriter(gzWriter)

	// Backup database
	dbPath := s.config.Storage.DatabasePath
	if err := addFileToTar(tarWriter, dbPath, "metadata.db"); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to backup database", err)
	}

	// Backup maildir
	maildirPath := s.config.Storage.MaildirPath
	if err := addDirToTar(tarWriter, maildirPath, "maildir"); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to backup maildir", err)
	}

	// Backup DKIM keys
	dkimPath := filepath.Join(dataDir, "dkim")
	if err := addDirToTar(tarWriter, dkimPath, "dkim"); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to backup DKIM keys", err)
	}

	tarWriter.Close()
	gzWriter.Close()

	// Get file size
	tempFile.Seek(0, 0)
	stat, _ := tempFile.Stat()

	// Log the backup
	if s.auditLogger != nil {
		s.auditLogger.Log(r.Context(), getSessionUsername(r), audit.EventConfigChange, "system", map[string]interface{}{"action": "backup"}, getIP(r))
	}

	// Send file as download
	filename := fmt.Sprintf("mailserver-backup-%s.tar.gz", time.Now().Format("2006-01-02-150405"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))

	io.Copy(w, tempFile)
}

// handleRestore handles backup restoration
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form (max 500MB)
	if err := parseMultipartFormWithLimit(w, r, maxAdminMultipartBody, maxAdminMultipartBody); err != nil {
		http.Error(w, "Failed to parse form", formErrorStatus(err))
		return
	}

	file, header, err := r.FormFile("backup")
	if err != nil {
		http.Error(w, "No backup file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Verify it's a tar.gz file
	if !strings.HasSuffix(header.Filename, ".tar.gz") {
		http.Error(w, "Invalid backup file format. Expected .tar.gz", http.StatusBadRequest)
		return
	}

	// Save to temp file
	tempFile, err := os.CreateTemp("", "mailserver-restore-*.tar.gz")
	if err != nil {
		http.Error(w, "Failed to process backup", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, file); err != nil {
		http.Error(w, "Failed to save backup file", http.StatusInternalServerError)
		return
	}

	// Extract backup
	tempFile.Seek(0, 0)
	dataDir := s.config.Storage.DataDir

	if err := extractBackup(tempFile, dataDir); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to extract backup", err)
		http.Error(w, "Failed to restore backup: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Log the restore
	if s.auditLogger != nil {
		s.auditLogger.Log(r.Context(), getSessionUsername(r), audit.EventConfigChange, "system", map[string]interface{}{"action": "restore"}, getIP(r))
	}

	// Return success with redirect instruction
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Backup restored successfully. Please restart the server for changes to take effect.",
	})
}

// handleDKIMAutoRotate triggers DKIM key rotation for old keys
func (s *Server) handleDKIMAutoRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse days parameter
	days := 90 // default
	if d := r.PostForm.Get("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	threshold := time.Now().AddDate(0, 0, -days)
	dkimPath := s.getDKIMPath()

	// Get domains with old keys
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, name, dkim_key_created_at, COALESCE(dkim_storage_type, 'file')
		FROM domains
		WHERE is_active = TRUE
		  AND dkim_key_created_at IS NOT NULL
		  AND dkim_key_created_at < ?
	`, threshold)
	if err != nil {
		http.Error(w, "Failed to query domains", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rotated []string
	var errors []string

	for rows.Next() {
		var id int64
		var name string
		var createdAt time.Time
		var storageType string

		if err := rows.Scan(&id, &name, &createdAt, &storageType); err != nil {
			continue
		}

		// Create key store and rotate
		store := security.NewKeyStore(storageType, dkimPath, s.db)
		newSelector := fmt.Sprintf("mail%d", time.Now().Unix())

		_, err := security.GenerateAndSaveKey(r.Context(), store, name, newSelector, 2048)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}

		rotated = append(rotated, name)
	}

	// Log the rotation
	if s.auditLogger != nil && len(rotated) > 0 {
		s.auditLogger.Log(r.Context(), getSessionUsername(r), audit.EventConfigChange, "dkim",
			map[string]interface{}{"action": "auto-rotate", "count": len(rotated), "domains": rotated}, getIP(r))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": len(errors) == 0,
		"rotated": rotated,
		"errors":  errors,
		"message": fmt.Sprintf("Rotated %d keys. Remember to update DNS records!", len(rotated)),
	})
}

// ============================================================================
// Domain Add Wizard Handlers
// ============================================================================

// wizardState holds temporary state during domain creation wizard
type wizardState struct {
	Domain      string
	DKIMKey     string // PEM-encoded private key
	DKIMPublic  string // DNS record value
	Selector    string
	Bits        int
	StorageType string
	CreatedAt   time.Time
}

var (
	wizardStates   = make(map[string]*wizardState)
	wizardStatesMu sync.Mutex
)

// handleDomainWizardValidate validates domain name in step 1
func (s *Server) handleDomainWizardValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Domain string `json:"domain"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid": false,
			"error": "Invalid request body",
		})
		return
	}

	// Validate domain format
	domain := strings.TrimSpace(strings.ToLower(req.Domain))
	if domain == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid": false,
			"error": "Domain name is required",
		})
		return
	}

	if err := validation.Domain(domain); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	// Check if domain already exists
	var count int
	err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM domains WHERE name = ?", domain).Scan(&count)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid": false,
			"error": "Database error",
		})
		return
	}

	if count > 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid": false,
			"error": "Domain already exists",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid":  true,
		"domain": domain,
	})
}

// handleDomainWizardDNSRecords generates DNS records for step 2
func (s *Server) handleDomainWizardDNSRecords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Domain   string `json:"domain"`
		Selector string `json:"selector"`
		Bits     int    `json:"bits"`
		Storage  string `json:"storage"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	domain := strings.TrimSpace(strings.ToLower(req.Domain))
	selector := req.Selector
	if selector == "" {
		selector = "mail"
	}
	bits := req.Bits
	if bits != 4096 {
		bits = 2048
	}
	storage := req.Storage
	if storage == "" {
		storage = "database"
	}

	// Generate DKIM key pair (temporary, not saved yet)
	privateKey, err := security.GenerateDKIMKey(bits)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to generate DKIM key: " + err.Error(),
		})
		return
	}

	// Format public key for DNS
	dkimPublic, err := security.FormatDKIMPublicKey(&privateKey.PublicKey)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to format DKIM public key: " + err.Error(),
		})
		return
	}

	// Store state for later use
	stateID := fmt.Sprintf("%s-%d", domain, time.Now().UnixNano())
	wizardStatesMu.Lock()
	wizardStates[stateID] = &wizardState{
		Domain:      domain,
		DKIMKey:     encodePEMPrivateKey(privateKey),
		DKIMPublic:  dkimPublic,
		Selector:    selector,
		Bits:        bits,
		StorageType: storage,
		CreatedAt:   time.Now(),
	}
	wizardStatesMu.Unlock()

	// Clean up old states (older than 1 hour)
	go cleanupWizardStates()

	// Generate all DNS records
	hostname := s.config.Server.Hostname

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"stateId": stateID,
		"records": map[string]interface{}{
			"mx": map[string]string{
				"name":     domain,
				"type":     "MX",
				"priority": "10",
				"value":    hostname,
			},
			"spf": map[string]string{
				"name":  domain,
				"type":  "TXT",
				"value": fmt.Sprintf("v=spf1 mx a:%s -all", hostname),
			},
			"dkim": map[string]string{
				"name":  fmt.Sprintf("%s._domainkey.%s", selector, domain),
				"type":  "TXT",
				"value": dkimPublic,
			},
			"dmarc": map[string]string{
				"name":  fmt.Sprintf("_dmarc.%s", domain),
				"type":  "TXT",
				"value": fmt.Sprintf("v=DMARC1; p=quarantine; rua=mailto:postmaster@%s", domain),
			},
		},
		"hostname": hostname,
		"selector": selector,
	})
}

// handleDomainWizardVerify verifies DNS configuration in step 3
func (s *Server) handleDomainWizardVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Domain   string `json:"domain"`
		StateID  string `json:"stateId"`
		Selector string `json:"selector"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	domain := strings.TrimSpace(strings.ToLower(req.Domain))
	selector := req.Selector
	if selector == "" {
		selector = "mail"
	}

	// Get expected DKIM from state if available
	var expectedDKIM string
	wizardStatesMu.Lock()
	if state, ok := wizardStates[req.StateID]; ok {
		expectedDKIM = state.DKIMPublic
	}
	wizardStatesMu.Unlock()

	// Verify all DNS records
	mxResult := s.verifyMXRecord(domain, s.config.Server.Hostname)
	spfResult := s.verifySPFRecord(domain)
	dkimResult := s.verifyDKIMRecord(domain, selector, expectedDKIM)
	dmarcResult := s.verifyDMARCRecord(domain)

	// Determine overall status
	allOk := mxResult["ok"].(bool) && spfResult["ok"].(bool) && dkimResult["ok"].(bool) && dmarcResult["ok"].(bool)
	requiredOk := mxResult["ok"].(bool) && spfResult["ok"].(bool) // MX and SPF are required

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"allOk":      allOk,
		"requiredOk": requiredOk,
		"mx":         mxResult,
		"spf":        spfResult,
		"dkim":       dkimResult,
		"dmarc":      dmarcResult,
	})
}

// handleDomainWizardComplete creates the domain in step 4
func (s *Server) handleDomainWizardComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Domain   string `json:"domain"`
		StateID  string `json:"stateId"`
		Selector string `json:"selector"`
		Bits     int    `json:"bits"`
		Storage  string `json:"storage"`
		Skipped  bool   `json:"skipped"` // User skipped DNS verification
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid request",
		})
		return
	}

	domain := strings.TrimSpace(strings.ToLower(req.Domain))
	selector := req.Selector
	if selector == "" {
		selector = "mail"
	}

	// Get wizard state
	wizardStatesMu.Lock()
	state, hasState := wizardStates[req.StateID]
	if hasState {
		delete(wizardStates, req.StateID) // Clean up
	}
	wizardStatesMu.Unlock()

	// Verify DNS if not skipped
	var dnsStatus string
	var mxVerified, spfVerified, dkimVerified, dmarcVerified int

	if !req.Skipped {
		mxResult := s.verifyMXRecord(domain, s.config.Server.Hostname)
		spfResult := s.verifySPFRecord(domain)
		var expectedDKIM string
		if hasState {
			expectedDKIM = state.DKIMPublic
		}
		dkimResult := s.verifyDKIMRecord(domain, selector, expectedDKIM)
		dmarcResult := s.verifyDMARCRecord(domain)

		if mxResult["ok"].(bool) {
			mxVerified = 1
		}
		if spfResult["ok"].(bool) {
			spfVerified = 1
		}
		if dkimResult["ok"].(bool) {
			dkimVerified = 1
		}
		if dmarcResult["ok"].(bool) {
			dmarcVerified = 1
		}

		if mxVerified == 1 && spfVerified == 1 && dkimVerified == 1 && dmarcVerified == 1 {
			dnsStatus = "ready"
		} else if mxVerified == 1 || spfVerified == 1 {
			dnsStatus = "partial"
		} else {
			dnsStatus = "pending"
		}
	} else {
		dnsStatus = "pending"
	}

	// Insert domain with auto-generated mail hostname
	mailHostname := "mail." + domain
	result, err := s.db.ExecContext(r.Context(),
		`INSERT INTO domains (name, dkim_selector, dkim_storage_type, dns_status,
		 dns_mx_verified, dns_spf_verified, dns_dkim_verified, dns_dmarc_verified, dns_last_checked, mail_hostname)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		domain, selector, req.Storage, dnsStatus,
		mxVerified, spfVerified, dkimVerified, dmarcVerified, time.Now(), mailHostname)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to create domain: " + err.Error(),
		})
		return
	}

	domainID, _ := result.LastInsertId()

	// Save DKIM key if we have state
	if hasState && state.DKIMKey != "" {
		dkimPath := s.getDKIMPath()
		store := security.NewKeyStore(req.Storage, dkimPath, s.db)

		privateKey, err := decodePEMPrivateKey(state.DKIMKey)
		if err == nil {
			err = store.SaveKey(r.Context(), domain, privateKey, selector, "rsa")
			if err != nil {
				s.logger.Error("Failed to save DKIM key", "domain", domain, "error", err.Error())
			}
		}
	}

	// Audit log
	if s.auditLogger != nil {
		s.auditLogger.Log(r.Context(), getSessionUsername(r), audit.EventConfigChange, "domain",
			map[string]interface{}{
				"action":     "create",
				"domain":     domain,
				"dns_status": dnsStatus,
				"skipped":    req.Skipped,
			}, getIP(r))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"domainId":  domainID,
		"domain":    domain,
		"dnsStatus": dnsStatus,
		"redirect":  "/admin/domains",
	})
}

// cleanupWizardStates removes wizard states older than 1 hour
func cleanupWizardStates() {
	wizardStatesMu.Lock()
	defer wizardStatesMu.Unlock()

	cutoff := time.Now().Add(-1 * time.Hour)
	for id, state := range wizardStates {
		if state.CreatedAt.Before(cutoff) {
			delete(wizardStates, id)
		}
	}
}

// encodePEMPrivateKey encodes an RSA private key to PEM format
func encodePEMPrivateKey(key *rsa.PrivateKey) string {
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	return string(pem.EncodeToMemory(block))
}

// decodePEMPrivateKey decodes a PEM-encoded RSA private key
func decodePEMPrivateKey(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// Helper functions for backup/restore

func addFileToTar(tw *tar.Writer, filePath, name string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    name,
		Size:    stat.Size(),
		Mode:    int64(stat.Mode()),
		ModTime: stat.ModTime(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, file)
	return err
}

func addDirToTar(tw *tar.Writer, srcDir, prefix string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return nil
		}

		tarPath := filepath.Join(prefix, relPath)

		if info.IsDir() {
			header := &tar.Header{
				Name:     tarPath + "/",
				Mode:     int64(info.Mode()),
				ModTime:  info.ModTime(),
				Typeflag: tar.TypeDir,
			}
			return tw.WriteHeader(header)
		}

		header := &tar.Header{
			Name:    tarPath,
			Size:    info.Size(),
			Mode:    int64(info.Mode()),
			ModTime: info.ModTime(),
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return nil // Skip unreadable files
		}
		defer file.Close()

		_, err = io.Copy(tw, file)
		return err
	})
}

func extractBackup(file *os.File, destDir string) error {
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("invalid gzip file: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading tar: %w", err)
		}

		if header.Size < 0 {
			return fmt.Errorf("invalid archive entry size for %q", header.Name)
		}

		targetPath, err := safeExtractBackupPath(destDir, header.Name)
		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}

			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}

			if _, err := io.CopyN(outFile, tarReader, header.Size); err != nil {
				outFile.Close()
				return err
			}
			if err := outFile.Close(); err != nil {
				return err
			}

			// Restore permissions
			mode, err := safecast.Int64ToFileMode(header.Mode)
			if err != nil {
				return err
			}
			if err := os.Chmod(targetPath, mode); err != nil {
				return err
			}
		}
	}

	return nil
}

func safeExtractBackupPath(destDir, headerName string) (string, error) {
	cleanName := filepath.Clean(headerName)
	if cleanName == "." || cleanName == "" {
		return "", fmt.Errorf("invalid archive entry path %q", headerName)
	}
	if filepath.IsAbs(cleanName) {
		return "", fmt.Errorf("archive entry %q uses absolute path", headerName)
	}

	destRoot, err := filepath.Abs(destDir)
	if err != nil {
		return "", err
	}

	targetPath := filepath.Join(destRoot, cleanName)
	relPath, err := filepath.Rel(destRoot, targetPath)
	if err != nil {
		return "", err
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes destination", headerName)
	}

	return targetPath, nil
}

func getDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func getSessionUsername(r *http.Request) string {
	cookie, err := r.Cookie("admin_session")
	if err != nil {
		return "unknown"
	}
	// In a real implementation, look up the session
	// For now, return a placeholder
	_ = cookie
	return "admin"
}
