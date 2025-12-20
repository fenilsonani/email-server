package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

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

// handleLogin handles admin login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Admin Login",
		})
		return
	}

	// POST - handle login
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	user, err := s.authenticator.Authenticate(r.Context(), username, password)
	if err != nil {
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Admin Login",
			"Error": "Invalid credentials",
		})
		return
	}

	// Check if user is admin
	var isAdmin bool
	err = s.db.QueryRowContext(r.Context(), "SELECT is_admin FROM users WHERE id = ?", user.ID).Scan(&isAdmin)
	if err != nil || !isAdmin {
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Admin Login",
			"Error": "Access denied - admin rights required",
		})
		return
	}

	// Create session
	token := s.createSession(user.ID)
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24 hours
	})

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
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT u.id, u.username, d.name as domain, u.is_admin, u.created_at
		FROM users u
		JOIN domains d ON u.domain_id = d.id
		ORDER BY d.name, u.username
	`)
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
		CreatedAt time.Time
	}

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Domain, &u.IsAdmin, &u.CreatedAt); err != nil {
			continue
		}
		u.Email = u.Username + "@" + u.Domain
		users = append(users, u)
	}

	s.renderTemplate(w, "users.html", map[string]interface{}{
		"Title": "Users",
		"Users": users,
	})
}

// handleUserAdd handles adding a new user
func (s *Server) handleUserAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Get domains for dropdown
		rows, _ := s.db.QueryContext(r.Context(), "SELECT id, name FROM domains ORDER BY name")
		defer rows.Close()

		type Domain struct {
			ID   int64
			Name string
		}
		var domains []Domain
		for rows.Next() {
			var d Domain
			rows.Scan(&d.ID, &d.Name)
			domains = append(domains, d)
		}

		s.renderTemplate(w, "user_form.html", map[string]interface{}{
			"Title":   "Add User",
			"Domains": domains,
		})
		return
	}

	// POST - create user
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	domainID, _ := strconv.ParseInt(r.FormValue("domain_id"), 10, 64)
	isAdmin := r.FormValue("is_admin") == "on"

	if username == "" || password == "" || domainID == 0 {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	_, err := s.authenticator.CreateUser(r.Context(), username, password, domainID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to create user", err)
		http.Error(w, "Failed to create user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if isAdmin {
		s.db.ExecContext(r.Context(), "UPDATE users SET is_admin = TRUE WHERE username = ?", username)
	}

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
	userID, _ := strconv.ParseInt(parts[4], 10, 64)

	if r.Method == http.MethodGet {
		var username, domain string
		var isAdmin bool
		err := s.db.QueryRowContext(r.Context(),
			`SELECT u.username, d.name, u.is_admin FROM users u
			 JOIN domains d ON u.domain_id = d.id WHERE u.id = ?`, userID).
			Scan(&username, &domain, &isAdmin)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		s.renderTemplate(w, "user_edit.html", map[string]interface{}{
			"Title":    "Edit User",
			"UserID":   userID,
			"Username": username,
			"Email":    username + "@" + domain,
			"IsAdmin":  isAdmin,
		})
		return
	}

	// POST - update user
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	password := r.FormValue("password")
	isAdmin := r.FormValue("is_admin") == "on"

	// Update admin status
	_, err := s.db.ExecContext(r.Context(), "UPDATE users SET is_admin = ? WHERE id = ?", isAdmin, userID)
	if err != nil {
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	// Update password if provided
	if password != "" {
		if err := s.authenticator.UpdatePassword(r.Context(), userID, password); err != nil {
			http.Error(w, "Failed to update password", http.StatusInternalServerError)
			return
		}
	}

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
	userID, _ := strconv.ParseInt(parts[4], 10, 64)

	_, err := s.db.ExecContext(r.Context(), "DELETE FROM users WHERE id = ?", userID)
	if err != nil {
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// handleDomains shows domain list
func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT d.id, d.name, d.created_at,
			(SELECT COUNT(*) FROM users WHERE domain_id = d.id) as user_count
		FROM domains d
		ORDER BY d.name
	`)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get domains", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Domain struct {
		ID        int64
		Name      string
		UserCount int
		CreatedAt time.Time
	}

	var domains []Domain
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.Name, &d.CreatedAt, &d.UserCount); err != nil {
			continue
		}
		domains = append(domains, d)
	}

	s.renderTemplate(w, "domains.html", map[string]interface{}{
		"Title":   "Domains",
		"Domains": domains,
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
	userID, _ := strconv.ParseInt(parts[3], 10, 64)

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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	content := r.FormValue("content")
	action := r.FormValue("action")

	switch action {
	case "create":
		_, err := s.sieveStore.CreateScript(r.Context(), userID, name, content)
		if err != nil {
			http.Error(w, "Failed to create script: "+err.Error(), http.StatusBadRequest)
			return
		}
	case "update":
		err := s.sieveStore.UpdateScript(r.Context(), userID, name, content)
		if err != nil {
			http.Error(w, "Failed to update script: "+err.Error(), http.StatusBadRequest)
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

// handleAuthLogs shows authentication logs
func (s *Server) handleAuthLogs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, username, remote_addr, protocol, success, failure_reason, created_at
		FROM auth_log
		ORDER BY created_at DESC
		LIMIT 100
	`)
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
			continue
		}
		logs = append(logs, l)
	}

	s.renderTemplate(w, "auth_logs.html", map[string]interface{}{
		"Title": "Authentication Logs",
		"Logs":  logs,
	})
}

// handleDeliveryLogs shows delivery logs
func (s *Server) handleDeliveryLogs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, message_id, sender, recipient, status, smtp_code, error_message, created_at
		FROM delivery_log
		ORDER BY created_at DESC
		LIMIT 100
	`)
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
			continue
		}
		logs = append(logs, l)
	}

	s.renderTemplate(w, "delivery_logs.html", map[string]interface{}{
		"Title": "Delivery Logs",
		"Logs":  logs,
	})
}

// handleDomainAdd handles adding a new domain
func (s *Server) handleDomainAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.renderTemplate(w, "domain_form.html", map[string]interface{}{
			"Title": "Add Domain",
		})
		return
	}

	// POST - create domain
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(strings.ToLower(r.FormValue("name")))
	if name == "" {
		s.renderTemplate(w, "domain_form.html", map[string]interface{}{
			"Title": "Add Domain",
			"Error": "Domain name is required",
		})
		return
	}

	// Check if domain already exists
	var exists int
	err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM domains WHERE name = ?", name).Scan(&exists)
	if err == nil && exists > 0 {
		s.renderTemplate(w, "domain_form.html", map[string]interface{}{
			"Title": "Add Domain",
			"Error": "Domain already exists",
		})
		return
	}

	// Insert domain
	_, err = s.db.ExecContext(r.Context(),
		"INSERT INTO domains (name, dkim_selector) VALUES (?, ?)",
		name, "mail",
	)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to create domain", err)
		s.renderTemplate(w, "domain_form.html", map[string]interface{}{
			"Title": "Add Domain",
			"Error": "Failed to create domain: " + err.Error(),
		})
		return
	}

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
	domainID, _ := strconv.ParseInt(parts[4], 10, 64)

	// Delete domain (users will be cascade deleted due to foreign key)
	_, err := s.db.ExecContext(r.Context(), "DELETE FROM domains WHERE id = ?", domainID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to delete domain", err)
		http.Error(w, "Failed to delete domain", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/domains", http.StatusSeeOther)
}

// handleAPIStats returns stats as JSON for AJAX updates
func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.getStats(r.Context())
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// Simple JSON response
	w.Write([]byte(`{"users":` + strconv.Itoa(stats.TotalUsers) +
		`,"domains":` + strconv.Itoa(stats.TotalDomains) +
		`,"messages":` + strconv.Itoa(stats.TotalMessages) + `}`))
}
