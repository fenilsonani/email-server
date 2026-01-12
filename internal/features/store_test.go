package features

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// setupTestDB creates a test database with the features schema
func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "features_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()

	db, err := sql.Open("sqlite3", tmpFile.Name()+"?_foreign_keys=on")
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to open database: %v", err)
	}

	// Create schema
	schema := `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain TEXT NOT NULL UNIQUE
		);

		CREATE TABLE messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subject TEXT
		);

		CREATE TABLE mailboxes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			name TEXT NOT NULL
		);

		CREATE TABLE screener_contacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			email TEXT,
			domain TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			CHECK (email IS NOT NULL OR domain IS NOT NULL),
			UNIQUE(user_id, email),
			UNIQUE(user_id, domain)
		);

		CREATE TABLE email_aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
			alias_address TEXT NOT NULL UNIQUE,
			alias_local TEXT NOT NULL,
			description TEXT,
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME,
			email_count INTEGER DEFAULT 0,
			UNIQUE(domain_id, alias_local)
		);

		CREATE TABLE scheduled_emails (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			send_at DATETIME NOT NULL,
			from_address TEXT NOT NULL,
			recipients TEXT NOT NULL,
			subject TEXT,
			body TEXT,
			html_body TEXT,
			headers TEXT,
			status TEXT DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			sent_at DATETIME,
			error TEXT
		);

		CREATE TABLE snoozed_emails (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			original_mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
			wake_at DATETIME NOT NULL,
			mark_unread BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(message_id)
		);

		CREATE TABLE vip_contacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			email TEXT NOT NULL,
			name TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, email)
		);

		CREATE TABLE user_preferences (
			user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
			undo_send_delay INTEGER DEFAULT 10,
			screener_enabled BOOLEAN DEFAULT TRUE,
			tracker_blocking TEXT DEFAULT 'block',
			zones_enabled BOOLEAN DEFAULT TRUE,
			snooze_mark_unread BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE pending_sends (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			cancel_token TEXT NOT NULL UNIQUE,
			from_address TEXT NOT NULL,
			recipients TEXT NOT NULL,
			subject TEXT,
			body TEXT,
			html_body TEXT,
			headers TEXT,
			send_after DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		-- Insert test data
		INSERT INTO users (id, username) VALUES (1, 'testuser');
		INSERT INTO domains (id, domain) VALUES (1, 'example.com');
		INSERT INTO messages (id, subject) VALUES (1, 'Test message');
		INSERT INTO mailboxes (id, user_id, name) VALUES (1, 1, 'INBOX');
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to create schema: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}

	return db, cleanup
}

// =============================================================================
// Screener Tests
// =============================================================================

func TestScreener_GetStatus_Unknown(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	status, err := store.GetScreenerStatus(ctx, 1, "unknown@example.com")
	if err != nil {
		t.Fatalf("GetScreenerStatus failed: %v", err)
	}
	if status != ScreenerPending {
		t.Errorf("Expected pending status for unknown sender, got %s", status)
	}
}

func TestScreener_ApproveContact(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Approve a contact
	err := store.ApproveContact(ctx, 1, "friend@example.com", "")
	if err != nil {
		t.Fatalf("ApproveContact failed: %v", err)
	}

	// Verify status
	status, err := store.GetScreenerStatus(ctx, 1, "friend@example.com")
	if err != nil {
		t.Fatalf("GetScreenerStatus failed: %v", err)
	}
	if status != ScreenerApproved {
		t.Errorf("Expected approved status, got %s", status)
	}
}

func TestScreener_BlockContact(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Block a contact
	err := store.BlockContact(ctx, 1, "spammer@example.com", "")
	if err != nil {
		t.Fatalf("BlockContact failed: %v", err)
	}

	// Verify status
	status, err := store.GetScreenerStatus(ctx, 1, "spammer@example.com")
	if err != nil {
		t.Fatalf("GetScreenerStatus failed: %v", err)
	}
	if status != ScreenerBlocked {
		t.Errorf("Expected blocked status, got %s", status)
	}
}

func TestScreener_ApproveDomain(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Approve entire domain
	err := store.ApproveContact(ctx, 1, "", "trusted.com")
	if err != nil {
		t.Fatalf("ApproveContact failed: %v", err)
	}

	// Verify any email from domain is approved
	status, err := store.GetScreenerStatus(ctx, 1, "anyone@trusted.com")
	if err != nil {
		t.Fatalf("GetScreenerStatus failed: %v", err)
	}
	if status != ScreenerApproved {
		t.Errorf("Expected approved status for domain, got %s", status)
	}
}

func TestScreener_EmailOverridesDomain(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Approve domain but block specific email
	store.ApproveContact(ctx, 1, "", "company.com")
	store.BlockContact(ctx, 1, "spammer@company.com", "")

	// Specific email should be blocked
	status, err := store.GetScreenerStatus(ctx, 1, "spammer@company.com")
	if err != nil {
		t.Fatalf("GetScreenerStatus failed: %v", err)
	}
	if status != ScreenerBlocked {
		t.Errorf("Expected blocked status for specific email, got %s", status)
	}

	// Other emails from domain should be approved
	status, err = store.GetScreenerStatus(ctx, 1, "other@company.com")
	if err != nil {
		t.Fatalf("GetScreenerStatus failed: %v", err)
	}
	if status != ScreenerApproved {
		t.Errorf("Expected approved status for domain, got %s", status)
	}
}

func TestScreener_ListContacts(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Add contacts
	store.ApproveContact(ctx, 1, "friend1@example.com", "")
	store.ApproveContact(ctx, 1, "friend2@example.com", "")
	store.BlockContact(ctx, 1, "spammer@example.com", "")

	// List all
	contacts, err := store.ListScreenerContacts(ctx, 1, "")
	if err != nil {
		t.Fatalf("ListScreenerContacts failed: %v", err)
	}
	if len(contacts) != 3 {
		t.Errorf("Expected 3 contacts, got %d", len(contacts))
	}

	// List approved only
	approved, err := store.ListScreenerContacts(ctx, 1, ScreenerApproved)
	if err != nil {
		t.Fatalf("ListScreenerContacts failed: %v", err)
	}
	if len(approved) != 2 {
		t.Errorf("Expected 2 approved contacts, got %d", len(approved))
	}

	// List blocked only
	blocked, err := store.ListScreenerContacts(ctx, 1, ScreenerBlocked)
	if err != nil {
		t.Fatalf("ListScreenerContacts failed: %v", err)
	}
	if len(blocked) != 1 {
		t.Errorf("Expected 1 blocked contact, got %d", len(blocked))
	}
}

func TestScreener_DeleteContact(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Add and then delete
	store.ApproveContact(ctx, 1, "temp@example.com", "")

	contacts, _ := store.ListScreenerContacts(ctx, 1, "")
	if len(contacts) == 0 {
		t.Fatal("Expected at least 1 contact")
	}

	err := store.DeleteScreenerContact(ctx, 1, contacts[0].ID)
	if err != nil {
		t.Fatalf("DeleteScreenerContact failed: %v", err)
	}

	// Verify deleted
	contacts, _ = store.ListScreenerContacts(ctx, 1, "")
	if len(contacts) != 0 {
		t.Errorf("Expected 0 contacts after delete, got %d", len(contacts))
	}
}

// =============================================================================
// Alias Tests
// =============================================================================

func TestAlias_Create(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	alias := &EmailAlias{
		UserID:       1,
		DomainID:     1,
		AliasLocal:   "shop_abc123",
		AliasAddress: "shop_abc123@example.com",
		Description:  "Amazon shopping",
	}

	err := store.CreateAlias(ctx, alias)
	if err != nil {
		t.Fatalf("CreateAlias failed: %v", err)
	}

	if alias.ID == 0 {
		t.Error("Expected alias ID to be set")
	}
	if !alias.IsActive {
		t.Error("Expected alias to be active by default")
	}
}

func TestAlias_GetByAddress(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Create alias
	alias := &EmailAlias{
		UserID:       1,
		DomainID:     1,
		AliasLocal:   "test_xyz",
		AliasAddress: "test_xyz@example.com",
		Description:  "Test alias",
	}
	store.CreateAlias(ctx, alias)

	// Find by address
	found, err := store.GetAliasByAddress(ctx, "test_xyz@example.com")
	if err != nil {
		t.Fatalf("GetAliasByAddress failed: %v", err)
	}
	if found.ID != alias.ID {
		t.Errorf("Expected alias ID %d, got %d", alias.ID, found.ID)
	}
	if found.Description != "Test alias" {
		t.Errorf("Expected description 'Test alias', got '%s'", found.Description)
	}

	// Not found
	_, err = store.GetAliasByAddress(ctx, "nonexistent@example.com")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestAlias_ListAliases(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Create multiple aliases
	for i := 0; i < 3; i++ {
		alias := &EmailAlias{
			UserID:       1,
			DomainID:     1,
			AliasLocal:   GenerateAliasLocal("test"),
			AliasAddress: GenerateAliasLocal("test") + "@example.com",
		}
		store.CreateAlias(ctx, alias)
	}

	aliases, err := store.ListAliases(ctx, 1)
	if err != nil {
		t.Fatalf("ListAliases failed: %v", err)
	}
	if len(aliases) != 3 {
		t.Errorf("Expected 3 aliases, got %d", len(aliases))
	}
}

func TestAlias_Update(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	alias := &EmailAlias{
		UserID:       1,
		DomainID:     1,
		AliasLocal:   "update_test",
		AliasAddress: "update_test@example.com",
	}
	store.CreateAlias(ctx, alias)

	// Disable alias
	inactive := false
	err := store.UpdateAlias(ctx, 1, alias.ID, &inactive, nil)
	if err != nil {
		t.Fatalf("UpdateAlias failed: %v", err)
	}

	// Verify
	found, _ := store.GetAliasByAddress(ctx, "update_test@example.com")
	if found.IsActive {
		t.Error("Expected alias to be inactive")
	}

	// Update description
	newDesc := "Updated description"
	store.UpdateAlias(ctx, 1, alias.ID, nil, &newDesc)
	found, _ = store.GetAliasByAddress(ctx, "update_test@example.com")
	if found.Description != newDesc {
		t.Errorf("Expected description '%s', got '%s'", newDesc, found.Description)
	}
}

func TestAlias_Delete(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	alias := &EmailAlias{
		UserID:       1,
		DomainID:     1,
		AliasLocal:   "delete_me",
		AliasAddress: "delete_me@example.com",
	}
	store.CreateAlias(ctx, alias)

	err := store.DeleteAlias(ctx, 1, alias.ID)
	if err != nil {
		t.Fatalf("DeleteAlias failed: %v", err)
	}

	_, err = store.GetAliasByAddress(ctx, "delete_me@example.com")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound after delete, got %v", err)
	}
}

func TestAlias_IncrementCount(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	alias := &EmailAlias{
		UserID:       1,
		DomainID:     1,
		AliasLocal:   "counter_test",
		AliasAddress: "counter_test@example.com",
	}
	store.CreateAlias(ctx, alias)

	// Increment multiple times
	for i := 0; i < 5; i++ {
		store.IncrementAliasCount(ctx, alias.ID)
	}

	found, _ := store.GetAliasByAddress(ctx, "counter_test@example.com")
	if found.EmailCount != 5 {
		t.Errorf("Expected email count 5, got %d", found.EmailCount)
	}
	if found.LastUsedAt.IsZero() {
		t.Error("Expected last_used_at to be set")
	}
}

func TestGenerateAliasLocal(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		wantLen  int
		contains string
	}{
		{"no prefix", "", 8, ""},
		{"with prefix", "shop", 13, "shop_"},
		{"prefix with spaces", "my alias", 17, "my_alias_"},
		{"uppercase prefix", "SHOP", 13, "shop_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateAliasLocal(tt.prefix)
			if len(result) != tt.wantLen {
				t.Errorf("Expected length %d, got %d (%s)", tt.wantLen, len(result), result)
			}
			if tt.contains != "" && !contains(result, tt.contains) {
				t.Errorf("Expected result to contain '%s', got '%s'", tt.contains, result)
			}
		})
	}

	// Test uniqueness
	results := make(map[string]bool)
	for i := 0; i < 100; i++ {
		r := GenerateAliasLocal("")
		if results[r] {
			t.Errorf("Generated duplicate alias: %s", r)
		}
		results[r] = true
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr
}

// =============================================================================
// VIP Tests
// =============================================================================

func TestVIP_AddAndList(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	vip := &VIPContact{
		UserID: 1,
		Email:  "boss@company.com",
		Name:   "The Boss",
	}

	err := store.AddVIP(ctx, vip)
	if err != nil {
		t.Fatalf("AddVIP failed: %v", err)
	}

	if vip.ID == 0 {
		t.Error("Expected VIP ID to be set")
	}

	vips, err := store.ListVIPs(ctx, 1)
	if err != nil {
		t.Fatalf("ListVIPs failed: %v", err)
	}
	if len(vips) != 1 {
		t.Errorf("Expected 1 VIP, got %d", len(vips))
	}
	if vips[0].Email != "boss@company.com" {
		t.Errorf("Expected email 'boss@company.com', got '%s'", vips[0].Email)
	}
}

func TestVIP_IsVIP(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	store.AddVIP(ctx, &VIPContact{UserID: 1, Email: "vip@example.com"})

	isVIP, err := store.IsVIP(ctx, 1, "vip@example.com")
	if err != nil {
		t.Fatalf("IsVIP failed: %v", err)
	}
	if !isVIP {
		t.Error("Expected true for VIP email")
	}

	isVIP, _ = store.IsVIP(ctx, 1, "notavip@example.com")
	if isVIP {
		t.Error("Expected false for non-VIP email")
	}
}

func TestVIP_Delete(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	vip := &VIPContact{UserID: 1, Email: "temp@example.com"}
	store.AddVIP(ctx, vip)

	err := store.DeleteVIP(ctx, 1, vip.ID)
	if err != nil {
		t.Fatalf("DeleteVIP failed: %v", err)
	}

	isVIP, _ := store.IsVIP(ctx, 1, "temp@example.com")
	if isVIP {
		t.Error("Expected false after VIP deletion")
	}
}

func TestVIP_DuplicatePrevented(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	store.AddVIP(ctx, &VIPContact{UserID: 1, Email: "dupe@example.com"})
	err := store.AddVIP(ctx, &VIPContact{UserID: 1, Email: "dupe@example.com"})
	if err != ErrAlreadyExists {
		t.Errorf("Expected ErrAlreadyExists, got %v", err)
	}
}

// =============================================================================
// Preferences Tests
// =============================================================================

func TestPreferences_DefaultCreation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Get preferences for user without existing preferences
	prefs, err := store.GetPreferences(ctx, 1)
	if err != nil {
		t.Fatalf("GetPreferences failed: %v", err)
	}

	// Verify defaults
	if prefs.UndoSendDelay != 10 {
		t.Errorf("Expected UndoSendDelay 10, got %d", prefs.UndoSendDelay)
	}
	if !prefs.ScreenerEnabled {
		t.Error("Expected ScreenerEnabled true")
	}
	if prefs.TrackerBlocking != "block" {
		t.Errorf("Expected TrackerBlocking 'block', got '%s'", prefs.TrackerBlocking)
	}
	if !prefs.ZonesEnabled {
		t.Error("Expected ZonesEnabled true")
	}
	if !prefs.SnoozeMarkUnread {
		t.Error("Expected SnoozeMarkUnread true")
	}
}

func TestPreferences_SaveAndRetrieve(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	prefs := &UserPreferences{
		UserID:          1,
		UndoSendDelay:   30,
		ScreenerEnabled: false,
		TrackerBlocking: "proxy",
		ZonesEnabled:    false,
		SnoozeMarkUnread: false,
	}

	err := store.SavePreferences(ctx, prefs)
	if err != nil {
		t.Fatalf("SavePreferences failed: %v", err)
	}

	retrieved, err := store.GetPreferences(ctx, 1)
	if err != nil {
		t.Fatalf("GetPreferences failed: %v", err)
	}

	if retrieved.UndoSendDelay != 30 {
		t.Errorf("Expected UndoSendDelay 30, got %d", retrieved.UndoSendDelay)
	}
	if retrieved.ScreenerEnabled {
		t.Error("Expected ScreenerEnabled false")
	}
	if retrieved.TrackerBlocking != "proxy" {
		t.Errorf("Expected TrackerBlocking 'proxy', got '%s'", retrieved.TrackerBlocking)
	}
}

// =============================================================================
// Scheduled Email Tests
// =============================================================================

func TestScheduledEmail_Create(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	email := &ScheduledEmail{
		UserID:      1,
		SendAt:      time.Now().Add(time.Hour),
		FromAddress: "test@example.com",
		Recipients:  []string{"recipient@example.com"},
		Subject:     "Test Subject",
		Body:        "Test body",
	}

	err := store.CreateScheduledEmail(ctx, email)
	if err != nil {
		t.Fatalf("CreateScheduledEmail failed: %v", err)
	}

	if email.ID == 0 {
		t.Error("Expected email ID to be set")
	}
	if email.Status != ScheduledStatusPending {
		t.Errorf("Expected status 'pending', got '%s'", email.Status)
	}
}

func TestScheduledEmail_GetPending(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Create one due and one future
	store.CreateScheduledEmail(ctx, &ScheduledEmail{
		UserID:      1,
		SendAt:      time.Now().Add(-time.Minute), // Due now
		FromAddress: "test@example.com",
		Recipients:  []string{"r@example.com"},
		Subject:     "Due",
	})
	store.CreateScheduledEmail(ctx, &ScheduledEmail{
		UserID:      1,
		SendAt:      time.Now().Add(time.Hour), // Future
		FromAddress: "test@example.com",
		Recipients:  []string{"r@example.com"},
		Subject:     "Future",
	})

	pending, err := store.GetPendingScheduledEmails(ctx)
	if err != nil {
		t.Fatalf("GetPendingScheduledEmails failed: %v", err)
	}

	if len(pending) != 1 {
		t.Errorf("Expected 1 pending email, got %d", len(pending))
	}
	if pending[0].Subject != "Due" {
		t.Errorf("Expected subject 'Due', got '%s'", pending[0].Subject)
	}
}

func TestScheduledEmail_UpdateStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	email := &ScheduledEmail{
		UserID:      1,
		SendAt:      time.Now().Add(time.Hour),
		FromAddress: "test@example.com",
		Recipients:  []string{"r@example.com"},
	}
	store.CreateScheduledEmail(ctx, email)

	err := store.UpdateScheduledEmailStatus(ctx, email.ID, ScheduledStatusSent, "")
	if err != nil {
		t.Fatalf("UpdateScheduledEmailStatus failed: %v", err)
	}

	retrieved, _ := store.GetScheduledEmail(ctx, 1, email.ID)
	if retrieved.Status != ScheduledStatusSent {
		t.Errorf("Expected status 'sent', got '%s'", retrieved.Status)
	}
}

func TestScheduledEmail_Cancel(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	email := &ScheduledEmail{
		UserID:      1,
		SendAt:      time.Now().Add(time.Hour),
		FromAddress: "test@example.com",
		Recipients:  []string{"r@example.com"},
	}
	store.CreateScheduledEmail(ctx, email)

	err := store.CancelScheduledEmail(ctx, 1, email.ID)
	if err != nil {
		t.Fatalf("CancelScheduledEmail failed: %v", err)
	}

	retrieved, _ := store.GetScheduledEmail(ctx, 1, email.ID)
	if retrieved.Status != ScheduledStatusCancelled {
		t.Errorf("Expected status 'cancelled', got '%s'", retrieved.Status)
	}
}

// =============================================================================
// Pending Sends (Undo Send) Tests
// =============================================================================

func TestPendingSend_CreateAndCancel(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	pending := &PendingSend{
		UserID:      1,
		CancelToken: "cancel_abc123",
		FromAddress: "test@example.com",
		Recipients:  []string{"r@example.com"},
		Subject:     "Test",
		SendAfter:   time.Now().Add(10 * time.Second),
	}

	err := store.CreatePendingSend(ctx, pending)
	if err != nil {
		t.Fatalf("CreatePendingSend failed: %v", err)
	}

	// Cancel it
	err = store.CancelPendingSend(ctx, 1, "cancel_abc123")
	if err != nil {
		t.Fatalf("CancelPendingSend failed: %v", err)
	}

	// Try to cancel again - should fail
	err = store.CancelPendingSend(ctx, 1, "cancel_abc123")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound on second cancel, got %v", err)
	}
}

func TestPendingSend_GetReady(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Create one ready and one not ready
	store.CreatePendingSend(ctx, &PendingSend{
		UserID:      1,
		CancelToken: "ready",
		FromAddress: "test@example.com",
		Recipients:  []string{"r@example.com"},
		SendAfter:   time.Now().Add(-time.Minute), // Ready
	})
	store.CreatePendingSend(ctx, &PendingSend{
		UserID:      1,
		CancelToken: "notready",
		FromAddress: "test@example.com",
		Recipients:  []string{"r@example.com"},
		SendAfter:   time.Now().Add(time.Minute), // Not ready
	})

	ready, err := store.GetReadyPendingSends(ctx)
	if err != nil {
		t.Fatalf("GetReadyPendingSends failed: %v", err)
	}

	if len(ready) != 1 {
		t.Errorf("Expected 1 ready send, got %d", len(ready))
	}
	if ready[0].CancelToken != "ready" {
		t.Errorf("Expected token 'ready', got '%s'", ready[0].CancelToken)
	}
}

// =============================================================================
// Snoozed Email Tests
// =============================================================================

func TestSnoozedEmail_CreateAndList(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	snooze := &SnoozedEmail{
		UserID:            1,
		MessageID:         1,
		OriginalMailboxID: 1,
		WakeAt:            time.Now().Add(time.Hour),
		MarkUnread:        true,
	}

	err := store.SnoozeEmail(ctx, snooze)
	if err != nil {
		t.Fatalf("SnoozeEmail failed: %v", err)
	}

	snoozed, err := store.ListSnoozedEmails(ctx, 1)
	if err != nil {
		t.Fatalf("ListSnoozedEmails failed: %v", err)
	}
	if len(snoozed) != 1 {
		t.Errorf("Expected 1 snoozed email, got %d", len(snoozed))
	}
}

func TestSnoozedEmail_GetReady(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	// Create ready snooze
	store.SnoozeEmail(ctx, &SnoozedEmail{
		UserID:            1,
		MessageID:         1,
		OriginalMailboxID: 1,
		WakeAt:            time.Now().Add(-time.Minute),
	})

	ready, err := store.GetReadySnoozedEmails(ctx)
	if err != nil {
		t.Fatalf("GetReadySnoozedEmails failed: %v", err)
	}
	if len(ready) != 1 {
		t.Errorf("Expected 1 ready snoozed email, got %d", len(ready))
	}
}

func TestSnoozedEmail_Delete(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	snooze := &SnoozedEmail{
		UserID:            1,
		MessageID:         1,
		OriginalMailboxID: 1,
		WakeAt:            time.Now().Add(time.Hour),
	}
	store.SnoozeEmail(ctx, snooze)

	err := store.DeleteSnoozedEmail(ctx, 1, snooze.ID)
	if err != nil {
		t.Fatalf("DeleteSnoozedEmail failed: %v", err)
	}

	snoozed, _ := store.ListSnoozedEmails(ctx, 1)
	if len(snoozed) != 0 {
		t.Errorf("Expected 0 snoozed emails after delete, got %d", len(snoozed))
	}
}

func TestSnoozedEmail_DuplicatePrevented(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	ctx := context.Background()

	store.SnoozeEmail(ctx, &SnoozedEmail{
		UserID:            1,
		MessageID:         1,
		OriginalMailboxID: 1,
		WakeAt:            time.Now().Add(time.Hour),
	})

	// Try to snooze same message again
	err := store.SnoozeEmail(ctx, &SnoozedEmail{
		UserID:            1,
		MessageID:         1,
		OriginalMailboxID: 1,
		WakeAt:            time.Now().Add(2 * time.Hour),
	})
	if err != ErrAlreadyExists {
		t.Errorf("Expected ErrAlreadyExists for duplicate snooze, got %v", err)
	}
}
