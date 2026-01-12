package metadata

import (
	"context"
	"database/sql"
	"time"
)

// Store defines the database interface that both SQLite and PostgreSQL implement
type Store interface {
	// Transaction management
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)

	// Query execution
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row

	// Lifecycle
	Close() error
	Migrate(ctx context.Context) error
	Ping(ctx context.Context) error

	// Statistics
	Stats() DBStats

	// Driver info
	Driver() string

	// RawDB returns the underlying *sql.DB for components that need direct access
	// This is for backward compatibility with existing code
	RawDB() *sql.DB
}

// DBStats holds database statistics
type DBStats struct {
	// Pool statistics
	MaxOpenConnections int           `json:"max_open_connections"`
	OpenConnections    int           `json:"open_connections"`
	InUse              int           `json:"in_use"`
	Idle               int           `json:"idle"`
	WaitCount          int64         `json:"wait_count"`
	WaitDuration       time.Duration `json:"wait_duration"`
	MaxIdleClosed      int64         `json:"max_idle_closed"`
	MaxIdleTimeClosed  int64         `json:"max_idle_time_closed"`
	MaxLifetimeClosed  int64         `json:"max_lifetime_closed"`
}

// DBConfig holds common database configuration
type DBConfig struct {
	// Driver selection: "sqlite3" or "postgres"
	Driver string

	// SQLite options
	Path string

	// PostgreSQL options
	DSN string

	// Connection pool settings
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultDBConfig returns sensible defaults for database configuration
func DefaultDBConfig() DBConfig {
	return DBConfig{
		Driver:          "sqlite3",
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 0, // No limit
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

// DefaultPostgresConfig returns defaults optimized for PostgreSQL
func DefaultPostgresConfig() DBConfig {
	return DBConfig{
		Driver:          "postgres",
		MaxOpenConns:    100,
		MaxIdleConns:    25,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}
