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
	"sync"
	"sync/atomic"
	"time"
)

const (
	webhookTimeout        = 10 * time.Second
	maxWebhookRetries     = 3
	webhookMaxFailures    = 5
	maxConcurrentWebhooks = 50 // Maximum concurrent webhook deliveries

	// Extended retry settings for the background retry worker
	webhookMaxRetryAttempts = 8              // Total attempts including initial 3
	webhookRetryMaxAge      = 24 * time.Hour // Stop retrying after 24 hours
	webhookRetryInterval    = 1 * time.Minute // How often the retry worker runs
)

// webhookSemaphore limits concurrent webhook deliveries to prevent goroutine explosion
var webhookSemaphore = make(chan struct{}, maxConcurrentWebhooks)

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

		// Send webhook with bounded concurrency
		// Copy webhook to avoid closure capture issues
		wh := webhook
		go func() {
			// Acquire semaphore (blocks if too many concurrent webhooks)
			select {
			case webhookSemaphore <- struct{}{}:
				defer func() { <-webhookSemaphore }()
				s.deliverWebhook(ctx, &wh, event)
			case <-time.After(30 * time.Second):
				// Timeout waiting for semaphore - too many webhooks queued
				s.logger.Warn("Webhook delivery skipped: too many concurrent deliveries",
					"webhook_id", wh.ID,
					"event", event.Event,
				)
			}
		}()
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
	eventsJSON, err := json.Marshal(req.Events)
	if err != nil {
		s.logger.Error("Failed to marshal webhook events", "error", err.Error())
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
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

	id, err := result.LastInsertId()
	if err != nil {
		s.logger.Debug("Failed to get webhook insert ID", "error", err.Error())
		// Continue anyway - webhook was created, just can't get ID
		id = 0
	}

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

// =============================================================================
// Webhook Retry Worker
// =============================================================================

// WebhookRetryWorker retries failed webhook deliveries with extended backoff.
type WebhookRetryWorker struct {
	db      *sql.DB
	server  *Server
	running int32
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewWebhookRetryWorker creates a new webhook retry worker.
func NewWebhookRetryWorker(db *sql.DB, server *Server) *WebhookRetryWorker {
	return &WebhookRetryWorker{
		db:     db,
		server: server,
		stopCh: make(chan struct{}),
	}
}

// Start begins the retry worker.
func (w *WebhookRetryWorker) Start() {
	if !atomic.CompareAndSwapInt32(&w.running, 0, 1) {
		return
	}
	w.wg.Add(1)
	go w.run()
}

// Stop stops the retry worker.
func (w *WebhookRetryWorker) Stop() {
	if !atomic.CompareAndSwapInt32(&w.running, 1, 0) {
		return
	}
	close(w.stopCh)
	w.wg.Wait()
}

func (w *WebhookRetryWorker) run() {
	defer w.wg.Done()

	ticker := time.NewTicker(webhookRetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.retryFailedDeliveries()
		}
	}
}

// webhookRetryBackoff returns the minimum age a failed delivery must have
// before the next retry attempt. Backoff schedule:
// attempt 4: 1 min, attempt 5: 5 min, attempt 6: 30 min, attempt 7: 2 hours, attempt 8: 8 hours
func webhookRetryBackoff(attemptCount int) time.Duration {
	switch {
	case attemptCount <= 3:
		return 1 * time.Minute
	case attemptCount == 4:
		return 5 * time.Minute
	case attemptCount == 5:
		return 30 * time.Minute
	case attemptCount == 6:
		return 2 * time.Hour
	default:
		return 8 * time.Hour
	}
}

func (w *WebhookRetryWorker) retryFailedDeliveries() {
	ctx := context.Background()
	cutoff := time.Now().Add(-webhookRetryMaxAge)

	// Find failed deliveries that are eligible for retry:
	// - failed (success = FALSE)
	// - not yet exhausted (attempt_count < max)
	// - not too old (created_at > cutoff)
	rows, err := w.db.QueryContext(ctx, `
		SELECT wd.id, wd.webhook_id, wd.event_type, wd.payload, wd.attempt_count, wd.created_at,
		       wh.url, wh.secret, wh.is_active
		FROM webhook_deliveries wd
		JOIN webhooks wh ON wd.webhook_id = wh.id
		WHERE wd.success = FALSE
		  AND wd.attempt_count < ?
		  AND wd.created_at > ?
		  AND wh.is_active = TRUE
		  AND wh.failure_count < ?
		ORDER BY wd.created_at ASC
		LIMIT 50
	`, webhookMaxRetryAttempts, cutoff, webhookMaxFailures)
	if err != nil {
		w.server.logger.Error("Failed to query failed webhook deliveries", "error", err.Error())
		return
	}
	defer rows.Close()

	type failedDelivery struct {
		ID           int64
		WebhookID    int64
		EventType    string
		Payload      string
		AttemptCount int
		CreatedAt    time.Time
		URL          string
		Secret       string
	}

	var deliveries []failedDelivery
	for rows.Next() {
		var d failedDelivery
		var isActive bool
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.EventType, &d.Payload, &d.AttemptCount, &d.CreatedAt, &d.URL, &d.Secret, &isActive); err != nil {
			continue
		}
		// Check backoff: only retry if enough time has passed since creation
		backoff := webhookRetryBackoff(d.AttemptCount)
		if time.Since(d.CreatedAt) < backoff {
			continue
		}
		deliveries = append(deliveries, d)
	}

	for _, d := range deliveries {
		signature := generateWebhookSignature([]byte(d.Payload), d.Secret)
		respCode, respBody, err := w.server.sendWebhookRequest(d.URL, []byte(d.Payload), signature)

		if err == nil && respCode >= 200 && respCode < 300 {
			// Success — mark delivery as successful
			w.db.ExecContext(ctx, `UPDATE webhook_deliveries SET success = TRUE, response_code = ?, response_body = ?, attempt_count = ? WHERE id = ?`,
				respCode, truncateString(respBody, 1000), d.AttemptCount+1, d.ID)
			// Reset webhook failure count
			w.db.ExecContext(ctx, `UPDATE webhooks SET failure_count = 0, last_success_at = ?, last_triggered_at = ? WHERE id = ?`,
				time.Now(), time.Now(), d.WebhookID)
			w.server.logger.Info("Webhook retry succeeded",
				"delivery_id", d.ID, "webhook_id", d.WebhookID, "attempt", d.AttemptCount+1)
		} else {
			// Still failing — increment attempt count
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			w.db.ExecContext(ctx, `UPDATE webhook_deliveries SET attempt_count = ?, response_code = ?, response_body = ? WHERE id = ?`,
				d.AttemptCount+1, respCode, truncateString(errMsg, 1000), d.ID)

			if d.AttemptCount+1 >= webhookMaxRetryAttempts {
				w.server.logger.Warn("Webhook delivery exhausted all retries",
					"delivery_id", d.ID, "webhook_id", d.WebhookID, "attempts", d.AttemptCount+1)
			}
		}
	}
}
