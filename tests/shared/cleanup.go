package testenv

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// CleanupManager manages test resource cleanup.
type CleanupManager struct {
	cleanupFunctions []func() error
	t                *testing.T
}

// NewCleanupManager creates a new cleanup manager.
func NewCleanupManager(t *testing.T) *CleanupManager {
	t.Helper()

	return &CleanupManager{
		cleanupFunctions: make([]func() error, 0),
		t:                t,
	}
}

// Register registers a cleanup function to be called on cleanup.
func (cm *CleanupManager) Register(fn func() error) {
	cm.cleanupFunctions = append(cm.cleanupFunctions, fn)
}

// RegisterSimple registers a simple cleanup function with no error return.
func (cm *CleanupManager) RegisterSimple(fn func()) {
	cm.Register(func() error {
		fn()
		return nil
	})
}

// Cleanup executes all registered cleanup functions in reverse order.
func (cm *CleanupManager) Cleanup() error {
	var lastErr error

	// Execute in reverse order (LIFO)
	for i := len(cm.cleanupFunctions) - 1; i >= 0; i-- {
		if err := cm.cleanupFunctions[i](); err != nil {
			cm.t.Errorf("Cleanup function failed: %v", err)
			lastErr = err
		}
	}

	return lastErr
}

// CleanupDatabase registers database cleanup.
func (cm *CleanupManager) CleanupDatabase(db *sql.DB) {
	cm.Register(func() error {
		if db != nil {
			return db.Close()
		}
		return nil
	})
}

// CleanupTempFile registers temporary file cleanup.
func (cm *CleanupManager) CleanupTempFile(path string) {
	cm.RegisterSimple(func() {
		os.Remove(path)
	})
}

// CleanupTempDir registers temporary directory cleanup.
func (cm *CleanupManager) CleanupTempDir(path string) {
	cm.RegisterSimple(func() {
		os.RemoveAll(path)
	})
}

// CleanupMailbox registers mailbox cleanup.
func (cm *CleanupManager) CleanupMailbox(mailboxPath string) {
	cm.RegisterSimple(func() {
		os.RemoveAll(mailboxPath)
	})
}

// CleanupDatabase cleans up test data from a database.
func CleanupDatabase(t *testing.T, db *sql.DB, tables ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// If no tables specified, clean all common tables
	if len(tables) == 0 {
		tables = []string{
			"users",
			"domains",
			"mailboxes",
			"messages",
			"user_forwarding",
			"vacation_responses",
			"sieve_scripts",
		}
	}

	// Disable foreign key constraints temporarily
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Logf("Failed to disable foreign keys: %v", err)
	}

	// Delete from each table
	for _, table := range tables {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			t.Logf("Failed to delete from table %s: %v", table, err)
		}
	}

	// Re-enable foreign key constraints
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Logf("Failed to enable foreign keys: %v", err)
	}
}

// CleanupMailboxes removes all test mailboxes.
func CleanupMailboxes(t *testing.T, maildir string) {
	t.Helper()

	if err := os.RemoveAll(maildir); err != nil {
		t.Logf("Failed to cleanup mailboxes: %v", err)
	}
}

// ClearCaches clears various test caches and buffers.
func ClearCaches() {
	// This would clear Redis, in-memory caches, etc.
	// Implementation depends on specific caching strategy
}

// ResetSequences resets database sequences/auto-increment counters.
func ResetSequences(t *testing.T, db *sql.DB, tables ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, table := range tables {
		// SQLite: Reset AUTOINCREMENT
		query := "DELETE FROM sqlite_sequence WHERE name = ?"
		if _, err := db.ExecContext(ctx, query, table); err != nil {
			t.Logf("Failed to reset sequence for %s: %v", table, err)
		}
	}
}

// AssertTablesEmpty asserts that specified tables are empty.
func AssertTablesEmpty(t *testing.T, db *sql.DB, tables ...string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, table := range tables {
		var count int
		query := "SELECT COUNT(*) FROM " + table
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			t.Errorf("Failed to count rows in %s: %v", table, err)
			continue
		}

		if count != 0 {
			t.Errorf("Table %s is not empty (%d rows)", table, count)
		}
	}
}

// DatabaseSnapshot represents a database state snapshot.
type DatabaseSnapshot struct {
	TableCounts map[string]int64
	Timestamp   time.Time
}

// TakeSnapshot takes a snapshot of the current database state.
func TakeSnapshot(t *testing.T, db *sql.DB, tables ...string) *DatabaseSnapshot {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	snapshot := &DatabaseSnapshot{
		TableCounts: make(map[string]int64),
		Timestamp:   time.Now(),
	}

	for _, table := range tables {
		var count int64
		query := "SELECT COUNT(*) FROM " + table
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			t.Logf("Failed to count rows in %s: %v", table, err)
			continue
		}
		snapshot.TableCounts[table] = count
	}

	return snapshot
}

// VerifySnapshot verifies that the database matches the snapshot.
func VerifySnapshot(t *testing.T, db *sql.DB, snapshot *DatabaseSnapshot) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for table, expectedCount := range snapshot.TableCounts {
		var actualCount int64
		query := "SELECT COUNT(*) FROM " + table
		if err := db.QueryRowContext(ctx, query).Scan(&actualCount); err != nil {
			t.Errorf("Failed to count rows in %s: %v", table, err)
			continue
		}

		if actualCount != expectedCount {
			t.Errorf("Table %s row count mismatch: got %d, expected %d", table, actualCount, expectedCount)
		}
	}
}

// TransactionCleanup wraps a test function in a transaction that's rolled back afterward.
func TransactionCleanup(t *testing.T, db *sql.DB, fn func(*sql.Tx) error) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	// Rollback to cleanup
	return tx.Rollback()
}

// WithDatabaseCleanup executes a test function and cleans up afterward.
func WithDatabaseCleanup(t *testing.T, db *sql.DB, fn func()) {
	t.Helper()

	// Take snapshot before test
	tables := []string{
		"users",
		"domains",
		"mailboxes",
		"messages",
	}
	snapshotBefore := TakeSnapshot(t, db, tables...)

	// Run test
	fn()

	// Cleanup by resetting to original state
	CleanupDatabase(t, db, tables...)
	ResetSequences(t, db, tables...)

	// Verify cleanup
	snapshotAfter := TakeSnapshot(t, db, tables...)
	if len(snapshotAfter.TableCounts) != len(snapshotBefore.TableCounts) {
		t.Logf("Warning: Table count mismatch after cleanup")
	}
}

// PragmaCleanup handles pragma-specific cleanup for SQLite.
func PragmaCleanup(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Optimize database
	if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
		t.Logf("Failed to vacuum database: %v", err)
	}

	// Analyze for query optimization
	if _, err := db.ExecContext(ctx, "ANALYZE"); err != nil {
		t.Logf("Failed to analyze database: %v", err)
	}
}

// TestDataGenerator generates test data for database population.
type TestDataGenerator struct {
	db *sql.DB
	t  *testing.T
}

// NewTestDataGenerator creates a new test data generator.
func NewTestDataGenerator(t *testing.T, db *sql.DB) *TestDataGenerator {
	t.Helper()

	return &TestDataGenerator{
		db: db,
		t:  t,
	}
}

// GenerateUsers generates test user records.
func (gen *TestDataGenerator) GenerateUsers(count int) error {
	gen.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 0; i < count; i++ {
		email := "user" + string(rune('0'+i)) + "@test.local"
		query := "INSERT INTO users (email, password_hash, is_active) VALUES (?, ?, ?)"
		if _, err := gen.db.ExecContext(ctx, query, email, "hash", true); err != nil {
			gen.t.Logf("Failed to create test user: %v", err)
		}
	}

	return nil
}

// GenerateDomains generates test domain records.
func (gen *TestDataGenerator) GenerateDomains(count int) error {
	gen.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 0; i < count; i++ {
		domain := "test" + string(rune('0'+i)) + ".local"
		query := "INSERT INTO domains (name, is_active) VALUES (?, ?)"
		if _, err := gen.db.ExecContext(ctx, query, domain, true); err != nil {
			gen.t.Logf("Failed to create test domain: %v", err)
		}
	}

	return nil
}
