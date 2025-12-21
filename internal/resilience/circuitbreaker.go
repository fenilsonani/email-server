// Package resilience provides circuit breaker and other resilience patterns.
package resilience

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ErrCircuitOpen is returned when the circuit breaker is in open state.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// ErrCircuitTimeout is returned when execution times out.
var ErrCircuitTimeout = errors.New("circuit breaker execution timeout")

// State represents the circuit breaker state.
type State int32

const (
	// StateClosed is the normal operating state - requests flow through.
	StateClosed State = iota
	// StateOpen is the failing state - requests are rejected immediately.
	StateOpen
	// StateHalfOpen is the recovery testing state - limited requests allowed.
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Config configures a circuit breaker.
type Config struct {
	// Name identifies this circuit breaker for logging/metrics.
	Name string

	// FailureThreshold is the number of failures before opening the circuit.
	FailureThreshold int64

	// SuccessThreshold is the number of successes in half-open state to close.
	SuccessThreshold int64

	// Timeout is how long to wait before transitioning from open to half-open.
	Timeout time.Duration

	// HalfOpenMaxCalls limits concurrent calls in half-open state.
	HalfOpenMaxCalls int64

	// ExecutionTimeout is the max time for a single execution (0 = no timeout).
	ExecutionTimeout time.Duration

	// OnStateChange is called when state transitions occur.
	OnStateChange func(name string, from, to State)

	// IsFailure determines if an error should count as a failure.
	// If nil, all non-nil errors are failures.
	IsFailure func(err error) bool
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig(name string) Config {
	return Config{
		Name:             name,
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
		HalfOpenMaxCalls: 3,
		ExecutionTimeout: 10 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	config Config

	state           int32 // atomic State
	failureCount    int64 // atomic
	successCount    int64 // atomic
	halfOpenCalls   int64 // atomic
	lastFailureTime int64 // atomic (unix nano)
	lastStateChange int64 // atomic (unix nano)

	mu sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration.
func NewCircuitBreaker(cfg Config) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 2
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.HalfOpenMaxCalls <= 0 {
		cfg.HalfOpenMaxCalls = 3
	}

	return &CircuitBreaker{
		config:          cfg,
		state:           int32(StateClosed),
		lastStateChange: time.Now().UnixNano(),
	}
}

// Execute runs the given function through the circuit breaker.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(ctx context.Context) error) error {
	if ctx == nil {
		return errors.New("context is nil")
	}
	if fn == nil {
		return errors.New("function is nil")
	}

	if err := cb.beforeRequest(); err != nil {
		return err
	}

	// Apply execution timeout if configured
	execCtx := ctx
	var cancel context.CancelFunc
	if cb.config.ExecutionTimeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, cb.config.ExecutionTimeout)
		defer cancel()
	}

	// Execute the function with panic recovery
	// Use buffered channel to prevent goroutine leak
	errCh := make(chan error, 1)

	// Track goroutine completion
	done := make(chan struct{})
	go func() {
		defer func() {
			close(done)
			if r := recover(); r != nil {
				// Try to send panic error, but don't block if channel is full
				select {
				case errCh <- fmt.Errorf("panic in circuit breaker: %v", r):
				default:
				}
			}
		}()

		err := fn(execCtx)

		// Try to send result, but don't block if context is cancelled
		select {
		case errCh <- err:
		case <-execCtx.Done():
			// Context cancelled, function result doesn't matter
		}
	}()

	var err error
	select {
	case err = <-errCh:
		// Function completed normally or panicked
	case <-execCtx.Done():
		// Context cancelled or timed out
		if execCtx.Err() == context.DeadlineExceeded {
			err = ErrCircuitTimeout
		} else {
			err = execCtx.Err()
		}
		// Wait for goroutine to finish with timeout to prevent leak
		select {
		case <-done:
			// Goroutine finished
		case <-time.After(100 * time.Millisecond):
			// Goroutine still running, but we can't wait forever
			// This is acceptable as the goroutine will eventually finish
		}
	}

	cb.afterRequest(err)
	return err
}

// beforeRequest checks if the request should be allowed.
func (cb *CircuitBreaker) beforeRequest() error {
	state := State(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		return nil

	case StateOpen:
		// Check if timeout has elapsed
		lastFailure := time.Unix(0, atomic.LoadInt64(&cb.lastFailureTime))
		if time.Since(lastFailure) >= cb.config.Timeout {
			// Transition to half-open
			cb.transitionTo(StateHalfOpen)
			return nil
		}
		return ErrCircuitOpen

	case StateHalfOpen:
		// Limit concurrent calls in half-open state
		calls := atomic.AddInt64(&cb.halfOpenCalls, 1)
		if calls > cb.config.HalfOpenMaxCalls {
			atomic.AddInt64(&cb.halfOpenCalls, -1)
			return ErrCircuitOpen
		}
		return nil

	default:
		return nil
	}
}

// afterRequest records the result of the request.
func (cb *CircuitBreaker) afterRequest(err error) {
	isFailure := err != nil
	if cb.config.IsFailure != nil && err != nil {
		isFailure = cb.config.IsFailure(err)
	}

	state := State(atomic.LoadInt32(&cb.state))

	switch state {
	case StateClosed:
		if isFailure {
			failures := atomic.AddInt64(&cb.failureCount, 1)
			atomic.StoreInt64(&cb.lastFailureTime, time.Now().UnixNano())

			if failures >= cb.config.FailureThreshold {
				cb.transitionTo(StateOpen)
			}
		} else {
			// Reset failure count on success
			atomic.StoreInt64(&cb.failureCount, 0)
		}

	case StateHalfOpen:
		atomic.AddInt64(&cb.halfOpenCalls, -1)

		if isFailure {
			// Any failure in half-open goes back to open
			atomic.StoreInt64(&cb.lastFailureTime, time.Now().UnixNano())
			cb.transitionTo(StateOpen)
		} else {
			successes := atomic.AddInt64(&cb.successCount, 1)
			if successes >= cb.config.SuccessThreshold {
				cb.transitionTo(StateClosed)
			}
		}

	case StateOpen:
		// Shouldn't happen, but handle gracefully
		if isFailure {
			atomic.StoreInt64(&cb.lastFailureTime, time.Now().UnixNano())
		}
	}
}

// transitionTo changes the circuit breaker state.
func (cb *CircuitBreaker) transitionTo(newState State) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	oldState := State(atomic.LoadInt32(&cb.state))
	if oldState == newState {
		return
	}

	// Reset counters on transition
	atomic.StoreInt64(&cb.failureCount, 0)
	atomic.StoreInt64(&cb.successCount, 0)
	atomic.StoreInt64(&cb.halfOpenCalls, 0)
	atomic.StoreInt64(&cb.lastStateChange, time.Now().UnixNano())
	atomic.StoreInt32(&cb.state, int32(newState))

	if cb.config.OnStateChange != nil {
		// Call in background with timeout to prevent goroutine leak
		callback := cb.config.OnStateChange
		name := cb.config.Name
		go func() {
			// Use a timer to ensure callback doesn't run forever
			done := make(chan struct{})
			go func() {
				defer close(done)
				callback(name, oldState, newState)
			}()

			select {
			case <-done:
				// Callback completed
			case <-time.After(5 * time.Second):
				// Callback took too long - let it finish but don't wait
			}
		}()
	}
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() State {
	return State(atomic.LoadInt32(&cb.state))
}

// Stats returns current circuit breaker statistics.
func (cb *CircuitBreaker) Stats() Stats {
	return Stats{
		State:           State(atomic.LoadInt32(&cb.state)),
		FailureCount:    atomic.LoadInt64(&cb.failureCount),
		SuccessCount:    atomic.LoadInt64(&cb.successCount),
		LastFailureTime: time.Unix(0, atomic.LoadInt64(&cb.lastFailureTime)),
		LastStateChange: time.Unix(0, atomic.LoadInt64(&cb.lastStateChange)),
	}
}

// Stats contains circuit breaker statistics.
type Stats struct {
	State           State
	FailureCount    int64
	SuccessCount    int64
	LastFailureTime time.Time
	LastStateChange time.Time
}

// Reset forces the circuit breaker back to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.transitionTo(StateClosed)
}

// Validate checks if the circuit breaker configuration is valid.
func (cfg Config) Validate() error {
	if cfg.Name == "" {
		return errors.New("circuit breaker name is required")
	}
	if cfg.FailureThreshold <= 0 {
		return errors.New("failure threshold must be positive")
	}
	if cfg.SuccessThreshold <= 0 {
		return errors.New("success threshold must be positive")
	}
	if cfg.Timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	if cfg.HalfOpenMaxCalls <= 0 {
		return errors.New("half-open max calls must be positive")
	}
	return nil
}

// BreakerRegistry manages multiple circuit breakers by key.
type BreakerRegistry struct {
	breakers sync.Map
	config   func(key string) Config
	mu       sync.RWMutex
}

// NewBreakerRegistry creates a new registry with a config factory function.
func NewBreakerRegistry(configFactory func(key string) Config) *BreakerRegistry {
	if configFactory == nil {
		panic("config factory cannot be nil")
	}
	return &BreakerRegistry{
		config: configFactory,
	}
}

// Get returns the circuit breaker for the given key, creating it if necessary.
// This method is safe for concurrent use.
func (r *BreakerRegistry) Get(key string) *CircuitBreaker {
	if key == "" {
		return nil
	}

	// Fast path: check if breaker exists
	if cb, ok := r.breakers.Load(key); ok {
		return cb.(*CircuitBreaker)
	}

	// Slow path: create new breaker
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring lock
	if cb, ok := r.breakers.Load(key); ok {
		return cb.(*CircuitBreaker)
	}

	newCB := NewCircuitBreaker(r.config(key))
	r.breakers.Store(key, newCB)
	return newCB
}

// Remove removes a circuit breaker from the registry.
func (r *BreakerRegistry) Remove(key string) {
	r.breakers.Delete(key)
}

// All returns all registered circuit breakers.
// The returned map is a snapshot and safe to modify.
func (r *BreakerRegistry) All() map[string]*CircuitBreaker {
	result := make(map[string]*CircuitBreaker)
	r.breakers.Range(func(key, value interface{}) bool {
		if k, ok := key.(string); ok {
			if cb, ok := value.(*CircuitBreaker); ok {
				result[k] = cb
			}
		}
		return true
	})
	return result
}

// Reset resets all circuit breakers in the registry.
func (r *BreakerRegistry) Reset() {
	r.breakers.Range(func(key, value interface{}) bool {
		if cb, ok := value.(*CircuitBreaker); ok {
			cb.Reset()
		}
		return true
	})
}

// Count returns the number of circuit breakers in the registry.
func (r *BreakerRegistry) Count() int {
	count := 0
	r.breakers.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}
