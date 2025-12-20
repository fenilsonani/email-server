package metadata

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps the SQLite database connection
type DB struct {
	*sql.DB
}

// Open opens or creates a SQLite database at the given path
func Open(path string) (*DB, error) {
	// Enable foreign keys and WAL mode for better concurrency
	dsn := fmt.Sprintf("%s?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000", path)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{DB: db}, nil
}

// Migrate runs all pending database migrations
func (db *DB) Migrate(ctx context.Context) error {
	// Get current schema version
	currentVersion, err := db.getSchemaVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	// Read migration files
	migrations, err := db.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// Sort migrations by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	// Apply pending migrations
	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}

		if err := db.applyMigration(ctx, m); err != nil {
			return fmt.Errorf("failed to apply migration %d: %w", m.version, err)
		}
	}

	return nil
}

type migration struct {
	version int
	name    string
	sql     string
}

func (db *DB) getSchemaVersion(ctx context.Context) (int, error) {
	// Check if schema_migrations table exists
	var exists int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'",
	).Scan(&exists)
	if err != nil {
		return 0, err
	}

	if exists == 0 {
		return 0, nil
	}

	var version int
	err = db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations",
	).Scan(&version)
	if err != nil {
		return 0, err
	}

	return version, nil
}

func (db *DB) loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}

	var migrations []migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		// Parse version from filename (e.g., "001_initial.sql")
		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}

		version, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}

		content, err := fs.ReadFile(migrationsFS, "migrations/"+entry.Name())
		if err != nil {
			return nil, err
		}

		migrations = append(migrations, migration{
			version: version,
			name:    entry.Name(),
			sql:     string(content),
		})
	}

	return migrations, nil
}

func (db *DB) applyMigration(ctx context.Context, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Execute migration SQL
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("migration SQL error: %w", err)
	}

	return tx.Commit()
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}
