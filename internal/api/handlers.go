package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxRequestSize = 1 << 20 // 1 MB

// handleSendEmail handles POST /api/v1/send
func (s *Server) handleSendEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req SendEmailRequest
	if err := parseJSONRequest(r, &req); err != nil {
		jsonError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	// Validate request
	if err := validateSendRequest(&req); err != nil {
		jsonError(w, err.Error(), "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Check domain permission
	canSend, err := s.canSendFromDomain(r.Context(), req.From)
	if err != nil || !canSend {
		jsonError(w, "Cannot send from this domain", "FORBIDDEN", http.StatusForbidden)
		return
	}

	// Generate message ID and tracking ID
	// Extract sender domain for Message-ID (multi-domain support)
	senderDomain := s.config.Server.Hostname // fallback
	if parts := splitEmail(req.From); len(parts) == 2 && parts[1] != "" {
		senderDomain = parts[1]
	}
	messageID := generateMessageID(senderDomain)
	trackingID := generateTrackingID()

	// Process HTML for tracking if enabled
	htmlBody := req.HTML
	if s.config.API.EnableTracking {
		if req.TrackOpens {
			htmlBody = injectOpenTracking(htmlBody, trackingID, s.config.API.TrackingDomain, senderDomain)
		}
		if req.TrackClicks {
			htmlBody = rewriteLinksForTracking(htmlBody, trackingID, s.config.API.TrackingDomain, senderDomain)
		}
	}

	// Get domain ID
	domainID, _ := s.getDomainID(r.Context())
	apiKey := getAPIKeyFromContext(r.Context())
	if apiKey == nil {
		jsonError(w, "Authentication required", "UNAUTHORIZED", http.StatusUnauthorized)
		return
	}

	// Store sent email record
	tagsJSON, err := json.Marshal(req.Tags)
	if err != nil {
		s.logger.Error("Failed to marshal tags", "error", err.Error())
		tagsJSON = []byte("[]")
	}
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO sent_emails (domain_id, api_key_id, message_id, tracking_id, from_email, to_email, subject, tags, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, domainID, apiKey.ID, messageID, trackingID, req.From, req.To, req.Subject, string(tagsJSON), StatusQueued)
	if err != nil {
		s.logger.Error("Failed to store sent email record", "error", err.Error())
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	// Queue the email for delivery
	err = s.queueEmail(r.Context(), messageID, req.From, req.To, req.Subject, htmlBody, req.Text, req.ReplyTo, req.Headers)
	if err != nil {
		s.logger.Error("Failed to queue email", "error", err.Error())
		// Update status to failed
		s.db.ExecContext(r.Context(), `UPDATE sent_emails SET status = ? WHERE message_id = ?`, StatusFailed, messageID)
		jsonError(w, "Failed to queue email", "QUEUE_ERROR", http.StatusInternalServerError)
		return
	}

	// Trigger webhook for queued event
	go s.triggerWebhook(r.Context(), domainID, EventQueued, &WebhookEvent{
		Event:     EventQueued,
		Timestamp: time.Now(),
		MessageID: messageID,
		Recipient: req.To,
	})

	jsonResponse(w, SendResponse{
		Success:   true,
		MessageID: messageID,
		Status:    StatusQueued,
	}, http.StatusOK)
}

// handleSendTemplate handles POST /api/v1/send/template
func (s *Server) handleSendTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	var req SendTemplateRequest
	if err := parseJSONRequest(r, &req); err != nil {
		jsonError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	// Validate
	if req.From == "" || req.To == "" || req.Template == "" {
		jsonError(w, "from, to, and template are required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Check domain permission
	canSend, err := s.canSendFromDomain(r.Context(), req.From)
	if err != nil || !canSend {
		jsonError(w, "Cannot send from this domain", "FORBIDDEN", http.StatusForbidden)
		return
	}

	// Get domain ID
	domainID, _ := s.getDomainID(r.Context())

	// Fetch template
	template, err := s.getTemplateBySlug(r.Context(), domainID, req.Template)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "Template not found", "NOT_FOUND", http.StatusNotFound)
		} else {
			jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		}
		return
	}

	// Render template with variables
	subject := renderTemplateString(template.Subject, req.Variables)
	htmlBody := renderTemplateString(template.HTMLBody, req.Variables)
	textBody := renderTemplateString(template.TextBody, req.Variables)

	// Generate IDs - use sender domain for Message-ID (multi-domain support)
	senderDomain := s.config.Server.Hostname // fallback
	if parts := splitEmail(req.From); len(parts) == 2 && parts[1] != "" {
		senderDomain = parts[1]
	}
	messageID := generateMessageID(senderDomain)
	trackingID := generateTrackingID()

	// Process tracking
	trackOpens := req.TrackOpens == nil || *req.TrackOpens
	trackClicks := req.TrackClick == nil || *req.TrackClick
	if s.config.API.EnableTracking {
		if trackOpens {
			htmlBody = injectOpenTracking(htmlBody, trackingID, s.config.API.TrackingDomain, senderDomain)
		}
		if trackClicks {
			htmlBody = rewriteLinksForTracking(htmlBody, trackingID, s.config.API.TrackingDomain, senderDomain)
		}
	}

	apiKey := getAPIKeyFromContext(r.Context())
	if apiKey == nil {
		jsonError(w, "Authentication required", "UNAUTHORIZED", http.StatusUnauthorized)
		return
	}

	tagsJSON, err := json.Marshal(req.Tags)
	if err != nil {
		s.logger.Error("Failed to marshal tags", "error", err.Error())
		tagsJSON = []byte("[]")
	}

	// Store record
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO sent_emails (domain_id, api_key_id, message_id, tracking_id, from_email, to_email, subject, template_slug, tags, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, domainID, apiKey.ID, messageID, trackingID, req.From, req.To, subject, req.Template, string(tagsJSON), StatusQueued)
	if err != nil {
		s.logger.Error("Failed to store sent email record", "error", err.Error())
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	// Queue email
	err = s.queueEmail(r.Context(), messageID, req.From, req.To, subject, htmlBody, textBody, req.ReplyTo, nil)
	if err != nil {
		s.db.ExecContext(r.Context(), `UPDATE sent_emails SET status = ? WHERE message_id = ?`, StatusFailed, messageID)
		jsonError(w, "Failed to queue email", "QUEUE_ERROR", http.StatusInternalServerError)
		return
	}

	go s.triggerWebhook(r.Context(), domainID, EventQueued, &WebhookEvent{
		Event:     EventQueued,
		Timestamp: time.Now(),
		MessageID: messageID,
		Recipient: req.To,
	})

	jsonResponse(w, SendResponse{
		Success:   true,
		MessageID: messageID,
		Status:    StatusQueued,
	}, http.StatusOK)
}

// handleSendBatch handles POST /api/v1/send/batch
func (s *Server) handleSendBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	var req BatchSendRequest
	if err := parseJSONRequest(r, &req); err != nil {
		jsonError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	if req.From == "" {
		jsonError(w, "from is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		jsonError(w, "messages array is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	if len(req.Messages) > 100 {
		jsonError(w, "maximum 100 messages per batch", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Check domain permission
	canSend, err := s.canSendFromDomain(r.Context(), req.From)
	if err != nil || !canSend {
		jsonError(w, "Cannot send from this domain", "FORBIDDEN", http.StatusForbidden)
		return
	}

	domainID, _ := s.getDomainID(r.Context())
	apiKey := getAPIKeyFromContext(r.Context())
	if apiKey == nil {
		jsonError(w, "Authentication required", "UNAUTHORIZED", http.StatusUnauthorized)
		return
	}

	// Extract sender domain for Message-ID (multi-domain support)
	senderDomain := s.config.Server.Hostname // fallback
	if parts := splitEmail(req.From); len(parts) == 2 && parts[1] != "" {
		senderDomain = parts[1]
	}

	var responses []SendResponse
	var errors []BatchError

	for i, msg := range req.Messages {
		if msg.To == "" {
			errors = append(errors, BatchError{Index: i, To: msg.To, Message: "to is required"})
			continue
		}

		messageID := generateMessageID(senderDomain)
		trackingID := generateTrackingID()

		tagsJSON, err := json.Marshal(msg.Tags)
		if err != nil {
			s.logger.Error("Failed to marshal batch tags", "error", err.Error())
			tagsJSON = []byte("[]")
		}
		_, err = s.db.ExecContext(r.Context(), `
			INSERT INTO sent_emails (domain_id, api_key_id, message_id, tracking_id, from_email, to_email, subject, tags, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, domainID, apiKey.ID, messageID, trackingID, req.From, msg.To, msg.Subject, string(tagsJSON), StatusQueued)
		if err != nil {
			errors = append(errors, BatchError{Index: i, To: msg.To, Message: "failed to store"})
			continue
		}

		err = s.queueEmail(r.Context(), messageID, req.From, msg.To, msg.Subject, msg.HTML, msg.Text, "", nil)
		if err != nil {
			s.db.ExecContext(r.Context(), `UPDATE sent_emails SET status = ? WHERE message_id = ?`, StatusFailed, messageID)
			errors = append(errors, BatchError{Index: i, To: msg.To, Message: "failed to queue"})
			continue
		}

		responses = append(responses, SendResponse{
			Success:   true,
			MessageID: messageID,
			Status:    StatusQueued,
		})
	}

	jsonResponse(w, BatchSendResponse{
		Success:  len(errors) == 0,
		Messages: responses,
		Errors:   errors,
	}, http.StatusOK)
}

// handleTemplates handles GET/POST /api/v1/templates
func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	domainID, _ := s.getDomainID(r.Context())

	switch r.Method {
	case http.MethodGet:
		s.listTemplates(w, r, domainID)
	case http.MethodPost:
		s.createTemplate(w, r, domainID)
	default:
		jsonError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
	}
}

// handleTemplateBySlug handles GET/PUT/DELETE /api/v1/templates/{slug}
func (s *Server) handleTemplateBySlug(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/api/v1/templates/")
	if slug == "" {
		jsonError(w, "Template slug required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	domainID, _ := s.getDomainID(r.Context())

	switch r.Method {
	case http.MethodGet:
		s.getTemplate(w, r, domainID, slug)
	case http.MethodPut:
		s.updateTemplate(w, r, domainID, slug)
	case http.MethodDelete:
		s.deleteTemplate(w, r, domainID, slug)
	default:
		jsonError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
	}
}

// handleListEmails handles GET /api/v1/emails
func (s *Server) handleListEmails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	domainID, _ := s.getDomainID(r.Context())

	// Parse pagination
	page := 1
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	perPage := 20
	if perPageStr := r.URL.Query().Get("per_page"); perPageStr != "" {
		if pp, err := strconv.Atoi(perPageStr); err == nil && pp > 0 && pp <= 100 {
			perPage = pp
		}
	}
	offset := (page - 1) * perPage

	// Count total
	var total int64
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sent_emails WHERE domain_id = ?`, domainID).Scan(&total); err != nil {
		s.logger.Error("Failed to count sent emails", "error", err.Error())
		total = 0
	}

	// Fetch emails
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, message_id, tracking_id, from_email, to_email, subject, template_slug,
		       status, opened_at, opened_count, clicked_at, clicked_count, created_at
		FROM sent_emails
		WHERE domain_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, domainID, perPage, offset)
	if err != nil {
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var emails []SentEmail
	for rows.Next() {
		var e SentEmail
		var templateSlug, subject sql.NullString
		var openedAt, clickedAt sql.NullTime

		err := rows.Scan(&e.ID, &e.MessageID, &e.TrackingID, &e.FromEmail, &e.ToEmail,
			&subject, &templateSlug, &e.Status, &openedAt, &e.OpenedCount, &clickedAt, &e.ClickedCount, &e.CreatedAt)
		if err != nil {
			continue
		}

		if subject.Valid {
			e.Subject = subject.String
		}
		if templateSlug.Valid {
			e.TemplateSlug = templateSlug.String
		}
		if openedAt.Valid {
			e.OpenedAt = &openedAt.Time
		}
		if clickedAt.Valid {
			e.ClickedAt = &clickedAt.Time
		}

		emails = append(emails, e)
	}

	if err := rows.Err(); err != nil {
		s.logger.Error("Error iterating sent emails", "error", err.Error())
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	totalPages := int((total + int64(perPage) - 1) / int64(perPage))

	jsonResponse(w, ListResponse{
		Data:       emails,
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
	}, http.StatusOK)
}

// handleGetEmail handles GET /api/v1/emails/{id}
func (s *Server) handleGetEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/emails/")
	messageID := idStr

	domainID, _ := s.getDomainID(r.Context())

	var e SentEmail
	var templateSlug, subject, smtpResponse, bounceReason sql.NullString
	var openedAt, clickedAt, deliveredAt, bouncedAt sql.NullTime
	var apiKeyID sql.NullInt64

	err := s.db.QueryRowContext(r.Context(), `
		SELECT id, api_key_id, message_id, tracking_id, from_email, to_email, subject,
		       template_slug, status, smtp_response, opened_at, opened_count, clicked_at,
		       clicked_count, created_at, delivered_at, bounced_at, bounce_reason
		FROM sent_emails
		WHERE domain_id = ? AND message_id = ?
	`, domainID, messageID).Scan(
		&e.ID, &apiKeyID, &e.MessageID, &e.TrackingID, &e.FromEmail, &e.ToEmail,
		&subject, &templateSlug, &e.Status, &smtpResponse, &openedAt, &e.OpenedCount,
		&clickedAt, &e.ClickedCount, &e.CreatedAt, &deliveredAt, &bouncedAt, &bounceReason,
	)

	if err == sql.ErrNoRows {
		jsonError(w, "Email not found", "NOT_FOUND", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	if apiKeyID.Valid {
		e.APIKeyID = &apiKeyID.Int64
	}
	if subject.Valid {
		e.Subject = subject.String
	}
	if templateSlug.Valid {
		e.TemplateSlug = templateSlug.String
	}
	if smtpResponse.Valid {
		e.SMTPResponse = smtpResponse.String
	}
	if bounceReason.Valid {
		e.BounceReason = bounceReason.String
	}
	if openedAt.Valid {
		e.OpenedAt = &openedAt.Time
	}
	if clickedAt.Valid {
		e.ClickedAt = &clickedAt.Time
	}
	if deliveredAt.Valid {
		e.DeliveredAt = &deliveredAt.Time
	}
	if bouncedAt.Valid {
		e.BouncedAt = &bouncedAt.Time
	}

	jsonResponse(w, e, http.StatusOK)
}

// handleStats handles GET /api/v1/stats
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	domainID, _ := s.getDomainID(r.Context())

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "24h"
	}

	var duration time.Duration
	switch period {
	case "1h":
		duration = time.Hour
	case "24h":
		duration = 24 * time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	default:
		duration = 24 * time.Hour
	}

	endDate := time.Now()
	startDate := endDate.Add(-duration)

	var stats StatsResponse
	stats.Period = period
	stats.StartDate = startDate
	stats.EndDate = endDate

	// Count by status
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sent_emails WHERE domain_id = ? AND created_at >= ? AND status IN ('queued', 'sent')`, domainID, startDate).Scan(&stats.Sent); err != nil {
		s.logger.Warn("Failed to count sent emails", "error", err.Error())
	}
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sent_emails WHERE domain_id = ? AND created_at >= ? AND status = 'delivered'`, domainID, startDate).Scan(&stats.Delivered); err != nil {
		s.logger.Warn("Failed to count delivered emails", "error", err.Error())
	}
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sent_emails WHERE domain_id = ? AND created_at >= ? AND opened_count > 0`, domainID, startDate).Scan(&stats.Opened); err != nil {
		s.logger.Warn("Failed to count opened emails", "error", err.Error())
	}
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sent_emails WHERE domain_id = ? AND created_at >= ? AND clicked_count > 0`, domainID, startDate).Scan(&stats.Clicked); err != nil {
		s.logger.Warn("Failed to count clicked emails", "error", err.Error())
	}
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sent_emails WHERE domain_id = ? AND created_at >= ? AND status = 'bounced'`, domainID, startDate).Scan(&stats.Bounced); err != nil {
		s.logger.Warn("Failed to count bounced emails", "error", err.Error())
	}
	if err := s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sent_emails WHERE domain_id = ? AND created_at >= ? AND status = 'failed'`, domainID, startDate).Scan(&stats.Failed); err != nil {
		s.logger.Warn("Failed to count failed emails", "error", err.Error())
	}

	jsonResponse(w, stats, http.StatusOK)
}

// Helper functions

func parseJSONRequest(r *http.Request, v interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestSize))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

func validateSendRequest(req *SendEmailRequest) error {
	if req.From == "" {
		return fmt.Errorf("from is required")
	}
	if req.To == "" {
		return fmt.Errorf("to is required")
	}
	if req.Subject == "" {
		return fmt.Errorf("subject is required")
	}
	if req.HTML == "" && req.Text == "" {
		return fmt.Errorf("html or text content is required")
	}
	return nil
}

func generateMessageID(hostname string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if entropy unavailable
		return fmt.Sprintf("<%d@%s>", time.Now().UnixNano(), hostname)
	}
	return fmt.Sprintf("<%s@%s>", hex.EncodeToString(b), hostname)
}

func generateTrackingID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if entropy unavailable
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// queueEmail queues an email for delivery
func (s *Server) queueEmail(ctx context.Context, messageID, from, to, subject, html, text, replyTo string, headers map[string]string) error {
	// Build the email message
	msg := buildEmailMessage(messageID, from, to, subject, html, text, replyTo, headers)

	// Save message to disk
	messagePath, err := s.saveMessageToQueue([]byte(msg))
	if err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	// Queue via delivery engine
	if s.delivery != nil {
		if err := s.delivery.Enqueue(ctx, from, []string{to}, messagePath); err != nil {
			// Clean up orphaned file
			os.Remove(messagePath)
			return err
		}
		return nil
	}

	return fmt.Errorf("no delivery engine configured")
}

// saveMessageToQueue saves a message to the queue directory
func (s *Server) saveMessageToQueue(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("message data is empty")
	}

	// Generate unique filename
	filename := fmt.Sprintf("%d-%s.eml", time.Now().UnixNano(), generateTrackingID()[:16])
	path := filepath.Join(s.queuePath, filename)

	// Write file atomically using a temp file
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0600); err != nil {
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return "", fmt.Errorf("failed to rename temp file: %w", err)
	}

	return path, nil
}

// buildEmailMessage constructs a MIME email message
func buildEmailMessage(messageID, from, to, subject, html, text, replyTo string, headers map[string]string) string {
	var sb strings.Builder

	// Headers
	sb.WriteString(fmt.Sprintf("Message-ID: %s\r\n", messageID))
	sb.WriteString(fmt.Sprintf("From: %s\r\n", from))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", to))
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	sb.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	sb.WriteString("MIME-Version: 1.0\r\n")

	if replyTo != "" {
		sb.WriteString(fmt.Sprintf("Reply-To: %s\r\n", replyTo))
	}

	for k, v := range headers {
		sb.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}

	// Content
	if html != "" && text != "" {
		// Multipart alternative
		boundary := generateBoundary()
		sb.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary))

		sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		sb.WriteString(text)
		sb.WriteString("\r\n\r\n")

		sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		sb.WriteString("Content-Type: text/html; charset=utf-8\r\n")
		sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		sb.WriteString(html)
		sb.WriteString("\r\n\r\n")

		sb.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else if html != "" {
		sb.WriteString("Content-Type: text/html; charset=utf-8\r\n")
		sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		sb.WriteString(html)
	} else {
		sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		sb.WriteString(text)
	}

	return sb.String()
}

func generateBoundary() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if entropy unavailable
		return fmt.Sprintf("=_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("=_%s", hex.EncodeToString(b))
}
