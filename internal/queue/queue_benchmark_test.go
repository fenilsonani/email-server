package queue

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// BenchmarkGenerateMessageID_Extended benchmarks unique ID generation.
func BenchmarkGenerateMessageID_Extended(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		generateMessageID()
	}
}

// BenchmarkGenerateMessageID_Parallel benchmarks parallel ID generation.
func BenchmarkGenerateMessageID_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			generateMessageID()
		}
	})
}

// BenchmarkCalculateNextRetry_Extended benchmarks retry calculation.
func BenchmarkCalculateNextRetry_Extended(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		calculateNextRetry(i % 15)
	}
}

// BenchmarkCalculateNextRetry_FirstAttempt benchmarks first attempt calculation.
func BenchmarkCalculateNextRetry_FirstAttempt(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		calculateNextRetry(1)
	}
}

// BenchmarkCalculateNextRetry_MaxAttempt benchmarks max attempt calculation.
func BenchmarkCalculateNextRetry_MaxAttempt(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		calculateNextRetry(100)
	}
}

// BenchmarkMessage_Marshal_Full benchmarks message JSON serialization.
func BenchmarkMessage_Marshal_Full(b *testing.B) {
	msg := &Message{
		ID:          "test-id-12345",
		Sender:      "sender@example.com",
		Recipients:  []string{"recipient@example.com"},
		MessagePath: "/path/to/message.eml",
		Size:        1024,
		Attempts:    3,
		MaxAttempts: 15,
		LastAttempt: time.Now(),
		NextAttempt: time.Now().Add(time.Hour),
		LastError:   "previous error",
		Status:      StatusPending,
		CreatedAt:   time.Now(),
		Domain:      "example.com",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(msg)
	}
}

// BenchmarkMessage_Unmarshal_Full benchmarks message JSON deserialization.
func BenchmarkMessage_Unmarshal_Full(b *testing.B) {
	msg := &Message{
		ID:          "test-id-12345",
		Sender:      "sender@example.com",
		Recipients:  []string{"recipient@example.com"},
		MessagePath: "/path/to/message.eml",
		Size:        1024,
		Attempts:    3,
		MaxAttempts: 15,
		LastAttempt: time.Now(),
		NextAttempt: time.Now().Add(time.Hour),
		LastError:   "previous error",
		Status:      StatusPending,
		CreatedAt:   time.Now(),
		Domain:      "example.com",
	}
	data, _ := json.Marshal(msg)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var m Message
		json.Unmarshal(data, &m)
	}
}

// BenchmarkMessage_MarshalLarge benchmarks large message serialization.
func BenchmarkMessage_MarshalLarge(b *testing.B) {
	recipients := make([]string, 100)
	for i := 0; i < 100; i++ {
		recipients[i] = "recipient" + string(rune('0'+i%10)) + "@example.com"
	}
	msg := &Message{
		ID:          "test-id-12345",
		Sender:      "sender@example.com",
		Recipients:  recipients,
		MessagePath: "/path/to/message.eml",
		Size:        1024000,
		Attempts:    3,
		MaxAttempts: 15,
		LastError:   strings.Repeat("error message ", 100),
		Status:      StatusPending,
		CreatedAt:   time.Now(),
		Domain:      "example.com",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(msg)
	}
}

// BenchmarkIsTransientRedisError benchmarks error classification.
func BenchmarkIsTransientRedisError(b *testing.B) {
	errors := []error{
		nil,
		newError("connection refused"),
		newError("timeout"),
		newError("syntax error"),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isTransientRedisError(errors[i%len(errors)])
	}
}

// BenchmarkIsTransientRedisError_Transient benchmarks transient error check.
func BenchmarkIsTransientRedisError_Transient(b *testing.B) {
	err := newError("connection refused")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isTransientRedisError(err)
	}
}

// BenchmarkIsTransientRedisError_NonTransient benchmarks non-transient error check.
func BenchmarkIsTransientRedisError_NonTransient(b *testing.B) {
	err := newError("syntax error in command")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isTransientRedisError(err)
	}
}

// BenchmarkContains benchmarks string contains helper.
func BenchmarkContains(b *testing.B) {
	s := "connection refused by remote host"
	substr := "refused"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		contains(s, substr)
	}
}

// BenchmarkContains_NotFound benchmarks contains when not found.
func BenchmarkContains_NotFound(b *testing.B) {
	s := "connection refused by remote host"
	substr := "timeout"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		contains(s, substr)
	}
}

// BenchmarkRedisQueue_KeyGeneration benchmarks key generation.
func BenchmarkRedisQueue_KeyGeneration(b *testing.B) {
	q := &RedisQueue{
		config: Config{Prefix: "mail"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.pendingKey()
		_ = q.processingKey()
		_ = q.failedKey()
		_ = q.sentKey()
		_ = q.messageKey("test-123")
		_ = q.statsKey()
	}
}

// BenchmarkRedisQueue_PendingKey benchmarks single key generation.
func BenchmarkRedisQueue_PendingKey(b *testing.B) {
	q := &RedisQueue{
		config: Config{Prefix: "mail"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.pendingKey()
	}
}

// BenchmarkRedisQueue_MessageKey benchmarks message key generation.
func BenchmarkRedisQueue_MessageKey(b *testing.B) {
	q := &RedisQueue{
		config: Config{Prefix: "mail"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.messageKey("test-message-id-12345")
	}
}

// BenchmarkRedisQueue_ValidateContext benchmarks context validation.
func BenchmarkRedisQueue_ValidateContext(b *testing.B) {
	q := &RedisQueue{closed: 0}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.validateContext(ctx)
	}
}

// BenchmarkRedisQueue_ValidateContext_Parallel benchmarks parallel validation.
func BenchmarkRedisQueue_ValidateContext_Parallel(b *testing.B) {
	q := &RedisQueue{closed: 0}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			q.validateContext(ctx)
		}
	})
}

// BenchmarkRedisQueue_IsClosed benchmarks closed check.
func BenchmarkRedisQueue_IsClosed(b *testing.B) {
	q := &RedisQueue{closed: 0}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.isClosed()
	}
}

// BenchmarkRedisQueue_IsClosed_Parallel benchmarks parallel closed check.
func BenchmarkRedisQueue_IsClosed_Parallel(b *testing.B) {
	q := &RedisQueue{closed: 0}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			q.isClosed()
		}
	})
}

// BenchmarkDefaultConfig benchmarks default config creation.
func BenchmarkDefaultConfig(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DefaultConfig()
	}
}

// BenchmarkQueueStats_ZeroAlloc benchmarks zero-allocation access.
func BenchmarkQueueStats_ZeroAlloc(b *testing.B) {
	stats := QueueStats{
		Pending:       100,
		Processing:    10,
		Sent:          5000,
		Failed:        50,
		TotalEnqueued: 10000,
		TotalSent:     9500,
		TotalFailed:   450,
		TotalRetried:  1000,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = stats.Pending
		_ = stats.Processing
		_ = stats.Sent
		_ = stats.Failed
	}
}

// BenchmarkMessage_StatusCheck benchmarks status comparison.
func BenchmarkMessage_StatusCheck(b *testing.B) {
	msg := Message{Status: StatusPending}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = msg.Status == StatusPending
		_ = msg.Status == StatusSending
		_ = msg.Status == StatusSent
		_ = msg.Status == StatusFailed
	}
}

// BenchmarkWaitGroup benchmarks sync.WaitGroup operations.
func BenchmarkWaitGroup(b *testing.B) {
	var wg sync.WaitGroup
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		wg.Done()
	}
}

// BenchmarkWaitGroup_Parallel benchmarks parallel WaitGroup usage.
func BenchmarkWaitGroup_Parallel(b *testing.B) {
	var wg sync.WaitGroup
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			wg.Add(1)
			wg.Done()
		}
	})
}

// helper for creating errors
type simpleError struct {
	msg string
}

func (e *simpleError) Error() string {
	return e.msg
}

func newError(msg string) error {
	return &simpleError{msg: msg}
}

// BenchmarkMessage_RecipientIteration benchmarks iterating over recipients.
func BenchmarkMessage_RecipientIteration_10(b *testing.B) {
	msg := Message{
		Recipients: make([]string, 10),
	}
	for i := 0; i < 10; i++ {
		msg.Recipients[i] = "recipient@example.com"
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, r := range msg.Recipients {
			_ = r
		}
	}
}

// BenchmarkMessage_RecipientIteration_100 benchmarks iterating over 100 recipients.
func BenchmarkMessage_RecipientIteration_100(b *testing.B) {
	msg := Message{
		Recipients: make([]string, 100),
	}
	for i := 0; i < 100; i++ {
		msg.Recipients[i] = "recipient@example.com"
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, r := range msg.Recipients {
			_ = r
		}
	}
}

// BenchmarkTimeNow benchmarks time.Now() calls.
func BenchmarkTimeNow(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		time.Now()
	}
}

// BenchmarkTimeSince benchmarks time.Since() calls.
func BenchmarkTimeSince(b *testing.B) {
	t := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		time.Since(t)
	}
}

// BenchmarkTimeAdd benchmarks time.Add() calls.
func BenchmarkTimeAdd(b *testing.B) {
	t := time.Now()
	d := time.Hour
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t.Add(d)
	}
}
