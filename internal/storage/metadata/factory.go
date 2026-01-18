package metadata

import (
	"fmt"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
)

// OpenFromConfig creates a database connection based on configuration
func OpenFromConfig(cfg config.DatabaseConfig) (Store, error) {
	// Parse duration strings
	connMaxLifetime, err := parseDurationOrZero(cfg.ConnMaxLifetime)
	if err != nil {
		return nil, fmt.Errorf("invalid conn_max_lifetime: %w", err)
	}

	connMaxIdleTime, err := parseDurationOrZero(cfg.ConnMaxIdleTime)
	if err != nil {
		return nil, fmt.Errorf("invalid conn_max_idle_time: %w", err)
	}

	driver := cfg.Driver
	if driver == "" {
		driver = "sqlite3" // Default to SQLite for backward compatibility
	}

	switch driver {
	case "sqlite3":
		sqliteCfg := SQLiteConfig{
			Path:            cfg.Path,
			MaxOpenConns:    cfg.MaxOpenConns,
			MaxIdleConns:    cfg.MaxIdleConns,
			ConnMaxLifetime: connMaxLifetime,
			ConnMaxIdleTime: connMaxIdleTime,
		}
		return OpenSQLite(sqliteCfg)

	case "postgres":
		postgresCfg := PostgresConfig{
			DSN:             cfg.DSN,
			MaxOpenConns:    cfg.MaxOpenConns,
			MaxIdleConns:    cfg.MaxIdleConns,
			ConnMaxLifetime: connMaxLifetime,
			ConnMaxIdleTime: connMaxIdleTime,
		}
		return OpenPostgres(postgresCfg)

	default:
		return nil, fmt.Errorf("unsupported database driver: %s (supported: sqlite3, postgres)", driver)
	}
}

// parseDurationOrZero parses a duration string, treating empty or "0" as zero duration
func parseDurationOrZero(s string) (time.Duration, error) {
	if s == "" || s == "0" {
		return 0, nil
	}
	return time.ParseDuration(s)
}

// MustOpenFromConfig creates a database connection or panics on error
func MustOpenFromConfig(cfg config.DatabaseConfig) Store {
	db, err := OpenFromConfig(cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to open database: %v", err))
	}
	return db
}
