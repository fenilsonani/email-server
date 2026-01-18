package smtp

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/fenilsonani/email-server/internal/features"
	_ "github.com/mattn/go-sqlite3"
)

// setupFeaturesTestDB creates a test database with the features schema
func setupFeaturesTestDB(t *testing.T) (*sql.DB, *features.Store, func()) {
	tmpFile, err := os.CreateTemp("", "smtp_features_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()

	db, err := sql.Open("sqlite3", tmpFile.Name()+"?_foreign_keys=on")
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to open database: %v", err)
	}

	schema := `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			username TEXT UNIQUE
		);

		CREATE TABLE email_aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			domain_id INTEGER,
			alias_local TEXT NOT NULL,
			alias_address TEXT UNIQUE NOT NULL,
			description TEXT,
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME,
			email_count INTEGER DEFAULT 0,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		CREATE TABLE screener_contacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			email TEXT,
			domain TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, email),
			UNIQUE(user_id, domain),
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		CREATE TABLE user_preferences (
			user_id INTEGER PRIMARY KEY,
			undo_send_delay INTEGER DEFAULT 10,
			screener_enabled BOOLEAN DEFAULT TRUE,
			tracker_blocking TEXT DEFAULT 'block',
			zones_enabled BOOLEAN DEFAULT FALSE,
			snooze_mark_unread BOOLEAN DEFAULT TRUE,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		INSERT INTO users (id, username) VALUES (1, 'testuser@example.com');
		INSERT INTO users (id, username) VALUES (2, 'otheruser@example.com');
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to create schema: %v", err)
	}

	store := features.NewStore(db)

	cleanup := func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}

	return db, store, cleanup
}

// =============================================================================
// Alias Resolution Tests
// =============================================================================

func TestAliasResolution_ActiveAlias(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create an active alias
	alias := &features.EmailAlias{
		UserID:       1,
		AliasLocal:   "shopping",
		AliasAddress: "shopping.x7k2@example.com",
		Description:  "Shopping alias",
		IsActive:     true,
	}
	if err := store.CreateAlias(ctx, alias); err != nil {
		t.Fatalf("Failed to create alias: %v", err)
	}

	// Resolve the alias
	resolved, err := store.GetAliasByAddress(ctx, "shopping.x7k2@example.com")
	if err != nil {
		t.Fatalf("Failed to resolve alias: %v", err)
	}

	if resolved == nil {
		t.Fatal("Expected alias to be resolved")
	}
	if resolved.UserID != 1 {
		t.Errorf("Expected UserID 1, got %d", resolved.UserID)
	}
	if !resolved.IsActive {
		t.Error("Expected alias to be active")
	}
}

func TestAliasResolution_InactiveAlias(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create an inactive alias
	alias := &features.EmailAlias{
		UserID:       1,
		AliasLocal:   "disabled",
		AliasAddress: "disabled.abc@example.com",
		IsActive:     true,
	}
	store.CreateAlias(ctx, alias)

	// Disable the alias
	isActive := false
	store.UpdateAlias(ctx, 1, alias.ID, &isActive, nil)

	// Resolve the alias
	resolved, err := store.GetAliasByAddress(ctx, "disabled.abc@example.com")
	if err != nil {
		t.Fatalf("Failed to resolve alias: %v", err)
	}

	if resolved == nil {
		t.Fatal("Expected alias to be found (even if inactive)")
	}
	if resolved.IsActive {
		t.Error("Expected alias to be inactive")
	}
}

func TestAliasResolution_NonexistentAlias(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Try to resolve non-existent alias
	resolved, err := store.GetAliasByAddress(ctx, "nonexistent@example.com")

	// Should return nil without error (or ErrNotFound)
	if resolved != nil {
		t.Error("Expected nil for non-existent alias")
	}
	// Error might be nil or ErrNotFound depending on implementation
	if err != nil && err != features.ErrNotFound {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestAliasResolution_CaseInsensitive(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create alias with lowercase
	alias := &features.EmailAlias{
		UserID:       1,
		AliasLocal:   "myalias",
		AliasAddress: "myalias@example.com",
	}
	store.CreateAlias(ctx, alias)

	// Try to resolve with different case
	resolved, err := store.GetAliasByAddress(ctx, "MYALIAS@EXAMPLE.COM")
	if err != nil {
		t.Fatalf("Failed to resolve alias: %v", err)
	}

	if resolved == nil {
		t.Fatal("Expected case-insensitive resolution to work")
	}
}

func TestAliasCount_Increments(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create alias
	alias := &features.EmailAlias{
		UserID:       1,
		AliasLocal:   "counter",
		AliasAddress: "counter@example.com",
	}
	store.CreateAlias(ctx, alias)

	// Initial count should be 0
	resolved, _ := store.GetAliasByAddress(ctx, "counter@example.com")
	if resolved.EmailCount != 0 {
		t.Errorf("Expected initial count 0, got %d", resolved.EmailCount)
	}

	// Increment count
	store.IncrementAliasCount(ctx, alias.ID)
	store.IncrementAliasCount(ctx, alias.ID)
	store.IncrementAliasCount(ctx, alias.ID)

	// Check updated count
	resolved, _ = store.GetAliasByAddress(ctx, "counter@example.com")
	if resolved.EmailCount != 3 {
		t.Errorf("Expected count 3 after increments, got %d", resolved.EmailCount)
	}
}

// =============================================================================
// Screener Tests
// =============================================================================

func TestScreener_ApprovedSender(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Approve a sender
	store.ApproveContact(ctx, 1, "friend@trusted.com", "")

	// Check status
	status, err := store.GetScreenerStatus(ctx, 1, "friend@trusted.com")
	if err != nil {
		t.Fatalf("Failed to get screener status: %v", err)
	}

	if status != features.ScreenerApproved {
		t.Errorf("Expected status approved, got %s", status)
	}
}

func TestScreener_BlockedSender(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Block a sender
	store.BlockContact(ctx, 1, "spammer@evil.com", "")

	// Check status
	status, err := store.GetScreenerStatus(ctx, 1, "spammer@evil.com")
	if err != nil {
		t.Fatalf("Failed to get screener status: %v", err)
	}

	if status != features.ScreenerBlocked {
		t.Errorf("Expected status blocked, got %s", status)
	}
}

func TestScreener_UnknownSender(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Don't add any contacts, just query
	status, err := store.GetScreenerStatus(ctx, 1, "stranger@unknown.com")
	if err != nil {
		t.Fatalf("Failed to get screener status: %v", err)
	}

	// Unknown should be pending
	if status != features.ScreenerPending {
		t.Errorf("Expected status pending for unknown sender, got %s", status)
	}
}

func TestScreener_DomainApproval(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Approve entire domain
	store.ApproveContact(ctx, 1, "", "company.com")

	// Any sender from that domain should be approved
	status1, _ := store.GetScreenerStatus(ctx, 1, "alice@company.com")
	status2, _ := store.GetScreenerStatus(ctx, 1, "bob@company.com")
	status3, _ := store.GetScreenerStatus(ctx, 1, "ceo@company.com")

	if status1 != features.ScreenerApproved {
		t.Errorf("alice@company.com: expected approved, got %s", status1)
	}
	if status2 != features.ScreenerApproved {
		t.Errorf("bob@company.com: expected approved, got %s", status2)
	}
	if status3 != features.ScreenerApproved {
		t.Errorf("ceo@company.com: expected approved, got %s", status3)
	}

	// Different domain should still be pending
	status4, _ := store.GetScreenerStatus(ctx, 1, "person@other.com")
	if status4 != features.ScreenerPending {
		t.Errorf("person@other.com: expected pending, got %s", status4)
	}
}

func TestScreener_EmailOverridesDomain(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Approve domain
	store.ApproveContact(ctx, 1, "", "mixed.com")

	// Block specific email from that domain
	store.BlockContact(ctx, 1, "badactor@mixed.com", "")

	// Domain-approved senders should still be approved
	status1, _ := store.GetScreenerStatus(ctx, 1, "goodperson@mixed.com")
	if status1 != features.ScreenerApproved {
		t.Errorf("goodperson@mixed.com: expected approved, got %s", status1)
	}

	// But the specific blocked email should be blocked
	status2, _ := store.GetScreenerStatus(ctx, 1, "badactor@mixed.com")
	if status2 != features.ScreenerBlocked {
		t.Errorf("badactor@mixed.com: expected blocked, got %s", status2)
	}
}

func TestScreener_UserIsolation(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// User 1 approves a sender
	store.ApproveContact(ctx, 1, "shared@external.com", "")

	// User 2 blocks the same sender
	store.BlockContact(ctx, 2, "shared@external.com", "")

	// Check each user's view
	status1, _ := store.GetScreenerStatus(ctx, 1, "shared@external.com")
	status2, _ := store.GetScreenerStatus(ctx, 2, "shared@external.com")

	if status1 != features.ScreenerApproved {
		t.Errorf("User 1: expected approved, got %s", status1)
	}
	if status2 != features.ScreenerBlocked {
		t.Errorf("User 2: expected blocked, got %s", status2)
	}
}

// =============================================================================
// Preferences Tests
// =============================================================================

func TestPreferences_ScreenerEnabled(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Get default preferences (should be created)
	prefs, err := store.GetPreferences(ctx, 1)
	if err != nil {
		t.Fatalf("Failed to get preferences: %v", err)
	}

	// Screener should be enabled by default
	if !prefs.ScreenerEnabled {
		t.Error("Expected screener to be enabled by default")
	}
}

func TestPreferences_ScreenerDisabled(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Get and modify preferences
	prefs, _ := store.GetPreferences(ctx, 1)
	prefs.ScreenerEnabled = false
	store.SavePreferences(ctx, prefs)

	// Verify change persisted
	prefs2, _ := store.GetPreferences(ctx, 1)
	if prefs2.ScreenerEnabled {
		t.Error("Expected screener to be disabled after update")
	}
}

// =============================================================================
// Integration Scenario Tests
// =============================================================================

func TestScenario_NewSenderToScreener(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Ensure screener is enabled
	prefs, _ := store.GetPreferences(ctx, 1)
	if !prefs.ScreenerEnabled {
		prefs.ScreenerEnabled = true
		store.SavePreferences(ctx, prefs)
	}

	// New sender should be pending (goes to Screener mailbox)
	status, _ := store.GetScreenerStatus(ctx, 1, "firstcontact@newdomain.com")
	if status != features.ScreenerPending {
		t.Errorf("New sender should be pending, got %s", status)
	}

	// Simulate user approving the sender
	store.ApproveContact(ctx, 1, "firstcontact@newdomain.com", "")

	// Now should be approved
	status, _ = store.GetScreenerStatus(ctx, 1, "firstcontact@newdomain.com")
	if status != features.ScreenerApproved {
		t.Errorf("After approval should be approved, got %s", status)
	}
}

func TestScenario_AliasWithScreener(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create an alias for user 1
	alias := &features.EmailAlias{
		UserID:       1,
		AliasLocal:   "newsletter",
		AliasAddress: "newsletter.abc@example.com",
	}
	store.CreateAlias(ctx, alias)

	// Approve a sender for user 1's screener
	store.ApproveContact(ctx, 1, "sender@newsletter.com", "")

	// Resolve alias
	resolved, _ := store.GetAliasByAddress(ctx, "newsletter.abc@example.com")
	if resolved == nil {
		t.Fatal("Alias should resolve")
	}

	// Check screener status for the alias owner
	status, _ := store.GetScreenerStatus(ctx, resolved.UserID, "sender@newsletter.com")
	if status != features.ScreenerApproved {
		t.Errorf("Approved sender through alias should be approved, got %s", status)
	}
}

func TestScenario_MultipleAliasesSameUser(t *testing.T) {
	_, store, cleanup := setupFeaturesTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create multiple aliases for user 1
	aliases := []struct {
		local   string
		address string
	}{
		{"work", "work.xyz@example.com"},
		{"personal", "personal.abc@example.com"},
		{"spam", "spam.123@example.com"},
	}

	for _, a := range aliases {
		store.CreateAlias(ctx, &features.EmailAlias{
			UserID:       1,
			AliasLocal:   a.local,
			AliasAddress: a.address,
		})
	}

	// All should resolve to user 1
	for _, a := range aliases {
		resolved, err := store.GetAliasByAddress(ctx, a.address)
		if err != nil {
			t.Fatalf("Failed to resolve %s: %v", a.address, err)
		}
		if resolved.UserID != 1 {
			t.Errorf("%s resolved to user %d, expected 1", a.address, resolved.UserID)
		}
	}

	// List all aliases
	allAliases, _ := store.ListAliases(ctx, 1)
	if len(allAliases) != 3 {
		t.Errorf("Expected 3 aliases, got %d", len(allAliases))
	}
}
