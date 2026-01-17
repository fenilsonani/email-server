package metadata

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Database metrics for observability
var (
	// DBQueryDuration tracks query execution time by type
	DBQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mailserver_db_query_duration_seconds",
		Help:    "Database query duration in seconds",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to ~4s
	}, []string{"query_type"})

	// DBQueryErrors tracks query errors by type
	DBQueryErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_db_query_errors_total",
		Help: "Total database query errors",
	}, []string{"query_type", "error_type"})

	// DBSlowQueries tracks queries exceeding the slow threshold
	DBSlowQueries = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_db_slow_queries_total",
		Help: "Total number of slow database queries",
	})

	// DBCircuitState tracks circuit breaker state (0=closed, 1=half-open, 2=open)
	DBCircuitState = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mailserver_db_circuit_breaker_state",
		Help: "Database circuit breaker state (0=closed, 1=half-open, 2=open)",
	})

	// DBCircuitTransitions tracks circuit breaker state changes
	DBCircuitTransitions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_db_circuit_transitions_total",
		Help: "Database circuit breaker state transitions",
	}, []string{"from", "to"})

	// DBConnectionsInUse tracks active database connections
	DBConnectionsInUse = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mailserver_db_connections_in_use",
		Help: "Number of database connections currently in use",
	})

	// DBConnectionsIdle tracks idle database connections
	DBConnectionsIdle = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mailserver_db_connections_idle",
		Help: "Number of idle database connections",
	})

	// DBConnectionsTotal tracks total open database connections
	DBConnectionsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mailserver_db_connections_total",
		Help: "Total number of open database connections",
	})

	// DBConnectionWaitCount tracks how many times connections had to wait
	DBConnectionWaitCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_db_connection_wait_total",
		Help: "Total number of times a connection had to wait",
	})
)

// RecordDBQuery records a database query with its duration and any error
func RecordDBQuery(queryType string, durationSeconds float64, err error) {
	DBQueryDuration.WithLabelValues(queryType).Observe(durationSeconds)
	if err != nil {
		errorType := classifyDBError(err)
		DBQueryErrors.WithLabelValues(queryType, errorType).Inc()
	}
}

// RecordSlowQuery records a slow query occurrence
func RecordSlowQuery() {
	DBSlowQueries.Inc()
}

// UpdateDBCircuitState updates the circuit breaker state metric
func UpdateDBCircuitState(state int) {
	DBCircuitState.Set(float64(state))
}

// RecordDBCircuitTransition records a circuit breaker state transition
func RecordDBCircuitTransition(from, to string) {
	DBCircuitTransitions.WithLabelValues(from, to).Inc()
}

// UpdateDBConnectionStats updates connection pool statistics
func UpdateDBConnectionStats(inUse, idle, total int, waitCount int64) {
	DBConnectionsInUse.Set(float64(inUse))
	DBConnectionsIdle.Set(float64(idle))
	DBConnectionsTotal.Set(float64(total))
}

// classifyDBError categorizes database errors for metrics
func classifyDBError(err error) string {
	if err == nil {
		return "none"
	}

	errStr := err.Error()

	// Check for common error types
	switch {
	case contains(errStr, "timeout"):
		return "timeout"
	case contains(errStr, "connection refused"):
		return "connection_refused"
	case contains(errStr, "database is locked"):
		return "locked"
	case contains(errStr, "no such table"):
		return "schema_error"
	case contains(errStr, "constraint"):
		return "constraint_violation"
	case contains(errStr, "syntax"):
		return "syntax_error"
	default:
		return "unknown"
	}
}

// contains checks if s contains substr (simple implementation to avoid import)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
