package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestNewMonitor(t *testing.T) {
	m := NewMonitor()
	if m == nil {
		t.Fatal("NewMonitor returned nil")
	}

	// Should have system checker registered by default
	results := m.GetResults()
	// Initially empty until first check
	if len(results) != 0 {
		t.Logf("Initial results: %d (expected 0 before first check)", len(results))
	}

	// Run a check
	m.Check(context.Background())
	results = m.GetResults()
	if _, ok := results["system"]; !ok {
		t.Error("System check should be registered by default")
	}
}

func TestMonitor_RegisterChecker(t *testing.T) {
	m := NewMonitor()

	called := false
	m.RegisterChecker("test", func(ctx context.Context) CheckResult {
		called = true
		return CheckResult{
			Name:    "test",
			Status:  StatusHealthy,
			Message: "test passed",
		}
	})

	results := m.Check(context.Background())

	if !called {
		t.Error("Custom checker was not called")
	}

	if result, ok := results["test"]; !ok {
		t.Error("Test result not found")
	} else if result.Status != StatusHealthy {
		t.Errorf("Expected healthy status, got %s", result.Status)
	}
}

func TestMonitor_RegisterDatabase(t *testing.T) {
	// Create an in-memory SQLite database for testing
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	m := NewMonitor()
	m.RegisterDatabase(db)

	results := m.Check(context.Background())

	if result, ok := results["database"]; !ok {
		t.Error("Database check not found")
	} else {
		if result.Status != StatusHealthy {
			t.Errorf("Expected healthy database, got %s: %s", result.Status, result.Message)
		}
		// Check that details are populated
		if result.Details["open_connections"] == nil {
			t.Error("Database details should include open_connections")
		}
	}
}

func TestMonitor_RegisterDiskSpace(t *testing.T) {
	m := NewMonitor()
	m.RegisterDiskSpace("/tmp") // /tmp should exist on most systems

	results := m.Check(context.Background())

	if result, ok := results["disk"]; !ok {
		t.Error("Disk check not found")
	} else {
		if result.Status == StatusUnhealthy {
			t.Logf("Disk check unhealthy: %s", result.Message)
		}
		// Details should have path info
		if result.Details["/tmp"] == nil {
			t.Error("Disk details should include /tmp")
		}
	}
}

func TestMonitor_IsHealthy(t *testing.T) {
	m := NewMonitor()

	// Register a healthy checker
	m.RegisterChecker("healthy", func(ctx context.Context) CheckResult {
		return CheckResult{Status: StatusHealthy}
	})

	m.Check(context.Background())

	if !m.IsHealthy() {
		t.Error("Monitor should be healthy")
	}

	if !m.IsReady() {
		t.Error("Monitor should be ready")
	}
}

func TestMonitor_IsUnhealthy(t *testing.T) {
	m := NewMonitor()

	// Override system check with unhealthy one
	m.RegisterChecker("system", func(ctx context.Context) CheckResult {
		return CheckResult{Status: StatusUnhealthy, Message: "test failure"}
	})

	m.Check(context.Background())

	if m.IsHealthy() {
		t.Error("Monitor should not be healthy")
	}

	if m.IsReady() {
		t.Error("Monitor should not be ready when unhealthy")
	}

	if m.OverallStatus() != StatusUnhealthy {
		t.Errorf("Expected unhealthy status, got %s", m.OverallStatus())
	}
}

func TestMonitor_IsDegraded(t *testing.T) {
	m := NewMonitor()

	// Override system check with degraded one
	m.RegisterChecker("system", func(ctx context.Context) CheckResult {
		return CheckResult{Status: StatusDegraded, Message: "partially working"}
	})

	m.Check(context.Background())

	if m.IsHealthy() {
		t.Error("Monitor should not be healthy when degraded")
	}

	if !m.IsReady() {
		t.Error("Monitor should be ready when degraded")
	}

	if m.OverallStatus() != StatusDegraded {
		t.Errorf("Expected degraded status, got %s", m.OverallStatus())
	}
}

func TestMonitor_Callbacks(t *testing.T) {
	m := NewMonitor()

	unhealthyCalled := false
	recoveredCalled := false

	m.OnUnhealthy(func(name string, result CheckResult) {
		unhealthyCalled = true
	})
	m.OnRecovered(func(name string, result CheckResult) {
		recoveredCalled = true
	})

	// First check - healthy
	healthy := true
	m.RegisterChecker("test", func(ctx context.Context) CheckResult {
		if healthy {
			return CheckResult{Status: StatusHealthy}
		}
		return CheckResult{Status: StatusUnhealthy}
	})

	m.Check(context.Background())

	// Second check - unhealthy
	healthy = false
	m.Check(context.Background())

	if !unhealthyCalled {
		t.Error("OnUnhealthy callback should have been called")
	}

	// Third check - recovered
	healthy = true
	m.Check(context.Background())

	if !recoveredCalled {
		t.Error("OnRecovered callback should have been called")
	}
}

func TestMonitor_StartStop(t *testing.T) {
	m := NewMonitor()

	checkCount := 0
	m.RegisterChecker("counter", func(ctx context.Context) CheckResult {
		checkCount++
		return CheckResult{Status: StatusHealthy}
	})

	// Reduce interval for testing
	m.interval = 100 * time.Millisecond

	m.Start()
	time.Sleep(350 * time.Millisecond) // Should trigger at least 3 checks
	m.Stop()

	if checkCount < 2 {
		t.Errorf("Expected at least 2 checks, got %d", checkCount)
	}
}

func TestMonitor_HTTPHandler_Health(t *testing.T) {
	m := NewMonitor()
	handler := m.HTTPHandler()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("Expected status 'ok', got %v", response["status"])
	}
}

func TestMonitor_HTTPHandler_Healthz(t *testing.T) {
	m := NewMonitor()
	handler := m.HTTPHandler()

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
}

func TestMonitor_HTTPHandler_Ready(t *testing.T) {
	m := NewMonitor()
	m.RegisterChecker("system", func(ctx context.Context) CheckResult {
		return CheckResult{Status: StatusHealthy}
	})

	handler := m.HTTPHandler()

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["status"] != string(StatusHealthy) {
		t.Errorf("Expected healthy status, got %v", response["status"])
	}
}

func TestMonitor_HTTPHandler_Ready_Unhealthy(t *testing.T) {
	m := NewMonitor()
	m.RegisterChecker("system", func(ctx context.Context) CheckResult {
		return CheckResult{Status: StatusUnhealthy, Message: "test failure"}
	})

	handler := m.HTTPHandler()

	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", w.Code)
	}
}

func TestMonitor_HTTPHandler_Status(t *testing.T) {
	m := NewMonitor()

	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	m.RegisterDatabase(db)

	// Test buildResponse and getSystemInfo directly first
	results := m.Check(context.Background())
	response := m.buildResponse(results)
	response["system"] = m.getSystemInfo()

	// Verify the response can be JSON encoded
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("Response should not be empty")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	// Should have checks
	if parsed["checks"] == nil {
		t.Error("Response should include checks")
	}

	// Should have system info
	if parsed["system"] == nil {
		t.Error("Response should include system info")
	}
}

func TestCheckResult_Duration(t *testing.T) {
	m := NewMonitor()

	m.RegisterChecker("slow", func(ctx context.Context) CheckResult {
		start := time.Now()
		time.Sleep(50 * time.Millisecond)
		return CheckResult{
			Status:   StatusHealthy,
			Duration: time.Since(start), // Checker must set duration
		}
	})

	results := m.Check(context.Background())

	if result, ok := results["slow"]; ok {
		if result.Duration < 50*time.Millisecond {
			t.Errorf("Duration should be at least 50ms, got %v", result.Duration)
		}
	} else {
		t.Error("slow checker result not found")
	}
}

func TestMonitor_ConcurrentChecks(t *testing.T) {
	m := NewMonitor()

	// Register multiple checkers
	for i := 0; i < 5; i++ {
		name := string(rune('a' + i))
		m.RegisterChecker(name, func(ctx context.Context) CheckResult {
			time.Sleep(10 * time.Millisecond)
			return CheckResult{Status: StatusHealthy}
		})
	}

	start := time.Now()
	results := m.Check(context.Background())
	duration := time.Since(start)

	// All checks should run concurrently, so total time should be ~10ms, not 50ms
	if duration > 100*time.Millisecond {
		t.Errorf("Checks should run concurrently, took %v", duration)
	}

	if len(results) < 5 {
		t.Errorf("Expected at least 5 results, got %d", len(results))
	}
}

func TestStatus_Constants(t *testing.T) {
	// Verify status constants are what we expect
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
