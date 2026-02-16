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

	"github.com/fenilsonani/email-server/internal/auth"
	_ "github.com/mattn/go-sqlite3"
)

func setupCardDAVTestDB(t *testing.T) (*sql.DB, func()) {
	tmpDir, err := os.MkdirTemp("", "carddav_test_*")
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

func TestCardDAVBackend_CreateAddressBook(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	ab, err := backend.CreateAddressBook(ctx, 1, "Personal Contacts", "My personal contacts")
	if err != nil {
		t.Fatalf("CreateAddressBook failed: %v", err)
	}

	if ab.Name != "Personal Contacts" {
		t.Errorf("Expected name 'Personal Contacts', got '%s'", ab.Name)
	}

	if ab.Description != "My personal contacts" {
		t.Errorf("Expected description 'My personal contacts', got '%s'", ab.Description)
	}

	if ab.UID == "" {
		t.Error("Expected non-empty UID")
	}

	if ab.CTag == "" {
		t.Error("Expected non-empty CTag")
	}
}

func TestCardDAVBackend_GetAddressBook(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	created, _ := backend.CreateAddressBook(ctx, 1, "Test Contacts", "")

	ab, err := backend.GetAddressBook(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetAddressBook failed: %v", err)
	}

	if ab.Name != "Test Contacts" {
		t.Errorf("Expected name 'Test Contacts', got '%s'", ab.Name)
	}

	// Non-existent
	_, err = backend.GetAddressBook(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent address book")
	}
}

func TestCardDAVBackend_ListAddressBooks(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	backend.CreateAddressBook(ctx, 1, "Personal", "")
	backend.CreateAddressBook(ctx, 1, "Work", "")
	backend.CreateAddressBook(ctx, 1, "Family", "")

	addressBooks, err := backend.ListAddressBooks(ctx, 1)
	if err != nil {
		t.Fatalf("ListAddressBooks failed: %v", err)
	}

	if len(addressBooks) != 3 {
		t.Errorf("Expected 3 address books, got %d", len(addressBooks))
	}
}

func TestCardDAVBackend_UpdateAddressBook(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Original", "Original description")
	originalCTag := ab.CTag

	err = backend.UpdateAddressBook(ctx, ab.UID, "Updated", "Updated description")
	if err != nil {
		t.Fatalf("UpdateAddressBook failed: %v", err)
	}

	updated, _ := backend.GetAddressBook(ctx, ab.UID)
	if updated.Name != "Updated" {
		t.Errorf("Expected name 'Updated', got '%s'", updated.Name)
	}

	if updated.CTag == originalCTag {
		t.Error("Expected CTag to change after update")
	}
}

func TestCardDAVBackend_DeleteAddressBook(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "To Delete", "")

	err = backend.DeleteAddressBook(ctx, ab.UID)
	if err != nil {
		t.Fatalf("DeleteAddressBook failed: %v", err)
	}

	_, err = backend.GetAddressBook(ctx, ab.UID)
	if err == nil {
		t.Error("Expected error for deleted address book")
	}
}

func TestCardDAVBackend_CreateContact(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	contact := &Contact{
		UID:          "contact-123",
		VCardData:    "BEGIN:VCARD\nVERSION:3.0\nFN:John Doe\nEND:VCARD",
		FullName:     "John Doe",
		GivenName:    "John",
		FamilyName:   "Doe",
		Emails:       `["john@example.com"]`,
		Phones:       `["+1234567890"]`,
		Organization: "ACME Corp",
	}

	err = backend.CreateContact(ctx, ab.UID, contact)
	if err != nil {
		t.Fatalf("CreateContact failed: %v", err)
	}

	if contact.ETag == "" {
		t.Error("Expected non-empty ETag")
	}
}

func TestCardDAVBackend_GetContact(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	contact := &Contact{
		UID:        "contact-456",
		VCardData:  "BEGIN:VCARD\nVERSION:3.0\nFN:Jane Smith\nEND:VCARD",
		FullName:   "Jane Smith",
		GivenName:  "Jane",
		FamilyName: "Smith",
	}
	backend.CreateContact(ctx, ab.UID, contact)

	retrieved, err := backend.GetContact(ctx, ab.UID, "contact-456")
	if err != nil {
		t.Fatalf("GetContact failed: %v", err)
	}

	if retrieved.FullName != "Jane Smith" {
		t.Errorf("Expected full name 'Jane Smith', got '%s'", retrieved.FullName)
	}

	if retrieved.GivenName != "Jane" {
		t.Errorf("Expected given name 'Jane', got '%s'", retrieved.GivenName)
	}

	// Non-existent
	_, err = backend.GetContact(ctx, ab.UID, "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent contact")
	}
}

func TestCardDAVBackend_ListContacts(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	// Create multiple contacts
	names := []string{"Alice", "Bob", "Charlie", "David", "Eve"}
	for _, name := range names {
		uid, _ := generateUID()
		contact := &Contact{
			UID:       uid,
			VCardData: "BEGIN:VCARD\nEND:VCARD",
			FullName:  name,
		}
		backend.CreateContact(ctx, ab.UID, contact)
	}

	contacts, err := backend.ListContacts(ctx, ab.UID)
	if err != nil {
		t.Fatalf("ListContacts failed: %v", err)
	}

	if len(contacts) != 5 {
		t.Errorf("Expected 5 contacts, got %d", len(contacts))
	}
}

func TestCardDAVBackend_SearchContacts(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	// Create contacts
	contacts := []struct {
		name   string
		emails string
	}{
		{"John Doe", `["john@example.com"]`},
		{"Jane Doe", `["jane@example.com"]`},
		{"Bob Smith", `["bob@company.com"]`},
		{"Alice Johnson", `["alice@example.com"]`},
	}

	for _, c := range contacts {
		uid, _ := generateUID()
		contact := &Contact{
			UID:       uid,
			VCardData: "BEGIN:VCARD\nEND:VCARD",
			FullName:  c.name,
			Emails:    c.emails,
		}
		backend.CreateContact(ctx, ab.UID, contact)
	}

	// Search by name
	results, err := backend.SearchContacts(ctx, ab.UID, "Doe")
	if err != nil {
		t.Fatalf("SearchContacts failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 contacts matching 'Doe', got %d", len(results))
	}

	// Search by email domain
	results, _ = backend.SearchContacts(ctx, ab.UID, "example.com")
	if len(results) != 3 {
		t.Errorf("Expected 3 contacts with example.com email, got %d", len(results))
	}
}

func TestCardDAVBackend_UpdateContact(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	contact := &Contact{
		UID:       "update-contact",
		VCardData: "BEGIN:VCARD\nEND:VCARD",
		FullName:  "Original Name",
	}
	backend.CreateContact(ctx, ab.UID, contact)

	original, _ := backend.GetContact(ctx, ab.UID, "update-contact")
	originalETag := original.ETag

	// Update
	contact.FullName = "Updated Name"
	contact.VCardData = "BEGIN:VCARD\nUPDATED\nEND:VCARD"

	err = backend.UpdateContact(ctx, ab.UID, contact)
	if err != nil {
		t.Fatalf("UpdateContact failed: %v", err)
	}

	updated, _ := backend.GetContact(ctx, ab.UID, "update-contact")
	if updated.FullName != "Updated Name" {
		t.Errorf("Expected full name 'Updated Name', got '%s'", updated.FullName)
	}

	if updated.ETag == originalETag {
		t.Error("Expected ETag to change after update")
	}
}

func TestCardDAVBackend_DeleteContact(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	contact := &Contact{
		UID:       "delete-contact",
		VCardData: "BEGIN:VCARD\nEND:VCARD",
		FullName:  "To Delete",
	}
	backend.CreateContact(ctx, ab.UID, contact)

	err = backend.DeleteContact(ctx, ab.UID, "delete-contact")
	if err != nil {
		t.Fatalf("DeleteContact failed: %v", err)
	}

	_, err = backend.GetContact(ctx, ab.UID, "delete-contact")
	if err == nil {
		t.Error("Expected error for deleted contact")
	}
}

func TestCardDAVBackend_CascadeDelete(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	// Create contacts
	for i := 0; i < 5; i++ {
		uid, _ := generateUID()
		contact := &Contact{
			UID:       uid,
			VCardData: "BEGIN:VCARD\nEND:VCARD",
			FullName:  "Contact",
		}
		backend.CreateContact(ctx, ab.UID, contact)
	}

	// Delete address book
	err = backend.DeleteAddressBook(ctx, ab.UID)
	if err != nil {
		t.Fatalf("DeleteAddressBook failed: %v", err)
	}

	// Contacts should be deleted too
	contacts, _ := backend.ListContacts(ctx, ab.UID)
	if len(contacts) != 0 {
		t.Errorf("Expected 0 contacts after address book deletion, got %d", len(contacts))
	}
}

func TestCardDAVBackend_MultipleAddressBooks(t *testing.T) {
	db, cleanup := setupCardDAVTestDB(t)
	defer cleanup()

	backend, err := NewCardDAVBackend(db)
	if err != nil {
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	ctx := context.Background()

	ab1, _ := backend.CreateAddressBook(ctx, 1, "Personal", "")
	ab2, _ := backend.CreateAddressBook(ctx, 1, "Work", "")

	// Add contacts to each
	for i := 0; i < 3; i++ {
		uid, _ := generateUID()
		backend.CreateContact(ctx, ab1.UID, &Contact{
			UID:       uid,
			VCardData: "BEGIN:VCARD\nEND:VCARD",
			FullName:  "Personal Contact",
		})
	}

	for i := 0; i < 5; i++ {
		uid, _ := generateUID()
		backend.CreateContact(ctx, ab2.UID, &Contact{
			UID:       uid,
			VCardData: "BEGIN:VCARD\nEND:VCARD",
			FullName:  "Work Contact",
		})
	}

	// Verify isolation
	personalContacts, _ := backend.ListContacts(ctx, ab1.UID)
	workContacts, _ := backend.ListContacts(ctx, ab2.UID)

	if len(personalContacts) != 3 {
		t.Errorf("Expected 3 personal contacts, got %d", len(personalContacts))
	}

	if len(workContacts) != 5 {
		t.Errorf("Expected 5 work contacts, got %d", len(workContacts))
	}
}

// --- HTTP-level tests ---

func newTestCardDAVServer(t *testing.T) (*Server, *CardDAVBackend, func()) {
	db, cleanup := setupCardDAVTestDB(t)
	backend, err := NewCardDAVBackend(db)
	if err != nil {
		cleanup()
		t.Fatalf("NewCardDAVBackend failed: %v", err)
	}
	srv := &Server{
		carddavBackend: backend,
		logger:         slog.Default(),
	}
	return srv, backend, cleanup
}

func cardDAVTestUser() *auth.User {
	return &auth.User{
		ID:          1,
		Email:       "testuser@test.com",
		DisplayName: "Test User",
	}
}

func cardDAVRequest(method, path string, depth string, body string) *http.Request {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if depth != "" {
		r.Header.Set("Depth", depth)
	}
	user := cardDAVTestUser()
	ctx := context.WithValue(r.Context(), userContextKey, user)
	return r.WithContext(ctx)
}

func TestHTTP_CardDAV_PropfindHome_Depth0(t *testing.T) {
	srv, backend, cleanup := newTestCardDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	backend.CreateAddressBook(ctx, 1, "Personal", "Personal contacts")

	r := cardDAVRequest("PROPFIND", "/addressbooks/testuser@test.com/", "0", "")
	w := httptest.NewRecorder()
	srv.handleCardDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "<D:displayname>Address Books</D:displayname>") {
		t.Error("Expected home collection displayname")
	}
	if strings.Contains(body, "<D:displayname>Personal</D:displayname>") {
		t.Error("Depth 0 should NOT include individual address books")
	}
}

func TestHTTP_CardDAV_PropfindHome_Depth1(t *testing.T) {
	srv, backend, cleanup := newTestCardDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	backend.CreateAddressBook(ctx, 1, "Personal", "")
	backend.CreateAddressBook(ctx, 1, "Work", "")

	r := cardDAVRequest("PROPFIND", "/addressbooks/testuser@test.com/", "1", "")
	w := httptest.NewRecorder()
	srv.handleCardDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "<D:displayname>Personal</D:displayname>") {
		t.Error("Expected address book 'Personal' in Depth 1 response")
	}
	if !strings.Contains(body, "<D:displayname>Work</D:displayname>") {
		t.Error("Expected address book 'Work' in Depth 1 response")
	}
	if !strings.Contains(body, "<A:addressbook/>") {
		t.Error("Expected addressbook resourcetype in response")
	}
}

func TestHTTP_CardDAV_PropfindAddressBook_Depth1_ReturnsContacts(t *testing.T) {
	srv, backend, cleanup := newTestCardDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	ab, _ := backend.CreateAddressBook(ctx, 1, "Personal", "")

	contact := &Contact{
		UID:       "contact-abc",
		VCardData: "BEGIN:VCARD\nVERSION:3.0\nFN:John Doe\nEND:VCARD",
		FullName:  "John Doe",
	}
	backend.CreateContact(ctx, ab.UID, contact)

	path := fmt.Sprintf("/addressbooks/testuser@test.com/%s/", ab.UID)
	r := cardDAVRequest("PROPFIND", path, "1", "")
	w := httptest.NewRecorder()
	srv.handleCardDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "<D:displayname>Personal</D:displayname>") {
		t.Error("Expected address book displayname")
	}
	if !strings.Contains(body, "contact-abc.vcf") {
		t.Error("Expected contact href in Depth 1 response")
	}
	if !strings.Contains(body, "<D:getetag>") {
		t.Error("Expected getetag in contact entry")
	}
	if !strings.Contains(body, "<D:getcontenttype>text/vcard") {
		t.Error("Expected getcontenttype in contact entry")
	}
}

func TestHTTP_CardDAV_PropfindAddressBook_Depth0_NoContacts(t *testing.T) {
	srv, backend, cleanup := newTestCardDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	ab, _ := backend.CreateAddressBook(ctx, 1, "Personal", "")

	contact := &Contact{
		UID:       "contact-xyz",
		VCardData: "BEGIN:VCARD\nEND:VCARD",
		FullName:  "Test",
	}
	backend.CreateContact(ctx, ab.UID, contact)

	path := fmt.Sprintf("/addressbooks/testuser@test.com/%s/", ab.UID)
	r := cardDAVRequest("PROPFIND", path, "0", "")
	w := httptest.NewRecorder()
	srv.handleCardDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "<D:displayname>Personal</D:displayname>") {
		t.Error("Expected address book displayname")
	}
	if strings.Contains(body, "contact-xyz.vcf") {
		t.Error("Depth 0 should NOT include contact entries")
	}
}

func TestHTTP_CardDAV_Report_Multiget(t *testing.T) {
	srv, backend, cleanup := newTestCardDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	for i := 0; i < 5; i++ {
		contact := &Contact{
			UID:       fmt.Sprintf("contact-%d", i),
			VCardData: fmt.Sprintf("BEGIN:VCARD\nVERSION:3.0\nFN:Person %d\nEND:VCARD", i),
			FullName:  fmt.Sprintf("Person %d", i),
		}
		backend.CreateContact(ctx, ab.UID, contact)
	}

	path := fmt.Sprintf("/addressbooks/testuser@test.com/%s/", ab.UID)
	multigetBody := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<A:addressbook-multiget xmlns:D="DAV:" xmlns:A="urn:ietf:params:xml:ns:carddav">
  <D:prop><D:getetag/><A:address-data/></D:prop>
  <D:href>/addressbooks/testuser@test.com/%s/contact-0.vcf</D:href>
  <D:href>/addressbooks/testuser@test.com/%s/contact-2.vcf</D:href>
</A:addressbook-multiget>`, ab.UID, ab.UID)

	r := cardDAVRequest("REPORT", path, "1", multigetBody)
	w := httptest.NewRecorder()
	srv.handleCardDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "contact-0.vcf") {
		t.Error("Expected contact-0 in multiget response")
	}
	if !strings.Contains(body, "contact-2.vcf") {
		t.Error("Expected contact-2 in multiget response")
	}
	if strings.Contains(body, "contact-1.vcf") {
		t.Error("Should NOT include contact-1 in multiget response")
	}
	if strings.Contains(body, "contact-3.vcf") {
		t.Error("Should NOT include contact-3 in multiget response")
	}
}

func TestHTTP_CardDAV_Report_XMLEscaping(t *testing.T) {
	srv, backend, cleanup := newTestCardDAVServer(t)
	defer cleanup()

	ctx := context.Background()
	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	vcardData := "BEGIN:VCARD\nVERSION:3.0\nFN:O'Brien & Sons <LLC>\nEND:VCARD"
	contact := &Contact{
		UID:       "special-contact",
		VCardData: vcardData,
		FullName:  "O'Brien & Sons <LLC>",
	}
	backend.CreateContact(ctx, ab.UID, contact)

	path := fmt.Sprintf("/addressbooks/testuser@test.com/%s/", ab.UID)
	r := cardDAVRequest("REPORT", path, "1", "")
	w := httptest.NewRecorder()
	srv.handleCardDAV(w, r)

	if w.Code != http.StatusMultiStatus {
		t.Fatalf("Expected 207, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "<![CDATA[") {
		t.Error("Expected CDATA wrapping for vCard data with special chars")
	}
	if !strings.Contains(body, "O'Brien & Sons <LLC>") {
		t.Error("Expected raw vCard data preserved inside CDATA")
	}
}
