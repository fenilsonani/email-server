package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/testutil"
)

// TestOpenPostgres tests PostgreSQL database connection
func TestOpenPostgres(t *testing.T) {
	if !testutil.PostgresAvailable() {
		t.Skip("PostgreSQL not configured (set TEST_POSTGRES_DSN)")
	}

	t.Run("open_postgres_connection", func(t *testing.T) {
		cfg := PostgresConfig{
			DSN:             testutil.TestPostgresDSN(),
			MaxOpenConns:    50,
			MaxIdleConns:    10,
			ConnMaxLifetime: 30 * time.Minute,
		}

		db, err := OpenPostgres(cfg)
		if err != nil {
			t.Fatalf("Failed to open PostgreSQL: %v", err)
		}
		defer db.Close()

		if db.Driver() != "postgres" {
			t.Errorf("Expected driver 'postgres', got %q", db.Driver())
		}
	})

	t.Run("postgres_invalid_dsn", func(t *testing.T) {
		cfg := PostgresConfig{
			DSN: "invalid://connection/string",
		}

		_, err := OpenPostgres(cfg)
		if err == nil {
			t.Error("Expected error with invalid DSN")
		}
	})
}

// TestPostgresBasics tests basic PostgreSQL operations
func TestPostgresBasics(t *testing.T) {
	if !testutil.PostgresAvailable() {
		t.Skip("PostgreSQL not configured (set TEST_POSTGRES_DSN)")
	}

	db, err := OpenPostgres(PostgresConfig{
		DSN: testutil.TestPostgresDSN(),
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	t.Run("ping_succeeds", func(t *testing.T) {
		if err := db.Ping(ctx); err != nil {
			t.Errorf("Ping failed: %v", err)
		}
	})

	t.Run("driver_name", func(t *testing.T) {
		if db.Driver() != "postgres" {
			t.Errorf("Expected postgres driver, got %v", db.Driver())
		}
	})

	t.Run("stats_available", func(t *testing.T) {
		stats := db.Stats()
		if stats.MaxOpenConnections < 1 {
			t.Errorf("MaxOpenConnections not set: %d", stats.MaxOpenConnections)
		}
	})
}

// TestPostgresTransaction tests PostgreSQL transactions
func TestPostgresTransaction(t *testing.T) {
	if !testutil.PostgresAvailable() {
		t.Skip("PostgreSQL not configured (set TEST_POSTGRES_DSN)")
	}

	db, err := OpenPostgres(PostgresConfig{
		DSN: testutil.TestPostgresDSN(),
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	testTableName := fmt.Sprintf("test_txn_%d", time.Now().UnixNano())

	// Create test table
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			value TEXT NOT NULL
		)
	`, testTableName))
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}
	defer db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", testTableName))

	t.Run("transaction_commit", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("Failed to begin transaction: %v", err)
		}

		if _, err := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (value) VALUES ($1)", testTableName), "test_value"); err != nil {
			tx.Rollback()
			t.Fatalf("Failed to insert: %v", err)
		}

		if err := tx.Commit(); err != nil {
			t.Fatalf("Failed to commit: %v", err)
		}

		// Verify data was committed
		var count int
		err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE value = $1", testTableName), "test_value").Scan(&count)
		if err != nil {
			t.Errorf("Count query failed: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 row, got %d", count)
		}
	})

	t.Run("transaction_rollback", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("Failed to begin transaction: %v", err)
		}

		if _, err := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (value) VALUES ($1)", testTableName), "rollback_value"); err != nil {
			tx.Rollback()
			t.Fatalf("Failed to insert: %v", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("Failed to rollback: %v", err)
		}

		// Verify data was NOT committed
		var count int
		err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE value = $1", testTableName), "rollback_value").Scan(&count)
		if err != nil {
			t.Errorf("Count query failed: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0 rows after rollback, got %d", count)
		}
	})
}

// TestPostgresConcurrency tests concurrent PostgreSQL access
func TestPostgresConcurrency(t *testing.T) {
	if !testutil.PostgresAvailable() {
		t.Skip("PostgreSQL not configured (set TEST_POSTGRES_DSN)")
	}

	db, err := OpenPostgres(PostgresConfig{
		DSN: testutil.TestPostgresDSN(),
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	testTableName := fmt.Sprintf("test_concurrent_%d", time.Now().UnixNano())

	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			value INTEGER NOT NULL
		)
	`, testTableName))
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}
	defer db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", testTableName))

	t.Run("concurrent_inserts", func(t *testing.T) {
		testutil.RunConcurrent(t, 20, func(i int) error {
			_, err := db.ExecContext(context.Background(),
				fmt.Sprintf("INSERT INTO %s (value) VALUES ($1)", testTableName), i)
			return err
		})

		var count int
		err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", testTableName)).Scan(&count)
		if err != nil {
			t.Fatalf("Count query failed: %v", err)
		}
		if count != 20 {
			t.Errorf("Expected 20 rows, got %d", count)
		}
	})

	t.Run("concurrent_reads", func(t *testing.T) {
		testutil.RunConcurrent(t, 30, func(i int) error {
			rows, err := db.QueryContext(context.Background(),
				fmt.Sprintf("SELECT * FROM %s LIMIT 5", testTableName))
			if err != nil {
				return err
			}
			defer rows.Close()
			return nil
		})
	})
}

// TestPostgresForeignKeys tests foreign key constraints
func TestPostgresForeignKeys(t *testing.T) {
	if !testutil.PostgresAvailable() {
		t.Skip("PostgreSQL not configured (set TEST_POSTGRES_DSN)")
	}

	db, err := OpenPostgres(PostgresConfig{
		DSN: testutil.TestPostgresDSN(),
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	timestamp := time.Now().UnixNano()
	parentTable := fmt.Sprintf("parent_%d", timestamp)
	childTable := fmt.Sprintf("child_%d", timestamp)

	// Create tables
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL
		)
	`, parentTable))
	if err != nil {
		t.Fatalf("Failed to create parent table: %v", err)
	}
	defer db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", parentTable))

	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			parent_id INTEGER NOT NULL,
			FOREIGN KEY (parent_id) REFERENCES %s(id)
		)
	`, childTable, parentTable))
	if err != nil {
		t.Fatalf("Failed to create child table: %v", err)
	}
	defer db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", childTable))

	t.Run("foreign_key_constraint_enforced", func(t *testing.T) {
		_, err := db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (parent_id) VALUES ($1)", childTable), 999)
		if err == nil {
			t.Error("Expected foreign key constraint error")
		}
	})

	t.Run("foreign_key_valid_reference", func(t *testing.T) {
		// Insert parent
		_, err := db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (name) VALUES ($1)", parentTable), "parent1")
		if err != nil {
			t.Fatalf("Failed to insert parent: %v", err)
		}

		// Get parent ID
		var parentID int
		err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT id FROM %s WHERE name = $1", parentTable), "parent1").Scan(&parentID)
		if err != nil {
			t.Fatalf("Failed to get parent ID: %v", err)
		}

		// Insert child with valid parent
		_, err = db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (parent_id) VALUES ($1)", childTable), parentID)
		if err != nil {
			t.Errorf("Failed to insert child with valid parent: %v", err)
		}
	})
}

// TestPostgresErrorHandling tests error scenarios
func TestPostgresErrorHandling(t *testing.T) {
	if !testutil.PostgresAvailable() {
		t.Skip("PostgreSQL not configured (set TEST_POSTGRES_DSN)")
	}

	db, err := OpenPostgres(PostgresConfig{
		DSN: testutil.TestPostgresDSN(),
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	t.Run("query_nonexistent_table", func(t *testing.T) {
		_, err := db.QueryContext(ctx, "SELECT * FROM nonexistent_table_xyz_123")
		if err == nil {
			t.Error("Expected error when querying nonexistent table")
		}
	})

	t.Run("invalid_sql_syntax", func(t *testing.T) {
		_, err := db.ExecContext(ctx, "INVALID SQL SYNTAX HERE")
		if err == nil {
			t.Error("Expected error for invalid SQL")
		}
	})

	t.Run("duplicate_unique_constraint", func(t *testing.T) {
		testTableName := fmt.Sprintf("unique_test_%d", time.Now().UnixNano())

		// Create table with unique constraint
		_, err := db.ExecContext(ctx, fmt.Sprintf(`
			CREATE TABLE %s (
				id SERIAL PRIMARY KEY,
				email TEXT UNIQUE NOT NULL
			)
		`, testTableName))
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}
		defer db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", testTableName))

		// Insert first row
		_, err = db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (email) VALUES ($1)", testTableName), "test@example.com")
		if err != nil {
			t.Fatalf("Failed to insert first row: %v", err)
		}

		// Try to insert duplicate
		_, err = db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (email) VALUES ($1)", testTableName), "test@example.com")
		if err == nil {
			t.Error("Expected unique constraint violation")
		}
	})
}

// TestPostgresQueryRow tests QueryRowContext
func TestPostgresQueryRow(t *testing.T) {
	if !testutil.PostgresAvailable() {
		t.Skip("PostgreSQL not configured (set TEST_POSTGRES_DSN)")
	}

	db, err := OpenPostgres(PostgresConfig{
		DSN: testutil.TestPostgresDSN(),
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	testTableName := fmt.Sprintf("row_test_%d", time.Now().UnixNano())

	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			value TEXT NOT NULL
		)
	`, testTableName))
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}
	defer db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", testTableName))

	_, err = db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (value) VALUES ($1)", testTableName), "test_value")
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	t.Run("query_row_success", func(t *testing.T) {
		row := db.QueryRowContext(ctx, fmt.Sprintf("SELECT value FROM %s WHERE id = (SELECT MIN(id) FROM %s)", testTableName, testTableName))
		var value string
		if err := row.Scan(&value); err != nil {
			t.Errorf("Scan failed: %v", err)
		}
		if value != "test_value" {
			t.Errorf("Got %q, want test_value", value)
		}
	})

	t.Run("query_row_not_found", func(t *testing.T) {
		row := db.QueryRowContext(ctx, fmt.Sprintf("SELECT value FROM %s WHERE id = $1", testTableName), 99999)
		var value string
		if err := row.Scan(&value); err != sql.ErrNoRows {
			t.Errorf("Expected sql.ErrNoRows, got %v", err)
		}
	})
}

// TestPostgresArrayTypes tests PostgreSQL array types
func TestPostgresArrayTypes(t *testing.T) {
	if !testutil.PostgresAvailable() {
		t.Skip("PostgreSQL not configured (set TEST_POSTGRES_DSN)")
	}

	db, err := OpenPostgres(PostgresConfig{
		DSN: testutil.TestPostgresDSN(),
	})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	testTableName := fmt.Sprintf("array_test_%d", time.Now().UnixNano())

	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			tags TEXT[] NOT NULL
		)
	`, testTableName))
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}
	defer db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", testTableName))

	t.Run("array_insert_and_query", func(t *testing.T) {
		tags := "{tag1,tag2,tag3}"
		_, err := db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (tags) VALUES ($1)", testTableName), tags)
		if err != nil {
			t.Fatalf("Failed to insert array: %v", err)
		}

		var retrieved string
		err = db.QueryRowContext(ctx, fmt.Sprintf("SELECT tags FROM %s WHERE id = (SELECT MIN(id) FROM %s)", testTableName, testTableName)).Scan(&retrieved)
		if err != nil {
			t.Fatalf("Failed to query array: %v", err)
		}

		if retrieved != tags {
			t.Errorf("Got %q, want %q", retrieved, tags)
		}
	})
}

// BenchmarkPostgresInsert benchmarks PostgreSQL insert operations
func BenchmarkPostgresInsert(b *testing.B) {
	if !testutil.PostgresAvailable() {
		b.Skip("PostgreSQL not configured")
	}

	db, err := OpenPostgres(PostgresConfig{
		DSN: testutil.TestPostgresDSN(),
	})
	if err != nil {
		b.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	testTableName := fmt.Sprintf("bench_insert_%d", time.Now().UnixNano())
	db.ExecContext(context.Background(), fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			value TEXT NOT NULL
		)
	`, testTableName))
	defer db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", testTableName))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.ExecContext(context.Background(),
			fmt.Sprintf("INSERT INTO %s (value) VALUES ($1)", testTableName), "test")
	}
}

// BenchmarkPostgresQuery benchmarks PostgreSQL query operations
func BenchmarkPostgresQuery(b *testing.B) {
	if !testutil.PostgresAvailable() {
		b.Skip("PostgreSQL not configured")
	}

	db, err := OpenPostgres(PostgresConfig{
		DSN: testutil.TestPostgresDSN(),
	})
	if err != nil {
		b.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	testTableName := fmt.Sprintf("bench_query_%d", time.Now().UnixNano())
	db.ExecContext(context.Background(), fmt.Sprintf(`
		CREATE TABLE %s (
			id SERIAL PRIMARY KEY,
			value TEXT NOT NULL
		)
	`, testTableName))
	defer db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", testTableName))

	// Insert test data
	for i := 0; i < 100; i++ {
		db.ExecContext(context.Background(),
			fmt.Sprintf("INSERT INTO %s (value) VALUES ($1)", testTableName), "test")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, _ := db.QueryContext(context.Background(),
			fmt.Sprintf("SELECT * FROM %s LIMIT 10", testTableName))
		rows.Close()
	}
}
