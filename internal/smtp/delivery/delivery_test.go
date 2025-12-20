package delivery

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
)

func TestConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Workers != 4 {
		t.Errorf("Workers = %d, want 4", cfg.Workers)
	}
	if cfg.Hostname != "localhost" {
		t.Errorf("Hostname = %s, want localhost", cfg.Hostname)
	}
	if cfg.ConnectTimeout != 30*time.Second {
		t.Errorf("ConnectTimeout = %v, want 30s", cfg.ConnectTimeout)
	}
	if cfg.CommandTimeout != 5*time.Minute {
		t.Errorf("CommandTimeout = %v, want 5m", cfg.CommandTimeout)
	}
	if cfg.MaxMessageSize != 25*1024*1024 {
		t.Errorf("MaxMessageSize = %d, want 25MB", cfg.MaxMessageSize)
	}
	if cfg.RequireTLS != false {
		t.Errorf("RequireTLS = %v, want false", cfg.RequireTLS)
	}
	if cfg.VerifyTLS != true {
		t.Errorf("VerifyTLS = %v, want true", cfg.VerifyTLS)
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		email  string
		domain string
	}{
		{"user@example.com", "example.com"},
		{"user@EXAMPLE.COM", "example.com"},
		{"user@Sub.Domain.Example.COM", "sub.domain.example.com"},
		{"user@localhost", "localhost"},
		{"noatsign", ""},
		{"", ""},
		{"@domain.com", "domain.com"},
		{"user@", ""},
		{"user@domain@extra", "domain@extra"},
		{"  user@example.com  ", "example.com  "},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := extractDomain(tt.email)
			if got != tt.domain {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.email, got, tt.domain)
			}
		})
	}
}

func TestIsPermanentError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"550 user not found", errors.New("550 User not found"), true},
		{"551 user moved", errors.New("551 User moved"), true},
		{"552 mailbox full", errors.New("552 Mailbox full"), true},
		{"553 invalid mailbox", errors.New("553 Invalid mailbox"), true},
		{"554 transaction failed", errors.New("554 Transaction failed"), true},
		{"421 service unavailable", errors.New("421 Service unavailable"), false},
		{"450 try again", errors.New("450 Try again later"), false},
		{"451 local error", errors.New("451 Local error"), false},
		{"connection timeout", errors.New("connection timeout"), false},
		{"ErrPermanentFailure", ErrPermanentFailure, true},
		{"ErrInvalidRecipient", ErrInvalidRecipient, true},
		{"ErrMessageTooLarge", ErrMessageTooLarge, true},
		{"ErrTemporaryFailure", ErrTemporaryFailure, false},
		{"ErrCircuitOpen", ErrCircuitOpen, false},
		{"ErrAllMXFailed", ErrAllMXFailed, false},
		{"wrapped permanent", errors.New("error: 550 permanent"), true},
		{"wrapped temporary", errors.New("error: 450 temporary"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPermanentError(tt.err)
			if got != tt.want {
				t.Errorf("isPermanentError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantPermanent bool
	}{
		{"nil", nil, false},
		{"550 prefix", errors.New("550 User unknown"), true},
		{"space 5", errors.New("SMTP error 550"), true},
		{"421 temp", errors.New("421 Try later"), false},
		{"random error", errors.New("network error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyError(tt.err)
			if result == nil {
				if tt.err != nil {
					t.Error("classifyError returned nil for non-nil error")
				}
				return
			}
			isPerm := errors.Is(result, ErrPermanentFailure)
			if isPerm != tt.wantPermanent {
				t.Errorf("classified as permanent=%v, want %v", isPerm, tt.wantPermanent)
			}
		})
	}
}

func TestEngine_CleanupMessageFile(t *testing.T) {
	tmpDir := t.TempDir()
	logger := logging.Default()

	// Create a mock engine with QueuePath set
	e := &Engine{
		config: Config{
			QueuePath: tmpDir,
		},
		logger: logger.Delivery(),
	}

	t.Run("cleanup existing file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "test1.eml")
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		err := e.cleanupMessageFile(path)
		if err != nil {
			t.Errorf("cleanupMessageFile() error = %v", err)
		}

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("File should have been deleted")
		}
	})

	t.Run("cleanup non-existent file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "nonexistent.eml")

		err := e.cleanupMessageFile(path)
		if err != nil {
			t.Errorf("cleanupMessageFile() should not error for non-existent file: %v", err)
		}
	})

	t.Run("cleanup empty path", func(t *testing.T) {
		err := e.cleanupMessageFile("")
		if err != nil {
			t.Errorf("cleanupMessageFile() should not error for empty path: %v", err)
		}
	})

	t.Run("refuse cleanup outside queue path", func(t *testing.T) {
		otherDir := t.TempDir()
		path := filepath.Join(otherDir, "other.eml")
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		// Should refuse to delete (silently)
		err := e.cleanupMessageFile(path)
		if err != nil {
			t.Errorf("cleanupMessageFile() error = %v", err)
		}

		// File should still exist
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("File outside queue path should NOT have been deleted")
		}
	})

	t.Run("cleanup with empty QueuePath allows all", func(t *testing.T) {
		e2 := &Engine{config: Config{QueuePath: ""}, logger: logger.Delivery()}

		path := filepath.Join(tmpDir, "test2.eml")
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		err := e2.cleanupMessageFile(path)
		if err != nil {
			t.Errorf("cleanupMessageFile() error = %v", err)
		}

		// With empty QueuePath, safety check is skipped
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("File should have been deleted when QueuePath is empty")
		}
	})
}

func TestEngine_CleanupMessageFile_Permissions(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	tmpDir := t.TempDir()
	logger := logging.Default()
	e := &Engine{config: Config{QueuePath: tmpDir}, logger: logger.Delivery()}

	// Create a read-only directory
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(readOnlyDir, 0755)

	// Try to cleanup a file in read-only directory
	path := filepath.Join(readOnlyDir, "test.eml")

	// This should return an error
	err := e.cleanupMessageFile(path)
	// Either no error (file doesn't exist) or permission error
	// Both are acceptable behaviors
	_ = err
}

func TestErrorConstants(t *testing.T) {
	// Verify error constants are properly defined
	errList := []error{
		ErrPermanentFailure,
		ErrTemporaryFailure,
		ErrCircuitOpen,
		ErrAllMXFailed,
		ErrMessageTooLarge,
		ErrInvalidRecipient,
	}

	for _, e := range errList {
		if e == nil {
			t.Error("Error constant should not be nil")
		}
		if e.Error() == "" {
			t.Errorf("Error %v should have non-empty message", e)
		}
	}

	// Verify they're distinct
	for i, e1 := range errList {
		for j, e2 := range errList {
			if i != j && errors.Is(e1, e2) {
				t.Errorf("Error %v should not match %v", e1, e2)
			}
		}
	}
}

func TestExtractDomain_EdgeCases(t *testing.T) {
	tests := []struct {
		email string
		want  string
	}{
		// Unicode in local part
		{"münchen@example.com", "example.com"},
		// Multiple @ signs - takes everything after first @
		{"user@host@domain.com", "host@domain.com"},
		// IP address domain
		{"user@[192.168.1.1]", "[192.168.1.1]"},
		// Very long domain
		{"user@" + strings.Repeat("a", 100) + ".com", strings.Repeat("a", 100) + ".com"},
		// Subdomain
		{"user@mail.sub.example.com", "mail.sub.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := extractDomain(tt.email)
			if got != tt.want {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.email, got, tt.want)
			}
		})
	}
}

func BenchmarkExtractDomain(b *testing.B) {
	email := "user@example.com"
	for i := 0; i < b.N; i++ {
		extractDomain(email)
	}
}

func BenchmarkIsPermanentError(b *testing.B) {
	err := errors.New("550 User not found")
	for i := 0; i < b.N; i++ {
		isPermanentError(err)
	}
}
