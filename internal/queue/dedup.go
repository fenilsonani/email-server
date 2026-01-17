package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// DeliveryTracker tracks message delivery state for deduplication.
// It prevents double delivery if a worker crashes after SMTP DATA
// but before marking the message as complete.
type DeliveryTracker struct {
	client redis.UniversalClient
	prefix string
	ttl    time.Duration
}

// DeliveryState represents the delivery state of a message.
type DeliveryState struct {
	SMTPMessageID string    `json:"smtp_message_id"`
	QueueID       string    `json:"queue_id"`
	Recipients    []string  `json:"recipients"`
	State         string    `json:"state"` // pending, delivering, delivered, failed
	WorkerID      string    `json:"worker_id"`
	SMTPResponse  string    `json:"smtp_response,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	CompletedAt   time.Time `json:"completed_at,omitempty"`
}

// State constants
const (
	StatePending    = "pending"
	StateDelivering = "delivering"
	StateDelivered  = "delivered"
	StateFailed     = "failed"
)

// ErrAlreadyDelivered is returned when a message has already been delivered.
var ErrAlreadyDelivered = errors.New("message already delivered")

// NewDeliveryTracker creates a new delivery tracker.
func NewDeliveryTracker(client redis.UniversalClient, prefix string, ttl time.Duration) *DeliveryTracker {
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour // 7 days default
	}
	return &DeliveryTracker{
		client: client,
		prefix: prefix,
		ttl:    ttl,
	}
}

// dedupKey returns the Redis key for a message's dedup state.
func (t *DeliveryTracker) dedupKey(smtpMessageID string) string {
	return fmt.Sprintf("%s:dedup:%s", t.prefix, smtpMessageID)
}

// StartDelivery marks a message as being delivered.
// Returns ErrAlreadyDelivered if the message is already delivered or being delivered.
func (t *DeliveryTracker) StartDelivery(ctx context.Context, smtpMessageID, queueID, workerID string, recipients []string) error {
	if smtpMessageID == "" {
		return nil // No Message-ID, skip dedup tracking
	}

	key := t.dedupKey(smtpMessageID)

	// Check current state
	existing, err := t.GetState(ctx, smtpMessageID)
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to check dedup state: %w", err)
	}

	if existing != nil {
		switch existing.State {
		case StateDelivered:
			return ErrAlreadyDelivered
		case StateDelivering:
			// Check if the previous delivery attempt is stale (> 10 minutes)
			if time.Since(existing.StartedAt) < 10*time.Minute {
				return ErrAlreadyDelivered
			}
			// Stale, allow new delivery attempt
		case StateFailed:
			// Allow retry of failed messages
		}
	}

	state := DeliveryState{
		SMTPMessageID: smtpMessageID,
		QueueID:       queueID,
		Recipients:    recipients,
		State:         StateDelivering,
		WorkerID:      workerID,
		StartedAt:     time.Now(),
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal dedup state: %w", err)
	}

	// Set with TTL
	if err := t.client.Set(ctx, key, data, t.ttl).Err(); err != nil {
		return fmt.Errorf("failed to set dedup state: %w", err)
	}

	return nil
}

// MarkDelivered marks a message as successfully delivered.
func (t *DeliveryTracker) MarkDelivered(ctx context.Context, smtpMessageID, smtpResponse string) error {
	if smtpMessageID == "" {
		return nil
	}

	key := t.dedupKey(smtpMessageID)

	// Get current state
	state, err := t.GetState(ctx, smtpMessageID)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// No existing state, create new delivered state
			state = &DeliveryState{
				SMTPMessageID: smtpMessageID,
				StartedAt:     time.Now(),
			}
		} else {
			return fmt.Errorf("failed to get dedup state: %w", err)
		}
	}

	state.State = StateDelivered
	state.SMTPResponse = smtpResponse
	state.CompletedAt = time.Now()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal dedup state: %w", err)
	}

	if err := t.client.Set(ctx, key, data, t.ttl).Err(); err != nil {
		return fmt.Errorf("failed to update dedup state: %w", err)
	}

	return nil
}

// MarkFailed marks a message as failed.
func (t *DeliveryTracker) MarkFailed(ctx context.Context, smtpMessageID, reason string) error {
	if smtpMessageID == "" {
		return nil
	}

	key := t.dedupKey(smtpMessageID)

	state, err := t.GetState(ctx, smtpMessageID)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			state = &DeliveryState{
				SMTPMessageID: smtpMessageID,
				StartedAt:     time.Now(),
			}
		} else {
			return fmt.Errorf("failed to get dedup state: %w", err)
		}
	}

	state.State = StateFailed
	state.SMTPResponse = reason
	state.CompletedAt = time.Now()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal dedup state: %w", err)
	}

	if err := t.client.Set(ctx, key, data, t.ttl).Err(); err != nil {
		return fmt.Errorf("failed to update dedup state: %w", err)
	}

	return nil
}

// IsAlreadyDelivered checks if a message has been delivered.
func (t *DeliveryTracker) IsAlreadyDelivered(ctx context.Context, smtpMessageID string) (bool, error) {
	if smtpMessageID == "" {
		return false, nil
	}

	state, err := t.GetState(ctx, smtpMessageID)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, err
	}

	return state.State == StateDelivered, nil
}

// GetState retrieves the delivery state for a message.
func (t *DeliveryTracker) GetState(ctx context.Context, smtpMessageID string) (*DeliveryState, error) {
	if smtpMessageID == "" {
		return nil, redis.Nil
	}

	key := t.dedupKey(smtpMessageID)

	data, err := t.client.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	var state DeliveryState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal dedup state: %w", err)
	}

	return &state, nil
}

// CleanupStale removes stale delivery tracking entries.
// This is called periodically to clean up entries that got stuck.
func (t *DeliveryTracker) CleanupStale(ctx context.Context, staleThreshold time.Duration) (int, error) {
	// Note: This is a simplified implementation. In production,
	// you might want to use Redis SCAN to iterate over keys.
	// For now, we rely on TTL-based cleanup.
	return 0, nil
}

// Delete removes a delivery tracking entry.
func (t *DeliveryTracker) Delete(ctx context.Context, smtpMessageID string) error {
	if smtpMessageID == "" {
		return nil
	}
	return t.client.Del(ctx, t.dedupKey(smtpMessageID)).Err()
}
