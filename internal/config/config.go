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
	Server       ServerConfig       `koanf:"server"`
	TLS          TLSConfig          `koanf:"tls"`
	Storage      StorageConfig      `koanf:"storage"`
	Database     DatabaseConfig     `koanf:"database"`
	Domains      []DomainConfig     `koanf:"domains"`
	Security     SecurityConfig     `koanf:"security"`
	Logging      LoggingConfig      `koanf:"logging"`
	Queue        QueueConfig        `koanf:"queue"`
	Delivery     DeliveryConfig     `koanf:"delivery"`
	Admin        AdminConfig        `koanf:"admin"`
	Sieve        SieveConfig        `koanf:"sieve"`
	Autodiscover AutodiscoverConfig `koanf:"autodiscover"`
	API          APIConfig          `koanf:"api"`
	Search       SearchConfig       `koanf:"search"`
	Updater      UpdaterConfig      `koanf:"updater"`
}

// IMAPConfig holds IMAP-specific configuration
type IMAPConfig struct {
	IdleKeepaliveInterval string `koanf:"idle_keepalive_interval"` // IDLE keepalive interval
	TCPKeepalivePeriod    string `koanf:"tcp_keepalive_period"`     // TCP keepalive period
	MaxConnections        int    `koanf:"max_connections"`          // Maximum concurrent connections
	MaxConnectionsPerIP   int    `koanf:"max_connections_per_ip"`   // Maximum connections per IP
}

// ServerConfig holds server-related configuration
type ServerConfig struct {
	Hostname        string     `koanf:"hostname"`         // mail.example.com
	BindAddress     string     `koanf:"bind_address"`     // Listen address (default 0.0.0.0)
	Domain          string     `koanf:"domain"`           // Primary email domain (e.g., example.com)
	SMTPPort        int        `koanf:"smtp_port"`        // 25 for MX receiving
	SubmissionPort  int        `koanf:"submission_port"`  // 587 for client submission
	SMTPSPort       int        `koanf:"smtps_port"`       // 465 for implicit TLS
	IMAPPort        int        `koanf:"imap_port"`        // 143 for STARTTLS
	IMAPSPort       int        `koanf:"imaps_port"`       // 993 for implicit TLS
	DAVPort         int        `koanf:"dav_port"`         // 443 for CalDAV/CardDAV
	ShutdownTimeout string     `koanf:"shutdown_timeout"` // Graceful shutdown timeout
	IMAP            IMAPConfig `koanf:"imap"`             // IMAP-specific configuration
}

// TLSConfig holds TLS/ACME configuration
type TLSConfig struct {
	AutoTLS  bool   `koanf:"auto_tls"`   // Use Let's Encrypt
	Email    string `koanf:"email"`      // ACME account email
	CertFile string `koanf:"cert_file"`  // Manual cert path
	KeyFile  string `koanf:"key_file"`   // Manual key path
	CacheDir string `koanf:"cache_dir"`  // ACME cache directory

	// Hot-reload and automation settings
	EnableHTTPChallenge  bool     `koanf:"enable_http_challenge"`   // Enable HTTP-01 challenge server for ACME
	HTTPChallengePort    int      `koanf:"http_challenge_port"`     // Port for HTTP-01 challenges (default 80)
	ReloadOnChange       bool     `koanf:"reload_on_change"`        // Auto-reload certificates when file changes
	RenewalCheckInterval string   `koanf:"renewal_check_interval"`  // How often to check for renewal (e.g., "12h")
	RenewalDaysBefore    int      `koanf:"renewal_days_before"`     // Days before expiry to trigger renewal (default 30)
	RenewalHookScript    string   `koanf:"renewal_hook_script"`     // Script to execute after renewal
	Domains              []string `koanf:"domains"`                 // Additional domains for AutoTLS beyond server.hostname
}

// StorageConfig holds storage paths configuration
type StorageConfig struct {
	DataDir      string `koanf:"data_dir"`      // Base data directory
	DatabasePath string `koanf:"database_path"` // SQLite database path (legacy, use database.path)
	MaildirPath  string `koanf:"maildir_path"`  // Maildir storage path
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Driver          string `koanf:"driver"`            // Database driver: sqlite3, postgres
	Path            string `koanf:"path"`              // SQLite database file path
	DSN             string `koanf:"dsn"`               // PostgreSQL connection string
	MaxOpenConns    int    `koanf:"max_open_conns"`    // Maximum open connections
	MaxIdleConns    int    `koanf:"max_idle_conns"`    // Maximum idle connections
	ConnMaxLifetime string `koanf:"conn_max_lifetime"` // Maximum connection lifetime
	ConnMaxIdleTime string `koanf:"conn_max_idle_time"` // Maximum idle time before closing

	// Resilience settings
	QueryTimeout          string `koanf:"query_timeout"`           // Maximum time for a single query (default: 30s)
	SlowQueryThreshold    string `koanf:"slow_query_threshold"`    // Duration above which queries are logged as slow (default: 1s)
	CircuitBreakerEnabled bool   `koanf:"circuit_breaker_enabled"` // Enable circuit breaker protection (default: true)
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

	// ARC configuration (RFC 8617)
	ARCEnabled    bool   `koanf:"arc_enabled"`     // Enable ARC signing for forwarded messages
	ARCSelector   string `koanf:"arc_selector"`    // ARC key selector (default: arc)
	ARCAuthServID string `koanf:"arc_authserv_id"` // Auth service identifier for A-R headers
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `koanf:"level"`  // debug, info, warn, error
	Format string `koanf:"format"` // json, text
	Output string `koanf:"output"` // stdout, stderr, or file path
}

// QueueConfig holds Redis queue configuration
type QueueConfig struct {
	RedisURL       string   `koanf:"redis_url"`       // Redis connection URL (for standalone mode)
	Mode           string   `koanf:"mode"`            // Connection mode: standalone, sentinel, cluster
	SentinelMaster string   `koanf:"sentinel_master"` // Master name for Sentinel mode
	SentinelAddrs  []string `koanf:"sentinel_addrs"`  // Sentinel addresses
	ClusterAddrs   []string `koanf:"cluster_addrs"`   // Cluster node addresses
	Password       string   `koanf:"password"`        // Redis password (optional)
	DB             int      `koanf:"db"`              // Database number (not used in cluster mode)
	Prefix         string   `koanf:"prefix"`          // Key prefix for queue entries
	MaxRetries     int      `koanf:"max_retries"`     // Maximum delivery attempts
	RetryMaxAge    string   `koanf:"retry_max_age"`   // Max time to retry (e.g., "168h")
	PoolSize       int      `koanf:"pool_size"`       // Connection pool size
	MinIdleConns   int      `koanf:"min_idle_conns"`  // Minimum idle connections
	DialTimeout    string   `koanf:"dial_timeout"`    // Connection dial timeout
	ReadTimeout    string   `koanf:"read_timeout"`    // Read timeout
	WriteTimeout   string   `koanf:"write_timeout"`   // Write timeout
}

// DeliveryConfig holds outbound delivery configuration
type DeliveryConfig struct {
	Workers        int    `koanf:"workers"`         // Number of delivery workers
	ConnectTimeout string `koanf:"connect_timeout"` // TCP connection timeout
	CommandTimeout string `koanf:"command_timeout"` // SMTP command timeout
	RequireTLS     bool   `koanf:"require_tls"`     // Require TLS for outbound
	VerifyTLS      bool   `koanf:"verify_tls"`      // Verify TLS certificates
	RelayHost      string `koanf:"relay_host"`      // Optional smarthost (host:port)

	// MTA-STS configuration (RFC 8461)
	MTASTSEnabled   bool   `koanf:"mta_sts_enabled"`    // Enable MTA-STS policy checking
	MTASTSCacheTime string `koanf:"mta_sts_cache_time"` // Override cache TTL (e.g., "24h")

	// DANE/TLSA configuration (RFC 6698, RFC 7672)
	DANEEnabled       bool   `koanf:"dane_enabled"`         // Enable DANE/TLSA checking
	DANERequireDNSSEC bool   `koanf:"dane_require_dnssec"`  // Require DNSSEC validation
	DANECacheTTL      string `koanf:"dane_cache_ttl"`       // TLSA cache TTL (e.g., "5m")
	DANEDNSServer     string `koanf:"dane_dns_server"`      // DNS server for TLSA lookups
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

// AutodiscoverConfig holds autodiscover/autoconfig settings
type AutodiscoverConfig struct {
	Enabled     bool   `koanf:"enabled"`      // Enable autodiscover endpoints
	Port        int    `koanf:"port"`         // Autodiscover port (default 8081)
	Listen      string `koanf:"listen"`       // Listen address (default 0.0.0.0)
	DisplayName string `koanf:"display_name"` // Display name for email service
}

// APIConfig holds transactional email API configuration
type APIConfig struct {
	Enabled                   bool     `koanf:"enabled"`                       // Enable transactional API
	Port                      int      `koanf:"port"`                          // API port (default 8082)
	Listen                    string   `koanf:"listen"`                        // Listen address (default 0.0.0.0)
	TrackingDomain            string   `koanf:"tracking_domain"`               // Domain for open/click tracking
	RateLimitDefault          int      `koanf:"rate_limit_default"`            // Default rate limit per hour
	EnableTracking            bool     `koanf:"enable_tracking"`               // Enable open/click tracking
	BlockedAttachmentTypes    []string `koanf:"blocked_attachment_types"`      // Blocked file extensions (e.g., [".exe", ".bat"])
	MaxAttachmentSizeMB       int      `koanf:"max_attachment_size_mb"`        // Max size per attachment in MB (default 10)
	MaxTotalAttachmentsSizeMB int      `koanf:"max_total_attachments_size_mb"` // Max total attachments size in MB (default 25)
}

// SearchConfig holds full-text search configuration
type SearchConfig struct {
	Enabled          bool   `koanf:"enabled"`           // Enable full-text search
	Engine           string `koanf:"engine"`            // Search engine: bleve, sqlite, postgres, auto
	IndexPath        string `koanf:"index_path"`        // Path for Bleve index
	Realtime         bool   `koanf:"realtime"`          // Enable real-time indexing
	BatchSize        int    `koanf:"batch_size"`        // Batch size for indexing
	FlushInterval    string `koanf:"flush_interval"`    // Flush interval for batches
	Timeout          string `koanf:"timeout"`           // Search timeout
	FuzzyEnabled     bool   `koanf:"fuzzy_enabled"`     // Enable fuzzy matching
	FuzzyDistance    int    `koanf:"fuzzy_distance"`    // Fuzzy edit distance
	HighlightEnabled bool   `koanf:"highlight_enabled"` // Enable result highlighting
	MaxResults       int    `koanf:"max_results"`       // Maximum search results
	Workers          int    `koanf:"workers"`           // Number of indexing workers
}

// UpdaterConfig holds update system configuration
type UpdaterConfig struct {
	Mode                   string `koanf:"mode"`                      // Update mode: normal, power (default "normal")
	AutoCheckEnabled       bool   `koanf:"auto_check_enabled"`        // Enable automatic update checks (default true)
	AutoCheckInterval      int    `koanf:"auto_check_interval"`       // Check interval in seconds (default 3600)
	GitRepoURL             string `koanf:"git_repo_url"`              // GitHub repository URL
	BuildPath              string `koanf:"build_path"`                // Build working directory (default "/tmp/mailserver-build")
	BackupBeforeUpdate     bool   `koanf:"backup_before_update"`      // Create backup before updating (default true)
	MaxBackups             int    `koanf:"max_backups"`               // Maximum number of backups to keep (default 5)
	RequireHealthCheck     bool   `koanf:"require_health_check"`      // Require health check before deploying (default true)
	AutoRollbackOnFailure  bool   `koanf:"auto_rollback_on_failure"`  // Automatically rollback on failure (default true)
	BinaryPath             string `koanf:"binary_path"`               // Path to mailserver binary
	SystemdService         string `koanf:"systemd_service"`           // Systemd service name (default "mailserver.service")
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Hostname:        "localhost",
			BindAddress:     "0.0.0.0",
			Domain:          "localhost",
			SMTPPort:        25,
			SubmissionPort:  587,
			SMTPSPort:       465,
			IMAPPort:        143,
			IMAPSPort:       993,
			DAVPort:         443,
			ShutdownTimeout: "30s",
			IMAP: IMAPConfig{
				IdleKeepaliveInterval: "5m",
				TCPKeepalivePeriod:    "3m",
				MaxConnections:        0,
				MaxConnectionsPerIP:   0,
			},
		},
		TLS: TLSConfig{
			AutoTLS:              false,
			CacheDir:             "/var/lib/mailserver/acme",
			EnableHTTPChallenge:  true,
			HTTPChallengePort:    80,
			ReloadOnChange:       false,
			RenewalCheckInterval: "12h",
			RenewalDaysBefore:    30,
			Domains:              []string{},
		},
		Storage: StorageConfig{
			DataDir:      "/var/lib/mailserver",
			DatabasePath: "/var/lib/mailserver/mail.db",
			MaildirPath:  "/var/lib/mailserver/maildir",
		},
		Database: DatabaseConfig{
			Driver:                "sqlite3",
			Path:                  "/var/lib/mailserver/mail.db",
			MaxOpenConns:          25,
			MaxIdleConns:          5,
			ConnMaxLifetime:       "0",
			ConnMaxIdleTime:       "5m",
			QueryTimeout:          "30s",
			SlowQueryThreshold:    "1s",
			CircuitBreakerEnabled: true,
		},
		Security: SecurityConfig{
			RequireTLS:     true,
			VerifySPF:      true,
			VerifyDKIM:     true,
			VerifyDMARC:    true,
			SignOutbound:   true,
			MaxMessageSize: 26214400, // 25MB
			ARCEnabled:     false,    // Opt-in for ARC
			ARCSelector:    "arc",
			ARCAuthServID:  "",       // Defaults to hostname
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		},
		Queue: QueueConfig{
			RedisURL:     "redis://localhost:6379/0",
			Mode:         "standalone",
			Prefix:       "mail",
			MaxRetries:   15,
			RetryMaxAge:  "168h", // 7 days
			PoolSize:     10,
			MinIdleConns: 5,
			DialTimeout:  "5s",
			ReadTimeout:  "3s",
			WriteTimeout: "3s",
		},
		Delivery: DeliveryConfig{
			Workers:          4,
			ConnectTimeout:   "30s",
			CommandTimeout:   "5m",
			RequireTLS:       false,
			VerifyTLS:        true,
			MTASTSEnabled:    true,       // Enable MTA-STS by default
			MTASTSCacheTime:  "24h",
			DANEEnabled:      true,       // Enable DANE by default
			DANERequireDNSSEC: false,     // Don't require DNSSEC (needs special resolver)
			DANECacheTTL:     "5m",
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
		Autodiscover: AutodiscoverConfig{
			Enabled: true,
			Port:    8081,
			Listen:  "0.0.0.0",
		},
		API: APIConfig{
			Enabled:          false,
			Port:             8082,
			Listen:           "0.0.0.0",
			RateLimitDefault: 1000,
			EnableTracking:   true,
		},
		Search: SearchConfig{
			Enabled:          true,
			Engine:           "auto",
			IndexPath:        "/var/lib/mailserver/search.bleve",
			Realtime:         true,
			BatchSize:        100,
			FlushInterval:    "100ms",
			Timeout:          "5s",
			FuzzyEnabled:     true,
			FuzzyDistance:    2,
			HighlightEnabled: true,
			MaxResults:       1000,
			Workers:          2,
		},
		Updater: UpdaterConfig{
			Mode:                  "normal",
			AutoCheckEnabled:      true,
			AutoCheckInterval:     3600,
			GitRepoURL:            "https://github.com/fenilsonani/email-server",
			BuildPath:             "/tmp/mailserver-build",
			BackupBeforeUpdate:    true,
			MaxBackups:            5,
			RequireHealthCheck:    true,
			AutoRollbackOnFailure: true,
			BinaryPath:            "/usr/local/bin/mailserver",
			SystemdService:        "mailserver.service",
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
	if c.Delivery.Workers > 500 {
		return fmt.Errorf("delivery.workers cannot exceed 500")
	}

	// Database validation
	if err := c.validateDatabase(); err != nil {
		return err
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

	// API validation
	if c.API.Enabled {
		if c.API.Port < 1 || c.API.Port > 65535 {
			return fmt.Errorf("api.port must be between 1 and 65535 (got: %d)", c.API.Port)
		}
		if c.API.Listen == "" {
			return fmt.Errorf("api.listen is required when api is enabled")
		}
		if c.API.RateLimitDefault < 1 {
			return fmt.Errorf("api.rate_limit_default must be at least 1")
		}
		if c.API.RateLimitDefault > 100000 {
			return fmt.Errorf("api.rate_limit_default cannot exceed 100000")
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

// validateDatabase validates database configuration
func (c *Config) validateDatabase() error {
	validDrivers := map[string]bool{"sqlite3": true, "postgres": true}
	if c.Database.Driver != "" && !validDrivers[c.Database.Driver] {
		return fmt.Errorf("database.driver must be one of: sqlite3, postgres (got: %s)", c.Database.Driver)
	}

	// Use legacy storage.database_path if database.path is not set
	if c.Database.Path == "" && c.Storage.DatabasePath != "" {
		c.Database.Path = c.Storage.DatabasePath
	}

	switch c.Database.Driver {
	case "sqlite3", "":
		if c.Database.Path == "" {
			return fmt.Errorf("database.path is required for SQLite")
		}
		if !filepath.IsAbs(c.Database.Path) {
			return fmt.Errorf("database.path must be an absolute path (got: %s)", c.Database.Path)
		}
	case "postgres":
		if c.Database.DSN == "" {
			return fmt.Errorf("database.dsn is required for PostgreSQL")
		}
	}

	// Connection pool validation
	if c.Database.MaxOpenConns < 1 {
		return fmt.Errorf("database.max_open_conns must be at least 1")
	}
	if c.Database.MaxOpenConns > 1000 {
		return fmt.Errorf("database.max_open_conns cannot exceed 1000")
	}
	if c.Database.MaxIdleConns < 0 {
		return fmt.Errorf("database.max_idle_conns cannot be negative")
	}
	if c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		return fmt.Errorf("database.max_idle_conns cannot exceed max_open_conns")
	}

	// Validate lifetime durations
	if c.Database.ConnMaxLifetime != "" && c.Database.ConnMaxLifetime != "0" {
		if _, err := time.ParseDuration(c.Database.ConnMaxLifetime); err != nil {
			return fmt.Errorf("database.conn_max_lifetime is invalid: %w", err)
		}
	}
	if c.Database.ConnMaxIdleTime != "" {
		if _, err := time.ParseDuration(c.Database.ConnMaxIdleTime); err != nil {
			return fmt.Errorf("database.conn_max_idle_time is invalid: %w", err)
		}
	}

	// Validate resilience settings
	if c.Database.QueryTimeout != "" {
		d, err := time.ParseDuration(c.Database.QueryTimeout)
		if err != nil {
			return fmt.Errorf("database.query_timeout is invalid: %w", err)
		}
		if d < time.Second {
			return fmt.Errorf("database.query_timeout must be at least 1s (got: %s)", c.Database.QueryTimeout)
		}
		if d > 5*time.Minute {
			return fmt.Errorf("database.query_timeout cannot exceed 5m (got: %s)", c.Database.QueryTimeout)
		}
	}
	if c.Database.SlowQueryThreshold != "" {
		d, err := time.ParseDuration(c.Database.SlowQueryThreshold)
		if err != nil {
			return fmt.Errorf("database.slow_query_threshold is invalid: %w", err)
		}
		if d < 100*time.Millisecond {
			return fmt.Errorf("database.slow_query_threshold must be at least 100ms (got: %s)", c.Database.SlowQueryThreshold)
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
