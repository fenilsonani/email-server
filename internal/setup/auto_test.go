package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewAutoSetup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "autosetup-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	setup := NewAutoSetup(tmpDir, nil)
	if setup == nil {
		t.Fatal("NewAutoSetup returned nil")
	}

	if setup.dataDir != tmpDir {
		t.Errorf("Expected dataDir=%s, got %s", tmpDir, setup.dataDir)
	}
}

func TestAutoSetup_Run(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "autosetup-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	setup := NewAutoSetup(tmpDir, nil)
	result, err := setup.Run(context.Background())

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Setup should succeed, errors: %v", result.Errors)
	}

	// Verify directories were created
	expectedDirs := []string{
		tmpDir,
		filepath.Join(tmpDir, "maildir"),
		filepath.Join(tmpDir, "dkim"),
		filepath.Join(tmpDir, "queue"),
		filepath.Join(tmpDir, "backups"),
		filepath.Join(tmpDir, "acme"),
		filepath.Join(tmpDir, "logs"),
		filepath.Join(tmpDir, "tmp"),
	}

	for _, dir := range expectedDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("Directory should exist: %s", dir)
		}
	}
}

func TestAutoSetup_Run_ConfigGenerated(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "autosetup-config-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	setup := NewAutoSetup(tmpDir, nil)
	result, err := setup.Run(context.Background())

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !result.ConfigGenerated {
		t.Error("Config should be generated on first run")
	}

	// Verify config file exists
	configPath := filepath.Join(tmpDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Config file should exist")
	}

	// Verify config content
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "server:") {
		t.Error("Config should contain server section")
	}
	if !strings.Contains(content, "database:") {
		t.Error("Config should contain database section")
	}
}

func TestAutoSetup_Run_Idempotent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "autosetup-idem-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	setup := NewAutoSetup(tmpDir, nil)

	// First run
	result1, err := setup.Run(context.Background())
	if err != nil {
		t.Fatalf("First run failed: %v", err)
	}
	if !result1.ConfigGenerated {
		t.Error("Config should be generated on first run")
	}

	// Second run - should be safe
	result2, err := setup.Run(context.Background())
	if err != nil {
		t.Fatalf("Second run failed: %v", err)
	}
	if result2.ConfigGenerated {
		t.Error("Config should NOT be regenerated on second run")
	}

	// Both should succeed
	if !result1.Success || !result2.Success {
		t.Error("Both runs should succeed")
	}
}

func TestAutoSetup_EnsureDirectories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "autosetup-dirs-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	setup := NewAutoSetup(tmpDir, nil)
	if err := setup.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	// Verify all directories exist
	dirs := setup.getRequiredDirectories()
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if os.IsNotExist(err) {
			t.Errorf("Directory should exist: %s", dir)
			continue
		}
		if !info.IsDir() {
			t.Errorf("Should be a directory: %s", dir)
		}
	}
}

func TestAutoSetup_GetPaths(t *testing.T) {
	tmpDir := "/var/lib/mailserver"
	setup := NewAutoSetup(tmpDir, nil)

	if setup.GetDataDir() != tmpDir {
		t.Errorf("GetDataDir mismatch")
	}

	if setup.GetMaildirPath() != filepath.Join(tmpDir, "maildir") {
		t.Errorf("GetMaildirPath mismatch: %s", setup.GetMaildirPath())
	}

	if setup.GetDatabasePath() != filepath.Join(tmpDir, "mail.db") {
		t.Errorf("GetDatabasePath mismatch: %s", setup.GetDatabasePath())
	}

	if setup.GetDKIMPath() != filepath.Join(tmpDir, "dkim") {
		t.Errorf("GetDKIMPath mismatch: %s", setup.GetDKIMPath())
	}

	if setup.GetQueuePath() != filepath.Join(tmpDir, "queue") {
		t.Errorf("GetQueuePath mismatch: %s", setup.GetQueuePath())
	}

	if setup.GetBackupPath() != filepath.Join(tmpDir, "backups") {
		t.Errorf("GetBackupPath mismatch: %s", setup.GetBackupPath())
	}
}

func TestQuickStart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "quickstart-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	result, err := QuickStart(tmpDir)
	if err != nil {
		t.Fatalf("QuickStart failed: %v", err)
	}

	if result["data_dir"] != tmpDir {
		t.Errorf("data_dir mismatch")
	}

	if result["config_path"] != filepath.Join(tmpDir, "config.yaml") {
		t.Errorf("config_path mismatch")
	}

	if result["database"] != filepath.Join(tmpDir, "mail.db") {
		t.Errorf("database path mismatch")
	}

	if result["maildir"] != filepath.Join(tmpDir, "maildir") {
		t.Errorf("maildir path mismatch")
	}
}

func TestDetectEnvironment(t *testing.T) {
	env := DetectEnvironment()

	// Should always have these keys
	if _, ok := env["container"]; !ok {
		t.Error("Should have container key")
	}

	if _, ok := env["data_dir"]; !ok {
		t.Error("Should have data_dir key")
	}

	if _, ok := env["redis_url"]; !ok {
		t.Error("Should have redis_url key")
	}

	if _, ok := env["database_driver"]; !ok {
		t.Error("Should have database_driver key")
	}

	// Container should be one of: docker, kubernetes, none
	container := env["container"]
	if container != "docker" && container != "kubernetes" && container != "none" {
		t.Errorf("Unexpected container value: %s", container)
	}

	// Database driver should be sqlite3 or postgres
	driver := env["database_driver"]
	if driver != "sqlite3" && driver != "postgres" {
		t.Errorf("Unexpected database_driver: %s", driver)
	}
}

func TestDetectEnvironment_PostgresEnv(t *testing.T) {
	// Set PostgreSQL environment
	os.Setenv("POSTGRES_HOST", "localhost")
	os.Setenv("POSTGRES_USER", "testuser")
	os.Setenv("POSTGRES_PASSWORD", "testpass")
	os.Setenv("POSTGRES_DB", "testdb")
	defer func() {
		os.Unsetenv("POSTGRES_HOST")
		os.Unsetenv("POSTGRES_USER")
		os.Unsetenv("POSTGRES_PASSWORD")
		os.Unsetenv("POSTGRES_DB")
	}()

	env := DetectEnvironment()

	if env["database_driver"] != "postgres" {
		t.Errorf("Expected postgres driver, got %s", env["database_driver"])
	}

	if !strings.Contains(env["database_dsn"], "testuser") {
		t.Errorf("DSN should contain user: %s", env["database_dsn"])
	}

	if !strings.Contains(env["database_dsn"], "localhost") {
		t.Errorf("DSN should contain host: %s", env["database_dsn"])
	}
}

func TestDetectEnvironment_RedisEnv(t *testing.T) {
	os.Setenv("REDIS_HOST", "redis.example.com")
	defer os.Unsetenv("REDIS_HOST")

	env := DetectEnvironment()

	if !strings.Contains(env["redis_url"], "redis.example.com") {
		t.Errorf("Redis URL should contain custom host: %s", env["redis_url"])
	}
}

func TestFileExists(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-file-")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	if !fileExists(tmpFile.Name()) {
		t.Error("fileExists should return true for existing file")
	}

	if fileExists("/nonexistent/path/to/file") {
		t.Error("fileExists should return false for nonexistent file")
	}
}

func TestDirExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-dir-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if !dirExists(tmpDir) {
		t.Error("dirExists should return true for existing directory")
	}

	if dirExists("/nonexistent/path/to/dir") {
		t.Error("dirExists should return false for nonexistent directory")
	}

	// Create a file, not a directory
	tmpFile, _ := os.CreateTemp("", "not-a-dir-")
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	if dirExists(tmpFile.Name()) {
		t.Error("dirExists should return false for file")
	}
}

func TestGenerateSecureToken(t *testing.T) {
	token1 := generateSecureToken(16)
	token2 := generateSecureToken(16)

	if len(token1) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("Token should be 32 chars, got %d", len(token1))
	}

	if token1 == token2 {
		t.Error("Tokens should be unique")
	}

	// Test different lengths
	token8 := generateSecureToken(8)
	if len(token8) != 16 {
		t.Errorf("8-byte token should be 16 chars, got %d", len(token8))
	}
}

func TestAutoSetup_Run_Timeout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "autosetup-timeout-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	setup := NewAutoSetup(tmpDir, nil)

	// Create a context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := setup.Run(ctx)

	if err != nil {
		t.Fatalf("Run should not fail with reasonable timeout: %v", err)
	}

	if !result.Success {
		t.Error("Setup should succeed")
	}
}

func TestAutoSetup_DatabaseMarker(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "autosetup-marker-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	setup := NewAutoSetup(tmpDir, nil)
	result, err := setup.Run(context.Background())

	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if !result.DatabaseSetup {
		t.Error("DatabaseSetup should be true on first run")
	}

	// Check marker file exists
	markerPath := filepath.Join(tmpDir, ".db_initialized")
	if !fileExists(markerPath) {
		t.Error("Database marker file should exist")
	}

	// Second run should not set DatabaseSetup
	result2, _ := setup.Run(context.Background())
	if result2.DatabaseSetup {
		t.Error("DatabaseSetup should be false on subsequent runs")
	}
}

// TestLogger is a test implementation of Logger
type TestLogger struct {
	infos  []string
	warns  []string
	errors []string
}

func (l *TestLogger) Info(msg string, args ...interface{})  { l.infos = append(l.infos, msg) }
func (l *TestLogger) Warn(msg string, args ...interface{})  { l.warns = append(l.warns, msg) }
func (l *TestLogger) Error(msg string, args ...interface{}) { l.errors = append(l.errors, msg) }

func TestAutoSetup_WithLogger(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "autosetup-logger-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := &TestLogger{}
	setup := NewAutoSetup(tmpDir, logger)

	_, err = setup.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Logger should have received info messages
	if len(logger.infos) == 0 {
		t.Error("Logger should have received info messages")
	}
}
