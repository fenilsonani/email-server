package queue

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestPriorityQueue_EnqueueWithPriority(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	cfg := Config{
		RedisURL:   "redis://" + mr.Addr(),
		Prefix:     "test",
		MaxRetries: 3,
	}

	q, err := NewRedisQueue(cfg)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	ctx := context.Background()

	// Enqueue messages with different priorities
	msgHigh := &Message{
		ID:       "msg-high",
		Sender:   "sender@example.com",
		Priority: PriorityHigh,
	}
	msgNormal := &Message{
		ID:       "msg-normal",
		Sender:   "sender@example.com",
		Priority: PriorityNormal,
	}
	msgLow := &Message{
		ID:       "msg-low",
		Sender:   "sender@example.com",
		Priority: PriorityLow,
	}

	// Enqueue in reverse priority order
	if err := q.Enqueue(ctx, msgLow); err != nil {
		t.Fatalf("Failed to enqueue low priority: %v", err)
	}
	if err := q.Enqueue(ctx, msgNormal); err != nil {
		t.Fatalf("Failed to enqueue normal priority: %v", err)
	}
	if err := q.Enqueue(ctx, msgHigh); err != nil {
		t.Fatalf("Failed to enqueue high priority: %v", err)
	}

	// Dequeue should return high priority first
	msg, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}
	if msg.ID != "msg-high" {
		t.Errorf("Expected high priority message first, got %s", msg.ID)
	}

	// Complete the message to allow next dequeue
	q.Complete(ctx, msg.ID)

	// Then normal
	msg, err = q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}
	if msg.ID != "msg-normal" {
		t.Errorf("Expected normal priority message second, got %s", msg.ID)
	}

	q.Complete(ctx, msg.ID)

	// Then low
	msg, err = q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}
	if msg.ID != "msg-low" {
		t.Errorf("Expected low priority message third, got %s", msg.ID)
	}
}

func TestPriorityQueue_DefaultPriority(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	cfg := Config{
		RedisURL:   "redis://" + mr.Addr(),
		Prefix:     "test",
		MaxRetries: 3,
	}

	q, err := NewRedisQueue(cfg)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	ctx := context.Background()

	// Enqueue without priority (should default to normal)
	msg := &Message{
		ID:     "msg-no-priority",
		Sender: "sender@example.com",
	}

	if err := q.Enqueue(ctx, msg); err != nil {
		t.Fatalf("Failed to enqueue: %v", err)
	}

	// Dequeue
	dequeued, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}
	if dequeued.Priority != PriorityNormal {
		t.Errorf("Expected normal priority, got %s", dequeued.Priority)
	}
}

func TestPriorityQueue_Stats(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	cfg := Config{
		RedisURL:   "redis://" + mr.Addr(),
		Prefix:     "test",
		MaxRetries: 3,
	}

	q, err := NewRedisQueue(cfg)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	ctx := context.Background()

	// Enqueue messages with different priorities
	for i := 0; i < 3; i++ {
		q.Enqueue(ctx, &Message{ID: "high-" + string(rune('0'+i)), Priority: PriorityHigh})
	}
	for i := 0; i < 5; i++ {
		q.Enqueue(ctx, &Message{ID: "normal-" + string(rune('0'+i)), Priority: PriorityNormal})
	}
	for i := 0; i < 2; i++ {
		q.Enqueue(ctx, &Message{ID: "low-" + string(rune('0'+i)), Priority: PriorityLow})
	}

	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.PendingHigh != 3 {
		t.Errorf("PendingHigh = %d, want 3", stats.PendingHigh)
	}
	if stats.PendingNormal != 5 {
		t.Errorf("PendingNormal = %d, want 5", stats.PendingNormal)
	}
	if stats.PendingLow != 2 {
		t.Errorf("PendingLow = %d, want 2", stats.PendingLow)
	}
	if stats.Pending != 10 {
		t.Errorf("Total Pending = %d, want 10", stats.Pending)
	}
}

func TestPriorityQueue_PendingCount(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	cfg := Config{
		RedisURL:   "redis://" + mr.Addr(),
		Prefix:     "test",
		MaxRetries: 3,
	}

	q, err := NewRedisQueue(cfg)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	ctx := context.Background()

	// Enqueue messages with different priorities
	q.Enqueue(ctx, &Message{ID: "high-1", Priority: PriorityHigh})
	q.Enqueue(ctx, &Message{ID: "normal-1", Priority: PriorityNormal})
	q.Enqueue(ctx, &Message{ID: "low-1", Priority: PriorityLow})

	count, err := q.PendingCount(ctx)
	if err != nil {
		t.Fatalf("PendingCount failed: %v", err)
	}
	if count != 3 {
		t.Errorf("PendingCount = %d, want 3", count)
	}
}

func TestPriorityQueue_HighPriorityBeforeNormalByTime(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	cfg := Config{
		RedisURL:   "redis://" + mr.Addr(),
		Prefix:     "test",
		MaxRetries: 3,
	}

	q, err := NewRedisQueue(cfg)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	ctx := context.Background()

	// Enqueue normal priority first (earlier timestamp)
	normalMsg := &Message{
		ID:          "msg-normal",
		Sender:      "sender@example.com",
		Priority:    PriorityNormal,
		NextAttempt: time.Now().Add(-time.Hour), // 1 hour ago
	}
	if err := q.Enqueue(ctx, normalMsg); err != nil {
		t.Fatalf("Failed to enqueue normal: %v", err)
	}

	// Enqueue high priority later (but high priority should still win)
	highMsg := &Message{
		ID:          "msg-high",
		Sender:      "sender@example.com",
		Priority:    PriorityHigh,
		NextAttempt: time.Now(), // now
	}
	if err := q.Enqueue(ctx, highMsg); err != nil {
		t.Fatalf("Failed to enqueue high: %v", err)
	}

	// High priority should be dequeued first even though normal was added earlier
	msg, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}
	if msg.ID != "msg-high" {
		t.Errorf("Expected high priority message first (regardless of time), got %s", msg.ID)
	}
}

func TestPriorityKeys(t *testing.T) {
	cfg := Config{
		Prefix: "test",
	}

	// Create a minimal queue just to test key generation
	q := &RedisQueue{
		config:                 cfg,
		cachedPendingKey:       cfg.Prefix + ":queue:pending",
		cachedPendingKeyHigh:   cfg.Prefix + ":queue:pending:high",
		cachedPendingKeyNormal: cfg.Prefix + ":queue:pending:normal",
		cachedPendingKeyLow:    cfg.Prefix + ":queue:pending:low",
	}

	if key := q.pendingKeyForPriority(PriorityHigh); key != "test:queue:pending:high" {
		t.Errorf("High priority key = %s, want test:queue:pending:high", key)
	}
	if key := q.pendingKeyForPriority(PriorityNormal); key != "test:queue:pending:normal" {
		t.Errorf("Normal priority key = %s, want test:queue:pending:normal", key)
	}
	if key := q.pendingKeyForPriority(PriorityLow); key != "test:queue:pending:low" {
		t.Errorf("Low priority key = %s, want test:queue:pending:low", key)
	}
	// Empty string should default to normal
	if key := q.pendingKeyForPriority(""); key != "test:queue:pending:normal" {
		t.Errorf("Empty priority key = %s, want test:queue:pending:normal", key)
	}
}

func TestPriorityQueue_RetryPreservesPriority(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	defer mr.Close()

	cfg := Config{
		RedisURL:   "redis://" + mr.Addr(),
		Prefix:     "test",
		MaxRetries: 3,
	}

	q, err := NewRedisQueue(cfg)
	if err != nil {
		t.Fatalf("Failed to create queue: %v", err)
	}
	defer q.Close()

	ctx := context.Background()

	msg := &Message{
		ID:          "msg-high-retry",
		Sender:      "sender@example.com",
		Priority:    PriorityHigh,
		NextAttempt: time.Now().Add(-time.Minute),
	}
	if err := q.Enqueue(ctx, msg); err != nil {
		t.Fatalf("Failed to enqueue: %v", err)
	}

	dequeued, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	if err := q.Retry(ctx, dequeued.ID, context.DeadlineExceeded); err != nil {
		t.Fatalf("Retry failed: %v", err)
	}

	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.PendingHigh != 1 {
		t.Fatalf("PendingHigh = %d, want 1", stats.PendingHigh)
	}
	if stats.Pending != 1 {
		t.Fatalf("Pending = %d, want 1", stats.Pending)
	}

	if got := q.client.ZCard(ctx, "test:queue:pending:high").Val(); got != 1 {
		t.Fatalf("high priority retry queue length = %d, want 1", got)
	}
	if got := q.client.ZCard(ctx, "test:queue:pending").Val(); got != 0 {
		t.Fatalf("legacy retry queue length = %d, want 0", got)
	}
}
