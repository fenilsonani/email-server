package ratelimit

import (
	"testing"
	"time"
)

func TestNewPersistentLimiter_MemoryOnly(t *testing.T) {
	// Create limiter without Redis (memory-only mode)
	limiter := NewPersistentLimiter(nil, DefaultConfig())
	if limiter == nil {
		t.Fatal("NewPersistentLimiter returned nil")
	}
	defer limiter.Close()

	// Should work in memory-only mode
	if !limiter.Allow("test-key") {
		t.Error("Should allow first request")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxAttempts != 5 {
		t.Errorf("Expected MaxAttempts=5, got %d", cfg.MaxAttempts)
	}

	if cfg.WindowSize != 15*time.Minute {
		t.Errorf("Expected WindowSize=15m, got %v", cfg.WindowSize)
	}

	if cfg.BlockTime != 30*time.Minute {
		t.Errorf("Expected BlockTime=30m, got %v", cfg.BlockTime)
	}

	if cfg.KeyPrefix != "ratelimit" {
		t.Errorf("Expected KeyPrefix='ratelimit', got %q", cfg.KeyPrefix)
	}
}

func TestPersistentLimiter_Allow(t *testing.T) {
	cfg := Config{
		MaxAttempts: 3,
		WindowSize:  1 * time.Minute,
		BlockTime:   1 * time.Minute,
	}
	limiter := NewPersistentLimiter(nil, cfg)
	defer limiter.Close()

	key := "test-allow"

	// First 3 attempts should be allowed
	for i := 0; i < 3; i++ {
		if !limiter.Allow(key) {
			t.Errorf("Attempt %d should be allowed", i+1)
		}
		limiter.RecordAttempt(key, false)
	}

	// 4th attempt should be blocked (after recording 3 failures)
	if limiter.Allow(key) {
		t.Error("4th attempt should be blocked")
	}
}

func TestPersistentLimiter_RecordAttempt_Success(t *testing.T) {
	cfg := Config{
		MaxAttempts: 3,
		WindowSize:  1 * time.Minute,
		BlockTime:   1 * time.Minute,
	}
	limiter := NewPersistentLimiter(nil, cfg)
	defer limiter.Close()

	key := "test-success"

	// Record 2 failed attempts
	limiter.RecordAttempt(key, false)
	limiter.RecordAttempt(key, false)

	// Remaining should be 1
	if limiter.RemainingAttempts(key) != 1 {
		t.Errorf("Expected 1 remaining, got %d", limiter.RemainingAttempts(key))
	}

	// Record successful attempt - should reset
	limiter.RecordAttempt(key, true)

	// Should be back to max attempts
	if limiter.RemainingAttempts(key) != 3 {
		t.Errorf("Expected 3 remaining after success, got %d", limiter.RemainingAttempts(key))
	}
}

func TestPersistentLimiter_Block(t *testing.T) {
	limiter := NewPersistentLimiter(nil, DefaultConfig())
	defer limiter.Close()

	key := "test-block"

	// Block the key
	limiter.Block(key, 100*time.Millisecond)

	if !limiter.IsBlocked(key) {
		t.Error("Key should be blocked")
	}

	if limiter.Allow(key) {
		t.Error("Blocked key should not be allowed")
	}

	// Wait for block to expire
	time.Sleep(150 * time.Millisecond)

	if limiter.IsBlocked(key) {
		t.Error("Block should have expired")
	}

	if !limiter.Allow(key) {
		t.Error("Key should be allowed after block expires")
	}
}

func TestPersistentLimiter_Unblock(t *testing.T) {
	limiter := NewPersistentLimiter(nil, DefaultConfig())
	defer limiter.Close()

	key := "test-unblock"

	// Block the key
	limiter.Block(key, 10*time.Minute)

	if !limiter.IsBlocked(key) {
		t.Error("Key should be blocked")
	}

	// Unblock
	limiter.Unblock(key)

	if limiter.IsBlocked(key) {
		t.Error("Key should be unblocked")
	}
}

func TestPersistentLimiter_RemainingAttempts(t *testing.T) {
	cfg := Config{
		MaxAttempts: 5,
		WindowSize:  1 * time.Minute,
		BlockTime:   1 * time.Minute,
	}
	limiter := NewPersistentLimiter(nil, cfg)
	defer limiter.Close()

	key := "test-remaining"

	// Initially should have all attempts
	if limiter.RemainingAttempts(key) != 5 {
		t.Errorf("Expected 5 remaining, got %d", limiter.RemainingAttempts(key))
	}

	// After 2 failures, should have 3 remaining
	limiter.RecordAttempt(key, false)
	limiter.RecordAttempt(key, false)

	if limiter.RemainingAttempts(key) != 3 {
		t.Errorf("Expected 3 remaining, got %d", limiter.RemainingAttempts(key))
	}
}

func TestPersistentLimiter_BlockedUntil(t *testing.T) {
	limiter := NewPersistentLimiter(nil, DefaultConfig())
	defer limiter.Close()

	key := "test-blocked-until"

	// Not blocked - should return zero time
	until := limiter.BlockedUntil(key)
	if !until.IsZero() {
		t.Error("Should return zero time when not blocked")
	}

	// Block and check
	blockDuration := 5 * time.Minute
	limiter.Block(key, blockDuration)

	until = limiter.BlockedUntil(key)
	expected := time.Now().Add(blockDuration)

	// Should be within 1 second of expected
	if until.Before(expected.Add(-1*time.Second)) || until.After(expected.Add(1*time.Second)) {
		t.Errorf("BlockedUntil %v not close to expected %v", until, expected)
	}
}

func TestPersistentLimiter_Stats(t *testing.T) {
	limiter := NewPersistentLimiter(nil, DefaultConfig())
	defer limiter.Close()

	// Record some activity
	limiter.RecordAttempt("key1", false)
	limiter.RecordAttempt("key2", false)
	limiter.Block("key3", 1*time.Minute)

	stats := limiter.Stats()

	if stats.TrackedKeys < 2 {
		t.Errorf("Expected at least 2 tracked keys, got %d", stats.TrackedKeys)
	}

	if stats.BlockedKeys < 1 {
		t.Errorf("Expected at least 1 blocked key, got %d", stats.BlockedKeys)
	}

	// Memory-only mode
	if stats.RedisHealthy {
		t.Error("Redis should not be healthy in memory-only mode")
	}
}

func TestPersistentLimiter_AutoBlock(t *testing.T) {
	cfg := Config{
		MaxAttempts: 3,
		WindowSize:  1 * time.Minute,
		BlockTime:   100 * time.Millisecond,
	}
	limiter := NewPersistentLimiter(nil, cfg)
	defer limiter.Close()

	key := "test-auto-block"

	// Record max failures
	for i := 0; i < 3; i++ {
		limiter.RecordAttempt(key, false)
	}

	// Should be auto-blocked
	if !limiter.IsBlocked(key) {
		t.Error("Key should be auto-blocked after max attempts")
	}

	// Wait for block to expire
	time.Sleep(150 * time.Millisecond)

	if limiter.IsBlocked(key) {
		t.Error("Block should have expired")
	}
}

func TestPersistentLimiter_Close(t *testing.T) {
	limiter := NewPersistentLimiter(nil, DefaultConfig())

	// Close should not error
	if err := limiter.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	// Double close should be safe
	if err := limiter.Close(); err != nil {
		t.Errorf("Double close returned error: %v", err)
	}
}

func TestPersistentLimiter_KeyGeneration(t *testing.T) {
	cfg := Config{KeyPrefix: "myprefix"}
	limiter := NewPersistentLimiter(nil, cfg)
	defer limiter.Close()

	// Test internal key generation
	attemptKey := limiter.attemptKey("user@example.com")
	if attemptKey != "myprefix:attempts:user@example.com" {
		t.Errorf("Unexpected attempt key: %s", attemptKey)
	}

	blockKey := limiter.blockKey("192.168.1.1")
	if blockKey != "myprefix:blocks:192.168.1.1" {
		t.Errorf("Unexpected block key: %s", blockKey)
	}
}

func TestPersistentLimiter_WindowExpiry(t *testing.T) {
	cfg := Config{
		MaxAttempts: 3,
		WindowSize:  100 * time.Millisecond,
		BlockTime:   1 * time.Minute,
	}
	limiter := NewPersistentLimiter(nil, cfg)
	defer limiter.Close()

	key := "test-window-expiry"

	// Record 2 failures
	limiter.RecordAttempt(key, false)
	limiter.RecordAttempt(key, false)

	if limiter.RemainingAttempts(key) != 1 {
		t.Errorf("Expected 1 remaining, got %d", limiter.RemainingAttempts(key))
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should be back to full attempts
	if limiter.RemainingAttempts(key) != 3 {
		t.Errorf("Expected 3 remaining after window expiry, got %d", limiter.RemainingAttempts(key))
	}
}

func TestPersistentLimiter_MultipleKeys(t *testing.T) {
	cfg := Config{
		MaxAttempts: 3,
		WindowSize:  1 * time.Minute,
		BlockTime:   1 * time.Minute,
	}
	limiter := NewPersistentLimiter(nil, cfg)
	defer limiter.Close()

	// Test that different keys are tracked separately
	limiter.RecordAttempt("key1", false)
	limiter.RecordAttempt("key1", false)
	limiter.RecordAttempt("key2", false)

	if limiter.RemainingAttempts("key1") != 1 {
		t.Errorf("key1: expected 1 remaining, got %d", limiter.RemainingAttempts("key1"))
	}

	if limiter.RemainingAttempts("key2") != 2 {
		t.Errorf("key2: expected 2 remaining, got %d", limiter.RemainingAttempts("key2"))
	}

	if limiter.RemainingAttempts("key3") != 3 {
		t.Errorf("key3: expected 3 remaining (new key), got %d", limiter.RemainingAttempts("key3"))
	}
}

func TestPersistentLimiter_ConfigDefaults(t *testing.T) {
	// Test that defaults are applied for zero values
	cfg := Config{} // All zeros
	limiter := NewPersistentLimiter(nil, cfg)
	defer limiter.Close()

	// Should use defaults
	if limiter.maxAttempts != 5 {
		t.Errorf("Expected default maxAttempts=5, got %d", limiter.maxAttempts)
	}

	if limiter.windowSize != 15*time.Minute {
		t.Errorf("Expected default windowSize=15m, got %v", limiter.windowSize)
	}

	if limiter.blockTime != 30*time.Minute {
		t.Errorf("Expected default blockTime=30m, got %v", limiter.blockTime)
	}

	if limiter.prefix != "ratelimit" {
		t.Errorf("Expected default prefix='ratelimit', got %q", limiter.prefix)
	}
}

func BenchmarkPersistentLimiter_Allow(b *testing.B) {
	limiter := NewPersistentLimiter(nil, DefaultConfig())
	defer limiter.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow("bench-key")
	}
}

func BenchmarkPersistentLimiter_RecordAttempt(b *testing.B) {
	limiter := NewPersistentLimiter(nil, DefaultConfig())
	defer limiter.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.RecordAttempt("bench-key", i%2 == 0)
	}
}
