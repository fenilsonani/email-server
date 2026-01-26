package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Maximum scheduling time (30 days from now)
const MaxScheduleTime = 30 * 24 * time.Hour

// Scheduler processes scheduled emails.
type Scheduler struct {
	db       *sql.DB
	server   *Server
	interval time.Duration
	running  int32
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewScheduler creates a new email scheduler.
func NewScheduler(db *sql.DB, server *Server, interval time.Duration) *Scheduler {
	if interval == 0 {
		interval = 30 * time.Second // Check every 30 seconds
	}
	return &Scheduler{
		db:       db,
		server:   server,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start starts the scheduler background processor.
func (s *Scheduler) Start() {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return // Already running
	}

	s.wg.Add(1)
	go s.run()
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	if !atomic.CompareAndSwapInt32(&s.running, 1, 0) {
		return // Not running
	}

	close(s.stopCh)
	s.wg.Wait()
}

func (s *Scheduler) run() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Process any due emails immediately on startup
	s.processDueEmails()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.processDueEmails()
		}
	}
}

func (s *Scheduler) processDueEmails() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Fetch scheduled emails that are due
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, domain_id, api_key_id, message_id, request_payload, scheduled_at
		FROM scheduled_emails
		WHERE status = 'pending' AND scheduled_at <= ?
		ORDER BY scheduled_at ASC
		LIMIT 100
	`, time.Now())
	if err != nil {
		s.server.logger.Error("Failed to fetch scheduled emails", "error", err.Error())
		return
	}
	defer rows.Close()

	var scheduled []ScheduledEmail
	for rows.Next() {
		var se ScheduledEmail
		err := rows.Scan(&se.ID, &se.DomainID, &se.APIKeyID, &se.MessageID, &se.RequestPayload, &se.ScheduledAt)
		if err != nil {
			s.server.logger.Error("Failed to scan scheduled email", "error", err.Error())
			continue
		}
		scheduled = append(scheduled, se)
	}

	if err := rows.Err(); err != nil {
		s.server.logger.Error("Error iterating scheduled emails", "error", err.Error())
		return
	}

	// Process each scheduled email
	for _, se := range scheduled {
		s.processScheduledEmail(ctx, &se)
	}
}

func (s *Scheduler) processScheduledEmail(ctx context.Context, se *ScheduledEmail) {
	// Deserialize the request
	var req SendEmailRequest
	if err := json.Unmarshal([]byte(se.RequestPayload), &req); err != nil {
		s.server.logger.Error("Failed to unmarshal scheduled email request",
			"id", se.ID,
			"error", err.Error())
		s.markScheduledEmailFailed(ctx, se.ID, "invalid request payload")
		return
	}

	// Mark as processing (to prevent double processing)
	_, err := s.db.ExecContext(ctx, `
		UPDATE scheduled_emails SET status = 'processing' WHERE id = ? AND status = 'pending'
	`, se.ID)
	if err != nil {
		s.server.logger.Error("Failed to mark scheduled email as processing",
			"id", se.ID,
			"error", err.Error())
		return
	}

	// Extract sender domain for Message-ID
	senderDomain := s.server.config.Server.Hostname
	if parts := splitEmail(req.From); len(parts) == 2 && parts[1] != "" {
		senderDomain = parts[1]
	}

	// Use the existing message ID
	messageID := se.MessageID
	trackingID := generateTrackingID()

	// Process HTML for tracking if enabled
	htmlBody := req.HTML
	if s.server.config.API.EnableTracking {
		if req.TrackOpens {
			htmlBody = injectOpenTracking(htmlBody, trackingID, s.server.config.API.TrackingDomain, senderDomain)
		}
		if req.TrackClicks {
			htmlBody = rewriteLinksForTracking(htmlBody, trackingID, s.server.config.API.TrackingDomain, senderDomain)
		}
	}

	// Update sent_emails record
	tagsJSON, err := json.Marshal(req.Tags)
	if err != nil {
		tagsJSON = []byte("[]")
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE sent_emails SET tracking_id = ?, tags = ?, status = ? WHERE message_id = ?
	`, trackingID, string(tagsJSON), StatusQueued, messageID)
	if err != nil {
		s.server.logger.Error("Failed to update sent email record",
			"id", se.ID,
			"message_id", messageID,
			"error", err.Error())
	}

	// Queue the email for delivery
	err = s.server.queueEmail(ctx, messageID, req.From, req.To, req.Subject, htmlBody, req.Text, req.ReplyTo, req.Headers, req.Attachments)
	if err != nil {
		s.server.logger.Error("Failed to queue scheduled email",
			"id", se.ID,
			"message_id", messageID,
			"error", err.Error())
		s.markScheduledEmailFailed(ctx, se.ID, err.Error())
		// Update sent_emails status
		s.db.ExecContext(ctx, `UPDATE sent_emails SET status = ? WHERE message_id = ?`, StatusFailed, messageID)
		return
	}

	// Mark as sent
	now := time.Now()
	_, err = s.db.ExecContext(ctx, `
		UPDATE scheduled_emails SET status = 'sent', processed_at = ? WHERE id = ?
	`, now, se.ID)
	if err != nil {
		s.server.logger.Error("Failed to mark scheduled email as sent",
			"id", se.ID,
			"error", err.Error())
	}

	// Trigger webhook for queued event
	go s.server.triggerWebhook(ctx, se.DomainID, EventQueued, &WebhookEvent{
		Event:     EventQueued,
		Timestamp: now,
		MessageID: messageID,
		Recipient: req.To,
	})

	s.server.logger.Info("Processed scheduled email",
		"id", se.ID,
		"message_id", messageID,
		"to", req.To)
}

func (s *Scheduler) markScheduledEmailFailed(ctx context.Context, id int64, reason string) {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		UPDATE scheduled_emails SET status = 'failed', processed_at = ? WHERE id = ?
	`, now, id)
	if err != nil {
		s.server.logger.Error("Failed to mark scheduled email as failed",
			"id", id,
			"error", err.Error())
	}
}

// Schedule schedules an email for future delivery.
func (s *Server) scheduleEmail(ctx context.Context, domainID, apiKeyID int64, messageID string, req *SendEmailRequest, scheduledAt time.Time) error {
	// Validate scheduling time
	if scheduledAt.Before(time.Now()) {
		return fmt.Errorf("scheduled_at must be in the future")
	}
	if scheduledAt.After(time.Now().Add(MaxScheduleTime)) {
		return fmt.Errorf("scheduled_at cannot be more than 30 days in the future")
	}

	// Serialize the request
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to serialize request: %w", err)
	}

	// Store scheduled email
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO scheduled_emails (domain_id, api_key_id, message_id, request_payload, scheduled_at, status)
		VALUES (?, ?, ?, ?, ?, 'pending')
	`, domainID, apiKeyID, messageID, string(payload), scheduledAt)
	if err != nil {
		return fmt.Errorf("failed to store scheduled email: %w", err)
	}

	return nil
}

// CancelScheduledEmail cancels a scheduled email.
func (s *Server) cancelScheduledEmail(ctx context.Context, domainID int64, messageID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE scheduled_emails SET status = 'cancelled'
		WHERE domain_id = ? AND message_id = ? AND status = 'pending'
	`, domainID, messageID)
	if err != nil {
		return fmt.Errorf("failed to cancel scheduled email: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("scheduled email not found or already processed")
	}

	// Also update the sent_emails status
	s.db.ExecContext(ctx, `UPDATE sent_emails SET status = 'cancelled' WHERE message_id = ?`, messageID)

	return nil
}
