package dav

import (
	"context"
	"database/sql"
	"os"
	"testing"

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

	backend := NewCardDAVBackend(db)
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

	backend := NewCardDAVBackend(db)
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

	backend := NewCardDAVBackend(db)
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

	backend := NewCardDAVBackend(db)
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Original", "Original description")
	originalCTag := ab.CTag

	err := backend.UpdateAddressBook(ctx, ab.UID, "Updated", "Updated description")
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

	backend := NewCardDAVBackend(db)
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "To Delete", "")

	err := backend.DeleteAddressBook(ctx, ab.UID)
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

	backend := NewCardDAVBackend(db)
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

	err := backend.CreateContact(ctx, ab.UID, contact)
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

	backend := NewCardDAVBackend(db)
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

	backend := NewCardDAVBackend(db)
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	// Create multiple contacts
	names := []string{"Alice", "Bob", "Charlie", "David", "Eve"}
	for _, name := range names {
		contact := &Contact{
			UID:       generateUID(),
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

	backend := NewCardDAVBackend(db)
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
		contact := &Contact{
			UID:       generateUID(),
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

	backend := NewCardDAVBackend(db)
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

	err := backend.UpdateContact(ctx, ab.UID, contact)
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

	backend := NewCardDAVBackend(db)
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	contact := &Contact{
		UID:       "delete-contact",
		VCardData: "BEGIN:VCARD\nEND:VCARD",
		FullName:  "To Delete",
	}
	backend.CreateContact(ctx, ab.UID, contact)

	err := backend.DeleteContact(ctx, ab.UID, "delete-contact")
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

	backend := NewCardDAVBackend(db)
	ctx := context.Background()

	ab, _ := backend.CreateAddressBook(ctx, 1, "Contacts", "")

	// Create contacts
	for i := 0; i < 5; i++ {
		contact := &Contact{
			UID:       generateUID(),
			VCardData: "BEGIN:VCARD\nEND:VCARD",
			FullName:  "Contact",
		}
		backend.CreateContact(ctx, ab.UID, contact)
	}

	// Delete address book
	err := backend.DeleteAddressBook(ctx, ab.UID)
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

	backend := NewCardDAVBackend(db)
	ctx := context.Background()

	ab1, _ := backend.CreateAddressBook(ctx, 1, "Personal", "")
	ab2, _ := backend.CreateAddressBook(ctx, 1, "Work", "")

	// Add contacts to each
	for i := 0; i < 3; i++ {
		backend.CreateContact(ctx, ab1.UID, &Contact{
			UID:       generateUID(),
			VCardData: "BEGIN:VCARD\nEND:VCARD",
			FullName:  "Personal Contact",
		})
	}

	for i := 0; i < 5; i++ {
		backend.CreateContact(ctx, ab2.UID, &Contact{
			UID:       generateUID(),
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
