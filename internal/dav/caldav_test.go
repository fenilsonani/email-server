package dav

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func setupCalDAVTestDB(t *testing.T) (*sql.DB, func()) {
	tmpDir, err := os.MkdirTemp("", "caldav_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := tmpDir + "/test.db"
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to open database: %v", err)
	}

	// Create schema
	schema := `
		CREATE TABLE domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		);

		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL REFERENCES domains(id),
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			UNIQUE(domain_id, username)
		);

		CREATE TABLE calendars (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			uid TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT,
			color TEXT DEFAULT '#0066CC',
			timezone TEXT DEFAULT 'UTC',
			ctag TEXT NOT NULL,
			is_default BOOLEAN DEFAULT FALSE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE calendar_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			calendar_id INTEGER NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
			uid TEXT NOT NULL,
			etag TEXT NOT NULL,
			icalendar_data TEXT NOT NULL,
			summary TEXT,
			description TEXT,
			location TEXT,
			start_time DATETIME,
			end_time DATETIME,
			all_day BOOLEAN DEFAULT FALSE,
			recurrence_rule TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(calendar_id, uid)
		);

		INSERT INTO domains (id, name) VALUES (1, 'test.com');
		INSERT INTO users (id, domain_id, username, password_hash) VALUES (1, 1, 'testuser', 'hash');
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create schema: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

func TestCalDAVBackend_CreateCalendar(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	// Create calendar
	cal, err := backend.CreateCalendar(ctx, 1, "Work Calendar", "My work events")
	if err != nil {
		t.Fatalf("CreateCalendar failed: %v", err)
	}

	if cal.Name != "Work Calendar" {
		t.Errorf("Expected name 'Work Calendar', got '%s'", cal.Name)
	}

	if cal.Description != "My work events" {
		t.Errorf("Expected description 'My work events', got '%s'", cal.Description)
	}

	if cal.UID == "" {
		t.Error("Expected non-empty UID")
	}

	if cal.CTag == "" {
		t.Error("Expected non-empty CTag")
	}

	if cal.UserID != 1 {
		t.Errorf("Expected UserID 1, got %d", cal.UserID)
	}
}

func TestCalDAVBackend_GetCalendar(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	// Create calendar first
	created, err := backend.CreateCalendar(ctx, 1, "Test Calendar", "")
	if err != nil {
		t.Fatalf("CreateCalendar failed: %v", err)
	}

	// Get calendar
	cal, err := backend.GetCalendar(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetCalendar failed: %v", err)
	}

	if cal.Name != "Test Calendar" {
		t.Errorf("Expected name 'Test Calendar', got '%s'", cal.Name)
	}

	if cal.ID != created.ID {
		t.Errorf("Expected ID %d, got %d", created.ID, cal.ID)
	}

	// Test non-existent calendar
	_, err = backend.GetCalendar(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent calendar")
	}
}

func TestCalDAVBackend_ListCalendars(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	// Create multiple calendars
	backend.CreateCalendar(ctx, 1, "Calendar 1", "")
	backend.CreateCalendar(ctx, 1, "Calendar 2", "")
	backend.CreateCalendar(ctx, 1, "Calendar 3", "")

	// List calendars
	calendars, err := backend.ListCalendars(ctx, 1)
	if err != nil {
		t.Fatalf("ListCalendars failed: %v", err)
	}

	if len(calendars) != 3 {
		t.Errorf("Expected 3 calendars, got %d", len(calendars))
	}
}

func TestCalDAVBackend_UpdateCalendar(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	// Create calendar
	cal, _ := backend.CreateCalendar(ctx, 1, "Original Name", "Original description")
	originalCTag := cal.CTag

	// Update calendar
	err := backend.UpdateCalendar(ctx, cal.UID, "New Name", "New description", "#FF0000")
	if err != nil {
		t.Fatalf("UpdateCalendar failed: %v", err)
	}

	// Verify update
	updated, _ := backend.GetCalendar(ctx, cal.UID)
	if updated.Name != "New Name" {
		t.Errorf("Expected name 'New Name', got '%s'", updated.Name)
	}

	if updated.Description != "New description" {
		t.Errorf("Expected description 'New description', got '%s'", updated.Description)
	}

	if updated.Color != "#FF0000" {
		t.Errorf("Expected color '#FF0000', got '%s'", updated.Color)
	}

	if updated.CTag == originalCTag {
		t.Error("Expected CTag to change after update")
	}
}

func TestCalDAVBackend_DeleteCalendar(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	// Create calendar
	cal, _ := backend.CreateCalendar(ctx, 1, "To Delete", "")

	// Delete calendar
	err := backend.DeleteCalendar(ctx, cal.UID)
	if err != nil {
		t.Fatalf("DeleteCalendar failed: %v", err)
	}

	// Verify deletion
	_, err = backend.GetCalendar(ctx, cal.UID)
	if err == nil {
		t.Error("Expected error for deleted calendar")
	}

	// Delete non-existent should error
	err = backend.DeleteCalendar(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent calendar")
	}
}

func TestCalDAVBackend_CreateEvent(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	// Create calendar first
	cal, _ := backend.CreateCalendar(ctx, 1, "Events Calendar", "")

	// Create event
	event := &CalendarEvent{
		UID:           "event-123",
		ICalendarData: "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:event-123\nSUMMARY:Test Event\nEND:VEVENT\nEND:VCALENDAR",
		Summary:       "Test Event",
		Description:   "A test event",
		Location:      "Office",
		StartTime:     time.Now(),
		EndTime:       time.Now().Add(time.Hour),
		AllDay:        false,
	}

	err := backend.CreateEvent(ctx, cal.UID, event)
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}

	if event.ETag == "" {
		t.Error("Expected non-empty ETag")
	}
}

func TestCalDAVBackend_GetEvent(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	cal, _ := backend.CreateCalendar(ctx, 1, "Events Calendar", "")

	event := &CalendarEvent{
		UID:           "event-456",
		ICalendarData: "BEGIN:VCALENDAR\nEND:VCALENDAR",
		Summary:       "Get Test Event",
		Location:      "Home",
	}
	backend.CreateEvent(ctx, cal.UID, event)

	// Get event
	retrieved, err := backend.GetEvent(ctx, cal.UID, "event-456")
	if err != nil {
		t.Fatalf("GetEvent failed: %v", err)
	}

	if retrieved.Summary != "Get Test Event" {
		t.Errorf("Expected summary 'Get Test Event', got '%s'", retrieved.Summary)
	}

	if retrieved.Location != "Home" {
		t.Errorf("Expected location 'Home', got '%s'", retrieved.Location)
	}

	// Get non-existent event
	_, err = backend.GetEvent(ctx, cal.UID, "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent event")
	}
}

func TestCalDAVBackend_ListEvents(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	cal, _ := backend.CreateCalendar(ctx, 1, "Events Calendar", "")

	// Create multiple events
	for i := 1; i <= 5; i++ {
		event := &CalendarEvent{
			UID:           generateUID(),
			ICalendarData: "BEGIN:VCALENDAR\nEND:VCALENDAR",
			Summary:       "Event",
			StartTime:     time.Now().Add(time.Duration(i) * time.Hour),
			EndTime:       time.Now().Add(time.Duration(i+1) * time.Hour),
		}
		backend.CreateEvent(ctx, cal.UID, event)
	}

	// List events
	events, err := backend.ListEvents(ctx, cal.UID)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}

	if len(events) != 5 {
		t.Errorf("Expected 5 events, got %d", len(events))
	}
}

func TestCalDAVBackend_UpdateEvent(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	cal, _ := backend.CreateCalendar(ctx, 1, "Events Calendar", "")

	event := &CalendarEvent{
		UID:           "update-event",
		ICalendarData: "BEGIN:VCALENDAR\nEND:VCALENDAR",
		Summary:       "Original Summary",
	}
	backend.CreateEvent(ctx, cal.UID, event)

	original, _ := backend.GetEvent(ctx, cal.UID, "update-event")
	originalETag := original.ETag

	// Update event
	event.Summary = "Updated Summary"
	event.ICalendarData = "BEGIN:VCALENDAR\nUPDATED\nEND:VCALENDAR"

	err := backend.UpdateEvent(ctx, cal.UID, event)
	if err != nil {
		t.Fatalf("UpdateEvent failed: %v", err)
	}

	// Verify update
	updated, _ := backend.GetEvent(ctx, cal.UID, "update-event")
	if updated.Summary != "Updated Summary" {
		t.Errorf("Expected summary 'Updated Summary', got '%s'", updated.Summary)
	}

	if updated.ETag == originalETag {
		t.Error("Expected ETag to change after update")
	}
}

func TestCalDAVBackend_DeleteEvent(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	cal, _ := backend.CreateCalendar(ctx, 1, "Events Calendar", "")

	event := &CalendarEvent{
		UID:           "delete-event",
		ICalendarData: "BEGIN:VCALENDAR\nEND:VCALENDAR",
		Summary:       "To Delete",
	}
	backend.CreateEvent(ctx, cal.UID, event)

	// Delete event
	err := backend.DeleteEvent(ctx, cal.UID, "delete-event")
	if err != nil {
		t.Fatalf("DeleteEvent failed: %v", err)
	}

	// Verify deletion
	_, err = backend.GetEvent(ctx, cal.UID, "delete-event")
	if err == nil {
		t.Error("Expected error for deleted event")
	}
}

func TestCalDAVBackend_ListEventsInRange(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	cal, _ := backend.CreateCalendar(ctx, 1, "Events Calendar", "")

	baseTime := time.Now()

	// Create events at different times
	for i := 0; i < 10; i++ {
		event := &CalendarEvent{
			UID:           generateUID(),
			ICalendarData: "BEGIN:VCALENDAR\nEND:VCALENDAR",
			Summary:       "Event",
			StartTime:     baseTime.Add(time.Duration(i*24) * time.Hour),
			EndTime:       baseTime.Add(time.Duration(i*24+1) * time.Hour),
		}
		backend.CreateEvent(ctx, cal.UID, event)
	}

	// Query range (days 2-5)
	start := baseTime.Add(48 * time.Hour)
	end := baseTime.Add(120 * time.Hour)

	events, err := backend.ListEventsInRange(ctx, cal.UID, start, end)
	if err != nil {
		t.Fatalf("ListEventsInRange failed: %v", err)
	}

	// Should get events for days 2, 3, 4 (3 events)
	if len(events) < 1 {
		t.Errorf("Expected at least 1 event in range, got %d", len(events))
	}
}

func TestCalDAVBackend_CascadeDelete(t *testing.T) {
	db, cleanup := setupCalDAVTestDB(t)
	defer cleanup()

	backend := NewCalDAVBackend(db)
	ctx := context.Background()

	cal, _ := backend.CreateCalendar(ctx, 1, "Events Calendar", "")

	// Create events
	for i := 0; i < 5; i++ {
		event := &CalendarEvent{
			UID:           generateUID(),
			ICalendarData: "BEGIN:VCALENDAR\nEND:VCALENDAR",
			Summary:       "Event",
		}
		backend.CreateEvent(ctx, cal.UID, event)
	}

	// Delete calendar
	err := backend.DeleteCalendar(ctx, cal.UID)
	if err != nil {
		t.Fatalf("DeleteCalendar failed: %v", err)
	}

	// Events should be deleted too (cascade)
	events, _ := backend.ListEvents(ctx, cal.UID)
	if len(events) != 0 {
		t.Errorf("Expected 0 events after calendar deletion, got %d", len(events))
	}
}
