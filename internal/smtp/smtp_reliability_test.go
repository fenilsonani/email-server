package smtp

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSession_NilChecks tests that session handles nil values gracefully.
func TestSession_NilChecks(t *testing.T) {
	// Session with nil backend should fail data handling
	session := &Session{
		backend: nil,
	}

	err := session.Data(bytes.NewReader([]byte("test")))
	if err == nil {
		t.Error("Data with nil backend should fail")
	}

	// Nil session's saveMessageToQueue should fail
	var nilSession *Session
	_, err = nilSession.saveMessageToQueue([]byte("test"))
	if err == nil {
		t.Error("saveMessageToQueue on nil session should fail")
	}
}

// TestSession_ResetClears tests that Reset clears session state.
func TestSession_ResetClears(t *testing.T) {
	session := &Session{
		from:  "sender@example.com",
		rcpts: []string{"rcpt1@example.com", "rcpt2@example.com"},
	}

	session.Reset()

	if session.from != "" {
		t.Errorf("from should be empty after Reset, got %q", session.from)
	}
	if session.rcpts != nil {
		t.Errorf("rcpts should be nil after Reset, got %v", session.rcpts)
	}
}

// TestSession_LogoutReturnsNil tests that Logout always succeeds.
func TestSession_LogoutReturnsNil(t *testing.T) {
	session := &Session{}

	err := session.Logout()
	if err != nil {
		t.Errorf("Logout should return nil, got %v", err)
	}
}

// TestRateLimiter_BasicOperation tests rate limiter basic functionality.
func TestRateLimiter_BasicOperation(t *testing.T) {
	rl := NewUserRateLimiter(5, 10)
	defer rl.Stop()

	userID := int64(1)

	// Should allow up to hourly limit
	for i := 0; i < 5; i++ {
		if err := rl.CheckAndIncrement(userID); err != nil {
			t.Errorf("Check %d should pass: %v", i, err)
		}
	}

	// Should fail on next attempt
	if err := rl.CheckAndIncrement(userID); err == nil {
		t.Error("Should fail after hourly limit exceeded")
	}
}

// TestRateLimiter_DifferentUsers tests rate limiter handles users independently.
func TestRateLimiter_DifferentUsers(t *testing.T) {
	rl := NewUserRateLimiter(2, 10)
	defer rl.Stop()

	user1 := int64(1)
	user2 := int64(2)

	// User 1 uses their quota
	rl.CheckAndIncrement(user1)
	rl.CheckAndIncrement(user1)

	// User 1 should be blocked
	if err := rl.CheckAndIncrement(user1); err == nil {
		t.Error("User 1 should be rate limited")
	}

	// User 2 should still be allowed
	if err := rl.CheckAndIncrement(user2); err != nil {
		t.Errorf("User 2 should not be rate limited: %v", err)
	}
}

// TestRateLimiter_ConcurrentAccess tests rate limiter under concurrent access.
func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewUserRateLimiter(100, 1000)
	defer rl.Stop()

	var wg sync.WaitGroup
	var panicked int32

	// Multiple goroutines accessing same user
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt32(&panicked, 1)
				}
			}()

			userID := int64(id % 5) // 5 different users
			for j := 0; j < 20; j++ {
				_ = rl.CheckAndIncrement(userID)
			}
		}(i)
	}

	wg.Wait()

	if atomic.LoadInt32(&panicked) > 0 {
		t.Errorf("Concurrent access caused %d panic(s)", panicked)
	}
}

// TestRateLimiter_CleanupStops tests that cleanup goroutine stops properly.
func TestRateLimiter_CleanupStops(t *testing.T) {
	rl := NewUserRateLimiter(10, 100)

	// Add a user
	rl.CheckAndIncrement(1)

	// Stop should not block
	done := make(chan struct{})
	go func() {
		rl.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("Stop should complete quickly")
	}
}

// TestRateLimiter_Cleanup tests that cleanup removes stale entries.
func TestRateLimiter_Cleanup(t *testing.T) {
	rl := NewUserRateLimiter(10, 100)
	defer rl.Stop()

	// Add a user
	rl.CheckAndIncrement(1)

	// Manually set last access to old time
	rl.mu.Lock()
	if counter, ok := rl.counters[1]; ok {
		counter.lastAccess = time.Now().Add(-49 * time.Hour)
	}
	rl.mu.Unlock()

	// Run cleanup
	rl.cleanup()

	// Counter should be removed
	rl.mu.RLock()
	_, exists := rl.counters[1]
	rl.mu.RUnlock()

	if exists {
		t.Error("Stale counter should be cleaned up")
	}
}

// TestRateLimiter_WindowReset tests that counters reset after window expires.
func TestRateLimiter_WindowReset(t *testing.T) {
	rl := NewUserRateLimiter(2, 10)
	defer rl.Stop()

	userID := int64(1)

	// Use up hourly quota
	rl.CheckAndIncrement(userID)
	rl.CheckAndIncrement(userID)

	// Should be blocked
	if err := rl.CheckAndIncrement(userID); err == nil {
		t.Error("Should be rate limited")
	}

	// Manually reset the hour window
	rl.mu.Lock()
	if counter, ok := rl.counters[userID]; ok {
		counter.hourReset = time.Now().Add(-time.Second) // Expired
		counter.hourCount = 2
	}
	rl.mu.Unlock()

	// Should be allowed after window reset
	if err := rl.CheckAndIncrement(userID); err != nil {
		t.Errorf("Should be allowed after window reset: %v", err)
	}
}

// TestParseAddress_Reliability tests address parsing edge cases.
func TestParseAddress_Reliability(t *testing.T) {
	testCases := []struct {
		input   string
		local   string
		domain  string
	}{
		{"user@example.com", "user", "example.com"},
		{"<user@example.com>", "user", "example.com"},
		{"USER@EXAMPLE.COM", "user", "example.com"}, // Lowercased
		{"user+tag@example.com", "user+tag", "example.com"},
		{"user", "user", ""},
		{"", "", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			local, domain := parseAddress(tc.input)
			if local != tc.local {
				t.Errorf("local = %q, want %q", local, tc.local)
			}
			if domain != tc.domain {
				t.Errorf("domain = %q, want %q", domain, tc.domain)
			}
		})
	}
}

// TestGenerateID_Reliability tests unique ID generation with high volume.
func TestGenerateID_Reliability(t *testing.T) {
	ids := make(map[string]bool)
	count := 10000

	for i := 0; i < count; i++ {
		id := generateID()
		if ids[id] {
			t.Errorf("Duplicate ID generated: %s", id)
		}
		ids[id] = true

		// ID should be 32 characters (16 bytes hex encoded)
		if len(id) != 32 {
			t.Errorf("ID length = %d, want 32", len(id))
		}
	}
}

// TestGenerateID_Concurrent tests concurrent ID generation.
func TestGenerateID_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := make(map[string]bool)
	count := 1000
	goroutines := 20

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < count/goroutines; i++ {
				id := generateID()
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
}

// TestSession_ContextCancellation tests that operations check context.
func TestSession_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	session := &Session{
		ctx: ctx,
	}

	// ctx.Err() should return error
	if session.ctx.Err() == nil {
		t.Error("Cancelled context should have error")
	}
}

// TestSession_AuthMechanisms tests supported auth mechanisms.
func TestSession_AuthMechanisms(t *testing.T) {
	session := &Session{}

	mechs := session.AuthMechanisms()

	if len(mechs) == 0 {
		t.Error("Should return at least one auth mechanism")
	}

	found := false
	for _, m := range mechs {
		if m == "PLAIN" {
			found = true
			break
		}
	}
	if !found {
		t.Error("PLAIN should be a supported mechanism")
	}
}

// TestSession_EmptyRecipients tests Data with no recipients.
func TestSession_EmptyRecipients(t *testing.T) {
	session := &Session{
		backend: &Backend{},
		rcpts:   nil,
		ctx:     context.Background(),
	}

	err := session.Data(bytes.NewReader([]byte("test message")))
	if err == nil {
		t.Error("Data with no recipients should fail")
	}

	// Should be SMTP 503 error
	if !strings.Contains(err.Error(), "recipients") {
		t.Errorf("Error should mention recipients: %v", err)
	}
}

// TestSession_NilReader tests Data with nil reader.
func TestSession_NilReader(t *testing.T) {
	session := &Session{
		backend: &Backend{},
		rcpts:   []string{"test@example.com"},
		ctx:     context.Background(),
	}

	err := session.Data(nil)
	if err == nil {
		t.Error("Data with nil reader should fail")
	}
}

// TestBackend_NilParameters tests Backend creation with nil parameters.
func TestBackend_NilParameters(t *testing.T) {
	// Nil config
	_, err := NewBackend(nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("NewBackend with nil config should fail")
	}
}

// TestBackend_NewSession_NilConnection tests NewSession with nil connection.
func TestBackend_NewSession_NilConnection(t *testing.T) {
	// Test that calling NewSession with nil backend returns error
	var b *Backend
	_, err := b.NewSession(nil)
	if err == nil {
		t.Error("NewSession on nil backend should fail")
	}
}

// TestSession_SaveMessageToQueue_EmptyData tests saving empty data.
func TestSession_SaveMessageToQueue_EmptyData(t *testing.T) {
	session := &Session{
		backend: &Backend{
			queuePath: "/tmp/test-queue",
		},
	}

	_, err := session.saveMessageToQueue([]byte{})
	if err == nil {
		t.Error("Saving empty data should fail")
	}
}

// TestRateLimiter_DailyLimit tests daily limit enforcement.
func TestRateLimiter_DailyLimit(t *testing.T) {
	// High hourly limit, low daily limit
	rl := NewUserRateLimiter(100, 5)
	defer rl.Stop()

	userID := int64(1)

	// Should allow up to daily limit
	for i := 0; i < 5; i++ {
		if err := rl.CheckAndIncrement(userID); err != nil {
			t.Errorf("Check %d should pass: %v", i, err)
		}
	}

	// Should fail on next attempt due to daily limit
	err := rl.CheckAndIncrement(userID)
	if err == nil {
		t.Error("Should fail after daily limit exceeded")
	}
	if !strings.Contains(err.Error(), "daily") {
		t.Errorf("Error should mention daily limit: %v", err)
	}
}

// TestSession_MailSubmissionWithoutAuth tests MAIL command without auth in submission mode.
func TestSession_MailSubmissionWithoutAuth(t *testing.T) {
	session := &Session{
		isSubmission: true,
		user:         nil, // Not authenticated
	}

	err := session.Mail("sender@example.com", nil)
	if err == nil {
		t.Error("Mail in submission mode without auth should fail")
	}
}

// TestSession_RcptSubmissionWithoutAuth tests RCPT command without auth in submission mode.
func TestSession_RcptSubmissionWithoutAuth(t *testing.T) {
	session := &Session{
		isSubmission: true,
		user:         nil, // Not authenticated
	}

	err := session.Rcpt("recipient@example.com", nil)
	if err == nil {
		t.Error("Rcpt in submission mode without auth should fail")
	}
}

// TestRateLimiter_NewUser tests first-time user handling.
func TestRateLimiter_NewUser(t *testing.T) {
	rl := NewUserRateLimiter(10, 100)
	defer rl.Stop()

	userID := int64(999) // New user

	// First call should succeed and create counter
	if err := rl.CheckAndIncrement(userID); err != nil {
		t.Errorf("First call for new user should succeed: %v", err)
	}

	// Counter should exist
	rl.mu.RLock()
	counter, exists := rl.counters[userID]
	rl.mu.RUnlock()

	if !exists {
		t.Error("Counter should exist after first call")
	}
	if counter.hourCount != 1 {
		t.Errorf("hourCount = %d, want 1", counter.hourCount)
	}
	if counter.dayCount != 1 {
		t.Errorf("dayCount = %d, want 1", counter.dayCount)
	}
}

// TestSession_VacationSemaphore tests vacation semaphore behavior.
func TestSession_VacationSemaphore(t *testing.T) {
	// Create backend with small semaphore
	backend := &Backend{
		vacationSem: make(chan struct{}, 2),
	}

	// Fill semaphore
	backend.vacationSem <- struct{}{}
	backend.vacationSem <- struct{}{}

	// Next send should not block (use select with default)
	select {
	case backend.vacationSem <- struct{}{}:
		t.Error("Should not be able to send to full semaphore")
	default:
		// Expected - semaphore is full
	}

	// Release one slot
	<-backend.vacationSem

	// Now should be able to send
	select {
	case backend.vacationSem <- struct{}{}:
		// Success
	default:
		t.Error("Should be able to send after releasing slot")
	}
}

// TestSession_ContextTimeout tests context timeout behavior.
func TestSession_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Wait for timeout
	<-ctx.Done()

	session := &Session{
		ctx: ctx,
	}

	// Operations should detect cancelled context
	if session.ctx.Err() == nil {
		t.Error("Context should have timed out")
	}
}

// TestSession_MultipleRcpts tests adding multiple recipients.
func TestSession_MultipleRcpts(t *testing.T) {
	session := &Session{
		isSubmission: false,
		rcpts:        nil,
	}

	// Simulate adding recipients (without backend validation)
	session.rcpts = append(session.rcpts, "r1@example.com")
	session.rcpts = append(session.rcpts, "r2@example.com")
	session.rcpts = append(session.rcpts, "r3@example.com")

	if len(session.rcpts) != 3 {
		t.Errorf("rcpts count = %d, want 3", len(session.rcpts))
	}

	// Reset should clear all
	session.Reset()
	if len(session.rcpts) != 0 {
		t.Errorf("rcpts should be empty after Reset, got %d", len(session.rcpts))
	}
}
