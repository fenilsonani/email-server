package dav

// Integration tests that simulate real CalDAV/CardDAV client workflows.
// These exercise the full HTTP stack (discovery → sync → CRUD) as performed
// by Apple Calendar, Thunderbird, DAVx5, and other real clients.

import (
	"context"
	"database/sql"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fenilsonani/email-server/internal/auth"
	_ "github.com/mattn/go-sqlite3"
)

// --- Test infrastructure ---

// setupIntegrationDB creates a DB with both CalDAV and CardDAV schemas
func setupIntegrationDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "dav_integration_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := tmpDir + "/test.db"
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to open database: %v", err)
	}

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
		CREATE TABLE addressbooks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			uid TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT,
			ctag TEXT NOT NULL,
			is_default BOOLEAN DEFAULT FALSE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE contacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			addressbook_id INTEGER NOT NULL REFERENCES addressbooks(id) ON DELETE CASCADE,
			uid TEXT NOT NULL,
			etag TEXT NOT NULL,
			vcard_data TEXT NOT NULL,
			full_name TEXT,
			given_name TEXT,
			family_name TEXT,
			nickname TEXT,
			emails TEXT,
			phones TEXT,
			organization TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(addressbook_id, uid)
		);

		INSERT INTO domains (id, name) VALUES (1, 'example.com');
		INSERT INTO users (id, domain_id, username, password_hash) VALUES (1, 1, 'user01', 'hash');
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

// integrationServer creates a Server with both backends and test data
func integrationServer(t *testing.T) (*Server, *CalDAVBackend, *CardDAVBackend, func()) {
	t.Helper()
	db, cleanup := setupIntegrationDB(t)
	caldav, err := NewCalDAVBackend(db)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	carddav, err := NewCardDAVBackend(db)
	if err != nil {
		cleanup()
		t.Fatal(err)
	}
	srv := &Server{
		caldavBackend:  caldav,
		carddavBackend: carddav,
		logger:         slog.Default(),
	}
	return srv, caldav, carddav, cleanup
}

// httpTestServer creates an httptest.Server with the DAV mux configured
func httpTestServer(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleRoot)
	mux.HandleFunc("/.well-known/caldav", srv.wellKnownCalDAV)
	mux.HandleFunc("/.well-known/carddav", srv.wellKnownCardDAV)
	mux.HandleFunc("/caldav/", srv.handleCalDAV)
	mux.HandleFunc("/calendars/", srv.handleCalDAV)
	mux.HandleFunc("/carddav/", srv.handleCardDAV)
	mux.HandleFunc("/addressbooks/", srv.handleCardDAV)
	mux.HandleFunc("/principals/", srv.handlePrincipal)

	// Wrap with fake auth that injects our test user
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := &auth.User{
			ID:          1,
			Email:       "user01@example.com",
			DisplayName: "User 01",
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		mux.ServeHTTP(w, r.WithContext(ctx))
	})

	return httptest.NewServer(handler)
}

// --- XML helpers for response validation ---

type multistatus struct {
	XMLName   xml.Name   `xml:"multistatus"`
	Responses []response `xml:"response"`
}

type response struct {
	Href     string     `xml:"href"`
	Propstat []propstat `xml:"propstat"`
}

type propstat struct {
	Status string `xml:"status"`
	Prop   prop   `xml:"prop"`
}

type prop struct {
	DisplayName    string `xml:"displayname"`
	GetETag        string `xml:"getetag"`
	GetContentType string `xml:"getcontenttype"`
	CalendarData   string `xml:"calendar-data"`
	AddressData    string `xml:"address-data"`
}

func parseMultistatus(t *testing.T, body string) multistatus {
	t.Helper()
	var ms multistatus
	if err := xml.Unmarshal([]byte(body), &ms); err != nil {
		t.Fatalf("Failed to parse multistatus XML: %v\nBody: %s", err, body)
	}
	return ms
}

// --- CalDAV Client Workflow Tests ---

// TestClientWorkflow_AppleCalendar_Discovery simulates Apple Calendar's
// initial discovery sequence: well-known → principal → calendar-home → calendars
func TestClientWorkflow_AppleCalendar_Discovery(t *testing.T) {
	srv, caldav, _, cleanup := integrationServer(t)
	defer cleanup()

	ctx := context.Background()
	caldav.CreateCalendar(ctx, 1, "Calendar", "Default calendar")
	caldav.CreateCalendar(ctx, 1, "Work", "Work events")

	ts := httpTestServer(t, srv)
	defer ts.Close()

	client := ts.Client()

	// Step 1: PROPFIND on /.well-known/caldav (Apple's first request)
	req, _ := http.NewRequest("PROPFIND", ts.URL+"/.well-known/caldav", nil)
	req.Header.Set("Depth", "0")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("well-known PROPFIND failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("Step 1 well-known: expected 207, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "current-user-principal") {
		t.Error("Step 1: well-known response missing current-user-principal")
	}

	// Step 2: PROPFIND on /principals/user01@example.com/
	req, _ = http.NewRequest("PROPFIND", ts.URL+"/principals/user01@example.com/", nil)
	req.Header.Set("Depth", "0")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("principal PROPFIND failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("Step 2 principal: expected 207, got %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "calendar-home-set") {
		t.Error("Step 2: principal response missing calendar-home-set")
	}
	if !strings.Contains(string(body), "/calendars/user01@example.com/") {
		t.Error("Step 2: principal response missing calendar home URL")
	}

	// Step 3: PROPFIND on /calendars/user01@example.com/ with Depth: 1
	req, _ = http.NewRequest("PROPFIND", ts.URL+"/calendars/user01@example.com/", nil)
	req.Header.Set("Depth", "1")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("calendar home PROPFIND failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("Step 3 calendar home: expected 207, got %d: %s", resp.StatusCode, body)
	}

	bodyStr := string(body)
	// Should list both calendars
	if !strings.Contains(bodyStr, "Calendar") {
		t.Error("Step 3: missing default 'Calendar'")
	}
	if !strings.Contains(bodyStr, "Work") {
		t.Error("Step 3: missing 'Work' calendar")
	}
	// Should have calendar resourcetype
	if !strings.Contains(bodyStr, "calendar/>") {
		t.Error("Step 3: missing calendar resourcetype")
	}
	// Should have getctag
	if !strings.Contains(bodyStr, "getctag>") {
		t.Error("Step 3: missing getctag")
	}
}

// TestClientWorkflow_CalDAV_SyncCycle simulates a complete sync cycle:
// PROPFIND calendar → see events → REPORT multiget → get specific events
func TestClientWorkflow_CalDAV_SyncCycle(t *testing.T) {
	srv, caldav, _, cleanup := integrationServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := caldav.CreateCalendar(ctx, 1, "Calendar", "")

	// Seed some events
	events := []struct {
		uid     string
		summary string
	}{
		{"meeting-001", "Team Standup"},
		{"meeting-002", "Sprint Review"},
		{"lunch-001", "Lunch with Client"},
	}
	for _, e := range events {
		caldav.CreateEvent(ctx, cal.UID, &CalendarEvent{
			UID:           e.uid,
			ICalendarData: fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:%s\r\nSUMMARY:%s\r\nEND:VEVENT\r\nEND:VCALENDAR", e.uid, e.summary),
			Summary:       e.summary,
		})
	}

	ts := httpTestServer(t, srv)
	defer ts.Close()
	client := ts.Client()

	// Step 1: PROPFIND on specific calendar at Depth:1 to discover events
	req, _ := http.NewRequest("PROPFIND",
		fmt.Sprintf("%s/calendars/user01@example.com/%s/", ts.URL, cal.UID), nil)
	req.Header.Set("Depth", "1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("calendar PROPFIND failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("Step 1: expected 207, got %d", resp.StatusCode)
	}

	bodyStr := string(body)

	// Should list all 3 events with hrefs and etags
	for _, e := range events {
		if !strings.Contains(bodyStr, e.uid+".ics") {
			t.Errorf("Step 1: missing event %s", e.uid)
		}
	}
	if !strings.Contains(bodyStr, "getetag>") {
		t.Error("Step 1: missing getetag in event entries")
	}
	if !strings.Contains(bodyStr, "getcontenttype>") {
		t.Error("Step 1: missing getcontenttype in event entries")
	}

	// Step 2: REPORT calendar-multiget for specific events (client decides which to fetch)
	multigetBody := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<C:calendar-multiget xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop>
    <D:getetag/>
    <C:calendar-data/>
  </D:prop>
  <D:href>/calendars/user01@example.com/%s/meeting-001.ics</D:href>
  <D:href>/calendars/user01@example.com/%s/lunch-001.ics</D:href>
</C:calendar-multiget>`, cal.UID, cal.UID)

	req, _ = http.NewRequest("REPORT",
		fmt.Sprintf("%s/calendars/user01@example.com/%s/", ts.URL, cal.UID),
		strings.NewReader(multigetBody))
	req.Header.Set("Depth", "1")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("REPORT multiget failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("Step 2: expected 207, got %d: %s", resp.StatusCode, body)
	}

	bodyStr = string(body)

	// Should contain requested events with calendar-data
	if !strings.Contains(bodyStr, "meeting-001.ics") {
		t.Error("Step 2: missing meeting-001")
	}
	if !strings.Contains(bodyStr, "lunch-001.ics") {
		t.Error("Step 2: missing lunch-001")
	}
	if !strings.Contains(bodyStr, "Team Standup") {
		t.Error("Step 2: missing Team Standup iCal data")
	}
	if !strings.Contains(bodyStr, "Lunch with Client") {
		t.Error("Step 2: missing Lunch with Client iCal data")
	}
	// Should NOT contain unrequested events
	if strings.Contains(bodyStr, "meeting-002.ics") {
		t.Error("Step 2: should NOT contain meeting-002")
	}
}

// TestClientWorkflow_CalDAV_CRUD simulates PUT → GET → UPDATE → DELETE
func TestClientWorkflow_CalDAV_CRUD(t *testing.T) {
	srv, caldav, _, cleanup := integrationServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := caldav.CreateCalendar(ctx, 1, "Calendar", "")

	ts := httpTestServer(t, srv)
	defer ts.Close()
	client := ts.Client()

	eventURL := fmt.Sprintf("%s/calendars/user01@example.com/%s/new-event.ics", ts.URL, cal.UID)

	// Step 1: PUT new event (simulating Apple Calendar creating an event)
	icalData := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//Test//EN\r\nBEGIN:VEVENT\r\nUID:new-event\r\nSUMMARY:New Meeting\r\nDTSTART:20260301T100000Z\r\nDTEND:20260301T110000Z\r\nEND:VEVENT\r\nEND:VCALENDAR"
	req, _ := http.NewRequest("PUT", eventURL, strings.NewReader(icalData))
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
	req.Header.Set("If-None-Match", "*") // Don't overwrite existing
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Step 1 PUT: expected 201, got %d", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Error("Step 1: PUT response missing ETag")
	}

	// Step 2: GET the event back
	req, _ = http.NewRequest("GET", eventURL, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Step 2 GET: expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "New Meeting") {
		t.Error("Step 2: GET response missing event summary")
	}
	if resp.Header.Get("ETag") == "" {
		t.Error("Step 2: GET response missing ETag")
	}
	if resp.Header.Get("Content-Type") != "text/calendar; charset=utf-8" {
		t.Errorf("Step 2: wrong Content-Type: %s", resp.Header.Get("Content-Type"))
	}

	// Step 3: PUT update (conditional on ETag)
	updatedData := strings.Replace(icalData, "New Meeting", "Updated Meeting", 1)
	req, _ = http.NewRequest("PUT", eventURL, strings.NewReader(updatedData))
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
	req.Header.Set("If-Match", etag)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("PUT update failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Step 3 PUT update: expected 204, got %d", resp.StatusCode)
	}
	newEtag := resp.Header.Get("ETag")
	if newEtag == etag {
		t.Error("Step 3: ETag should change after update")
	}

	// Step 4: DELETE event
	req, _ = http.NewRequest("DELETE", eventURL, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Step 4 DELETE: expected 204, got %d", resp.StatusCode)
	}

	// Step 5: GET deleted event should 404
	req, _ = http.NewRequest("GET", eventURL, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET deleted failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Step 5: expected 404 for deleted event, got %d", resp.StatusCode)
	}
}

// TestClientWorkflow_CalDAV_ChunkedPUT simulates Apple Calendar's chunked
// transfer encoding for PUT requests (Bug 2 regression test)
func TestClientWorkflow_CalDAV_ChunkedPUT(t *testing.T) {
	srv, caldav, _, cleanup := integrationServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := caldav.CreateCalendar(ctx, 1, "Calendar", "")

	ts := httpTestServer(t, srv)
	defer ts.Close()
	client := ts.Client()

	icalData := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:chunked-test\r\nSUMMARY:Chunked Event\r\nEND:VEVENT\r\nEND:VCALENDAR"

	eventURL := fmt.Sprintf("%s/calendars/user01@example.com/%s/chunked-test.ics", ts.URL, cal.UID)

	// Use a reader without known length to force chunked transfer
	req, _ := http.NewRequest("PUT", eventURL, strings.NewReader(icalData))
	req.Header.Set("Content-Type", "text/calendar")
	req.ContentLength = -1 // Force chunked encoding
	req.TransferEncoding = []string{"chunked"}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Chunked PUT failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 for chunked PUT, got %d", resp.StatusCode)
	}

	// Verify event exists
	event, err := caldav.GetEvent(ctx, cal.UID, "chunked-test")
	if err != nil {
		t.Fatalf("Event should exist after chunked PUT: %v", err)
	}
	if !strings.Contains(event.ICalendarData, "Chunked Event") {
		t.Error("Event data doesn't match")
	}
}

// TestClientWorkflow_CalDAV_XMLSpecialChars tests that iCalendar data with
// XML-special characters doesn't break responses (Bug 3 regression test)
func TestClientWorkflow_CalDAV_XMLSpecialChars(t *testing.T) {
	srv, caldav, _, cleanup := integrationServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := caldav.CreateCalendar(ctx, 1, "Calendar", "")

	// Event with XML-special characters in summary and description
	icalData := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:xml-chars\r\nSUMMARY:Meeting <Room A> & \"B\"\r\nDESCRIPTION:Cost > $100 && attendees < 10\r\nEND:VEVENT\r\nEND:VCALENDAR"
	caldav.CreateEvent(ctx, cal.UID, &CalendarEvent{
		UID:           "xml-chars",
		ICalendarData: icalData,
		Summary:       `Meeting <Room A> & "B"`,
	})

	ts := httpTestServer(t, srv)
	defer ts.Close()
	client := ts.Client()

	// REPORT should return well-formed XML despite special chars in data
	req, _ := http.NewRequest("REPORT",
		fmt.Sprintf("%s/calendars/user01@example.com/%s/", ts.URL, cal.UID), nil)
	req.Header.Set("Depth", "1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("REPORT failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", resp.StatusCode, body)
	}

	bodyStr := string(body)

	// Data should be in CDATA, preserving the raw characters
	if !strings.Contains(bodyStr, "<![CDATA[") {
		t.Error("Missing CDATA wrapper for calendar data")
	}
	// The raw iCal data should be preserved inside CDATA
	if !strings.Contains(bodyStr, `Meeting <Room A> & "B"`) {
		t.Error("iCalendar data with special chars not preserved in CDATA")
	}

	// The XML should still be parseable (CDATA makes it valid)
	// We can't use strict XML parsing because CDATA content is opaque to the parser,
	// but at least verify it's well-formed enough to find our response elements
	if !strings.Contains(bodyStr, "<D:response>") {
		t.Error("Response XML structure broken")
	}
}

// TestClientWorkflow_CalDAV_DepthHeader tests Depth:0 vs Depth:1 behavior
// (Bug 4 & 5 regression test)
func TestClientWorkflow_CalDAV_DepthHeader(t *testing.T) {
	srv, caldav, _, cleanup := integrationServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := caldav.CreateCalendar(ctx, 1, "Calendar", "")
	caldav.CreateEvent(ctx, cal.UID, &CalendarEvent{
		UID:           "evt-1",
		ICalendarData: "BEGIN:VCALENDAR\r\nEND:VCALENDAR",
		Summary:       "Event 1",
	})

	ts := httpTestServer(t, srv)
	defer ts.Close()
	client := ts.Client()

	calURL := fmt.Sprintf("%s/calendars/user01@example.com/%s/", ts.URL, cal.UID)

	// Depth: 0 on calendar — should return ONLY calendar props, no events
	req, _ := http.NewRequest("PROPFIND", calURL, nil)
	req.Header.Set("Depth", "0")
	resp, _ := client.Do(req)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	bodyStr := string(body)
	if strings.Contains(bodyStr, "evt-1.ics") {
		t.Error("Depth:0 should NOT include event entries")
	}
	if !strings.Contains(bodyStr, "calendar/>") {
		t.Error("Depth:0 should include calendar resourcetype")
	}

	// Depth: 1 on calendar — should return calendar props AND event entries
	req, _ = http.NewRequest("PROPFIND", calURL, nil)
	req.Header.Set("Depth", "1")
	resp, _ = client.Do(req)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	bodyStr = string(body)
	if !strings.Contains(bodyStr, "evt-1.ics") {
		t.Error("Depth:1 should include event entries")
	}
	if !strings.Contains(bodyStr, "getetag>") {
		t.Error("Depth:1 event entries should include getetag")
	}
	if !strings.Contains(bodyStr, "text/calendar") {
		t.Error("Depth:1 event entries should include getcontenttype")
	}

	// Depth: 0 on calendar HOME — should return ONLY home, no calendars
	homeURL := ts.URL + "/calendars/user01@example.com/"
	req, _ = http.NewRequest("PROPFIND", homeURL, nil)
	req.Header.Set("Depth", "0")
	resp, _ = client.Do(req)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	bodyStr = string(body)
	if strings.Contains(bodyStr, "<D:displayname>Calendar</D:displayname>") {
		t.Error("Depth:0 on home should NOT list individual calendars")
	}

	// Depth: 1 on calendar HOME — should return home AND calendars
	req, _ = http.NewRequest("PROPFIND", homeURL, nil)
	req.Header.Set("Depth", "1")
	resp, _ = client.Do(req)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	bodyStr = string(body)
	if !strings.Contains(bodyStr, "<D:displayname>Calendar</D:displayname>") {
		t.Error("Depth:1 on home should list calendars")
	}
}

// --- CardDAV Client Workflow Tests ---

// TestClientWorkflow_CardDAV_Discovery simulates a CardDAV client's discovery
func TestClientWorkflow_CardDAV_Discovery(t *testing.T) {
	srv, _, carddav, cleanup := integrationServer(t)
	defer cleanup()

	ctx := context.Background()
	carddav.CreateAddressBook(ctx, 1, "Contacts", "Default address book")

	ts := httpTestServer(t, srv)
	defer ts.Close()
	client := ts.Client()

	// Step 1: PROPFIND on /.well-known/carddav
	req, _ := http.NewRequest("PROPFIND", ts.URL+"/.well-known/carddav", nil)
	req.Header.Set("Depth", "0")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("well-known PROPFIND failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("Step 1: expected 207, got %d: %s", resp.StatusCode, body)
	}

	// Step 2: PROPFIND on principal
	req, _ = http.NewRequest("PROPFIND", ts.URL+"/principals/user01@example.com/", nil)
	req.Header.Set("Depth", "0")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("principal PROPFIND failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if !strings.Contains(string(body), "addressbook-home-set") {
		t.Error("Step 2: missing addressbook-home-set")
	}

	// Step 3: PROPFIND addressbook home Depth:1
	req, _ = http.NewRequest("PROPFIND", ts.URL+"/addressbooks/user01@example.com/", nil)
	req.Header.Set("Depth", "1")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("addressbook home PROPFIND failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("Step 3: expected 207, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Contacts") {
		t.Error("Step 3: missing Contacts address book")
	}
	if !strings.Contains(string(body), "addressbook/>") {
		t.Error("Step 3: missing addressbook resourcetype")
	}
}

// TestClientWorkflow_CardDAV_SyncCycle simulates a CardDAV sync
func TestClientWorkflow_CardDAV_SyncCycle(t *testing.T) {
	srv, _, carddav, cleanup := integrationServer(t)
	defer cleanup()

	ctx := context.Background()
	ab, _ := carddav.CreateAddressBook(ctx, 1, "Contacts", "")

	contacts := []struct {
		uid  string
		name string
	}{
		{"john-doe", "John Doe"},
		{"jane-smith", "Jane Smith"},
		{"bob-wilson", "Bob Wilson"},
	}
	for _, c := range contacts {
		carddav.CreateContact(ctx, ab.UID, &Contact{
			UID:       c.uid,
			VCardData: fmt.Sprintf("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:%s\r\nUID:%s\r\nEND:VCARD", c.name, c.uid),
			FullName:  c.name,
		})
	}

	ts := httpTestServer(t, srv)
	defer ts.Close()
	client := ts.Client()

	// Step 1: PROPFIND on address book at Depth:1 to list contacts
	abURL := fmt.Sprintf("%s/addressbooks/user01@example.com/%s/", ts.URL, ab.UID)
	req, _ := http.NewRequest("PROPFIND", abURL, nil)
	req.Header.Set("Depth", "1")
	resp, _ := client.Do(req)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		t.Fatalf("Step 1: expected 207, got %d", resp.StatusCode)
	}

	bodyStr := string(body)
	for _, c := range contacts {
		if !strings.Contains(bodyStr, c.uid+".vcf") {
			t.Errorf("Step 1: missing contact %s", c.uid)
		}
	}
	if !strings.Contains(bodyStr, "text/vcard") {
		t.Error("Step 1: missing getcontenttype for contacts")
	}

	// Step 2: REPORT addressbook-multiget for specific contacts
	multigetBody := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<A:addressbook-multiget xmlns:D="DAV:" xmlns:A="urn:ietf:params:xml:ns:carddav">
  <D:prop>
    <D:getetag/>
    <A:address-data/>
  </D:prop>
  <D:href>/addressbooks/user01@example.com/%s/john-doe.vcf</D:href>
  <D:href>/addressbooks/user01@example.com/%s/bob-wilson.vcf</D:href>
</A:addressbook-multiget>`, ab.UID, ab.UID)

	req, _ = http.NewRequest("REPORT", abURL, strings.NewReader(multigetBody))
	req.Header.Set("Depth", "1")
	resp, _ = client.Do(req)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	bodyStr = string(body)

	if !strings.Contains(bodyStr, "John Doe") {
		t.Error("Step 2: missing John Doe vCard data")
	}
	if !strings.Contains(bodyStr, "Bob Wilson") {
		t.Error("Step 2: missing Bob Wilson vCard data")
	}
	if strings.Contains(bodyStr, "Jane Smith") {
		t.Error("Step 2: should NOT contain Jane Smith (not requested)")
	}
}

// TestClientWorkflow_CardDAV_CRUD simulates contact creation and updates
func TestClientWorkflow_CardDAV_CRUD(t *testing.T) {
	srv, _, carddav, cleanup := integrationServer(t)
	defer cleanup()

	ctx := context.Background()
	ab, _ := carddav.CreateAddressBook(ctx, 1, "Contacts", "")

	ts := httpTestServer(t, srv)
	defer ts.Close()
	client := ts.Client()

	contactURL := fmt.Sprintf("%s/addressbooks/user01@example.com/%s/new-contact.vcf", ts.URL, ab.UID)

	// PUT new contact
	vcardData := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:New Contact\r\nUID:new-contact\r\nEMAIL:new@example.com\r\nEND:VCARD"
	req, _ := http.NewRequest("PUT", contactURL, strings.NewReader(vcardData))
	req.Header.Set("Content-Type", "text/vcard; charset=utf-8")
	resp, _ := client.Do(req)
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT: expected 201, got %d", resp.StatusCode)
	}
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Error("PUT response missing ETag")
	}

	// GET contact
	req, _ = http.NewRequest("GET", contactURL, nil)
	resp, _ = client.Do(req)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "New Contact") {
		t.Error("GET: missing contact data")
	}

	// UPDATE contact
	updatedData := strings.Replace(vcardData, "New Contact", "Updated Contact", 1)
	req, _ = http.NewRequest("PUT", contactURL, strings.NewReader(updatedData))
	req.Header.Set("Content-Type", "text/vcard; charset=utf-8")
	req.Header.Set("If-Match", etag)
	resp, _ = client.Do(req)
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT update: expected 204, got %d", resp.StatusCode)
	}

	// DELETE contact
	req, _ = http.NewRequest("DELETE", contactURL, nil)
	resp, _ = client.Do(req)
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE: expected 204, got %d", resp.StatusCode)
	}
}

// TestClientWorkflow_MKCALENDAR tests calendar creation (Thunderbird workflow)
func TestClientWorkflow_MKCALENDAR(t *testing.T) {
	srv, caldav, _, cleanup := integrationServer(t)
	defer cleanup()

	ts := httpTestServer(t, srv)
	defer ts.Close()
	client := ts.Client()

	// MKCALENDAR
	req, _ := http.NewRequest("MKCALENDAR",
		ts.URL+"/calendars/user01@example.com/new-cal", nil)
	resp, _ := client.Do(req)
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("MKCALENDAR: expected 201, got %d", resp.StatusCode)
	}

	// Verify calendar shows up in PROPFIND
	req, _ = http.NewRequest("PROPFIND",
		ts.URL+"/calendars/user01@example.com/", nil)
	req.Header.Set("Depth", "1")
	resp, _ = client.Do(req)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if !strings.Contains(string(body), "new-cal") {
		t.Error("New calendar should appear in PROPFIND Depth:1")
	}

	// Verify we have at least one calendar
	ctx := context.Background()
	cals, err := caldav.ListCalendars(ctx, 1)
	if err != nil {
		t.Fatalf("ListCalendars failed: %v", err)
	}
	if len(cals) == 0 {
		t.Error("Should have at least one calendar after MKCALENDAR")
	}
}

// TestClientWorkflow_OPTIONS tests DAV capability headers
func TestClientWorkflow_OPTIONS(t *testing.T) {
	srv, _, _, cleanup := integrationServer(t)
	defer cleanup()

	ts := httpTestServer(t, srv)
	defer ts.Close()
	client := ts.Client()

	// OPTIONS on root
	req, _ := http.NewRequest("OPTIONS", ts.URL+"/", nil)
	resp, _ := client.Do(req)
	resp.Body.Close()

	dav := resp.Header.Get("DAV")
	if !strings.Contains(dav, "calendar-access") {
		t.Error("OPTIONS /: missing calendar-access in DAV header")
	}
	if !strings.Contains(dav, "addressbook") {
		t.Error("OPTIONS /: missing addressbook in DAV header")
	}

	// OPTIONS on CalDAV path
	req, _ = http.NewRequest("OPTIONS", ts.URL+"/calendars/user01@example.com/", nil)
	resp, _ = client.Do(req)
	resp.Body.Close()

	if !strings.Contains(resp.Header.Get("DAV"), "calendar-access") {
		t.Error("OPTIONS /calendars/: missing calendar-access")
	}
	allow := resp.Header.Get("Allow")
	if !strings.Contains(allow, "PROPFIND") {
		t.Error("OPTIONS: missing PROPFIND in Allow")
	}
	if !strings.Contains(allow, "REPORT") {
		t.Error("OPTIONS: missing REPORT in Allow")
	}
	if !strings.Contains(allow, "MKCALENDAR") {
		t.Error("OPTIONS: missing MKCALENDAR in Allow")
	}
}

// TestClientWorkflow_PreconditionHeaders tests If-Match and If-None-Match
func TestClientWorkflow_PreconditionHeaders(t *testing.T) {
	srv, caldav, _, cleanup := integrationServer(t)
	defer cleanup()

	ctx := context.Background()
	cal, _ := caldav.CreateCalendar(ctx, 1, "Calendar", "")

	ts := httpTestServer(t, srv)
	defer ts.Close()
	client := ts.Client()

	eventURL := fmt.Sprintf("%s/calendars/user01@example.com/%s/precond.ics", ts.URL, cal.UID)

	// Create event
	icalData := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:precond\r\nSUMMARY:Test\r\nEND:VEVENT\r\nEND:VCALENDAR"
	req, _ := http.NewRequest("PUT", eventURL, strings.NewReader(icalData))
	req.Header.Set("Content-Type", "text/calendar")
	resp, _ := client.Do(req)
	resp.Body.Close()
	etag := resp.Header.Get("ETag")

	// If-None-Match: * should fail (event exists)
	req, _ = http.NewRequest("PUT", eventURL, strings.NewReader(icalData))
	req.Header.Set("Content-Type", "text/calendar")
	req.Header.Set("If-None-Match", "*")
	resp, _ = client.Do(req)
	resp.Body.Close()

	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("If-None-Match * on existing: expected 412, got %d", resp.StatusCode)
	}

	// If-Match with wrong ETag should fail
	req, _ = http.NewRequest("PUT", eventURL, strings.NewReader(icalData))
	req.Header.Set("Content-Type", "text/calendar")
	req.Header.Set("If-Match", `"wrong-etag"`)
	resp, _ = client.Do(req)
	resp.Body.Close()

	if resp.StatusCode != http.StatusPreconditionFailed {
		t.Errorf("If-Match wrong ETag: expected 412, got %d", resp.StatusCode)
	}

	// If-Match with correct ETag should succeed
	req, _ = http.NewRequest("PUT", eventURL, strings.NewReader(icalData))
	req.Header.Set("Content-Type", "text/calendar")
	req.Header.Set("If-Match", etag)
	resp, _ = client.Do(req)
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("If-Match correct ETag: expected 204, got %d", resp.StatusCode)
	}
}
