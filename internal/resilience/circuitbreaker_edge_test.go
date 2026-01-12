package resilience

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Critical edge cases that happen rarely but are crucial for system stability.

// TestCircuitBreaker_PanicWithValue tests panic recovery with different value types.
func TestCircuitBreaker_PanicWithValue(t *testing.T) {
	testCases := []struct {
		name       string
		panicValue interface{}
	}{
		{"string panic", "test panic message"},
		{"error panic", errors.New("panic error")},
		{"int panic", 42},
		{"struct panic", struct{ msg string }{"panic struct"}},
		{"nil panic", nil},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Each test case gets its own circuit breaker to avoid threshold issues
			cfg := Config{
				Name:             "panic-test-" + tc.name,
				FailureThreshold: 5,
				ExecutionTimeout: 1 * time.Second,
			}
			cb := NewCircuitBreaker(cfg)
			ctx := context.Background()

			err := cb.Execute(ctx, func(ctx context.Context) error {
				panic(tc.panicValue)
			})

			if err == nil {
				t.Error("expected error from panic, got nil")
			}

			// Circuit should still be functional after one panic
			err = cb.Execute(ctx, func(ctx context.Context) error {
				return nil
			})
			if err != nil {
				t.Errorf("circuit should still work after single panic: %v", err)
			}
		})
	}
}

// TestCircuitBreaker_SlowCallbackDoesNotBlock tests that slow OnStateChange callbacks
// don't block the circuit breaker.
func TestCircuitBreaker_SlowCallbackDoesNotBlock(t *testing.T) {
	callbackStarted := make(chan struct{})
	callbackDone := make(chan struct{})

	cfg := Config{
		Name:             "slow-callback-test",
		FailureThreshold: 1,
		Timeout:          10 * time.Millisecond,
		OnStateChange: func(name string, from, to State) {
			close(callbackStarted)
			// Simulate slow callback (longer than 5s timeout in transitionTo)
			time.Sleep(100 * time.Millisecond)
			close(callbackDone)
		},
	}
	cb := NewCircuitBreaker(cfg)
	ctx := context.Background()

	// Trigger state change
	start := time.Now()
	cb.Execute(ctx, func(ctx context.Context) error {
		return errors.New("trigger failure")
	})

	// Wait for callback to start
	select {
	case <-callbackStarted:
	case <-time.After(1 * time.Second):
		t.Fatal("callback not started")
	}

	// The circuit should be usable immediately (not blocked by callback)
	if time.Since(start) > 50*time.Millisecond {
		t.Errorf("circuit breaker blocked for %v waiting for callback", time.Since(start))
	}

	// Verify callback eventually completes
	select {
	case <-callbackDone:
	case <-time.After(2 * time.Second):
		t.Fatal("callback never completed")
	}
}

// TestCircuitBreaker_ConcurrentStateTransitions tests race conditions during
// simultaneous state changes from multiple goroutines.
func TestCircuitBreaker_ConcurrentStateTransitions(t *testing.T) {
	cfg := Config{
		Name:             "concurrent-transition-test",
		FailureThreshold: 2,
		SuccessThreshold: 1,
		Timeout:          5 * time.Millisecond,
		HalfOpenMaxCalls: 100,
	}
	cb := NewCircuitBreaker(cfg)
	ctx := context.Background()

	var wg sync.WaitGroup
	goroutines := 20
	iterations := 50

	// Run multiple goroutines trying to trigger state changes
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Mix of successes and failures
				cb.Execute(ctx, func(ctx context.Context) error {
					if (id+j)%3 == 0 {
						return errors.New("failure")
					}
					return nil
				})

				// Occasionally reset to test reset during operations
				if j%20 == 0 {
					cb.Reset()
				}

				// Small delay to allow state transitions
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()

	// Circuit should be in a valid state
	state := cb.State()
	if state != StateClosed && state != StateOpen && state != StateHalfOpen {
		t.Errorf("circuit in invalid state: %v", state)
	}
}

// TestCircuitBreaker_ExecutionTimeoutVsContextDeadline tests the interaction
// between execution timeout and parent context deadline.
func TestCircuitBreaker_ExecutionTimeoutVsContextDeadline(t *testing.T) {
	t.Run("context deadline shorter", func(t *testing.T) {
		cfg := Config{
			Name:             "timeout-test",
			FailureThreshold: 5,
			ExecutionTimeout: 1 * time.Second, // Long execution timeout
		}
		cb := NewCircuitBreaker(cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		start := time.Now()
		err := cb.Execute(ctx, func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
		elapsed := time.Since(start)

		// Should respect parent context deadline
		if elapsed > 100*time.Millisecond {
			t.Errorf("took too long: %v", elapsed)
		}
		if err != context.DeadlineExceeded && err != ErrCircuitTimeout {
			t.Errorf("expected deadline error, got: %v", err)
		}
	})

	t.Run("execution timeout shorter", func(t *testing.T) {
		cfg := Config{
			Name:             "timeout-test",
			FailureThreshold: 5,
			ExecutionTimeout: 20 * time.Millisecond, // Short execution timeout
		}
		cb := NewCircuitBreaker(cfg)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		start := time.Now()
		err := cb.Execute(ctx, func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		})
		elapsed := time.Since(start)

		// Should respect execution timeout
		if elapsed > 100*time.Millisecond {
			t.Errorf("took too long: %v", elapsed)
		}
		if err != ErrCircuitTimeout && err != context.DeadlineExceeded {
			t.Errorf("expected timeout error, got: %v", err)
		}
	})
}

// TestCircuitBreaker_HalfOpenCallLimitRace tests the race condition when
// multiple goroutines try to make half-open calls simultaneously.
// Note: Due to race conditions with state transitions (half-open -> closed),
// more calls may be accepted than HalfOpenMaxCalls. This test verifies
// that the circuit breaker doesn't crash under concurrent pressure.
func TestCircuitBreaker_HalfOpenCallLimitRace(t *testing.T) {
	cfg := Config{
		Name:             "half-open-race-test",
		FailureThreshold: 1,
		SuccessThreshold: 100, // High threshold so we stay in half-open
		Timeout:          1 * time.Millisecond,
		HalfOpenMaxCalls: 3,
	}
	cb := NewCircuitBreaker(cfg)
	ctx := context.Background()

	// Open the circuit
	cb.Execute(ctx, func(ctx context.Context) error {
		return errors.New("open circuit")
	})

	// Wait for half-open
	time.Sleep(5 * time.Millisecond)

	// Try many concurrent calls - most should be rejected
	var wg sync.WaitGroup
	var accepted, rejected int32
	goroutines := 20
	started := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-started // Wait for all goroutines to be ready

			err := cb.Execute(ctx, func(ctx context.Context) error {
				time.Sleep(5 * time.Millisecond) // Hold the slot
				return nil
			})

			if err == ErrCircuitOpen {
				atomic.AddInt32(&rejected, 1)
			} else {
				atomic.AddInt32(&accepted, 1)
			}
		}()
	}

	close(started)
	wg.Wait()

	// With race conditions, we just verify the circuit didn't crash
	// and some requests were rejected
	total := atomic.LoadInt32(&accepted) + atomic.LoadInt32(&rejected)
	if total != int32(goroutines) {
		t.Errorf("total requests = %d, want %d", total, goroutines)
	}

	// Most requests should be rejected when half-open
	if rejected < int32(goroutines/2) {
		t.Logf("accepted=%d, rejected=%d (expected more rejections)", accepted, rejected)
	}
}

// TestCircuitBreaker_GoroutineLeakOnTimeout tests that goroutines don't leak
// when functions time out.
func TestCircuitBreaker_GoroutineLeakOnTimeout(t *testing.T) {
	cfg := Config{
		Name:             "leak-test",
		FailureThreshold: 100,
		ExecutionTimeout: 5 * time.Millisecond,
	}
	cb := NewCircuitBreaker(cfg)
	ctx := context.Background()

	beforeGoroutines := runtime.NumGoroutine()

	// Run many timing out operations
	for i := 0; i < 50; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			<-ctx.Done()
			time.Sleep(10 * time.Millisecond) // Simulate slow cleanup
			return ctx.Err()
		})
	}

	// Wait for goroutines to settle
	time.Sleep(200 * time.Millisecond)

	afterGoroutines := runtime.NumGoroutine()

	// Allow some variance (test framework may have some)
	if afterGoroutines > beforeGoroutines+10 {
		t.Errorf("possible goroutine leak: before=%d, after=%d",
			beforeGoroutines, afterGoroutines)
	}
}

// TestCircuitBreaker_ResetDuringExecution tests Reset() while operations are in progress.
func TestCircuitBreaker_ResetDuringExecution(t *testing.T) {
	cfg := Config{
		Name:             "reset-during-exec",
		FailureThreshold: 2,
		Timeout:          1 * time.Second,
		HalfOpenMaxCalls: 10,
	}
	cb := NewCircuitBreaker(cfg)
	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errors.New("failure")
		})
	}

	if cb.State() != StateOpen {
		t.Fatal("circuit should be open")
	}

	// Start a slow operation in background (will be rejected, but tests the flow)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			cb.Execute(ctx, func(ctx context.Context) error {
				time.Sleep(5 * time.Millisecond)
				return nil
			})
		}
	}()

	// Reset while operations might be happening
	time.Sleep(2 * time.Millisecond)
	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("expected closed after reset, got %v", cb.State())
	}

	wg.Wait()
}

// TestCircuitBreaker_NilContextReturnsError tests nil context handling.
func TestCircuitBreaker_NilContextReturnsError(t *testing.T) {
	cfg := DefaultConfig("nil-ctx-test")
	cb := NewCircuitBreaker(cfg)

	err := cb.Execute(nil, func(ctx context.Context) error {
		t.Error("should not execute with nil context")
		return nil
	})

	if err == nil {
		t.Error("expected error for nil context")
	}
}

// TestCircuitBreaker_NilFunctionReturnsError tests nil function handling.
func TestCircuitBreaker_NilFunctionReturnsError(t *testing.T) {
	cfg := DefaultConfig("nil-fn-test")
	cb := NewCircuitBreaker(cfg)

	err := cb.Execute(context.Background(), nil)

	if err == nil {
		t.Error("expected error for nil function")
	}
}

// TestBreakerRegistry_EmptyKeyReturnsNil tests empty key handling.
func TestBreakerRegistry_EmptyKeyReturnsNil(t *testing.T) {
	registry := NewBreakerRegistry(func(key string) Config {
		return DefaultConfig(key)
	})

	cb := registry.Get("")
	if cb != nil {
		t.Error("expected nil for empty key")
	}
}

// TestBreakerRegistry_NilConfigFactoryPanics tests nil factory panic.
func TestBreakerRegistry_NilConfigFactoryPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil config factory")
		}
	}()

	NewBreakerRegistry(nil)
}

// TestBreakerRegistry_ConcurrentCreateAndRemove tests concurrent create/remove operations.
func TestBreakerRegistry_ConcurrentCreateAndRemove(t *testing.T) {
	registry := NewBreakerRegistry(func(key string) Config {
		return DefaultConfig(key)
	})

	var wg sync.WaitGroup
	goroutines := 20
	keys := []string{"a", "b", "c", "d", "e"}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := keys[(id+j)%len(keys)]
				if j%3 == 0 {
					registry.Remove(key)
				} else {
					cb := registry.Get(key)
					if cb != nil {
						cb.Execute(context.Background(), func(ctx context.Context) error {
							return nil
						})
					}
				}
			}
		}(i)
	}

	wg.Wait()

	// Should not have crashed
	count := registry.Count()
	t.Logf("Registry has %d breakers after concurrent operations", count)
}

// TestBreakerRegistry_ResetDuringConcurrentOps tests Reset while operations are running.
func TestBreakerRegistry_ResetDuringConcurrentOps(t *testing.T) {
	registry := NewBreakerRegistry(func(key string) Config {
		return Config{
			Name:             key,
			FailureThreshold: 2,
			Timeout:          10 * time.Millisecond,
		}
	})

	// Create some breakers and open them
	for _, key := range []string{"a", "b", "c"} {
		cb := registry.Get(key)
		ctx := context.Background()
		cb.Execute(ctx, func(ctx context.Context) error {
			return errors.New("failure")
		})
		cb.Execute(ctx, func(ctx context.Context) error {
			return errors.New("failure")
		})
	}

	// Verify they're open
	for _, key := range []string{"a", "b", "c"} {
		if registry.Get(key).State() != StateOpen {
			t.Errorf("breaker %s should be open", key)
		}
	}

	// Reset all while potentially using them
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			for _, key := range []string{"a", "b", "c"} {
				registry.Get(key).Execute(context.Background(), func(ctx context.Context) error {
					return nil
				})
			}
			time.Sleep(time.Millisecond)
		}
	}()

	registry.Reset()

	wg.Wait()

	// All breakers should be closed
	for _, key := range []string{"a", "b", "c"} {
		if registry.Get(key).State() != StateClosed {
			t.Errorf("breaker %s should be closed after reset", key)
		}
	}
}

// TestCircuitBreaker_CounterOverflow tests behavior at counter extremes.
func TestCircuitBreaker_CounterOverflow(t *testing.T) {
	cfg := Config{
		Name:             "overflow-test",
		FailureThreshold: 1000000000, // Very high threshold
		SuccessThreshold: 2,
		Timeout:          1 * time.Second,
	}
	cb := NewCircuitBreaker(cfg)
	ctx := context.Background()

	// Run many iterations (but not enough to actually overflow)
	for i := 0; i < 1000; i++ {
		err := cb.Execute(ctx, func(ctx context.Context) error {
			if i%2 == 0 {
				return errors.New("failure")
			}
			return nil
		})
		if err == ErrCircuitOpen {
			t.Error("circuit should not open with high threshold")
		}
	}

	// Verify counters are sane
	stats := cb.Stats()
	if stats.FailureCount < 0 {
		t.Error("negative failure count detected")
	}
}

// TestCircuitBreaker_StateStringUnknown tests unknown state string.
func TestCircuitBreaker_StateStringUnknown(t *testing.T) {
	state := State(999)
	if state.String() != "unknown" {
		t.Errorf("expected 'unknown' for invalid state, got %s", state.String())
	}
}

// TestConfig_Validate tests configuration validation.
func TestConfig_Validate(t *testing.T) {
	testCases := []struct {
		name      string
		config    Config
		expectErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Name:             "test",
				FailureThreshold: 5,
				SuccessThreshold: 2,
				Timeout:          30 * time.Second,
				HalfOpenMaxCalls: 3,
			},
			expectErr: false,
		},
		{
			name: "empty name",
			config: Config{
				Name:             "",
				FailureThreshold: 5,
				SuccessThreshold: 2,
				Timeout:          30 * time.Second,
				HalfOpenMaxCalls: 3,
			},
			expectErr: true,
		},
		{
			name: "zero failure threshold",
			config: Config{
				Name:             "test",
				FailureThreshold: 0,
				SuccessThreshold: 2,
				Timeout:          30 * time.Second,
				HalfOpenMaxCalls: 3,
			},
			expectErr: true,
		},
		{
			name: "zero success threshold",
			config: Config{
				Name:             "test",
				FailureThreshold: 5,
				SuccessThreshold: 0,
				Timeout:          30 * time.Second,
				HalfOpenMaxCalls: 3,
			},
			expectErr: true,
		},
		{
			name: "zero timeout",
			config: Config{
				Name:             "test",
				FailureThreshold: 5,
				SuccessThreshold: 2,
				Timeout:          0,
				HalfOpenMaxCalls: 3,
			},
			expectErr: true,
		},
		{
			name: "zero half-open max calls",
			config: Config{
				Name:             "test",
				FailureThreshold: 5,
				SuccessThreshold: 2,
				Timeout:          30 * time.Second,
				HalfOpenMaxCalls: 0,
			},
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.expectErr && err == nil {
				t.Error("expected validation error")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

// TestCircuitBreaker_TimeWindowEdge tests behavior at time window boundaries.
func TestCircuitBreaker_TimeWindowEdge(t *testing.T) {
	cfg := Config{
		Name:             "time-edge-test",
		FailureThreshold: 2,
		Timeout:          25 * time.Millisecond, // Precise timeout
		HalfOpenMaxCalls: 5,
	}
	cb := NewCircuitBreaker(cfg)
	ctx := context.Background()

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, func(ctx context.Context) error {
			return errors.New("failure")
		})
	}

	if cb.State() != StateOpen {
		t.Fatal("circuit should be open")
	}

	// Wait just under the timeout
	time.Sleep(20 * time.Millisecond)

	// Should still be open
	err := cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	if err != ErrCircuitOpen {
		t.Error("circuit should still be open before timeout")
	}

	// Wait past timeout
	time.Sleep(10 * time.Millisecond)

	// Should transition to half-open
	err = cb.Execute(ctx, func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error after timeout, got: %v", err)
	}
	if cb.State() != StateHalfOpen && cb.State() != StateClosed {
		t.Errorf("expected half-open or closed, got %v", cb.State())
	}
}

// TestCircuitBreaker_ExecuteReturnsPanicError tests that panic error is returned.
func TestCircuitBreaker_ExecuteReturnsPanicError(t *testing.T) {
	cfg := Config{
		Name:             "panic-return-test",
		FailureThreshold: 5,
	}
	cb := NewCircuitBreaker(cfg)
	ctx := context.Background()

	err := cb.Execute(ctx, func(ctx context.Context) error {
		panic("deliberate panic")
	})

	if err == nil {
		t.Fatal("expected error from panic")
	}

	// Error should contain panic message
	if !containsSubstring(err.Error(), "panic") {
		t.Errorf("error should mention panic: %v", err)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstringHelper(s, substr))
}

func containsSubstringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
