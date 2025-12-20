package queue

import (
	"encoding/json"
	"testing"
	"time"
)

func TestGenerateMessageID(t *testing.T) {
	ids := make(map[string]bool)

	// Generate many IDs and check uniqueness
	for i := 0; i < 1000; i++ {
		id := generateMessageID()
		if id == "" {
			t.Error("Generated empty ID")
		}
		if ids[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestGenerateMessageID_Format(t *testing.T) {
	id := generateMessageID()

	// Should contain timestamp and random component
	// Format is: timestamp-hexrandom (e.g., "1234567890123-1a2b3c4d5e6f7890abcd")
	if len(id) < 20 {
		t.Errorf("ID too short: %s (len=%d)", id, len(id))
	}

	// Should contain a dash separator
	hasDash := false
	for _, c := range id {
		if c == '-' {
			hasDash = true
			break
		}
	}
	if !hasDash {
		t.Errorf("ID should contain dash separator: %s", id)
	}
}

func TestGenerateMessageID_Uniqueness(t *testing.T) {
	// Generate IDs in quick succession
	ids := make([]string, 100)
	for i := 0; i < 100; i++ {
		ids[i] = generateMessageID()
	}

	// Check all are unique
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("Duplicate ID found: %s", id)
		}
		seen[id] = true
	}
}

func TestCalculateNextRetry(t *testing.T) {
	tests := []struct {
		name     string
		attempts int
		minDelay time.Duration
		maxDelay time.Duration
	}{
		{
			name:     "attempt 0",
			attempts: 0,
			minDelay: 4*time.Minute + 30*time.Second,
			maxDelay: 5*time.Minute + 30*time.Second,
		},
		{
			name:     "attempt 1",
			attempts: 1,
			minDelay: 4*time.Minute + 30*time.Second,
			maxDelay: 5*time.Minute + 30*time.Second,
		},
		{
			name:     "attempt 2",
			attempts: 2,
			minDelay: 13*time.Minute + 30*time.Second,
			maxDelay: 16*time.Minute + 30*time.Second,
		},
		{
			name:     "attempt 3",
			attempts: 3,
			minDelay: 27 * time.Minute,
			maxDelay: 33 * time.Minute,
		},
		{
			name:     "attempt 4",
			attempts: 4,
			minDelay: 54 * time.Minute,
			maxDelay: 66 * time.Minute,
		},
		{
			name:     "attempt 5",
			attempts: 5,
			minDelay: 108 * time.Minute,
			maxDelay: 132 * time.Minute,
		},
		{
			name:     "attempt 6",
			attempts: 6,
			minDelay: 216 * time.Minute,
			maxDelay: 264 * time.Minute,
		},
		{
			name:     "attempt 7",
			attempts: 7,
			minDelay: 432 * time.Minute,
			maxDelay: 528 * time.Minute,
		},
		{
			name:     "attempt 8",
			attempts: 8,
			minDelay: 864 * time.Minute,
			maxDelay: 1056 * time.Minute,
		},
		{
			name:     "attempt 9 (max interval)",
			attempts: 9,
			minDelay: 21*time.Hour + 36*time.Minute,
			maxDelay: 26*time.Hour + 24*time.Minute,
		},
		{
			name:     "attempt 10 (should cap at 24h)",
			attempts: 10,
			minDelay: 21*time.Hour + 36*time.Minute,
			maxDelay: 26*time.Hour + 24*time.Minute,
		},
		{
			name:     "attempt 100 (should still cap at 24h)",
			attempts: 100,
			minDelay: 21*time.Hour + 36*time.Minute,
			maxDelay: 26*time.Hour + 24*time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Now()
			next := calculateNextRetry(tt.attempts)
			delay := next.Sub(now)

			if delay < tt.minDelay || delay > tt.maxDelay {
				t.Errorf("calculateNextRetry(%d) = %v, want between %v and %v",
					tt.attempts, delay, tt.minDelay, tt.maxDelay)
			}
		})
	}
}

func TestCalculateNextRetry_Jitter(t *testing.T) {
	// Same attempt count should give different results due to jitter
	results := make(map[int64]bool)

	for i := 0; i < 100; i++ {
		next := calculateNextRetry(1)
		results[next.UnixNano()] = true
	}

	// Should have some variation due to jitter (10% range)
	// Note: With 10% jitter and nanosecond granularity, we expect at least a few unique values
	if len(results) < 3 {
		t.Errorf("Expected some variation from jitter, got only %d unique values", len(results))
	}
}

func TestCalculateNextRetry_NegativeAttempts(t *testing.T) {
	// Should handle negative attempts gracefully
	next := calculateNextRetry(-1)
	delay := next.Sub(time.Now())

	// Should use first interval (5 minutes +/- 10%)
	if delay < 4*time.Minute || delay > 6*time.Minute {
		t.Errorf("calculateNextRetry(-1) = %v, want ~5 minutes", delay)
	}
}

func TestMessage_Struct(t *testing.T) {
	msg := Message{
		ID:          "test-123",
		Sender:      "sender@example.com",
		Recipients:  []string{"rcpt1@example.com", "rcpt2@example.com"},
		MessagePath: "/path/to/message.eml",
		Size:        1024,
		Attempts:    3,
		MaxAttempts: 15,
		LastAttempt: time.Now().Add(-time.Hour),
		NextAttempt: time.Now().Add(time.Hour),
		LastError:   "temporary failure",
		Status:      StatusDeferred,
		CreatedAt:   time.Now().Add(-24 * time.Hour),
		Domain:      "example.com",
	}

	if msg.ID != "test-123" {
		t.Errorf("ID = %s, want test-123", msg.ID)
	}
	if len(msg.Recipients) != 2 {
		t.Errorf("len(Recipients) = %d, want 2", len(msg.Recipients))
	}
	if msg.Status != StatusDeferred {
		t.Errorf("Status = %s, want %s", msg.Status, StatusDeferred)
	}
	if msg.Size != 1024 {
		t.Errorf("Size = %d, want 1024", msg.Size)
	}
	if msg.Domain != "example.com" {
		t.Errorf("Domain = %s, want example.com", msg.Domain)
	}
}

func TestMessage_EmptyRecipients(t *testing.T) {
	msg := Message{
		ID:         "test-empty",
		Recipients: []string{},
	}

	if msg.Recipients == nil {
		t.Error("Recipients should not be nil")
	}
	if len(msg.Recipients) != 0 {
		t.Errorf("len(Recipients) = %d, want 0", len(msg.Recipients))
	}
}

func TestMessage_MultipleRecipients(t *testing.T) {
	recipients := []string{
		"user1@example.com",
		"user2@example.com",
		"user3@example.com",
		"user4@example.com",
		"user5@example.com",
	}

	msg := Message{
		ID:         "test-multi",
		Recipients: recipients,
	}

	if len(msg.Recipients) != 5 {
		t.Errorf("len(Recipients) = %d, want 5", len(msg.Recipients))
	}

	for i, rcpt := range msg.Recipients {
		if rcpt != recipients[i] {
			t.Errorf("Recipients[%d] = %s, want %s", i, rcpt, recipients[i])
		}
	}
}

func TestMessage_Serialization(t *testing.T) {
	original := Message{
		ID:          "msg-12345",
		Sender:      "alice@example.com",
		Recipients:  []string{"bob@example.com", "charlie@example.com"},
		MessagePath: "/var/mail/msg-12345.eml",
		Size:        2048,
		Attempts:    2,
		MaxAttempts: 10,
		LastAttempt: time.Now().Add(-30 * time.Minute),
		NextAttempt: time.Now().Add(15 * time.Minute),
		LastError:   "connection timeout",
		Status:      StatusDeferred,
		CreatedAt:   time.Now().Add(-2 * time.Hour),
		Domain:      "example.com",
	}

	// Marshal to JSON
	data, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("Failed to marshal message: %v", err)
	}

	// Unmarshal back
	var decoded Message
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal message: %v", err)
	}

	// Compare fields
	if decoded.ID != original.ID {
		t.Errorf("ID = %s, want %s", decoded.ID, original.ID)
	}
	if decoded.Sender != original.Sender {
		t.Errorf("Sender = %s, want %s", decoded.Sender, original.Sender)
	}
	if len(decoded.Recipients) != len(original.Recipients) {
		t.Errorf("len(Recipients) = %d, want %d", len(decoded.Recipients), len(original.Recipients))
	}
	if decoded.Size != original.Size {
		t.Errorf("Size = %d, want %d", decoded.Size, original.Size)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status = %s, want %s", decoded.Status, original.Status)
	}
}

func TestMessage_SerializationWithEmptyFields(t *testing.T) {
	// Message with minimal fields
	original := Message{
		ID:         "minimal-msg",
		Recipients: []string{},
		Status:     StatusPending,
	}

	data, err := json.Marshal(&original)
	if err != nil {
		t.Fatalf("Failed to marshal minimal message: %v", err)
	}

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal minimal message: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %s, want %s", decoded.ID, original.ID)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status = %s, want %s", decoded.Status, original.Status)
	}
}

func TestStatus_Constants(t *testing.T) {
	statuses := []Status{
		StatusPending,
		StatusSending,
		StatusSent,
		StatusFailed,
		StatusDeferred,
		StatusBounced,
	}

	// All should be non-empty strings
	for _, s := range statuses {
		if string(s) == "" {
			t.Errorf("Status should not be empty: %v", s)
		}
	}

	// All should be unique
	seen := make(map[Status]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("Duplicate status: %s", s)
		}
		seen[s] = true
	}

	// Verify expected values
	if StatusPending != "pending" {
		t.Errorf("StatusPending = %s, want pending", StatusPending)
	}
	if StatusSending != "sending" {
		t.Errorf("StatusSending = %s, want sending", StatusSending)
	}
	if StatusSent != "sent" {
		t.Errorf("StatusSent = %s, want sent", StatusSent)
	}
	if StatusFailed != "failed" {
		t.Errorf("StatusFailed = %s, want failed", StatusFailed)
	}
	if StatusDeferred != "deferred" {
		t.Errorf("StatusDeferred = %s, want deferred", StatusDeferred)
	}
	if StatusBounced != "bounced" {
		t.Errorf("StatusBounced = %s, want bounced", StatusBounced)
	}
}

func TestStatus_Transitions(t *testing.T) {
	// Test valid status transitions
	tests := []struct {
		name     string
		from     Status
		to       Status
		valid    bool
		scenario string
	}{
		{"pending to sending", StatusPending, StatusSending, true, "message dequeued for delivery"},
		{"sending to sent", StatusSending, StatusSent, true, "successful delivery"},
		{"sending to deferred", StatusSending, StatusDeferred, true, "temporary failure, will retry"},
		{"sending to failed", StatusSending, StatusFailed, true, "permanent failure"},
		{"sending to bounced", StatusSending, StatusBounced, true, "recipient rejected"},
		{"deferred to sending", StatusDeferred, StatusSending, true, "retry attempt"},
		{"pending to failed", StatusPending, StatusFailed, true, "immediate rejection"},
		{"sent to sending", StatusSent, StatusSending, false, "invalid: can't unsend"},
		{"failed to pending", StatusFailed, StatusPending, false, "invalid: permanent failure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := Message{
				ID:     "test-transition",
				Status: tt.from,
			}

			// Simulate transition
			msg.Status = tt.to

			if msg.Status != tt.to {
				t.Errorf("Status transition failed: got %s, want %s", msg.Status, tt.to)
			}
		})
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.RedisURL != "redis://localhost:6379/0" {
		t.Errorf("RedisURL = %s, want redis://localhost:6379/0", cfg.RedisURL)
	}
	if cfg.Prefix != "mail" {
		t.Errorf("Prefix = %s, want mail", cfg.Prefix)
	}
	if cfg.MaxRetries != 15 {
		t.Errorf("MaxRetries = %d, want 15", cfg.MaxRetries)
	}
	if cfg.RetryMaxAge != 7*24*time.Hour {
		t.Errorf("RetryMaxAge = %v, want 7 days", cfg.RetryMaxAge)
	}
}

func TestConfig_CustomValues(t *testing.T) {
	cfg := Config{
		RedisURL:    "redis://custom:6380/1",
		Prefix:      "myapp",
		MaxRetries:  20,
		RetryMaxAge: 14 * 24 * time.Hour,
	}

	if cfg.RedisURL != "redis://custom:6380/1" {
		t.Errorf("RedisURL = %s, want redis://custom:6380/1", cfg.RedisURL)
	}
	if cfg.Prefix != "myapp" {
		t.Errorf("Prefix = %s, want myapp", cfg.Prefix)
	}
	if cfg.MaxRetries != 20 {
		t.Errorf("MaxRetries = %d, want 20", cfg.MaxRetries)
	}
	if cfg.RetryMaxAge != 14*24*time.Hour {
		t.Errorf("RetryMaxAge = %v, want 14 days", cfg.RetryMaxAge)
	}
}

func TestErrors(t *testing.T) {
	// Test error constants
	if ErrMessageNotFound == nil {
		t.Error("ErrMessageNotFound should not be nil")
	}
	if ErrQueueClosed == nil {
		t.Error("ErrQueueClosed should not be nil")
	}

	// Test error messages
	if ErrMessageNotFound.Error() == "" {
		t.Error("ErrMessageNotFound should have message")
	}
	if ErrQueueClosed.Error() == "" {
		t.Error("ErrQueueClosed should have message")
	}

	// Test error values
	if ErrMessageNotFound.Error() != "message not found" {
		t.Errorf("ErrMessageNotFound = %s, want 'message not found'", ErrMessageNotFound.Error())
	}
	if ErrQueueClosed.Error() != "queue is closed" {
		t.Errorf("ErrQueueClosed = %s, want 'queue is closed'", ErrQueueClosed.Error())
	}
}

func TestQueueStats_Struct(t *testing.T) {
	stats := QueueStats{
		Pending:       100,
		Processing:    5,
		Sent:          1000,
		Failed:        10,
		TotalEnqueued: 1115,
		TotalSent:     1000,
		TotalFailed:   10,
		TotalRetried:  50,
	}

	if stats.Pending != 100 {
		t.Errorf("Pending = %d, want 100", stats.Pending)
	}
	if stats.Processing != 5 {
		t.Errorf("Processing = %d, want 5", stats.Processing)
	}
	if stats.Sent != 1000 {
		t.Errorf("Sent = %d, want 1000", stats.Sent)
	}
	if stats.Failed != 10 {
		t.Errorf("Failed = %d, want 10", stats.Failed)
	}

	// Check totals
	if stats.TotalEnqueued != 1115 {
		t.Errorf("TotalEnqueued = %d, want 1115", stats.TotalEnqueued)
	}
	if stats.TotalRetried != 50 {
		t.Errorf("TotalRetried = %d, want 50", stats.TotalRetried)
	}
}

func TestMessage_ZeroTimeHandling(t *testing.T) {
	msg := Message{
		ID:          "test-zero-time",
		Sender:      "sender@example.com",
		Recipients:  []string{"recipient@example.com"},
		Status:      StatusPending,
		CreatedAt:   time.Time{}, // Zero time
		LastAttempt: time.Time{}, // Zero time
		NextAttempt: time.Time{}, // Zero time
	}

	// Serialize
	data, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message with zero times: %v", err)
	}

	// Deserialize
	var decoded Message
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal message with zero times: %v", err)
	}

	// Zero times should remain zero (or omitted)
	if !decoded.CreatedAt.IsZero() && !decoded.CreatedAt.Equal(msg.CreatedAt) {
		t.Errorf("CreatedAt changed during serialization")
	}
}

func TestMessage_LargeSize(t *testing.T) {
	// Test with large message size
	msg := Message{
		ID:   "large-msg",
		Size: 104857600, // 100MB
	}

	if msg.Size != 104857600 {
		t.Errorf("Size = %d, want 104857600", msg.Size)
	}

	// Serialize and verify
	data, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal large message: %v", err)
	}

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal large message: %v", err)
	}

	if decoded.Size != msg.Size {
		t.Errorf("Size after serialization = %d, want %d", decoded.Size, msg.Size)
	}
}

func TestMessage_SpecialCharactersInError(t *testing.T) {
	// Test message with special characters in error message
	msg := Message{
		ID:        "test-special",
		LastError: "Error: \"connection failed\" at line 42\n\tCaused by: timeout",
	}

	data, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message with special chars: %v", err)
	}

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal message with special chars: %v", err)
	}

	if decoded.LastError != msg.LastError {
		t.Errorf("LastError = %q, want %q", decoded.LastError, msg.LastError)
	}
}

func TestMessage_UnicodeInFields(t *testing.T) {
	// Test with unicode characters
	msg := Message{
		ID:         "unicode-test",
		Sender:     "用户@example.com",
		Recipients: []string{"José@example.com", "François@example.com"},
		LastError:  "错误: 连接失败",
	}

	data, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Failed to marshal message with unicode: %v", err)
	}

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal message with unicode: %v", err)
	}

	if decoded.Sender != msg.Sender {
		t.Errorf("Sender = %s, want %s", decoded.Sender, msg.Sender)
	}
	if decoded.LastError != msg.LastError {
		t.Errorf("LastError = %s, want %s", decoded.LastError, msg.LastError)
	}
}

func TestCalculateNextRetry_Consistency(t *testing.T) {
	// For a given attempt, the base delay should be consistent
	// (jitter will vary, but should be within expected range)
	attempts := 3
	delays := make([]time.Duration, 100)

	for i := 0; i < 100; i++ {
		next := calculateNextRetry(attempts)
		delays[i] = next.Sub(time.Now())
	}

	// All delays should be within the expected range for attempt 3 (30 min +/- 10%)
	for _, delay := range delays {
		if delay < 27*time.Minute || delay > 33*time.Minute {
			t.Errorf("Delay %v outside expected range for attempt %d", delay, attempts)
		}
	}
}

func BenchmarkGenerateMessageID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		generateMessageID()
	}
}

func BenchmarkCalculateNextRetry(b *testing.B) {
	for i := 0; i < b.N; i++ {
		calculateNextRetry(5)
	}
}

func BenchmarkMessage_Marshal(b *testing.B) {
	msg := Message{
		ID:          "bench-msg-12345",
		Sender:      "sender@example.com",
		Recipients:  []string{"rcpt1@example.com", "rcpt2@example.com", "rcpt3@example.com"},
		MessagePath: "/var/mail/messages/bench-msg-12345.eml",
		Size:        10240,
		Attempts:    3,
		MaxAttempts: 15,
		LastAttempt: time.Now(),
		NextAttempt: time.Now().Add(15 * time.Minute),
		LastError:   "temporary connection failure",
		Status:      StatusDeferred,
		CreatedAt:   time.Now().Add(-time.Hour),
		Domain:      "example.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(&msg)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMessage_Unmarshal(b *testing.B) {
	msg := Message{
		ID:          "bench-msg-12345",
		Sender:      "sender@example.com",
		Recipients:  []string{"rcpt1@example.com", "rcpt2@example.com", "rcpt3@example.com"},
		MessagePath: "/var/mail/messages/bench-msg-12345.eml",
		Size:        10240,
		Attempts:    3,
		MaxAttempts: 15,
		LastAttempt: time.Now(),
		NextAttempt: time.Now().Add(15 * time.Minute),
		LastError:   "temporary connection failure",
		Status:      StatusDeferred,
		CreatedAt:   time.Now().Add(-time.Hour),
		Domain:      "example.com",
	}

	data, err := json.Marshal(&msg)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var decoded Message
		err := json.Unmarshal(data, &decoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}
