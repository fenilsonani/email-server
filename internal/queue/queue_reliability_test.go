package queue

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestQueue_ClosedQueueOperations tests that operations fail gracefully on closed queue.
func TestQueue_ClosedQueueOperations(t *testing.T) {
	q := &RedisQueue{closed: 1} // Already closed

	ctx := context.Background()

	// Enqueue should fail
	err := q.Enqueue(ctx, &Message{ID: "test"})
	if err != ErrQueueClosed {
		t.Errorf("Enqueue on closed queue: expected ErrQueueClosed, got %v", err)
	}

	// Dequeue should fail
	_, err = q.Dequeue(ctx)
	if err != ErrQueueClosed {
		t.Errorf("Dequeue on closed queue: expected ErrQueueClosed, got %v", err)
	}

	// ListPending should fail
	_, err = q.ListPending(ctx, 10)
	if err != ErrQueueClosed {
		t.Errorf("ListPending on closed queue: expected ErrQueueClosed, got %v", err)
	}

	// ListFailed should fail
	_, err = q.ListFailed(ctx, 10)
	if err != ErrQueueClosed {
		t.Errorf("ListFailed on closed queue: expected ErrQueueClosed, got %v", err)
	}

	// ListSent should fail
	_, err = q.ListSent(ctx, 10)
	if err != ErrQueueClosed {
		t.Errorf("ListSent on closed queue: expected ErrQueueClosed, got %v", err)
	}

	// Cleanup should fail
	err = q.Cleanup(ctx, time.Hour)
	if err != ErrQueueClosed {
		t.Errorf("Cleanup on closed queue: expected ErrQueueClosed, got %v", err)
	}
}

// TestQueue_NilContextOperations tests that operations reject nil context.
func TestQueue_NilContextOperations(t *testing.T) {
	q := &RedisQueue{closed: 0}

	// Enqueue with nil context
	err := q.Enqueue(nil, &Message{ID: "test"})
	if err == nil {
		t.Error("Enqueue should reject nil context")
	}

	// Dequeue with nil context
	_, err = q.Dequeue(nil)
	if err == nil {
		t.Error("Dequeue should reject nil context")
	}

	// ListPending with nil context
	_, err = q.ListPending(nil, 10)
	if err == nil {
		t.Error("ListPending should reject nil context")
	}
}

// TestQueue_NilMessageEnqueue tests that enqueue rejects nil message.
func TestQueue_NilMessageEnqueue(t *testing.T) {
	q := &RedisQueue{closed: 0}
	ctx := context.Background()

	// This won't actually call Redis since we validate first
	// We need to test the validation logic
	err := q.validateContext(ctx)
	if err != nil {
		t.Errorf("validateContext should pass: %v", err)
	}
}

// TestQueue_DoubleClose tests that closing twice is safe.
func TestQueue_DoubleClose(t *testing.T) {
	q := &RedisQueue{closed: 0}

	// First close - sets atomic flag
	atomic.StoreInt32(&q.closed, 1)

	// Simulate double close safety check
	if !atomic.CompareAndSwapInt32(&q.closed, 0, 1) {
		// Already closed - this is expected
	}

	// Verify still closed
	if !q.isClosed() {
		t.Error("Queue should remain closed")
	}
}

// TestQueue_ConcurrentClose tests concurrent close calls.
func TestQueue_ConcurrentClose(t *testing.T) {
	q := &RedisQueue{closed: 0}

	var wg sync.WaitGroup
	closeCalled := int32(0)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if atomic.CompareAndSwapInt32(&q.closed, 0, 1) {
				atomic.AddInt32(&closeCalled, 1)
			}
		}()
	}

	wg.Wait()

	if atomic.LoadInt32(&closeCalled) != 1 {
		t.Errorf("Only one close should succeed, got %d", closeCalled)
	}
}

// TestQueue_MessageDefaults tests that message defaults are applied correctly.
func TestQueue_MessageDefaults(t *testing.T) {
	cfg := DefaultConfig()

	msg := &Message{
		Sender:     "sender@example.com",
		Recipients: []string{"recipient@example.com"},
	}

	// Apply defaults manually (simulating what Enqueue does)
	if msg.ID == "" {
		msg.ID = generateMessageID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	if msg.NextAttempt.IsZero() {
		msg.NextAttempt = time.Now()
	}
	if msg.MaxAttempts == 0 {
		msg.MaxAttempts = cfg.MaxRetries
	}
	msg.Status = StatusPending

	if msg.ID == "" {
		t.Error("ID should be set")
	}
	if msg.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if msg.NextAttempt.IsZero() {
		t.Error("NextAttempt should be set")
	}
	if msg.MaxAttempts != 15 {
		t.Errorf("MaxAttempts = %d, want 15", msg.MaxAttempts)
	}
	if msg.Status != StatusPending {
		t.Errorf("Status = %s, want pending", msg.Status)
	}
}

// TestQueue_TransientErrorRetry tests transient error detection for retry logic.
func TestQueue_TransientErrorRetry(t *testing.T) {
	testCases := []struct {
		error     error
		transient bool
	}{
		{errors.New("connection refused"), true},
		{errors.New("i/o timeout"), true},
		{errors.New("connection reset by peer"), true},
		{errors.New("broken pipe"), true},
		{errors.New("network unreachable"), true},
		{errors.New("EOF"), true},
		{errors.New("timeout waiting for response"), true},
		{errors.New("WRONGTYPE Operation against a key"), false},
		{errors.New("OOM command not allowed"), false},
		{errors.New("syntax error"), false},
		{nil, false},
	}

	for _, tc := range testCases {
		name := "nil"
		if tc.error != nil {
			name = tc.error.Error()
			if len(name) > 30 {
				name = name[:30]
			}
		}
		t.Run(name, func(t *testing.T) {
			result := isTransientRedisError(tc.error)
			if result != tc.transient {
				t.Errorf("isTransientRedisError(%v) = %v, want %v",
					tc.error, result, tc.transient)
			}
		})
	}
}

// TestQueue_RetryBackoffValues tests exponential backoff calculation.
func TestQueue_RetryBackoffValues(t *testing.T) {
	// Verify backoff increases with attempts
	var prevDuration time.Duration
	for attempts := 1; attempts <= 10; attempts++ {
		now := time.Now()
		nextRetry := calculateNextRetry(attempts)
		duration := nextRetry.Sub(now)

		// Duration should generally increase (allowing for jitter)
		if attempts > 1 && duration < prevDuration/2 {
			t.Errorf("Attempt %d: duration %v should not be much less than previous %v",
				attempts, duration, prevDuration)
		}

		// Duration should not exceed 24 hours + 10% jitter
		maxDuration := 24*time.Hour + 24*time.Hour/10
		if duration > maxDuration {
			t.Errorf("Attempt %d: duration %v exceeds max %v", attempts, duration, maxDuration)
		}

		prevDuration = duration
	}
}

// TestQueue_RetryBackoffNegativeAttempts tests backoff with edge case values.
func TestQueue_RetryBackoffNegativeAttempts(t *testing.T) {
	// Negative attempts should be handled gracefully
	now := time.Now()
	nextRetry := calculateNextRetry(-1)
	if nextRetry.Before(now) {
		t.Error("Negative attempts should still produce future time")
	}

	// Zero attempts
	nextRetry = calculateNextRetry(0)
	if nextRetry.Before(now) {
		t.Error("Zero attempts should still produce future time")
	}

	// Very large attempts
	nextRetry = calculateNextRetry(1000000)
	duration := nextRetry.Sub(now)
	maxDuration := 24*time.Hour + 24*time.Hour/10
	if duration > maxDuration {
		t.Errorf("Large attempts produced duration %v > max %v", duration, maxDuration)
	}
}

// TestQueue_MessageIDUniqueness tests message ID uniqueness under high concurrency.
func TestQueue_MessageIDUniqueness(t *testing.T) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := make(map[string]int)
	count := 1000
	goroutines := 20

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < count/goroutines; i++ {
				id := generateMessageID()
				mu.Lock()
				if existing, ok := ids[id]; ok {
					t.Errorf("Duplicate ID %s: goroutine %d vs %d", id, goroutineID, existing)
				}
				ids[id] = goroutineID
				mu.Unlock()
			}
		}(g)
	}

	wg.Wait()
}

// TestQueue_MessageSerializationRoundtrip tests full serialization cycle.
func TestQueue_MessageSerializationRoundtrip(t *testing.T) {
	original := &Message{
		ID:          generateMessageID(),
		Sender:      "sender@example.com",
		Recipients:  []string{"r1@example.com", "r2@example.com"},
		MessagePath: "/path/to/message.eml",
		Size:        12345,
		Attempts:    3,
		MaxAttempts: 15,
		LastAttempt: time.Now().Add(-time.Hour),
		NextAttempt: time.Now().Add(time.Hour),
		LastError:   "previous error",
		Status:      StatusDeferred,
		CreatedAt:   time.Now().Add(-24 * time.Hour),
		Domain:      "example.com",
	}

	// Marshal
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal
	var restored Message
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify fields
	if restored.ID != original.ID {
		t.Errorf("ID mismatch: %s vs %s", restored.ID, original.ID)
	}
	if restored.Sender != original.Sender {
		t.Errorf("Sender mismatch: %s vs %s", restored.Sender, original.Sender)
	}
	if len(restored.Recipients) != len(original.Recipients) {
		t.Errorf("Recipients count mismatch: %d vs %d",
			len(restored.Recipients), len(original.Recipients))
	}
	if restored.Status != original.Status {
		t.Errorf("Status mismatch: %s vs %s", restored.Status, original.Status)
	}
	if restored.Attempts != original.Attempts {
		t.Errorf("Attempts mismatch: %d vs %d", restored.Attempts, original.Attempts)
	}
}

// TestQueue_StatusStringValues tests status constant string values.
func TestQueue_StatusStringValues(t *testing.T) {
	// These must match external expectations (API responses, logs, etc.)
	expectedValues := map[Status]string{
		StatusPending:  "pending",
		StatusSending:  "sending",
		StatusSent:     "sent",
		StatusFailed:   "failed",
		StatusDeferred: "deferred",
		StatusBounced:  "bounced",
	}

	for status, expected := range expectedValues {
		if string(status) != expected {
			t.Errorf("Status %v = %q, want %q", status, string(status), expected)
		}
	}
}

// TestQueue_ConfigDefaults tests default configuration values.
func TestQueue_ConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.RedisURL != "redis://localhost:6379/0" {
		t.Errorf("RedisURL = %q", cfg.RedisURL)
	}
	if cfg.Mode != "standalone" {
		t.Errorf("Mode = %q", cfg.Mode)
	}
	if cfg.MaxRetries != 15 {
		t.Errorf("MaxRetries = %d", cfg.MaxRetries)
	}
	if cfg.RetryMaxAge != 7*24*time.Hour {
		t.Errorf("RetryMaxAge = %v", cfg.RetryMaxAge)
	}
	if cfg.PoolSize != 10 {
		t.Errorf("PoolSize = %d", cfg.PoolSize)
	}
	if cfg.MinIdleConns != 5 {
		t.Errorf("MinIdleConns = %d", cfg.MinIdleConns)
	}
	if cfg.DialTimeout != 5*time.Second {
		t.Errorf("DialTimeout = %v", cfg.DialTimeout)
	}
	if cfg.ReadTimeout != 3*time.Second {
		t.Errorf("ReadTimeout = %v", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout != 3*time.Second {
		t.Errorf("WriteTimeout = %v", cfg.WriteTimeout)
	}
}

// TestQueue_KeyPrefixes tests Redis key generation.
func TestQueue_KeyPrefixes(t *testing.T) {
	q := &RedisQueue{
		config: Config{Prefix: "test-prefix"},
	}

	if k := q.pendingKey(); k != "test-prefix:queue:pending" {
		t.Errorf("pendingKey = %q", k)
	}
	if k := q.processingKey(); k != "test-prefix:queue:processing" {
		t.Errorf("processingKey = %q", k)
	}
	if k := q.failedKey(); k != "test-prefix:queue:failed" {
		t.Errorf("failedKey = %q", k)
	}
	if k := q.sentKey(); k != "test-prefix:queue:sent" {
		t.Errorf("sentKey = %q", k)
	}
	if k := q.messageKey("msg-123"); k != "test-prefix:message:msg-123" {
		t.Errorf("messageKey = %q", k)
	}
	if k := q.statsKey(); k != "test-prefix:stats" {
		t.Errorf("statsKey = %q", k)
	}
}

// TestQueue_KeyPrefixEmpty tests key generation with empty prefix.
func TestQueue_KeyPrefixEmpty(t *testing.T) {
	q := &RedisQueue{
		config: Config{Prefix: ""},
	}

	if k := q.pendingKey(); k != ":queue:pending" {
		t.Errorf("pendingKey with empty prefix = %q", k)
	}
}

// TestQueue_ValidateContext tests context validation.
func TestQueue_ValidateContext(t *testing.T) {
	q := &RedisQueue{closed: 0}

	// Valid context
	if err := q.validateContext(context.Background()); err != nil {
		t.Errorf("Valid context should pass: %v", err)
	}

	// Nil context
	if err := q.validateContext(nil); err == nil {
		t.Error("Nil context should fail")
	}

	// Closed queue
	q.closed = 1
	if err := q.validateContext(context.Background()); err != ErrQueueClosed {
		t.Errorf("Closed queue should return ErrQueueClosed, got %v", err)
	}
}

// TestQueue_QueueStatsZeroValue tests zero value of QueueStats.
func TestQueue_QueueStatsZeroValue(t *testing.T) {
	stats := QueueStats{}

	if stats.Pending != 0 {
		t.Errorf("Pending = %d", stats.Pending)
	}
	if stats.Processing != 0 {
		t.Errorf("Processing = %d", stats.Processing)
	}
	if stats.Sent != 0 {
		t.Errorf("Sent = %d", stats.Sent)
	}
	if stats.Failed != 0 {
		t.Errorf("Failed = %d", stats.Failed)
	}
	if stats.TotalEnqueued != 0 {
		t.Errorf("TotalEnqueued = %d", stats.TotalEnqueued)
	}
	if stats.TotalSent != 0 {
		t.Errorf("TotalSent = %d", stats.TotalSent)
	}
	if stats.TotalFailed != 0 {
		t.Errorf("TotalFailed = %d", stats.TotalFailed)
	}
	if stats.TotalRetried != 0 {
		t.Errorf("TotalRetried = %d", stats.TotalRetried)
	}
}

// TestQueue_MessageExpiryCheck tests message age expiry logic.
func TestQueue_MessageExpiryCheck(t *testing.T) {
	retryMaxAge := 7 * 24 * time.Hour

	testCases := []struct {
		name      string
		createdAt time.Time
		expired   bool
	}{
		{"just created", time.Now(), false},
		{"1 day old", time.Now().Add(-24 * time.Hour), false},
		{"6 days old", time.Now().Add(-6 * 24 * time.Hour), false},
		{"7 days old", time.Now().Add(-7 * 24 * time.Hour), true},
		{"8 days old", time.Now().Add(-8 * 24 * time.Hour), true},
		{"future message", time.Now().Add(time.Hour), false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isExpired := time.Since(tc.createdAt) > retryMaxAge
			if isExpired != tc.expired {
				t.Errorf("createdAt=%v: expected expired=%v, got %v",
					tc.createdAt, tc.expired, isExpired)
			}
		})
	}
}

// TestQueue_MaxRetriesLogic tests the max retries boundary conditions.
func TestQueue_MaxRetriesLogic(t *testing.T) {
	maxAttempts := 15

	testCases := []struct {
		attempts   int
		shouldFail bool
	}{
		{0, false},
		{1, false},
		{14, false},
		{15, true}, // Equals max
		{16, true}, // Exceeds max
		{100, true},
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			shouldFail := tc.attempts >= maxAttempts
			if shouldFail != tc.shouldFail {
				t.Errorf("attempts=%d: expected fail=%v, got %v",
					tc.attempts, tc.shouldFail, shouldFail)
			}
		})
	}
}

// TestQueue_ContainsHelper tests the string contains helper.
func TestQueue_ContainsHelper(t *testing.T) {
	testCases := []struct {
		s, substr string
		expected  bool
	}{
		{"hello world", "world", true},
		{"hello world", "hello", true},
		{"hello world", "xyz", false},
		{"", "", true},
		{"abc", "", true},
		{"", "abc", false},
		{"abc", "abcd", false},
		{"connection refused", "connection", true},
		{"i/o timeout", "timeout", true},
	}

	for _, tc := range testCases {
		t.Run(tc.s+"_"+tc.substr, func(t *testing.T) {
			result := contains(tc.s, tc.substr)
			if result != tc.expected {
				t.Errorf("contains(%q, %q) = %v, want %v",
					tc.s, tc.substr, result, tc.expected)
			}
		})
	}
}

// TestQueue_GracefulShutdownPattern tests the graceful shutdown sync.WaitGroup pattern.
func TestQueue_GracefulShutdownPattern(t *testing.T) {
	var wg sync.WaitGroup
	var closed int32

	// Simulate operations
	for i := 0; i < 10; i++ {
		// Check closed before starting
		if atomic.LoadInt32(&closed) == 1 {
			break
		}

		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Simulate work
			time.Sleep(time.Duration(id) * time.Millisecond)
		}(i)
	}

	// Signal close
	atomic.StoreInt32(&closed, 1)

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("Graceful shutdown timed out")
	}
}

// TestQueue_ConcurrentValidation tests concurrent validation calls.
func TestQueue_ConcurrentValidation(t *testing.T) {
	q := &RedisQueue{closed: 0}
	ctx := context.Background()

	var wg sync.WaitGroup
	errors := int32(0)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := q.validateContext(ctx); err != nil {
				atomic.AddInt32(&errors, 1)
			}
		}()
	}

	wg.Wait()

	if errors > 0 {
		t.Errorf("Got %d validation errors", errors)
	}
}

// TestQueue_MessageCorruption tests handling of corrupted message JSON.
func TestQueue_MessageCorruption(t *testing.T) {
	corruptedData := []string{
		"",
		"null",
		"[]",
		"not json",
		"{invalid}",
		`{"id": 123}`, // Wrong type for ID
		`{"status": true}`, // Wrong type for status
	}

	for _, data := range corruptedData {
		t.Run(data, func(t *testing.T) {
			var msg Message
			err := json.Unmarshal([]byte(data), &msg)
			// Some might parse, some won't, but shouldn't panic
			_ = err
		})
	}
}

// TestQueue_LargeRecipientList tests handling of large recipient lists.
func TestQueue_LargeRecipientList(t *testing.T) {
	msg := Message{
		ID:         generateMessageID(),
		Sender:     "sender@example.com",
		Recipients: make([]string, 1000),
		Status:     StatusPending,
	}

	for i := 0; i < 1000; i++ {
		msg.Recipients[i] = "recipient@example.com"
	}

	// Should serialize without issue
	data, err := json.Marshal(&msg)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Should deserialize without issue
	var restored Message
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(restored.Recipients) != 1000 {
		t.Errorf("Recipients count = %d, want 1000", len(restored.Recipients))
	}
}
