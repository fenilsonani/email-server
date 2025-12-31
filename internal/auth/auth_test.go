package auth

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	// Create temp database
	tmpFile, err := os.CreateTemp("", "auth_test_*.db")
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
		CREATE TABLE domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			dkim_selector TEXT NOT NULL DEFAULT 'mail',
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			display_name TEXT,
			quota_bytes INTEGER DEFAULT 1073741824,
			used_bytes INTEGER DEFAULT 0,
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(domain_id, username)
		);

		CREATE TABLE aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
			source_address TEXT NOT NULL,
			destination_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			destination_external TEXT,
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(domain_id, source_address)
		);
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

func TestHashPassword(t *testing.T) {
	password := "testpassword123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	// Check hash format
	if hash == "" {
		t.Error("Hash should not be empty")
	}

	if hash[:10] != "$argon2id$" {
		t.Errorf("Hash should start with $argon2id$, got: %s", hash[:10])
	}
}

func TestVerifyPassword(t *testing.T) {
	password := "testpassword123"
	wrongPassword := "wrongpassword"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	// Test correct password
	if !VerifyPassword(password, hash) {
		t.Error("VerifyPassword should return true for correct password")
	}

	// Test wrong password
	if VerifyPassword(wrongPassword, hash) {
		t.Error("VerifyPassword should return false for wrong password")
	}

	// Test empty password
	if VerifyPassword("", hash) {
		t.Error("VerifyPassword should return false for empty password")
	}

	// Test invalid hash
	if VerifyPassword(password, "invalid_hash") {
		t.Error("VerifyPassword should return false for invalid hash")
	}
}

func TestAuthenticator_Authenticate(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	auth := NewAuthenticator(db)
	ctx := context.Background()

	// Create test domain and user
	_, err := db.Exec("INSERT INTO domains (name) VALUES (?)", "example.com")
	if err != nil {
		t.Fatalf("Failed to create domain: %v", err)
	}

	password := "testpass123"
	hash, _ := HashPassword(password)
	_, err = db.Exec(
		"INSERT INTO users (domain_id, username, password_hash) VALUES (1, ?, ?)",
		"testuser", hash,
	)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Test successful authentication
	user, err := auth.Authenticate(ctx, "testuser@example.com", password)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	if user.Email != "testuser@example.com" {
		t.Errorf("Expected email testuser@example.com, got %s", user.Email)
	}

	if user.Username != "testuser" {
		t.Errorf("Expected username testuser, got %s", user.Username)
	}

	if user.Domain != "example.com" {
		t.Errorf("Expected domain example.com, got %s", user.Domain)
	}

	// Test wrong password
	_, err = auth.Authenticate(ctx, "testuser@example.com", "wrongpass")
	if err != ErrInvalidCredentials {
		t.Errorf("Expected ErrInvalidCredentials, got %v", err)
	}

	// Test non-existent user
	_, err = auth.Authenticate(ctx, "nonexistent@example.com", password)
	if err != ErrInvalidCredentials {
		t.Errorf("Expected ErrInvalidCredentials, got %v", err)
	}

	// Test invalid email format
	_, err = auth.Authenticate(ctx, "invalid-email", password)
	if err != ErrInvalidCredentials {
		t.Errorf("Expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthenticator_ValidateAddress(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	auth := NewAuthenticator(db)
	ctx := context.Background()

	// Create test domain and user
	_, err := db.Exec("INSERT INTO domains (name) VALUES (?)", "example.com")
	if err != nil {
		t.Fatalf("Failed to create domain: %v", err)
	}

	hash, _ := HashPassword("test")
	_, err = db.Exec(
		"INSERT INTO users (domain_id, username, password_hash) VALUES (1, ?, ?)",
		"validuser", hash,
	)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Test valid address
	valid, err := auth.ValidateAddress(ctx, "validuser@example.com")
	if err != nil {
		t.Fatalf("ValidateAddress failed: %v", err)
	}
	if !valid {
		t.Error("Expected address to be valid")
	}

	// Test invalid user
	valid, err = auth.ValidateAddress(ctx, "invaliduser@example.com")
	if err != nil {
		t.Fatalf("ValidateAddress failed: %v", err)
	}
	if valid {
		t.Error("Expected address to be invalid")
	}

	// Test invalid domain
	valid, err = auth.ValidateAddress(ctx, "user@unknown.com")
	if err != nil {
		t.Fatalf("ValidateAddress failed: %v", err)
	}
	if valid {
		t.Error("Expected address to be invalid for unknown domain")
	}
}

func TestAuthenticator_LookupUser(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	auth := NewAuthenticator(db)
	ctx := context.Background()

	// Create test domain and user
	_, err := db.Exec("INSERT INTO domains (name) VALUES (?)", "example.com")
	if err != nil {
		t.Fatalf("Failed to create domain: %v", err)
	}

	hash, _ := HashPassword("test")
	_, err = db.Exec(
		"INSERT INTO users (domain_id, username, password_hash, display_name) VALUES (1, ?, ?, ?)",
		"john", hash, "John Doe",
	)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Test lookup
	user, err := auth.LookupUser(ctx, "john@example.com")
	if err != nil {
		t.Fatalf("LookupUser failed: %v", err)
	}

	if user.Username != "john" {
		t.Errorf("Expected username john, got %s", user.Username)
	}

	if user.DisplayName != "John Doe" {
		t.Errorf("Expected display name 'John Doe', got %s", user.DisplayName)
	}

	// Test lookup non-existent
	_, err = auth.LookupUser(ctx, "nonexistent@example.com")
	if err != ErrUserNotFound {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

func TestAuthenticator_DisabledUser(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	auth := NewAuthenticator(db)
	ctx := context.Background()

	// Create test domain and disabled user
	_, err := db.Exec("INSERT INTO domains (name) VALUES (?)", "example.com")
	if err != nil {
		t.Fatalf("Failed to create domain: %v", err)
	}

	password := "test123"
	hash, _ := HashPassword(password)
	_, err = db.Exec(
		"INSERT INTO users (domain_id, username, password_hash, is_active) VALUES (1, ?, ?, FALSE)",
		"disabled", hash,
	)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Test authentication of disabled user
	_, err = auth.Authenticate(ctx, "disabled@example.com", password)
	if err != ErrUserDisabled {
		t.Errorf("Expected ErrUserDisabled, got %v", err)
	}
}

func TestAuthenticator_Aliases(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	auth := NewAuthenticator(db)
	ctx := context.Background()

	// Create test domain and user
	_, err := db.Exec("INSERT INTO domains (name) VALUES (?)", "example.com")
	if err != nil {
		t.Fatalf("Failed to create domain: %v", err)
	}

	hash, _ := HashPassword("test")
	result, err := db.Exec(
		"INSERT INTO users (domain_id, username, password_hash) VALUES (1, ?, ?)",
		"realuser", hash,
	)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, _ := result.LastInsertId()

	// Create alias pointing to user
	_, err = db.Exec(
		"INSERT INTO aliases (domain_id, source_address, destination_user_id) VALUES (1, ?, ?)",
		"alias", userID,
	)
	if err != nil {
		t.Fatalf("Failed to create alias: %v", err)
	}

	// Create external alias
	_, err = db.Exec(
		"INSERT INTO aliases (domain_id, source_address, destination_external) VALUES (1, ?, ?)",
		"external", "forward@other.com",
	)
	if err != nil {
		t.Fatalf("Failed to create external alias: %v", err)
	}

	// Test alias resolves to user
	destUserID, external, err := auth.ResolveAlias(ctx, "alias@example.com")
	if err != nil {
		t.Fatalf("ResolveAlias failed: %v", err)
	}
	if destUserID == nil || *destUserID != userID {
		t.Errorf("Expected user ID %d, got %v", userID, destUserID)
	}
	if external != nil {
		t.Error("Expected external to be nil for local alias")
	}

	// Test external alias
	destUserID, external, err = auth.ResolveAlias(ctx, "external@example.com")
	if err != nil {
		t.Fatalf("ResolveAlias failed: %v", err)
	}
	if destUserID != nil {
		t.Error("Expected destUserID to be nil for external alias")
	}
	if external == nil || *external != "forward@other.com" {
		t.Errorf("Expected external 'forward@other.com', got %v", external)
	}

	// Test ValidateAddress with alias
	valid, err := auth.ValidateAddress(ctx, "alias@example.com")
	if err != nil {
		t.Fatalf("ValidateAddress failed: %v", err)
	}
	if !valid {
		t.Error("Expected alias to be a valid address")
	}
}

func TestAuthenticator_UpdateUsedBytes(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	auth := NewAuthenticator(db)
	ctx := context.Background()

	// Create test domain and user
	_, err := db.Exec("INSERT INTO domains (name) VALUES (?)", "example.com")
	if err != nil {
		t.Fatalf("Failed to create domain: %v", err)
	}

	hash, _ := HashPassword("test")
	result, err := db.Exec(
		"INSERT INTO users (domain_id, username, password_hash, quota_bytes, used_bytes) VALUES (1, ?, ?, 1073741824, 0)",
		"testuser", hash,
	)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, _ := result.LastInsertId()

	// Test increasing used bytes
	err = auth.UpdateUsedBytes(ctx, userID, 1000)
	if err != nil {
		t.Fatalf("UpdateUsedBytes failed: %v", err)
	}

	// Verify the change
	var usedBytes int64
	err = db.QueryRow("SELECT used_bytes FROM users WHERE id = ?", userID).Scan(&usedBytes)
	if err != nil {
		t.Fatalf("Failed to query used_bytes: %v", err)
	}
	if usedBytes != 1000 {
		t.Errorf("Expected used_bytes=1000, got %d", usedBytes)
	}

	// Test adding more
	err = auth.UpdateUsedBytes(ctx, userID, 500)
	if err != nil {
		t.Fatalf("UpdateUsedBytes (add more) failed: %v", err)
	}

	err = db.QueryRow("SELECT used_bytes FROM users WHERE id = ?", userID).Scan(&usedBytes)
	if err != nil {
		t.Fatalf("Failed to query used_bytes: %v", err)
	}
	if usedBytes != 1500 {
		t.Errorf("Expected used_bytes=1500, got %d", usedBytes)
	}

	// Test decreasing used bytes
	err = auth.UpdateUsedBytes(ctx, userID, -500)
	if err != nil {
		t.Fatalf("UpdateUsedBytes (decrease) failed: %v", err)
	}

	err = db.QueryRow("SELECT used_bytes FROM users WHERE id = ?", userID).Scan(&usedBytes)
	if err != nil {
		t.Fatalf("Failed to query used_bytes: %v", err)
	}
	if usedBytes != 1000 {
		t.Errorf("Expected used_bytes=1000 after decrease, got %d", usedBytes)
	}

	// Test that used_bytes can't go negative
	err = auth.UpdateUsedBytes(ctx, userID, -5000)
	if err != nil {
		t.Fatalf("UpdateUsedBytes (large decrease) failed: %v", err)
	}

	err = db.QueryRow("SELECT used_bytes FROM users WHERE id = ?", userID).Scan(&usedBytes)
	if err != nil {
		t.Fatalf("Failed to query used_bytes: %v", err)
	}
	if usedBytes != 0 {
		t.Errorf("Expected used_bytes=0 (not negative), got %d", usedBytes)
	}
}

func TestAuthenticator_GetQuotaStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	auth := NewAuthenticator(db)
	ctx := context.Background()

	// Create test domain and user
	_, err := db.Exec("INSERT INTO domains (name) VALUES (?)", "example.com")
	if err != nil {
		t.Fatalf("Failed to create domain: %v", err)
	}

	hash, _ := HashPassword("test")
	result, err := db.Exec(
		"INSERT INTO users (domain_id, username, password_hash, quota_bytes, used_bytes) VALUES (1, ?, ?, 1073741824, 536870912)",
		"testuser", hash,
	)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, _ := result.LastInsertId()

	// Test GetQuotaStatus
	quotaBytes, usedBytes, err := auth.GetQuotaStatus(ctx, userID)
	if err != nil {
		t.Fatalf("GetQuotaStatus failed: %v", err)
	}

	if quotaBytes != 1073741824 {
		t.Errorf("Expected quotaBytes=1073741824, got %d", quotaBytes)
	}

	if usedBytes != 536870912 {
		t.Errorf("Expected usedBytes=536870912, got %d", usedBytes)
	}
}

func TestAuthenticator_QuotaCheck(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	auth := NewAuthenticator(db)
	ctx := context.Background()

	// Create test domain and user with specific quota
	_, err := db.Exec("INSERT INTO domains (name) VALUES (?)", "example.com")
	if err != nil {
		t.Fatalf("Failed to create domain: %v", err)
	}

	hash, _ := HashPassword("test")
	result, err := db.Exec(
		"INSERT INTO users (domain_id, username, password_hash, quota_bytes, used_bytes) VALUES (1, ?, ?, 1000, 900)",
		"testuser", hash,
	)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, _ := result.LastInsertId()

	// Check quota status
	quotaBytes, usedBytes, err := auth.GetQuotaStatus(ctx, userID)
	if err != nil {
		t.Fatalf("GetQuotaStatus failed: %v", err)
	}

	// Simulate quota check for a message
	messageSize := int64(150)
	if usedBytes+messageSize > quotaBytes {
		// This should happen - quota would be exceeded
		t.Log("Quota would be exceeded as expected")
	} else {
		t.Error("Expected quota to be exceeded for 150 byte message")
	}

	// Smaller message should fit
	messageSize = int64(50)
	if usedBytes+messageSize > quotaBytes {
		t.Error("Expected 50 byte message to fit within quota")
	}
}

func TestAuthenticator_QuotaFieldsInUser(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	auth := NewAuthenticator(db)
	ctx := context.Background()

	// Create test domain and user
	_, err := db.Exec("INSERT INTO domains (name) VALUES (?)", "example.com")
	if err != nil {
		t.Fatalf("Failed to create domain: %v", err)
	}

	password := "testpass123"
	hash, _ := HashPassword(password)
	_, err = db.Exec(
		"INSERT INTO users (domain_id, username, password_hash, quota_bytes, used_bytes) VALUES (1, ?, ?, 2147483648, 1073741824)",
		"quotauser", hash,
	)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Authenticate and verify quota fields are populated
	user, err := auth.Authenticate(ctx, "quotauser@example.com", password)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	if user.QuotaBytes != 2147483648 {
		t.Errorf("Expected QuotaBytes=2147483648, got %d", user.QuotaBytes)
	}

	if user.UsedBytes != 1073741824 {
		t.Errorf("Expected UsedBytes=1073741824, got %d", user.UsedBytes)
	}

	// Also test via LookupUser
	user, err = auth.LookupUser(ctx, "quotauser@example.com")
	if err != nil {
		t.Fatalf("LookupUser failed: %v", err)
	}

	if user.QuotaBytes != 2147483648 {
		t.Errorf("LookupUser: Expected QuotaBytes=2147483648, got %d", user.QuotaBytes)
	}

	if user.UsedBytes != 1073741824 {
		t.Errorf("LookupUser: Expected UsedBytes=1073741824, got %d", user.UsedBytes)
	}
}
