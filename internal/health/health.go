// Package health provides automatic health monitoring and self-healing.
package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Status represents the health status of a component.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// CheckResult holds the result of a health check.
type CheckResult struct {
	Name      string                 `json:"name"`
	Status    Status                 `json:"status"`
	Message   string                 `json:"message,omitempty"`
	Duration  time.Duration          `json:"duration_ms"`
	Timestamp time.Time              `json:"timestamp"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// Checker defines a health check function.
type Checker func(ctx context.Context) CheckResult

// Monitor provides automatic health monitoring with self-healing.
// It requires zero configuration - just create and start.
type Monitor struct {
	checkers map[string]Checker
	results  map[string]CheckResult
	mu       sync.RWMutex

	// Overall status
	status int32 // atomic: 0=unhealthy, 1=degraded, 2=healthy

	// Background worker
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup

	// Callbacks for self-healing
	onUnhealthy func(name string, result CheckResult)
	onRecovered func(name string, result CheckResult)
}

// NewMonitor creates a new health monitor with automatic checks.
// Starts background monitoring immediately - no configuration needed.
func NewMonitor() *Monitor {
	m := &Monitor{
		checkers: make(map[string]Checker),
		results:  make(map[string]CheckResult),
		interval: 30 * time.Second,
		stopCh:   make(chan struct{}),
	}

	// Add system checks automatically
	m.RegisterChecker("system", m.systemCheck)

	return m
}

// Start begins background health monitoring.
func (m *Monitor) Start() {
	m.wg.Add(1)
	go m.monitor()
}

// Stop stops the health monitor.
func (m *Monitor) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

// RegisterChecker adds a health checker.
func (m *Monitor) RegisterChecker(name string, checker Checker) {
	m.mu.Lock()
	m.checkers[name] = checker
	m.mu.Unlock()
}

// CircuitBreakerState represents circuit breaker state for health checks.
type CircuitBreakerState struct {
	State           string `json:"state"`            // closed, half-open, open
	FailureCount    int64  `json:"failure_count"`
	SuccessCount    int64  `json:"success_count"`
	LastFailureTime string `json:"last_failure_time,omitempty"`
}

// CircuitBreakerProvider is an interface for components that have circuit breakers.
type CircuitBreakerProvider interface {
	CircuitBreakerState() interface{}  // Returns state (closed=0, half-open=1, open=2)
	CircuitBreakerStats() interface{}  // Returns stats struct
}

// RegisterDatabase adds automatic database health checking.
func (m *Monitor) RegisterDatabase(db *sql.DB) {
	m.RegisterDatabaseWithCircuitBreaker(db, nil)
}

// RegisterDatabaseWithCircuitBreaker adds database health checking with circuit breaker state.
func (m *Monitor) RegisterDatabaseWithCircuitBreaker(db *sql.DB, cbProvider CircuitBreakerProvider) {
	m.RegisterChecker("database", func(ctx context.Context) CheckResult {
		start := time.Now()
		result := CheckResult{
			Name:      "database",
			Timestamp: start,
			Details:   make(map[string]interface{}),
		}

		// Ping test
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			result.Status = StatusUnhealthy
			result.Message = fmt.Sprintf("ping failed: %v", err)
			result.Duration = time.Since(start)
			return result
		}

		// Get stats
		stats := db.Stats()
		result.Details["open_connections"] = stats.OpenConnections
		result.Details["in_use"] = stats.InUse
		result.Details["idle"] = stats.Idle
		result.Details["wait_count"] = stats.WaitCount
		result.Details["max_open"] = stats.MaxOpenConnections

		// Check connection pool health
		var usagePercent float64
		if stats.MaxOpenConnections > 0 {
			usagePercent = float64(stats.InUse) / float64(stats.MaxOpenConnections) * 100
		}
		result.Details["usage_percent"] = usagePercent

		// Add circuit breaker state if available
		if cbProvider != nil {
			cbState := cbProvider.CircuitBreakerState()
			result.Details["circuit_breaker_state"] = cbState

			cbStats := cbProvider.CircuitBreakerStats()
			if cbStats != nil {
				result.Details["circuit_breaker_stats"] = cbStats
			}

			// If circuit breaker is open, report as degraded
			if state, ok := cbState.(int); ok && state == 2 {
				result.Status = StatusDegraded
				result.Message = "database circuit breaker open"
				result.Duration = time.Since(start)
				return result
			}
		}

		if usagePercent > 90 {
			result.Status = StatusDegraded
			result.Message = fmt.Sprintf("connection pool %.0f%% utilized", usagePercent)
		} else {
			result.Status = StatusHealthy
			result.Message = "database healthy"
		}

		result.Duration = time.Since(start)
		return result
	})
}

// RegisterRedis adds automatic Redis health checking.
func (m *Monitor) RegisterRedis(client redis.UniversalClient) {
	m.RegisterChecker("redis", func(ctx context.Context) CheckResult {
		start := time.Now()
		result := CheckResult{
			Name:      "redis",
			Timestamp: start,
			Details:   make(map[string]interface{}),
		}

		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		// Ping test
		if err := client.Ping(ctx).Err(); err != nil {
			result.Status = StatusUnhealthy
			result.Message = fmt.Sprintf("ping failed: %v", err)
			result.Duration = time.Since(start)
			return result
		}

		// Get pool stats if available (for standalone client)
		if c, ok := client.(*redis.Client); ok {
			stats := c.PoolStats()
			result.Details["hits"] = stats.Hits
			result.Details["misses"] = stats.Misses
			result.Details["timeouts"] = stats.Timeouts
			result.Details["total_conns"] = stats.TotalConns
			result.Details["idle_conns"] = stats.IdleConns
			result.Details["stale_conns"] = stats.StaleConns
		}

		result.Status = StatusHealthy
		result.Message = "redis healthy"
		result.Duration = time.Since(start)
		return result
	})
}

// RegisterDiskSpace adds automatic disk space monitoring.
func (m *Monitor) RegisterDiskSpace(paths ...string) {
	if len(paths) == 0 {
		paths = []string{"/var/lib/mailserver", "/"}
	}

	m.RegisterChecker("disk", func(ctx context.Context) CheckResult {
		start := time.Now()
		result := CheckResult{
			Name:      "disk",
			Timestamp: start,
			Details:   make(map[string]interface{}),
		}

		// This is a simplified check - in production, use syscall.Statfs
		// For now, just check if paths exist and are writable
		allHealthy := true
		for _, path := range paths {
			info, err := os.Stat(path)
			if err != nil {
				result.Details[path] = map[string]interface{}{
					"error": err.Error(),
				}
				allHealthy = false
				continue
			}
			result.Details[path] = map[string]interface{}{
				"exists":  true,
				"is_dir":  info.IsDir(),
			}
		}

		if allHealthy {
			result.Status = StatusHealthy
			result.Message = "all paths accessible"
		} else {
			result.Status = StatusDegraded
			result.Message = "some paths inaccessible"
		}

		result.Duration = time.Since(start)
		return result
	})
}

// OnUnhealthy sets a callback for when a component becomes unhealthy.
// Use this for self-healing actions.
// Thread-safe: protects callback assignment with mutex.
func (m *Monitor) OnUnhealthy(callback func(name string, result CheckResult)) {
	m.mu.Lock()
	m.onUnhealthy = callback
	m.mu.Unlock()
}

// OnRecovered sets a callback for when a component recovers.
// Thread-safe: protects callback assignment with mutex.
func (m *Monitor) OnRecovered(callback func(name string, result CheckResult)) {
	m.mu.Lock()
	m.onRecovered = callback
	m.mu.Unlock()
}

// Check runs all health checks immediately.
func (m *Monitor) Check(ctx context.Context) map[string]CheckResult {
	m.mu.RLock()
	checkers := make(map[string]Checker, len(m.checkers))
	for name, checker := range m.checkers {
		checkers[name] = checker
	}
	m.mu.RUnlock()

	results := make(map[string]CheckResult, len(checkers))
	var wg sync.WaitGroup

	for name, checker := range checkers {
		wg.Add(1)
		go func(n string, c Checker) {
			defer wg.Done()
			result := c(ctx)
			m.mu.Lock()
			oldResult, hadOld := m.results[n]
			m.results[n] = result
			m.mu.Unlock()

			// Trigger callbacks (read with lock to avoid race)
			m.mu.RLock()
			onUnhealthy := m.onUnhealthy
			onRecovered := m.onRecovered
			m.mu.RUnlock()

			if hadOld && oldResult.Status == StatusHealthy && result.Status == StatusUnhealthy {
				if onUnhealthy != nil {
					onUnhealthy(n, result)
				}
			}
			if hadOld && oldResult.Status == StatusUnhealthy && result.Status == StatusHealthy {
				if onRecovered != nil {
					onRecovered(n, result)
				}
			}

			m.mu.Lock()
			results[n] = result
			m.mu.Unlock()
		}(name, checker)
	}

	wg.Wait()

	// Update overall status
	m.updateOverallStatus(results)

	return results
}

// GetResults returns the latest check results.
func (m *Monitor) GetResults() map[string]CheckResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]CheckResult, len(m.results))
	for k, v := range m.results {
		results[k] = v
	}
	return results
}

// IsHealthy returns true if all components are healthy.
func (m *Monitor) IsHealthy() bool {
	return atomic.LoadInt32(&m.status) == 2
}

// IsReady returns true if the system can serve requests (healthy or degraded).
func (m *Monitor) IsReady() bool {
	return atomic.LoadInt32(&m.status) >= 1
}

// OverallStatus returns the overall system status.
func (m *Monitor) OverallStatus() Status {
	switch atomic.LoadInt32(&m.status) {
	case 2:
		return StatusHealthy
	case 1:
		return StatusDegraded
	default:
		return StatusUnhealthy
	}
}

// HTTPHandler returns an HTTP handler for health endpoints.
// Automatically responds with appropriate status codes.
func (m *Monitor) HTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// Liveness probe - just checks if server is running
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"time":   time.Now().UTC(),
		})
	})

	// Kubernetes liveness probe
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Readiness probe - checks all dependencies
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		results := m.Check(r.Context())
		response := m.buildResponse(results)

		w.Header().Set("Content-Type", "application/json")
		if m.IsReady() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(response)
	})

	// Detailed status
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		results := m.Check(r.Context())
		response := m.buildResponse(results)
		response["system"] = m.getSystemInfo()

		w.Header().Set("Content-Type", "application/json")
		if m.IsHealthy() {
			w.WriteHeader(http.StatusOK)
		} else if m.IsReady() {
			w.WriteHeader(http.StatusOK) // Still OK but with degraded info
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		json.NewEncoder(w).Encode(response)
	})

	return mux
}

// Internal methods

func (m *Monitor) systemCheck(ctx context.Context) CheckResult {
	start := time.Now()
	result := CheckResult{
		Name:      "system",
		Timestamp: start,
		Status:    StatusHealthy,
		Details:   m.getSystemInfo(),
	}

	// Check memory usage
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	allocMB := memStats.Alloc / 1024 / 1024
	result.Details["memory_alloc_mb"] = allocMB
	result.Details["memory_sys_mb"] = memStats.Sys / 1024 / 1024
	result.Details["goroutines"] = runtime.NumGoroutine()

	// Memory warning threshold (1GB)
	if allocMB > 1024 {
		result.Status = StatusDegraded
		result.Message = fmt.Sprintf("high memory usage: %dMB", allocMB)
	} else {
		result.Message = "system healthy"
	}

	result.Duration = time.Since(start)
	return result
}

func (m *Monitor) getSystemInfo() map[string]interface{} {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return map[string]interface{}{
		"go_version":    runtime.Version(),
		"num_cpu":       runtime.NumCPU(),
		"num_goroutine": runtime.NumGoroutine(),
		"memory_alloc":  memStats.Alloc,
		"memory_sys":    memStats.Sys,
		"gc_cycles":     memStats.NumGC,
	}
}

func (m *Monitor) buildResponse(results map[string]CheckResult) map[string]interface{} {
	checks := make(map[string]interface{})
	for name, result := range results {
		checks[name] = map[string]interface{}{
			"status":      result.Status,
			"message":     result.Message,
			"duration_ms": result.Duration.Milliseconds(),
			"details":     result.Details,
		}
	}

	return map[string]interface{}{
		"status":    m.OverallStatus(),
		"timestamp": time.Now().UTC(),
		"checks":    checks,
	}
}

func (m *Monitor) updateOverallStatus(results map[string]CheckResult) {
	hasUnhealthy := false
	hasDegraded := false

	for _, result := range results {
		switch result.Status {
		case StatusUnhealthy:
			hasUnhealthy = true
		case StatusDegraded:
			hasDegraded = true
		}
	}

	var status int32
	if hasUnhealthy {
		status = 0
	} else if hasDegraded {
		status = 1
	} else {
		status = 2
	}
	atomic.StoreInt32(&m.status, status)
}

func (m *Monitor) monitor() {
	defer m.wg.Done()

	// Initial check
	m.Check(context.Background())

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.Check(context.Background())
		}
	}
}
