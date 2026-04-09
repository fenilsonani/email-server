package updater

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
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
		Mode:               "normal",
		AutoCheckEnabled:   true,
		AutoCheckInterval:  3600,
		GitRepoURL:         "https://github.com/test/repo",
		BuildPath:          "/tmp/test-build",
		BackupBeforeUpdate: true,
		MaxBackups:         5,
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

// rollbackFixture wires up the on-disk and in-DB state needed to exercise
// UpdateManager.RollbackUpdate without reaching for a real updater config
// on each test.
type rollbackFixture struct {
	db         *sql.DB
	um         *UpdateManager
	updateID   int64
	binaryPath string
	backupDir  string
}

func newRollbackFixture(t *testing.T, status UpdateStatus, rollbackAvailable bool) *rollbackFixture {
	t.Helper()
	db := setupTestDB(t)
	t.Cleanup(func() { db.Close() })

	buildDir := t.TempDir()
	binaryPath := filepath.Join(buildDir, "mailserver")
	backupDir := filepath.Join(buildDir, "backups", "pre-update-1")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("failed to create backup dir: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("new binary"), 0o750); err != nil {
		t.Fatalf("failed to write binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "mailserver-binary"), []byte("old binary"), 0o750); err != nil {
		t.Fatalf("failed to write backup binary: %v", err)
	}

	availableFlag := 0
	if rollbackAvailable {
		availableFlag = 1
	}
	res, err := db.Exec(`INSERT INTO update_history (update_type, from_version, to_version, from_commit, to_commit, status, started_by, backup_path, rollback_available) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"release", "v1.0.0", "v1.1.0", "oldcommit", "newcommit", string(status), "admin", backupDir, availableFlag)
	if err != nil {
		t.Fatalf("failed to insert update history: %v", err)
	}
	updateID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("failed to get update id: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO rollback_snapshots (update_id, snapshot_type, version, commit_sha, binary_path, backup_path) VALUES (?, 'pre_update', ?, ?, ?, ?)`, updateID, "v1.0.0", "oldcommit", binaryPath, backupDir); err != nil {
		t.Fatalf("failed to insert rollback snapshot: %v", err)
	}

	cfg := &config.UpdaterConfig{
		GitRepoURL:     "https://github.com/fenilsonani/email-server",
		BuildPath:      buildDir,
		BinaryPath:     binaryPath,
		SystemdService: "mailserver.service",
	}
	return &rollbackFixture{
		db:         db,
		um:         NewUpdateManager(db, cfg, logging.Default(), nil),
		updateID:   updateID,
		binaryPath: binaryPath,
		backupDir:  backupDir,
	}
}

func (f *rollbackFixture) rollbackAvailable(t *testing.T) bool {
	t.Helper()
	var available bool
	if err := f.db.QueryRow(`SELECT rollback_available FROM update_history WHERE id = ?`, f.updateID).Scan(&available); err != nil {
		t.Fatalf("failed to query rollback_available: %v", err)
	}
	return available
}

func (f *rollbackFixture) status(t *testing.T) string {
	t.Helper()
	var status string
	if err := f.db.QueryRow(`SELECT status FROM update_history WHERE id = ?`, f.updateID).Scan(&status); err != nil {
		t.Fatalf("failed to query status: %v", err)
	}
	return status
}

func TestRollbackUpdateRestoresBackup(t *testing.T) {
	f := newRollbackFixture(t, StatusFailed, true)

	if err := f.um.RollbackUpdate(context.Background(), f.updateID); err != nil {
		t.Fatalf("RollbackUpdate() error = %v", err)
	}

	data, err := os.ReadFile(f.binaryPath)
	if err != nil {
		t.Fatalf("failed to read restored binary: %v", err)
	}
	if string(data) != "old binary" {
		t.Fatalf("restored binary = %q, want %q", string(data), "old binary")
	}
	if got := f.status(t); got != string(StatusRolledBack) {
		t.Fatalf("status = %q, want %q", got, StatusRolledBack)
	}
	if f.rollbackAvailable(t) {
		t.Fatal("rollback_available = true, want false")
	}
}

// Second rollback of the same update must be rejected. The first call flips
// rollback_available to 0 as part of its atomic claim; the second finds no
// row to claim and returns an error without touching the binary.
func TestRollbackUpdateRejectsDoubleRollback(t *testing.T) {
	f := newRollbackFixture(t, StatusFailed, true)
	ctx := context.Background()

	if err := f.um.RollbackUpdate(ctx, f.updateID); err != nil {
		t.Fatalf("first RollbackUpdate() error = %v", err)
	}

	// Overwrite the binary so we can detect whether the second rollback
	// (wrongly) restored it again.
	if err := os.WriteFile(f.binaryPath, []byte("post-rollback edit"), 0o750); err != nil {
		t.Fatalf("failed to overwrite binary: %v", err)
	}

	if err := f.um.RollbackUpdate(ctx, f.updateID); err == nil {
		t.Fatal("second RollbackUpdate() succeeded, want error")
	}
	data, err := os.ReadFile(f.binaryPath)
	if err != nil {
		t.Fatalf("failed to read binary: %v", err)
	}
	if string(data) != "post-rollback edit" {
		t.Fatalf("binary was restored by second rollback: got %q", string(data))
	}
}

// Rollback must refuse updates that are still in progress — pulling the
// binary out from under a running deploy would corrupt on-disk state.
func TestRollbackUpdateRefusesInProgressUpdate(t *testing.T) {
	f := newRollbackFixture(t, StatusInProgress, true)

	err := f.um.RollbackUpdate(context.Background(), f.updateID)
	if err == nil {
		t.Fatal("RollbackUpdate() on in-progress update succeeded, want error")
	}
	if !f.rollbackAvailable(t) {
		t.Fatal("rollback_available cleared after refused rollback, want preserved")
	}
	if got := f.status(t); got != string(StatusInProgress) {
		t.Fatalf("status = %q, want %q", got, StatusInProgress)
	}
}

// If the restore itself fails, the claim must be released so the operator
// can retry. Otherwise one bad attempt would permanently wedge the rollback.
func TestRollbackUpdateReleasesClaimOnRestoreFailure(t *testing.T) {
	f := newRollbackFixture(t, StatusFailed, true)

	// Corrupt the backup so RestoreFromBackup fails.
	if err := os.Remove(filepath.Join(f.backupDir, "mailserver-binary")); err != nil {
		t.Fatalf("failed to remove backup binary: %v", err)
	}

	if err := f.um.RollbackUpdate(context.Background(), f.updateID); err == nil {
		t.Fatal("RollbackUpdate() with missing backup succeeded, want error")
	}
	if !f.rollbackAvailable(t) {
		t.Fatal("rollback_available = false after failed restore, want reset to true so operator can retry")
	}
	if got := f.status(t); got != string(StatusFailed) {
		t.Fatalf("status = %q, want %q (unchanged on failed rollback)", got, StatusFailed)
	}
}

func TestRecordRollbackSnapshotPersistsMetadata(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	buildDir := t.TempDir()
	binaryPath := filepath.Join(buildDir, "mailserver")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o750); err != nil {
		t.Fatalf("failed to write binary: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO update_history (update_type, from_version, to_version, from_commit, to_commit, status, started_by, rollback_available) VALUES (?, ?, ?, ?, ?, ?, ?, 0)`, "release", "v1.0.0", "v1.1.0", "oldcommit", "newcommit", string(StatusPending), "admin"); err != nil {
		t.Fatalf("failed to insert update history: %v", err)
	}
	var updateID int64
	if err := db.QueryRow(`SELECT id FROM update_history LIMIT 1`).Scan(&updateID); err != nil {
		t.Fatalf("failed to get update id: %v", err)
	}

	cfg := &config.UpdaterConfig{BuildPath: buildDir, BinaryPath: binaryPath}
	um := NewUpdateManager(db, cfg, logging.Default(), nil)
	if err := um.recordRollbackSnapshot(context.Background(), updateID, "v1.0.0", "oldcommit", filepath.Join(buildDir, "backups", "pre-update-1")); err != nil {
		t.Fatalf("recordRollbackSnapshot() error = %v", err)
	}
	if err := um.markRollbackAvailable(context.Background(), updateID, filepath.Join(buildDir, "backups", "pre-update-1")); err != nil {
		t.Fatalf("markRollbackAvailable() error = %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM rollback_snapshots WHERE update_id = ?`, updateID).Scan(&count); err != nil {
		t.Fatalf("failed to query rollback snapshots: %v", err)
	}
	if count != 1 {
		t.Fatalf("rollback snapshot count = %d, want 1", count)
	}

	var backupPath string
	var rollbackAvailable bool
	if err := db.QueryRow(`SELECT backup_path, rollback_available FROM update_history WHERE id = ?`, updateID).Scan(&backupPath, &rollbackAvailable); err != nil {
		t.Fatalf("failed to query update history: %v", err)
	}
	if backupPath == "" {
		t.Fatal("expected backup path to be persisted")
	}
	if !rollbackAvailable {
		t.Fatal("rollback_available = false, want true")
	}
}
