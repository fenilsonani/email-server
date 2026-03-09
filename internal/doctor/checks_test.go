package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
)

func TestCheckDatabaseDriver(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		wantStatus Status
	}{
		{
			name: "sqlite with invalid path",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Database.Driver = "sqlite3"
				cfg.Database.Path = "/nonexistent/path/db.sqlite"
				return cfg
			}(),
			wantStatus: StatusFail,
		},
		{
			name: "postgres with invalid DSN",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Database.Driver = "postgres"
				cfg.Database.DSN = "postgres://invalid:invalid@localhost:5432/nonexistent"
				return cfg
			}(),
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			result := checkDatabaseDriver(ctx, tt.cfg, nil)

			if result.Status != tt.wantStatus {
				t.Errorf("checkDatabaseDriver() status = %v, want %v (message: %s)",
					result.Status, tt.wantStatus, result.Message)
			}

			// Verify result has required fields
			if result.ID == "" {
				t.Error("Result ID should not be empty")
			}
			if result.Name == "" {
				t.Error("Result Name should not be empty")
			}
			if result.Category != CategoryInfra {
				t.Errorf("Category = %v, want %v", result.Category, CategoryInfra)
			}
		})
	}
}

func TestCheckDiskSpace(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Storage.MaildirPath = "/"
	cfg.Storage.DataDir = "/"

	ctx := context.Background()
	result := checkDiskSpace(ctx, cfg, nil)

	// Should at least not panic and return a valid result
	if result.ID != "disk-space" {
		t.Errorf("ID = %v, want disk-space", result.ID)
	}

	if result.Category != CategoryInfra {
		t.Errorf("Category = %v, want %v", result.Category, CategoryInfra)
	}

	// Status should be one of the valid values
	validStatuses := map[Status]bool{StatusPass: true, StatusFail: true, StatusWarn: true}
	if !validStatuses[result.Status] {
		t.Errorf("Invalid status: %v", result.Status)
	}
}

func TestCheckMaildirPermissions(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "maildir_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name       string
		setupDir   func() string
		wantStatus Status
	}{
		{
			name: "restrictive directory (0700)",
			setupDir: func() string {
				dir := filepath.Join(tmpDir, "restrictive")
				os.MkdirAll(dir, 0700)
				return dir
			},
			wantStatus: StatusPass,
		},
		{
			name: "nonexistent directory",
			setupDir: func() string {
				return filepath.Join(tmpDir, "nonexistent")
			},
			wantStatus: StatusFail,
		},
		{
			name: "world-accessible permissions",
			setupDir: func() string {
				dir := filepath.Join(tmpDir, "permissive")
				os.MkdirAll(dir, 0777)
				return dir
			},
			wantStatus: StatusFail, // World-accessible is a security risk
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Storage.MaildirPath = tt.setupDir()

			ctx := context.Background()
			result := checkMaildirPermissions(ctx, cfg, nil)

			if result.Status != tt.wantStatus {
				t.Errorf("checkMaildirPermissions() status = %v, want %v (message: %s)",
					result.Status, tt.wantStatus, result.Message)
			}
		})
	}
}

func TestCheckMemoryUsage(t *testing.T) {
	cfg := config.DefaultConfig()
	ctx := context.Background()

	result := checkMemoryUsage(ctx, cfg, nil)

	if result.ID != "memory-usage" {
		t.Errorf("ID = %v, want memory-usage", result.ID)
	}

	// Should have details
	if result.Details == nil {
		t.Error("Details should not be nil")
	}

	// Check for required detail fields
	requiredFields := []string{"heap_mb", "sys_mb", "goroutines"}
	for _, field := range requiredFields {
		if _, ok := result.Details[field]; !ok {
			t.Errorf("Missing required detail field: %s", field)
		}
	}
}

func TestCheckServiceRunning(t *testing.T) {
	cfg := config.DefaultConfig()
	// Use high ports that are unlikely to be in use
	cfg.Server.SMTPPort = 62525
	cfg.Server.SubmissionPort = 62587
	cfg.Server.IMAPPort = 62143

	ctx := context.Background()
	result := checkServiceRunning(ctx, cfg, nil)

	// With no server running, should fail
	if result.Status != StatusFail {
		t.Errorf("Expected fail status when no services running, got %v", result.Status)
	}

	if result.Category != CategoryNetwork {
		t.Errorf("Category = %v, want %v", result.Category, CategoryNetwork)
	}
}

func TestCheckHealthEndpoint(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		wantStatus Status
	}{
		{
			name: "admin disabled",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Admin.Enabled = false
				return cfg
			}(),
			wantStatus: StatusWarn,
		},
		{
			name: "admin enabled but not running",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Admin.Enabled = true
				cfg.Admin.Port = 62080 // High port unlikely to be in use
				cfg.Admin.Listen = "127.0.0.1"
				return cfg
			}(),
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			result := checkHealthEndpoint(ctx, tt.cfg, nil)

			if result.Status != tt.wantStatus {
				t.Errorf("checkHealthEndpoint() status = %v, want %v (message: %s)",
					result.Status, tt.wantStatus, result.Message)
			}
		})
	}
}

func TestCheckRedisConnection(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Queue.RedisURL = "redis://localhost:6379/0"

	ctx := context.Background()
	result := checkRedisConnection(ctx, cfg, nil)

	// Without a queue, it tries to dial directly
	if result.ID != "redis" {
		t.Errorf("ID = %v, want redis", result.ID)
	}

	if result.Category != CategoryNetwork {
		t.Errorf("Category = %v, want %v", result.Category, CategoryNetwork)
	}
}

func TestCheckOutboundSMTP(t *testing.T) {
	cfg := config.DefaultConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := checkOutboundSMTP(ctx, cfg, nil)

	if result.ID != "outbound-smtp" {
		t.Errorf("ID = %v, want outbound-smtp", result.ID)
	}

	// Status depends on network conditions
	validStatuses := map[Status]bool{StatusPass: true, StatusWarn: true}
	if !validStatuses[result.Status] {
		t.Logf("Outbound SMTP check status: %v (message: %s)", result.Status, result.Message)
	}
}

func TestCheckTLSCertificates(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		wantStatus Status
	}{
		{
			name: "no TLS configured",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.TLS.CertFile = ""
				cfg.TLS.KeyFile = ""
				cfg.TLS.AutoTLS = false
				return cfg
			}(),
			wantStatus: StatusFail,
		},
		{
			name: "auto TLS enabled",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.TLS.CertFile = ""
				cfg.TLS.KeyFile = ""
				cfg.TLS.AutoTLS = true
				return cfg
			}(),
			wantStatus: StatusPass,
		},
		{
			name: "invalid cert path",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.TLS.CertFile = "/nonexistent/cert.pem"
				cfg.TLS.KeyFile = "/nonexistent/key.pem"
				cfg.TLS.AutoTLS = false
				return cfg
			}(),
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result := checkTLSCertificates(ctx, tt.cfg, nil)

			if result.Status != tt.wantStatus {
				t.Errorf("checkTLSCertificates() status = %v, want %v (message: %s)",
					result.Status, tt.wantStatus, result.Message)
			}
		})
	}
}

func TestCheckCertExpiry(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		wantStatus Status
	}{
		{
			name: "no cert configured with auto TLS",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.TLS.CertFile = ""
				cfg.TLS.AutoTLS = true
				return cfg
			}(),
			wantStatus: StatusPass,
		},
		{
			name: "no cert configured without auto TLS",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.TLS.CertFile = ""
				cfg.TLS.AutoTLS = false
				return cfg
			}(),
			wantStatus: StatusWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result := checkCertExpiry(ctx, tt.cfg, nil)

			if result.Status != tt.wantStatus {
				t.Errorf("checkCertExpiry() status = %v, want %v (message: %s)",
					result.Status, tt.wantStatus, result.Message)
			}
		})
	}
}

func TestCheckAllDomainsDKIM(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dkim_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name       string
		cfg        *config.Config
		wantStatus Status
	}{
		{
			name: "no domains configured",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Domains = nil
				return cfg
			}(),
			wantStatus: StatusWarn,
		},
		{
			name: "domain with missing DKIM file",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Domains = []config.DomainConfig{
					{Name: "example.com", DKIMKeyFile: "/nonexistent/dkim.key"},
				}
				return cfg
			}(),
			wantStatus: StatusWarn,
		},
		{
			name: "domain with valid DKIM file",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				keyFile := filepath.Join(tmpDir, "dkim.key")
				os.WriteFile(keyFile, []byte("test key"), 0600)
				cfg.Domains = []config.DomainConfig{
					{Name: "example.com", DKIMKeyFile: keyFile},
				}
				return cfg
			}(),
			wantStatus: StatusPass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result := checkAllDomainsDKIM(ctx, tt.cfg, nil)

			if result.Status != tt.wantStatus {
				t.Errorf("checkAllDomainsDKIM() status = %v, want %v (message: %s)",
					result.Status, tt.wantStatus, result.Message)
			}
		})
	}
}

func TestCheckAllDomainsDNS(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		wantStatus Status
	}{
		{
			name: "no domains configured",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Domains = nil
				return cfg
			}(),
			wantStatus: StatusWarn,
		},
		{
			name: "domain with valid DNS (google.com)",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Domains = []config.DomainConfig{
					{Name: "google.com"},
				}
				return cfg
			}(),
			wantStatus: StatusPass,
		},
		{
			name: "domain with no DNS records",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.Domains = []config.DomainConfig{
					{Name: "nonexistent-domain-12345.example"},
				}
				return cfg
			}(),
			wantStatus: StatusWarn,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			result := checkAllDomainsDNS(ctx, tt.cfg, nil)

			if result.Status != tt.wantStatus {
				t.Errorf("checkAllDomainsDNS() status = %v, want %v (message: %s)",
					result.Status, tt.wantStatus, result.Message)
			}
		})
	}
}

func TestCheckDatabasePermissions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dbperm_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name       string
		setup      func() string
		wantStatus Status
	}{
		{
			name: "no database path configured",
			setup: func() string {
				return ""
			},
			wantStatus: StatusWarn,
		},
		{
			name: "correct permissions (0640)",
			setup: func() string {
				dbPath := filepath.Join(tmpDir, "good.db")
				os.WriteFile(dbPath, []byte("test"), 0640)
				return dbPath
			},
			wantStatus: StatusPass,
		},
		{
			name: "world-readable (0644)",
			setup: func() string {
				dbPath := filepath.Join(tmpDir, "worldread.db")
				os.WriteFile(dbPath, []byte("test"), 0600)
				os.Chmod(dbPath, 0644) // Force past umask
				return dbPath
			},
			wantStatus: StatusFail,
		},
		{
			name: "world-writable (0666)",
			setup: func() string {
				dbPath := filepath.Join(tmpDir, "worldwrite.db")
				os.WriteFile(dbPath, []byte("test"), 0600)
				os.Chmod(dbPath, 0666) // Force past umask
				return dbPath
			},
			wantStatus: StatusFail,
		},
		{
			name: "group-writable (0660)",
			setup: func() string {
				dbPath := filepath.Join(tmpDir, "groupwrite.db")
				os.WriteFile(dbPath, []byte("test"), 0600)
				os.Chmod(dbPath, 0660) // Force past umask
				return dbPath
			},
			wantStatus: StatusWarn,
		},
		{
			name: "non-existent db file",
			setup: func() string {
				return filepath.Join(tmpDir, "nonexistent.db")
			},
			wantStatus: StatusPass, // Non-existent files are skipped
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Storage.DatabasePath = tt.setup()

			ctx := context.Background()
			result := checkDatabasePermissions(ctx, cfg, nil)

			if result.Status != tt.wantStatus {
				t.Errorf("checkDatabasePermissions() status = %v, want %v (message: %s)",
					result.Status, tt.wantStatus, result.Message)
			}

			if result.ID != "db-permissions" {
				t.Errorf("ID = %v, want db-permissions", result.ID)
			}
			if result.Category != CategorySecurity {
				t.Errorf("Category = %v, want %v", result.Category, CategorySecurity)
			}
		})
	}
}

func TestCheckDatabasePermissions_WithWALFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dbperm_wal_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	t.Run("db ok but WAL world-readable", func(t *testing.T) {
		dbPath := filepath.Join(tmpDir, "wal_test.db")
		os.WriteFile(dbPath, []byte("test"), 0640)
		os.WriteFile(dbPath+"-wal", []byte("wal"), 0600)
		os.Chmod(dbPath+"-wal", 0644) // Force past umask

		cfg := config.DefaultConfig()
		cfg.Storage.DatabasePath = dbPath

		result := checkDatabasePermissions(context.Background(), cfg, nil)
		if result.Status != StatusFail {
			t.Errorf("Expected fail when WAL is world-readable, got %v", result.Status)
		}
		if result.FixID != "db-permissions" {
			t.Errorf("FixID = %v, want db-permissions", result.FixID)
		}
	})

	t.Run("all files correct", func(t *testing.T) {
		dbPath := filepath.Join(tmpDir, "all_ok.db")
		os.WriteFile(dbPath, []byte("test"), 0640)
		os.WriteFile(dbPath+"-wal", []byte("wal"), 0640)
		os.WriteFile(dbPath+"-shm", []byte("shm"), 0640)

		cfg := config.DefaultConfig()
		cfg.Storage.DatabasePath = dbPath

		result := checkDatabasePermissions(context.Background(), cfg, nil)
		if result.Status != StatusPass {
			t.Errorf("Expected pass when all files are 0640, got %v (message: %s)", result.Status, result.Message)
		}
	})
}

func TestCheckConfigPorts(t *testing.T) {
	cfg := config.DefaultConfig()
	// Use high ports that are unlikely to be in use
	cfg.Server.SMTPPort = 62525
	cfg.Server.SubmissionPort = 62587
	cfg.Server.IMAPPort = 62143

	ctx := context.Background()
	result := checkConfigPorts(ctx, cfg, nil)

	// Should warn since ports aren't listening
	if result.Status != StatusWarn {
		t.Errorf("Expected warn status, got %v (message: %s)", result.Status, result.Message)
	}

	if result.Category != CategoryConfig {
		t.Errorf("Category = %v, want %v", result.Category, CategoryConfig)
	}
}

func TestCheckQueueHealth(t *testing.T) {
	cfg := config.DefaultConfig()

	ctx := context.Background()
	result := checkQueueHealth(ctx, cfg, nil)

	// Without a queue, should warn
	if result.Status != StatusWarn {
		t.Errorf("Expected warn status without queue, got %v", result.Status)
	}

	if result.Category != CategoryQueue {
		t.Errorf("Category = %v, want %v", result.Category, CategoryQueue)
	}
}

func TestCheckQueueStale(t *testing.T) {
	cfg := config.DefaultConfig()

	ctx := context.Background()
	result := checkQueueStale(ctx, cfg, nil)

	// Without a queue, should warn
	if result.Status != StatusWarn {
		t.Errorf("Expected warn status without queue, got %v", result.Status)
	}

	if result.Category != CategoryQueue {
		t.Errorf("Category = %v, want %v", result.Category, CategoryQueue)
	}
}

func TestCheckDomainDNSRecords(t *testing.T) {
	tests := []struct {
		name       string
		domain     string
		wantIssues bool
	}{
		{
			name:       "valid domain with DNS",
			domain:     "google.com",
			wantIssues: false,
		},
		{
			name:       "invalid domain",
			domain:     "nonexistent-domain-xyz-12345.invalid",
			wantIssues: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := checkDomainDNSRecords(tt.domain)
			hasIssues := len(issues) > 0

			if hasIssues != tt.wantIssues {
				t.Errorf("checkDomainDNSRecords(%s) hasIssues = %v, want %v (issues: %v)",
					tt.domain, hasIssues, tt.wantIssues, issues)
			}
		})
	}
}

func TestParsePEMBlock(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
	}{
		{
			name:    "empty input",
			input:   "",
			wantNil: true,
		},
		{
			name:    "no PEM block",
			input:   "not a pem block",
			wantNil: true,
		},
		{
			name:    "incomplete PEM block",
			input:   "-----BEGIN CERTIFICATE-----\ndata",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block, _ := parsePEMBlock([]byte(tt.input))

			if tt.wantNil && block != nil {
				t.Error("Expected nil block")
			}
			if !tt.wantNil && block == nil {
				t.Error("Expected non-nil block")
			}
		})
	}
}

// Edge case tests

func TestCheckWithNilConfig(t *testing.T) {
	// This should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Check panicked with nil config: %v", r)
		}
	}()

	ctx := context.Background()

	// Test checkMemoryUsage which doesn't heavily depend on config
	result := checkMemoryUsage(ctx, &config.Config{}, nil)
	if result.ID == "" {
		t.Error("Should return valid result even with empty config")
	}
}

func TestCheckWithCancelledContext(t *testing.T) {
	cfg := config.DefaultConfig()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Checks should handle cancelled context gracefully
	result := checkMemoryUsage(ctx, cfg, nil)

	// Should still return a valid result
	if result.ID == "" {
		t.Error("Should return valid result even with cancelled context")
	}
}

func TestBase64Decode(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty string",
			input: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := make([]byte, 100)
			n := base64Decode(dst, []byte(tt.input))
			// Just verify it doesn't panic
			_ = n
		})
	}
}
