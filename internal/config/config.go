package config

import (
	"fmt"
	"os"
	"path/filepath"

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
}

// ServerConfig holds server-related configuration
type ServerConfig struct {
	Hostname       string `koanf:"hostname"`        // mail.example.com
	SMTPPort       int    `koanf:"smtp_port"`       // 25 for MX receiving
	SubmissionPort int    `koanf:"submission_port"` // 587 for client submission
	SMTPSPort      int    `koanf:"smtps_port"`      // 465 for implicit TLS
	IMAPPort       int    `koanf:"imap_port"`       // 143 for STARTTLS
	IMAPSPort      int    `koanf:"imaps_port"`      // 993 for implicit TLS
	DAVPort        int    `koanf:"dav_port"`        // 443 for CalDAV/CardDAV
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

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Hostname:       "localhost",
			SMTPPort:       25,
			SubmissionPort: 587,
			SMTPSPort:      465,
			IMAPPort:       143,
			IMAPSPort:      993,
			DAVPort:        443,
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
	if c.Server.Hostname == "" {
		return fmt.Errorf("server.hostname is required")
	}

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
	}

	if c.TLS.AutoTLS {
		if c.TLS.Email == "" {
			return fmt.Errorf("tls.email is required when auto_tls is enabled")
		}
	} else {
		if c.TLS.CertFile != "" && c.TLS.KeyFile == "" {
			return fmt.Errorf("tls.key_file is required when tls.cert_file is set")
		}
		if c.TLS.KeyFile != "" && c.TLS.CertFile == "" {
			return fmt.Errorf("tls.cert_file is required when tls.key_file is set")
		}
	}

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
