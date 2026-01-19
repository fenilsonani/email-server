package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
)

func TestCompareConfigToReality(t *testing.T) {
	cfg := config.DefaultConfig()
	// Use high ports that are unlikely to be in use
	cfg.Server.SMTPPort = 62525
	cfg.Server.SubmissionPort = 62587
	cfg.Server.IMAPPort = 62143
	cfg.Server.SMTPSPort = 62465
	cfg.Server.IMAPSPort = 62993
	cfg.Admin.Enabled = false

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := CompareConfigToReality(ctx, cfg, nil)

	if results == nil {
		t.Fatal("CompareConfigToReality() returned nil")
	}

	if len(results.Comparisons) == 0 {
		t.Error("Should have comparisons")
	}

	// Verify counts add up
	total := results.Matched + results.Mismatched + results.Errors
	if total != len(results.Comparisons) {
		t.Errorf("Count mismatch: %d + %d + %d = %d, but got %d comparisons",
			results.Matched, results.Mismatched, results.Errors, total, len(results.Comparisons))
	}

	// Verify duration is set
	if results.Duration == 0 {
		t.Error("Duration should be set")
	}
}

func TestComparePort(t *testing.T) {
	tests := []struct {
		name       string
		port       int
		wantStatus Status
	}{
		{
			name:       "port not listening",
			port:       62999, // High port unlikely to be in use
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := comparePort("Test", "test.port", tt.port)

			if comp.Status != tt.wantStatus {
				t.Errorf("comparePort() status = %v, want %v", comp.Status, tt.wantStatus)
			}

			if comp.ConfigValue != tt.port {
				t.Errorf("ConfigValue = %v, want %v", comp.ConfigValue, tt.port)
			}

			if comp.ConfigPath != "test.port" {
				t.Errorf("ConfigPath = %v, want test.port", comp.ConfigPath)
			}
		})
	}
}

func TestCompareDomainDNS(t *testing.T) {
	tests := []struct {
		name       string
		domain     string
		wantStatus Status
	}{
		{
			name:       "valid domain",
			domain:     "google.com",
			wantStatus: StatusPass,
		},
		{
			name:       "invalid domain",
			domain:     "nonexistent-domain-xyz-12345.invalid",
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := compareDomainDNS(tt.domain)

			if comp.Status != tt.wantStatus {
				t.Errorf("compareDomainDNS(%s) status = %v, want %v (message: %s)",
					tt.domain, comp.Status, tt.wantStatus, comp.Message)
			}
		})
	}
}

func TestCompareDKIMKey(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dkim_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	validKeyFile := filepath.Join(tmpDir, "dkim.key")
	os.WriteFile(validKeyFile, []byte("test key data"), 0600)

	tests := []struct {
		name            string
		domain          string
		keyFile         string
		selector        string
		wantStatusOneOf []Status // Accept any of these statuses
	}{
		{
			name:            "missing key file",
			domain:          "example.com",
			keyFile:         "/nonexistent/key.pem",
			selector:        "mail",
			wantStatusOneOf: []Status{StatusFail},
		},
		{
			name:            "existing key file",
			domain:          "example.com",
			keyFile:         validKeyFile,
			selector:        "mail",
			wantStatusOneOf: []Status{StatusWarn, StatusPass}, // Key exists, DNS may or may not be found
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := compareDKIMKey(tt.domain, tt.keyFile, tt.selector)

			found := false
			for _, s := range tt.wantStatusOneOf {
				if comp.Status == s {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("compareDKIMKey() status = %v, want one of %v (message: %s)",
					comp.Status, tt.wantStatusOneOf, comp.Message)
			}
		})
	}
}

func TestCompareDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dir_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file (not directory)
	testFile := filepath.Join(tmpDir, "testfile")
	os.WriteFile(testFile, []byte("test"), 0644)

	tests := []struct {
		name       string
		path       string
		wantStatus Status
	}{
		{
			name:       "existing directory",
			path:       tmpDir,
			wantStatus: StatusPass,
		},
		{
			name:       "nonexistent directory",
			path:       "/nonexistent/path",
			wantStatus: StatusFail,
		},
		{
			name:       "path is a file",
			path:       testFile,
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := compareDirectory("Test", "test.path", tt.path)

			if comp.Status != tt.wantStatus {
				t.Errorf("compareDirectory() status = %v, want %v (message: %s)",
					comp.Status, tt.wantStatus, comp.Message)
			}
		})
	}
}

func TestCompareAdminServer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Admin.Enabled = true
	cfg.Admin.Port = 62080 // High port unlikely to be in use
	cfg.Admin.Listen = "127.0.0.1"

	comp := compareAdminServer(cfg)

	// Should fail since server isn't running
	if comp.Status != StatusFail {
		t.Errorf("Expected fail status when admin server not running, got %v", comp.Status)
	}

	if comp.Name != "Admin Server" {
		t.Errorf("Name = %v, want Admin Server", comp.Name)
	}
}

func TestCompareTLSCert(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		wantStatus Status
	}{
		{
			name: "invalid cert path",
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.TLS.CertFile = "/nonexistent/cert.pem"
				cfg.TLS.KeyFile = "/nonexistent/key.pem"
				return cfg
			}(),
			wantStatus: StatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comp := compareTLSCert(tt.cfg)

			if comp.Status != tt.wantStatus {
				t.Errorf("compareTLSCert() status = %v, want %v", comp.Status, tt.wantStatus)
			}
		})
	}
}

func TestComparisonResults(t *testing.T) {
	results := &ComparisonResults{
		Comparisons: []Comparison{
			{Name: "Test1", Status: StatusPass, Matches: true},
			{Name: "Test2", Status: StatusFail, Matches: false},
			{Name: "Test3", Status: StatusWarn, Matches: false},
		},
		Matched:    1,
		Mismatched: 1,
		Errors:     1,
	}

	if len(results.Comparisons) != 3 {
		t.Errorf("Expected 3 comparisons, got %d", len(results.Comparisons))
	}

	total := results.Matched + results.Mismatched + results.Errors
	if total != len(results.Comparisons) {
		t.Error("Count totals don't match comparison count")
	}
}

// Edge case tests

func TestCompareWithEmptyConfig(t *testing.T) {
	cfg := &config.Config{}

	ctx := context.Background()
	results := CompareConfigToReality(ctx, cfg, nil)

	if results == nil {
		t.Fatal("Should return results even with empty config")
	}
}

func TestCompareWithCancelledContext(t *testing.T) {
	cfg := config.DefaultConfig()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	results := CompareConfigToReality(ctx, cfg, nil)

	if results == nil {
		t.Fatal("Should return results even with cancelled context")
	}
}

func TestComparisonStatusValues(t *testing.T) {
	// Test that all comparison functions set valid statuses
	comp1 := comparePort("Test", "test", 62999)
	comp2 := compareDirectory("Test", "test", "/nonexistent")
	comp3 := compareDomainDNS("invalid-domain-xyz.invalid")

	validStatuses := map[Status]bool{
		StatusPass: true,
		StatusFail: true,
		StatusWarn: true,
	}

	comps := []Comparison{comp1, comp2, comp3}
	for _, comp := range comps {
		if !validStatuses[comp.Status] {
			t.Errorf("Invalid status %v for comparison %s", comp.Status, comp.Name)
		}
	}
}

func TestComparisonMessageNotEmpty(t *testing.T) {
	comp := comparePort("Test", "test", 62999)

	if comp.Message == "" {
		t.Error("Comparison message should not be empty")
	}
}

func TestCompareConfigWithIPv6(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Admin.Enabled = true
	cfg.Admin.Listen = "::1" // IPv6 localhost
	cfg.Admin.Port = 62080

	comp := compareAdminServer(cfg)

	// Should not panic and should return valid status
	validStatuses := map[Status]bool{
		StatusPass: true,
		StatusFail: true,
		StatusWarn: true,
	}

	if !validStatuses[comp.Status] {
		t.Errorf("Invalid status: %v", comp.Status)
	}
}
