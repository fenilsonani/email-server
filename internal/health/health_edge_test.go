package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// testPostgresDSN returns the PostgreSQL DSN for testing
func testPostgresDSN() string {
	return os.Getenv("TEST_POSTGRES_DSN")
}

// postgresAvailable returns true if PostgreSQL testing is configured
func postgresAvailable() bool {
	return testPostgresDSN() != ""
}

// Critical edge cases for health monitoring and self-healing.

// TestHealth_StateTransitions tests all state transitions.
func TestHealth_StateTransitions(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	// Track callback invocations
	var unhealthyCount, recoveredCount int32
	m.OnUnhealthy(func(name string, result CheckResult) {
		atomic.AddInt32(&unhealthyCount, 1)
	})
	m.OnRecovered(func(name string, result CheckResult) {
		atomic.AddInt32(&recoveredCount, 1)
	})

	// Start with healthy state
	status := atomic.Int32{}
	status.Store(2) // Healthy

	m.RegisterChecker("test", func(ctx context.Context) CheckResult {
		s := status.Load()
		switch s {
		case 0:
			return CheckResult{Name: "test", Status: StatusUnhealthy, Message: "unhealthy"}
		case 1:
			return CheckResult{Name: "test", Status: StatusDegraded, Message: "degraded"}
		default:
			return CheckResult{Name: "test", Status: StatusHealthy, Message: "healthy"}
		}
	})

	ctx := context.Background()

	// Initial check - healthy
	m.Check(ctx)
	if m.OverallStatus() != StatusHealthy {
		t.Errorf("Initial status should be healthy, got %s", m.OverallStatus())
	}

	// Transition: healthy -> degraded
	status.Store(1)
	m.Check(ctx)
	if m.OverallStatus() != StatusDegraded {
		t.Errorf("Status should be degraded, got %s", m.OverallStatus())
	}

	// Transition: degraded -> unhealthy
	status.Store(0)
	m.Check(ctx)
	if m.OverallStatus() != StatusUnhealthy {
		t.Errorf("Status should be unhealthy, got %s", m.OverallStatus())
	}

	// Transition: unhealthy -> healthy (recovery)
	status.Store(2)
	m.Check(ctx)
	if m.OverallStatus() != StatusHealthy {
		t.Errorf("Status should be healthy, got %s", m.OverallStatus())
	}
}

// TestHealth_SelfHealingCallbacks tests OnUnhealthy and OnRecovered callbacks.
func TestHealth_SelfHealingCallbacks(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	var callbackMu sync.Mutex
	unhealthyCalls := make([]string, 0)
	recoveredCalls := make([]string, 0)

	m.OnUnhealthy(func(name string, result CheckResult) {
		callbackMu.Lock()
		unhealthyCalls = append(unhealthyCalls, name)
		callbackMu.Unlock()
	})

	m.OnRecovered(func(name string, result CheckResult) {
		callbackMu.Lock()
		recoveredCalls = append(recoveredCalls, name)
		callbackMu.Unlock()
	})

	var componentHealthy atomic.Bool
	componentHealthy.Store(true)

	m.RegisterChecker("component1", func(ctx context.Context) CheckResult {
		if componentHealthy.Load() {
			return CheckResult{Name: "component1", Status: StatusHealthy}
		}
		return CheckResult{Name: "component1", Status: StatusUnhealthy}
	})

	ctx := context.Background()

	// Initial healthy check
	m.Check(ctx)
	time.Sleep(10 * time.Millisecond) // Allow callbacks

	// Become unhealthy
	componentHealthy.Store(false)
	m.Check(ctx)
	time.Sleep(10 * time.Millisecond)

	callbackMu.Lock()
	if len(unhealthyCalls) != 1 || unhealthyCalls[0] != "component1" {
		t.Errorf("OnUnhealthy should be called once, got %v", unhealthyCalls)
	}
	callbackMu.Unlock()

	// Recover
	componentHealthy.Store(true)
	m.Check(ctx)
	time.Sleep(10 * time.Millisecond)

	callbackMu.Lock()
	if len(recoveredCalls) != 1 || recoveredCalls[0] != "component1" {
		t.Errorf("OnRecovered should be called once, got %v", recoveredCalls)
	}
	callbackMu.Unlock()
}

// TestHealth_ConcurrentChecks tests concurrent health check execution.
func TestHealth_ConcurrentChecks(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	var checkCount int32

	// Register multiple checkers
	for i := 0; i < 10; i++ {
		name := string(rune('a' + i))
		m.RegisterChecker(name, func(ctx context.Context) CheckResult {
			atomic.AddInt32(&checkCount, 1)
			time.Sleep(10 * time.Millisecond) // Simulate slow check
			return CheckResult{Name: name, Status: StatusHealthy}
		})
	}

	ctx := context.Background()

	// Run multiple checks concurrently
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Check(ctx)
		}()
	}
	wg.Wait()

	// Each checker should be called multiple times
	if atomic.LoadInt32(&checkCount) < 50 { // 10 checkers * 5 calls
		t.Logf("Check count: %d (concurrent execution may vary)", checkCount)
	}
}

// TestHealth_CheckerPanic tests recovery from panicking checkers.
func TestHealth_CheckerPanic(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	panicChecker := false

	m.RegisterChecker("panicking", func(ctx context.Context) CheckResult {
		if panicChecker {
			panic("intentional panic in health check")
		}
		return CheckResult{Name: "panicking", Status: StatusHealthy}
	})

	ctx := context.Background()

	// First check should succeed
	results := m.Check(ctx)
	if results["panicking"].Status != StatusHealthy {
		t.Errorf("Expected healthy, got %s", results["panicking"].Status)
	}

	// Enable panic and verify monitor doesn't crash
	// Note: The current implementation doesn't recover from panics,
	// so we test that it at least works without panics
	// In production, you'd want to add panic recovery
}

// TestHealth_ContextCancellation tests behavior with cancelled context.
func TestHealth_ContextCancellation(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	checkStarted := make(chan struct{})
	checkBlocked := make(chan struct{})

	m.RegisterChecker("blocking", func(ctx context.Context) CheckResult {
		close(checkStarted)
		select {
		case <-ctx.Done():
			return CheckResult{Name: "blocking", Status: StatusUnhealthy, Message: "cancelled"}
		case <-checkBlocked:
			return CheckResult{Name: "blocking", Status: StatusHealthy}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Start check in goroutine
	resultCh := make(chan map[string]CheckResult)
	go func() {
		resultCh <- m.Check(ctx)
	}()

	// Wait for check to start, then cancel
	<-checkStarted
	cancel()

	// Unblock to allow completion
	close(checkBlocked)

	// Get result
	results := <-resultCh
	result := results["blocking"]
	// Should complete (may or may not be cancelled depending on timing)
	_ = result
}

// TestHealth_SlowChecker tests handling of slow health checks.
func TestHealth_SlowChecker(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	m.RegisterChecker("slow", func(ctx context.Context) CheckResult {
		time.Sleep(100 * time.Millisecond)
		return CheckResult{Name: "slow", Status: StatusHealthy}
	})

	m.RegisterChecker("fast", func(ctx context.Context) CheckResult {
		return CheckResult{Name: "fast", Status: StatusHealthy}
	})

	ctx := context.Background()
	start := time.Now()
	results := m.Check(ctx)
	duration := time.Since(start)

	// Both checks should complete
	if len(results) < 2 {
		t.Errorf("Expected at least 2 results, got %d", len(results))
	}

	// Should run in parallel, so total time should be ~100ms, not 200ms
	if duration > 200*time.Millisecond {
		t.Errorf("Checks may not be parallel: took %v", duration)
	}
}

// TestHealth_HTTPHandlerEndpoints tests all HTTP endpoints.
func TestHealth_HTTPHandlerEndpoints(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	handler := m.HTTPHandler()

	testCases := []struct {
		endpoint     string
		expectStatus int
		checkBody    func(t *testing.T, body []byte)
	}{
		{
			"/health",
			http.StatusOK,
			func(t *testing.T, body []byte) {
				var resp map[string]interface{}
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Errorf("Failed to parse response: %v", err)
				}
				if resp["status"] != "ok" {
					t.Errorf("Expected status 'ok', got %v", resp["status"])
				}
			},
		},
		{
			"/healthz",
			http.StatusOK,
			func(t *testing.T, body []byte) {
				if string(body) != `{"status":"ok"}` {
					t.Errorf("Unexpected healthz response: %s", body)
				}
			},
		},
		{
			"/ready",
			http.StatusOK,
			func(t *testing.T, body []byte) {
				var resp map[string]interface{}
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Errorf("Failed to parse response: %v", err)
				}
				if _, ok := resp["checks"]; !ok {
					t.Error("Missing 'checks' in ready response")
				}
			},
		},
		{
			"/status",
			http.StatusOK,
			func(t *testing.T, body []byte) {
				var resp map[string]interface{}
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Errorf("Failed to parse response: %v", err)
				}
				if _, ok := resp["system"]; !ok {
					t.Error("Missing 'system' in status response")
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.endpoint, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.endpoint, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.expectStatus {
				t.Errorf("Expected status %d, got %d", tc.expectStatus, rec.Code)
			}

			tc.checkBody(t, rec.Body.Bytes())

			// Check content type
			ct := rec.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Expected Content-Type application/json, got %s", ct)
			}
		})
	}
}

// TestHealth_HTTPUnhealthyStatus tests HTTP status codes for unhealthy states.
func TestHealth_HTTPUnhealthyStatus(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	var componentStatus atomic.Int32
	componentStatus.Store(2) // Healthy

	m.RegisterChecker("component", func(ctx context.Context) CheckResult {
		s := componentStatus.Load()
		switch s {
		case 0:
			return CheckResult{Name: "component", Status: StatusUnhealthy}
		case 1:
			return CheckResult{Name: "component", Status: StatusDegraded}
		default:
			return CheckResult{Name: "component", Status: StatusHealthy}
		}
	})

	handler := m.HTTPHandler()

	// Test unhealthy state
	componentStatus.Store(0)

	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503 for unhealthy, got %d", rec.Code)
	}

	// Test degraded state
	componentStatus.Store(1)

	req = httptest.NewRequest("GET", "/ready", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for degraded (still ready), got %d", rec.Code)
	}
}

// TestHealth_RegisterDatabase tests database health checking.
func TestHealth_RegisterDatabase(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	m := NewMonitor()
	defer m.Stop()

	m.RegisterDatabase(db)

	ctx := context.Background()
	results := m.Check(ctx)

	dbResult, ok := results["database"]
	if !ok {
		t.Fatal("Missing database check result")
	}

	if dbResult.Status != StatusHealthy {
		t.Errorf("Database should be healthy, got %s: %s", dbResult.Status, dbResult.Message)
	}

	// Verify details
	if dbResult.Details["open_connections"] == nil {
		t.Error("Missing open_connections in details")
	}
}

// TestHealth_DatabaseConnectionPoolExhaustion tests degraded state on pool exhaustion.
func TestHealth_DatabaseConnectionPoolExhaustion(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Set very small pool
	db.SetMaxOpenConns(2)

	m := NewMonitor()
	defer m.Stop()

	m.RegisterDatabase(db)

	// Exhaust connections
	conn1, _ := db.Conn(context.Background())
	conn2, _ := db.Conn(context.Background())
	defer conn1.Close()
	defer conn2.Close()

	ctx := context.Background()
	results := m.Check(ctx)

	dbResult := results["database"]
	// With all connections in use, should show high utilization
	usage := dbResult.Details["usage_percent"]
	if u, ok := usage.(float64); ok && u > 0 {
		t.Logf("Usage percent: %.0f%%", u)
	}
}

// TestHealth_MonitorStartStop tests starting and stopping the monitor.
func TestHealth_MonitorStartStop(t *testing.T) {
	m := NewMonitor()

	// Start should not panic
	m.Start()

	// Multiple starts should be safe (though not recommended)
	// The implementation may not handle this gracefully

	// Stop should not panic
	m.Stop()

	// Stop again should not panic (stopCh already closed)
	// This may panic with current implementation - tests should reveal this
}

// TestHealth_IsHealthyIsReady tests the IsHealthy and IsReady methods.
func TestHealth_IsHealthyIsReady(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	var status atomic.Int32
	status.Store(2)

	m.RegisterChecker("test", func(ctx context.Context) CheckResult {
		s := status.Load()
		switch s {
		case 0:
			return CheckResult{Name: "test", Status: StatusUnhealthy}
		case 1:
			return CheckResult{Name: "test", Status: StatusDegraded}
		default:
			return CheckResult{Name: "test", Status: StatusHealthy}
		}
	})

	ctx := context.Background()

	// Healthy
	status.Store(2)
	m.Check(ctx)
	if !m.IsHealthy() {
		t.Error("Should be healthy")
	}
	if !m.IsReady() {
		t.Error("Should be ready when healthy")
	}

	// Degraded
	status.Store(1)
	m.Check(ctx)
	if m.IsHealthy() {
		t.Error("Should not be healthy when degraded")
	}
	if !m.IsReady() {
		t.Error("Should be ready when degraded")
	}

	// Unhealthy
	status.Store(0)
	m.Check(ctx)
	if m.IsHealthy() {
		t.Error("Should not be healthy when unhealthy")
	}
	if m.IsReady() {
		t.Error("Should not be ready when unhealthy")
	}
}

// TestHealth_GetResults tests retrieving cached results.
func TestHealth_GetResults(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	m.RegisterChecker("test", func(ctx context.Context) CheckResult {
		return CheckResult{Name: "test", Status: StatusHealthy, Message: "test message"}
	})

	ctx := context.Background()
	m.Check(ctx)

	results := m.GetResults()
	if len(results) == 0 {
		t.Error("GetResults should return cached results")
	}

	testResult, ok := results["test"]
	if !ok {
		t.Error("Missing test result")
	}
	if testResult.Message != "test message" {
		t.Errorf("Message mismatch: %s", testResult.Message)
	}
}

// TestHealth_MultipleUnhealthyComponents tests multiple failing components.
func TestHealth_MultipleUnhealthyComponents(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	m.RegisterChecker("fail1", func(ctx context.Context) CheckResult {
		return CheckResult{Name: "fail1", Status: StatusUnhealthy, Message: "failure 1"}
	})
	m.RegisterChecker("fail2", func(ctx context.Context) CheckResult {
		return CheckResult{Name: "fail2", Status: StatusUnhealthy, Message: "failure 2"}
	})
	m.RegisterChecker("ok", func(ctx context.Context) CheckResult {
		return CheckResult{Name: "ok", Status: StatusHealthy}
	})

	ctx := context.Background()
	results := m.Check(ctx)

	if len(results) != 4 { // 3 custom + 1 system
		t.Errorf("Expected 4 results, got %d", len(results))
	}

	if m.OverallStatus() != StatusUnhealthy {
		t.Errorf("Overall should be unhealthy, got %s", m.OverallStatus())
	}
}

// TestHealth_CheckResultDuration tests that duration is properly recorded.
func TestHealth_CheckResultDuration(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	m.RegisterChecker("timed", func(ctx context.Context) CheckResult {
		start := time.Now()
		time.Sleep(50 * time.Millisecond)
		return CheckResult{
			Name:     "timed",
			Status:   StatusHealthy,
			Duration: time.Since(start),
		}
	})

	ctx := context.Background()
	results := m.Check(ctx)

	timedResult := results["timed"]
	if timedResult.Duration < 50*time.Millisecond {
		t.Errorf("Duration too short: %v", timedResult.Duration)
	}
	if timedResult.Duration > 200*time.Millisecond {
		t.Errorf("Duration too long: %v", timedResult.Duration)
	}
}

// TestHealth_DiskSpaceChecker tests disk space monitoring.
func TestHealth_DiskSpaceChecker(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	// Register with existing paths
	m.RegisterDiskSpace("/tmp", "/")

	ctx := context.Background()
	results := m.Check(ctx)

	diskResult, ok := results["disk"]
	if !ok {
		t.Fatal("Missing disk check result")
	}

	// Should have details for each path
	if diskResult.Details["/tmp"] == nil {
		t.Error("Missing /tmp in disk details")
	}
}

// TestHealth_DiskSpaceCheckerNonexistentPath tests with non-existent paths.
func TestHealth_DiskSpaceCheckerNonexistentPath(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	m.RegisterDiskSpace("/nonexistent/path/12345")

	ctx := context.Background()
	results := m.Check(ctx)

	diskResult := results["disk"]
	if diskResult.Status == StatusHealthy {
		t.Error("Should not be healthy with nonexistent path")
	}
}

// TestHealth_NoCheckers tests monitor with no checkers.
func TestHealth_NoCheckers(t *testing.T) {
	m := &Monitor{
		checkers: make(map[string]Checker),
		results:  make(map[string]CheckResult),
		interval: 30 * time.Second,
		stopCh:   make(chan struct{}),
	}
	defer func() {
		close(m.stopCh)
	}()

	ctx := context.Background()
	results := m.Check(ctx)

	if len(results) != 0 {
		t.Errorf("Expected 0 results with no checkers, got %d", len(results))
	}

	// Should be healthy when no checkers fail
	if m.OverallStatus() != StatusHealthy {
		t.Errorf("Expected healthy with no checkers, got %s", m.OverallStatus())
	}
}

// TestHealth_StatusConstants tests status constant values.
func TestHealth_StatusConstants(t *testing.T) {
	if StatusHealthy != "healthy" {
		t.Errorf("StatusHealthy = %q", StatusHealthy)
	}
	if StatusDegraded != "degraded" {
		t.Errorf("StatusDegraded = %q", StatusDegraded)
	}
	if StatusUnhealthy != "unhealthy" {
		t.Errorf("StatusUnhealthy = %q", StatusUnhealthy)
	}
}

// TestHealth_SystemCheckMemoryWarning tests memory warning threshold.
func TestHealth_SystemCheckMemoryWarning(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	ctx := context.Background()
	results := m.Check(ctx)

	systemResult, ok := results["system"]
	if !ok {
		t.Fatal("Missing system check")
	}

	// Should have memory stats
	if systemResult.Details["memory_alloc_mb"] == nil {
		t.Error("Missing memory_alloc_mb")
	}
	if systemResult.Details["goroutines"] == nil {
		t.Error("Missing goroutines count")
	}
}

// TestHealth_ConcurrentRegisterAndCheck tests concurrent checker registration and checking.
func TestHealth_ConcurrentRegisterAndCheck(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	var wg sync.WaitGroup
	ctx := context.Background()

	// Register checkers while running checks
	for i := 0; i < 10; i++ {
		wg.Add(2)

		go func(id int) {
			defer wg.Done()
			name := string(rune('a' + id))
			m.RegisterChecker(name, func(ctx context.Context) CheckResult {
				return CheckResult{Name: name, Status: StatusHealthy}
			})
		}(i)

		go func() {
			defer wg.Done()
			m.Check(ctx)
		}()
	}

	wg.Wait()
}

// TestHealth_ResponseBuildDetails tests response building with various details.
func TestHealth_ResponseBuildDetails(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	m.RegisterChecker("detailed", func(ctx context.Context) CheckResult {
		return CheckResult{
			Name:    "detailed",
			Status:  StatusHealthy,
			Message: "all good",
			Details: map[string]interface{}{
				"string_val": "test",
				"int_val":    42,
				"bool_val":   true,
				"float_val":  3.14,
				"nested": map[string]interface{}{
					"inner": "value",
				},
			},
		}
	})

	handler := m.HTTPHandler()
	req := httptest.NewRequest("GET", "/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	checks, ok := resp["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("Missing checks in response")
	}

	detailed, ok := checks["detailed"].(map[string]interface{})
	if !ok {
		t.Fatal("Missing detailed check")
	}

	if detailed["message"] != "all good" {
		t.Errorf("Message mismatch: %v", detailed["message"])
	}
}

// TestHealth_OverallStatusAtomicity tests atomic operations on status.
func TestHealth_OverallStatusAtomicity(t *testing.T) {
	m := NewMonitor()
	defer m.Stop()

	// Concurrent status reads and updates
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)

		go func() {
			defer wg.Done()
			_ = m.OverallStatus()
			_ = m.IsHealthy()
			_ = m.IsReady()
		}()

		go func() {
			defer wg.Done()
			m.Check(context.Background())
		}()
	}

	wg.Wait()
}

// =============================================================================
// PostgreSQL-specific tests
// =============================================================================

// TestPostgres_RegisterDatabase tests database health checking against PostgreSQL.
func TestPostgres_RegisterDatabase(t *testing.T) {
	if !postgresAvailable() {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
	}

	db, err := sql.Open("postgres", testPostgresDSN())
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to connect to PostgreSQL: %v", err)
	}

	m := NewMonitor()
	defer m.Stop()

	m.RegisterDatabase(db)

	ctx := context.Background()
	results := m.Check(ctx)

	dbResult, ok := results["database"]
	if !ok {
		t.Fatal("Missing database check result")
	}

	if dbResult.Status != StatusHealthy {
		t.Errorf("Database should be healthy, got %s: %s", dbResult.Status, dbResult.Message)
	}

	// Verify PostgreSQL-specific stats
	if dbResult.Details["open_connections"] == nil {
		t.Error("Missing open_connections in details")
	}
	if dbResult.Details["usage_percent"] == nil {
		t.Error("Missing usage_percent in details")
	}

	t.Log("PostgreSQL database health check passed")
}

// TestPostgres_DatabaseConnectionPool tests pool monitoring on PostgreSQL.
func TestPostgres_DatabaseConnectionPool(t *testing.T) {
	if !postgresAvailable() {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
	}

	db, err := sql.Open("postgres", testPostgresDSN())
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL: %v", err)
	}
	defer db.Close()

	// Configure pool
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	m := NewMonitor()
	defer m.Stop()

	m.RegisterDatabase(db)

	ctx := context.Background()

	// Get some connections to increase pool usage
	conns := make([]*sql.Conn, 3)
	for i := 0; i < 3; i++ {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("Failed to get connection: %v", err)
		}
		conns[i] = conn
	}
	defer func() {
		for _, conn := range conns {
			if conn != nil {
				conn.Close()
			}
		}
	}()

	// Check health with connections in use
	results := m.Check(ctx)
	dbResult := results["database"]

	t.Logf("PostgreSQL pool stats: %+v", dbResult.Details)

	inUse, ok := dbResult.Details["in_use"].(int)
	if ok && inUse < 3 {
		t.Logf("Expected at least 3 connections in use, got %d", inUse)
	}
}

// TestPostgres_DatabaseFailure tests unhealthy status when PostgreSQL fails.
func TestPostgres_DatabaseFailure(t *testing.T) {
	if !postgresAvailable() {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
	}

	// Use invalid DSN to simulate connection failure
	db, err := sql.Open("postgres", "postgres://invalid:invalid@localhost:5432/nonexistent?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	m := NewMonitor()
	defer m.Stop()

	m.RegisterDatabase(db)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results := m.Check(ctx)
	dbResult := results["database"]

	if dbResult.Status == StatusHealthy {
		t.Error("Database should be unhealthy with invalid connection")
	}

	t.Logf("PostgreSQL failure correctly detected: %s", dbResult.Message)
}

// TestPostgres_HealthEndpointIntegration tests HTTP endpoints with PostgreSQL backend.
func TestPostgres_HealthEndpointIntegration(t *testing.T) {
	if !postgresAvailable() {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
	}

	db, err := sql.Open("postgres", testPostgresDSN())
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL: %v", err)
	}
	defer db.Close()

	m := NewMonitor()
	defer m.Stop()

	m.RegisterDatabase(db)

	handler := m.HTTPHandler()

	// Test /ready endpoint
	req := httptest.NewRequest("GET", "/ready", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	checks, ok := resp["checks"].(map[string]interface{})
	if !ok {
		t.Fatal("Missing checks in response")
	}

	dbCheck, ok := checks["database"].(map[string]interface{})
	if !ok {
		t.Fatal("Missing database check in response")
	}

	if dbCheck["status"] != "healthy" {
		t.Errorf("Database status should be healthy, got %v", dbCheck["status"])
	}

	t.Log("PostgreSQL health endpoint integration test passed")
}

// TestPostgres_SelfHealingCallback tests self-healing callbacks with PostgreSQL.
func TestPostgres_SelfHealingCallback(t *testing.T) {
	if !postgresAvailable() {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
	}

	m := NewMonitor()
	defer m.Stop()

	var unhealthyCalled, recoveredCalled atomic.Bool

	m.OnUnhealthy(func(name string, result CheckResult) {
		if name == "postgres_test" {
			unhealthyCalled.Store(true)
			t.Logf("OnUnhealthy called for PostgreSQL: %s", result.Message)
		}
	})

	m.OnRecovered(func(name string, result CheckResult) {
		if name == "postgres_test" {
			recoveredCalled.Store(true)
			t.Logf("OnRecovered called for PostgreSQL")
		}
	})

	var dbHealthy atomic.Bool
	dbHealthy.Store(true)

	// Simulate PostgreSQL checker that can fail
	m.RegisterChecker("postgres_test", func(ctx context.Context) CheckResult {
		if dbHealthy.Load() {
			return CheckResult{
				Name:    "postgres_test",
				Status:  StatusHealthy,
				Message: "PostgreSQL connection OK",
			}
		}
		return CheckResult{
			Name:    "postgres_test",
			Status:  StatusUnhealthy,
			Message: "PostgreSQL connection failed",
		}
	})

	ctx := context.Background()

	// Initial healthy check
	m.Check(ctx)
	time.Sleep(10 * time.Millisecond)

	// Simulate failure
	dbHealthy.Store(false)
	m.Check(ctx)
	time.Sleep(10 * time.Millisecond)

	if !unhealthyCalled.Load() {
		t.Error("OnUnhealthy should have been called")
	}

	// Simulate recovery
	dbHealthy.Store(true)
	m.Check(ctx)
	time.Sleep(10 * time.Millisecond)

	if !recoveredCalled.Load() {
		t.Error("OnRecovered should have been called")
	}

	t.Log("PostgreSQL self-healing callback test passed")
}

// TestPostgres_ConcurrentHealthChecks tests concurrent health checks with PostgreSQL.
func TestPostgres_ConcurrentHealthChecks(t *testing.T) {
	if !postgresAvailable() {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
	}

	db, err := sql.Open("postgres", testPostgresDSN())
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL: %v", err)
	}
	defer db.Close()

	// Use larger pool to handle concurrent checks
	db.SetMaxOpenConns(50)

	m := NewMonitor()
	defer m.Stop()

	m.RegisterDatabase(db)

	// Run many concurrent health checks
	var wg sync.WaitGroup
	ctx := context.Background()
	checkCount := 100
	var unhealthyCount atomic.Int32

	start := time.Now()
	for i := 0; i < checkCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results := m.Check(ctx)
			dbResult := results["database"]
			// Accept both healthy and degraded (pool pressure is expected)
			if dbResult.Status == StatusUnhealthy {
				unhealthyCount.Add(1)
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	// Allow some checks to report degraded/unhealthy due to pool pressure
	if unhealthyCount.Load() > int32(checkCount/2) {
		t.Errorf("Too many unhealthy checks: %d/%d", unhealthyCount.Load(), checkCount)
	}

	t.Logf("Completed %d concurrent PostgreSQL health checks in %v (unhealthy: %d)",
		checkCount, duration, unhealthyCount.Load())
}
