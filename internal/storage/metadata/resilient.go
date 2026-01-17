package metadata

import (
	"context"
	"database/sql"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/resilience"
)

// ResilientConfig configures the resilient store wrapper
type ResilientConfig struct {
	// QueryTimeout is the maximum time for a single query (default: 30s)
	QueryTimeout time.Duration

	// SlowQueryThreshold is the duration above which queries are logged as slow (default: 1s)
	SlowQueryThreshold time.Duration

	// CircuitBreakerEnabled enables circuit breaker protection (default: true)
	CircuitBreakerEnabled bool

	// CircuitBreakerConfig configures the circuit breaker (optional, uses defaults if nil)
	CircuitBreakerConfig *resilience.Config
}

// DefaultResilientConfig returns sensible defaults
func DefaultResilientConfig() ResilientConfig {
	return ResilientConfig{
		QueryTimeout:          30 * time.Second,
		SlowQueryThreshold:    1 * time.Second,
		CircuitBreakerEnabled: true,
	}
}

// ResilientStore wraps a SQLiteDB with circuit breaker protection,
// query timeouts, and slow query logging
type ResilientStore struct {
	inner         *SQLiteDB
	breaker       *resilience.CircuitBreaker
	config        ResilientConfig
	logger        *logging.Logger
	metricsUpdate time.Time
}

// NewResilientStore creates a new resilient store wrapper
func NewResilientStore(db *SQLiteDB, cfg ResilientConfig, logger *logging.Logger) *ResilientStore {
	// Apply defaults
	if cfg.QueryTimeout == 0 {
		cfg.QueryTimeout = 30 * time.Second
	}
	if cfg.SlowQueryThreshold == 0 {
		cfg.SlowQueryThreshold = 1 * time.Second
	}

	rs := &ResilientStore{
		inner:  db,
		config: cfg,
		logger: logger,
	}

	// Create circuit breaker if enabled
	if cfg.CircuitBreakerEnabled {
		var cbConfig resilience.Config
		if cfg.CircuitBreakerConfig != nil {
			cbConfig = *cfg.CircuitBreakerConfig
		} else {
			cbConfig = resilience.DefaultConfig("database")
			cbConfig.ExecutionTimeout = cfg.QueryTimeout
			cbConfig.OnStateChange = rs.onCircuitStateChange
		}
		rs.breaker = resilience.NewCircuitBreaker(cbConfig)
	}

	return rs
}

// onCircuitStateChange handles circuit breaker state transitions
func (rs *ResilientStore) onCircuitStateChange(name string, from, to resilience.State) {
	// Update metrics
	UpdateDBCircuitState(int(to))
	RecordDBCircuitTransition(from.String(), to.String())

	// Log state change
	if rs.logger != nil {
		if to == resilience.StateOpen {
			rs.logger.Warn("Database circuit breaker opened",
				"from", from.String(),
				"to", to.String())
		} else if to == resilience.StateClosed && from == resilience.StateHalfOpen {
			rs.logger.Info("Database circuit breaker recovered",
				"from", from.String(),
				"to", to.String())
		}
	}
}

// ExecContext executes a query with circuit breaker protection and timeout
func (rs *ResilientStore) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	var result sql.Result
	var execErr error

	start := time.Now()

	err := rs.executeWithProtection(ctx, "exec", func(ctx context.Context) error {
		var err error
		result, err = rs.inner.ExecContext(ctx, query, args...)
		execErr = err
		return err
	})

	duration := time.Since(start)
	rs.recordQueryMetrics("exec", duration, err)

	if err != nil {
		return nil, err
	}
	return result, execErr
}

// QueryContext executes a query that returns rows with protection
func (rs *ResilientStore) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	var rows *sql.Rows
	var queryErr error

	start := time.Now()

	err := rs.executeWithProtection(ctx, "query", func(ctx context.Context) error {
		var err error
		rows, err = rs.inner.QueryContext(ctx, query, args...)
		queryErr = err
		return err
	})

	duration := time.Since(start)
	rs.recordQueryMetrics("query", duration, err)

	if err != nil {
		return nil, err
	}
	return rows, queryErr
}

// QueryRowContext executes a query that returns a single row with protection
func (rs *ResilientStore) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	start := time.Now()

	// Apply timeout to context
	queryCtx, cancel := context.WithTimeout(ctx, rs.config.QueryTimeout)
	defer cancel()

	row := rs.inner.QueryRowContext(queryCtx, query, args...)

	duration := time.Since(start)
	rs.checkSlowQuery("query_row", duration)

	return row
}

// BeginTx starts a transaction with protection
func (rs *ResilientStore) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	var tx *sql.Tx
	var txErr error

	start := time.Now()

	err := rs.executeWithProtection(ctx, "begin_tx", func(ctx context.Context) error {
		var err error
		tx, err = rs.inner.BeginTx(ctx, opts)
		txErr = err
		return err
	})

	duration := time.Since(start)
	rs.recordQueryMetrics("begin_tx", duration, err)

	if err != nil {
		return nil, err
	}
	return tx, txErr
}

// executeWithProtection runs a function with circuit breaker and timeout protection
func (rs *ResilientStore) executeWithProtection(ctx context.Context, queryType string, fn func(ctx context.Context) error) error {
	// Apply query timeout
	queryCtx, cancel := context.WithTimeout(ctx, rs.config.QueryTimeout)
	defer cancel()

	// Execute with or without circuit breaker
	if rs.breaker != nil {
		return rs.breaker.Execute(queryCtx, fn)
	}

	return fn(queryCtx)
}

// recordQueryMetrics records query metrics and checks for slow queries
func (rs *ResilientStore) recordQueryMetrics(queryType string, duration time.Duration, err error) {
	// Record duration metric
	RecordDBQuery(queryType, duration.Seconds(), err)

	// Check for slow query
	rs.checkSlowQuery(queryType, duration)

	// Periodically update connection stats
	rs.maybeUpdateConnectionStats()
}

// checkSlowQuery logs and records slow queries
func (rs *ResilientStore) checkSlowQuery(queryType string, duration time.Duration) {
	if duration > rs.config.SlowQueryThreshold {
		RecordSlowQuery()
		if rs.logger != nil {
			rs.logger.Warn("Slow database query",
				"query_type", queryType,
				"duration_ms", duration.Milliseconds(),
				"threshold_ms", rs.config.SlowQueryThreshold.Milliseconds())
		}
	}
}

// maybeUpdateConnectionStats periodically updates connection pool metrics
func (rs *ResilientStore) maybeUpdateConnectionStats() {
	// Only update every 5 seconds to avoid overhead
	if time.Since(rs.metricsUpdate) < 5*time.Second {
		return
	}
	rs.metricsUpdate = time.Now()

	stats := rs.inner.Stats()
	UpdateDBConnectionStats(stats.InUse, stats.Idle, stats.OpenConnections, stats.WaitCount)
}

// Ping checks database connectivity through the circuit breaker
func (rs *ResilientStore) Ping(ctx context.Context) error {
	return rs.executeWithProtection(ctx, "ping", func(ctx context.Context) error {
		return rs.inner.Ping(ctx)
	})
}

// Close closes the underlying database
func (rs *ResilientStore) Close() error {
	return rs.inner.Close()
}

// RawDB returns the underlying *sql.DB
func (rs *ResilientStore) RawDB() *sql.DB {
	return rs.inner.RawDB()
}

// Inner returns the underlying SQLiteDB
func (rs *ResilientStore) Inner() *SQLiteDB {
	return rs.inner
}

// Stats returns database statistics
func (rs *ResilientStore) Stats() DBStats {
	return rs.inner.Stats()
}

// Driver returns the database driver name
func (rs *ResilientStore) Driver() string {
	return rs.inner.Driver()
}

// CircuitBreakerState returns the current circuit breaker state as int for health checks
// Implements health.CircuitBreakerProvider interface
// Returns: 0=closed, 1=half-open, 2=open
func (rs *ResilientStore) CircuitBreakerState() interface{} {
	if rs.breaker == nil {
		return int(resilience.StateClosed)
	}
	return int(rs.breaker.State())
}

// CircuitBreakerStats returns circuit breaker statistics
// Implements health.CircuitBreakerProvider interface
func (rs *ResilientStore) CircuitBreakerStats() interface{} {
	if rs.breaker == nil {
		return nil
	}
	stats := rs.breaker.Stats()
	return map[string]interface{}{
		"state":            stats.State.String(),
		"failure_count":    stats.FailureCount,
		"success_count":    stats.SuccessCount,
		"last_failure_time": stats.LastFailureTime,
		"last_state_change": stats.LastStateChange,
	}
}

// ResetCircuitBreaker forces the circuit breaker back to closed state
func (rs *ResilientStore) ResetCircuitBreaker() {
	if rs.breaker != nil {
		rs.breaker.Reset()
	}
}

// Migrate runs migrations on the underlying database
func (rs *ResilientStore) Migrate(ctx context.Context) error {
	return rs.inner.Migrate(ctx)
}
