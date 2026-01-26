// Package testutil provides test utilities and helpers for edge case testing.
package helpers

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// DBType represents the type of database being used
type DBType string

const (
	DBTypeSQLite   DBType = "sqlite"
	DBTypePostgres DBType = "postgres"
)

// TestPostgresDSN returns the PostgreSQL DSN for testing, or empty if not configured.
// Set TEST_POSTGRES_DSN environment variable to enable PostgreSQL testing.
// Example: TEST_POSTGRES_DSN="postgres://user:pass@localhost:5432/testdb?sslmode=disable"
func TestPostgresDSN() string {
	return os.Getenv("TEST_POSTGRES_DSN")
}

// PostgresAvailable returns true if PostgreSQL testing is configured.
func PostgresAvailable() bool {
	return TestPostgresDSN() != ""
}

// WithTimeout runs a test function with a timeout.
func WithTimeout(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	select {
	case <-done:
		return
	case <-time.After(d):
		t.Fatalf("Test timed out after %v", d)
	}
}

// WithTempDir creates a temporary directory for tests and cleans up after.
func WithTempDir(t *testing.T, fn func(path string)) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	fn(tmpDir)
}

// WithTempMaildir creates a temporary maildir structure for testing.
func WithTempMaildir(t *testing.T, fn func(path string)) {
	t.Helper()
	WithTempDir(t, func(tmpDir string) {
		maildir := filepath.Join(tmpDir, "maildir")
		for _, subdir := range []string{"cur", "new", "tmp"} {
			if err := os.MkdirAll(filepath.Join(maildir, subdir), 0755); err != nil {
				t.Fatalf("Failed to create maildir subdir: %v", err)
			}
		}
		fn(maildir)
	})
}

// WithTestDB creates an in-memory SQLite database for testing.
func WithTestDB(t *testing.T, fn func(db *sql.DB)) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()
	fn(db)
}

// WithTestDBAndSchema creates an in-memory SQLite database with basic schema.
func WithTestDBAndSchema(t *testing.T, fn func(db *sql.DB)) {
	t.Helper()
	WithTestDB(t, func(db *sql.DB) {
		schema := `
			CREATE TABLE IF NOT EXISTS users (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				email TEXT UNIQUE NOT NULL,
				password_hash TEXT NOT NULL,
				enabled INTEGER DEFAULT 1,
				quota_bytes INTEGER DEFAULT 0,
				used_bytes INTEGER DEFAULT 0,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);
			CREATE TABLE IF NOT EXISTS domains (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT UNIQUE NOT NULL,
				enabled INTEGER DEFAULT 1,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);
			CREATE TABLE IF NOT EXISTS mailboxes (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				user_id INTEGER NOT NULL,
				name TEXT NOT NULL,
				uidvalidity INTEGER DEFAULT 1,
				FOREIGN KEY (user_id) REFERENCES users(id)
			);
		`
		_, err := db.Exec(schema)
		if err != nil {
			t.Fatalf("Failed to create schema: %v", err)
		}
		fn(db)
	})
}

// WithTestPostgresDB creates a PostgreSQL connection for testing.
// Skips the test if TEST_POSTGRES_DSN is not set.
func WithTestPostgresDB(t *testing.T, fn func(db *sql.DB)) {
	t.Helper()
	dsn := TestPostgresDSN()
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set, skipping PostgreSQL test")
		return
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("Failed to open PostgreSQL database: %v", err)
	}
	defer db.Close()

	// Verify connection
	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping PostgreSQL database: %v", err)
	}

	fn(db)
}

// WithTestPostgresDBAndCleanup creates a PostgreSQL connection and cleans up test tables after.
func WithTestPostgresDBAndCleanup(t *testing.T, tableName string, fn func(db *sql.DB)) {
	t.Helper()
	WithTestPostgresDB(t, func(db *sql.DB) {
		// Clean up before test
		db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", tableName))

		// Run test
		fn(db)

		// Clean up after test
		db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", tableName))
	})
}

// DBTestCase represents a database test configuration
type DBTestCase struct {
	Name   string
	DBType DBType
	Setup  func(t *testing.T) *sql.DB
	Cleanup func(db *sql.DB)
}

// GetTestDatabases returns test cases for all available databases.
// Always includes SQLite, includes PostgreSQL if TEST_POSTGRES_DSN is set.
func GetTestDatabases(t *testing.T) []DBTestCase {
	t.Helper()

	cases := []DBTestCase{
		{
			Name:   "SQLite",
			DBType: DBTypeSQLite,
			Setup: func(t *testing.T) *sql.DB {
				db, err := sql.Open("sqlite3", ":memory:")
				if err != nil {
					t.Fatalf("Failed to open SQLite: %v", err)
				}
				return db
			},
			Cleanup: func(db *sql.DB) {
				db.Close()
			},
		},
	}

	// Add PostgreSQL if available
	if PostgresAvailable() {
		cases = append(cases, DBTestCase{
			Name:   "PostgreSQL",
			DBType: DBTypePostgres,
			Setup: func(t *testing.T) *sql.DB {
				db, err := sql.Open("postgres", TestPostgresDSN())
				if err != nil {
					t.Fatalf("Failed to open PostgreSQL: %v", err)
				}
				if err := db.Ping(); err != nil {
					t.Fatalf("Failed to ping PostgreSQL: %v", err)
				}
				return db
			},
			Cleanup: func(db *sql.DB) {
				db.Close()
			},
		})
	}

	return cases
}

// RunOnAllDatabases runs a test function against all available databases.
func RunOnAllDatabases(t *testing.T, fn func(t *testing.T, db *sql.DB, dbType DBType)) {
	t.Helper()
	for _, tc := range GetTestDatabases(t) {
		t.Run(tc.Name, func(t *testing.T) {
			db := tc.Setup(t)
			defer tc.Cleanup(db)
			fn(t, db, tc.DBType)
		})
	}
}

// CreateAuditTableSQL returns the SQL to create audit_log table for the given database type.
func CreateAuditTableSQL(dbType DBType) string {
	switch dbType {
	case DBTypePostgres:
		return `
			CREATE TABLE IF NOT EXISTS audit_log (
				id SERIAL PRIMARY KEY,
				timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				actor TEXT NOT NULL,
				action TEXT NOT NULL,
				target TEXT,
				details TEXT,
				ip_address TEXT
			);
			CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
			CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor);
			CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);
		`
	default: // SQLite
		return `
			CREATE TABLE IF NOT EXISTS audit_log (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
				actor TEXT NOT NULL,
				action TEXT NOT NULL,
				target TEXT,
				details TEXT,
				ip_address TEXT
			);
			CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
			CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor);
			CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);
		`
	}
}

// ConcurrentRunner runs functions concurrently and collects errors.
type ConcurrentRunner struct {
	wg     sync.WaitGroup
	mu     sync.Mutex
	errors []error
}

// Run executes a function in a goroutine.
func (r *ConcurrentRunner) Run(fn func() error) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if err := fn(); err != nil {
			r.mu.Lock()
			r.errors = append(r.errors, err)
			r.mu.Unlock()
		}
	}()
}

// Wait waits for all goroutines and returns collected errors.
func (r *ConcurrentRunner) Wait() []error {
	r.wg.Wait()
	return r.errors
}

// RunConcurrent runs multiple functions concurrently and fails if any error.
func RunConcurrent(t *testing.T, count int, fn func(i int) error) {
	t.Helper()
	var runner ConcurrentRunner
	for i := 0; i < count; i++ {
		idx := i
		runner.Run(func() error {
			return fn(idx)
		})
	}
	errors := runner.Wait()
	if len(errors) > 0 {
		t.Errorf("Concurrent execution had %d errors: %v", len(errors), errors)
	}
}

// AssertPanics asserts that a function panics.
func AssertPanics(t *testing.T, fn func(), msgContains string) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Error("Expected panic but function completed normally")
			return
		}
		msg := fmt.Sprintf("%v", r)
		if msgContains != "" && !strings.Contains(msg, msgContains) {
			t.Errorf("Panic message %q does not contain %q", msg, msgContains)
		}
	}()
	fn()
}

// AssertNoPanic asserts that a function does not panic.
func AssertNoPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Unexpected panic: %v", r)
		}
	}()
	fn()
}

// AssertErrorContains checks that an error contains a substring.
func AssertErrorContains(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Errorf("Expected error containing %q, got nil", substr)
		return
	}
	if !strings.Contains(err.Error(), substr) {
		t.Errorf("Error %q does not contain %q", err.Error(), substr)
	}
}

// RepeatString creates a string by repeating a char n times.
func RepeatString(char string, n int) string {
	return strings.Repeat(char, n)
}

// MakeContext creates a context with timeout.
func MakeContext(t *testing.T, timeout time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), timeout)
}

// SlowReader simulates a slow network reader.
type SlowReader struct {
	data  []byte
	pos   int
	delay time.Duration
}

// NewSlowReader creates a reader that reads slowly.
func NewSlowReader(data []byte, bytesPerSecond int) *SlowReader {
	delay := time.Second / time.Duration(bytesPerSecond)
	return &SlowReader{data: data, delay: delay}
}

// Read implements io.Reader with artificial delays.
func (r *SlowReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, fmt.Errorf("EOF")
	}
	time.Sleep(r.delay)
	n = copy(p, r.data[r.pos:r.pos+1])
	r.pos += n
	return n, nil
}

// FailingWriter is a writer that fails after n bytes.
type FailingWriter struct {
	bytesUntilFail int
	written        int
}

// NewFailingWriter creates a writer that fails after n bytes.
func NewFailingWriter(bytesUntilFail int) *FailingWriter {
	return &FailingWriter{bytesUntilFail: bytesUntilFail}
}

// Write implements io.Writer that fails after configured bytes.
func (w *FailingWriter) Write(p []byte) (n int, err error) {
	if w.written >= w.bytesUntilFail {
		return 0, fmt.Errorf("simulated disk full")
	}
	remaining := w.bytesUntilFail - w.written
	if len(p) > remaining {
		w.written += remaining
		return remaining, fmt.Errorf("simulated disk full")
	}
	w.written += len(p)
	return len(p), nil
}
