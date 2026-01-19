package search

import (
	"fmt"
	"time"
)

// EngineType represents the search engine type.
type EngineType string

const (
	// EngineBleve uses the Bleve embedded search engine
	EngineBleve EngineType = "bleve"

	// EngineSQLite uses SQLite FTS5 for search
	EngineSQLite EngineType = "sqlite"

	// EnginePostgres uses PostgreSQL tsvector for search
	EnginePostgres EngineType = "postgres"

	// EngineAuto automatically selects the best engine
	EngineAuto EngineType = "auto"
)

// Config holds search configuration.
type Config struct {
	// Enabled enables or disables the search functionality
	Enabled bool `koanf:"enabled"`

	// Engine specifies which search engine to use
	Engine EngineType `koanf:"engine"`

	// IndexPath is the path for the search index (Bleve)
	IndexPath string `koanf:"index_path"`

	// Realtime enables real-time indexing of new messages
	Realtime bool `koanf:"realtime"`

	// BatchSize is the number of documents to batch before flushing
	BatchSize int `koanf:"batch_size"`

	// FlushInterval is how often to flush pending batches
	FlushInterval string `koanf:"flush_interval"`

	// Timeout is the maximum time for a search operation
	Timeout string `koanf:"timeout"`

	// FuzzyEnabled enables fuzzy matching support
	FuzzyEnabled bool `koanf:"fuzzy_enabled"`

	// FuzzyDistance is the default edit distance for fuzzy matching
	FuzzyDistance int `koanf:"fuzzy_distance"`

	// HighlightEnabled enables result highlighting
	HighlightEnabled bool `koanf:"highlight_enabled"`

	// MaxResults is the maximum number of search results
	MaxResults int `koanf:"max_results"`

	// MinScore is the minimum relevance score for results
	MinScore float64 `koanf:"min_score"`

	// Workers is the number of indexing workers
	Workers int `koanf:"workers"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled:          true,
		Engine:           EngineAuto,
		IndexPath:        "./data/search.bleve",
		Realtime:         true,
		BatchSize:        100,
		FlushInterval:    "100ms",
		Timeout:          "5s",
		FuzzyEnabled:     true,
		FuzzyDistance:    2,
		HighlightEnabled: true,
		MaxResults:       1000,
		MinScore:         0.0,
		Workers:          2,
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil // No validation needed if disabled
	}

	// Validate engine type
	switch c.Engine {
	case EngineBleve, EngineSQLite, EnginePostgres, EngineAuto:
		// Valid
	default:
		return fmt.Errorf("invalid search engine: %s (must be bleve, sqlite, postgres, or auto)", c.Engine)
	}

	// Validate index path for Bleve
	if c.Engine == EngineBleve || c.Engine == EngineAuto {
		if c.IndexPath == "" {
			return fmt.Errorf("search.index_path is required for bleve engine")
		}
	}

	// Validate batch size
	if c.BatchSize < 1 {
		return fmt.Errorf("search.batch_size must be at least 1")
	}
	if c.BatchSize > 10000 {
		return fmt.Errorf("search.batch_size cannot exceed 10000")
	}

	// Validate flush interval
	if c.FlushInterval != "" {
		d, err := time.ParseDuration(c.FlushInterval)
		if err != nil {
			return fmt.Errorf("search.flush_interval is invalid: %w", err)
		}
		if d < time.Millisecond {
			return fmt.Errorf("search.flush_interval must be at least 1ms")
		}
		if d > time.Minute {
			return fmt.Errorf("search.flush_interval cannot exceed 1m")
		}
	}

	// Validate timeout
	if c.Timeout != "" {
		d, err := time.ParseDuration(c.Timeout)
		if err != nil {
			return fmt.Errorf("search.timeout is invalid: %w", err)
		}
		if d < 100*time.Millisecond {
			return fmt.Errorf("search.timeout must be at least 100ms")
		}
		if d > time.Minute {
			return fmt.Errorf("search.timeout cannot exceed 1m")
		}
	}

	// Validate fuzzy distance
	if c.FuzzyEnabled {
		if c.FuzzyDistance < 1 || c.FuzzyDistance > 3 {
			return fmt.Errorf("search.fuzzy_distance must be between 1 and 3")
		}
	}

	// Validate max results
	if c.MaxResults < 1 {
		return fmt.Errorf("search.max_results must be at least 1")
	}
	if c.MaxResults > 100000 {
		return fmt.Errorf("search.max_results cannot exceed 100000")
	}

	// Validate workers
	if c.Workers < 1 {
		return fmt.Errorf("search.workers must be at least 1")
	}
	if c.Workers > 32 {
		return fmt.Errorf("search.workers cannot exceed 32")
	}

	return nil
}

// GetFlushInterval returns the flush interval as a duration.
func (c *Config) GetFlushInterval() time.Duration {
	if c.FlushInterval == "" {
		return 100 * time.Millisecond
	}
	d, _ := time.ParseDuration(c.FlushInterval)
	return d
}

// GetTimeout returns the timeout as a duration.
func (c *Config) GetTimeout() time.Duration {
	if c.Timeout == "" {
		return 5 * time.Second
	}
	d, _ := time.ParseDuration(c.Timeout)
	return d
}
