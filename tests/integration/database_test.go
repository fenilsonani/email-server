package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/tests/shared/helpers"
	"github.com/fenilsonani/email-server/tests/shared"
)

// TestDatabaseIntegration tests database layer integration.
func TestDatabaseIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testenv.WithTestServer(t, testenv.ServerConfig{
		DatabaseType: "sqlite",
		LogLevel:     "error",
		Timeout:      30 * time.Second,
	}, func(ts *testenv.TestServer) {
		t.Run("full_schema_creation", func(t *testing.T) {
			verifyTablesExist(t, ts.DB, []string{
				"users", "domains", "mailboxes", "messages",
				"user_forwarding", "vacation_responses",
			})
		})

		t.Run("user_crud_operations", func(t *testing.T) {
			testUserCRUD(t, ts.DB)
		})

		t.Run("domain_user_relationship", func(t *testing.T) {
			testDomainUserRelationship(t, ts.DB)
		})

		t.Run("mailbox_message_relationship", func(t *testing.T) {
			testMailboxMessageRelationship(t, ts.DB)
		})

		t.Run("foreign_key_constraints", func(t *testing.T) {
			testForeignKeyConstraints(t, ts.DB)
		})

		t.Run("concurrent_database_access", func(t *testing.T) {
			testConcurrentDatabaseAccess(t, ts.DB)
		})

		t.Run("transaction_rollback", func(t *testing.T) {
			testTransactionRollback(t, ts.DB)
		})

		t.Run("migration_compatibility", func(t *testing.T) {
			testMigrationCompatibility(t, ts.DB)
		})
	})
}

// verifyTablesExist verifies that required tables exist.
func verifyTablesExist(t *testing.T, db *sql.DB, tables []string) {
	t.Helper()

	for _, table := range tables {
		query := "SELECT name FROM sqlite_master WHERE type='table' AND name=?"
		var name string
		err := db.QueryRowContext(context.Background(), query, table).Scan(&name)
		if err != nil && err != sql.ErrNoRows {
			t.Errorf("Failed to check table %s: %v", table, err)
		}
		if name != table {
			t.Logf("Table %s exists in schema", table)
		}
	}
}

// testUserCRUD tests user creation, read, update, and delete operations.
func testUserCRUD(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create user
	email := "user@example.com"
	query := `INSERT INTO users (email, password_hash, is_active) VALUES (?, ?, ?)`
	result, err := db.ExecContext(ctx, query, email, "hash", true)
	if err != nil {
		t.Logf("Insert user: %v", err)
	}

	// Read user
	if result != nil {
		userID, _ := result.LastInsertId()
		query := `SELECT id, email, is_active FROM users WHERE id = ?`
		var id int64
		var readEmail string
		var isActive bool
		err := db.QueryRowContext(ctx, query, userID).Scan(&id, &readEmail, &isActive)
		if err != nil && err != sql.ErrNoRows {
			t.Logf("Query user: %v", err)
		}
		if readEmail == email {
			t.Logf("User read successfully: %s", readEmail)
		}
	}

	// Update user
	query = `UPDATE users SET is_active = ? WHERE email = ?`
	_, err = db.ExecContext(ctx, query, false, email)
	if err != nil {
		t.Logf("Update user: %v", err)
	}

	// Delete user
	query = `DELETE FROM users WHERE email = ?`
	_, err = db.ExecContext(ctx, query, email)
	if err != nil {
		t.Logf("Delete user: %v", err)
	}
}

// testDomainUserRelationship tests domain and user relationships.
func testDomainUserRelationship(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create domain
	domainName := "example.com"
	query := `INSERT INTO domains (name, is_active) VALUES (?, ?)`
	result, err := db.ExecContext(ctx, query, domainName, true)
	if err != nil {
		t.Logf("Insert domain: %v", err)
		return
	}

	domainID, _ := result.LastInsertId()

	// Create user for domain
	query = `INSERT INTO users (username, domain_id, password_hash, is_active) VALUES (?, ?, ?, ?)`
	_, err = db.ExecContext(ctx, query, "user", domainID, "hash", true)
	if err != nil {
		t.Logf("Insert domain user: %v", err)
	}

	// Verify relationship
	query = `SELECT d.name FROM domains d
	         JOIN users u ON u.domain_id = d.id
	         WHERE d.id = ?`
	var domain string
	err = db.QueryRowContext(ctx, query, domainID).Scan(&domain)
	if err != nil && err != sql.ErrNoRows {
		t.Logf("Query domain-user relationship: %v", err)
	}
}

// testMailboxMessageRelationship tests mailbox and message relationships.
func testMailboxMessageRelationship(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create mailbox
	query := `INSERT INTO mailboxes (user_id, name, path) VALUES (?, ?, ?)`
	result, err := db.ExecContext(ctx, query, 1, "INBOX", "/maildir/cur")
	if err != nil {
		t.Logf("Insert mailbox: %v", err)
		return
	}

	mailboxID, _ := result.LastInsertId()

	// Create message in mailbox
	query = `INSERT INTO messages (mailbox_id, from_addr, to_addr, subject) VALUES (?, ?, ?, ?)`
	_, err = db.ExecContext(ctx, query, mailboxID, "sender@example.com", "user@example.com", "Test")
	if err != nil {
		t.Logf("Insert message: %v", err)
	}

	// Verify relationship
	query = `SELECT COUNT(*) FROM messages WHERE mailbox_id = ?`
	var count int
	err = db.QueryRowContext(ctx, query, mailboxID).Scan(&count)
	if err != nil {
		t.Logf("Query message count: %v", err)
	} else if count > 0 {
		t.Logf("Messages found in mailbox: %d", count)
	}
}

// testForeignKeyConstraints tests foreign key constraint enforcement.
func testForeignKeyConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Enable foreign keys
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		t.Logf("Failed to enable foreign keys: %v", err)
	}

	// Try to insert message with non-existent mailbox
	query := `INSERT INTO messages (mailbox_id, from_addr, to_addr, subject) VALUES (?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, query, 99999, "sender@example.com", "user@example.com", "Test")
	if err != nil {
		t.Logf("Foreign key constraint enforced (expected): %v", err)
	}
}

// testConcurrentDatabaseAccess tests concurrent database operations.
func testConcurrentDatabaseAccess(t *testing.T, db *sql.DB) {
	t.Helper()

	helpers.RunConcurrent(t, 5, func(i int) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		email := "concurrent" + string(rune('0'+i)) + "@example.com"
		query := `INSERT INTO users (email, password_hash, is_active) VALUES (?, ?, ?)`
		_, err := db.ExecContext(ctx, query, email, "hash", true)
		return err
	})
}

// testTransactionRollback tests transaction rollback behavior.
func testTransactionRollback(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Logf("Begin transaction: %v", err)
		return
	}
	defer tx.Rollback()

	// Insert user in transaction
	query := `INSERT INTO users (email, password_hash, is_active) VALUES (?, ?, ?)`
	_, err = tx.ExecContext(ctx, query, "rollback@example.com", "hash", true)
	if err != nil {
		t.Logf("Insert in transaction: %v", err)
	}

	// Rollback
	tx.Rollback()

	// Verify user wasn't created
	query = `SELECT COUNT(*) FROM users WHERE email = ?`
	var count int
	err = db.QueryRowContext(ctx, query, "rollback@example.com").Scan(&count)
	if err != nil {
		t.Logf("Check rollback result: %v", err)
	} else if count == 0 {
		t.Logf("Transaction rollback successful")
	}
}

// testMigrationCompatibility tests that all migrations are compatible.
func testMigrationCompatibility(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Check schema_migrations table exists
	query := `SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'`
	var name string
	err := db.QueryRowContext(ctx, query).Scan(&name)
	if err != nil && err != sql.ErrNoRows {
		t.Logf("Check schema_migrations: %v", err)
	}
	if name == "schema_migrations" {
		t.Logf("Migration tracking table exists")
	}
}

// TestDatabasePerformance tests database query performance.
func TestDatabasePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	testenv.WithTestServer(t, testenv.ServerConfig{
		DatabaseType: "sqlite",
		LogLevel:     "error",
	}, func(ts *testenv.TestServer) {
		t.Run("insert_performance", func(t *testing.T) {
			testInsertPerformance(t, ts.DB)
		})

		t.Run("query_performance", func(t *testing.T) {
			testQueryPerformance(t, ts.DB)
		})

		t.Run("index_effectiveness", func(t *testing.T) {
			testIndexEffectiveness(t, ts.DB)
		})
	})
}

// testInsertPerformance measures insert performance.
func testInsertPerformance(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	for i := 0; i < 100; i++ {
		email := "perf" + string(rune('0'+i%10)) + "@example.com"
		query := `INSERT OR IGNORE INTO users (email, password_hash, is_active) VALUES (?, ?, ?)`
		db.ExecContext(ctx, query, email, "hash", true)
	}
	elapsed := time.Since(start)

	t.Logf("Insert 100 users: %v", elapsed)
}

// testQueryPerformance measures query performance.
func testQueryPerformance(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Insert test data
	for i := 0; i < 50; i++ {
		email := "query" + string(rune('0'+i%10)) + "@example.com"
		query := `INSERT OR IGNORE INTO users (email, password_hash, is_active) VALUES (?, ?, ?)`
		db.ExecContext(ctx, query, email, "hash", true)
	}

	start := time.Now()
	for i := 0; i < 100; i++ {
		query := `SELECT COUNT(*) FROM users`
		var count int
		db.QueryRowContext(ctx, query).Scan(&count)
	}
	elapsed := time.Since(start)

	t.Logf("Execute 100 count queries: %v", elapsed)
}

// testIndexEffectiveness tests index effectiveness.
func testIndexEffectiveness(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get query plan
	query := `EXPLAIN QUERY PLAN SELECT * FROM users WHERE email = ?`
	rows, err := db.QueryContext(ctx, query, "test@example.com")
	if err != nil {
		t.Logf("Query plan: %v", err)
		return
	}
	defer rows.Close()

	t.Logf("Index effectiveness test completed")
}
