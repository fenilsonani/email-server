package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig_BindAddress(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.BindAddress != "0.0.0.0" {
		t.Errorf("Expected default BindAddress to be '0.0.0.0', got '%s'", cfg.Server.BindAddress)
	}
}

func TestDefaultConfig_AllFields(t *testing.T) {
	cfg := DefaultConfig()

	// Server defaults
	if cfg.Server.Hostname != "localhost" {
		t.Errorf("Expected default Hostname 'localhost', got '%s'", cfg.Server.Hostname)
	}
	if cfg.Server.SMTPPort != 25 {
		t.Errorf("Expected default SMTPPort 25, got %d", cfg.Server.SMTPPort)
	}
	if cfg.Server.SubmissionPort != 587 {
		t.Errorf("Expected default SubmissionPort 587, got %d", cfg.Server.SubmissionPort)
	}
	if cfg.Server.SMTPSPort != 465 {
		t.Errorf("Expected default SMTPSPort 465, got %d", cfg.Server.SMTPSPort)
	}
	if cfg.Server.IMAPPort != 143 {
		t.Errorf("Expected default IMAPPort 143, got %d", cfg.Server.IMAPPort)
	}
	if cfg.Server.IMAPSPort != 993 {
		t.Errorf("Expected default IMAPSPort 993, got %d", cfg.Server.IMAPSPort)
	}
	if cfg.Server.DAVPort != 443 {
		t.Errorf("Expected default DAVPort 443, got %d", cfg.Server.DAVPort)
	}
}

func TestBindAddress_AddressFormatting(t *testing.T) {
	tests := []struct {
		name        string
		bindAddress string
		port        int
		expected    string
	}{
		{
			name:        "all interfaces",
			bindAddress: "0.0.0.0",
			port:        25,
			expected:    "0.0.0.0:25",
		},
		{
			name:        "localhost only",
			bindAddress: "127.0.0.1",
			port:        587,
			expected:    "127.0.0.1:587",
		},
		{
			name:        "specific IP",
			bindAddress: "192.168.1.100",
			port:        993,
			expected:    "192.168.1.100:993",
		},
		{
			name:        "IPv6 localhost",
			bindAddress: "::1",
			port:        143,
			expected:    "::1:143",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr := fmt.Sprintf("%s:%d", tt.bindAddress, tt.port)
			if addr != tt.expected {
				t.Errorf("Expected address '%s', got '%s'", tt.expected, addr)
			}
		})
	}
}

func TestLoadConfig_WithBindAddress(t *testing.T) {
	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configContent := `
server:
  hostname: mail.test.com
  bind_address: 192.168.1.50
  smtp_port: 25
  submission_port: 587
  smtps_port: 465
  imap_port: 143
  imaps_port: 993
  dav_port: 443
  shutdown_timeout: 30s

storage:
  data_dir: /tmp/mailtest
  database_path: /tmp/mailtest/mail.db
  maildir_path: /tmp/mailtest/maildir

domains:
  - name: test.com
    dkim_selector: mail

queue:
  redis_url: redis://localhost:6379/0
  prefix: mail
  max_retries: 15
  retry_max_age: 168h

delivery:
  workers: 4
  connect_timeout: 30s
  command_timeout: 5m
`

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Server.BindAddress != "192.168.1.50" {
		t.Errorf("Expected BindAddress '192.168.1.50', got '%s'", cfg.Server.BindAddress)
	}

	if cfg.Server.Hostname != "mail.test.com" {
		t.Errorf("Expected Hostname 'mail.test.com', got '%s'", cfg.Server.Hostname)
	}
}

func TestLoadConfig_DefaultBindAddress(t *testing.T) {
	// Create a temporary config file without bind_address
	tmpDir, err := os.MkdirTemp("", "config_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configContent := `
server:
  hostname: mail.test.com
  smtp_port: 25
  submission_port: 587
  smtps_port: 465
  imap_port: 143
  imaps_port: 993
  dav_port: 443
  shutdown_timeout: 30s

storage:
  data_dir: /tmp/mailtest
  database_path: /tmp/mailtest/mail.db
  maildir_path: /tmp/mailtest/maildir

domains:
  - name: test.com
    dkim_selector: mail

queue:
  redis_url: redis://localhost:6379/0
  prefix: mail
  max_retries: 15
  retry_max_age: 168h

delivery:
  workers: 4
  connect_timeout: 30s
  command_timeout: 5m
`

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Should use default bind address when not specified
	if cfg.Server.BindAddress != "0.0.0.0" {
		t.Errorf("Expected default BindAddress '0.0.0.0', got '%s'", cfg.Server.BindAddress)
	}
}

func TestLoadConfig_NonExistentFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("Load should not error on nonexistent file: %v", err)
	}

	// Should return defaults
	if cfg.Server.BindAddress != "0.0.0.0" {
		t.Errorf("Expected default BindAddress '0.0.0.0', got '%s'", cfg.Server.BindAddress)
	}
}

func TestServerAddresses(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.BindAddress = "10.0.0.1"

	// Test SMTP address
	smtpAddr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.SMTPPort)
	if smtpAddr != "10.0.0.1:25" {
		t.Errorf("Expected SMTP address '10.0.0.1:25', got '%s'", smtpAddr)
	}

	// Test Submission address
	submissionAddr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.SubmissionPort)
	if submissionAddr != "10.0.0.1:587" {
		t.Errorf("Expected Submission address '10.0.0.1:587', got '%s'", submissionAddr)
	}

	// Test SMTPS address
	smtpsAddr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.SMTPSPort)
	if smtpsAddr != "10.0.0.1:465" {
		t.Errorf("Expected SMTPS address '10.0.0.1:465', got '%s'", smtpsAddr)
	}

	// Test IMAP address
	imapAddr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.IMAPPort)
	if imapAddr != "10.0.0.1:143" {
		t.Errorf("Expected IMAP address '10.0.0.1:143', got '%s'", imapAddr)
	}

	// Test IMAPS address
	imapsAddr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.IMAPSPort)
	if imapsAddr != "10.0.0.1:993" {
		t.Errorf("Expected IMAPS address '10.0.0.1:993', got '%s'", imapsAddr)
	}

	// Test DAV address
	davAddr := fmt.Sprintf("%s:%d", cfg.Server.BindAddress, cfg.Server.DAVPort)
	if davAddr != "10.0.0.1:443" {
		t.Errorf("Expected DAV address '10.0.0.1:443', got '%s'", davAddr)
	}
}

func TestValidatePorts(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.Hostname = "mail.example.com"
	cfg.Domains = []DomainConfig{{Name: "example.com", DKIMSelector: "mail"}}
	cfg.Security.SignOutbound = false // Don't require DKIM key

	// Valid config should pass
	if err := cfg.Validate(); err != nil {
		t.Errorf("Valid config should not error: %v", err)
	}

	// Invalid port should fail
	cfg.Server.SMTPPort = 0
	if err := cfg.Validate(); err == nil {
		t.Error("Config with invalid port should fail validation")
	}

	// Restore and test port conflict
	cfg.Server.SMTPPort = 25
	cfg.Server.SubmissionPort = 25 // Same as SMTP - conflict
	if err := cfg.Validate(); err == nil {
		t.Error("Config with port conflict should fail validation")
	}
}
