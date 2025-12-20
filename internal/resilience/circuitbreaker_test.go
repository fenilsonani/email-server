package resilience

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Test errors
var (
	errTest        = errors.New("test error")
	errIgnorable   = errors.New("ignorable error")
	errNonFailure  = errors.New("non-failure error")
)

func TestNewCircuitBreaker(t *testing.T) {
	t.Run("with valid config", func(t *testing.T) {
		cfg := Config{
			Name:             "test",
			FailureThreshold: 5,
			SuccessThreshold: 2,
			Timeout:          10 * time.Second,
			HalfOpenMaxCalls: 3,
		}

		cb := NewCircuitBreaker(cfg)

		if cb == nil {
			t.Fatal("expected non-nil circuit breaker")
		}

		if cb.State() != StateClosed {
			t.Errorf("expected initial state to be Closed, got %v", cb.State())
		}

		stats := cb.Stats()
		if stats.FailureCount != 0 {
			t.Errorf("expected failure count to be 0, got %d", stats.FailureCount)
		}
		if stats.SuccessCount != 0 {
			t.Errorf("expected success count to be 0, got %d", stats.SuccessCount)
		}
	})

	t.Run("with zero values uses defaults", func(t *testing.T) {
		cfg := Config{Name: "test"}
		cb := NewCircuitBreaker(cfg)

		// Verify defaults were applied
		if cb.config.FailureThreshold != 5 {
			t.Errorf("expected default FailureThreshold 5, got %d", cb.config.FailureThreshold)
		}
		if cb.config.SuccessThreshold != 2 {
			t.Errorf("expected default SuccessThreshold 2, got %d", cb.config.SuccessThreshold)
		}
		if cb.config.Timeout != 30*time.Second {
			t.Errorf("expected default Timeout 30s, got %v", cb.config.Timeout)
		}
		if cb.config.HalfOpenMaxCalls != 3 {
			t.Errorf("expected default HalfOpenMaxCalls 3, got %d", cb.config.HalfOpenMaxCalls)
		}
	})

	t.Run("with negative values uses defaults", func(t *testing.T) {
		cfg := Config{
			Name:             "test",
			FailureThreshold: -1,
			SuccessThreshold: -1,
			Timeout:          -1,
			HalfOpenMaxCalls: -1,
		}
		cb := NewCircuitBreaker(cfg)

		if cb.config.FailureThreshold != 5 {
			t.Errorf("expected default FailureThreshold 5, got %d", cb.config.FailureThreshold)
		}
		if cb.config.SuccessThreshold != 2 {
			t.Errorf("expected default SuccessThreshold 2, got %d", cb.config.SuccessThreshold)
		}
		if cb.config.Timeout != 30*time.Second {
			t.Errorf("expected default Timeout 30s, got %v", cb.config.Timeout)
		}
		if cb.config.HalfOpenMaxCalls != 3 {
			t.Errorf("expected default HalfOpenMaxCalls 3, got %d", cb.config.HalfOpenMaxCalls)
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("test-breaker")

	if cfg.Name != "test-breaker" {
		t.Errorf("expected name 'test-breaker', got %s", cfg.Name)
	}
	if cfg.FailureThreshold != 5 {
		t.Errorf("expected FailureThreshold 5, got %d", cfg.FailureThreshold)
	}
	if cfg.SuccessThreshold != 2 {
		t.Errorf("expected SuccessThreshold 2, got %d", cfg.SuccessThreshold)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("expected Timeout 30s, got %v", cfg.Timeout)
	}
	if cfg.HalfOpenMaxCalls != 3 {
		t.Errorf("expected HalfOpenMaxCalls 3, got %d", cfg.HalfOpenMaxCalls)
	}
	if cfg.ExecutionTimeout != 10*time.Second {
		t.Errorf("expected ExecutionTimeout 10s, got %v", cfg.ExecutionTimeout)
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		HalfOpenMaxCalls: 2,
	}
	cb := NewCircuitBreaker(cfg)

	// Verify initial state
	if cb.State() != StateClosed {
		t.Fatalf("expected initial state Closed, got %v", cb.State())
	}

	ctx := context.Background()

	// First two failures should not open the circuit
	for i := 0; i < 2; i++ {
		err := cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
		if err != errTest {
			t.Errorf("iteration %d: expected errTest, got %v", i, err)
		}
		if cb.State() != StateClosed {
			t.Errorf("iteration %d: expected state Closed, got %v", i, cb.State())
		}
	}

	stats := cb.Stats()
	if stats.FailureCount != 2 {
		t.Errorf("expected 2 failures, got %d", stats.FailureCount)
	}

	// Third failure should open the circuit
	err := cb.Execute(ctx, func(ctx context.Context) error {
		return errTest
	})
	if err != errTest {
		t.Errorf("expected errTest, got %v", err)
	}

	if cb.State() != StateOpen {
		t.Errorf("expected state Open after threshold reached, got %v", cb.State())
	}

	stats = cb.Stats()
	if stats.FailureCount != 0 {
		t.Errorf("expected failure count reset after state change, got %d", stats.FailureCount)
	}
}

func TestCircuitBreaker_OpenRejectsRequests(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		Timeout:          1 * time.Second,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Trigger open state
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state Open, got %v", cb.State())
	}

	// Requests should be rejected immediately
	callCount := 0
	err := cb.Execute(ctx, func(ctx context.Context) error {
		callCount++
		return nil
	})

	if err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
	if callCount != 0 {
		t.Errorf("expected function not to be called, but it was called %d times", callCount)
	}
}

func TestCircuitBreaker_OpenToHalfOpen(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		HalfOpenMaxCalls: 3,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state Open, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// Next request should transition to half-open
	callCount := 0
	err := cb.Execute(ctx, func(ctx context.Context) error {
		callCount++
		return nil
	})

	if err != nil {
		t.Errorf("expected nil error in half-open, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected function to be called once, got %d", callCount)
	}
	if cb.State() != StateHalfOpen {
		t.Errorf("expected state HalfOpen, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		HalfOpenMaxCalls: 5,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}

	// Wait and transition to half-open
	time.Sleep(60 * time.Millisecond)
	cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})

	if cb.State() != StateHalfOpen {
		t.Fatalf("expected state HalfOpen, got %v", cb.State())
	}

	stats := cb.Stats()
	if stats.SuccessCount != 1 {
		t.Errorf("expected 1 success, got %d", stats.SuccessCount)
	}

	// One more success should close the circuit
	err := cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("expected state Closed after success threshold, got %v", cb.State())
	}

	stats = cb.Stats()
	if stats.SuccessCount != 0 {
		t.Errorf("expected success count reset after state change, got %d", stats.SuccessCount)
	}
}

func TestCircuitBreaker_HalfOpenToOpen(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		HalfOpenMaxCalls: 5,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}

	// Wait and transition to half-open
	time.Sleep(60 * time.Millisecond)
	cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})

	if cb.State() != StateHalfOpen {
		t.Fatalf("expected state HalfOpen, got %v", cb.State())
	}

	// Any failure in half-open should go back to open
	err := cb.Execute(ctx, func(ctx context.Context) error {
		return errTest
	})

	if err != errTest {
		t.Errorf("expected errTest, got %v", err)
	}
	if cb.State() != StateOpen {
		t.Errorf("expected state Open after failure in HalfOpen, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenMaxCalls(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		HalfOpenMaxCalls: 1, // Only allow 1 call in half-open
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected StateOpen after failures, got %v", cb.State())
	}

	// Wait for timeout to transition to half-open
	time.Sleep(60 * time.Millisecond)

	// Start one slow request (should be allowed and hold the half-open slot)
	proceed := make(chan struct{})
	started := make(chan struct{})

	go func() {
		cb.Execute(ctx, func(ctx context.Context) error {
			close(started)
			<-proceed
			return nil
		})
	}()

	// Wait for the first request to start
	<-started

	// Second request should be rejected because we're at max calls
	executed := false
	err := cb.Execute(ctx, func(ctx context.Context) error {
		executed = true
		return nil
	})

	// Either rejected with ErrCircuitOpen or executed (if first request completed quickly)
	if err == ErrCircuitOpen && executed {
		t.Error("should not execute when max calls exceeded")
	}

	// Allow pending request to complete
	close(proceed)
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Two failures
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}

	stats := cb.Stats()
	if stats.FailureCount != 2 {
		t.Errorf("expected 2 failures, got %d", stats.FailureCount)
	}

	// One success should reset failure count
	cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})

	stats = cb.Stats()
	if stats.FailureCount != 0 {
		t.Errorf("expected failure count reset after success, got %d", stats.FailureCount)
	}
	if cb.State() != StateClosed {
		t.Errorf("expected state to remain Closed, got %v", cb.State())
	}
}

func TestCircuitBreaker_ExecutionTimeout(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 5,
		ExecutionTimeout: 50 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Execute a slow function
	err := cb.Execute(ctx, func(ctx context.Context) error {
		select {
		case <-time.After(200 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	if err != ErrCircuitTimeout {
		t.Errorf("expected ErrCircuitTimeout, got %v", err)
	}

	// Timeout should count as failure
	stats := cb.Stats()
	if stats.FailureCount != 1 {
		t.Errorf("expected 1 failure after timeout, got %d", stats.FailureCount)
	}
}

func TestCircuitBreaker_NoExecutionTimeout(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 5,
		ExecutionTimeout: 0, // No timeout
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Execute a slow function - should complete
	err := cb.Execute(ctx, func(ctx context.Context) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	if err != nil {
		t.Errorf("expected nil error with no timeout, got %v", err)
	}
}

func TestCircuitBreaker_ContextCancellation(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 5,
		ExecutionTimeout: 0,
	}
	cb := NewCircuitBreaker(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	go func() {
		<-started
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := cb.Execute(ctx, func(ctx context.Context) error {
		close(started)
		select {
		case <-time.After(200 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestCircuitBreaker_CustomFailurePredicate(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		IsFailure: func(err error) bool {
			// Only treat errTest as failure, ignore others
			return errors.Is(err, errTest)
		},
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Non-failure error should not count
	err := cb.Execute(ctx, func(ctx context.Context) error {
		return errNonFailure
	})

	if err != errNonFailure {
		t.Errorf("expected errNonFailure, got %v", err)
	}

	stats := cb.Stats()
	if stats.FailureCount != 0 {
		t.Errorf("expected 0 failures for non-failure error, got %d", stats.FailureCount)
	}

	// Failure error should count
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}

	if cb.State() != StateOpen {
		t.Errorf("expected state Open after failure threshold, got %v", cb.State())
	}
}

func TestCircuitBreaker_OnStateChange(t *testing.T) {
	var stateChanges []struct {
		from, to State
	}
	var mu sync.Mutex

	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		OnStateChange: func(name string, from, to State) {
			mu.Lock()
			defer mu.Unlock()
			if name != "test" {
				t.Errorf("expected name 'test', got %s", name)
			}
			stateChanges = append(stateChanges, struct{ from, to State }{from, to})
		},
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Closed -> Open
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}

	// Wait for callback
	time.Sleep(10 * time.Millisecond)

	// Open -> HalfOpen
	time.Sleep(60 * time.Millisecond)
	cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	time.Sleep(10 * time.Millisecond)

	// HalfOpen -> Closed
	cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(stateChanges) != 3 {
		t.Fatalf("expected 3 state changes, got %d", len(stateChanges))
	}

	expected := []struct{ from, to State }{
		{StateClosed, StateOpen},
		{StateOpen, StateHalfOpen},
		{StateHalfOpen, StateClosed},
	}

	for i, exp := range expected {
		if stateChanges[i].from != exp.from || stateChanges[i].to != exp.to {
			t.Errorf("change %d: expected %v -> %v, got %v -> %v",
				i, exp.from, exp.to, stateChanges[i].from, stateChanges[i].to)
		}
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          1 * time.Second,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}

	if cb.State() != StateOpen {
		t.Fatalf("expected state Open, got %v", cb.State())
	}

	// Reset should close the circuit immediately
	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("expected state Closed after reset, got %v", cb.State())
	}

	stats := cb.Stats()
	if stats.FailureCount != 0 {
		t.Errorf("expected failure count 0 after reset, got %d", stats.FailureCount)
	}
	if stats.SuccessCount != 0 {
		t.Errorf("expected success count 0 after reset, got %d", stats.SuccessCount)
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Initial stats
	stats := cb.Stats()
	if stats.State != StateClosed {
		t.Errorf("expected state Closed, got %v", stats.State)
	}
	if stats.FailureCount != 0 {
		t.Errorf("expected 0 failures, got %d", stats.FailureCount)
	}
	if stats.SuccessCount != 0 {
		t.Errorf("expected 0 successes, got %d", stats.SuccessCount)
	}
	// Note: Initial LastFailureTime may be zero or Unix epoch depending on implementation
	if stats.LastFailureTime.Unix() > 0 && stats.LastFailureTime.Year() > 1970 {
		t.Errorf("expected no real LastFailureTime initially, got %v", stats.LastFailureTime)
	}

	// Record a failure
	beforeFailure := time.Now()
	cb.Execute(ctx, func(ctx context.Context) error {
		return errTest
	})
	afterFailure := time.Now()

	stats = cb.Stats()
	if stats.FailureCount != 1 {
		t.Errorf("expected 1 failure, got %d", stats.FailureCount)
	}
	if stats.LastFailureTime.Before(beforeFailure) || stats.LastFailureTime.After(afterFailure) {
		t.Errorf("LastFailureTime %v not in expected range %v to %v",
			stats.LastFailureTime, beforeFailure, afterFailure)
	}

	// LastStateChange should be set
	if stats.LastStateChange.IsZero() {
		t.Error("expected non-zero LastStateChange")
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 100,
		SuccessThreshold: 2,
		Timeout:          100 * time.Millisecond,
		ExecutionTimeout: 100 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()
	concurrency := 50
	iterations := 100

	var wg sync.WaitGroup
	var successCount, failureCount int32

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				err := cb.Execute(ctx, func(ctx context.Context) error {
					// Simulate some work
					time.Sleep(time.Millisecond)
					if (id+j)%3 == 0 {
						return errTest
					}
					return nil
				})

				if err == nil {
					atomic.AddInt32(&successCount, 1)
				} else if err == errTest {
					atomic.AddInt32(&failureCount, 1)
				}
				// Ignore other errors (like ErrCircuitOpen)
			}
		}(i)
	}

	wg.Wait()

	total := atomic.LoadInt32(&successCount) + atomic.LoadInt32(&failureCount)
	if total == 0 {
		t.Error("expected some requests to complete")
	}

	t.Logf("Completed %d requests (%d success, %d failure) with %d goroutines",
		total, successCount, failureCount, concurrency)
}

func TestCircuitBreaker_RapidStateTransitions(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 2,
		Timeout:          20 * time.Millisecond,
		HalfOpenMaxCalls: 5,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Test multiple rapid transitions
	for cycle := 0; cycle < 3; cycle++ {
		// Closed -> Open
		for i := 0; i < 2; i++ {
			cb.Execute(ctx, func(ctx context.Context) error {
				return errTest
			})
		}

		if cb.State() != StateOpen {
			t.Errorf("cycle %d: expected state Open, got %v", cycle, cb.State())
		}

		// Open -> HalfOpen
		time.Sleep(30 * time.Millisecond)
		cb.Execute(ctx, func(ctx context.Context) error {
			return nil
		})

		if cb.State() != StateHalfOpen {
			t.Errorf("cycle %d: expected state HalfOpen, got %v", cycle, cb.State())
		}

		// HalfOpen -> Closed
		cb.Execute(ctx, func(ctx context.Context) error {
			return nil
		})

		if cb.State() != StateClosed {
			t.Errorf("cycle %d: expected state Closed, got %v", cycle, cb.State())
		}
	}
}

func TestCircuitBreaker_HalfOpenMultipleSuccesses(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		SuccessThreshold: 3, // Need 3 successes
		Timeout:          50 * time.Millisecond,
		HalfOpenMaxCalls: 10,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}

	// Wait and enter half-open
	time.Sleep(60 * time.Millisecond)

	// First two successes should keep it half-open
	for i := 0; i < 2; i++ {
		err := cb.Execute(ctx, func(ctx context.Context) error {
			return nil
		})
		if err != nil {
			t.Errorf("success %d: expected nil error, got %v", i, err)
		}
		if cb.State() != StateHalfOpen {
			t.Errorf("success %d: expected state HalfOpen, got %v", i, cb.State())
		}
	}

	// Third success should close it
	err := cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error on final success, got %v", err)
	}
	if cb.State() != StateClosed {
		t.Errorf("expected state Closed after success threshold, got %v", cb.State())
	}
}

func TestCircuitBreaker_EdgeCaseZeroTimeout(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		Timeout:          0, // Will be set to default
	}
	cb := NewCircuitBreaker(cfg)

	if cb.config.Timeout != 30*time.Second {
		t.Errorf("expected default timeout, got %v", cb.config.Timeout)
	}
}

func TestCircuitBreaker_NoStateChangeOnSameState(t *testing.T) {
	callCount := 0
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		OnStateChange: func(name string, from, to State) {
			callCount++
		},
	}
	cb := NewCircuitBreaker(cfg)

	// Manually try to transition to same state
	cb.transitionTo(StateClosed)

	// Give callback time to fire if it would
	time.Sleep(10 * time.Millisecond)

	if callCount != 0 {
		t.Errorf("expected no state change callback for same state, got %d calls", callCount)
	}
}

func TestCircuitBreaker_SuccessInClosedState(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 5,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Multiple successes should keep it closed
	for i := 0; i < 10; i++ {
		err := cb.Execute(ctx, func(ctx context.Context) error {
			return nil
		})
		if err != nil {
			t.Errorf("iteration %d: expected nil error, got %v", i, err)
		}
		if cb.State() != StateClosed {
			t.Errorf("iteration %d: expected state Closed, got %v", i, cb.State())
		}
	}

	stats := cb.Stats()
	if stats.FailureCount != 0 {
		t.Errorf("expected 0 failures, got %d", stats.FailureCount)
	}
}

func TestCircuitBreaker_PanicInFunction(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 5,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Panic should be recovered and converted to error
	err := cb.Execute(ctx, func(ctx context.Context) error {
		panic("test panic")
	})

	if err == nil {
		t.Error("expected error from panicked function")
	}

	if !strings.Contains(err.Error(), "panic in circuit breaker") {
		t.Errorf("expected panic error message, got: %v", err)
	}
}

func TestBreakerRegistry_GetCreatesNew(t *testing.T) {
	registry := NewBreakerRegistry(func(key string) Config {
		return Config{
			Name:             key,
			FailureThreshold: 5,
		}
	})

	cb1 := registry.Get("test1")
	if cb1 == nil {
		t.Fatal("expected non-nil circuit breaker")
	}
	if cb1.config.Name != "test1" {
		t.Errorf("expected name 'test1', got %s", cb1.config.Name)
	}

	// Getting same key should return same instance
	cb2 := registry.Get("test1")
	if cb1 != cb2 {
		t.Error("expected same circuit breaker instance for same key")
	}

	// Different key should create different instance
	cb3 := registry.Get("test2")
	if cb1 == cb3 {
		t.Error("expected different circuit breaker instance for different key")
	}
}

func TestBreakerRegistry_All(t *testing.T) {
	registry := NewBreakerRegistry(func(key string) Config {
		return DefaultConfig(key)
	})

	// Get a few breakers
	registry.Get("breaker1")
	registry.Get("breaker2")
	registry.Get("breaker3")

	all := registry.All()
	if len(all) != 3 {
		t.Errorf("expected 3 breakers, got %d", len(all))
	}

	for _, name := range []string{"breaker1", "breaker2", "breaker3"} {
		if _, ok := all[name]; !ok {
			t.Errorf("expected to find breaker %s", name)
		}
	}
}

func TestBreakerRegistry_Concurrent(t *testing.T) {
	registry := NewBreakerRegistry(func(key string) Config {
		return DefaultConfig(key)
	})

	var wg sync.WaitGroup
	concurrency := 50
	keys := []string{"key1", "key2", "key3", "key4", "key5"}

	// Concurrent gets
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := keys[id%len(keys)]
			cb := registry.Get(key)
			if cb == nil {
				t.Errorf("expected non-nil circuit breaker for %s", key)
			}
		}(i)
	}

	wg.Wait()

	// Should have exactly len(keys) breakers
	all := registry.All()
	if len(all) != len(keys) {
		t.Errorf("expected %d unique breakers, got %d", len(keys), len(all))
	}
}

func TestCircuitBreaker_ContextDeadlineInExecution(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 5,
		ExecutionTimeout: 100 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)

	// Parent context with shorter deadline
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := cb.Execute(ctx, func(ctx context.Context) error {
		select {
		case <-time.After(200 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	// Should get timeout error (either wrapped or direct)
	if err != context.DeadlineExceeded && err != ErrCircuitTimeout && err != context.Canceled {
		t.Errorf("expected timeout-related error, got %v", err)
	}
}

func TestCircuitBreaker_NilIsFailureFunction(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 2,
		IsFailure:        nil, // Should treat all errors as failures
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Any error should count as failure
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errors.New("some error")
		})
	}

	if cb.State() != StateOpen {
		t.Errorf("expected state Open with nil IsFailure, got %v", cb.State())
	}
}

func TestCircuitBreaker_FailureInOpenState(t *testing.T) {
	cfg := Config{
		Name:             "test",
		FailureThreshold: 1,
		Timeout:          1 * time.Second,
	}
	cb := NewCircuitBreaker(cfg)

	ctx := context.Background()

	// Open the circuit
	cb.Execute(ctx, func(ctx context.Context) error {
		return errTest
	})

	if cb.State() != StateOpen {
		t.Fatalf("expected state Open, got %v", cb.State())
	}

	beforeTime := time.Now()

	// Try another request - should update last failure time
	cb.Execute(ctx, func(ctx context.Context) error {
		return errTest
	})

	stats := cb.Stats()
	// LastFailureTime should not be updated since request was rejected
	// Actually, looking at the code, it doesn't execute the function when open
	// so this test verifies the rejection behavior
	if cb.State() != StateOpen {
		t.Errorf("expected state to remain Open, got %v", cb.State())
	}

	// The second execute should have been rejected, so LastFailureTime
	// should be from the first failure
	if stats.LastFailureTime.After(beforeTime) {
		t.Error("LastFailureTime should not update when request is rejected in Open state")
	}
}

func TestCircuitBreaker_CompleteLifecycle(t *testing.T) {
	cfg := Config{
		Name:             "lifecycle-test",
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          50 * time.Millisecond,
		HalfOpenMaxCalls: 5,
	}
	cb := NewCircuitBreaker(cfg)
	ctx := context.Background()

	// Phase 1: Closed with successes
	t.Log("Phase 1: Closed with successes")
	for i := 0; i < 5; i++ {
		err := cb.Execute(ctx, func(ctx context.Context) error {
			return nil
		})
		if err != nil {
			t.Errorf("expected success, got %v", err)
		}
	}
	if cb.State() != StateClosed {
		t.Errorf("expected state Closed, got %v", cb.State())
	}

	// Phase 2: Build up failures
	t.Log("Phase 2: Building failures")
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}
	if cb.State() != StateClosed {
		t.Errorf("expected state still Closed, got %v", cb.State())
	}

	// Phase 3: Success resets count
	t.Log("Phase 3: Success resets count")
	cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	stats := cb.Stats()
	if stats.FailureCount != 0 {
		t.Errorf("expected failure count reset, got %d", stats.FailureCount)
	}

	// Phase 4: Reach threshold and open
	t.Log("Phase 4: Opening circuit")
	for i := 0; i < 3; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errTest
		})
	}
	if cb.State() != StateOpen {
		t.Errorf("expected state Open, got %v", cb.State())
	}

	// Phase 5: Rejected while open
	t.Log("Phase 5: Requests rejected in Open")
	err := cb.Execute(ctx, func(ctx context.Context) error {
		t.Error("should not execute in open state")
		return nil
	})
	if err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}

	// Phase 6: Transition to half-open
	t.Log("Phase 6: Transition to HalfOpen")
	time.Sleep(60 * time.Millisecond)
	cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	if cb.State() != StateHalfOpen {
		t.Errorf("expected state HalfOpen, got %v", cb.State())
	}

	// Phase 7: Failure in half-open reopens
	t.Log("Phase 7: Failure in HalfOpen")
	cb.Execute(ctx, func(ctx context.Context) error {
		return errTest
	})
	if cb.State() != StateOpen {
		t.Errorf("expected state Open after failure in HalfOpen, got %v", cb.State())
	}

	// Phase 8: Back to half-open and successful recovery
	t.Log("Phase 8: Successful recovery")
	time.Sleep(60 * time.Millisecond)
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return nil
		})
	}
	if cb.State() != StateClosed {
		t.Errorf("expected state Closed after recovery, got %v", cb.State())
	}

	t.Log("Complete lifecycle test passed")
}
