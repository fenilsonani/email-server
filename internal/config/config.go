package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config holds all configuration for the mail server
type Config struct {
	Server   ServerConfig   `koanf:"server"`
	TLS      TLSConfig      `koanf:"tls"`
	Storage  StorageConfig  `koanf:"storage"`
	Domains  []DomainConfig `koanf:"domains"`
	Security SecurityConfig `koanf:"security"`
	Logging  LoggingConfig  `koanf:"logging"`
	Queue    QueueConfig    `koanf:"queue"`
	Delivery DeliveryConfig `koanf:"delivery"`
	Admin    AdminConfig    `koanf:"admin"`
	Sieve    SieveConfig    `koanf:"sieve"`
}

// ServerConfig holds server-related configuration
type ServerConfig struct {
	Hostname        string `koanf:"hostname"`         // mail.example.com
	Domain          string `koanf:"domain"`           // Primary email domain (e.g., example.com)
	SMTPPort        int    `koanf:"smtp_port"`        // 25 for MX receiving
	SubmissionPort  int    `koanf:"submission_port"`  // 587 for client submission
	SMTPSPort       int    `koanf:"smtps_port"`       // 465 for implicit TLS
	IMAPPort        int    `koanf:"imap_port"`        // 143 for STARTTLS
	IMAPSPort       int    `koanf:"imaps_port"`       // 993 for implicit TLS
	DAVPort         int    `koanf:"dav_port"`         // 443 for CalDAV/CardDAV
	ShutdownTimeout string `koanf:"shutdown_timeout"` // Graceful shutdown timeout
}

// TLSConfig holds TLS/ACME configuration
type TLSConfig struct {
	AutoTLS  bool   `koanf:"auto_tls"`   // Use Let's Encrypt
	Email    string `koanf:"email"`      // ACME account email
	CertFile string `koanf:"cert_file"`  // Manual cert path
	KeyFile  string `koanf:"key_file"`   // Manual key path
	CacheDir string `koanf:"cache_dir"`  // ACME cache directory
}

// StorageConfig holds storage paths configuration
type StorageConfig struct {
	DataDir      string `koanf:"data_dir"`      // Base data directory
	DatabasePath string `koanf:"database_path"` // SQLite database path
	MaildirPath  string `koanf:"maildir_path"`  // Maildir storage path
}

// DomainConfig holds per-domain configuration
type DomainConfig struct {
	Name         string `koanf:"name"`           // example.com
	DKIMSelector string `koanf:"dkim_selector"`  // mail
	DKIMKeyFile  string `koanf:"dkim_key_file"`  // Path to DKIM private key
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	RequireTLS     bool `koanf:"require_tls"`      // Require TLS for connections
	VerifySPF      bool `koanf:"verify_spf"`       // Verify SPF on inbound
	VerifyDKIM     bool `koanf:"verify_dkim"`      // Verify DKIM on inbound
	VerifyDMARC    bool `koanf:"verify_dmarc"`     // Verify DMARC on inbound
	SignOutbound   bool `koanf:"sign_outbound"`    // DKIM sign outbound
	MaxMessageSize int  `koanf:"max_message_size"` // Max message size in bytes
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `koanf:"level"`  // debug, info, warn, error
	Format string `koanf:"format"` // json, text
	Output string `koanf:"output"` // stdout, stderr, or file path
}

// QueueConfig holds Redis queue configuration
type QueueConfig struct {
	RedisURL    string `koanf:"redis_url"`     // Redis connection URL
	Prefix      string `koanf:"prefix"`        // Key prefix for queue entries
	MaxRetries  int    `koanf:"max_retries"`   // Maximum delivery attempts
	RetryMaxAge string `koanf:"retry_max_age"` // Max time to retry (e.g., "168h")
}

// DeliveryConfig holds outbound delivery configuration
type DeliveryConfig struct {
	Workers        int    `koanf:"workers"`         // Number of delivery workers
	ConnectTimeout string `koanf:"connect_timeout"` // TCP connection timeout
	CommandTimeout string `koanf:"command_timeout"` // SMTP command timeout
	RequireTLS     bool   `koanf:"require_tls"`     // Require TLS for outbound
	VerifyTLS      bool   `koanf:"verify_tls"`      // Verify TLS certificates
	RelayHost      string `koanf:"relay_host"`      // Optional smarthost (host:port)
}

// AdminConfig holds admin web panel configuration
type AdminConfig struct {
	Enabled bool   `koanf:"enabled"` // Enable admin web panel
	Port    int    `koanf:"port"`    // Admin port (default 8080)
	Listen  string `koanf:"listen"`  // Listen address (default 127.0.0.1)
}

// SieveConfig holds Sieve filtering configuration
type SieveConfig struct {
	Enabled           bool `koanf:"enabled"`              // Enable Sieve filtering
	MaxScriptSize     int  `koanf:"max_script_size"`      // Maximum script size in bytes
	MaxScriptsPerUser int  `koanf:"max_scripts_per_user"` // Maximum scripts per user
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Hostname:        "localhost",
			Domain:          "localhost",
			SMTPPort:        25,
			SubmissionPort:  587,
			SMTPSPort:       465,
			IMAPPort:        143,
			IMAPSPort:       993,
			DAVPort:         443,
			ShutdownTimeout: "30s",
		},
		TLS: TLSConfig{
			AutoTLS:  false,
			CacheDir: "/var/lib/mailserver/acme",
		},
		Storage: StorageConfig{
			DataDir:      "/var/lib/mailserver",
			DatabasePath: "/var/lib/mailserver/mail.db",
			MaildirPath:  "/var/lib/mailserver/maildir",
		},
		Security: SecurityConfig{
			RequireTLS:     true,
			VerifySPF:      true,
			VerifyDKIM:     true,
			VerifyDMARC:    true,
			SignOutbound:   true,
			MaxMessageSize: 26214400, // 25MB
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		},
		Queue: QueueConfig{
			RedisURL:    "redis://localhost:6379/0",
			Prefix:      "mail",
			MaxRetries:  15,
			RetryMaxAge: "168h", // 7 days
		},
		Delivery: DeliveryConfig{
			Workers:        4,
			ConnectTimeout: "30s",
			CommandTimeout: "5m",
			RequireTLS:     false,
			VerifyTLS:      true,
		},
		Admin: AdminConfig{
			Enabled: true,
			Port:    8080,
			Listen:  "127.0.0.1",
		},
		Sieve: SieveConfig{
			Enabled:           true,
			MaxScriptSize:     32768, // 32KB
			MaxScriptsPerUser: 5,
		},
	}
}

// Load reads configuration from a YAML file
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	// Load defaults first
	cfg := DefaultConfig()

	// Check if config file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil // Return defaults if no config file
	}

	// Load YAML config file
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}

	// Unmarshal into config struct
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Server validation
	if c.Server.Hostname == "" {
		return fmt.Errorf("server.hostname is required")
	}

	// Port validation
	if err := c.validatePorts(); err != nil {
		return err
	}

	// Storage validation
	if err := c.validateStorage(); err != nil {
		return err
	}

	// Timeout validation
	if err := c.validateTimeouts(); err != nil {
		return err
	}

	// Domain validation
	if len(c.Domains) == 0 {
		return fmt.Errorf("at least one domain must be configured")
	}

	for i, domain := range c.Domains {
		if domain.Name == "" {
			return fmt.Errorf("domains[%d].name is required", i)
		}
		if c.Security.SignOutbound && domain.DKIMKeyFile == "" {
			return fmt.Errorf("domains[%d].dkim_key_file is required when sign_outbound is enabled", i)
		}
		if domain.DKIMKeyFile != "" {
			if err := validateFileReadable(domain.DKIMKeyFile); err != nil {
				return fmt.Errorf("domains[%d].dkim_key_file: %w", i, err)
			}
		}
	}

	// TLS validation
	if c.TLS.AutoTLS {
		if c.TLS.Email == "" {
			return fmt.Errorf("tls.email is required when auto_tls is enabled")
		}
		if c.TLS.CacheDir == "" {
			return fmt.Errorf("tls.cache_dir is required when auto_tls is enabled")
		}
	} else {
		if c.TLS.CertFile != "" && c.TLS.KeyFile == "" {
			return fmt.Errorf("tls.key_file is required when tls.cert_file is set")
		}
		if c.TLS.KeyFile != "" && c.TLS.CertFile == "" {
			return fmt.Errorf("tls.cert_file is required when tls.key_file is set")
		}
		if c.TLS.CertFile != "" {
			if err := validateFileReadable(c.TLS.CertFile); err != nil {
				return fmt.Errorf("tls.cert_file: %w", err)
			}
		}
		if c.TLS.KeyFile != "" {
			if err := validateFileReadable(c.TLS.KeyFile); err != nil {
				return fmt.Errorf("tls.key_file: %w", err)
			}
		}
	}

	// Security validation
	if c.Security.MaxMessageSize < 1024 {
		return fmt.Errorf("security.max_message_size must be at least 1024 bytes")
	}
	if c.Security.MaxMessageSize > 100*1024*1024 {
		return fmt.Errorf("security.max_message_size cannot exceed 100MB (104857600 bytes)")
	}

	// Queue validation
	if c.Queue.MaxRetries < 1 {
		return fmt.Errorf("queue.max_retries must be at least 1")
	}
	if c.Queue.MaxRetries > 100 {
		return fmt.Errorf("queue.max_retries cannot exceed 100")
	}
	if c.Queue.RedisURL == "" {
		return fmt.Errorf("queue.redis_url is required")
	}

	// Delivery validation
	if c.Delivery.Workers < 1 {
		return fmt.Errorf("delivery.workers must be at least 1")
	}
	if c.Delivery.Workers > 100 {
		return fmt.Errorf("delivery.workers cannot exceed 100")
	}

	// Logging validation
	if c.Logging.Level != "" {
		validLevels := map[string]bool{
			"debug": true, "info": true, "warn": true, "error": true,
		}
		if !validLevels[c.Logging.Level] {
			return fmt.Errorf("logging.level must be one of: debug, info, warn, error (got: %s)", c.Logging.Level)
		}
	}

	if c.Logging.Format != "" {
		validFormats := map[string]bool{"json": true, "text": true}
		if !validFormats[c.Logging.Format] {
			return fmt.Errorf("logging.format must be one of: json, text (got: %s)", c.Logging.Format)
		}
	}

	// Admin validation
	if c.Admin.Enabled {
		if c.Admin.Port < 1 || c.Admin.Port > 65535 {
			return fmt.Errorf("admin.port must be between 1 and 65535 (got: %d)", c.Admin.Port)
		}
		if c.Admin.Listen == "" {
			return fmt.Errorf("admin.listen is required when admin is enabled")
		}
	}

	// Sieve validation
	if c.Sieve.Enabled {
		if c.Sieve.MaxScriptSize < 1024 {
			return fmt.Errorf("sieve.max_script_size must be at least 1024 bytes")
		}
		if c.Sieve.MaxScriptsPerUser < 1 {
			return fmt.Errorf("sieve.max_scripts_per_user must be at least 1")
		}
	}

	return nil
}

// validatePorts ensures all port configurations are valid
func (c *Config) validatePorts() error {
	ports := map[string]int{
		"server.smtp_port":       c.Server.SMTPPort,
		"server.submission_port": c.Server.SubmissionPort,
		"server.smtps_port":      c.Server.SMTPSPort,
		"server.imap_port":       c.Server.IMAPPort,
		"server.imaps_port":      c.Server.IMAPSPort,
		"server.dav_port":        c.Server.DAVPort,
	}

	for name, port := range ports {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%s must be between 1 and 65535 (got: %d)", name, port)
		}
	}

	// Check for port conflicts
	usedPorts := make(map[int]string)
	for name, port := range ports {
		if existing, ok := usedPorts[port]; ok {
			return fmt.Errorf("port conflict: %s and %s both use port %d", name, existing, port)
		}
		usedPorts[port] = name
	}

	return nil
}

// validateStorage ensures all storage paths are valid
func (c *Config) validateStorage() error {
	if c.Storage.DataDir == "" {
		return fmt.Errorf("storage.data_dir is required")
	}
	if c.Storage.DatabasePath == "" {
		return fmt.Errorf("storage.database_path is required")
	}
	if c.Storage.MaildirPath == "" {
		return fmt.Errorf("storage.maildir_path is required")
	}

	// Validate paths are absolute for safety
	if !filepath.IsAbs(c.Storage.DataDir) {
		return fmt.Errorf("storage.data_dir must be an absolute path (got: %s)", c.Storage.DataDir)
	}
	if !filepath.IsAbs(c.Storage.DatabasePath) {
		return fmt.Errorf("storage.database_path must be an absolute path (got: %s)", c.Storage.DatabasePath)
	}
	if !filepath.IsAbs(c.Storage.MaildirPath) {
		return fmt.Errorf("storage.maildir_path must be an absolute path (got: %s)", c.Storage.MaildirPath)
	}

	return nil
}

// validateTimeouts ensures all timeout configurations are valid
func (c *Config) validateTimeouts() error {
	timeouts := map[string]string{
		"server.shutdown_timeout":  c.Server.ShutdownTimeout,
		"delivery.connect_timeout": c.Delivery.ConnectTimeout,
		"delivery.command_timeout": c.Delivery.CommandTimeout,
		"queue.retry_max_age":      c.Queue.RetryMaxAge,
	}

	for name, timeout := range timeouts {
		if timeout == "" {
			continue // Optional
		}
		duration, err := time.ParseDuration(timeout)
		if err != nil {
			return fmt.Errorf("%s is invalid: %w", name, err)
		}
		if duration < 0 {
			return fmt.Errorf("%s cannot be negative (got: %s)", name, timeout)
		}
		if duration == 0 {
			return fmt.Errorf("%s cannot be zero (got: %s)", name, timeout)
		}

		// Sanity checks for specific timeouts
		switch name {
		case "server.shutdown_timeout":
			if duration > 5*time.Minute {
				return fmt.Errorf("%s is too long, maximum is 5m (got: %s)", name, timeout)
			}
		case "delivery.connect_timeout":
			if duration > 2*time.Minute {
				return fmt.Errorf("%s is too long, maximum is 2m (got: %s)", name, timeout)
			}
		case "delivery.command_timeout":
			if duration > 10*time.Minute {
				return fmt.Errorf("%s is too long, maximum is 10m (got: %s)", name, timeout)
			}
		case "queue.retry_max_age":
			if duration > 30*24*time.Hour {
				return fmt.Errorf("%s is too long, maximum is 30d (got: %s)", name, timeout)
			}
		}
	}

	return nil
}

// validateFileReadable checks if a file exists and is readable
func validateFileReadable(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("must be an absolute path (got: %s)", path)
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", path)
		}
		return fmt.Errorf("cannot access file: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("path is a directory, expected a file: %s", path)
	}

	// Try to open for reading
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("file is not readable: %w", err)
	}
	f.Close()

	return nil
}

// EnsureDirectories creates necessary directories
func (c *Config) EnsureDirectories() error {
	dirs := []string{
		c.Storage.DataDir,
		c.Storage.MaildirPath,
		filepath.Dir(c.Storage.DatabasePath),
	}

	if c.TLS.AutoTLS && c.TLS.CacheDir != "" {
		dirs = append(dirs, c.TLS.CacheDir)
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}

// GetDomain returns the domain configuration for a given domain name
func (c *Config) GetDomain(name string) *DomainConfig {
	for i := range c.Domains {
		if c.Domains[i].Name == name {
			return &c.Domains[i]
		}
	}
	return nil
}

// IsManagedDomain checks if a domain is managed by this server
func (c *Config) IsManagedDomain(name string) bool {
	return c.GetDomain(name) != nil
}
