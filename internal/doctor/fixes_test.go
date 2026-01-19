package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fenilsonani/email-server/internal/config"
)

func TestFixMaildirCreate(t *testing.T) {
	fix := &FixMaildirCreate{}

	t.Run("ID", func(t *testing.T) {
		if fix.ID() != "maildir-missing" {
			t.Errorf("ID() = %v, want maildir-missing", fix.ID())
		}
	})

	t.Run("Description", func(t *testing.T) {
		if fix.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("CanAutoFix", func(t *testing.T) {
		if !fix.CanAutoFix() {
			t.Error("CanAutoFix() should return true")
		}
	})

	t.Run("DryRun", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Storage.MaildirPath = "/test/maildir"

		msg := fix.DryRun(context.Background(), cfg, nil)
		if msg == "" {
			t.Error("DryRun() should return a message")
		}
	})

	t.Run("Apply", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "fix_test")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		cfg := config.DefaultConfig()
		cfg.Storage.MaildirPath = filepath.Join(tmpDir, "new_maildir")

		err = fix.Apply(context.Background(), cfg, nil)
		if err != nil {
			t.Errorf("Apply() error = %v", err)
		}

		// Verify directory was created
		info, err := os.Stat(cfg.Storage.MaildirPath)
		if err != nil {
			t.Errorf("Directory not created: %v", err)
		}
		if !info.IsDir() {
			t.Error("Created path is not a directory")
		}
	})

	t.Run("Apply with empty path", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Storage.MaildirPath = ""

		err := fix.Apply(context.Background(), cfg, nil)
		if err == nil {
			t.Error("Apply() should fail with empty path")
		}
	})
}

func TestFixMaildirPermissions(t *testing.T) {
	fix := &FixMaildirPermissions{}

	t.Run("ID", func(t *testing.T) {
		if fix.ID() != "maildir-permissions" {
			t.Errorf("ID() = %v, want maildir-permissions", fix.ID())
		}
	})

	t.Run("Description", func(t *testing.T) {
		if fix.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("CanAutoFix", func(t *testing.T) {
		if !fix.CanAutoFix() {
			t.Error("CanAutoFix() should return true")
		}
	})

	t.Run("Apply", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "fix_test")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		// Create directory with wrong permissions
		testDir := filepath.Join(tmpDir, "maildir")
		os.MkdirAll(testDir, 0777)

		cfg := config.DefaultConfig()
		cfg.Storage.MaildirPath = testDir

		err = fix.Apply(context.Background(), cfg, nil)
		if err != nil {
			t.Errorf("Apply() error = %v", err)
		}

		// Verify permissions were changed
		info, _ := os.Stat(testDir)
		if info.Mode().Perm() != 0750 {
			t.Errorf("Permissions = %o, want 0750", info.Mode().Perm())
		}
	})
}

func TestFixRunMigrations(t *testing.T) {
	fix := &FixRunMigrations{}

	t.Run("ID", func(t *testing.T) {
		if fix.ID() != "migrations-pending" {
			t.Errorf("ID() = %v, want migrations-pending", fix.ID())
		}
	})

	t.Run("Description", func(t *testing.T) {
		if fix.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("CanAutoFix", func(t *testing.T) {
		if !fix.CanAutoFix() {
			t.Error("CanAutoFix() should return true")
		}
	})

	t.Run("DryRun", func(t *testing.T) {
		cfg := config.DefaultConfig()
		msg := fix.DryRun(context.Background(), cfg, nil)
		if msg == "" {
			t.Error("DryRun() should return a message")
		}
	})

	t.Run("Apply with invalid database", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Database.Driver = "sqlite3"
		cfg.Database.Path = "/nonexistent/path/db.sqlite"

		err := fix.Apply(context.Background(), cfg, nil)
		if err == nil {
			t.Error("Apply() should fail with invalid database path")
		}
	})
}

func TestFixRecoverStaleQueue(t *testing.T) {
	fix := &FixRecoverStaleQueue{}

	t.Run("ID", func(t *testing.T) {
		if fix.ID() != "queue-stale" {
			t.Errorf("ID() = %v, want queue-stale", fix.ID())
		}
	})

	t.Run("Description", func(t *testing.T) {
		if fix.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("CanAutoFix", func(t *testing.T) {
		if !fix.CanAutoFix() {
			t.Error("CanAutoFix() should return true")
		}
	})

	t.Run("DryRun without queue", func(t *testing.T) {
		cfg := config.DefaultConfig()
		msg := fix.DryRun(context.Background(), cfg, nil)
		if msg != "Queue not available" {
			t.Errorf("DryRun() = %v, want 'Queue not available'", msg)
		}
	})

	t.Run("Apply without queue", func(t *testing.T) {
		cfg := config.DefaultConfig()
		err := fix.Apply(context.Background(), cfg, nil)
		if err == nil {
			t.Error("Apply() should fail without queue")
		}
	})
}

func TestFixGenerateDKIM(t *testing.T) {
	fix := &FixGenerateDKIM{Domain: "example.com"}

	t.Run("ID", func(t *testing.T) {
		expected := "dkim-missing-example.com"
		if fix.ID() != expected {
			t.Errorf("ID() = %v, want %v", fix.ID(), expected)
		}
	})

	t.Run("Description", func(t *testing.T) {
		if fix.Description() == "" {
			t.Error("Description() should not be empty")
		}
	})

	t.Run("CanAutoFix", func(t *testing.T) {
		if fix.CanAutoFix() {
			t.Error("CanAutoFix() should return false (requires confirmation)")
		}
	})

	t.Run("DryRun", func(t *testing.T) {
		cfg := config.DefaultConfig()
		msg := fix.DryRun(context.Background(), cfg, nil)
		if msg == "" {
			t.Error("DryRun() should return a message")
		}
	})

	t.Run("Apply returns instructions", func(t *testing.T) {
		cfg := config.DefaultConfig()
		err := fix.Apply(context.Background(), cfg, nil)
		if err == nil {
			t.Error("Apply() should return error with instructions")
		}
	})
}

func TestFixCertRenewal(t *testing.T) {
	fix := &FixCertRenewal{}

	t.Run("ID", func(t *testing.T) {
		if fix.ID() != "cert-expired" {
			t.Errorf("ID() = %v, want cert-expired", fix.ID())
		}
	})

	t.Run("CanAutoFix", func(t *testing.T) {
		if fix.CanAutoFix() {
			t.Error("CanAutoFix() should return false")
		}
	})

	t.Run("DryRun with AutoTLS", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.TLS.AutoTLS = true

		msg := fix.DryRun(context.Background(), cfg, nil)
		if msg == "" {
			t.Error("DryRun() should return a message")
		}
	})

	t.Run("DryRun without AutoTLS", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.TLS.AutoTLS = false

		msg := fix.DryRun(context.Background(), cfg, nil)
		if msg == "" {
			t.Error("DryRun() should return a message")
		}
	})
}

func TestFixCreateDataDir(t *testing.T) {
	fix := &FixCreateDataDir{}

	t.Run("ID", func(t *testing.T) {
		if fix.ID() != "datadir-missing" {
			t.Errorf("ID() = %v, want datadir-missing", fix.ID())
		}
	})

	t.Run("CanAutoFix", func(t *testing.T) {
		if !fix.CanAutoFix() {
			t.Error("CanAutoFix() should return true")
		}
	})

	t.Run("Apply", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "fix_test")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		cfg := config.DefaultConfig()
		cfg.Storage.DataDir = filepath.Join(tmpDir, "new_data")

		err = fix.Apply(context.Background(), cfg, nil)
		if err != nil {
			t.Errorf("Apply() error = %v", err)
		}

		info, err := os.Stat(cfg.Storage.DataDir)
		if err != nil {
			t.Errorf("Directory not created: %v", err)
		}
		if !info.IsDir() {
			t.Error("Created path is not a directory")
		}
	})
}

func TestFixCleanupQueue(t *testing.T) {
	fix := &FixCleanupQueue{}

	t.Run("ID", func(t *testing.T) {
		if fix.ID() != "queue-cleanup" {
			t.Errorf("ID() = %v, want queue-cleanup", fix.ID())
		}
	})

	t.Run("CanAutoFix", func(t *testing.T) {
		if !fix.CanAutoFix() {
			t.Error("CanAutoFix() should return true")
		}
	})

	t.Run("DryRun without queue", func(t *testing.T) {
		cfg := config.DefaultConfig()
		msg := fix.DryRun(context.Background(), cfg, nil)
		if msg != "Queue not available" {
			t.Errorf("DryRun() = %v, want 'Queue not available'", msg)
		}
	})

	t.Run("Apply without queue", func(t *testing.T) {
		cfg := config.DefaultConfig()
		err := fix.Apply(context.Background(), cfg, nil)
		if err == nil {
			t.Error("Apply() should fail without queue")
		}
	})
}

func TestErrFixNotFound(t *testing.T) {
	if ErrFixNotFound.Error() == "" {
		t.Error("ErrFixNotFound should have a message")
	}
}

// Fix interface tests

func TestAllFixesImplementInterface(t *testing.T) {
	fixes := []Fix{
		&FixMaildirCreate{},
		&FixMaildirPermissions{},
		&FixRunMigrations{},
		&FixRecoverStaleQueue{},
		&FixGenerateDKIM{Domain: "test.com"},
		&FixCertRenewal{},
		&FixCreateDataDir{},
		&FixCleanupQueue{},
	}

	for _, fix := range fixes {
		t.Run(fix.ID(), func(t *testing.T) {
			// Verify all interface methods are implemented
			if fix.ID() == "" {
				t.Error("ID() should not be empty")
			}
			if fix.Description() == "" {
				t.Error("Description() should not be empty")
			}
			// CanAutoFix() returns bool, just call it
			_ = fix.CanAutoFix()

			// DryRun should return something
			cfg := config.DefaultConfig()
			msg := fix.DryRun(context.Background(), cfg, nil)
			if msg == "" {
				t.Error("DryRun() should return a message")
			}
		})
	}
}

// Edge case tests

func TestFixWithCancelledContext(t *testing.T) {
	fix := &FixMaildirCreate{}
	cfg := config.DefaultConfig()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should handle cancelled context gracefully
	msg := fix.DryRun(ctx, cfg, nil)
	if msg == "" {
		t.Error("DryRun should return message even with cancelled context")
	}
}

func TestFixApplyIdempotency(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fix_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	fix := &FixMaildirCreate{}
	cfg := config.DefaultConfig()
	cfg.Storage.MaildirPath = filepath.Join(tmpDir, "maildir")

	ctx := context.Background()

	// Apply twice should not fail
	err = fix.Apply(ctx, cfg, nil)
	if err != nil {
		t.Errorf("First Apply() failed: %v", err)
	}

	err = fix.Apply(ctx, cfg, nil)
	if err != nil {
		t.Errorf("Second Apply() failed: %v", err)
	}
}

func TestFixPermissionsOnExistingDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fix_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testDir := filepath.Join(tmpDir, "maildir")
	os.MkdirAll(testDir, 0755)

	fix := &FixMaildirPermissions{}
	cfg := config.DefaultConfig()
	cfg.Storage.MaildirPath = testDir

	err = fix.Apply(context.Background(), cfg, nil)
	if err != nil {
		t.Errorf("Apply() error = %v", err)
	}

	info, _ := os.Stat(testDir)
	if info.Mode().Perm() != 0750 {
		t.Errorf("Permissions = %o, want 0750", info.Mode().Perm())
	}
}
