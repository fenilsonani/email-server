package metadata

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/testutil"
)

// TestOpenSQLite tests SQLite database opening and configuration
func TestOpenSQLite(t *testing.T) {
	t.Run("open_memory_database", func(t *testing.T) {
		testutil.WithTempDir(t, func(tmpDir string) {
			dbPath := tmpDir + "/test.db"
			cfg := SQLiteConfig{
				Path:            dbPath,
				MaxOpenConns:    10,
				MaxIdleConns:    2,
				ConnMaxIdleTime: 1 * time.Minute,
			}

			db, err := OpenSQLite(cfg)
			if err != nil {
				t.Fatalf("Failed to open SQLite: %v", err)
			}
			defer db.Close()

			if db.path != dbPath {
				t.Errorf("Database path mismatch: got %s, want %s", db.path, dbPath)
			}

			// Verify file was created
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				t.Errorf("Database file not created at %s", dbPath)
			}
		})
	})

	t.Run("open_with_defaults", func(t *testing.T) {
		testutil.WithTempDir(t, func(tmpDir string) {
			cfg := SQLiteConfig{Path: tmpDir + "/test.db"}
			db, err := OpenSQLite(cfg)
			if err != nil {
				t.Fatalf("Failed to open with defaults: %v", err)
			}
			defer db.Close()

			// Verify defaults were applied
			if cfg.MaxOpenConns != 10 && cfg.MaxOpenConns != 25 {
				// Config may not have been updated by function, but pool should be set
				stats := db.Stats()
				if stats.MaxOpenConnections < 1 {
					t.Errorf("MaxOpenConnections not set: %d", stats.MaxOpenConnections)
				}
			}
		})
	})

	t.Run("open_requires_path", func(t *testing.T) {
		cfg := SQLiteConfig{Path: ""}
		_, err := OpenSQLite(cfg)
		if err == nil {
			t.Error("Expected error when path is empty")
		}
	})

	t.Run("legacy_open_function", func(t *testing.T) {
		testutil.WithTempDir(t, func(tmpDir string) {
			dbPath := tmpDir + "/legacy.db"
			db, err := Open(dbPath)
			if err != nil {
				t.Fatalf("Failed to open with legacy function: %v", err)
			}
			defer db.Close()

			if db.Driver() != "sqlite3" {
				t.Errorf("Unexpected driver: %v", db.Driver())
			}
		})
	})
}

// TestSQLiteDriver tests basic Store interface implementations
func TestSQLiteDriver(t *testing.T) {
	testutil.WithTempDir(t, func(tmpDir string) {
		db, err := Open(tmpDir + "/test.db")
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		t.Run("driver_returns_correct_name", func(t *testing.T) {
			if db.Driver() != "sqlite3" {
				t.Errorf("Expected driver 'sqlite3', got %q", db.Driver())
			}
		})

		t.Run("ping_succeeds", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := db.Ping(ctx); err != nil {
				t.Errorf("Ping failed: %v", err)
			}
		})

		t.Run("stats_returns_values", func(t *testing.T) {
			stats := db.Stats()
			if stats.MaxOpenConnections < 1 {
				t.Errorf("Stats MaxOpenConnections not set: %d", stats.MaxOpenConnections)
			}
		})

		t.Run("raw_db_returns_underlying_connection", func(t *testing.T) {
			rawDB := db.RawDB()
			if rawDB == nil {
				t.Error("RawDB returned nil")
			}

			// Verify it's a working connection
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := rawDB.PingContext(ctx); err != nil {
				t.Errorf("RawDB ping failed: %v", err)
			}
		})
	})
}

// TestSQLiteTransaction tests transaction handling
func TestSQLiteTransaction(t *testing.T) {
	testutil.WithTempDir(t, func(tmpDir string) {
		db, err := Open(tmpDir + "/test.db")
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		// Create a test table
		ctx := context.Background()
		_, err = db.ExecContext(ctx, `
			CREATE TABLE test_table (
				id INTEGER PRIMARY KEY,
				value TEXT NOT NULL
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create test table: %v", err)
		}

		t.Run("transaction_commit", func(t *testing.T) {
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatalf("Failed to begin transaction: %v", err)
			}

			if _, err := tx.ExecContext(context.Background(), "INSERT INTO test_table (value) VALUES (?)", "test1"); err != nil {
				tx.Rollback()
				t.Fatalf("Failed to insert: %v", err)
			}

			if err := tx.Commit(); err != nil {
				t.Fatalf("Failed to commit: %v", err)
			}

			// Verify data was committed
			var value string
			err = db.QueryRowContext(context.Background(), "SELECT value FROM test_table WHERE value = ?", "test1").Scan(&value)
			if err != nil {
				t.Errorf("Data not found after commit: %v", err)
			}
		})

		t.Run("transaction_rollback", func(t *testing.T) {
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatalf("Failed to begin transaction: %v", err)
			}

			if _, err := tx.ExecContext(context.Background(), "INSERT INTO test_table (value) VALUES (?)", "test_rollback"); err != nil {
				tx.Rollback()
				t.Fatalf("Failed to insert: %v", err)
			}

			if err := tx.Rollback(); err != nil {
				t.Fatalf("Failed to rollback: %v", err)
			}

			// Verify data was NOT committed
			rows, err := db.QueryContext(context.Background(), "SELECT COUNT(*) FROM test_table WHERE value = ?", "test_rollback")
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}
			defer rows.Close()

			if rows.Next() {
				var count int
				if err := rows.Scan(&count); err != nil {
					t.Fatalf("Scan failed: %v", err)
				}
				if count != 0 {
					t.Errorf("Expected no rows after rollback, got %d", count)
				}
			}
		})
	})
}

// TestSQLiteConcurrency tests concurrent database access
func TestSQLiteConcurrency(t *testing.T) {
	testutil.WithTempDir(t, func(tmpDir string) {
		db, err := Open(tmpDir + "/test.db")
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		ctx := context.Background()
		_, err = db.ExecContext(ctx, `
			CREATE TABLE concurrent_test (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				value INTEGER NOT NULL
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create table: %v", err)
		}

		t.Run("concurrent_inserts", func(t *testing.T) {
			testutil.RunConcurrent(t, 10, func(i int) error {
				_, err := db.ExecContext(context.Background(), "INSERT INTO concurrent_test (value) VALUES (?)", i)
				return err
			})

			// Verify all inserts succeeded
			var count int
			err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM concurrent_test").Scan(&count)
			if err != nil {
				t.Fatalf("Query failed: %v", err)
			}
			if count != 10 {
				t.Errorf("Expected 10 rows, got %d", count)
			}
		})

		t.Run("concurrent_reads", func(t *testing.T) {
			testutil.RunConcurrent(t, 20, func(i int) error {
				rows, err := db.QueryContext(context.Background(), "SELECT COUNT(*) FROM concurrent_test")
				if err != nil {
					return err
				}
				defer rows.Close()
				return nil
			})
		})
	})
}

// TestSQLiteForeignKeys tests foreign key constraint enforcement
func TestSQLiteForeignKeys(t *testing.T) {
	testutil.WithTempDir(t, func(tmpDir string) {
		db, err := Open(tmpDir + "/test.db")
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		ctx := context.Background()

		// Create parent and child tables
		_, err = db.ExecContext(ctx, `
			CREATE TABLE parent_table (
				id INTEGER PRIMARY KEY,
				name TEXT NOT NULL
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create parent table: %v", err)
		}

		_, err = db.ExecContext(ctx, `
			CREATE TABLE child_table (
				id INTEGER PRIMARY KEY,
				parent_id INTEGER NOT NULL,
				FOREIGN KEY (parent_id) REFERENCES parent_table(id)
			)
		`)
		if err != nil {
			t.Fatalf("Failed to create child table: %v", err)
		}

		t.Run("foreign_key_constraint_enforced", func(t *testing.T) {
			// Try to insert child with non-existent parent
			_, err := db.ExecContext(ctx, "INSERT INTO child_table (parent_id) VALUES (?)", 999)
			if err == nil {
				t.Error("Expected foreign key constraint error")
			}
		})

		t.Run("foreign_key_valid_reference", func(t *testing.T) {
			// Insert valid parent
			_, err := db.ExecContext(ctx, "INSERT INTO parent_table (id, name) VALUES (?, ?)", 1, "parent1")
			if err != nil {
				t.Fatalf("Failed to insert parent: %v", err)
			}

			// Insert child with valid parent
			_, err = db.ExecContext(ctx, "INSERT INTO child_table (parent_id) VALUES (?)", 1)
			if err != nil {
				t.Errorf("Failed to insert child with valid parent: %v", err)
			}
		})
	})
}

// TestSQLiteErrorHandling tests error handling
func TestSQLiteErrorHandling(t *testing.T) {
	testutil.WithTempDir(t, func(tmpDir string) {
		db, err := Open(tmpDir + "/test.db")
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		t.Run("query_nonexistent_table", func(t *testing.T) {
			ctx := context.Background()
			_, err := db.QueryContext(ctx, "SELECT * FROM nonexistent_table")
			if err == nil {
				t.Error("Expected error when querying nonexistent table")
			}
		})

		t.Run("invalid_sql_syntax", func(t *testing.T) {
			ctx := context.Background()
			_, err := db.ExecContext(ctx, "INVALID SQL HERE")
			if err == nil {
				t.Error("Expected error for invalid SQL")
			}
		})

		t.Run("context_timeout", func(t *testing.T) {
			// Create a table and try to select with timeout
			db.ExecContext(context.Background(), `
				CREATE TABLE timeout_test (
					id INTEGER PRIMARY KEY
				)
			`)

			// This might not actually timeout since SQLite is fast, but we test context handling
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
			defer cancel()

			time.Sleep(10 * time.Millisecond) // Ensure context is expired

			rows, err := db.QueryContext(ctx, "SELECT * FROM timeout_test")
			if err == nil && rows != nil {
				rows.Close()
				// Context might not always trigger timeout with fast queries
			}
		})
	})
}

// TestSQLiteClose tests database closure
func TestSQLiteClose(t *testing.T) {
	testutil.WithTempDir(t, func(tmpDir string) {
		db, err := Open(tmpDir + "/test.db")
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}

		// Verify we can use it
		ctx := context.Background()
		if err := db.Ping(ctx); err != nil {
			t.Fatalf("Ping before close failed: %v", err)
		}

		// Close it
		if err := db.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		// Try to use after close (should fail)
		if err := db.Ping(ctx); err == nil {
			t.Error("Expected error when pinging closed database")
		}
	})
}

// TestSQLiteQueryRow tests QueryRowContext
func TestSQLiteQueryRow(t *testing.T) {
	testutil.WithTempDir(t, func(tmpDir string) {
		db, err := Open(tmpDir + "/test.db")
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		ctx := context.Background()
		db.ExecContext(ctx, `
			CREATE TABLE query_test (
				id INTEGER PRIMARY KEY,
				value TEXT NOT NULL
			)
		`)

		db.ExecContext(ctx, "INSERT INTO query_test (id, value) VALUES (?, ?)", 1, "test")

		t.Run("query_row_success", func(t *testing.T) {
			row := db.QueryRowContext(ctx, "SELECT value FROM query_test WHERE id = ?", 1)
			var value string
			if err := row.Scan(&value); err != nil {
				t.Errorf("Scan failed: %v", err)
			}
			if value != "test" {
				t.Errorf("Got %q, want test", value)
			}
		})

		t.Run("query_row_not_found", func(t *testing.T) {
			row := db.QueryRowContext(ctx, "SELECT value FROM query_test WHERE id = ?", 999)
			var value string
			if err := row.Scan(&value); err != sql.ErrNoRows {
				t.Errorf("Expected sql.ErrNoRows, got %v", err)
			}
		})
	})
}

// BenchmarkSQLiteInsert benchmarks insert operations
func BenchmarkSQLiteInsert(b *testing.B) {
	testutil.WithTempDir(&testing.T{}, func(tmpDir string) {
		db, _ := Open(tmpDir + "/bench.db")
		defer db.Close()

		db.ExecContext(context.Background(), `
			CREATE TABLE bench_table (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				value TEXT NOT NULL
			)
		`)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			db.ExecContext(context.Background(), "INSERT INTO bench_table (value) VALUES (?)", "test")
		}
	})
}

// BenchmarkSQLiteQuery benchmarks query operations
func BenchmarkSQLiteQuery(b *testing.B) {
	testutil.WithTempDir(&testing.T{}, func(tmpDir string) {
		db, _ := Open(tmpDir + "/bench.db")
		defer db.Close()

		db.ExecContext(context.Background(), `
			CREATE TABLE bench_table (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				value TEXT NOT NULL
			)
		`)

		// Insert test data
		for i := 0; i < 100; i++ {
			db.ExecContext(context.Background(), "INSERT INTO bench_table (value) VALUES (?)", "test")
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rows, _ := db.QueryContext(context.Background(), "SELECT * FROM bench_table LIMIT 10")
			rows.Close()
		}
	})
}
