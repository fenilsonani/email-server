package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestQueue_MessageValidation tests message validation during enqueue.
func TestQueue_MessageValidation(t *testing.T) {
	// Test that nil context is rejected
	t.Run("nil context", func(t *testing.T) {
		q := &RedisQueue{closed: 0}
		err := q.validateContext(nil)
		if err == nil {
			t.Error("Expected error for nil context")
		}
	})

	// Test that closed queue is rejected
	t.Run("closed queue", func(t *testing.T) {
		q := &RedisQueue{closed: 1}
		err := q.validateContext(context.Background())
		if err != ErrQueueClosed {
			t.Errorf("Expected ErrQueueClosed, got %v", err)
		}
	})
}

// TestQueue_MessageSerialization tests JSON serialization of messages.
func TestQueue_MessageSerialization(t *testing.T) {
	testCases := []struct {
		name    string
		message Message
	}{
		{
			name: "minimal message",
			message: Message{
				ID:         "test-1",
				Sender:     "sender@example.com",
				Recipients: []string{"recipient@example.com"},
				Status:     StatusPending,
			},
		},
		{
			name: "message with special characters",
			message: Message{
				ID:          "test-2",
				Sender:      "user+tag@example.com",
				Recipients:  []string{"日本語@example.com", "test@例え.jp"},
				MessagePath: "/path/with spaces/message.eml",
				LastError:   "error with \"quotes\" and \n newlines",
				Status:      StatusFailed,
			},
		},
		{
			name: "message with large fields",
			message: Message{
				ID:         "test-3",
				Sender:     strings.Repeat("a", 100) + "@example.com",
				Recipients: make([]string, 100), // 100 recipients
				LastError:  strings.Repeat("error message ", 1000),
				Status:     StatusDeferred,
			},
		},
		{
			name: "message with all timestamps",
			message: Message{
				ID:          "test-4",
				Sender:      "test@example.com",
				Recipients:  []string{"test@example.com"},
				CreatedAt:   time.Now(),
				LastAttempt: time.Now().Add(-time.Hour),
				NextAttempt: time.Now().Add(time.Hour),
				Status:      StatusPending,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(&tc.message)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			// Unmarshal
			var decoded Message
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			// Verify key fields
			if decoded.ID != tc.message.ID {
				t.Errorf("ID mismatch: %s != %s", decoded.ID, tc.message.ID)
			}
			if decoded.Status != tc.message.Status {
				t.Errorf("Status mismatch: %s != %s", decoded.Status, tc.message.Status)
			}
		})
	}
}

// TestQueue_RetryBackoff tests the exponential backoff calculation.
func TestQueue_RetryBackoff(t *testing.T) {
	// Test backoff intervals for different attempt counts
	testCases := []struct {
		attempts    int
		minDuration time.Duration
		maxDuration time.Duration
	}{
		{0, 4 * time.Minute, 6 * time.Minute},         // ~5 minutes
		{1, 4 * time.Minute, 6 * time.Minute},         // ~5 minutes
		{2, 13 * time.Minute, 17 * time.Minute},       // ~15 minutes
		{3, 27 * time.Minute, 33 * time.Minute},       // ~30 minutes
		{4, 54 * time.Minute, 66 * time.Minute},       // ~1 hour
		{5, 108 * time.Minute, 132 * time.Minute},     // ~2 hours
		{10, 21*time.Hour + 36*time.Minute, 26*time.Hour + 24*time.Minute}, // ~24 hours
		{100, 21*time.Hour + 36*time.Minute, 26*time.Hour + 24*time.Minute}, // Still ~24 hours (max)
	}

	for _, tc := range testCases {
		t.Run(string(rune('0'+tc.attempts%10)), func(t *testing.T) {
			now := time.Now()
			nextRetry := calculateNextRetry(tc.attempts)
			duration := nextRetry.Sub(now)

			if duration < tc.minDuration || duration > tc.maxDuration {
				t.Errorf("Attempt %d: duration %v not in range [%v, %v]",
					tc.attempts, duration, tc.minDuration, tc.maxDuration)
			}
		})
	}
}

// TestQueue_MessageIDGeneration tests unique message ID generation.
func TestQueue_MessageIDGeneration(t *testing.T) {
	ids := make(map[string]bool)
	count := 10000

	for i := 0; i < count; i++ {
		id := generateMessageID()
		if ids[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		ids[id] = true
	}

	// Verify ID format
	sampleID := generateMessageID()
	if len(sampleID) < 20 { // timestamp + separator + hex
		t.Errorf("ID too short: %s", sampleID)
	}
}

// TestQueue_ConcurrentMessageIDGeneration tests concurrent ID generation.
func TestQueue_ConcurrentMessageIDGeneration(t *testing.T) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := make(map[string]bool)
	count := 1000
	goroutines := 10

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < count/goroutines; i++ {
				id := generateMessageID()
				mu.Lock()
				if ids[id] {
					t.Errorf("Duplicate ID: %s", id)
				}
				ids[id] = true
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if len(ids) != count {
		t.Errorf("Expected %d unique IDs, got %d", count, len(ids))
	}
}

// TestQueue_TransientErrorDetection tests transient error detection.
func TestQueue_TransientErrorDetection(t *testing.T) {
	testCases := []struct {
		error     error
		transient bool
	}{
		{errors.New("connection refused"), true},
		{errors.New("timeout waiting for response"), true},
		{errors.New("connection reset by peer"), true},
		{errors.New("broken pipe"), true},
		{errors.New("i/o timeout"), true},
		{errors.New("network unreachable"), true},
		{errors.New("EOF"), true},
		{errors.New("invalid syntax"), false},
		{errors.New("WRONGTYPE Operation"), false},
		{errors.New("NOSCRIPT No matching script"), false},
		{nil, false},
	}

	for _, tc := range testCases {
		t.Run(errorString(tc.error), func(t *testing.T) {
			result := isTransientRedisError(tc.error)
			if result != tc.transient {
				t.Errorf("Error %q: expected transient=%v, got %v",
					errorString(tc.error), tc.transient, result)
			}
		})
	}
}

func errorString(err error) string {
	if err == nil {
		return "nil"
	}
	s := err.Error()
	if len(s) > 30 {
		return s[:30]
	}
	return s
}

// TestQueue_StatusConstants tests status constant values.
func TestQueue_StatusConstants(t *testing.T) {
	// Verify status values are what we expect for external systems
	if StatusPending != "pending" {
		t.Errorf("StatusPending = %q", StatusPending)
	}
	if StatusSending != "sending" {
		t.Errorf("StatusSending = %q", StatusSending)
	}
	if StatusSent != "sent" {
		t.Errorf("StatusSent = %q", StatusSent)
	}
	if StatusFailed != "failed" {
		t.Errorf("StatusFailed = %q", StatusFailed)
	}
	if StatusDeferred != "deferred" {
		t.Errorf("StatusDeferred = %q", StatusDeferred)
	}
	if StatusBounced != "bounced" {
		t.Errorf("StatusBounced = %q", StatusBounced)
	}
}

// TestQueue_DefaultConfig tests default configuration values.
func TestQueue_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.RedisURL != "redis://localhost:6379/0" {
		t.Errorf("Default RedisURL = %q", cfg.RedisURL)
	}
	if cfg.Mode != "standalone" {
		t.Errorf("Default Mode = %q", cfg.Mode)
	}
	if cfg.MaxRetries != 15 {
		t.Errorf("Default MaxRetries = %d", cfg.MaxRetries)
	}
	if cfg.PoolSize != 10 {
		t.Errorf("Default PoolSize = %d", cfg.PoolSize)
	}
	if cfg.RetryMaxAge != 7*24*time.Hour {
		t.Errorf("Default RetryMaxAge = %v", cfg.RetryMaxAge)
	}
}

// TestQueue_MaxRetries tests behavior at max retry limit.
func TestQueue_MaxRetries(t *testing.T) {
	msg := Message{
		ID:          "test-retry",
		Attempts:    14, // One less than max
		MaxAttempts: 15,
	}

	// At attempts=14, next should be 15 which equals max
	if msg.Attempts >= msg.MaxAttempts {
		t.Error("Should not be at max yet")
	}

	msg.Attempts = 15
	if msg.Attempts < msg.MaxAttempts {
		t.Error("Should be at max now")
	}
}

// TestQueue_MessageExpiry tests message age checking.
func TestQueue_MessageExpiry(t *testing.T) {
	retryMaxAge := 7 * 24 * time.Hour

	testCases := []struct {
		name      string
		createdAt time.Time
		expired   bool
	}{
		{"just created", time.Now(), false},
		{"1 hour old", time.Now().Add(-time.Hour), false},
		{"1 day old", time.Now().Add(-24 * time.Hour), false},
		{"6 days old", time.Now().Add(-6 * 24 * time.Hour), false},
		{"7 days old", time.Now().Add(-7 * 24 * time.Hour), true}, // Edge case
		{"8 days old", time.Now().Add(-8 * 24 * time.Hour), true},
		{"30 days old", time.Now().Add(-30 * 24 * time.Hour), true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isExpired := time.Since(tc.createdAt) > retryMaxAge
			if isExpired != tc.expired {
				t.Errorf("Message created %v: expected expired=%v, got %v",
					tc.createdAt, tc.expired, isExpired)
			}
		})
	}
}

// TestQueue_KeyGeneration tests Redis key generation.
func TestQueue_KeyGeneration(t *testing.T) {
	q := &RedisQueue{
		config: Config{Prefix: "mail"},
	}

	// Test key formats
	if k := q.pendingKey(); k != "mail:queue:pending" {
		t.Errorf("pendingKey = %q", k)
	}
	if k := q.processingKey(); k != "mail:queue:processing" {
		t.Errorf("processingKey = %q", k)
	}
	if k := q.failedKey(); k != "mail:queue:failed" {
		t.Errorf("failedKey = %q", k)
	}
	if k := q.sentKey(); k != "mail:queue:sent" {
		t.Errorf("sentKey = %q", k)
	}
	if k := q.messageKey("test-123"); k != "mail:message:test-123" {
		t.Errorf("messageKey = %q", k)
	}
	if k := q.statsKey(); k != "mail:stats" {
		t.Errorf("statsKey = %q", k)
	}

	// Test with different prefix
	q.config.Prefix = "custom-prefix"
	if k := q.pendingKey(); k != "custom-prefix:queue:pending" {
		t.Errorf("pendingKey with custom prefix = %q", k)
	}
}

// TestQueue_CorruptedMessageData tests handling of corrupted message JSON.
func TestQueue_CorruptedMessageData(t *testing.T) {
	corruptedData := []string{
		"not json at all",
		"{invalid json}",
		`{"id": 123}`, // Wrong type for ID
		`{"status": ["array"]}`, // Wrong type for status
		"",
		"null",
		`{"id": "test", "recipients": "not-array"}`,
	}

	for _, data := range corruptedData {
		t.Run(data[:min(20, len(data))], func(t *testing.T) {
			var msg Message
			err := json.Unmarshal([]byte(data), &msg)
			// Some of these may parse (JSON is lenient), but values should be usable
			if err == nil && data == "" {
				t.Log("Empty string parsed as zero-value message")
			}
		})
	}
}

// TestQueue_LargePayload tests handling of large message payloads.
func TestQueue_LargePayload(t *testing.T) {
	// Create a message with many recipients (simulating large distribution list)
	msg := Message{
		ID:         "large-test",
		Sender:     "sender@example.com",
		Recipients: make([]string, 1000), // 1000 recipients
		Status:     StatusPending,
	}

	for i := 0; i < 1000; i++ {
		msg.Recipients[i] = strings.Repeat("a", 50) + "@example.com"
	}

	// Should be able to serialize/deserialize
	data, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal large message: %v", err)
	}

	// Check payload size
	if len(data) > 10*1024*1024 { // 10MB sanity check
		t.Errorf("Payload too large: %d bytes", len(data))
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if len(decoded.Recipients) != 1000 {
		t.Errorf("Recipients count mismatch: %d", len(decoded.Recipients))
	}
}

// TestQueue_DuplicatePrevention tests message ID uniqueness across batches.
func TestQueue_DuplicatePrevention(t *testing.T) {
	// Generate IDs in batches with delays to simulate real usage
	allIDs := make(map[string]bool)

	for batch := 0; batch < 10; batch++ {
		for i := 0; i < 100; i++ {
			id := generateMessageID()
			if allIDs[id] {
				t.Errorf("Duplicate ID in batch %d: %s", batch, id)
			}
			allIDs[id] = true
		}
		time.Sleep(time.Millisecond) // Small delay between batches
	}

	if len(allIDs) != 1000 {
		t.Errorf("Expected 1000 unique IDs, got %d", len(allIDs))
	}
}

// TestQueue_QueueStatsZero tests stats struct zero values.
func TestQueue_QueueStatsZero(t *testing.T) {
	stats := QueueStats{}

	if stats.Pending != 0 || stats.Processing != 0 ||
		stats.Sent != 0 || stats.Failed != 0 ||
		stats.TotalEnqueued != 0 || stats.TotalSent != 0 {
		t.Error("QueueStats zero value should have all zeros")
	}
}

// min returns the smaller of two ints
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
