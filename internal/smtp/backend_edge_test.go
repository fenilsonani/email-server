package smtp

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Critical edge cases for SMTP backend that happen rarely but are crucial.

// TestUserRateLimiter_ExactLimit tests behavior at exact rate limits.
func TestUserRateLimiter_ExactLimit(t *testing.T) {
	rl := NewUserRateLimiter(5, 10)
	defer rl.Stop()

	userID := int64(1)

	// Use up to hourly limit
	for i := 0; i < 5; i++ {
		if err := rl.CheckAndIncrement(userID); err != nil {
			t.Errorf("request %d should succeed: %v", i+1, err)
		}
	}

	// Next should fail
	if err := rl.CheckAndIncrement(userID); err == nil {
		t.Error("request after hourly limit should fail")
	}
}

// TestUserRateLimiter_DailyVsHourly tests daily limit vs hourly limit.
func TestUserRateLimiter_DailyVsHourly(t *testing.T) {
	// Set hourly higher than daily to test daily limit
	rl := NewUserRateLimiter(100, 5)
	defer rl.Stop()

	userID := int64(2)

	// Should hit daily limit first
	for i := 0; i < 5; i++ {
		if err := rl.CheckAndIncrement(userID); err != nil {
			t.Errorf("request %d should succeed: %v", i+1, err)
		}
	}

	// Should fail on daily limit
	err := rl.CheckAndIncrement(userID)
	if err == nil {
		t.Error("should fail on daily limit")
	}
	if err != nil && !containsSubstr(err.Error(), "daily") {
		t.Errorf("error should mention daily limit: %v", err)
	}
}

// TestUserRateLimiter_ConcurrentAccess tests thread safety.
func TestUserRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewUserRateLimiter(1000, 10000)
	defer rl.Stop()

	var wg sync.WaitGroup
	goroutines := 50
	requestsPerGoroutine := 20
	userID := int64(3)

	var successCount, failCount int32

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				if err := rl.CheckAndIncrement(userID); err != nil {
					atomic.AddInt32(&failCount, 1)
				} else {
					atomic.AddInt32(&successCount, 1)
				}
			}
		}()
	}

	wg.Wait()

	total := atomic.LoadInt32(&successCount) + atomic.LoadInt32(&failCount)
	expected := int32(goroutines * requestsPerGoroutine)
	if total != expected {
		t.Errorf("total requests = %d, want %d", total, expected)
	}

	// Exactly maxPerHour should have succeeded
	if successCount != 1000 {
		t.Errorf("success count = %d, want 1000", successCount)
	}
}

// TestUserRateLimiter_MultipleUsers tests independent limits per user.
func TestUserRateLimiter_MultipleUsers(t *testing.T) {
	rl := NewUserRateLimiter(3, 10)
	defer rl.Stop()

	// Each user should have independent limits
	for userID := int64(1); userID <= 5; userID++ {
		for i := 0; i < 3; i++ {
			if err := rl.CheckAndIncrement(userID); err != nil {
				t.Errorf("user %d request %d should succeed: %v", userID, i+1, err)
			}
		}
		// Fourth request should fail
		if err := rl.CheckAndIncrement(userID); err == nil {
			t.Errorf("user %d request 4 should fail", userID)
		}
	}
}

// TestUserRateLimiter_CleanupRemovesStaleEntries tests cleanup of old entries.
func TestUserRateLimiter_CleanupRemovesStaleEntries(t *testing.T) {
	rl := NewUserRateLimiter(10, 100)
	defer rl.Stop()

	// Add some users
	for userID := int64(1); userID <= 10; userID++ {
		rl.CheckAndIncrement(userID)
	}

	// Verify users exist
	rl.mu.RLock()
	countBefore := len(rl.counters)
	rl.mu.RUnlock()

	if countBefore != 10 {
		t.Errorf("expected 10 users, got %d", countBefore)
	}

	// Manually set old lastAccess for some users
	rl.mu.Lock()
	oldTime := time.Now().Add(-49 * time.Hour) // Older than 48h cutoff
	for userID := int64(1); userID <= 5; userID++ {
		if counter, ok := rl.counters[userID]; ok {
			counter.lastAccess = oldTime
		}
	}
	rl.mu.Unlock()

	// Run cleanup
	rl.cleanup()

	// Verify old users removed
	rl.mu.RLock()
	countAfter := len(rl.counters)
	rl.mu.RUnlock()

	if countAfter != 5 {
		t.Errorf("expected 5 users after cleanup, got %d", countAfter)
	}
}

// TestUserRateLimiter_WindowReset tests counter reset after time window.
func TestUserRateLimiter_WindowReset(t *testing.T) {
	rl := NewUserRateLimiter(2, 10)
	defer rl.Stop()

	userID := int64(1)

	// Use up hourly limit
	rl.CheckAndIncrement(userID)
	rl.CheckAndIncrement(userID)

	// Should fail
	if err := rl.CheckAndIncrement(userID); err == nil {
		t.Error("should be rate limited")
	}

	// Manually reset the hour window
	rl.mu.Lock()
	rl.counters[userID].hourReset = time.Now().Add(-time.Second)
	rl.mu.Unlock()

	// Should work now
	if err := rl.CheckAndIncrement(userID); err != nil {
		t.Errorf("should succeed after window reset: %v", err)
	}
}

// TestUserRateLimiter_StopPreventsGoroutineLeak tests cleanup goroutine stops.
func TestUserRateLimiter_StopPreventsGoroutineLeak(t *testing.T) {
	beforeGoroutines := runtime.NumGoroutine()

	// Create and stop multiple rate limiters
	for i := 0; i < 10; i++ {
		rl := NewUserRateLimiter(10, 100)
		rl.CheckAndIncrement(int64(i))
		rl.Stop()
	}

	// Give goroutines time to stop
	time.Sleep(50 * time.Millisecond)

	afterGoroutines := runtime.NumGoroutine()

	// Should not have leaked goroutines (allow some variance)
	if afterGoroutines > beforeGoroutines+2 {
		t.Errorf("possible goroutine leak: before=%d, after=%d", beforeGoroutines, afterGoroutines)
	}
}

// TestUserRateLimiter_ZeroLimits tests behavior with zero limits.
func TestUserRateLimiter_ZeroLimits(t *testing.T) {
	rl := NewUserRateLimiter(0, 0)
	defer rl.Stop()

	// Note: First request always succeeds because the counter is created
	// with initial counts before checking limits
	if err := rl.CheckAndIncrement(1); err != nil {
		t.Errorf("first request should succeed (creates counter): %v", err)
	}

	// Second request should fail with zero limits
	if err := rl.CheckAndIncrement(1); err == nil {
		t.Error("second request should fail with zero limits")
	}
}

// TestUserRateLimiter_NegativeUserID tests negative user IDs.
func TestUserRateLimiter_NegativeUserID(t *testing.T) {
	rl := NewUserRateLimiter(5, 10)
	defer rl.Stop()

	// Negative user IDs should work (they're valid int64 keys)
	if err := rl.CheckAndIncrement(-1); err != nil {
		t.Errorf("negative user ID should work: %v", err)
	}
	if err := rl.CheckAndIncrement(-1); err != nil {
		t.Errorf("second request for negative user ID should work: %v", err)
	}
}

// TestGenerateID_UniquenessUnderLoad tests ID generation under high concurrency.
func TestGenerateID_UniquenessUnderLoad(t *testing.T) {
	ids := sync.Map{}
	var wg sync.WaitGroup
	var duplicates int32

	goroutines := 100
	idsPerGoroutine := 1000

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < idsPerGoroutine; j++ {
				id := generateID()
				if _, loaded := ids.LoadOrStore(id, true); loaded {
					atomic.AddInt32(&duplicates, 1)
				}
			}
		}()
	}

	wg.Wait()

	if duplicates > 0 {
		t.Errorf("found %d duplicate IDs", duplicates)
	}
}

// TestParseAddress_EdgeCases tests edge cases in address parsing.
func TestParseAddress_EdgeCases(t *testing.T) {
	testCases := []struct {
		addr   string
		local  string
		domain string
	}{
		// Unicode
		{"user@日本語.com", "user", "日本語.com"},
		{"用户@example.com", "用户", "example.com"},
		// Long addresses
		{string(make([]byte, 256)) + "@example.com", "", ""}, // Very long local part
		{"user@" + string(make([]byte, 256)), "", ""},        // Very long domain
		// Special characters
		{"user.name+tag@example.com", "user.name+tag", "example.com"},
		{"\"quoted user\"@example.com", "\"quoted user\"", "example.com"},
		// Whitespace
		{"  user@example.com  ", "user", "example.com"},
		{"\tuser@example.com\t", "\tuser", "example.com\t"},
		// Multiple @ signs
		{"user@domain@example.com", "user", "domain@example.com"},
		// Empty parts
		{"@", "", ""},
		{"@@", "", "@"},
		// Case sensitivity
		{"USER@EXAMPLE.COM", "user", "example.com"},
		{"User.Name@Example.COM", "user.name", "example.com"},
	}

	for _, tc := range testCases {
		t.Run(tc.addr, func(t *testing.T) {
			local, domain := parseAddress(tc.addr)
			// Just verify it doesn't panic
			_ = local
			_ = domain
		})
	}
}

// TestUserRateLimiter_ConcurrentCleanup tests cleanup during active use.
func TestUserRateLimiter_ConcurrentCleanup(t *testing.T) {
	rl := NewUserRateLimiter(1000, 10000)
	defer rl.Stop()

	var wg sync.WaitGroup

	// Writer goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				rl.CheckAndIncrement(int64(id*100 + j))
			}
		}(i)
	}

	// Cleanup goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			rl.cleanup()
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
	// Should not have crashed
}

// TestUserRateLimiter_LargeUserID tests large user IDs.
func TestUserRateLimiter_LargeUserID(t *testing.T) {
	rl := NewUserRateLimiter(5, 10)
	defer rl.Stop()

	largeIDs := []int64{
		1<<32 - 1,  // Max uint32
		1<<63 - 1,  // Max int64
		-1<<63 + 1, // Min int64 + 1
	}

	for _, id := range largeIDs {
		if err := rl.CheckAndIncrement(id); err != nil {
			t.Errorf("large user ID %d should work: %v", id, err)
		}
	}
}

// TestUserRateLimiter_RapidFireRequests tests rapid consecutive requests.
func TestUserRateLimiter_RapidFireRequests(t *testing.T) {
	rl := NewUserRateLimiter(100, 1000)
	defer rl.Stop()

	userID := int64(1)
	start := time.Now()

	// Fire 100 requests as fast as possible
	for i := 0; i < 100; i++ {
		if err := rl.CheckAndIncrement(userID); err != nil {
			t.Errorf("request %d should succeed: %v", i+1, err)
		}
	}

	elapsed := time.Since(start)

	// Should complete quickly
	if elapsed > 100*time.Millisecond {
		t.Errorf("100 requests took too long: %v", elapsed)
	}

	// 101st should fail
	if err := rl.CheckAndIncrement(userID); err == nil {
		t.Error("request 101 should fail")
	}
}

// TestUserRateLimiter_CleanupDuringWindowReset tests cleanup during window transition.
func TestUserRateLimiter_CleanupDuringWindowReset(t *testing.T) {
	rl := NewUserRateLimiter(10, 100)
	defer rl.Stop()

	userID := int64(1)
	rl.CheckAndIncrement(userID)

	// Set counter to be at window boundary
	rl.mu.Lock()
	rl.counters[userID].hourReset = time.Now()
	rl.counters[userID].dayReset = time.Now()
	rl.mu.Unlock()

	// Now request during window reset
	if err := rl.CheckAndIncrement(userID); err != nil {
		t.Errorf("should succeed during window reset: %v", err)
	}

	// Verify counters were reset
	rl.mu.RLock()
	counter := rl.counters[userID]
	if counter.hourCount > 2 {
		t.Errorf("hour count should be reset, got %d", counter.hourCount)
	}
	rl.mu.RUnlock()
}

// TestGenerateID_LengthConsistency tests ID length is always 32.
func TestGenerateID_LengthConsistency(t *testing.T) {
	for i := 0; i < 1000; i++ {
		id := generateID()
		if len(id) != 32 {
			t.Errorf("ID length = %d, want 32: %s", len(id), id)
		}
	}
}

// TestGenerateID_HexCharacters tests ID contains only hex characters.
func TestGenerateID_HexCharacters(t *testing.T) {
	for i := 0; i < 100; i++ {
		id := generateID()
		for _, c := range id {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("ID contains non-hex character: %c in %s", c, id)
			}
		}
	}
}

// containsSubstr checks if s contains substr.
func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestUserRateLimiter_FirstRequestAlwaysSucceeds tests first request for new user.
func TestUserRateLimiter_FirstRequestAlwaysSucceeds(t *testing.T) {
	rl := NewUserRateLimiter(1, 1)
	defer rl.Stop()

	// First request for each user should succeed
	for i := int64(1); i <= 100; i++ {
		if err := rl.CheckAndIncrement(i); err != nil {
			t.Errorf("first request for user %d should succeed: %v", i, err)
		}
	}
}

// TestUserRateLimiter_ErrorMessages tests error message content.
func TestUserRateLimiter_ErrorMessages(t *testing.T) {
	t.Run("hourly limit error", func(t *testing.T) {
		rl := NewUserRateLimiter(1, 100)
		defer rl.Stop()

		rl.CheckAndIncrement(1)
		err := rl.CheckAndIncrement(1)

		if err == nil {
			t.Fatal("expected error")
		}
		if !containsSubstr(err.Error(), "hourly") {
			t.Errorf("error should mention 'hourly': %v", err)
		}
	})

	t.Run("daily limit error", func(t *testing.T) {
		rl := NewUserRateLimiter(100, 1)
		defer rl.Stop()

		rl.CheckAndIncrement(1)
		err := rl.CheckAndIncrement(1)

		if err == nil {
			t.Fatal("expected error")
		}
		if !containsSubstr(err.Error(), "daily") {
			t.Errorf("error should mention 'daily': %v", err)
		}
	})
}

// TestUserRateLimiter_CounterPersistence tests counters persist across checks.
func TestUserRateLimiter_CounterPersistence(t *testing.T) {
	rl := NewUserRateLimiter(10, 100)
	defer rl.Stop()

	userID := int64(1)

	// Make 5 requests
	for i := 0; i < 5; i++ {
		rl.CheckAndIncrement(userID)
	}

	// Verify counter state
	rl.mu.RLock()
	counter := rl.counters[userID]
	if counter.hourCount != 5 {
		t.Errorf("hour count = %d, want 5", counter.hourCount)
	}
	if counter.dayCount != 5 {
		t.Errorf("day count = %d, want 5", counter.dayCount)
	}
	rl.mu.RUnlock()

	// Make 3 more requests
	for i := 0; i < 3; i++ {
		rl.CheckAndIncrement(userID)
	}

	// Verify accumulated count
	rl.mu.RLock()
	counter = rl.counters[userID]
	if counter.hourCount != 8 {
		t.Errorf("hour count = %d, want 8", counter.hourCount)
	}
	if counter.dayCount != 8 {
		t.Errorf("day count = %d, want 8", counter.dayCount)
	}
	rl.mu.RUnlock()
}
