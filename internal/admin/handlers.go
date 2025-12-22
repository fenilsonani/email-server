package admin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/validation"
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

// handleLogin handles admin login with rate limiting
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	clientIP := getIP(r)

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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

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

	// Check if user is admin
	var isAdmin bool
	err = s.db.QueryRowContext(r.Context(), "SELECT is_admin FROM users WHERE id = ?", user.ID).Scan(&isAdmin)
	if err != nil || !isAdmin {
		s.rateLimiter.RecordFailure(clientIP)
		s.renderTemplate(w, "login.html", map[string]interface{}{
			"Title": "Admin Login",
			"Error": "Access denied - admin rights required",
		})
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
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   604800, // 7 days
	})

	s.logger.Info("Admin login successful", "ip", clientIP, "username", username)
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
			s.logger.ErrorContext(r.Context(), "Failed to scan user row", err)
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	domainIDStr := r.FormValue("domain_id")
	isAdmin := r.FormValue("is_admin") == "on"

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

	user, err := s.authenticator.CreateUser(r.Context(), username, password, domainID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to create user", err)
		http.Error(w, "Failed to create user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Initialize default mailboxes for the new user
	if err := s.store.InitializeUserMailboxes(r.Context(), user.ID); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to initialize mailboxes", err)
		// User was created but mailboxes failed - log but don't fail the request
	}

	if isAdmin {
		s.db.ExecContext(r.Context(), "UPDATE users SET is_admin = TRUE WHERE id = ?", user.ID)
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
	userID, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		http.Error(w, "Invalid user ID format", http.StatusBadRequest)
		return
	}

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
	var updateErr error
	_, updateErr = s.db.ExecContext(r.Context(), "UPDATE users SET is_admin = ? WHERE id = ?", isAdmin, userID)
	if updateErr != nil {
		http.Error(w, "Failed to update user", http.StatusInternalServerError)
		return
	}

	// Update password if provided
	if password != "" {
		// Validate password
		if err := validation.Password(password); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
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
			s.logger.ErrorContext(r.Context(), "Failed to scan domain row", err)
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
			s.logger.ErrorContext(r.Context(), "Failed to scan auth log row", err)
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
			s.logger.ErrorContext(r.Context(), "Failed to scan delivery log row", err)
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

	// Validate domain name
	if err := validation.Domain(name); err != nil {
		s.renderTemplate(w, "domain_form.html", map[string]interface{}{
			"Title": "Add Domain",
			"Error": err.Error(),
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
			"Error": "Failed to get queue stats: " + err.Error(),
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
		http.Error(w, "Failed to reschedule message: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "Failed to delete message: "+err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/queue", http.StatusSeeOther)
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	recipient := r.FormValue("recipient")
	if recipient == "" {
		s.renderTemplate(w, "test_email.html", map[string]interface{}{
			"Title": "Send Test Email",
			"Error": "Recipient email is required",
		})
		return
	}

	// Create a simple test message
	from := "postmaster@" + s.config.Server.Domain
	subject := "Test Email from " + s.config.Server.Hostname
	body := "This is a test email sent from the mail server admin panel.\n\n" +
		"Server: " + s.config.Server.Hostname + "\n" +
		"Time: " + time.Now().Format(time.RFC1123) + "\n\n" +
		"If you received this email, your mail server is working correctly!"

	messageID := generateMessageID(s.config.Server.Domain)
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
				"Error": "Failed to create message file: " + err.Error(),
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
				"Error": "Failed to queue message: " + err.Error(),
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

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
