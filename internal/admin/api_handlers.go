package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/audit"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/security"
	"github.com/fenilsonani/email-server/internal/validation"
)

// --- Dashboard ---

// handleAPIGetStats returns dashboard statistics as JSON
func (s *Server) handleAPIGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	stats, err := s.getStats(r.Context())
	if err != nil {
		s.logger.Error("Failed to get stats", "error", err.Error())
		s.jsonError(w, http.StatusInternalServerError, "Failed to get statistics")
		return
	}

	// Calculate uptime
	uptime := time.Since(s.startTime)

	// Health alerts
	var failedLogins24h int
	s.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM auth_log WHERE success = 0 AND created_at >= datetime('now', '-1 day')`).Scan(&failedLogins24h)

	var bouncedLast24h int
	s.db.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM delivery_log WHERE (status='bounced' OR status='failed') AND created_at >= datetime('now', '-1 day')`).Scan(&bouncedLast24h)

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"total_users":        stats.TotalUsers,
		"total_domains":      stats.TotalDomains,
		"total_messages":     stats.TotalMessages,
		"queue_pending":      stats.QueuePending,
		"queue_failed":       stats.QueueFailed,
		"total_lists":        stats.TotalLists,
		"total_list_members": stats.TotalListMembers,
		"pending_moderation": stats.PendingModeration,
		"uptime_seconds":     int(uptime.Seconds()),
		"uptime_human":       formatUptime(uptime),
		"recent_activity":    stats.RecentActivity,
		"server_hostname":    s.config.Server.Hostname,
		"failed_logins_24h":  failedLogins24h,
		"bounced_24h":        bouncedLast24h,
	})
}

func formatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// --- Users ---

// APIUser represents a user in API responses
type APIUser struct {
	ID         int64     `json:"id"`
	Username   string    `json:"username"`
	Domain     string    `json:"domain"`
	Email      string    `json:"email"`
	IsAdmin    bool      `json:"is_admin"`
	QuotaBytes int64     `json:"quota_bytes"`
	UsedBytes  int64     `json:"used_bytes"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Server) handleAPIUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetUsers(w, r)
	case http.MethodPost:
		s.handleAPICreateUser(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIGetUsers(w http.ResponseWriter, r *http.Request) {
	pagination := getPaginationParams(r)
	filterUsername := r.URL.Query().Get("username")
	filterDomain := r.URL.Query().Get("domain")
	filterIsAdmin := r.URL.Query().Get("is_admin")

	query := `SELECT u.id, u.username, d.name as domain, u.is_admin, COALESCE(u.quota_bytes, 1073741824), COALESCE(u.used_bytes, 0), u.created_at
		FROM users u JOIN domains d ON u.domain_id = d.id WHERE 1=1`
	countQuery := `SELECT COUNT(*) FROM users u JOIN domains d ON u.domain_id = d.id WHERE 1=1`
	args := []interface{}{}

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
	if filterIsAdmin == "1" {
		query += " AND u.is_admin = 1"
		countQuery += " AND u.is_admin = 1"
	}

	var totalCount int
	if err := s.db.QueryRowContext(r.Context(), countQuery, args...).Scan(&totalCount); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to count users")
		return
	}

	query += " ORDER BY d.name, u.username LIMIT ? OFFSET ?"
	args = append(args, pagination.PageSize, pagination.Offset)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query users")
		return
	}
	defer rows.Close()

	users := []APIUser{}
	for rows.Next() {
		var u APIUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Domain, &u.IsAdmin, &u.QuotaBytes, &u.UsedBytes, &u.CreatedAt); err != nil {
			continue
		}
		u.Email = u.Username + "@" + u.Domain
		users = append(users, u)
	}

	totalPages := (totalCount + pagination.PageSize - 1) / pagination.PageSize
	if totalPages == 0 {
		totalPages = 1
	}

	s.jsonResponseWithMeta(w, http.StatusOK, users, &APIMeta{
		Page:       pagination.Page,
		PageSize:   pagination.PageSize,
		TotalCount: totalCount,
		TotalPages: totalPages,
	})
}

func (s *Server) handleAPICreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		DomainID int64  `json:"domain_id"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" || req.DomainID == 0 {
		s.jsonError(w, http.StatusBadRequest, "Username, domain_id, and password are required")
		return
	}

	// Validate username
	if err := validation.Username(req.Username); err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Check domain exists
	var domainName string
	if err := s.db.QueryRowContext(r.Context(), "SELECT name FROM domains WHERE id = ?", req.DomainID).Scan(&domainName); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Domain not found")
		return
	}

	// Check user doesn't already exist
	var exists int
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users WHERE username = ? AND domain_id = ?", req.Username, req.DomainID).Scan(&exists)
	if exists > 0 {
		s.jsonError(w, http.StatusConflict, "User already exists")
		return
	}

	// Hash password
	hashedPassword, err := auth.HashPassword(req.Password)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	result, err := s.db.ExecContext(r.Context(),
		`INSERT INTO users (username, domain_id, password_hash, is_admin) VALUES (?, ?, ?, ?)`,
		req.Username, req.DomainID, hashedPassword, req.IsAdmin,
	)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	id, _ := result.LastInsertId()
	email := req.Username + "@" + domainName

	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventUserCreate, email, nil, s.rateLimiter.GetClientIP(r))

	s.jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"id":       id,
		"email":    email,
		"username": req.Username,
		"domain":   domainName,
		"is_admin": req.IsAdmin,
	})
}

func (s *Server) handleAPIUserByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /admin/api/v1/users/{id} or /admin/api/v1/users/{id}/quota
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		s.jsonError(w, http.StatusBadRequest, "User ID required")
		return
	}

	// Find "users" in path, ID is next
	usersIdx := -1
	for i, p := range parts {
		if p == "users" {
			usersIdx = i
			break
		}
	}
	if usersIdx < 0 || usersIdx+1 >= len(parts) {
		s.jsonError(w, http.StatusBadRequest, "User ID required")
		return
	}

	idStr := parts[usersIdx+1]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Check for sub-routes
	if usersIdx+2 < len(parts) && parts[usersIdx+2] == "quota" {
		s.handleAPIUpdateUserQuota(w, r, id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetUser(w, r, id)
	case http.MethodPut:
		s.handleAPIUpdateUser(w, r, id)
	case http.MethodDelete:
		s.handleAPIDeleteUser(w, r, id)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIUpdateUserQuota(w http.ResponseWriter, r *http.Request, id int64) {
	if r.Method != http.MethodPut {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		QuotaBytes int64 `json:"quota_bytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.QuotaBytes <= 0 {
		s.jsonError(w, http.StatusBadRequest, "quota_bytes must be positive")
		return
	}

	result, err := s.db.ExecContext(r.Context(), "UPDATE users SET quota_bytes = ? WHERE id = ?", req.QuotaBytes, id)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to update quota")
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		s.jsonError(w, http.StatusNotFound, "User not found")
		return
	}

	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventUserUpdate, fmt.Sprintf("user:%d quota:%d", id, req.QuotaBytes), nil, s.rateLimiter.GetClientIP(r))

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"updated": true, "quota_bytes": req.QuotaBytes})
}

func (s *Server) handleAPIGetUser(w http.ResponseWriter, r *http.Request, id int64) {
	var u APIUser
	err := s.db.QueryRowContext(r.Context(),
		`SELECT u.id, u.username, d.name, u.is_admin, COALESCE(u.quota_bytes, 1073741824), COALESCE(u.used_bytes, 0), u.created_at
		FROM users u JOIN domains d ON u.domain_id = d.id WHERE u.id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.Domain, &u.IsAdmin, &u.QuotaBytes, &u.UsedBytes, &u.CreatedAt)
	if err != nil {
		s.jsonError(w, http.StatusNotFound, "User not found")
		return
	}
	u.Email = u.Username + "@" + u.Domain
	s.jsonResponse(w, http.StatusOK, u)
}

func (s *Server) handleAPIUpdateUser(w http.ResponseWriter, r *http.Request, id int64) {
	var req struct {
		Password string `json:"password,omitempty"`
		IsAdmin  *bool  `json:"is_admin,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Password != "" {
		hashedPassword, err := auth.HashPassword(req.Password)
		if err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to hash password")
			return
		}
		if _, err := s.db.ExecContext(r.Context(), "UPDATE users SET password_hash = ? WHERE id = ?", hashedPassword, id); err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to update password")
			return
		}
	}

	if req.IsAdmin != nil {
		if _, err := s.db.ExecContext(r.Context(), "UPDATE users SET is_admin = ? WHERE id = ?", *req.IsAdmin, id); err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to update admin status")
			return
		}
	}

	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventUserUpdate, fmt.Sprintf("user:%d", id), nil, s.rateLimiter.GetClientIP(r))

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"updated": true})
}

func (s *Server) handleAPIDeleteUser(w http.ResponseWriter, r *http.Request, id int64) {
	// Get user info before deletion
	var username, domain string
	err := s.db.QueryRowContext(r.Context(),
		`SELECT u.username, d.name FROM users u JOIN domains d ON u.domain_id = d.id WHERE u.id = ?`, id,
	).Scan(&username, &domain)
	if err != nil {
		s.jsonError(w, http.StatusNotFound, "User not found")
		return
	}

	// Invalidate sessions
	s.invalidateUserSessions(id)

	// Delete user
	if _, err := s.db.ExecContext(r.Context(), "DELETE FROM users WHERE id = ?", id); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to delete user")
		return
	}

	email := username + "@" + domain
	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventUserDelete, email, nil, s.rateLimiter.GetClientIP(r))

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"deleted": true, "email": email})
}

// --- Domains ---

// APIDomain represents a domain in API responses
type APIDomain struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	MailHostname string    `json:"mail_hostname"`
	IsPrimary    bool      `json:"is_primary"`
	UserCount    int       `json:"user_count"`
	DKIMEnabled  bool      `json:"dkim_enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Server) handleAPIDomains(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetDomains(w, r)
	case http.MethodPost:
		s.handleAPICreateDomain(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIGetDomains(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT d.id, d.name, COALESCE(d.mail_hostname, 'mail.' || d.name),
			COALESCE(d.is_primary, 0),
			(SELECT COUNT(*) FROM users WHERE domain_id = d.id),
			CASE WHEN d.dkim_private_key IS NOT NULL AND d.dkim_private_key != '' THEN 1 ELSE 0 END,
			d.created_at
		FROM domains d ORDER BY d.is_primary DESC, d.name
	`)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query domains")
		return
	}
	defer rows.Close()

	domains := []APIDomain{}
	for rows.Next() {
		var d APIDomain
		var isPrimary, dkimEnabled int
		if err := rows.Scan(&d.ID, &d.Name, &d.MailHostname, &isPrimary, &d.UserCount, &dkimEnabled, &d.CreatedAt); err != nil {
			continue
		}
		d.IsPrimary = isPrimary == 1
		d.DKIMEnabled = dkimEnabled == 1
		domains = append(domains, d)
	}

	s.jsonResponse(w, http.StatusOK, domains)
}

func (s *Server) handleAPICreateDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		MailHostname string `json:"mail_hostname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		s.jsonError(w, http.StatusBadRequest, "Domain name is required")
		return
	}

	if req.MailHostname == "" {
		req.MailHostname = "mail." + req.Name
	}

	// Check for duplicate
	var exists int
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM domains WHERE name = ?", req.Name).Scan(&exists)
	if exists > 0 {
		s.jsonError(w, http.StatusConflict, "Domain already exists")
		return
	}

	result, err := s.db.ExecContext(r.Context(),
		`INSERT INTO domains (name, mail_hostname, created_at) VALUES (?, ?, ?)`,
		req.Name, req.MailHostname, time.Now(),
	)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to create domain")
		return
	}

	id, _ := result.LastInsertId()

	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventDomainCreate, req.Name, nil, s.rateLimiter.GetClientIP(r))

	s.jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"id":            id,
		"name":          req.Name,
		"mail_hostname": req.MailHostname,
	})
}

func (s *Server) handleAPIDomainByID(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		s.jsonError(w, http.StatusBadRequest, "Domain ID required")
		return
	}

	// Find the domain ID - it's right after "domains" in the path
	domainIdx := -1
	for i, p := range parts {
		if p == "domains" {
			domainIdx = i
			break
		}
	}
	if domainIdx < 0 || domainIdx+1 >= len(parts) {
		s.jsonError(w, http.StatusBadRequest, "Domain ID required")
		return
	}

	idStr := parts[domainIdx+1]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid domain ID")
		return
	}

	// Check for sub-routes
	if domainIdx+2 < len(parts) {
		switch parts[domainIdx+2] {
		case "dkim":
			s.handleAPIDomainDKIM(w, r, id, parts)
			return
		case "dns":
			s.handleAPIDomainDNS(w, r, id, parts)
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetDomain(w, r, id)
	case http.MethodDelete:
		s.handleAPIDeleteDomain(w, r, id)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIGetDomain(w http.ResponseWriter, r *http.Request, id int64) {
	var d APIDomain
	var isPrimary, dkimEnabled int
	err := s.db.QueryRowContext(r.Context(), `
		SELECT d.id, d.name, COALESCE(d.mail_hostname, 'mail.' || d.name),
			COALESCE(d.is_primary, 0),
			(SELECT COUNT(*) FROM users WHERE domain_id = d.id),
			CASE WHEN d.dkim_private_key IS NOT NULL AND d.dkim_private_key != '' THEN 1 ELSE 0 END,
			d.created_at
		FROM domains d WHERE d.id = ?`, id,
	).Scan(&d.ID, &d.Name, &d.MailHostname, &isPrimary, &d.UserCount, &dkimEnabled, &d.CreatedAt)
	if err != nil {
		s.jsonError(w, http.StatusNotFound, "Domain not found")
		return
	}
	d.IsPrimary = isPrimary == 1
	d.DKIMEnabled = dkimEnabled == 1
	s.jsonResponse(w, http.StatusOK, d)
}

func (s *Server) handleAPIDeleteDomain(w http.ResponseWriter, r *http.Request, id int64) {
	var name string
	var isPrimary int
	err := s.db.QueryRowContext(r.Context(), "SELECT name, COALESCE(is_primary, 0) FROM domains WHERE id = ?", id).Scan(&name, &isPrimary)
	if err != nil {
		s.jsonError(w, http.StatusNotFound, "Domain not found")
		return
	}
	if isPrimary == 1 {
		s.jsonError(w, http.StatusForbidden, "Cannot delete primary domain")
		return
	}

	// Check for users
	var userCount int
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users WHERE domain_id = ?", id).Scan(&userCount)
	if userCount > 0 {
		s.jsonError(w, http.StatusConflict, fmt.Sprintf("Domain has %d users. Delete users first.", userCount))
		return
	}

	if _, err := s.db.ExecContext(r.Context(), "DELETE FROM domains WHERE id = ?", id); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to delete domain")
		return
	}

	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventDomainDelete, name, nil, s.rateLimiter.GetClientIP(r))

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"deleted": true, "domain": name})
}

func (s *Server) handleAPIDomainDKIM(w http.ResponseWriter, r *http.Request, domainID int64, parts []string) {
	// Find "dkim" in parts and check for sub-action
	dkimIdx := -1
	for i, p := range parts {
		if p == "dkim" {
			dkimIdx = i
			break
		}
	}

	if dkimIdx >= 0 && dkimIdx+1 < len(parts) {
		action := parts[dkimIdx+1]
		switch action {
		case "generate":
			if r.Method != http.MethodPost {
				s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
				return
			}
			s.handleAPIDKIMGenerate(w, r, domainID)
			return
		case "rotate":
			if r.Method != http.MethodPost {
				s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
				return
			}
			s.handleAPIDKIMRotate(w, r, domainID)
			return
		}
	}

	// GET /admin/api/v1/domains/{id}/dkim - show DKIM info
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var domainName, dkimSelector string
	var storageType sql.NullString
	err := s.db.QueryRowContext(r.Context(),
		`SELECT name, COALESCE(dkim_selector, ''), dkim_storage_type FROM domains WHERE id = ?`,
		domainID,
	).Scan(&domainName, &dkimSelector, &storageType)
	if err != nil {
		s.jsonError(w, http.StatusNotFound, "Domain not found")
		return
	}

	storage := "file"
	if storageType.Valid && storageType.String != "" {
		storage = storageType.String
	}

	// Try to get the full DKIM DNS record
	dkimPath := s.getDKIMPath()
	store := security.NewKeyStore(storage, dkimPath, s.db)
	meta, metaErr := store.GetKeyMetadata(r.Context(), domainName)

	enabled := metaErr == nil && meta.HasKey
	dnsRecord := ""
	publicKey := ""

	if enabled {
		_, recordValue, recErr := security.GetDNSRecord(r.Context(), store, domainName)
		if recErr == nil {
			dnsRecord = recordValue
		}
		if meta.Selector != "" {
			dkimSelector = meta.Selector
		}
		// Extract public key from DNS record if available
		if dnsRecord != "" {
			// The DNS record contains p=<key>
			for _, part := range strings.Split(dnsRecord, ";") {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "p=") {
					publicKey = strings.TrimPrefix(part, "p=")
				}
			}
		}
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"enabled":    enabled,
		"selector":   dkimSelector,
		"public_key": publicKey,
		"dns_record": dnsRecord,
	})
}

func (s *Server) handleAPIDomainDNS(w http.ResponseWriter, r *http.Request, domainID int64, parts []string) {
	// Find "dns" in parts and check for sub-action
	dnsIdx := -1
	for i, p := range parts {
		if p == "dns" {
			dnsIdx = i
			break
		}
	}

	if dnsIdx >= 0 && dnsIdx+1 < len(parts) && parts[dnsIdx+1] == "verify" {
		if r.Method != http.MethodPost {
			s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		s.handleDNSVerify(w, r)
		return
	}

	// GET /admin/api/v1/domains/{id}/dns
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Get domain name
	var domainName, mailHostname string
	err := s.db.QueryRowContext(r.Context(),
		`SELECT name, COALESCE(mail_hostname, 'mail.' || name) FROM domains WHERE id = ?`,
		domainID,
	).Scan(&domainName, &mailHostname)
	if err != nil {
		s.jsonError(w, http.StatusNotFound, "Domain not found")
		return
	}

	// Build records array matching frontend DNSRecord interface:
	// {type, name, expected, actual, status}
	type DNSRecord struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Expected string `json:"expected"`
		Actual   string `json:"actual"`
		Status   string `json:"status"`
	}

	records := []DNSRecord{}
	mailServer := s.config.Server.Hostname

	// MX check
	mxActual := ""
	mxStatus := "fail"
	mxRecords, mxErr := net.LookupMX(domainName)
	if mxErr == nil {
		for _, mx := range mxRecords {
			host := strings.TrimSuffix(mx.Host, ".")
			mxActual += host + " "
			if strings.EqualFold(host, mailHostname) || strings.EqualFold(host, mailServer) {
				mxStatus = "pass"
			}
		}
		mxActual = strings.TrimSpace(mxActual)
	}
	records = append(records, DNSRecord{
		Type: "MX", Name: domainName,
		Expected: mailHostname, Actual: mxActual, Status: mxStatus,
	})

	// SPF check
	spfActual := ""
	spfStatus := "fail"
	txtRecords, txtErr := net.LookupTXT(domainName)
	if txtErr == nil {
		for _, txt := range txtRecords {
			if strings.HasPrefix(txt, "v=spf1") {
				spfActual = txt
				spfStatus = "pass"
				break
			}
		}
	}
	records = append(records, DNSRecord{
		Type: "SPF", Name: domainName,
		Expected: "v=spf1 mx ~all", Actual: spfActual, Status: spfStatus,
	})

	// DMARC check
	dmarcActual := ""
	dmarcStatus := "warning"
	dmarcRecords, dmarcErr := net.LookupTXT("_dmarc." + domainName)
	if dmarcErr == nil {
		for _, txt := range dmarcRecords {
			if strings.HasPrefix(txt, "v=DMARC1") {
				dmarcActual = txt
				dmarcStatus = "pass"
				break
			}
		}
	}
	records = append(records, DNSRecord{
		Type: "DMARC", Name: "_dmarc." + domainName,
		Expected: "v=DMARC1; p=quarantine; ...", Actual: dmarcActual, Status: dmarcStatus,
	})

	// DKIM check
	var dkimSelector string
	s.db.QueryRowContext(r.Context(),
		`SELECT COALESCE(dkim_selector, '') FROM domains WHERE id = ?`, domainID,
	).Scan(&dkimSelector)
	if dkimSelector != "" {
		dkimDomain := dkimSelector + "._domainkey." + domainName
		dkimActual := ""
		dkimStatus := "fail"
		dkimTxt, dkimErr := net.LookupTXT(dkimDomain)
		if dkimErr == nil {
			for _, txt := range dkimTxt {
				if strings.Contains(txt, "v=DKIM1") {
					dkimActual = txt
					dkimStatus = "pass"
					break
				}
			}
		}
		records = append(records, DNSRecord{
			Type: "DKIM", Name: dkimDomain,
			Expected: "v=DKIM1; k=rsa; p=...", Actual: dkimActual, Status: dkimStatus,
		})
	}

	// Mail hostname A record
	mailHostActual := ""
	mailHostStatus := "fail"
	ips, ipErr := net.LookupHost(mailHostname)
	if ipErr == nil && len(ips) > 0 {
		mailHostActual = strings.Join(ips, ", ")
		mailHostStatus = "pass"
	}
	records = append(records, DNSRecord{
		Type: "A", Name: mailHostname,
		Expected: "Server IP", Actual: mailHostActual, Status: mailHostStatus,
	})

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"records":    records,
		"checked_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// --- DKIM API ---

func (s *Server) handleAPIDKIMGenerate(w http.ResponseWriter, r *http.Request, domainID int64) {
	var domainName string
	err := s.db.QueryRowContext(r.Context(), "SELECT name FROM domains WHERE id = ?", domainID).Scan(&domainName)
	if err != nil {
		s.jsonError(w, http.StatusNotFound, "Domain not found")
		return
	}

	// Parse JSON body
	var req struct {
		Selector string `json:"selector"`
		Bits     int    `json:"bits"`
		Storage  string `json:"storage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Fallback defaults
		req.Selector = "mail"
		req.Bits = 2048
		req.Storage = "database"
	}
	if req.Selector == "" {
		req.Selector = "mail"
	}
	if req.Bits != 4096 {
		req.Bits = 2048
	}
	if req.Storage == "" {
		req.Storage = "database"
	}

	dkimPath := s.getDKIMPath()
	store := security.NewKeyStore(req.Storage, dkimPath, s.db)

	_, err = security.GenerateAndSaveKey(r.Context(), store, domainName, req.Selector, req.Bits)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to generate DKIM key: "+err.Error())
		return
	}

	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventConfigChange, domainName, map[string]interface{}{
		"action": "dkim_generate", "selector": req.Selector, "bits": req.Bits,
	}, getIP(r))

	// Return the new DKIM info
	_, recordValue, _ := security.GetDNSRecord(r.Context(), store, domainName)

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"generated":  true,
		"selector":   req.Selector,
		"dns_record": recordValue,
	})
}

func (s *Server) handleAPIDKIMRotate(w http.ResponseWriter, r *http.Request, domainID int64) {
	var domainName string
	var storageType sql.NullString
	err := s.db.QueryRowContext(r.Context(),
		"SELECT name, dkim_storage_type FROM domains WHERE id = ?",
		domainID).Scan(&domainName, &storageType)
	if err != nil {
		s.jsonError(w, http.StatusNotFound, "Domain not found")
		return
	}

	storage := "file"
	if storageType.Valid && storageType.String != "" {
		storage = storageType.String
	}

	dkimPath := s.getDKIMPath()
	store := security.NewKeyStore(storage, dkimPath, s.db)

	newSelector, _, err := security.RotateKey(r.Context(), store, domainName, 2048)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to rotate DKIM key: "+err.Error())
		return
	}

	adminUser := getSessionUser(r)
	s.auditLogger.Log(r.Context(), adminUser, audit.EventConfigChange, domainName, map[string]interface{}{
		"action": "dkim_rotate", "newSelector": newSelector,
	}, getIP(r))

	_, recordValue, _ := security.GetDNSRecord(r.Context(), store, domainName)

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"rotated":      true,
		"new_selector": newSelector,
		"dns_record":   recordValue,
	})
}

// --- Logs ---

func (s *Server) handleAPIGetLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Determine log type from path
	parts := strings.Split(r.URL.Path, "/")
	logType := "auth"
	for i, p := range parts {
		if p == "logs" && i+1 < len(parts) {
			logType = parts[i+1]
			break
		}
	}

	pagination := getPaginationParams(r)

	switch logType {
	case "auth":
		s.handleAPIAuthLogs(w, r, pagination)
	case "delivery":
		s.handleAPIDeliveryLogs(w, r, pagination)
	case "audit":
		s.handleAPIAuditLogs(w, r, pagination)
	default:
		s.jsonError(w, http.StatusBadRequest, "Invalid log type")
	}
}

func (s *Server) handleAPIAuthLogs(w http.ResponseWriter, r *http.Request, p PaginationParams) {
	fromParam := r.URL.Query().Get("from")
	toParam := r.URL.Query().Get("to")
	whereClause, dateArgs := buildDateFilter("created_at", fromParam, toParam)

	var totalCount int
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM auth_log"+whereClause, dateArgs...).Scan(&totalCount)

	args := append(dateArgs, p.PageSize, p.Offset)
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT username, remote_addr, protocol, success, created_at
		FROM auth_log`+whereClause+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query auth logs")
		return
	}
	defer rows.Close()

	type AuthLog struct {
		Username  string    `json:"username"`
		RemoteIP  string    `json:"remote_ip"`
		Protocol  string    `json:"protocol"`
		Success   bool      `json:"success"`
		CreatedAt time.Time `json:"created_at"`
	}

	logs := []AuthLog{}
	for rows.Next() {
		var l AuthLog
		if err := rows.Scan(&l.Username, &l.RemoteIP, &l.Protocol, &l.Success, &l.CreatedAt); err == nil {
			logs = append(logs, l)
		}
	}

	totalPages := (totalCount + p.PageSize - 1) / p.PageSize
	s.jsonResponseWithMeta(w, http.StatusOK, logs, &APIMeta{
		Page: p.Page, PageSize: p.PageSize, TotalCount: totalCount, TotalPages: totalPages,
	})
}

func (s *Server) handleAPIDeliveryLogs(w http.ResponseWriter, r *http.Request, p PaginationParams) {
	fromParam := r.URL.Query().Get("from")
	toParam := r.URL.Query().Get("to")
	whereClause, dateArgs := buildDateFilter("created_at", fromParam, toParam)

	var totalCount int
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM delivery_log"+whereClause, dateArgs...).Scan(&totalCount)

	args := append(dateArgs, p.PageSize, p.Offset)
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT sender, recipient, status, COALESCE(error_message, ''), created_at
		FROM delivery_log`+whereClause+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query delivery logs")
		return
	}
	defer rows.Close()

	type DeliveryLog struct {
		Sender    string    `json:"sender"`
		Recipient string    `json:"recipient"`
		Status    string    `json:"status"`
		Message   string    `json:"message"`
		CreatedAt time.Time `json:"created_at"`
	}

	logs := []DeliveryLog{}
	for rows.Next() {
		var l DeliveryLog
		if err := rows.Scan(&l.Sender, &l.Recipient, &l.Status, &l.Message, &l.CreatedAt); err == nil {
			logs = append(logs, l)
		}
	}

	totalPages := (totalCount + p.PageSize - 1) / p.PageSize
	s.jsonResponseWithMeta(w, http.StatusOK, logs, &APIMeta{
		Page: p.Page, PageSize: p.PageSize, TotalCount: totalCount, TotalPages: totalPages,
	})
}

func (s *Server) handleAPIAuditLogs(w http.ResponseWriter, r *http.Request, p PaginationParams) {
	fromParam := r.URL.Query().Get("from")
	toParam := r.URL.Query().Get("to")
	whereClause, dateArgs := buildDateFilter("timestamp", fromParam, toParam)

	var totalCount int
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM audit_log"+whereClause, dateArgs...).Scan(&totalCount)

	args := append(dateArgs, p.PageSize, p.Offset)
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT actor, action, target, details, ip_address, timestamp
		FROM audit_log`+whereClause+` ORDER BY timestamp DESC LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query audit logs")
		return
	}
	defer rows.Close()

	type AuditLog struct {
		Username  string    `json:"username"`
		Event     string    `json:"event"`
		Target    string    `json:"target"`
		Details   string    `json:"details"`
		IP        string    `json:"ip_address"`
		CreatedAt time.Time `json:"created_at"`
	}

	logs := []AuditLog{}
	for rows.Next() {
		var l AuditLog
		var details sql.NullString
		var target sql.NullString
		var ip sql.NullString
		if err := rows.Scan(&l.Username, &l.Event, &target, &details, &ip, &l.CreatedAt); err == nil {
			l.Target = target.String
			l.Details = details.String
			l.IP = ip.String
			logs = append(logs, l)
		}
	}

	totalPages := (totalCount + p.PageSize - 1) / p.PageSize
	s.jsonResponseWithMeta(w, http.StatusOK, logs, &APIMeta{
		Page: p.Page, PageSize: p.PageSize, TotalCount: totalCount, TotalPages: totalPages,
	})
}

// --- Queue ---

func (s *Server) handleAPIGetQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.queue == nil {
		s.jsonResponse(w, http.StatusOK, []interface{}{})
		return
	}

	ctx := r.Context()
	pending, err := s.queue.ListPending(ctx, 100)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list queue")
		return
	}

	failed, err := s.queue.ListFailed(ctx, 100)
	if err != nil {
		// Non-fatal: return pending only if failed list errors
		s.jsonResponse(w, http.StatusOK, pending)
		return
	}

	// Combine pending + failed
	all := make([]*queue.Message, 0, len(pending)+len(failed))
	all = append(all, pending...)
	all = append(all, failed...)

	s.jsonResponse(w, http.StatusOK, all)
}

// --- Features ---

func (s *Server) handleAPIGetFeatures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.featuresStore == nil {
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"screener_count": 0, "alias_count": 0, "vip_count": 0,
			"scheduled_count": 0, "snoozed_count": 0,
		})
		return
	}

	// Get counts for overview
	screenerCount := 0
	aliasCount := 0
	vipCount := 0
	scheduledCount := 0
	snoozedCount := 0

	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM screener").Scan(&screenerCount)
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM aliases").Scan(&aliasCount)
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM vip_contacts").Scan(&vipCount)
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM scheduled_emails WHERE status = 'pending'").Scan(&scheduledCount)
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM snoozed_messages WHERE status = 'pending'").Scan(&snoozedCount)

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"screener_count":  screenerCount,
		"alias_count":     aliasCount,
		"vip_count":       vipCount,
		"scheduled_count": scheduledCount,
		"snoozed_count":   snoozedCount,
	})
}

// --- Mailing Lists ---

func (s *Server) handleAPIGetLists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.listsStore == nil {
		s.jsonResponse(w, http.StatusOK, []struct{}{})
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT ml.id, ml.address, ml.name, ml.description, ml.is_active,
			(SELECT COUNT(*) FROM list_members WHERE list_id = ml.id),
			(SELECT COUNT(*) FROM list_moderation_queue WHERE list_id = ml.id AND status = 'pending'),
			ml.created_at
		FROM mailing_lists ml ORDER BY ml.name
	`)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query lists")
		return
	}
	defer rows.Close()

	type APIList struct {
		ID                int64     `json:"id"`
		Address           string    `json:"address"`
		Name              string    `json:"name"`
		Description       string    `json:"description"`
		IsActive          bool      `json:"is_active"`
		MemberCount       int       `json:"member_count"`
		PendingModeration int       `json:"pending_moderation"`
		CreatedAt         time.Time `json:"created_at"`
	}

	lists := []APIList{}
	for rows.Next() {
		var l APIList
		if err := rows.Scan(&l.ID, &l.Address, &l.Name, &l.Description, &l.IsActive, &l.MemberCount, &l.PendingModeration, &l.CreatedAt); err == nil {
			lists = append(lists, l)
		}
	}

	s.jsonResponse(w, http.StatusOK, lists)
}

// --- System ---

func (s *Server) handleAPIGetSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	uptime := time.Since(s.startTime)

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"hostname":       s.config.Server.Hostname,
		"domain":         s.config.Server.Domain,
		"uptime_seconds": int(uptime.Seconds()),
		"uptime_human":   formatUptime(uptime),
		"version":        "1.0.0",
		"go_version":     "1.22",
		"config": map[string]interface{}{
			"imap_port":    s.config.Server.IMAPPort,
			"imaps_port":   s.config.Server.IMAPSPort,
			"smtp_port":    s.config.Server.SMTPPort,
			"smtps_port":   s.config.Server.SMTPSPort,
			"admin_port":   s.config.Admin.Port,
			"storage_path": s.config.Storage.MaildirPath,
		},
	})
}

// --- Available Domains (for dropdowns) ---

func (s *Server) handleAPIGetAvailableDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	rows, err := s.db.QueryContext(r.Context(), "SELECT id, name FROM domains ORDER BY name")
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query domains")
		return
	}
	defer rows.Close()

	type DomainOption struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}

	domains := []DomainOption{}
	for rows.Next() {
		var d DomainOption
		if err := rows.Scan(&d.ID, &d.Name); err == nil {
			domains = append(domains, d)
		}
	}

	s.jsonResponse(w, http.StatusOK, domains)
}
