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

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Create required tables
	schema := `
	CREATE TABLE update_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		update_type TEXT NOT NULL,
		from_version TEXT,
		to_version TEXT,
		from_commit TEXT,
		to_commit TEXT,
		pr_number INTEGER,
		branch_name TEXT,
		status TEXT NOT NULL,
		started_by TEXT NOT NULL,
		started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		completed_at DATETIME,
		duration_seconds INTEGER,
		backup_path TEXT,
		rollback_available BOOLEAN DEFAULT 1,
		error_message TEXT,
		metadata TEXT
	);

	CREATE TABLE update_progress (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		update_id INTEGER NOT NULL,
		step_number INTEGER NOT NULL,
		step_name TEXT NOT NULL,
		status TEXT NOT NULL,
		message TEXT,
		progress_percent INTEGER DEFAULT 0,
		started_at DATETIME,
		completed_at DATETIME,
		FOREIGN KEY (update_id) REFERENCES update_history(id) ON DELETE CASCADE
	);

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

	CREATE TABLE rollback_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		update_id INTEGER NOT NULL,
		snapshot_type TEXT DEFAULT 'pre_update',
		version TEXT NOT NULL,
		commit_sha TEXT NOT NULL,
		binary_path TEXT NOT NULL,
		backup_path TEXT NOT NULL,
		config_snapshot TEXT,
		health_status TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME,
		FOREIGN KEY (update_id) REFERENCES update_history(id)
	);
	`

	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("Failed to create test schema: %v", err)
	}

	return db
}

func TestUpdateSteps(t *testing.T) {
	if len(UpdateSteps) != 8 {
		t.Errorf("Expected 8 update steps, got %d", len(UpdateSteps))
	}

	expectedSteps := []string{"validate", "backup", "fetch", "build", "test", "deploy", "verify", "cleanup"}
	for i, step := range UpdateSteps {
		if step.Name != expectedSteps[i] {
			t.Errorf("Step %d: expected %q, got %q", i, expectedSteps[i], step.Name)
		}
	}
}

func TestUpdateModeConstants(t *testing.T) {
	tests := []struct {
		name string
		mode UpdateMode
		want string
	}{
		{"Normal", ModeNormal, "normal"},
		{"Power", ModePower, "power"},
	}

	for _, tt := range tests {
		if string(tt.mode) != tt.want {
			t.Errorf("%s: expected %q, got %q", tt.name, tt.want, string(tt.mode))
		}
	}
}

func TestTargetTypeConstants(t *testing.T) {
	tests := []struct {
		name       string
		targetType TargetType
		want       string
	}{
		{"Release", TargetTypeRelease, "release"},
		{"PR", TargetTypePR, "pr"},
		{"Branch", TargetTypeBranch, "branch"},
		{"Commit", TargetTypeCommit, "commit"},
	}

	for _, tt := range tests {
		if string(tt.targetType) != tt.want {
			t.Errorf("%s: expected %q, got %q", tt.name, tt.want, string(tt.targetType))
		}
	}
}

func TestUpdateStatusConstants(t *testing.T) {
	tests := []struct {
		name   string
		status UpdateStatus
		want   string
	}{
		{"Pending", StatusPending, "pending"},
		{"InProgress", StatusInProgress, "in_progress"},
		{"Completed", StatusCompleted, "completed"},
		{"Failed", StatusFailed, "failed"},
		{"RolledBack", StatusRolledBack, "rolled_back"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.want {
			t.Errorf("%s: expected %q, got %q", tt.name, tt.want, string(tt.status))
		}
	}
}

func TestNewUpdateManager(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		Mode:              "normal",
		AutoCheckEnabled:  true,
		AutoCheckInterval: 3600,
		GitRepoURL:        "https://github.com/test/repo",
		BuildPath:         "/tmp/test-build",
		BackupBeforeUpdate: true,
		MaxBackups:        5,
	}

	logger := logging.Default()

	um := NewUpdateManager(db, cfg, logger, nil)
	if um == nil {
		t.Error("NewUpdateManager returned nil")
	}
	if um.config != cfg {
		t.Error("Config not set correctly")
	}
	if um.logger != logger {
		t.Error("Logger not set correctly")
	}
}

func TestCreateUpdateHistory(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/test/repo",
	}
	logger := logging.Default()
	um := NewUpdateManager(db, cfg, logger, nil)

	ctx := context.Background()
	opts := UpdateOptions{
		Mode:       ModeNormal,
		TargetType: TargetTypeRelease,
		Target:     "v1.0.0",
		Username:   "admin",
	}

	updateID, err := um.createUpdateHistory(ctx, opts, "v0.9.0", "abc123")
	if err != nil {
		t.Fatalf("Failed to create update history: %v", err)
	}

	if updateID <= 0 {
		t.Errorf("Expected positive update ID, got %d", updateID)
	}

	// Verify the record was created
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM update_history WHERE id = ?", updateID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query update history: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 record, got %d", count)
	}
}

func TestMarkUpdateCompleted(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/test/repo",
	}
	logger := logging.Default()
	um := NewUpdateManager(db, cfg, logger, nil)

	ctx := context.Background()

	// Create an update first
	opts := UpdateOptions{
		Mode:       ModeNormal,
		TargetType: TargetTypeRelease,
		Target:     "v1.0.0",
		Username:   "admin",
	}
	updateID, _ := um.createUpdateHistory(ctx, opts, "v0.9.0", "abc123")

	// Mark as completed
	duration := 5 * time.Minute
	err := um.markUpdateCompleted(ctx, updateID, duration, "def456")
	if err != nil {
		t.Fatalf("Failed to mark update completed: %v", err)
	}

	// Verify the update was marked
	var status string
	var durationSecs int
	err = db.QueryRowContext(ctx, "SELECT status, duration_seconds FROM update_history WHERE id = ?", updateID).Scan(&status, &durationSecs)
	if err != nil {
		t.Fatalf("Failed to query update: %v", err)
	}

	if status != string(StatusCompleted) {
		t.Errorf("Expected status 'completed', got %q", status)
	}
	if durationSecs != 300 {
		t.Errorf("Expected duration 300 seconds, got %d", durationSecs)
	}
}

func TestMarkUpdateFailed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/test/repo",
	}
	logger := logging.Default()
	um := NewUpdateManager(db, cfg, logger, nil)

	ctx := context.Background()

	// Create an update first
	opts := UpdateOptions{
		Mode:       ModeNormal,
		TargetType: TargetTypeRelease,
		Target:     "v1.0.0",
		Username:   "admin",
	}
	updateID, _ := um.createUpdateHistory(ctx, opts, "v0.9.0", "abc123")

	// Mark as failed
	err := um.markUpdateFailed(ctx, updateID, "Build failed: missing dependency")
	if err != nil {
		t.Fatalf("Failed to mark update failed: %v", err)
	}

	// Verify the update was marked
	var status string
	var errorMsg string
	err = db.QueryRowContext(ctx, "SELECT status, error_message FROM update_history WHERE id = ?", updateID).Scan(&status, &errorMsg)
	if err != nil {
		t.Fatalf("Failed to query update: %v", err)
	}

	if status != string(StatusFailed) {
		t.Errorf("Expected status 'failed', got %q", status)
	}
	if errorMsg != "Build failed: missing dependency" {
		t.Errorf("Expected error message, got %q", errorMsg)
	}
}
