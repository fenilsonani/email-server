package audit

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	// Use shared cache mode for in-memory database to allow concurrent access
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared&mode=rwc")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	// Set connection pool to 1 to avoid issues with in-memory databases
	db.SetMaxOpenConns(1)
	return db
}

func TestNewLogger(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger, err := NewLogger(db)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	if logger == nil {
		t.Fatal("NewLogger() returned nil")
	}

	// Verify table was created
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='audit_log'").Scan(&tableName)
	if err != nil {
		t.Errorf("audit_log table was not created: %v", err)
	}
}

func TestNewLoggerNilDB(t *testing.T) {
	logger, err := NewLogger(nil)
	if err != nil {
		t.Errorf("NewLogger(nil) should not return error, got: %v", err)
	}
	if logger != nil {
		t.Error("NewLogger(nil) should return nil logger")
	}
}

func TestLog(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger, err := NewLogger(db)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name      string
		actor     string
		action    EventType
		target    string
		details   map[string]interface{}
		ipAddress string
	}{
		{
			name:      "user create",
			actor:     "admin@example.com",
			action:    EventUserCreate,
			target:    "newuser@example.com",
			details:   map[string]interface{}{"domain_id": 1},
			ipAddress: "192.168.1.1",
		},
		{
			name:      "user delete",
			actor:     "admin@example.com",
			action:    EventUserDelete,
			target:    "olduser@example.com",
			details:   nil,
			ipAddress: "192.168.1.1",
		},
		{
			name:      "login success",
			actor:     "user@example.com",
			action:    EventLoginSuccess,
			target:    "user@example.com",
			details:   nil,
			ipAddress: "10.0.0.1",
		},
		{
			name:      "login failure",
			actor:     "attacker",
			action:    EventLoginFailure,
			target:    "admin@example.com",
			details:   map[string]interface{}{"remaining_attempts": 3, "blocked": false},
			ipAddress: "1.2.3.4",
		},
		{
			name:      "password change",
			actor:     "admin@example.com",
			action:    EventPasswordChange,
			target:    "user:123",
			details:   nil,
			ipAddress: "192.168.1.100",
		},
		{
			name:      "domain create",
			actor:     "admin@example.com",
			action:    EventDomainCreate,
			target:    "newdomain.com",
			details:   nil,
			ipAddress: "127.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := logger.Log(ctx, tt.actor, tt.action, tt.target, tt.details, tt.ipAddress)
			if err != nil {
				t.Errorf("Log() error = %v", err)
			}
		})
	}

	// Verify all events were logged
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count audit logs: %v", err)
	}

	if count != len(tests) {
		t.Errorf("Expected %d audit log entries, got %d", len(tests), count)
	}
}

func TestLogNilLogger(t *testing.T) {
	var logger *Logger
	ctx := context.Background()

	// Should not panic on nil logger
	err := logger.Log(ctx, "actor", EventUserCreate, "target", nil, "127.0.0.1")
	if err != nil {
		t.Errorf("Log on nil logger should return nil, got: %v", err)
	}
}

func TestQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger, err := NewLogger(db)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	ctx := context.Background()

	// Add test data
	testEvents := []struct {
		actor     string
		action    EventType
		target    string
		ipAddress string
	}{
		{"admin@example.com", EventUserCreate, "user1@example.com", "192.168.1.1"},
		{"admin@example.com", EventUserCreate, "user2@example.com", "192.168.1.1"},
		{"admin@example.com", EventUserDelete, "user3@example.com", "192.168.1.1"},
		{"user@example.com", EventLoginSuccess, "user@example.com", "10.0.0.1"},
		{"attacker", EventLoginFailure, "admin@example.com", "1.2.3.4"},
	}

	for _, e := range testEvents {
		logger.Log(ctx, e.actor, e.action, e.target, nil, e.ipAddress)
	}

	t.Run("query all", func(t *testing.T) {
		events, err := logger.Query(ctx, QueryFilter{Limit: 100})
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if len(events) != len(testEvents) {
			t.Errorf("Query() returned %d events, want %d", len(events), len(testEvents))
		}
	})

	t.Run("query by actor", func(t *testing.T) {
		events, err := logger.Query(ctx, QueryFilter{
			Actor: "admin@example.com",
			Limit: 100,
		})
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if len(events) != 3 {
			t.Errorf("Query(actor=admin) returned %d events, want 3", len(events))
		}
	})

	t.Run("query by action", func(t *testing.T) {
		events, err := logger.Query(ctx, QueryFilter{
			Action: EventUserCreate,
			Limit:  100,
		})
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if len(events) != 2 {
			t.Errorf("Query(action=user.create) returned %d events, want 2", len(events))
		}
	})

	t.Run("query by target", func(t *testing.T) {
		events, err := logger.Query(ctx, QueryFilter{
			Target: "user1@example.com",
			Limit:  100,
		})
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if len(events) != 1 {
			t.Errorf("Query(target=user1) returned %d events, want 1", len(events))
		}
	})

	t.Run("query with limit", func(t *testing.T) {
		events, err := logger.Query(ctx, QueryFilter{Limit: 2})
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if len(events) != 2 {
			t.Errorf("Query(limit=2) returned %d events, want 2", len(events))
		}
	})

	t.Run("query combined filters", func(t *testing.T) {
		events, err := logger.Query(ctx, QueryFilter{
			Actor:  "admin@example.com",
			Action: EventUserCreate,
			Limit:  100,
		})
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		if len(events) != 2 {
			t.Errorf("Query(actor+action) returned %d events, want 2", len(events))
		}
	})
}

func TestQueryNilLogger(t *testing.T) {
	var logger *Logger
	ctx := context.Background()

	events, err := logger.Query(ctx, QueryFilter{Limit: 100})
	if err != nil {
		t.Errorf("Query on nil logger should return nil error, got: %v", err)
	}
	if events != nil {
		t.Errorf("Query on nil logger should return nil events, got: %v", events)
	}
}

func TestEventFields(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger, err := NewLogger(db)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	ctx := context.Background()

	// Log an event with all fields
	actor := "test-actor@example.com"
	action := EventUserCreate
	target := "test-target@example.com"
	details := map[string]interface{}{"key": "value", "number": 42}
	ipAddress := "192.168.1.100"

	err = logger.Log(ctx, actor, action, target, details, ipAddress)
	if err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	// Query and verify fields
	events, err := logger.Query(ctx, QueryFilter{Actor: actor, Limit: 1})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	event := events[0]

	if event.Actor != actor {
		t.Errorf("Event.Actor = %s, want %s", event.Actor, actor)
	}

	if event.Action != action {
		t.Errorf("Event.Action = %s, want %s", event.Action, action)
	}

	if event.Target != target {
		t.Errorf("Event.Target = %s, want %s", event.Target, target)
	}

	if event.IPAddress != ipAddress {
		t.Errorf("Event.IPAddress = %s, want %s", event.IPAddress, ipAddress)
	}

	if event.ID == 0 {
		t.Error("Event.ID should not be 0")
	}

	if event.Timestamp.IsZero() {
		t.Error("Event.Timestamp should not be zero")
	}

	// Timestamp should be recent (within last minute)
	if time.Since(event.Timestamp) > time.Minute {
		t.Errorf("Event.Timestamp is too old: %v", event.Timestamp)
	}
}

func TestEventTypes(t *testing.T) {
	// Verify all event types are defined correctly
	eventTypes := []EventType{
		EventUserCreate,
		EventUserDelete,
		EventUserUpdate,
		EventPasswordChange,
		EventDomainCreate,
		EventDomainDelete,
		EventLoginSuccess,
		EventLoginFailure,
		EventConfigChange,
	}

	for _, et := range eventTypes {
		if et == "" {
			t.Error("EventType should not be empty")
		}
	}
}

func TestConcurrentLogging(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger, err := NewLogger(db)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}

	ctx := context.Background()
	numGoroutines := 10
	numLogsPerGoroutine := 10

	errChan := make(chan error, numGoroutines*numLogsPerGoroutine)
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numLogsPerGoroutine; j++ {
				err := logger.Log(ctx, "actor", EventUserCreate, "target", nil, "127.0.0.1")
				if err != nil {
					errChan <- err
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
	close(errChan)

	// Check for errors
	for err := range errChan {
		t.Errorf("Concurrent Log() error = %v", err)
	}

	// Verify total count
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count: %v", err)
	}

	expected := numGoroutines * numLogsPerGoroutine
	if count != expected {
		t.Errorf("Expected %d logs, got %d", expected, count)
	}
}
