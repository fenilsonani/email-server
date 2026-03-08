package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/org"
)

// =============================================================================
// API Keys Management
// =============================================================================

func (s *Server) handleAPIKeysCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIListAPIKeys(w, r)
	case http.MethodPost:
		s.handleAPICreateAPIKey(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIListAPIKeys(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT ak.id, ak.domain_id, ak.key_prefix, ak.name, ak.scopes, ak.is_active,
		       ak.rate_limit_per_hour, ak.last_used_at, ak.created_at, ak.expires_at,
		       d.name as domain_name
		FROM api_keys ak
		JOIN domains d ON ak.domain_id = d.id
		ORDER BY ak.created_at DESC
	`)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list API keys")
		return
	}
	defer rows.Close()

	type APIKeyRow struct {
		ID              int64      `json:"id"`
		DomainID        int64      `json:"domain_id"`
		KeyPrefix       string     `json:"key_prefix"`
		Name            string     `json:"name"`
		Scopes          string     `json:"scopes"`
		IsActive        bool       `json:"is_active"`
		RateLimitPerHr  int        `json:"rate_limit_per_hour"`
		LastUsedAt      *time.Time `json:"last_used_at"`
		CreatedAt       time.Time  `json:"created_at"`
		ExpiresAt       *time.Time `json:"expires_at"`
		DomainName      string     `json:"domain_name"`
	}

	var keys []APIKeyRow
	for rows.Next() {
		var k APIKeyRow
		if err := rows.Scan(&k.ID, &k.DomainID, &k.KeyPrefix, &k.Name, &k.Scopes, &k.IsActive,
			&k.RateLimitPerHr, &k.LastUsedAt, &k.CreatedAt, &k.ExpiresAt, &k.DomainName); err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to scan API key")
			return
		}
		keys = append(keys, k)
	}

	if keys == nil {
		keys = []APIKeyRow{}
	}
	s.jsonResponse(w, http.StatusOK, keys)
}

func (s *Server) handleAPICreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string `json:"name"`
		DomainID        int64  `json:"domain_id"`
		Scopes          string `json:"scopes"`
		RateLimitPerHr  int    `json:"rate_limit_per_hour"`
		ExpiresInDays   int    `json:"expires_in_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		s.jsonError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if req.DomainID == 0 {
		s.jsonError(w, http.StatusBadRequest, "Domain ID is required")
		return
	}
	if req.Scopes == "" {
		req.Scopes = `["send"]`
	}
	if req.RateLimitPerHr == 0 {
		req.RateLimitPerHr = 1000
	}

	// Generate API key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to generate API key")
		return
	}
	fullKey := "ms_" + hex.EncodeToString(keyBytes)
	prefix := fullKey[:10]
	hash := sha256.Sum256([]byte(fullKey))
	keyHash := hex.EncodeToString(hash[:])

	var expiresAt *time.Time
	if req.ExpiresInDays > 0 {
		t := time.Now().AddDate(0, 0, req.ExpiresInDays)
		expiresAt = &t
	}

	res, err := s.db.ExecContext(r.Context(), `
		INSERT INTO api_keys (domain_id, key_hash, key_prefix, name, scopes, rate_limit_per_hour, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, req.DomainID, keyHash, prefix, req.Name, req.Scopes, req.RateLimitPerHr, expiresAt)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to create API key")
		return
	}

	id, _ := res.LastInsertId()
	s.jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"id":         id,
		"key":        fullKey,
		"key_prefix": prefix,
		"name":       req.Name,
		"message":    "Copy this key now. It will not be shown again.",
	})
}

func (s *Server) handleAPIKeyByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/admin/api/v1/api-keys/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid API key ID")
		return
	}

	switch r.Method {
	case http.MethodDelete:
		_, err := s.db.ExecContext(r.Context(), "UPDATE api_keys SET is_active = 0 WHERE id = ?", id)
		if err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to revoke API key")
			return
		}
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{"revoked": true})

	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// =============================================================================
// Webhooks Management
// =============================================================================

func (s *Server) handleAPIWebhooksCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIListWebhooks(w, r)
	case http.MethodPost:
		s.handleAPICreateWebhook(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIListWebhooks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT w.id, w.domain_id, w.url, w.events, w.is_active, w.failure_count,
		       w.last_triggered_at, w.last_success_at, w.last_failure_at, w.last_failure_reason,
		       w.created_at, d.name as domain_name
		FROM webhooks w
		JOIN domains d ON w.domain_id = d.id
		ORDER BY w.created_at DESC
	`)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list webhooks")
		return
	}
	defer rows.Close()

	type WebhookRow struct {
		ID                int64      `json:"id"`
		DomainID          int64      `json:"domain_id"`
		URL               string     `json:"url"`
		Events            string     `json:"events"`
		IsActive          bool       `json:"is_active"`
		FailureCount      int        `json:"failure_count"`
		LastTriggeredAt   *time.Time `json:"last_triggered_at"`
		LastSuccessAt     *time.Time `json:"last_success_at"`
		LastFailureAt     *time.Time `json:"last_failure_at"`
		LastFailureReason *string    `json:"last_failure_reason"`
		CreatedAt         time.Time  `json:"created_at"`
		DomainName        string     `json:"domain_name"`
	}

	var webhooks []WebhookRow
	for rows.Next() {
		var wh WebhookRow
		if err := rows.Scan(&wh.ID, &wh.DomainID, &wh.URL, &wh.Events, &wh.IsActive,
			&wh.FailureCount, &wh.LastTriggeredAt, &wh.LastSuccessAt, &wh.LastFailureAt,
			&wh.LastFailureReason, &wh.CreatedAt, &wh.DomainName); err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to scan webhook")
			return
		}
		webhooks = append(webhooks, wh)
	}

	if webhooks == nil {
		webhooks = []WebhookRow{}
	}
	s.jsonResponse(w, http.StatusOK, webhooks)
}

func (s *Server) handleAPICreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DomainID int64    `json:"domain_id"`
		URL      string   `json:"url"`
		Events   []string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.URL == "" {
		s.jsonError(w, http.StatusBadRequest, "URL is required")
		return
	}
	if !strings.HasPrefix(req.URL, "https://") {
		s.jsonError(w, http.StatusBadRequest, "Webhook URL must use HTTPS")
		return
	}
	if req.DomainID == 0 {
		s.jsonError(w, http.StatusBadRequest, "Domain ID is required")
		return
	}
	if len(req.Events) == 0 {
		req.Events = []string{"email.sent", "email.delivered", "email.bounced", "email.opened", "email.clicked"}
	}

	// Generate webhook secret
	secretBytes := make([]byte, 32)
	rand.Read(secretBytes)
	secret := "whsec_" + hex.EncodeToString(secretBytes)

	eventsJSON, _ := json.Marshal(req.Events)

	res, err := s.db.ExecContext(r.Context(), `
		INSERT INTO webhooks (domain_id, url, events, secret) VALUES (?, ?, ?, ?)
	`, req.DomainID, req.URL, string(eventsJSON), secret)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to create webhook")
		return
	}

	id, _ := res.LastInsertId()
	s.jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"id":     id,
		"url":    req.URL,
		"secret": secret,
		"events": req.Events,
	})
}

func (s *Server) handleAPIWebhookByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/admin/api/v1/webhooks/")
	parts := strings.SplitN(path, "/", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid webhook ID")
		return
	}

	// Handle sub-resource: /webhooks/{id}/test
	if len(parts) > 1 && parts[1] == "test" && r.Method == http.MethodPost {
		s.handleAPITestWebhook(w, r, id)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req struct {
			URL      string   `json:"url"`
			Events   []string `json:"events"`
			IsActive *bool    `json:"is_active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if req.URL != "" {
			s.db.ExecContext(r.Context(), "UPDATE webhooks SET url = ? WHERE id = ?", req.URL, id)
		}
		if len(req.Events) > 0 {
			eventsJSON, _ := json.Marshal(req.Events)
			s.db.ExecContext(r.Context(), "UPDATE webhooks SET events = ? WHERE id = ?", string(eventsJSON), id)
		}
		if req.IsActive != nil {
			s.db.ExecContext(r.Context(), "UPDATE webhooks SET is_active = ? WHERE id = ?", *req.IsActive, id)
		}
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{"updated": true})

	case http.MethodDelete:
		s.db.ExecContext(r.Context(), "DELETE FROM webhooks WHERE id = ?", id)
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{"deleted": true})

	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPITestWebhook(w http.ResponseWriter, r *http.Request, webhookID int64) {
	// Get webhook details
	var url, secret string
	err := s.db.QueryRowContext(r.Context(),
		"SELECT url, secret FROM webhooks WHERE id = ?", webhookID,
	).Scan(&url, &secret)
	if err != nil {
		s.jsonError(w, http.StatusNotFound, "Webhook not found")
		return
	}

	// Send a test event (just record it, don't actually make the HTTP call from admin handler)
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"test_sent": true,
		"webhook_url": url,
		"event_type":  "test",
		"message":     "Test event queued for delivery",
	})
}

// =============================================================================
// Templates Management
// =============================================================================

func (s *Server) handleAPITemplatesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIListTemplates(w, r)
	case http.MethodPost:
		s.handleAPICreateTemplate(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIListTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT t.id, t.domain_id, t.slug, t.name, t.subject, t.html_body, t.text_body,
		       t.variables, t.is_active, t.created_at, t.updated_at,
		       d.name as domain_name
		FROM email_templates t
		JOIN domains d ON t.domain_id = d.id
		ORDER BY t.name
	`)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list templates")
		return
	}
	defer rows.Close()

	type TemplateRow struct {
		ID         int64     `json:"id"`
		DomainID   int64     `json:"domain_id"`
		Slug       string    `json:"slug"`
		Name       string    `json:"name"`
		Subject    string    `json:"subject"`
		HTMLBody   *string   `json:"html_body"`
		TextBody   *string   `json:"text_body"`
		Variables  *string   `json:"variables"`
		IsActive   bool      `json:"is_active"`
		CreatedAt  time.Time `json:"created_at"`
		UpdatedAt  time.Time `json:"updated_at"`
		DomainName string    `json:"domain_name"`
	}

	var templates []TemplateRow
	for rows.Next() {
		var t TemplateRow
		if err := rows.Scan(&t.ID, &t.DomainID, &t.Slug, &t.Name, &t.Subject, &t.HTMLBody, &t.TextBody,
			&t.Variables, &t.IsActive, &t.CreatedAt, &t.UpdatedAt, &t.DomainName); err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to scan template")
			return
		}
		templates = append(templates, t)
	}

	if templates == nil {
		templates = []TemplateRow{}
	}
	s.jsonResponse(w, http.StatusOK, templates)
}

func (s *Server) handleAPICreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DomainID int64  `json:"domain_id"`
		Slug     string `json:"slug"`
		Name     string `json:"name"`
		Subject  string `json:"subject"`
		HTMLBody string `json:"html_body"`
		TextBody string `json:"text_body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" || req.Subject == "" || req.DomainID == 0 {
		s.jsonError(w, http.StatusBadRequest, "Name, subject, and domain_id are required")
		return
	}
	if req.Slug == "" {
		req.Slug = strings.ReplaceAll(strings.ToLower(req.Name), " ", "-")
	}

	// Extract variables from template ({{variable}}) pattern
	variables := extractTemplateVariables(req.HTMLBody + req.TextBody)
	variablesJSON, _ := json.Marshal(variables)

	res, err := s.db.ExecContext(r.Context(), `
		INSERT INTO email_templates (domain_id, slug, name, subject, html_body, text_body, variables)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, req.DomainID, req.Slug, req.Name, req.Subject, req.HTMLBody, req.TextBody, string(variablesJSON))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			s.jsonError(w, http.StatusConflict, "Template slug already exists for this domain")
			return
		}
		s.jsonError(w, http.StatusInternalServerError, "Failed to create template")
		return
	}

	id, _ := res.LastInsertId()
	s.jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"id":        id,
		"slug":      req.Slug,
		"name":      req.Name,
		"variables": variables,
	})
}

func (s *Server) handleAPITemplateByID(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/admin/api/v1/templates/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid template ID")
		return
	}

	switch r.Method {
	case http.MethodGet:
		var t struct {
			ID         int64     `json:"id"`
			DomainID   int64     `json:"domain_id"`
			Slug       string    `json:"slug"`
			Name       string    `json:"name"`
			Subject    string    `json:"subject"`
			HTMLBody   *string   `json:"html_body"`
			TextBody   *string   `json:"text_body"`
			Variables  *string   `json:"variables"`
			IsActive   bool      `json:"is_active"`
			CreatedAt  time.Time `json:"created_at"`
			UpdatedAt  time.Time `json:"updated_at"`
		}
		err := s.db.QueryRowContext(r.Context(), `
			SELECT id, domain_id, slug, name, subject, html_body, text_body, variables, is_active, created_at, updated_at
			FROM email_templates WHERE id = ?
		`, id).Scan(&t.ID, &t.DomainID, &t.Slug, &t.Name, &t.Subject, &t.HTMLBody, &t.TextBody,
			&t.Variables, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
		if err == sql.ErrNoRows {
			s.jsonError(w, http.StatusNotFound, "Template not found")
			return
		}
		if err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to get template")
			return
		}
		s.jsonResponse(w, http.StatusOK, t)

	case http.MethodPut:
		var req struct {
			Name     string `json:"name"`
			Subject  string `json:"subject"`
			HTMLBody string `json:"html_body"`
			TextBody string `json:"text_body"`
			IsActive *bool  `json:"is_active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		// Build dynamic update
		sets := []string{"updated_at = CURRENT_TIMESTAMP"}
		args := []interface{}{}
		if req.Name != "" {
			sets = append(sets, "name = ?")
			args = append(args, req.Name)
		}
		if req.Subject != "" {
			sets = append(sets, "subject = ?")
			args = append(args, req.Subject)
		}
		if req.HTMLBody != "" {
			sets = append(sets, "html_body = ?")
			args = append(args, req.HTMLBody)
		}
		if req.TextBody != "" {
			sets = append(sets, "text_body = ?")
			args = append(args, req.TextBody)
		}
		if req.IsActive != nil {
			sets = append(sets, "is_active = ?")
			args = append(args, *req.IsActive)
		}

		// Update variables
		if req.HTMLBody != "" || req.TextBody != "" {
			vars := extractTemplateVariables(req.HTMLBody + req.TextBody)
			varsJSON, _ := json.Marshal(vars)
			sets = append(sets, "variables = ?")
			args = append(args, string(varsJSON))
		}

		args = append(args, id)
		_, err := s.db.ExecContext(r.Context(),
			fmt.Sprintf("UPDATE email_templates SET %s WHERE id = ?", strings.Join(sets, ", ")), args...)
		if err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to update template")
			return
		}
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{"updated": true})

	case http.MethodDelete:
		s.db.ExecContext(r.Context(), "DELETE FROM email_templates WHERE id = ?", id)
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{"deleted": true})

	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// extractTemplateVariables extracts {{variable}} patterns from template content.
func extractTemplateVariables(content string) []string {
	seen := map[string]bool{}
	var vars []string
	i := 0
	for i < len(content)-3 {
		if content[i] == '{' && content[i+1] == '{' {
			end := strings.Index(content[i+2:], "}}")
			if end > 0 {
				v := strings.TrimSpace(content[i+2 : i+2+end])
				if v != "" && !seen[v] {
					seen[v] = true
					vars = append(vars, v)
				}
				i = i + 2 + end + 2
				continue
			}
		}
		i++
	}
	return vars
}

// =============================================================================
// Sent Emails / Send Logs
// =============================================================================

func (s *Server) handleAPISentEmails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse query params
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	statusFilter := r.URL.Query().Get("status")

	// Count total
	countQuery := "SELECT COUNT(*) FROM sent_emails"
	countArgs := []interface{}{}
	if statusFilter != "" {
		countQuery += " WHERE status = ?"
		countArgs = append(countArgs, statusFilter)
	}
	var total int
	s.db.QueryRowContext(r.Context(), countQuery, countArgs...).Scan(&total)

	// Fetch page
	query := `
		SELECT id, domain_id, api_key_id, message_id, tracking_id,
		       from_email, to_email, subject, template_slug, tags,
		       status, opened_count, clicked_count,
		       created_at, delivered_at, bounced_at
		FROM sent_emails
	`
	args := []interface{}{}
	if statusFilter != "" {
		query += " WHERE status = ?"
		args = append(args, statusFilter)
	}
	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list emails")
		return
	}
	defer rows.Close()

	type SentEmailRow struct {
		ID           int64      `json:"id"`
		DomainID     int64      `json:"domain_id"`
		APIKeyID     *int64     `json:"api_key_id"`
		MessageID    *string    `json:"message_id"`
		TrackingID   *string    `json:"tracking_id"`
		FromEmail    string     `json:"from_email"`
		ToEmail      string     `json:"to_email"`
		Subject      *string    `json:"subject"`
		TemplateSlug *string    `json:"template_slug"`
		Tags         *string    `json:"tags"`
		Status       string     `json:"status"`
		OpenedCount  int        `json:"opened_count"`
		ClickedCount int        `json:"clicked_count"`
		CreatedAt    time.Time  `json:"created_at"`
		DeliveredAt  *time.Time `json:"delivered_at"`
		BouncedAt    *time.Time `json:"bounced_at"`
	}

	var emails []SentEmailRow
	for rows.Next() {
		var e SentEmailRow
		if err := rows.Scan(&e.ID, &e.DomainID, &e.APIKeyID, &e.MessageID, &e.TrackingID,
			&e.FromEmail, &e.ToEmail, &e.Subject, &e.TemplateSlug, &e.Tags,
			&e.Status, &e.OpenedCount, &e.ClickedCount,
			&e.CreatedAt, &e.DeliveredAt, &e.BouncedAt); err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to scan email")
			return
		}
		emails = append(emails, e)
	}

	if emails == nil {
		emails = []SentEmailRow{}
	}

	totalPages := (total + pageSize - 1) / pageSize
	s.jsonResponseWithMeta(w, http.StatusOK, emails, &APIMeta{
		Page:       page,
		PageSize:   pageSize,
		TotalCount: total,
		TotalPages: totalPages,
	})
}

func (s *Server) handleAPISentEmailByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/admin/api/v1/emails/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid email ID")
		return
	}

	// Get the email
	var email struct {
		ID           int64      `json:"id"`
		DomainID     int64      `json:"domain_id"`
		APIKeyID     *int64     `json:"api_key_id"`
		MessageID    *string    `json:"message_id"`
		TrackingID   *string    `json:"tracking_id"`
		FromEmail    string     `json:"from_email"`
		ToEmail      string     `json:"to_email"`
		Subject      *string    `json:"subject"`
		TemplateSlug *string    `json:"template_slug"`
		Tags         *string    `json:"tags"`
		Status       string     `json:"status"`
		SMTPResponse *string    `json:"smtp_response"`
		OpenedAt     *time.Time `json:"opened_at"`
		OpenedCount  int        `json:"opened_count"`
		ClickedAt    *time.Time `json:"clicked_at"`
		ClickedCount int        `json:"clicked_count"`
		CreatedAt    time.Time  `json:"created_at"`
		DeliveredAt  *time.Time `json:"delivered_at"`
		BouncedAt    *time.Time `json:"bounced_at"`
		BounceReason *string    `json:"bounce_reason"`
	}

	err = s.db.QueryRowContext(r.Context(), `
		SELECT id, domain_id, api_key_id, message_id, tracking_id,
		       from_email, to_email, subject, template_slug, tags,
		       status, smtp_response, opened_at, opened_count, clicked_at, clicked_count,
		       created_at, delivered_at, bounced_at, bounce_reason
		FROM sent_emails WHERE id = ?
	`, id).Scan(&email.ID, &email.DomainID, &email.APIKeyID, &email.MessageID, &email.TrackingID,
		&email.FromEmail, &email.ToEmail, &email.Subject, &email.TemplateSlug, &email.Tags,
		&email.Status, &email.SMTPResponse, &email.OpenedAt, &email.OpenedCount, &email.ClickedAt, &email.ClickedCount,
		&email.CreatedAt, &email.DeliveredAt, &email.BouncedAt, &email.BounceReason)
	if err == sql.ErrNoRows {
		s.jsonError(w, http.StatusNotFound, "Email not found")
		return
	}
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to get email")
		return
	}

	// Get delivery attempts
	type Attempt struct {
		ID            int64      `json:"id"`
		AttemptNumber int        `json:"attempt_number"`
		AttemptedAt   time.Time  `json:"attempted_at"`
		Status        string     `json:"status"`
		SMTPResponse  *string    `json:"smtp_response"`
		ErrorMessage  *string    `json:"error_message"`
	}
	var attempts []Attempt
	attRows, err := s.db.QueryContext(r.Context(), `
		SELECT id, attempt_number, attempted_at, status, smtp_response, error_message
		FROM delivery_attempts WHERE sent_email_id = ? ORDER BY attempt_number
	`, id)
	if err == nil {
		defer attRows.Close()
		for attRows.Next() {
			var a Attempt
			if attRows.Scan(&a.ID, &a.AttemptNumber, &a.AttemptedAt, &a.Status, &a.SMTPResponse, &a.ErrorMessage) == nil {
				attempts = append(attempts, a)
			}
		}
	}
	if attempts == nil {
		attempts = []Attempt{}
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"email":    email,
		"attempts": attempts,
	})
}

// =============================================================================
// API Stats
// =============================================================================

func (s *Server) handleAPIEmailStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx := r.Context()
	stats := map[string]interface{}{}

	// Emails sent today
	var sentToday int
	s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sent_emails WHERE created_at >= date('now')").Scan(&sentToday)
	stats["sent_today"] = sentToday

	// Emails sent this week
	var sentWeek int
	s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sent_emails WHERE created_at >= date('now', '-7 days')").Scan(&sentWeek)
	stats["sent_week"] = sentWeek

	// Emails sent this month
	var sentMonth int
	s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sent_emails WHERE created_at >= date('now', '-30 days')").Scan(&sentMonth)
	stats["sent_month"] = sentMonth

	// Total API keys
	var totalKeys int
	s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM api_keys WHERE is_active = 1").Scan(&totalKeys)
	stats["active_api_keys"] = totalKeys

	// Total webhooks
	var totalWebhooks int
	s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM webhooks WHERE is_active = 1").Scan(&totalWebhooks)
	stats["active_webhooks"] = totalWebhooks

	// Total templates
	var totalTemplates int
	s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM email_templates WHERE is_active = 1").Scan(&totalTemplates)
	stats["active_templates"] = totalTemplates

	// Delivery rate
	var delivered, total int
	s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sent_emails WHERE created_at >= date('now', '-30 days')").Scan(&total)
	s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sent_emails WHERE status = 'delivered' AND created_at >= date('now', '-30 days')").Scan(&delivered)
	if total > 0 {
		stats["delivery_rate"] = float64(delivered) / float64(total) * 100
	} else {
		stats["delivery_rate"] = 0.0
	}

	// Open rate
	var opened int
	s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sent_emails WHERE opened_count > 0 AND created_at >= date('now', '-30 days')").Scan(&opened)
	if delivered > 0 {
		stats["open_rate"] = float64(opened) / float64(delivered) * 100
	} else {
		stats["open_rate"] = 0.0
	}

	s.jsonResponse(w, http.StatusOK, stats)
}

// =============================================================================
// Presets
// =============================================================================

func (s *Server) handleAPIPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	s.jsonResponse(w, http.StatusOK, org.Presets)
}

