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

// setupIntegrationTestDB creates a complete test database with all required tables
func setupIntegrationTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	schema := `
	-- Core update tables
	CREATE TABLE update_settings (
		id INTEGER PRIMARY KEY,
		mode TEXT NOT NULL DEFAULT 'normal',
		auto_check_enabled BOOLEAN DEFAULT 1,
		auto_check_interval INTEGER DEFAULT 3600,
		git_repo_url TEXT DEFAULT 'https://github.com/fenilsonani/email-server',
		current_branch TEXT DEFAULT 'main',
		current_commit TEXT,
		current_version TEXT,
		build_path TEXT DEFAULT '/tmp/mailserver-build',
		backup_before_update BOOLEAN DEFAULT 1,
		max_backups INTEGER DEFAULT 5,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

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

	CREATE INDEX idx_update_history_status ON update_history(status);
	CREATE INDEX idx_update_history_started_by ON update_history(started_by);
	CREATE INDEX idx_update_history_created ON update_history(started_at DESC);

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

	CREATE INDEX idx_update_progress_update ON update_progress(update_id);
	CREATE INDEX idx_update_progress_step ON update_progress(update_id, step_number);

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

	CREATE INDEX idx_version_cache_type ON version_cache(version_type);
	CREATE INDEX idx_version_cache_cached ON version_cache(cached_at DESC);

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

	CREATE INDEX idx_rollback_snapshots_update ON rollback_snapshots(update_id);
	CREATE INDEX idx_rollback_snapshots_expires ON rollback_snapshots(expires_at);
	CREATE INDEX idx_rollback_snapshots_type ON rollback_snapshots(snapshot_type);
	`

	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("Failed to create test schema: %v", err)
	}

	return db
}

// TestIntegration_FullUpdateWorkflow tests a complete update workflow
func TestIntegration_FullUpdateWorkflow(t *testing.T) {
	db := setupIntegrationTestDB(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL:         "https://github.com/fenilsonani/email-server",
		BuildPath:          "/tmp/test-build",
		BackupBeforeUpdate: true,
		MaxBackups:         5,
		RequireHealthCheck: true,
	}
	logger := logging.Default()

	// Create managers
	um := NewUpdateManager(db, cfg, logger, nil)
	versionMgr := NewVersionManager(db, cfg, logger)
	progressMgr := NewProgressTracker(db, logger)

	ctx := context.Background()

	// Test 1: Get current version
	t.Run("GetCurrentVersion", func(t *testing.T) {
		currentVersion, err := versionMgr.GetCurrentVersion(ctx)
		if err != nil {
			t.Fatalf("Failed to get current version: %v", err)
		}
		if currentVersion == nil {
			t.Error("Expected version info, got nil")
		}
		t.Logf("Current version: %s (commit: %s)", currentVersion.Version, currentVersion.Commit)
	})

	// Test 2: Cache a release
	t.Run("CacheRelease", func(t *testing.T) {
		releaseInfo := &ReleaseInfo{
			Version:      "v1.0.0",
			CommitSHA:    "abc123def456",
			PublishedAt:  time.Now(),
			IsPrerelease: false,
			Changelog:    "Version 1.0.0 - Initial release",
		}

		err := versionMgr.CacheRelease(ctx, "release", "v1.0.0", releaseInfo)
		if err != nil {
			t.Fatalf("Failed to cache release: %v", err)
		}

		// Verify cached release can be retrieved
		cached, err := versionMgr.GetCachedRelease(ctx, "release", "v1.0.0")
		if err != nil {
			t.Fatalf("Failed to get cached release: %v", err)
		}
		if cached == nil {
			t.Error("Expected cached release, got nil")
		}
		if cached.Version != "v1.0.0" {
			t.Errorf("Expected version v1.0.0, got %s", cached.Version)
		}
		t.Logf("Successfully cached release: %s", cached.Version)
	})

	// Test 3: Create update history
	var updateID int64
	t.Run("CreateUpdateHistory", func(t *testing.T) {
		opts := UpdateOptions{
			Mode:       ModeNormal,
			TargetType: TargetTypeRelease,
			Target:     "v1.0.0",
			Username:   "admin",
		}

		var err error
		updateID, err = um.createUpdateHistory(ctx, opts, "v0.9.0", "old_commit_sha")
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
		t.Logf("Created update history with ID: %d", updateID)
	})

	// Test 4: Track update progress through all steps
	t.Run("TrackUpdateProgress", func(t *testing.T) {
		// Simulate progress through all 8 steps
		steps := []struct {
			stepNum int
			name    string
			status  string
		}{
			{1, "validate", "completed"},
			{2, "backup", "completed"},
			{3, "fetch", "completed"},
			{4, "build", "completed"},
			{5, "test", "completed"},
			{6, "deploy", "completed"},
			{7, "verify", "completed"},
			{8, "cleanup", "completed"},
		}

		for _, step := range steps {
			err := progressMgr.UpdateProgress(ctx, updateID, step.stepNum, step.name, step.status, "Step completed successfully")
			if err != nil {
				t.Fatalf("Failed to update progress for step %d: %v", step.stepNum, err)
			}
		}

		// Verify all steps were tracked
		progress, err := progressMgr.GetProgress(ctx, updateID)
		if err != nil {
			t.Fatalf("Failed to get progress: %v", err)
		}

		if len(progress) != 8 {
			t.Errorf("Expected 8 progress records, got %d", len(progress))
		}

		// Verify overall progress
		overallProgress, err := progressMgr.GetOverallProgress(ctx, updateID)
		if err != nil {
			t.Fatalf("Failed to get overall progress: %v", err)
		}

		if overallProgress <= 0 || overallProgress > 100 {
			t.Errorf("Expected progress between 1-100, got %d", overallProgress)
		}

		t.Logf("All 8 steps tracked successfully, overall progress: %d%%", overallProgress)
	})

	// Test 5: Mark update as completed
	t.Run("MarkUpdateCompleted", func(t *testing.T) {
		duration := 5 * time.Minute
		err := um.markUpdateCompleted(ctx, updateID, duration, "new_commit_sha")
		if err != nil {
			t.Fatalf("Failed to mark update completed: %v", err)
		}

		// Verify the update was marked as completed
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

		t.Logf("Update marked as completed in %d seconds", durationSecs)
	})

	// Test 6: Retrieve complete update history
	t.Run("QueryUpdateHistory", func(t *testing.T) {
		rows, err := db.QueryContext(ctx, `
			SELECT id, status, duration_seconds, started_by
			FROM update_history
			WHERE id = ?
		`, updateID)
		if err != nil {
			t.Fatalf("Failed to query history: %v", err)
		}
		defer rows.Close()

		count := 0
		for rows.Next() {
			count++
			var id int64
			var status string
			var durationSecs *int
			var startedBy string

			err := rows.Scan(&id, &status, &durationSecs, &startedBy)
			if err != nil {
				t.Fatalf("Failed to scan row: %v", err)
			}

			if status != string(StatusCompleted) {
				t.Errorf("Expected completed status, got %q", status)
			}
			if startedBy != "admin" {
				t.Errorf("Expected admin user, got %q", startedBy)
			}

			t.Logf("Retrieved update: ID=%d, Status=%s, Duration=%d seconds, User=%s",
				id, status, *durationSecs, startedBy)
		}

		if count != 1 {
			t.Errorf("Expected 1 update record, got %d", count)
		}
	})
}

// TestIntegration_FailedUpdateWithRollback tests update failure and rollback
func TestIntegration_FailedUpdateWithRollback(t *testing.T) {
	db := setupIntegrationTestDB(t)
	defer db.Close()

	buildDir := t.TempDir()
	binaryPath := filepath.Join(buildDir, "mailserver")
	backupDir := filepath.Join(buildDir, "backups", "pre-update-1")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("Failed to create backup dir: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("new binary"), 0o750); err != nil {
		t.Fatalf("Failed to write binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "mailserver-binary"), []byte("old binary"), 0o750); err != nil {
		t.Fatalf("Failed to write backup binary: %v", err)
	}

	cfg := &config.UpdaterConfig{
		GitRepoURL:         "https://github.com/fenilsonani/email-server",
		BuildPath:          buildDir,
		BinaryPath:         binaryPath,
		SystemdService:     "mailserver.service",
		SkipServiceRestart: true, // tests must not exec systemctl
	}
	logger := logging.Default()

	um := NewUpdateManager(db, cfg, logger, nil)
	progressMgr := NewProgressTracker(db, logger)

	ctx := context.Background()

	// Create an update
	opts := UpdateOptions{
		Mode:       ModeNormal,
		TargetType: TargetTypeRelease,
		Target:     "v1.0.0",
		Username:   "admin",
	}

	updateID, err := um.createUpdateHistory(ctx, opts, "v0.9.0", "old_commit")
	if err != nil {
		t.Fatalf("Failed to create update: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE update_history SET backup_path = ?, rollback_available = 1 WHERE id = ?`, backupDir, updateID); err != nil {
		t.Fatalf("Failed to update rollback metadata: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO rollback_snapshots (update_id, snapshot_type, version, commit_sha, binary_path, backup_path) VALUES (?, 'pre_update', ?, ?, ?, ?)`, updateID, "v0.9.0", "old_commit", binaryPath, backupDir); err != nil {
		t.Fatalf("Failed to insert rollback snapshot: %v", err)
	}

	t.Run("SimulateFailureAndRollback", func(t *testing.T) {
		// Simulate progress through some steps
		steps := []struct {
			stepNum int
			name    string
		}{
			{1, "validate"},
			{2, "backup"},
			{3, "fetch"},
			{4, "build"},
		}

		for _, step := range steps {
			err := progressMgr.UpdateProgress(ctx, updateID, step.stepNum, step.name, "in_progress", "")
			if err != nil {
				t.Fatalf("Failed to update progress: %v", err)
			}
		}

		// Mark first 3 as completed
		for i := 1; i <= 3; i++ {
			err := progressMgr.MarkStepCompleted(ctx, updateID, i)
			if err != nil {
				t.Fatalf("Failed to mark step completed: %v", err)
			}
		}

		// Simulate a failure at the build step (step 4)
		buildErr := progressMgr.MarkStepFailed(ctx, updateID, 4, "Build failed: missing dependency")
		if buildErr != nil {
			t.Fatalf("Failed to mark step failed: %v", buildErr)
		}

		// Mark the entire update as failed
		err := um.markUpdateFailed(ctx, updateID, "Build compilation failed")
		if err != nil {
			t.Fatalf("Failed to mark update failed: %v", err)
		}

		// Verify update is marked as failed
		var status string
		var errorMsg string
		err = db.QueryRowContext(ctx, "SELECT status, error_message FROM update_history WHERE id = ?", updateID).Scan(&status, &errorMsg)
		if err != nil {
			t.Fatalf("Failed to query update: %v", err)
		}

		if status != string(StatusFailed) {
			t.Errorf("Expected status 'failed', got %q", status)
		}
		if errorMsg != "Build compilation failed" {
			t.Errorf("Expected error message, got %q", errorMsg)
		}

		if err := um.RollbackUpdate(ctx, updateID); err != nil {
			t.Fatalf("RollbackUpdate failed: %v", err)
		}

		data, err := os.ReadFile(binaryPath)
		if err != nil {
			t.Fatalf("Failed to read restored binary: %v", err)
		}
		if string(data) != "old binary" {
			t.Fatalf("Expected restored binary contents, got %q", string(data))
		}

		if err := db.QueryRowContext(ctx, "SELECT status FROM update_history WHERE id = ?", updateID).Scan(&status); err != nil {
			t.Fatalf("Failed to query rolled back status: %v", err)
		}
		if status != string(StatusRolledBack) {
			t.Errorf("Expected status 'rolled_back', got %q", status)
		}

		// Verify only 3 of 8 steps completed before failure
		progress, err := progressMgr.GetProgress(ctx, updateID)
		if err != nil {
			t.Fatalf("Failed to get progress: %v", err)
		}

		completedSteps := 0
		failedSteps := 0
		for _, p := range progress {
			if p.Status == "completed" {
				completedSteps++
			} else if p.Status == "failed" {
				failedSteps++
			}
		}

		if completedSteps != 3 {
			t.Errorf("Expected 3 completed steps, got %d", completedSteps)
		}
		if failedSteps != 1 {
			t.Errorf("Expected 1 failed step, got %d", failedSteps)
		}

		t.Logf("Update failed as expected: %s (Completed: %d, Failed: %d)",
			errorMsg, completedSteps, failedSteps)
	})
}

// TestIntegration_MultipleUpdatesTracking tests tracking multiple concurrent updates
func TestIntegration_MultipleUpdatesTracking(t *testing.T) {
	db := setupIntegrationTestDB(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/fenilsonani/email-server",
	}
	logger := logging.Default()

	um := NewUpdateManager(db, cfg, logger, nil)
	progressMgr := NewProgressTracker(db, logger)

	ctx := context.Background()

	t.Run("TrackMultipleUpdates", func(t *testing.T) {
		// Create 3 updates
		updateIDs := make([]int64, 3)
		versions := []string{"v0.8.0", "v0.9.0", "v1.0.0"}
		targets := []string{"v0.9.0", "v1.0.0", "v1.1.0"}

		for i := 0; i < 3; i++ {
			opts := UpdateOptions{
				Mode:       ModeNormal,
				TargetType: TargetTypeRelease,
				Target:     targets[i],
				Username:   "admin",
			}

			id, err := um.createUpdateHistory(ctx, opts, versions[i], "commit"+string(rune(48+i)))
			if err != nil {
				t.Fatalf("Failed to create update %d: %v", i+1, err)
			}
			updateIDs[i] = id

			// Track some progress for each
			for step := 1; step <= i+1; step++ {
				err := progressMgr.UpdateProgress(ctx, id, step, "step"+string(rune(48+step)), "completed", "")
				if err != nil {
					t.Fatalf("Failed to update progress: %v", err)
				}
			}
		}

		// Verify all updates are tracked separately
		for i, id := range updateIDs {
			progress, err := progressMgr.GetProgress(ctx, id)
			if err != nil {
				t.Fatalf("Failed to get progress for update %d: %v", i+1, err)
			}

			expectedSteps := i + 1
			if len(progress) != expectedSteps {
				t.Errorf("Update %d: expected %d steps, got %d", i+1, expectedSteps, len(progress))
			}

			status, err := progressMgr.GetUpdateStatus(ctx, id)
			if err != nil {
				t.Fatalf("Failed to get status for update %d: %v", i+1, err)
			}

			t.Logf("Update %d: %d steps completed, status: %s", i+1, len(progress), status)
		}

		// Verify we can query all updates
		rows, err := db.QueryContext(ctx, "SELECT COUNT(*) FROM update_history")
		if err != nil {
			t.Fatalf("Failed to count updates: %v", err)
		}
		defer rows.Close()

		if rows.Next() {
			var count int
			if err := rows.Scan(&count); err != nil {
				t.Fatalf("Failed to scan count: %v", err)
			}
			if count != 3 {
				t.Errorf("Expected 3 updates, got %d", count)
			}
			t.Logf("Successfully tracked %d concurrent updates", count)
		}
	})
}

// TestIntegration_VersionCaching tests the version caching mechanism
func TestIntegration_VersionCaching(t *testing.T) {
	db := setupIntegrationTestDB(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/fenilsonani/email-server",
	}
	logger := logging.Default()

	versionMgr := NewVersionManager(db, cfg, logger)

	ctx := context.Background()

	t.Run("CacheAndRetrieveMultipleVersions", func(t *testing.T) {
		// Cache multiple releases
		releases := []struct {
			version string
			commit  string
		}{
			{"v1.0.0", "abc123"},
			{"v1.1.0", "def456"},
			{"v2.0.0", "ghi789"},
		}

		for _, rel := range releases {
			info := &ReleaseInfo{
				Version:      rel.version,
				CommitSHA:    rel.commit,
				PublishedAt:  time.Now(),
				IsPrerelease: false,
				Changelog:    "Release " + rel.version,
			}

			err := versionMgr.CacheRelease(ctx, "release", rel.version, info)
			if err != nil {
				t.Fatalf("Failed to cache %s: %v", rel.version, err)
			}
		}

		// Retrieve and verify all cached versions
		for _, rel := range releases {
			cached, err := versionMgr.GetCachedRelease(ctx, "release", rel.version)
			if err != nil {
				t.Fatalf("Failed to get cached %s: %v", rel.version, err)
			}

			if cached == nil {
				t.Errorf("Expected cached %s, got nil", rel.version)
				continue
			}

			if cached.Version != rel.version {
				t.Errorf("Expected version %s, got %s", rel.version, cached.Version)
			}
			if cached.CommitSHA != rel.commit {
				t.Errorf("Expected commit %s, got %s", rel.commit, cached.CommitSHA)
			}

			t.Logf("Successfully cached and retrieved: %s (%s)", cached.Version, cached.CommitSHA)
		}

		// Verify cache count
		var count int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM version_cache").Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count cache entries: %v", err)
		}
		if count != 3 {
			t.Errorf("Expected 3 cache entries, got %d", count)
		}

		t.Logf("Version cache contains %d entries", count)
	})
}

// TestIntegration_UpdateHistoryQueries tests complex update history queries
func TestIntegration_UpdateHistoryQueries(t *testing.T) {
	db := setupIntegrationTestDB(t)
	defer db.Close()

	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/fenilsonani/email-server",
	}
	logger := logging.Default()

	um := NewUpdateManager(db, cfg, logger, nil)

	ctx := context.Background()

	t.Run("QueryUpdatesByStatus", func(t *testing.T) {
		// Create updates with different statuses
		updates := []struct {
			status string
			target string
		}{
			{string(StatusCompleted), "v1.0.0"},
			{string(StatusCompleted), "v1.1.0"},
			{string(StatusFailed), "v2.0.0"},
		}

		createdIDs := []int64{}

		for _, u := range updates {
			opts := UpdateOptions{
				Mode:       ModeNormal,
				TargetType: TargetTypeRelease,
				Target:     u.target,
				Username:   "admin",
			}

			id, err := um.createUpdateHistory(ctx, opts, "v0.9.0", "old_commit")
			if err != nil {
				t.Fatalf("Failed to create update: %v", err)
			}
			createdIDs = append(createdIDs, id)

			// Mark with appropriate status
			if u.status == string(StatusCompleted) {
				um.markUpdateCompleted(ctx, id, 1*time.Minute, "new_commit")
			} else {
				um.markUpdateFailed(ctx, id, "Test failure")
			}
		}

		// Verify records were created
		if len(createdIDs) != 3 {
			t.Fatalf("Expected 3 updates created, got %d", len(createdIDs))
		}

		// Query completed updates
		var completedCount int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM update_history WHERE status = ?", string(StatusCompleted)).Scan(&completedCount)
		if err != nil {
			t.Fatalf("Failed to query completed: %v", err)
		}

		if completedCount != 2 {
			t.Errorf("Expected 2 completed updates, got %d", completedCount)
		}

		// Query failed updates
		var failedCount int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM update_history WHERE status = ?", string(StatusFailed)).Scan(&failedCount)
		if err != nil {
			t.Fatalf("Failed to query failed: %v", err)
		}

		if failedCount != 1 {
			t.Errorf("Expected 1 failed update, got %d", failedCount)
		}

		// Query by user
		var userCount int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM update_history WHERE started_by = ?", "admin").Scan(&userCount)
		if err != nil {
			t.Fatalf("Failed to query by user: %v", err)
		}

		if userCount != 3 {
			t.Errorf("Expected 3 updates by admin, got %d", userCount)
		}

		t.Logf("Update status query successful: %d completed, %d failed, %d by admin", completedCount, failedCount, userCount)
	})
}
