package dav

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/auth"
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	// Create calendar
	cal, _ := backend.CreateCalendar(ctx, 1, "Original Name", "Original description")
	originalCTag := cal.CTag

	// Update calendar
	err = backend.UpdateCalendar(ctx, cal.UID, "New Name", "New description", "#FF0000")
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	// Create calendar
	cal, _ := backend.CreateCalendar(ctx, 1, "To Delete", "")

	// Delete calendar
	err = backend.DeleteCalendar(ctx, cal.UID)
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
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

	err = backend.CreateEvent(ctx, cal.UID, event)
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	cal, _ := backend.CreateCalendar(ctx, 1, "Events Calendar", "")

	// Create multiple events
	for i := 1; i <= 5; i++ {
		uid, _ := generateUID()
		event := &CalendarEvent{
			UID:           uid,
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
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

	err = backend.UpdateEvent(ctx, cal.UID, event)
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	cal, _ := backend.CreateCalendar(ctx, 1, "Events Calendar", "")

	event := &CalendarEvent{
		UID:           "delete-event",
		ICalendarData: "BEGIN:VCALENDAR\nEND:VCALENDAR",
		Summary:       "To Delete",
	}
	backend.CreateEvent(ctx, cal.UID, event)

	// Delete event
	err = backend.DeleteEvent(ctx, cal.UID, "delete-event")
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	cal, _ := backend.CreateCalendar(ctx, 1, "Events Calendar", "")

	baseTime := time.Now()

	// Create events at different times
	for i := 0; i < 10; i++ {
		uid, _ := generateUID()
		event := &CalendarEvent{
			UID:           uid,
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

	backend, err := NewCalDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	cal, _ := backend.CreateCalendar(ctx, 1, "Events Calendar", "")

	// Create events
	for i := 0; i < 5; i++ {
		uid, _ := generateUID()
		event := &CalendarEvent{
			UID:           uid,
			ICalendarData: "BEGIN:VCALENDAR\nEND:VCALENDAR",
			Summary:       "Event",
		}
		backend.CreateEvent(ctx, cal.UID, event)
	}

	// Delete calendar
	err = backend.DeleteCalendar(ctx, cal.UID)
	if err != nil {
		t.Fatalf("DeleteCalendar failed: %v", err)
	}

	// Events should be deleted too (cascade)
	events, _ := backend.ListEvents(ctx, cal.UID)
	if len(events) != 0 {
		t.Errorf("Expected 0 events after calendar deletion, got %d", len(events))
	}
}

// --- HTTP-level tests ---

func newTestCalDAVServer(t *testing.T) (*Server, *CalDAVBackend, func()) {
	db, cleanup := setupCalDAVTestDB(t)
	backend, err := NewCalDAVBackend(db)
	if err != nil {
		cleanup()
		t.Fatalf("NewCalDAVBackend failed: %v", err)
	}
	srv := &Server{
		caldavBackend: backend,
		logger:        slog.Default(),
	}
	return srv, backend, cleanup
}

func testUser() *auth.User {
	return &auth.User{
		ID:          1,
		Email:       "testuser@test.com",
		DisplayName: "Test User",
	}
}

func calDAVRequest(method, path string, depth string, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if depth != "" {
		r.Header.Set("Depth", depth)
	}
	user := testUser()
	ctx := context.WithValue(r.Context(), userContextKey, user)
	return r.WithContext(ctx)
}

func TestHTTP_CalDAV_PropfindHome_Depth0(t *testing.T) {
	srv, backend, cleanup := newTestCalDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	backend.CreateCalendar(ctx, 1, "Work", "Work events")

	r := calDAVRequest("PROPFIND", "/calendars/testuser@test.com/", "0", "")
	w := httptest.NewRecorder()
	srv.handleCalDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// Depth 0: should include the home collection but NOT individual calendars
	if !strings.Contains(body, "<D:displayname>Calendars</D:displayname>") {
		t.Error("Expected home collection displayname 'Calendars'")
	}
	if strings.Contains(body, "<D:displayname>Work</D:displayname>") {
		t.Error("Depth 0 should NOT include individual calendars")
	}
}

func TestHTTP_CalDAV_PropfindHome_Depth1(t *testing.T) {
	srv, backend, cleanup := newTestCalDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	backend.CreateCalendar(ctx, 1, "Work", "Work events")
	backend.CreateCalendar(ctx, 1, "Personal", "")

	r := calDAVRequest("PROPFIND", "/calendars/testuser@test.com/", "1", "")
	w := httptest.NewRecorder()
	srv.handleCalDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "<D:displayname>Work</D:displayname>") {
		t.Error("Expected calendar 'Work' in Depth 1 response")
	}
	if !strings.Contains(body, "<D:displayname>Personal</D:displayname>") {
		t.Error("Expected calendar 'Personal' in Depth 1 response")
	}
	if !strings.Contains(body, "<C:calendar/>") {
		t.Error("Expected calendar resourcetype in response")
	}
}

func TestHTTP_CalDAV_PropfindCalendar_Depth1_ReturnsEvents(t *testing.T) {
	srv, backend, cleanup := newTestCalDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := backend.CreateCalendar(ctx, 1, "Work", "")

	event := &CalendarEvent{
		UID:           "event-abc",
		ICalendarData: "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:event-abc\nSUMMARY:Meeting\nEND:VEVENT\nEND:VCALENDAR",
		Summary:       "Meeting",
		StartTime:     time.Now(),
		EndTime:       time.Now().Add(time.Hour),
	}
	backend.CreateEvent(ctx, cal.UID, event)

	path := fmt.Sprintf("/calendars/testuser@test.com/%s/", cal.UID)
	r := calDAVRequest("PROPFIND", path, "1", "")
	w := httptest.NewRecorder()
	srv.handleCalDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// Should include calendar props
	if !strings.Contains(body, "<D:displayname>Work</D:displayname>") {
		t.Error("Expected calendar displayname in response")
	}
	// Should include event entry with ETag and content type
	if !strings.Contains(body, "event-abc.ics") {
		t.Error("Expected event href in Depth 1 response")
	}
	if !strings.Contains(body, "<D:getetag>") {
		t.Error("Expected getetag in event entry")
	}
	if !strings.Contains(body, "<D:getcontenttype>text/calendar") {
		t.Error("Expected getcontenttype in event entry")
	}
}

func TestHTTP_CalDAV_PropfindCalendar_Depth0_NoEvents(t *testing.T) {
	srv, backend, cleanup := newTestCalDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := backend.CreateCalendar(ctx, 1, "Work", "")

	event := &CalendarEvent{
		UID:           "event-xyz",
		ICalendarData: "BEGIN:VCALENDAR\nEND:VCALENDAR",
		Summary:       "Event",
	}
	backend.CreateEvent(ctx, cal.UID, event)

	path := fmt.Sprintf("/calendars/testuser@test.com/%s/", cal.UID)
	r := calDAVRequest("PROPFIND", path, "0", "")
	w := httptest.NewRecorder()
	srv.handleCalDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "<D:displayname>Work</D:displayname>") {
		t.Error("Expected calendar displayname")
	}
	if strings.Contains(body, "event-xyz.ics") {
		t.Error("Depth 0 should NOT include event entries")
	}
}

func TestHTTP_CalDAV_Report_CalendarQuery(t *testing.T) {
	srv, backend, cleanup := newTestCalDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := backend.CreateCalendar(ctx, 1, "Work", "")

	for i := 0; i < 3; i++ {
		event := &CalendarEvent{
			UID:           fmt.Sprintf("event-%d", i),
			ICalendarData: "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nEND:VEVENT\nEND:VCALENDAR",
			Summary:       fmt.Sprintf("Event %d", i),
		}
		backend.CreateEvent(ctx, cal.UID, event)
	}

	path := fmt.Sprintf("/calendars/testuser@test.com/%s/", cal.UID)
	reportBody := `<?xml version="1.0" encoding="UTF-8"?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/><C:calendar-data/></D:prop>
</C:calendar-query>`

	r := calDAVRequest("REPORT", path, "1", reportBody)
	w := httptest.NewRecorder()
	srv.handleCalDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// Should return all 3 events
	for i := 0; i < 3; i++ {
		if !strings.Contains(body, fmt.Sprintf("event-%d.ics", i)) {
			t.Errorf("Expected event-%d in calendar-query response", i)
		}
	}
	// Should use CDATA for calendar data
	if !strings.Contains(body, "<![CDATA[") {
		t.Error("Expected CDATA wrapping for calendar data")
	}
}

func TestHTTP_CalDAV_Report_Multiget(t *testing.T) {
	srv, backend, cleanup := newTestCalDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := backend.CreateCalendar(ctx, 1, "Work", "")

	for i := 0; i < 5; i++ {
		event := &CalendarEvent{
			UID:           fmt.Sprintf("event-%d", i),
			ICalendarData: "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nEND:VEVENT\nEND:VCALENDAR",
			Summary:       fmt.Sprintf("Event %d", i),
		}
		backend.CreateEvent(ctx, cal.UID, event)
	}

	path := fmt.Sprintf("/calendars/testuser@test.com/%s/", cal.UID)
	// Request only events 1 and 3
	multigetBody := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<C:calendar-multiget xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop><D:getetag/><C:calendar-data/></D:prop>
  <D:href>/calendars/testuser@test.com/%s/event-1.ics</D:href>
  <D:href>/calendars/testuser@test.com/%s/event-3.ics</D:href>
</C:calendar-multiget>`, cal.UID, cal.UID)

	r := calDAVRequest("REPORT", path, "1", multigetBody)
	w := httptest.NewRecorder()
	srv.handleCalDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// Should return only events 1 and 3
	if !strings.Contains(body, "event-1.ics") {
		t.Error("Expected event-1 in multiget response")
	}
	if !strings.Contains(body, "event-3.ics") {
		t.Error("Expected event-3 in multiget response")
	}
	// Should NOT return events 0, 2, 4
	if strings.Contains(body, "event-0.ics") {
		t.Error("Should NOT include event-0 in multiget response")
	}
	if strings.Contains(body, "event-2.ics") {
		t.Error("Should NOT include event-2 in multiget response")
	}
	if strings.Contains(body, "event-4.ics") {
		t.Error("Should NOT include event-4 in multiget response")
	}
}

func TestHTTP_CalDAV_Put_ChunkedTransfer(t *testing.T) {
	srv, backend, cleanup := newTestCalDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := backend.CreateCalendar(ctx, 1, "Work", "")

	icalData := "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:chunked-event\nSUMMARY:Chunked\nEND:VEVENT\nEND:VCALENDAR"
	path := fmt.Sprintf("/calendars/testuser@test.com/%s/chunked-event.ics", cal.UID)
	r := calDAVRequest("PUT", path, "", icalData)
	r.Header.Set("Content-Type", "text/calendar")
	// Simulate chunked transfer by setting ContentLength to -1
	r.ContentLength = -1

	w := httptest.NewRecorder()
	srv.handleCalDAV(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify event was created
	event, err := backend.GetEvent(ctx, cal.UID, "chunked-event")
	if err != nil {
		t.Fatalf("Event should have been created: %v", err)
	}
	if event.ICalendarData != icalData {
		t.Error("Event data mismatch")
	}
}

func TestHTTP_CalDAV_Report_XMLEscaping(t *testing.T) {
	srv, backend, cleanup := newTestCalDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := backend.CreateCalendar(ctx, 1, "Work", "")

	// Create event with XML-special characters in data
	icalData := "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nUID:xml-event\nSUMMARY:Meeting <Room A> & B\nEND:VEVENT\nEND:VCALENDAR"
	event := &CalendarEvent{
		UID:           "xml-event",
		ICalendarData: icalData,
		Summary:       "Meeting <Room A> & B",
	}
	backend.CreateEvent(ctx, cal.UID, event)

	path := fmt.Sprintf("/calendars/testuser@test.com/%s/", cal.UID)
	r := calDAVRequest("REPORT", path, "1", "")
	w := httptest.NewRecorder()
	srv.handleCalDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	// CDATA should wrap the raw data, preserving special chars
	if !strings.Contains(body, "<![CDATA[") {
		t.Error("Expected CDATA wrapping for calendar data with special chars")
	}
	if !strings.Contains(body, "Meeting <Room A> & B") {
		t.Error("Expected raw iCalendar data preserved inside CDATA")
	}
}
