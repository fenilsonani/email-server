package updater

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestDBForVersion(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	schema := `
	CREATE TABLE version_cache (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		version_type TEXT NOT NULL,
		version_name TEXT NOT NULL,
		commit_sha TEXT NOT NULL,
		published_at DATETIME,
		is_prerelease BOOLEAN DEFAULT 0,
		changelog TEXT,
		metadata TEXT,
		cached_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(version_type, version_name)
	);
	`

	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("Failed to create test schema: %v", err)
	}

	return db
}

func TestNewVersionManager(t *testing.T) {
	db := setupTestDBForVersion(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/test/repo",
	}
	logger := logging.Default()

	vm := NewVersionManager(db, cfg, logger)
	if vm == nil {
		t.Error("NewVersionManager returned nil")
	}
	if vm.config != cfg {
		t.Error("Config not set correctly")
	}
	if vm.logger != logger {
		t.Error("Logger not set correctly")
	}
}

func TestGetCurrentVersion(t *testing.T) {
	db := setupTestDBForVersion(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/test/repo",
	}
	logger := logging.Default()
	vm := NewVersionManager(db, cfg, logger)

	ctx := context.Background()
	info, err := vm.GetCurrentVersion(ctx)
	if err != nil {
		t.Fatalf("Failed to get current version: %v", err)
	}

	if info == nil {
		t.Error("GetCurrentVersion returned nil")
	}
	if info.Version == "" {
		t.Error("Version should not be empty")
	}
	if info.GoVersion == "" {
		t.Error("GoVersion should not be empty")
	}
}

func TestCacheRelease(t *testing.T) {
	db := setupTestDBForVersion(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/test/repo",
	}
	logger := logging.Default()
	vm := NewVersionManager(db, cfg, logger)

	ctx := context.Background()

	releaseInfo := &ReleaseInfo{
		Version:      "v1.0.0",
		CommitSHA:    "abc123def456",
		PublishedAt:  time.Now(),
		IsPrerelease: false,
		Changelog:    "Release v1.0.0 with new features",
	}

	err := vm.CacheRelease(ctx, "release", "v1.0.0", releaseInfo)
	if err != nil {
		t.Fatalf("Failed to cache release: %v", err)
	}

	// Verify the record was created
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM version_cache WHERE version_name = ?", "v1.0.0").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query version_cache: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 record, got %d", count)
	}
}

func TestGetCachedRelease(t *testing.T) {
	db := setupTestDBForVersion(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/test/repo",
	}
	logger := logging.Default()
	vm := NewVersionManager(db, cfg, logger)

	ctx := context.Background()

	// Cache a release first
	releaseInfo := &ReleaseInfo{
		Version:      "v1.0.0",
		CommitSHA:    "abc123def456",
		PublishedAt:  time.Now(),
		IsPrerelease: false,
		Changelog:    "Release v1.0.0",
	}
	err := vm.CacheRelease(ctx, "release", "v1.0.0", releaseInfo)
	if err != nil {
		t.Fatalf("Failed to cache release: %v", err)
	}

	// Retrieve the cached release
	cachedInfo, err := vm.GetCachedRelease(ctx, "release", "v1.0.0")
	if err != nil {
		t.Fatalf("Failed to get cached release: %v", err)
	}

	if cachedInfo == nil {
		t.Error("GetCachedRelease returned nil")
		return
	}

	if cachedInfo.Version != "v1.0.0" {
		t.Errorf("Expected version v1.0.0, got %s", cachedInfo.Version)
	}
	if cachedInfo.CommitSHA != "abc123def456" {
		t.Errorf("Expected commit abc123def456, got %s", cachedInfo.CommitSHA)
	}
}

func TestGetCachedReleaseNotFound(t *testing.T) {
	db := setupTestDBForVersion(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/test/repo",
	}
	logger := logging.Default()
	vm := NewVersionManager(db, cfg, logger)

	ctx := context.Background()

	// Try to get a non-existent release
	cachedInfo, err := vm.GetCachedRelease(ctx, "release", "v99.0.0")
	if err != nil {
		t.Fatalf("Failed to query cached release: %v", err)
	}

	if cachedInfo != nil {
		t.Error("GetCachedRelease should return nil for non-existent release")
	}
}

func TestVersionInfoType(t *testing.T) {
	info := &VersionInfo{
		Version:   "v1.0.0",
		Commit:    "abc123",
		BuildTime: "2024-01-25T10:00:00Z",
		GoVersion: "go1.21",
	}

	if info.Version != "v1.0.0" {
		t.Errorf("Expected version v1.0.0, got %s", info.Version)
	}
	if info.Commit != "abc123" {
		t.Errorf("Expected commit abc123, got %s", info.Commit)
	}
}

func TestReleaseInfoType(t *testing.T) {
	now := time.Now()
	info := &ReleaseInfo{
		Version:      "v1.0.0",
		CommitSHA:    "abc123",
		PublishedAt:  now,
		IsPrerelease: false,
		Changelog:    "Test release",
	}

	if info.Version != "v1.0.0" {
		t.Errorf("Expected version v1.0.0, got %s", info.Version)
	}
	if info.CommitSHA != "abc123" {
		t.Errorf("Expected commit abc123, got %s", info.CommitSHA)
	}
	if info.IsPrerelease {
		t.Error("Expected IsPrerelease to be false")
	}
	if !info.PublishedAt.Equal(now) {
		t.Error("PublishedAt not set correctly")
	}
}
