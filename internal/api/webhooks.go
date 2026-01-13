package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	webhookTimeout     = 10 * time.Second
	maxWebhookRetries  = 3
	webhookMaxFailures = 5
)

// triggerWebhook sends webhook events to registered endpoints
func (s *Server) triggerWebhook(ctx context.Context, domainID int64, eventType string, event *WebhookEvent) {
	// Get active webhooks for this domain and event type
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, url, events, secret
		FROM webhooks
		WHERE domain_id = ? AND is_active = TRUE AND failure_count < ?
	`, domainID, webhookMaxFailures)
	if err != nil {
		s.logger.Error("Failed to query webhooks", "error", err.Error())
		return
	}
	defer rows.Close()

	for rows.Next() {
		var webhook Webhook
		var eventsJSON string

		if err := rows.Scan(&webhook.ID, &webhook.URL, &eventsJSON, &webhook.Secret); err != nil {
			continue
		}

		// Parse events
		if err := json.Unmarshal([]byte(eventsJSON), &webhook.Events); err != nil {
			s.logger.Warn("Failed to parse webhook events", "webhook_id", webhook.ID, "error", err.Error())
			continue
		}

		// Check if this webhook subscribes to this event
		if !containsEvent(webhook.Events, eventType) {
			continue
		}

		// Send webhook in goroutine
		go s.deliverWebhook(ctx, &webhook, event)
	}

	if err := rows.Err(); err != nil {
		s.logger.Error("Error iterating webhooks", "error", err.Error())
	}
}

// containsEvent checks if an event list contains a specific event
func containsEvent(events []string, target string) bool {
	for _, e := range events {
		if e == target || e == "*" {
			return true
		}
	}
	return false
}

// deliverWebhook delivers a webhook event to an endpoint
func (s *Server) deliverWebhook(ctx context.Context, webhook *Webhook, event *WebhookEvent) {
	// Serialize payload
	payload, err := json.Marshal(event)
	if err != nil {
		s.logger.Error("Failed to marshal webhook payload", "error", err.Error())
		return
	}

	// Generate signature
	signature := generateWebhookSignature(payload, webhook.Secret)

	var lastErr error
	var responseCode int
	var responseBody string

	for attempt := 1; attempt <= maxWebhookRetries; attempt++ {
		responseCode, responseBody, lastErr = s.sendWebhookRequest(webhook.URL, payload, signature)

		if lastErr == nil && responseCode >= 200 && responseCode < 300 {
			// Success
			s.recordWebhookSuccess(webhook.ID, event.Event, payload, responseCode, responseBody)
			return
		}

		// Wait before retry (exponential backoff)
		if attempt < maxWebhookRetries {
			time.Sleep(time.Duration(attempt*attempt) * time.Second)
		}
	}

	// All retries failed
	s.recordWebhookFailure(webhook.ID, event.Event, payload, responseCode, responseBody, lastErr)
}

// sendWebhookRequest sends the HTTP request for a webhook
func (s *Server) sendWebhookRequest(url string, payload []byte, signature string) (int, string, error) {
	client := &http.Client{
		Timeout: webhookTimeout,
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signature)
	req.Header.Set("X-Webhook-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	req.Header.Set("User-Agent", "MailServer-Webhook/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(body), nil
}

// generateWebhookSignature creates an HMAC-SHA256 signature
func generateWebhookSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// recordWebhookSuccess records a successful webhook delivery
func (s *Server) recordWebhookSuccess(webhookID int64, eventType string, payload []byte, responseCode int, responseBody string) {
	now := time.Now()

	// Update webhook stats
	if _, err := s.db.Exec(`
		UPDATE webhooks
		SET last_triggered_at = ?, last_success_at = ?, failure_count = 0
		WHERE id = ?
	`, now, now, webhookID); err != nil {
		s.logger.Warn("Failed to update webhook stats", "webhook_id", webhookID, "error", err.Error())
	}

	// Log delivery
	if _, err := s.db.Exec(`
		INSERT INTO webhook_deliveries (webhook_id, event_type, payload, response_code, response_body, success, created_at)
		VALUES (?, ?, ?, ?, ?, TRUE, ?)
	`, webhookID, eventType, string(payload), responseCode, truncateString(responseBody, 1000), now); err != nil {
		s.logger.Warn("Failed to record webhook delivery", "webhook_id", webhookID, "error", err.Error())
	}
}

// recordWebhookFailure records a failed webhook delivery
func (s *Server) recordWebhookFailure(webhookID int64, eventType string, payload []byte, responseCode int, responseBody string, err error) {
	now := time.Now()

	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	if responseBody != "" && errMsg == "" {
		errMsg = responseBody
	}

	// Update webhook stats
	if _, dbErr := s.db.Exec(`
		UPDATE webhooks
		SET last_triggered_at = ?, last_failure_at = ?, last_failure_reason = ?, failure_count = failure_count + 1
		WHERE id = ?
	`, now, now, truncateString(errMsg, 500), webhookID); dbErr != nil {
		s.logger.Warn("Failed to update webhook failure stats", "webhook_id", webhookID, "error", dbErr.Error())
	}

	// Log delivery
	if _, dbErr := s.db.Exec(`
		INSERT INTO webhook_deliveries (webhook_id, event_type, payload, response_code, response_body, success, attempt_count, created_at)
		VALUES (?, ?, ?, ?, ?, FALSE, ?, ?)
	`, webhookID, eventType, string(payload), responseCode, truncateString(responseBody, 1000), maxWebhookRetries, now); dbErr != nil {
		s.logger.Warn("Failed to record webhook failure delivery", "webhook_id", webhookID, "error", dbErr.Error())
	}

	s.logger.Warn("Webhook delivery failed",
		"webhook_id", webhookID,
		"event_type", eventType,
		"response_code", responseCode,
		"error", errMsg,
	)
}

// truncateString truncates a string to max length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// Webhook management handlers (for admin use via API keys with admin scope)

// handleWebhooks handles GET/POST /api/v1/webhooks
func (s *Server) handleWebhooks(w http.ResponseWriter, r *http.Request) {
	domainID, _ := s.getDomainID(r.Context())

	switch r.Method {
	case http.MethodGet:
		s.listWebhooks(w, r, domainID)
	case http.MethodPost:
		s.createWebhook(w, r, domainID)
	default:
		jsonError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
	}
}

// listWebhooks returns all webhooks for a domain
func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request, domainID int64) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, url, events, is_active, failure_count, last_triggered_at, last_success_at, last_failure_at, last_failure_reason, created_at
		FROM webhooks
		WHERE domain_id = ?
		ORDER BY created_at DESC
	`, domainID)
	if err != nil {
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var webhooks []Webhook
	for rows.Next() {
		var wh Webhook
		var eventsJSON string
		var lastTriggered, lastSuccess, lastFailure sql.NullTime
		var lastFailureReason sql.NullString

		err := rows.Scan(&wh.ID, &wh.URL, &eventsJSON, &wh.IsActive, &wh.FailureCount,
			&lastTriggered, &lastSuccess, &lastFailure, &lastFailureReason, &wh.CreatedAt)
		if err != nil {
			continue
		}

		if err := json.Unmarshal([]byte(eventsJSON), &wh.Events); err != nil {
			s.logger.Warn("Failed to parse webhook events", "webhook_id", wh.ID, "error", err.Error())
			continue
		}

		if lastTriggered.Valid {
			wh.LastTriggeredAt = &lastTriggered.Time
		}
		if lastSuccess.Valid {
			wh.LastSuccessAt = &lastSuccess.Time
		}
		if lastFailure.Valid {
			wh.LastFailureAt = &lastFailure.Time
		}
		if lastFailureReason.Valid {
			wh.LastFailureReason = lastFailureReason.String
		}

		wh.DomainID = domainID
		webhooks = append(webhooks, wh)
	}

	jsonResponse(w, webhooks, http.StatusOK)
}

// createWebhook creates a new webhook
func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request, domainID int64) {
	var req CreateWebhookRequest
	if err := parseJSONRequest(r, &req); err != nil {
		jsonError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	// Validate
	if req.URL == "" {
		jsonError(w, "url is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	if len(req.Events) == 0 {
		jsonError(w, "events array is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Validate URL
	if !strings.HasPrefix(req.URL, "https://") {
		jsonError(w, "webhook URL must use HTTPS", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Validate events
	validEvents := map[string]bool{
		"*":           true,
		EventQueued:   true,
		EventSent:     true,
		EventDelivered: true,
		EventBounced:  true,
		EventOpened:   true,
		EventClicked:  true,
		EventFailed:   true,
	}
	for _, e := range req.Events {
		if !validEvents[e] {
			jsonError(w, fmt.Sprintf("invalid event type: %s", e), "VALIDATION_ERROR", http.StatusBadRequest)
			return
		}
	}

	// Generate secret
	secret := generateWebhookSecret()
	eventsJSON, _ := json.Marshal(req.Events)
	now := time.Now()

	result, err := s.db.ExecContext(r.Context(), `
		INSERT INTO webhooks (domain_id, url, events, secret, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, domainID, req.URL, string(eventsJSON), secret, now)

	if err != nil {
		s.logger.Error("Failed to create webhook", "error", err.Error())
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	// Return webhook with secret (only shown on creation)
	jsonResponse(w, map[string]interface{}{
		"id":         id,
		"url":        req.URL,
		"events":     req.Events,
		"secret":     secret,
		"is_active":  true,
		"created_at": now,
	}, http.StatusCreated)
}

// generateWebhookSecret generates a random webhook secret
func generateWebhookSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if entropy unavailable
		return "whsec_" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return "whsec_" + hex.EncodeToString(b)
}
