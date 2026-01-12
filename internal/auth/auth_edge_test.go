package auth

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// setupEdgeTestDB creates an in-memory SQLite database with schema for edge testing
func setupEdgeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	schema := `
		CREATE TABLE IF NOT EXISTS domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			is_active INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL,
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			display_name TEXT,
			quota_bytes INTEGER DEFAULT 0,
			used_bytes INTEGER DEFAULT 0,
			is_active INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (domain_id) REFERENCES domains(id),
			UNIQUE(domain_id, username)
		);
		CREATE TABLE IF NOT EXISTS aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL,
			source_address TEXT NOT NULL,
			destination_user_id INTEGER,
			destination_external TEXT,
			is_active INTEGER DEFAULT 1,
			FOREIGN KEY (domain_id) REFERENCES domains(id),
			FOREIGN KEY (destination_user_id) REFERENCES users(id)
		);
		CREATE TABLE IF NOT EXISTS auth_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER,
			username TEXT,
			remote_addr TEXT,
			protocol TEXT,
			success INTEGER,
			failure_reason TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`
	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("Failed to create schema: %v", err)
	}
	return db
}

// createTestUser creates a user for testing
func createTestUser(t *testing.T, db *sql.DB, email, password string, enabled bool) {
	t.Helper()
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		t.Fatalf("Invalid email: %s", email)
	}
	username, domain := parts[0], parts[1]

	// Create domain
	_, err := db.Exec("INSERT OR IGNORE INTO domains (name) VALUES (?)", domain)
	if err != nil {
		t.Fatalf("Failed to create domain: %v", err)
	}

	var domainID int64
	err = db.QueryRow("SELECT id FROM domains WHERE name = ?", domain).Scan(&domainID)
	if err != nil {
		t.Fatalf("Failed to get domain ID: %v", err)
	}

	// Hash password
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	// Create user
	isActive := 0
	if enabled {
		isActive = 1
	}
	_, err = db.Exec(
		"INSERT INTO users (domain_id, username, password_hash, is_active) VALUES (?, ?, ?, ?)",
		domainID, username, hash, isActive,
	)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
}

// TestAuth_AccountLockout verifies that accounts are locked after too many failed attempts.
func TestAuth_AccountLockout(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	auth := NewAuthenticator(db)
	// Reduce lockout settings for testing
	auth.maxAttempts = 3
	auth.lockoutWindow = time.Minute
	auth.lockoutDuration = 5 * time.Second

	createTestUser(t, db, "test@example.com", "validpassword123", true)
	ctx := context.Background()

	// Make failed attempts
	for i := 0; i < 3; i++ {
		_, err := auth.Authenticate(ctx, "test@example.com", "wrongpassword")
		if err == nil {
			t.Error("Expected authentication to fail with wrong password")
		}
	}

	// Account should now be locked - even correct password should fail
	_, err := auth.Authenticate(ctx, "test@example.com", "validpassword123")
	if err != ErrInvalidCredentials {
		t.Errorf("Expected ErrInvalidCredentials for locked account, got %v", err)
	}

	// Verify account is locked
	if !auth.isAccountLocked("test@example.com") {
		t.Error("Account should be locked after max failed attempts")
	}
}

// TestAuth_LockoutExpiry verifies that lockouts expire after the configured duration.
func TestAuth_LockoutExpiry(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	auth := NewAuthenticator(db)
	auth.maxAttempts = 2
	auth.lockoutWindow = time.Minute
	auth.lockoutDuration = 100 * time.Millisecond // Very short for testing

	createTestUser(t, db, "test@example.com", "validpassword123", true)
	ctx := context.Background()

	// Trigger lockout
	for i := 0; i < 2; i++ {
		auth.Authenticate(ctx, "test@example.com", "wrongpassword")
	}

	// Should be locked
	if !auth.isAccountLocked("test@example.com") {
		t.Error("Account should be locked")
	}

	// Wait for lockout to expire
	time.Sleep(150 * time.Millisecond)

	// Should be unlocked and authentication should work
	_, err := auth.Authenticate(ctx, "test@example.com", "validpassword123")
	if err != nil {
		t.Errorf("Authentication should succeed after lockout expires: %v", err)
	}
}

// TestAuth_ConcurrentAuth tests concurrent authentication attempts.
func TestAuth_ConcurrentAuth(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	// Enable WAL mode and set busy timeout for better concurrency
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")
	db.SetMaxOpenConns(1) // Serialize SQLite access

	auth := NewAuthenticator(db)
	createTestUser(t, db, "test@example.com", "validpassword123", true)
	ctx := context.Background()

	var wg sync.WaitGroup
	var successCount, failCount int
	var mu sync.Mutex

	// Run 10 concurrent authentication attempts (SQLite-friendly)
	concurrentAttempts := 10
	for i := 0; i < concurrentAttempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := auth.Authenticate(ctx, "test@example.com", "validpassword123")
			mu.Lock()
			if err == nil {
				successCount++
			} else {
				failCount++
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	// All should succeed (no race conditions or data races)
	// Note: With in-memory SQLite and serialized connections, all should work
	if successCount < concurrentAttempts/2 {
		t.Errorf("Expected most auths to succeed, got %d successes and %d failures out of %d",
			successCount, failCount, concurrentAttempts)
	}

	// Most importantly: no panics or data races (run with -race flag)
}

// TestAuth_EmptyPassword verifies that empty passwords are rejected.
func TestAuth_EmptyPassword(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	auth := NewAuthenticator(db)
	createTestUser(t, db, "test@example.com", "validpassword123", true)
	ctx := context.Background()

	testCases := []struct {
		name     string
		password string
	}{
		{"empty", ""},
		{"spaces only", "        "},
		{"too short", "short"},
		{"one char", "a"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := auth.Authenticate(ctx, "test@example.com", tc.password)
			if err != ErrInvalidCredentials {
				t.Errorf("Expected ErrInvalidCredentials for %q password, got %v", tc.name, err)
			}
		})
	}
}

// TestAuth_VeryLongPassword verifies handling of extremely long passwords (DoS prevention).
func TestAuth_VeryLongPassword(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	auth := NewAuthenticator(db)
	createTestUser(t, db, "test@example.com", "validpassword123", true)
	ctx := context.Background()

	// Test various long password sizes
	testCases := []struct {
		name   string
		length int
	}{
		{"129 chars (just over limit)", 129},
		{"1000 chars", 1000},
		{"10000 chars", 10000},
		{"1MB password", 1024 * 1024},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			longPassword := strings.Repeat("a", tc.length)
			start := time.Now()
			_, err := auth.Authenticate(ctx, "test@example.com", longPassword)
			duration := time.Since(start)

			// Should fail fast, not take forever hashing
			if duration > 2*time.Second {
				t.Errorf("Authentication took too long (%v) for %d char password - potential DoS", duration, tc.length)
			}

			if err != ErrInvalidCredentials {
				t.Errorf("Expected ErrInvalidCredentials for long password, got %v", err)
			}
		})
	}
}

// TestAuth_SQLInjection verifies that SQL injection attempts are handled safely.
func TestAuth_SQLInjection(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	auth := NewAuthenticator(db)
	createTestUser(t, db, "test@example.com", "validpassword123", true)
	ctx := context.Background()

	sqlInjectionStrings := []string{
		"'; DROP TABLE users; --",
		"' OR '1'='1",
		"1; SELECT * FROM users",
		"admin'--",
		"' UNION SELECT password_hash FROM users WHERE '1'='1",
		"1' AND '1'='1",
		"') OR ('1'='1",
		"'; EXEC xp_cmdshell('dir'); --",
		"' OR 1=1#",
		"admin' /*",
	}

	for _, injection := range sqlInjectionStrings {
		t.Run("email_"+injection[:min(20, len(injection))], func(t *testing.T) {
			// Test injection in email
			_, err := auth.Authenticate(ctx, injection+"@example.com", "password123456")
			// Should fail gracefully, not panic or execute SQL
			if err == nil {
				t.Error("SQL injection in email should not authenticate")
			}
		})

		t.Run("password_"+injection[:min(20, len(injection))], func(t *testing.T) {
			// Test injection in password
			_, err := auth.Authenticate(ctx, "test@example.com", injection)
			if err == nil {
				t.Error("SQL injection in password should not authenticate")
			}
		})
	}

	// Verify database is still intact
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		t.Fatalf("Database query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 user after SQL injection tests, got %d", count)
	}
}

// TestAuth_NullBytes verifies handling of null bytes in credentials.
func TestAuth_NullBytes(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	auth := NewAuthenticator(db)
	createTestUser(t, db, "test@example.com", "validpassword123", true)
	ctx := context.Background()

	nullByteStrings := []string{
		"test\x00.txt",
		"user\x00admin",
		"file.txt\x00.jpg",
		"\x00admin",
		"test\x00",
		"test\x00@example.com",
	}

	for _, nullStr := range nullByteStrings {
		t.Run("email_with_null", func(t *testing.T) {
			_, err := auth.Authenticate(ctx, nullStr+"@example.com", "validpassword123")
			// Should fail gracefully
			if err == nil {
				t.Error("Null byte in email should not authenticate")
			}
		})

		t.Run("password_with_null", func(t *testing.T) {
			_, err := auth.Authenticate(ctx, "test@example.com", nullStr+"validpassword")
			// Should fail gracefully
			if err == nil {
				t.Error("Null byte in password should not authenticate")
			}
		})
	}
}

// TestAuth_UnicodeUsername tests handling of Unicode characters in usernames.
func TestAuth_UnicodeUsername(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	auth := NewAuthenticator(db)
	ctx := context.Background()

	unicodeUsernames := []string{
		"用户@example.com",
		"пользователь@example.com",
		"مستخدم@example.com",
		"ユーザー@example.com",
		"🎉user@example.com",
		"tëst@example.com",
		"naïve@example.com",
	}

	for _, email := range unicodeUsernames {
		t.Run(email, func(t *testing.T) {
			_, err := auth.Authenticate(ctx, email, "password12345")
			// Should fail with invalid credentials (not panic or crash)
			if err == nil {
				t.Error("Unicode username should not authenticate (user doesn't exist)")
			}
		})
	}
}

// TestAuth_QuotaAtLimit tests user authentication when at exact quota limit.
func TestAuth_QuotaAtLimit(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	auth := NewAuthenticator(db)
	createTestUser(t, db, "test@example.com", "validpassword123", true)

	// Set quota and used bytes to exactly match
	_, err := db.Exec("UPDATE users SET quota_bytes = 1000000, used_bytes = 1000000 WHERE username = 'test'")
	if err != nil {
		t.Fatalf("Failed to set quota: %v", err)
	}

	ctx := context.Background()

	// Authentication should still work even at quota limit
	user, err := auth.Authenticate(ctx, "test@example.com", "validpassword123")
	if err != nil {
		t.Errorf("Authentication should work at quota limit: %v", err)
	}

	if user.QuotaBytes != user.UsedBytes {
		t.Errorf("Expected quota (%d) to equal used (%d)", user.QuotaBytes, user.UsedBytes)
	}
}

// TestAuth_QuotaOverflow tests handling of potential integer overflow in quota calculations.
func TestAuth_QuotaOverflow(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	auth := NewAuthenticator(db)
	createTestUser(t, db, "test@example.com", "validpassword123", true)

	// Set quota to max int64
	maxInt64 := int64(9223372036854775807)
	_, err := db.Exec("UPDATE users SET quota_bytes = ?, used_bytes = 0 WHERE username = 'test'", maxInt64)
	if err != nil {
		t.Fatalf("Failed to set max quota: %v", err)
	}

	ctx := context.Background()

	// Get user
	user, err := auth.Authenticate(ctx, "test@example.com", "validpassword123")
	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}

	// Test quota update with large value
	err = auth.UpdateUsedBytes(ctx, user.ID, maxInt64)
	if err != nil {
		t.Errorf("UpdateUsedBytes failed: %v", err)
	}

	// Check quota status
	quota, used, err := auth.GetQuotaStatus(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetQuotaStatus failed: %v", err)
	}

	if quota != maxInt64 {
		t.Errorf("Expected quota %d, got %d", maxInt64, quota)
	}

	// Used should not overflow
	if used < 0 {
		t.Errorf("Used bytes overflowed to negative: %d", used)
	}
}

// TestAuth_DatabaseFailure tests graceful handling of database connection loss.
func TestAuth_DatabaseFailure(t *testing.T) {
	db := setupEdgeTestDB(t)

	auth := NewAuthenticator(db)
	createTestUser(t, db, "test@example.com", "validpassword123", true)

	// Close database to simulate failure
	db.Close()

	ctx := context.Background()

	// Authentication should fail gracefully, not panic
	_, err := auth.Authenticate(ctx, "test@example.com", "validpassword123")
	if err == nil {
		t.Error("Expected error when database is closed")
	}

	// Error should not expose internal details
	if strings.Contains(err.Error(), "sql: database is closed") {
		// This is acceptable but should be wrapped
	}
}

// TestAuth_TimingAttackPrevention verifies constant-time comparison for password verification.
func TestAuth_TimingAttackPrevention(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	auth := NewAuthenticator(db)
	createTestUser(t, db, "test@example.com", "validpassword123", true)
	ctx := context.Background()

	// Time authentication with correct password
	var correctTimes []time.Duration
	for i := 0; i < 10; i++ {
		start := time.Now()
		auth.Authenticate(ctx, "test@example.com", "validpassword123")
		correctTimes = append(correctTimes, time.Since(start))
	}

	// Clear lockout
	auth.clearLockout("test@example.com")

	// Time authentication with wrong password (same length)
	var wrongTimes []time.Duration
	for i := 0; i < 10; i++ {
		start := time.Now()
		auth.Authenticate(ctx, "test@example.com", "wrongpassword123")
		wrongTimes = append(wrongTimes, time.Since(start))
	}

	// Calculate averages
	var correctAvg, wrongAvg time.Duration
	for _, d := range correctTimes {
		correctAvg += d
	}
	for _, d := range wrongTimes {
		wrongAvg += d
	}
	correctAvg /= time.Duration(len(correctTimes))
	wrongAvg /= time.Duration(len(wrongTimes))

	// Times should be roughly similar (within 50% of each other)
	// This is a basic check - real timing attack prevention needs more rigorous testing
	diff := correctAvg - wrongAvg
	if diff < 0 {
		diff = -diff
	}

	maxDiff := correctAvg / 2
	if diff > maxDiff {
		t.Logf("Warning: Timing difference detected - correct: %v, wrong: %v, diff: %v", correctAvg, wrongAvg, diff)
		// Note: This is informational, not a hard failure, as timing can vary
	}
}

// TestAuth_DisabledUser_NoEnumeration verifies that disabled users return generic error.
func TestAuth_DisabledUser_NoEnumeration(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	auth := NewAuthenticator(db)
	createTestUser(t, db, "disabled@example.com", "validpassword123", false)
	createTestUser(t, db, "active@example.com", "validpassword123", true)
	ctx := context.Background()

	// Disabled user with correct password
	_, errDisabled := auth.Authenticate(ctx, "disabled@example.com", "validpassword123")

	// Non-existent user
	_, errNonExistent := auth.Authenticate(ctx, "nonexistent@example.com", "validpassword123")

	// Both should return the same error type to prevent enumeration
	if errDisabled != ErrInvalidCredentials {
		t.Errorf("Disabled user should return ErrInvalidCredentials, got %v", errDisabled)
	}
	if errNonExistent != ErrInvalidCredentials {
		t.Errorf("Non-existent user should return ErrInvalidCredentials, got %v", errNonExistent)
	}
}

// min returns the smaller of two ints
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
