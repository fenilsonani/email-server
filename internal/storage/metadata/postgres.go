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
	"time"

	_ "github.com/lib/pq"
)

//go:embed migrations/postgres/*.sql
var postgresMigrationsFS embed.FS

// PostgresDB wraps the PostgreSQL database connection and implements Store interface
type PostgresDB struct {
	*sql.DB
	dsn string
}

// PostgresConfig holds PostgreSQL-specific configuration
type PostgresConfig struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultPostgresConfig returns sensible defaults for PostgreSQL
func DefaultPostgresDBConfig() PostgresConfig {
	return PostgresConfig{
		MaxOpenConns:    100,
		MaxIdleConns:    25,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

// OpenPostgres opens a PostgreSQL database connection with configuration
func OpenPostgres(cfg PostgresConfig) (*PostgresDB, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("database DSN is required")
	}

	// Apply defaults if not set
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = 100
	}
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = 25
	}

	db, err := sql.Open("postgres", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresDB{DB: db, dsn: cfg.DSN}, nil
}

// Driver returns the database driver name
func (db *PostgresDB) Driver() string {
	return "postgres"
}

// Ping checks database connectivity
func (db *PostgresDB) Ping(ctx context.Context) error {
	return db.DB.PingContext(ctx)
}

// Stats returns database statistics
func (db *PostgresDB) Stats() DBStats {
	stats := db.DB.Stats()
	return DBStats{
		MaxOpenConnections: stats.MaxOpenConnections,
		OpenConnections:    stats.OpenConnections,
		InUse:              stats.InUse,
		Idle:               stats.Idle,
		WaitCount:          stats.WaitCount,
		WaitDuration:       stats.WaitDuration,
		MaxIdleClosed:      stats.MaxIdleClosed,
		MaxIdleTimeClosed:  stats.MaxIdleTimeClosed,
		MaxLifetimeClosed:  stats.MaxLifetimeClosed,
	}
}

// Migrate runs all pending database migrations for PostgreSQL
func (db *PostgresDB) Migrate(ctx context.Context) error {
	// Create schema_migrations table if not exists
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

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
			return fmt.Errorf("failed to apply migration %d (%s): %w", m.version, m.name, err)
		}
	}

	return nil
}

func (db *PostgresDB) getSchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := db.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations",
	).Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

func (db *PostgresDB) loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(postgresMigrationsFS, "migrations/postgres")
	if err != nil {
		// If postgres migrations don't exist yet, return empty
		return nil, nil
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

		content, err := fs.ReadFile(postgresMigrationsFS, "migrations/postgres/"+entry.Name())
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

func (db *PostgresDB) applyMigration(ctx context.Context, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Execute migration SQL
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("migration SQL error: %w", err)
	}

	// Record migration
	_, err = tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (version) VALUES ($1)",
		m.version,
	)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return tx.Commit()
}

// Close closes the database connection
func (db *PostgresDB) Close() error {
	return db.DB.Close()
}

// RawDB returns the underlying *sql.DB for backward compatibility
func (db *PostgresDB) RawDB() *sql.DB {
	return db.DB
}
