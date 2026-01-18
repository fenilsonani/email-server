// Package setup provides automatic server initialization.
package setup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AutoSetup handles automatic server initialization.
// Users don't need to do anything - everything is configured automatically.
type AutoSetup struct {
	dataDir   string
	logger    Logger
}

// Logger interface for setup logging.
type Logger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// SetupResult holds the result of auto setup.
type SetupResult struct {
	Success         bool              `json:"success"`
	DirectoriesCreated []string       `json:"directories_created"`
	FilesCreated    []string          `json:"files_created"`
	ConfigGenerated bool              `json:"config_generated"`
	DKIMGenerated   map[string]bool   `json:"dkim_generated"`
	DatabaseSetup   bool              `json:"database_setup"`
	Errors          []string          `json:"errors,omitempty"`
}

// NewAutoSetup creates a new auto setup handler.
func NewAutoSetup(dataDir string, logger Logger) *AutoSetup {
	return &AutoSetup{
		dataDir: dataDir,
		logger:  logger,
	}
}

// Run performs automatic setup. Safe to call on every startup.
func (s *AutoSetup) Run(ctx context.Context) (*SetupResult, error) {
	result := &SetupResult{
		DKIMGenerated: make(map[string]bool),
	}

	// 1. Create required directories
	dirs := s.getRequiredDirectories()
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0750); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to create %s: %v", dir, err))
			continue
		}
		if !dirExists(dir) {
			result.DirectoriesCreated = append(result.DirectoriesCreated, dir)
		}
	}

	// 2. Generate default config if not exists
	configPath := filepath.Join(s.dataDir, "config.yaml")
	if !fileExists(configPath) {
		if err := s.generateDefaultConfig(configPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to generate config: %v", err))
		} else {
			result.ConfigGenerated = true
			result.FilesCreated = append(result.FilesCreated, configPath)
			if s.logger != nil {
				s.logger.Info("Generated default configuration at %s", configPath)
			}
		}
	}

	// 3. Set up database file location marker
	dbMarker := filepath.Join(s.dataDir, ".db_initialized")
	if !fileExists(dbMarker) {
		if err := os.WriteFile(dbMarker, []byte(time.Now().Format(time.RFC3339)), 0640); err == nil {
			result.DatabaseSetup = true
		}
	}

	result.Success = len(result.Errors) == 0
	return result, nil
}

// EnsureDirectories ensures all required directories exist.
func (s *AutoSetup) EnsureDirectories() error {
	dirs := s.getRequiredDirectories()
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

// GetDataDir returns the data directory.
func (s *AutoSetup) GetDataDir() string {
	return s.dataDir
}

// GetMaildirPath returns the maildir path.
func (s *AutoSetup) GetMaildirPath() string {
	return filepath.Join(s.dataDir, "maildir")
}

// GetDatabasePath returns the database path.
func (s *AutoSetup) GetDatabasePath() string {
	return filepath.Join(s.dataDir, "mail.db")
}

// GetDKIMPath returns the DKIM keys path.
func (s *AutoSetup) GetDKIMPath() string {
	return filepath.Join(s.dataDir, "dkim")
}

// GetQueuePath returns the queue path.
func (s *AutoSetup) GetQueuePath() string {
	return filepath.Join(s.dataDir, "queue")
}

// GetBackupPath returns the backup path.
func (s *AutoSetup) GetBackupPath() string {
	return filepath.Join(s.dataDir, "backups")
}

func (s *AutoSetup) getRequiredDirectories() []string {
	return []string{
		s.dataDir,
		s.GetMaildirPath(),
		s.GetDKIMPath(),
		s.GetQueuePath(),
		s.GetBackupPath(),
		filepath.Join(s.dataDir, "acme"),  // TLS certificates
		filepath.Join(s.dataDir, "logs"),  // Log files
		filepath.Join(s.dataDir, "tmp"),   // Temporary files
	}
}

func (s *AutoSetup) generateDefaultConfig(path string) error {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "localhost"
	}

	// Generate a random admin password
	adminPass := generateSecureToken(16)

	config := fmt.Sprintf(`# Auto-generated configuration
# Generated at: %s

server:
  hostname: %s
  bind_address: 0.0.0.0
  smtp_port: 25
  submission_port: 587
  smtps_port: 465
  imap_port: 143
  imaps_port: 993
  dav_port: 443
  shutdown_timeout: "30s"

# Database - auto-configured
database:
  driver: sqlite3
  path: %s
  max_open_conns: 25
  max_idle_conns: 5

# Storage paths - auto-configured
storage:
  data_dir: %s
  maildir_path: %s

# Queue - auto-configured with fallback
queue:
  redis_url: redis://localhost:6379/0
  mode: standalone
  prefix: mail
  max_retries: 15
  retry_max_age: "168h"
  pool_size: 10

# Delivery
delivery:
  workers: 4
  connect_timeout: "30s"
  command_timeout: "5m"

# Security - secure defaults
security:
  require_tls: true
  verify_spf: true
  verify_dkim: true
  verify_dmarc: true
  sign_outbound: true
  max_message_size: 26214400

# Logging
logging:
  level: info
  format: json
  output: stdout

# Admin panel - enabled by default
admin:
  enabled: true
  port: 8080
  listen: 127.0.0.1

# Sieve filtering - enabled by default
sieve:
  enabled: true
  max_script_size: 32768
  max_scripts_per_user: 5

# Autodiscover - enabled by default
autodiscover:
  enabled: true
  port: 8081
  listen: 0.0.0.0

# TLS - auto-configured
tls:
  auto_tls: false
  cache_dir: %s

# Add your domains here:
# domains:
#   - name: example.com
#     dkim_selector: mail
#     dkim_key_file: %s/example.com.key

# NOTE: Default admin credentials (change immediately!)
# Username: admin@%s
# Password: %s
`,
		time.Now().Format(time.RFC3339),
		hostname,
		s.GetDatabasePath(),
		s.dataDir,
		s.GetMaildirPath(),
		filepath.Join(s.dataDir, "acme"),
		s.GetDKIMPath(),
		hostname,
		adminPass,
	)

	return os.WriteFile(path, []byte(config), 0640)
}

// Helper functions

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func generateSecureToken(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based token
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// QuickStart performs a complete quick start setup.
// Returns configuration values that were auto-generated.
func QuickStart(dataDir string) (map[string]string, error) {
	setup := NewAutoSetup(dataDir, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := setup.Run(ctx)
	if err != nil {
		return nil, err
	}

	if !result.Success {
		return nil, fmt.Errorf("setup failed: %v", result.Errors)
	}

	return map[string]string{
		"data_dir":     dataDir,
		"config_path":  filepath.Join(dataDir, "config.yaml"),
		"database":     setup.GetDatabasePath(),
		"maildir":      setup.GetMaildirPath(),
	}, nil
}

// DetectEnvironment auto-detects the best configuration based on the environment.
func DetectEnvironment() map[string]string {
	env := make(map[string]string)

	// Detect if running in container
	if _, err := os.Stat("/.dockerenv"); err == nil {
		env["container"] = "docker"
		env["data_dir"] = "/var/lib/mailserver"
	} else if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		env["container"] = "kubernetes"
		env["data_dir"] = "/var/lib/mailserver"
	} else {
		env["container"] = "none"
		// Use home directory for local development
		home, _ := os.UserHomeDir()
		if home != "" {
			env["data_dir"] = filepath.Join(home, ".mailserver")
		} else {
			env["data_dir"] = "/var/lib/mailserver"
		}
	}

	// Detect Redis availability
	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = "localhost"
	}
	env["redis_url"] = fmt.Sprintf("redis://%s:6379/0", redisHost)

	// Detect PostgreSQL if configured
	pgHost := os.Getenv("POSTGRES_HOST")
	if pgHost != "" {
		pgUser := os.Getenv("POSTGRES_USER")
		if pgUser == "" {
			pgUser = "mailserver"
		}
		pgPass := os.Getenv("POSTGRES_PASSWORD")
		pgDB := os.Getenv("POSTGRES_DB")
		if pgDB == "" {
			pgDB = "mailserver"
		}
		env["database_driver"] = "postgres"
		env["database_dsn"] = fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable",
			pgUser, pgPass, pgHost, pgDB)
	} else {
		env["database_driver"] = "sqlite3"
	}

	return env
}
