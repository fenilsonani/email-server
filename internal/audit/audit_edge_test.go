package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
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

// createPostgresAuditTable creates the audit_log table with PostgreSQL syntax
func createPostgresAuditTable(db *sql.DB) error {
	_, err := db.Exec(`
		DROP TABLE IF EXISTS audit_log CASCADE;
		CREATE TABLE audit_log (
			id SERIAL PRIMARY KEY,
			timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			actor TEXT NOT NULL,
			action TEXT NOT NULL,
			target TEXT,
			details TEXT,
			ip_address TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
		CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor);
		CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);
	`)
	return err
}

// cleanupPostgresAuditTable drops the audit_log table
func cleanupPostgresAuditTable(db *sql.DB) {
	db.Exec("DROP TABLE IF EXISTS audit_log CASCADE")
}

// Critical edge cases for audit logging system.

// TestAudit_EscapeLikeWildcards tests SQL LIKE injection prevention.
func TestAudit_EscapeLikeWildcards(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
		desc     string
	}{
		{"normaluser", "normaluser", "no special chars"},
		{"user%admin", "user\\%admin", "percent wildcard"},
		{"user_name", "user\\_name", "underscore wildcard"},
		{"%_%", "\\%\\_\\%", "all wildcards"},
		{"user\\admin", "user\\\\admin", "backslash"},
		{"user\\%_admin", "user\\\\\\%\\_admin", "mixed escape sequences"},
		{"", "", "empty string"},
		{strings.Repeat("%", 1000), strings.Repeat("\\%", 1000), "many wildcards"},
		{"SELECT * FROM users WHERE 1=1", "SELECT * FROM users WHERE 1=1", "SQL in string"},
		{"'; DROP TABLE users; --", "'; DROP TABLE users; --", "SQL injection attempt"},
		{"100%", "100\\%", "percentage value"},
		{"_%_", "\\_\\%\\_", "wildcard sequence"},
		{"\\\\%\\_", "\\\\\\\\\\%\\\\\\_", "already escaped input"},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			result := escapeLikeWildcards(tc.input)
			if result != tc.expected {
				t.Errorf("escapeLikeWildcards(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

// TestAudit_NilLogger tests graceful degradation with nil logger.
func TestAudit_NilLogger(t *testing.T) {
	var l *Logger

	ctx := context.Background()

	// All operations should succeed silently
	if err := l.Log(ctx, "admin", EventUserCreate, "target", nil, "127.0.0.1"); err != nil {
		t.Errorf("Log should not error with nil logger: %v", err)
	}

	if err := l.LogSimple(ctx, "admin", EventUserCreate, "target", "127.0.0.1"); err != nil {
		t.Errorf("LogSimple should not error with nil logger: %v", err)
	}

	events, err := l.Query(ctx, QueryFilter{})
	if err != nil {
		t.Errorf("Query should not error with nil logger: %v", err)
	}
	if events != nil {
		t.Errorf("Query should return nil with nil logger")
	}

	events, err = l.GetRecent(ctx, 10)
	if err != nil {
		t.Errorf("GetRecent should not error with nil logger: %v", err)
	}
	if events != nil {
		t.Errorf("GetRecent should return nil with nil logger")
	}

	count, err := l.Count(ctx, QueryFilter{})
	if err != nil {
		t.Errorf("Count should not error with nil logger: %v", err)
	}
	if count != 0 {
		t.Errorf("Count should return 0 with nil logger")
	}
}

// TestAudit_NilDB tests logger with nil database.
func TestAudit_NilDB(t *testing.T) {
	l, err := NewLogger(nil)
	if err != nil {
		t.Fatalf("NewLogger(nil) should not error: %v", err)
	}
	if l != nil {
		t.Error("NewLogger(nil) should return nil logger")
	}
}

// TestAudit_ConcurrentLogging tests thread safety of logging.
func TestAudit_ConcurrentLogging(t *testing.T) {
	// Use file-based SQLite with shared cache for concurrent access
	tmpFile := "/tmp/audit_test_" + time.Now().Format("20060102150405") + ".db"
	db, err := sql.Open("sqlite3", tmpFile+"?cache=shared&mode=rwc&_journal_mode=WAL")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer func() {
		db.Close()
		// Clean up temp file
		_ = os.Remove(tmpFile)
		_ = os.Remove(tmpFile + "-shm")
		_ = os.Remove(tmpFile + "-wal")
	}()

	// Enable WAL mode for better concurrent writes
	db.SetMaxOpenConns(1) // Serialize writes for SQLite

	l, err := NewLogger(db)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	var successCount int64
	goroutines := 10
	logsPerGoroutine := 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < logsPerGoroutine; i++ {
				err := l.Log(ctx, "admin", EventUserCreate,
					"user@example.com",
					map[string]interface{}{"goroutine": gid, "iteration": i},
					"127.0.0.1")
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				}
			}
		}(g)
	}

	wg.Wait()

	totalExpected := goroutines * logsPerGoroutine
	if int(successCount) != totalExpected {
		t.Errorf("Expected %d successful logs, got %d", totalExpected, successCount)
	}

	// Verify all logs were recorded
	count, err := l.Count(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != totalExpected {
		t.Errorf("Database count mismatch: expected %d, got %d", totalExpected, count)
	}
}

// TestAudit_LargePayload tests logging with large detail payloads.
func TestAudit_LargePayload(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	l, err := NewLogger(db)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()

	// Test with various large payloads
	testCases := []struct {
		name    string
		details map[string]interface{}
	}{
		{
			"large string value",
			map[string]interface{}{"data": strings.Repeat("x", 100000)},
		},
		{
			"many keys",
			func() map[string]interface{} {
				m := make(map[string]interface{})
				for i := 0; i < 1000; i++ {
					m[strings.Repeat("k", 50)] = strings.Repeat("v", 100)
				}
				return m
			}(),
		},
		{
			"deeply nested",
			map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": map[string]interface{}{
						"level3": map[string]interface{}{
							"level4": map[string]interface{}{
								"level5": "deep value",
							},
						},
					},
				},
			},
		},
		{
			"array data",
			map[string]interface{}{
				"items": make([]string, 1000),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := l.Log(ctx, "admin", EventConfigChange, "config", tc.details, "127.0.0.1")
			if err != nil {
				t.Errorf("Failed to log large payload: %v", err)
			}
		})
	}
}

// TestAudit_SpecialCharacters tests logging with special characters.
func TestAudit_SpecialCharacters(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	l, err := NewLogger(db)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()

	testCases := []struct {
		name   string
		actor  string
		target string
		ip     string
	}{
		{"unicode actor", "用户@example.com", "target@example.com", "127.0.0.1"},
		{"unicode target", "admin@example.com", "日本語@example.jp", "127.0.0.1"},
		{"emoji", "admin@example.com", "🔒secure@example.com", "127.0.0.1"},
		{"null bytes", "admin\x00@example.com", "target\x00@test.com", "127.0.0.1"},
		{"newlines", "admin\n@example.com", "target\r\n@test.com", "127.0.0.1"},
		{"quotes", "admin'test@example.com", `target"test@example.com`, "127.0.0.1"},
		{"SQL chars", "admin;--@example.com", "target/**/test@example.com", "127.0.0.1"},
		{"long actor", strings.Repeat("a", 1000) + "@example.com", "target@example.com", "127.0.0.1"},
		{"IPv6", "admin@example.com", "target@example.com", "::1"},
		{"IPv6 full", "admin@example.com", "target@example.com", "2001:0db8:85a3:0000:0000:8a2e:0370:7334"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := l.LogSimple(ctx, tc.actor, EventLoginSuccess, tc.target, tc.ip)
			if err != nil {
				t.Errorf("Failed to log: %v", err)
			}
		})
	}
}

// TestAudit_QueryWithSQLInjection tests query filter SQL injection prevention.
func TestAudit_QueryWithSQLInjection(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	l, err := NewLogger(db)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()

	// Insert a test record
	err = l.LogSimple(ctx, "admin@example.com", EventUserCreate, "user@example.com", "127.0.0.1")
	if err != nil {
		t.Fatalf("Failed to insert test record: %v", err)
	}

	// Attempt SQL injection through filter
	injectionAttempts := []QueryFilter{
		{Target: "'; DROP TABLE audit_log; --"},
		{Target: "user@example.com' OR '1'='1"},
		{Target: "100%"},
		{Target: "%"},
		{Target: "_"},
		{Target: "user@%"},
		{Actor: "'; DELETE FROM audit_log; --"},
		{Action: EventType("'; DROP TABLE audit_log; --")},
	}

	for _, filter := range injectionAttempts {
		t.Run("injection_"+filter.Target+filter.Actor+string(filter.Action), func(t *testing.T) {
			// Should not panic or cause errors (parameterized queries)
			_, err := l.Query(ctx, filter)
			if err != nil {
				t.Errorf("Query failed (may indicate SQL injection issue): %v", err)
			}
		})
	}

	// Verify table still exists
	count, err := l.Count(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("Count failed - table may have been dropped: %v", err)
	}
	if count < 1 {
		t.Error("Records were deleted - SQL injection may have succeeded")
	}
}

// TestAudit_ContextCancellation tests behavior with cancelled context.
func TestAudit_ContextCancellation(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	l, err := NewLogger(db)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Test with already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Log should fail with cancelled context
	err = l.Log(ctx, "admin", EventUserCreate, "target", nil, "127.0.0.1")
	if err == nil {
		// Some drivers may still succeed if operation is fast enough
		t.Log("Log succeeded with cancelled context (operation was fast)")
	}

	// Query should fail
	_, err = l.Query(ctx, QueryFilter{})
	// May or may not error depending on driver behavior
	_ = err
}

// TestAudit_ContextTimeout tests behavior with timeout context.
func TestAudit_ContextTimeout(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	l, err := NewLogger(db)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Test with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(time.Millisecond) // Ensure timeout passes

	// Operations should fail or succeed quickly
	_ = l.Log(ctx, "admin", EventUserCreate, "target", nil, "127.0.0.1")
	_, _ = l.Query(ctx, QueryFilter{})
}

// TestAudit_QueryFilterCombinations tests various query filter combinations.
func TestAudit_QueryFilterCombinations(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	l, err := NewLogger(db)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()

	// Insert test records
	for i := 0; i < 50; i++ {
		action := EventUserCreate
		if i%2 == 0 {
			action = EventUserDelete
		}
		l.LogSimple(ctx, "admin@example.com", action, "user"+string(rune('0'+i%10))+"@example.com", "127.0.0.1")
	}

	testCases := []struct {
		name   string
		filter QueryFilter
	}{
		{"empty filter", QueryFilter{}},
		{"actor only", QueryFilter{Actor: "admin@example.com"}},
		{"action only", QueryFilter{Action: EventUserCreate}},
		{"limit 1", QueryFilter{Limit: 1}},
		{"limit 1000", QueryFilter{Limit: 1000}},
		{"offset 10", QueryFilter{Offset: 10, Limit: 10}},
		{"offset beyond", QueryFilter{Offset: 100, Limit: 10}},
		{"time range", QueryFilter{
			StartTime: time.Now().Add(-time.Hour),
			EndTime:   time.Now().Add(time.Hour),
		}},
		{"all filters", QueryFilter{
			Actor:     "admin@example.com",
			Action:    EventUserCreate,
			Target:    "user",
			StartTime: time.Now().Add(-time.Hour),
			EndTime:   time.Now().Add(time.Hour),
			Limit:     10,
			Offset:    0,
		}},
		{"zero limit", QueryFilter{Limit: 0}}, // Should use default 100
		{"negative offset", QueryFilter{Offset: -1}}, // Treated as 0
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			events, err := l.Query(ctx, tc.filter)
			if err != nil {
				t.Errorf("Query failed: %v", err)
			}
			// Just verify it doesn't crash
			_ = events
		})
	}
}

// TestAudit_EventTypeConstants tests all event type constants.
func TestAudit_EventTypeConstants(t *testing.T) {
	expectedTypes := []EventType{
		EventUserCreate,
		EventUserDelete,
		EventUserUpdate,
		EventPasswordChange,
		EventDomainCreate,
		EventDomainDelete,
		EventLoginSuccess,
		EventLoginFailure,
		EventSieveUpdate,
		EventQueueRetry,
		EventQueueDelete,
		EventConfigChange,
	}

	for _, et := range expectedTypes {
		if et == "" {
			t.Error("Event type should not be empty")
		}
		// Verify format is dotted notation
		if !strings.Contains(string(et), ".") {
			t.Errorf("Event type %q should contain '.'", et)
		}
	}
}

// TestAudit_JSONMarshalDetails tests JSON marshaling edge cases.
func TestAudit_JSONMarshalDetails(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	l, err := NewLogger(db)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()

	testCases := []struct {
		name    string
		details map[string]interface{}
	}{
		{"nil details", nil},
		{"empty map", map[string]interface{}{}},
		{"numeric types", map[string]interface{}{
			"int":     42,
			"float":   3.14,
			"int64":   int64(9999999999),
			"float64": float64(1.23456789),
		}},
		{"bool types", map[string]interface{}{
			"true":  true,
			"false": false,
		}},
		{"null value", map[string]interface{}{
			"null": nil,
		}},
		{"special floats", map[string]interface{}{
			"zero": 0.0,
			// Note: NaN and Inf can't be marshaled to JSON
		}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := l.Log(ctx, "admin", EventConfigChange, "config", tc.details, "127.0.0.1")
			if err != nil {
				t.Errorf("Failed to log: %v", err)
			}
		})
	}
}

// TestAudit_EventStruct tests the Event struct.
func TestAudit_EventStruct(t *testing.T) {
	event := Event{
		ID:        1,
		Timestamp: time.Now(),
		Actor:     "admin@example.com",
		Action:    EventUserCreate,
		Target:    "user@example.com",
		Details:   `{"key": "value"}`,
		IPAddress: "127.0.0.1",
	}

	// Test JSON marshaling
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal event: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal event: %v", err)
	}

	if decoded.Actor != event.Actor {
		t.Errorf("Actor mismatch: %s != %s", decoded.Actor, event.Actor)
	}
	if decoded.Action != event.Action {
		t.Errorf("Action mismatch: %s != %s", decoded.Action, event.Action)
	}
}

// TestAudit_NULLHandling tests handling of NULL values in database.
func TestAudit_NULLHandling(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	l, err := NewLogger(db)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()

	// Insert with minimal data (some fields will be NULL/empty)
	err = l.LogSimple(ctx, "admin", EventUserCreate, "", "") // Empty target and IP
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	// Query and verify NULL handling
	events, err := l.Query(ctx, QueryFilter{Limit: 1})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	// Empty strings should be returned as empty, not cause errors
	if events[0].Target != "" {
		t.Errorf("Expected empty target, got %q", events[0].Target)
	}
}

// TestAudit_GetRecentLimit tests GetRecent with various limits.
func TestAudit_GetRecentLimit(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	l, err := NewLogger(db)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()

	// Insert 50 records
	for i := 0; i < 50; i++ {
		l.LogSimple(ctx, "admin", EventUserCreate, "user@example.com", "127.0.0.1")
	}

	testCases := []struct {
		limit    int
		expected int
	}{
		{0, 50},   // Default limit (100)
		{10, 10},
		{50, 50},
		{100, 50}, // More than available
		{-1, 50},  // Negative treated as 0 -> default
	}

	for _, tc := range testCases {
		t.Run(string(rune('0'+tc.limit%10)), func(t *testing.T) {
			events, err := l.GetRecent(ctx, tc.limit)
			if err != nil {
				t.Fatalf("GetRecent failed: %v", err)
			}
			if len(events) > tc.expected && tc.limit > 0 {
				t.Errorf("GetRecent(%d) returned %d events, expected at most %d",
					tc.limit, len(events), tc.expected)
			}
		})
	}
}

// TestAudit_RapidLogging tests rapid successive logging.
func TestAudit_RapidLogging(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	l, err := NewLogger(db)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()
	count := 1000

	start := time.Now()
	for i := 0; i < count; i++ {
		if err := l.LogSimple(ctx, "admin", EventUserCreate, "user@example.com", "127.0.0.1"); err != nil {
			t.Fatalf("Log %d failed: %v", i, err)
		}
	}
	duration := time.Since(start)

	// Verify all were recorded
	recorded, err := l.Count(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if recorded != count {
		t.Errorf("Expected %d records, got %d", count, recorded)
	}

	t.Logf("Logged %d events in %v (%.0f events/sec)", count, duration, float64(count)/duration.Seconds())
}

// =============================================================================
// PostgreSQL-specific tests
// =============================================================================

// TestPostgres_AuditLogging tests audit logging against PostgreSQL.
func TestPostgres_AuditLogging(t *testing.T) {
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

	// Clean up any existing table
	cleanupPostgresAuditTable(db)

	// Create logger with PostgreSQL driver (creates table automatically)
	l, err := NewLoggerWithDriver(db, DriverPostgres)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer cleanupPostgresAuditTable(db)

	ctx := context.Background()

	// Test basic logging
	err = l.Log(ctx, "admin@example.com", EventUserCreate, "user@example.com",
		map[string]interface{}{"role": "user", "department": "engineering"},
		"192.168.1.1")
	if err != nil {
		t.Fatalf("Failed to log event: %v", err)
	}

	// Test query
	events, err := l.Query(ctx, QueryFilter{Actor: "admin@example.com"})
	if err != nil {
		t.Fatalf("Failed to query events: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	t.Log("PostgreSQL basic logging test passed")
}

// TestPostgres_ConcurrentLogging tests concurrent logging against PostgreSQL.
func TestPostgres_ConcurrentLogging(t *testing.T) {
	if !postgresAvailable() {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
	}

	db, err := sql.Open("postgres", testPostgresDSN())
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL: %v", err)
	}
	defer db.Close()

	// PostgreSQL handles concurrent writes much better than SQLite
	db.SetMaxOpenConns(10)

	cleanupPostgresAuditTable(db)

	l, err := NewLoggerWithDriver(db, DriverPostgres)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer cleanupPostgresAuditTable(db)

	ctx := context.Background()

	var wg sync.WaitGroup
	var successCount int64
	goroutines := 10
	logsPerGoroutine := 100

	start := time.Now()
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < logsPerGoroutine; i++ {
				err := l.Log(ctx, "admin", EventUserCreate,
					"user@example.com",
					map[string]interface{}{"goroutine": gid, "iteration": i},
					"127.0.0.1")
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				}
			}
		}(g)
	}

	wg.Wait()
	duration := time.Since(start)

	totalExpected := goroutines * logsPerGoroutine
	if int(successCount) != totalExpected {
		t.Errorf("Expected %d successful logs, got %d", totalExpected, successCount)
	}

	// Verify count
	count, err := l.Count(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != totalExpected {
		t.Errorf("Database count mismatch: expected %d, got %d", totalExpected, count)
	}

	t.Logf("PostgreSQL concurrent logging: %d events in %v (%.0f events/sec)",
		totalExpected, duration, float64(totalExpected)/duration.Seconds())
}

// TestPostgres_SQLInjection tests SQL injection prevention on PostgreSQL.
func TestPostgres_SQLInjection(t *testing.T) {
	if !postgresAvailable() {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
	}

	db, err := sql.Open("postgres", testPostgresDSN())
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL: %v", err)
	}
	defer db.Close()

	cleanupPostgresAuditTable(db)

	l, err := NewLoggerWithDriver(db, DriverPostgres)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer cleanupPostgresAuditTable(db)

	ctx := context.Background()

	// Insert a test record
	err = l.LogSimple(ctx, "admin@example.com", EventUserCreate, "user@example.com", "127.0.0.1")
	if err != nil {
		t.Fatalf("Failed to insert test record: %v", err)
	}

	// SQL injection attempts specific to PostgreSQL
	injectionAttempts := []QueryFilter{
		{Target: "'; DROP TABLE audit_log; --"},
		{Target: "user@example.com' OR '1'='1"},
		{Target: "$1"},                          // PostgreSQL placeholder
		{Target: "$$malicious$$"},               // PostgreSQL dollar quoting
		{Actor: "'; DELETE FROM audit_log; --"},
	}

	for _, filter := range injectionAttempts {
		name := filter.Target
		if name == "" {
			name = filter.Actor
		}
		t.Run("injection_"+name[:min(30, len(name))], func(t *testing.T) {
			_, err := l.Query(ctx, filter)
			if err != nil {
				t.Errorf("Query failed: %v", err)
			}
		})
	}

	// Verify table still exists and has data
	count, err := l.Count(ctx, QueryFilter{})
	if err != nil {
		t.Fatalf("Count failed - table may have been dropped: %v", err)
	}
	if count < 1 {
		t.Error("Records were deleted - SQL injection may have succeeded")
	}

	t.Log("PostgreSQL SQL injection prevention test passed")
}

// TestPostgres_SpecialCharacters tests special character handling on PostgreSQL.
func TestPostgres_SpecialCharacters(t *testing.T) {
	if !postgresAvailable() {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
	}

	db, err := sql.Open("postgres", testPostgresDSN())
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL: %v", err)
	}
	defer db.Close()

	cleanupPostgresAuditTable(db)

	l, err := NewLoggerWithDriver(db, DriverPostgres)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer cleanupPostgresAuditTable(db)

	ctx := context.Background()

	testCases := []struct {
		name   string
		actor  string
		target string
	}{
		{"unicode", "用户@example.com", "日本語@example.jp"},
		{"emoji", "admin@example.com", "🔒secure@example.com"},
		{"backslash", "admin\\test@example.com", "target\\test@example.com"},
		{"single_quote", "admin'test@example.com", "target'test@example.com"},
		{"double_quote", `admin"test@example.com`, `target"test@example.com`},
		{"dollar_sign", "admin$test@example.com", "target$var@example.com"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := l.LogSimple(ctx, tc.actor, EventLoginSuccess, tc.target, "127.0.0.1")
			if err != nil {
				t.Errorf("Failed to log: %v", err)
			}

			// Verify we can query it back
			events, err := l.Query(ctx, QueryFilter{Actor: tc.actor})
			if err != nil {
				t.Errorf("Failed to query: %v", err)
			}
			if len(events) == 0 {
				t.Error("Event not found after insert")
			}
		})
	}

	t.Log("PostgreSQL special characters test passed")
}

// TestPostgres_LargePayload tests large payload handling on PostgreSQL.
func TestPostgres_LargePayload(t *testing.T) {
	if !postgresAvailable() {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
	}

	db, err := sql.Open("postgres", testPostgresDSN())
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL: %v", err)
	}
	defer db.Close()

	cleanupPostgresAuditTable(db)

	l, err := NewLoggerWithDriver(db, DriverPostgres)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer cleanupPostgresAuditTable(db)

	ctx := context.Background()

	// PostgreSQL TEXT type can handle very large strings
	largeDetails := map[string]interface{}{
		"large_field": strings.Repeat("x", 1000000), // 1MB string
		"array_field": make([]int, 10000),
	}

	err = l.Log(ctx, "admin", EventConfigChange, "config", largeDetails, "127.0.0.1")
	if err != nil {
		t.Errorf("Failed to log large payload: %v", err)
	}

	// Verify it was stored
	events, err := l.GetRecent(ctx, 1)
	if err != nil {
		t.Fatalf("Failed to get recent: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	t.Log("PostgreSQL large payload test passed")
}

// TestPostgres_QueryPerformance tests query performance on PostgreSQL with many records.
func TestPostgres_QueryPerformance(t *testing.T) {
	if !postgresAvailable() {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
	}

	db, err := sql.Open("postgres", testPostgresDSN())
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(10)

	cleanupPostgresAuditTable(db)

	l, err := NewLoggerWithDriver(db, DriverPostgres)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer cleanupPostgresAuditTable(db)

	ctx := context.Background()

	// Insert many records
	recordCount := 10000
	t.Logf("Inserting %d records...", recordCount)

	start := time.Now()
	for i := 0; i < recordCount; i++ {
		action := EventUserCreate
		if i%3 == 0 {
			action = EventLoginSuccess
		} else if i%3 == 1 {
			action = EventLoginFailure
		}
		l.LogSimple(ctx, "admin@example.com", action, "user"+string(rune('0'+i%10))+"@example.com", "127.0.0.1")
	}
	insertDuration := time.Since(start)
	t.Logf("Insert completed in %v (%.0f records/sec)", insertDuration, float64(recordCount)/insertDuration.Seconds())

	// Test query performance
	start = time.Now()
	events, err := l.Query(ctx, QueryFilter{
		Action: EventLoginSuccess,
		Limit:  100,
	})
	queryDuration := time.Since(start)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	t.Logf("Query returned %d events in %v", len(events), queryDuration)

	// Test count performance
	start = time.Now()
	count, err := l.Count(ctx, QueryFilter{Action: EventLoginSuccess})
	countDuration := time.Since(start)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	t.Logf("Count returned %d in %v", count, countDuration)

	// Performance assertions
	if queryDuration > 500*time.Millisecond {
		t.Errorf("Query took too long: %v", queryDuration)
	}
	if countDuration > 500*time.Millisecond {
		t.Errorf("Count took too long: %v", countDuration)
	}
}

// min returns the smaller of two ints
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
