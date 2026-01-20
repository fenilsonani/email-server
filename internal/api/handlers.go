package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
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

	"github.com/fenilsonani/email-server/internal/metrics"
)

const maxRequestSize = 1 << 20 // 1 MB

// handleSendEmail handles POST /api/v1/send
func (s *Server) handleSendEmail(w http.ResponseWriter, r *http.Request) {
	requestID := getRequestIDFromContext(r.Context())

	if r.Method != http.MethodPost {
		errorResponse(w, requestID, "Method not allowed", CodeMethodNotAllowed, http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req SendEmailRequest
	if err := parseJSONRequest(r, &req); err != nil {
		errorResponse(w, requestID, "Invalid request body", CodeInvalidRequest, http.StatusBadRequest)
		return
	}

	// Get domain ID and API key early (needed for idempotency)
	domainID, _ := s.getDomainID(r.Context())
	apiKey := getAPIKeyFromContext(r.Context())
	if apiKey == nil {
		errorResponse(w, requestID, "Authentication required", CodeUnauthorized, http.StatusUnauthorized)
		return
	}

	// Check idempotency key if provided
	if req.IdempotencyKey != "" && s.idempotency != nil {
		// Check for existing result
		result, err := s.idempotency.Check(r.Context(), domainID, req.IdempotencyKey)
		if err == ErrIdempotencyKeyInProgress {
			metrics.RecordIdempotencyConflict()
			errorResponse(w, requestID, "A request with this idempotency key is already in progress", CodeIdempotencyConflict, http.StatusConflict)
			return
		}
		if err != nil {
			s.logger.Error("Failed to check idempotency key", "error", err.Error())
			// Continue without idempotency on error
		} else if result != nil {
			// Return cached response
			metrics.RecordIdempotencyHit()
			w.Header().Set("X-Idempotent-Replayed", "true")
			successResponse(w, requestID, SendResponse{
				Success:            true,
				MessageID:          result.MessageID,
				Status:             result.Status,
				IdempotentReplayed: true,
			}, result.StatusCode)
			return
		}

		// Acquire lock for this idempotency key
		locked, err := s.idempotency.Lock(r.Context(), domainID, req.IdempotencyKey)
		if err != nil {
			s.logger.Error("Failed to acquire idempotency lock", "error", err.Error())
		} else if !locked {
			metrics.RecordIdempotencyConflict()
			errorResponse(w, requestID, "A request with this idempotency key is already in progress", CodeIdempotencyConflict, http.StatusConflict)
			return
		}
		// Defer unlock in case of error (successful response stores result instead)
		defer func() {
			if req.IdempotencyKey != "" && s.idempotency != nil {
				s.idempotency.Unlock(r.Context(), domainID, req.IdempotencyKey)
			}
		}()
	}

	// Sanitize input to prevent header injection
	if err := SanitizeSendRequest(&req); err != nil {
		if validErr, ok := err.(*ValidationError); ok {
			metrics.RecordValidationError(validErr.Field)
			errorResponseWithField(w, requestID, validErr.Message, CodeValidationError, validErr.Field, http.StatusBadRequest)
		} else {
			errorResponse(w, requestID, err.Error(), CodeValidationError, http.StatusBadRequest)
		}
		return
	}

	// Validate request
	if err := validateSendRequest(&req); err != nil {
		errorResponse(w, requestID, err.Error(), CodeValidationError, http.StatusBadRequest)
		return
	}

	// Check domain permission
	canSend, err := s.canSendFromDomain(r.Context(), req.From)
	if err != nil || !canSend {
		errorResponse(w, requestID, "Cannot send from this domain", CodeForbidden, http.StatusForbidden)
		return
	}

	// Check suppression list
	if s.suppression != nil {
		suppressed, suppression, err := s.suppression.IsSuppressed(r.Context(), domainID, req.To)
		if err != nil {
			s.logger.Error("Failed to check suppression list", "error", err.Error())
			// Continue on error - don't block sends due to suppression check failure
		} else if suppressed {
			metrics.RecordSuppressionHit()
			successResponse(w, requestID, SendResponse{
				Success:   true,
				MessageID: "", // No message ID for suppressed emails
				Status:    StatusSuppressed,
			}, http.StatusOK)
			// Store idempotency result for suppressed emails
			if req.IdempotencyKey != "" && s.idempotency != nil {
				s.idempotency.Store(r.Context(), domainID, req.IdempotencyKey, &IdempotencyResult{
					MessageID:  "",
					Status:     StatusSuppressed,
					StatusCode: http.StatusOK,
					CreatedAt:  time.Now(),
				})
			}
			s.logger.Info("Email suppressed",
				"to", req.To,
				"reason", suppression.Reason,
				"request_id", requestID)
			return
		}
	}

	// Generate message ID and tracking ID
	senderDomain := s.config.Server.Hostname
	if parts := splitEmail(req.From); len(parts) == 2 && parts[1] != "" {
		senderDomain = parts[1]
	}
	messageID := generateMessageID(senderDomain)
	trackingID := generateTrackingID()

	// Validate and handle scheduled sending
	if req.ScheduledAt != nil {
		if req.ScheduledAt.Before(time.Now()) {
			errorResponseWithField(w, requestID, "scheduled_at must be in the future", CodeValidationError, "scheduled_at", http.StatusBadRequest)
			return
		}
		if req.ScheduledAt.After(time.Now().Add(MaxScheduleTime)) {
			errorResponseWithField(w, requestID, "scheduled_at cannot be more than 30 days in the future", CodeValidationError, "scheduled_at", http.StatusBadRequest)
			return
		}

		// Store scheduled email
		tagsJSON, _ := json.Marshal(req.Tags)
		_, err := s.db.ExecContext(r.Context(), `
			INSERT INTO sent_emails (domain_id, api_key_id, message_id, tracking_id, from_email, to_email, subject, tags, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, domainID, apiKey.ID, messageID, trackingID, req.From, req.To, req.Subject, string(tagsJSON), StatusScheduled)
		if err != nil {
			s.logger.Error("Failed to store scheduled email record", "error", err.Error())
			errorResponse(w, requestID, "Internal server error", CodeInternalError, http.StatusInternalServerError)
			return
		}

		err = s.scheduleEmail(r.Context(), domainID, apiKey.ID, messageID, &req, *req.ScheduledAt)
		if err != nil {
			s.logger.Error("Failed to schedule email", "error", err.Error())
			s.db.ExecContext(r.Context(), `DELETE FROM sent_emails WHERE message_id = ?`, messageID)
			errorResponse(w, requestID, "Failed to schedule email", CodeSchedulingError, http.StatusInternalServerError)
			return
		}

		metrics.RecordEmailScheduled()

		// Store idempotency result
		if req.IdempotencyKey != "" && s.idempotency != nil {
			s.idempotency.Store(r.Context(), domainID, req.IdempotencyKey, &IdempotencyResult{
				MessageID:  messageID,
				Status:     StatusScheduled,
				StatusCode: http.StatusOK,
				CreatedAt:  time.Now(),
			})
		}

		successResponse(w, requestID, SendResponse{
			Success:     true,
			MessageID:   messageID,
			Status:      StatusScheduled,
			ScheduledAt: req.ScheduledAt,
		}, http.StatusOK)
		return
	}

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

	// Normalize priority
	priority := string(req.Priority)
	if priority == "" {
		priority = string(PriorityNormal)
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
		errorResponse(w, requestID, "Internal server error", CodeInternalError, http.StatusInternalServerError)
		return
	}

	// Queue the email for delivery
	err = s.queueEmailWithPriority(r.Context(), messageID, req.From, req.To, req.Subject, htmlBody, req.Text, req.ReplyTo, req.Headers, req.Attachments, priority)
	if err != nil {
		s.logger.Error("Failed to queue email", "error", err.Error())
		s.db.ExecContext(r.Context(), `UPDATE sent_emails SET status = ? WHERE message_id = ?`, StatusFailed, messageID)
		errorResponse(w, requestID, "Failed to queue email", CodeQueueError, http.StatusInternalServerError)
		return
	}

	metrics.RecordEmailQueued(priority)

	// Trigger webhook for queued event
	go s.triggerWebhook(r.Context(), domainID, EventQueued, &WebhookEvent{
		Event:     EventQueued,
		Timestamp: time.Now(),
		MessageID: messageID,
		Recipient: req.To,
	})

	// Store idempotency result
	if req.IdempotencyKey != "" && s.idempotency != nil {
		s.idempotency.Store(r.Context(), domainID, req.IdempotencyKey, &IdempotencyResult{
			MessageID:  messageID,
			Status:     StatusQueued,
			StatusCode: http.StatusOK,
			CreatedAt:  time.Now(),
		})
	}

	successResponse(w, requestID, SendResponse{
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

	// Queue email (templates don't support attachments yet)
	err = s.queueEmail(r.Context(), messageID, req.From, req.To, subject, htmlBody, textBody, req.ReplyTo, nil, nil)
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

		err = s.queueEmail(r.Context(), messageID, req.From, msg.To, msg.Subject, msg.HTML, msg.Text, "", nil, msg.Attachments)
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
	requestID := getRequestIDFromContext(r.Context())

	if r.Method != http.MethodGet {
		errorResponse(w, requestID, "Method not allowed", CodeMethodNotAllowed, http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/emails/")
	messageID := idStr

	domainID, _ := s.getDomainID(r.Context())

	var e EnhancedSentEmail
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
		errorResponse(w, requestID, "Email not found", CodeNotFound, http.StatusNotFound)
		return
	}
	if err != nil {
		errorResponse(w, requestID, "Internal server error", CodeInternalError, http.StatusInternalServerError)
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

	// Fetch delivery attempts
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT attempt_number, attempted_at, status, smtp_response, error_message
		FROM delivery_attempts
		WHERE sent_email_id = ?
		ORDER BY attempt_number ASC
	`, e.ID)
	if err == nil {
		defer rows.Close()
		var attempts []DeliveryAttempt
		for rows.Next() {
			var da DeliveryAttempt
			var smtpResp, errMsg sql.NullString
			if err := rows.Scan(&da.Attempt, &da.AttemptedAt, &da.Status, &smtpResp, &errMsg); err == nil {
				if smtpResp.Valid {
					da.SMTPResponse = smtpResp.String
				}
				if errMsg.Valid {
					da.ErrorMessage = errMsg.String
				}
				attempts = append(attempts, da)
			}
		}
		e.DeliveryAttempts = attempts
	}

	// Check if this was a scheduled email
	var scheduledAt sql.NullTime
	err = s.db.QueryRowContext(r.Context(), `
		SELECT scheduled_at FROM scheduled_emails WHERE message_id = ?
	`, messageID).Scan(&scheduledAt)
	if err == nil && scheduledAt.Valid {
		e.ScheduledAt = &scheduledAt.Time
	}

	successResponse(w, requestID, e, http.StatusOK)
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

	// Validate attachments
	if err := validateAttachments(req.Attachments); err != nil {
		return err
	}

	return nil
}

// Default attachment limits
const (
	defaultMaxAttachmentSize       = 10 * 1024 * 1024  // 10MB per attachment
	defaultMaxTotalAttachmentsSize = 25 * 1024 * 1024  // 25MB total
	defaultMaxAttachmentCount      = 10
)

// validateAttachments validates all attachments
func validateAttachments(attachments []Attachment) error {
	return validateAttachmentsWithConfig(attachments, nil)
}

// validateAttachmentsWithConfig validates attachments with optional config for blocked extensions
func validateAttachmentsWithConfig(attachments []Attachment, blockedExtensions []string) error {
	if len(attachments) == 0 {
		return nil
	}

	if len(attachments) > defaultMaxAttachmentCount {
		return fmt.Errorf("too many attachments (max %d)", defaultMaxAttachmentCount)
	}

	// Build blocked extensions map from config
	blocked := make(map[string]bool)
	for _, ext := range blockedExtensions {
		blocked[strings.ToLower(ext)] = true
	}

	var totalSize int64

	for i, att := range attachments {
		// Validate filename
		if att.Filename == "" {
			return fmt.Errorf("attachment %d: filename is required", i+1)
		}

		// Check for blocked extensions (only if configured)
		if len(blocked) > 0 {
			ext := strings.ToLower(filepath.Ext(att.Filename))
			if blocked[ext] {
				return fmt.Errorf("attachment %d: file type %s is not allowed", i+1, ext)
			}
		}

		// Validate content is present
		if att.Content == "" {
			return fmt.Errorf("attachment %d: content is required", i+1)
		}

		// Validate base64 encoding
		decoded, err := base64.StdEncoding.DecodeString(att.Content)
		if err != nil {
			return fmt.Errorf("attachment %d: invalid base64 encoding", i+1)
		}

		// Check individual attachment size
		if len(decoded) > defaultMaxAttachmentSize {
			return fmt.Errorf("attachment %d: size exceeds maximum of %d MB", i+1, defaultMaxAttachmentSize/(1024*1024))
		}

		totalSize += int64(len(decoded))

		// Validate Content-Type format if provided
		if att.ContentType != "" {
			if !isValidContentType(att.ContentType) {
				return fmt.Errorf("attachment %d: invalid content type format", i+1)
			}
		}
	}

	// Check total size
	if totalSize > defaultMaxTotalAttachmentsSize {
		return fmt.Errorf("total attachments size exceeds maximum of %d MB", defaultMaxTotalAttachmentsSize/(1024*1024))
	}

	return nil
}

// isValidContentType checks if a content type has valid format
func isValidContentType(ct string) bool {
	// Basic validation - must have type/subtype format
	parts := strings.Split(ct, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
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

// queueEmail queues an email for delivery with normal priority
func (s *Server) queueEmail(ctx context.Context, messageID, from, to, subject, html, text, replyTo string, headers map[string]string, attachments []Attachment) error {
	return s.queueEmailWithPriority(ctx, messageID, from, to, subject, html, text, replyTo, headers, attachments, string(PriorityNormal))
}

// queueEmailWithPriority queues an email for delivery with specified priority
func (s *Server) queueEmailWithPriority(ctx context.Context, messageID, from, to, subject, html, text, replyTo string, headers map[string]string, attachments []Attachment, priority string) error {
	// Build the email message
	msg := buildEmailMessage(messageID, from, to, subject, html, text, replyTo, headers, attachments)

	// Save message to disk
	messagePath, err := s.saveMessageToQueue([]byte(msg))
	if err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	// Queue via delivery engine
	if s.delivery != nil {
		// TODO: When priority queue support is added to the delivery engine,
		// pass priority here: s.delivery.EnqueueWithPriority(ctx, from, []string{to}, messagePath, priority)
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
func buildEmailMessage(messageID, from, to, subject, html, text, replyTo string, headers map[string]string, attachments []Attachment) string {
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

	hasAttachments := len(attachments) > 0
	hasMultipleBodyTypes := html != "" && text != ""

	if hasAttachments {
		// Use multipart/mixed as outer container for attachments
		mixedBoundary := generateBoundary()
		sb.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", mixedBoundary))

		// First part: the message body
		sb.WriteString(fmt.Sprintf("--%s\r\n", mixedBoundary))

		if hasMultipleBodyTypes {
			// Nested multipart/alternative for text+HTML
			altBoundary := generateBoundary()
			sb.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", altBoundary))

			sb.WriteString(fmt.Sprintf("--%s\r\n", altBoundary))
			sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
			sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
			sb.WriteString(text)
			sb.WriteString("\r\n\r\n")

			sb.WriteString(fmt.Sprintf("--%s\r\n", altBoundary))
			sb.WriteString("Content-Type: text/html; charset=utf-8\r\n")
			sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
			sb.WriteString(html)
			sb.WriteString("\r\n\r\n")

			sb.WriteString(fmt.Sprintf("--%s--\r\n", altBoundary))
		} else if html != "" {
			sb.WriteString("Content-Type: text/html; charset=utf-8\r\n")
			sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
			sb.WriteString(html)
			sb.WriteString("\r\n")
		} else {
			sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
			sb.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
			sb.WriteString(text)
			sb.WriteString("\r\n")
		}

		// Attachment parts
		for _, att := range attachments {
			sb.WriteString(fmt.Sprintf("\r\n--%s\r\n", mixedBoundary))
			writeAttachmentPart(&sb, att)
		}

		sb.WriteString(fmt.Sprintf("\r\n--%s--\r\n", mixedBoundary))
	} else {
		// No attachments - use existing logic
		if hasMultipleBodyTypes {
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
	}

	return sb.String()
}

// writeAttachmentPart writes a single attachment part to the message
func writeAttachmentPart(sb *strings.Builder, att Attachment) {
	contentType := att.ContentType
	if contentType == "" {
		contentType = detectContentType(att.Filename)
	}

	// Sanitize filename to prevent header injection
	safeFilename := sanitizeFilename(att.Filename)

	sb.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", contentType, safeFilename))
	sb.WriteString("Content-Transfer-Encoding: base64\r\n")
	sb.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", safeFilename))
	sb.WriteString("\r\n")

	// Write base64 content in 76-char lines per RFC 2045
	content := att.Content
	for len(content) > 76 {
		sb.WriteString(content[:76])
		sb.WriteString("\r\n")
		content = content[76:]
	}
	if len(content) > 0 {
		sb.WriteString(content)
		sb.WriteString("\r\n")
	}
}

// detectContentType returns the MIME type based on file extension
func detectContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".zip":
		return "application/zip"
	case ".txt":
		return "text/plain"
	case ".csv":
		return "text/csv"
	case ".html", ".htm":
		return "text/html"
	case ".xml":
		return "application/xml"
	case ".json":
		return "application/json"
	case ".mp3":
		return "audio/mpeg"
	case ".mp4":
		return "video/mp4"
	case ".wav":
		return "audio/wav"
	case ".avi":
		return "video/x-msvideo"
	default:
		return "application/octet-stream"
	}
}

// sanitizeFilename removes potentially dangerous characters from filename
func sanitizeFilename(filename string) string {
	// Remove path separators and control characters
	filename = filepath.Base(filename)
	// Remove quotes and newlines that could break MIME headers
	filename = strings.ReplaceAll(filename, "\"", "")
	filename = strings.ReplaceAll(filename, "\r", "")
	filename = strings.ReplaceAll(filename, "\n", "")
	return filename
}

func generateBoundary() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if entropy unavailable
		return fmt.Sprintf("=_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("=_%s", hex.EncodeToString(b))
}
