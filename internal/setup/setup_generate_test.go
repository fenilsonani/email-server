package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/config"
)

// TestGenerateConfig_RoundtripsThroughConfigLoad guards against the class of
// bugs reported in issue #46: the wizard wrote YAML that the runtime config
// loader either rejected outright (`domains[0]` schema mismatch) or silently
// dropped (TLS keys under wrong names, bogus top-level `dkim:` block). Any
// regression where setup emits keys that don't match the koanf-tagged Config
// struct will now fail this test.
func TestGenerateConfig_RoundtripsThroughConfigLoad(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "etc")
	dataDir := filepath.Join(tmp, "var")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir configDir: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir dataDir: %v", err)
	}

	cfg := &SetupConfig{
		Domain:     "example.com",
		Hostname:   "mail.example.com",
		AdminEmail: "admin@example.com",
		AdminPass:  "irrelevant",
		TLSEmail:   "tls@example.com",
		DataDir:    dataDir,
		ConfigDir:  configDir,
	}

	if err := generateConfig(cfg); err != nil {
		t.Fatalf("generateConfig: %v", err)
	}

	loaded, err := config.Load(filepath.Join(configDir, "config.yaml"))
	if err != nil {
		t.Fatalf("config.Load failed (the wizard emitted a YAML the runtime can't parse): %v", err)
	}

	// Bug 2: domains were written as strings instead of []DomainConfig.
	if got := len(loaded.Domains); got != 1 {
		t.Fatalf("Domains = %d entries, want 1", got)
	}
	d := loaded.Domains[0]
	if d.Name != "example.com" {
		t.Errorf("Domains[0].Name = %q, want %q", d.Name, "example.com")
	}
	if d.DKIMSelector != "mail" {
		t.Errorf("Domains[0].DKIMSelector = %q, want %q", d.DKIMSelector, "mail")
	}
	wantKey := filepath.Join(configDir, "dkim", "example.com.key")
	if d.DKIMKeyFile != wantKey {
		t.Errorf("Domains[0].DKIMKeyFile = %q, want %q", d.DKIMKeyFile, wantKey)
	}

	// Bug 3: TLS keys were `auto_cert`/`acme_email`/`acme_cache_dir`, none of
	// which match the schema, so user answers were silently discarded.
	if !loaded.TLS.AutoTLS {
		t.Error("TLS.AutoTLS = false, want true (user TLS answer was silently dropped)")
	}
	if loaded.TLS.Email != "tls@example.com" {
		t.Errorf("TLS.Email = %q, want %q", loaded.TLS.Email, "tls@example.com")
	}
	wantCache := filepath.Join(dataDir, "acme")
	if loaded.TLS.CacheDir != wantCache {
		t.Errorf("TLS.CacheDir = %q, want %q", loaded.TLS.CacheDir, wantCache)
	}

	// Server + storage round-trip sanity.
	if loaded.Server.Hostname != "mail.example.com" {
		t.Errorf("Server.Hostname = %q, want %q", loaded.Server.Hostname, "mail.example.com")
	}
	if loaded.Server.Domain != "example.com" {
		t.Errorf("Server.Domain = %q, want %q", loaded.Server.Domain, "example.com")
	}
	if want := filepath.Join(dataDir, "mail.db"); loaded.Storage.DatabasePath != want {
		t.Errorf("Storage.DatabasePath = %q, want %q", loaded.Storage.DatabasePath, want)
	}
	if want := filepath.Join(dataDir, "maildir"); loaded.Storage.MaildirPath != want {
		t.Errorf("Storage.MaildirPath = %q, want %q", loaded.Storage.MaildirPath, want)
	}

	if loaded.Queue.RedisURL == "" {
		t.Error("Queue.RedisURL empty after load")
	}
	if !loaded.Admin.Enabled {
		t.Error("Admin.Enabled = false, want true")
	}
}

// TestGenerateConfig_KeepsExistingConfig verifies the idempotency branch:
// re-running setup against an existing config.yaml should not clobber it.
func TestGenerateConfig_KeepsExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, "etc")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	existing := []byte("# pre-existing config\n")
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, existing, 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cfg := &SetupConfig{
		Domain:    "example.com",
		Hostname:  "mail.example.com",
		TLSEmail:  "tls@example.com",
		DataDir:   filepath.Join(tmp, "var"),
		ConfigDir: configDir,
	}

	if err := generateConfig(cfg); err != nil {
		t.Fatalf("generateConfig: %v", err)
	}
	if !cfg.UseExisting {
		t.Error("UseExisting = false, want true when config.yaml exists")
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != string(existing) {
		t.Errorf("existing config was overwritten: %q", got)
	}
}

// TestAdminPasswordHash_VerifiesViaAuthPackage guards against a regression of
// the algorithm-mismatch bug flagged on PR #47: setup used bcrypt while
// internal/auth.VerifyPassword only accepts argon2id, leaving the admin user
// unable to authenticate. The wizard's hash must round-trip through the same
// verifier the runtime uses, otherwise `mailserver setup` silently produces a
// broken admin account.
func TestAdminPasswordHash_VerifiesViaAuthPackage(t *testing.T) {
	const plain = "correct horse battery staple"

	hash, err := auth.HashPassword(plain)
	if err != nil {
		t.Fatalf("auth.HashPassword: %v", err)
	}

	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("hash prefix = %q, want $argon2id$ (a bcrypt hash like $2a$ would be silently rejected by VerifyPassword)", hash[:min(len(hash), 12)])
	}

	if !auth.VerifyPassword(plain, hash) {
		t.Fatal("auth.VerifyPassword rejected hash produced by auth.HashPassword — algorithm mismatch")
	}
	if auth.VerifyPassword("wrong", hash) {
		t.Fatal("auth.VerifyPassword accepted wrong password")
	}
}

// TestSelfExe_ReturnsAbsolutePath ensures the helper that replaces the broken
// `exec.Command("mailserver", ...)` lookups returns a usable absolute path.
func TestSelfExe_ReturnsAbsolutePath(t *testing.T) {
	got, err := selfExe()
	if err != nil {
		t.Fatalf("selfExe: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("selfExe = %q, want absolute path", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("selfExe path %q does not exist: %v", got, err)
	}
}
