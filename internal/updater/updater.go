package updater

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/doctor"
	"github.com/fenilsonani/email-server/internal/logging"
)

type UpdateMode string
type TargetType string
type UpdateStatus string

const (
	ModeNormal UpdateMode = "normal"
	ModePower  UpdateMode = "power"

	TargetTypeRelease TargetType = "release"
	TargetTypePR      TargetType = "pr"
	TargetTypeBranch  TargetType = "branch"
	TargetTypeCommit  TargetType = "commit"

	StatusPending    UpdateStatus = "pending"
	StatusInProgress UpdateStatus = "in_progress"
	StatusCompleted  UpdateStatus = "completed"
	StatusFailed     UpdateStatus = "failed"
	StatusRolledBack UpdateStatus = "rolled_back"
)

// UpdateStep represents a single step in the update process
type UpdateStep struct {
	Number      int
	Name        string
	Description string
}

var UpdateSteps = []UpdateStep{
	{1, "validate", "Validating prerequisites and health"},
	{2, "backup", "Creating pre-update backup"},
	{3, "fetch", "Fetching target version from Git"},
	{4, "build", "Building new version"},
	{5, "test", "Running tests (power mode only)"},
	{6, "deploy", "Deploying new binary"},
	{7, "verify", "Verifying deployment and health"},
	{8, "cleanup", "Cleaning up temporary files"},
}

// UpdateOptions specifies parameters for an update
type UpdateOptions struct {
	Mode            UpdateMode
	TargetType      TargetType
	Target          string // Version, PR#, branch name, or commit SHA
	DryRun          bool
	SkipBackup      bool
	Force           bool
	SkipHealthCheck bool
	Username        string // Admin user performing the update
}

// UpdateResult contains the outcome of an update
type UpdateResult struct {
	Success           bool
	UpdateID          int64
	FromVersion       string
	ToVersion         string
	Duration          time.Duration
	StepsCompleted    int
	BackupPath        string
	RollbackAvailable bool
	HealthStatus      *doctor.Results
	Errors            []string
}

// UpdateManager orchestrates the entire update process
type UpdateManager struct {
	db              *sql.DB
	config          *config.UpdaterConfig
	logger          *logging.Logger
	doctor          *doctor.Doctor
	gitManager      *GitManager
	buildManager    *BuildManager
	deployManager   *DeployManager
	progressTracker *ProgressTracker
	versionManager  *VersionManager
	backupManager   *BackupManager
}

// NewUpdateManager creates a new UpdateManager
func NewUpdateManager(
	db *sql.DB,
	cfg *config.UpdaterConfig,
	logger *logging.Logger,
	doc *doctor.Doctor,
) *UpdateManager {
	return &UpdateManager{
		db:              db,
		config:          cfg,
		logger:          logger,
		doctor:          doc,
		gitManager:      NewGitManager(cfg, logger),
		buildManager:    NewBuildManager(cfg, logger),
		deployManager:   NewDeployManager(cfg, logger),
		progressTracker: NewProgressTracker(db, logger),
		versionManager:  NewVersionManager(db, cfg, logger),
		backupManager:   NewBackupManager(cfg, logger),
	}
}

// StartUpdate initiates an update process
func (um *UpdateManager) StartUpdate(ctx context.Context, opts UpdateOptions) (*UpdateResult, error) {
	startTime := time.Now()
	result := &UpdateResult{
		Errors: []string{},
	}

	// Step 1: Validate
	um.progressTracker.UpdateProgress(ctx, 0, 1, "validate", "pending", "Validating prerequisites...")

	currentVersion, currentCommit, err := um.getCurrentVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current version: %w", err)
	}
	result.FromVersion = currentVersion

	// Create update history record
	updateID, err := um.createUpdateHistory(ctx, opts, currentVersion, currentCommit)
	if err != nil {
		return nil, fmt.Errorf("failed to create update history: %w", err)
	}
	result.UpdateID = updateID

	// Step 2: Backup (if enabled)
	if !opts.SkipBackup && um.config.BackupBeforeUpdate {
		um.progressTracker.UpdateProgress(ctx, updateID, 2, "backup", "in_progress", "Creating pre-update backup...")
		backupPath, err := um.backupManager.CreateBackup(ctx, currentVersion, currentCommit)
		if err != nil {
			um.progressTracker.UpdateProgress(ctx, updateID, 2, "backup", "failed", fmt.Sprintf("Backup failed: %v", err))
			result.Errors = append(result.Errors, fmt.Sprintf("backup failed: %v", err))
		} else {
			result.BackupPath = backupPath
			if err := um.recordRollbackSnapshot(ctx, updateID, currentVersion, currentCommit, backupPath); err != nil {
				um.logger.Warn("Failed to record rollback snapshot", "update_id", updateID, "error", err)
			}
			// Only advertise the rollback as available once the DB row has
			// been updated to reflect it. If this persistence fails we can
			// still continue the update — but the caller must not be told
			// rollback is available, because rollbackUpdate() will refuse to
			// restore an update whose rollback_available column is not 1.
			if err := um.markRollbackAvailable(ctx, updateID, backupPath); err != nil {
				um.logger.Warn("Failed to persist rollback availability", "update_id", updateID, "error", err)
			} else {
				result.RollbackAvailable = true
			}
			um.progressTracker.UpdateProgress(ctx, updateID, 2, "backup", "completed", "Backup created successfully")
		}
	}

	// Step 3: Fetch
	um.progressTracker.UpdateProgress(ctx, updateID, 3, "fetch", "in_progress", "Fetching target version...")
	commitSHA, err := um.gitManager.Fetch(ctx, opts.TargetType, opts.Target)
	if err != nil {
		um.progressTracker.UpdateProgress(ctx, updateID, 3, "fetch", "failed", fmt.Sprintf("Fetch failed: %v", err))
		um.markUpdateFailed(ctx, updateID, fmt.Sprintf("fetch failed: %v", err))
		return result, fmt.Errorf("fetch failed: %w", err)
	}
	result.ToVersion = opts.Target
	um.progressTracker.UpdateProgress(ctx, updateID, 3, "fetch", "completed", "Fetch successful")

	// Step 4: Build
	um.progressTracker.UpdateProgress(ctx, updateID, 4, "build", "in_progress", "Building new version...")
	binPath, err := um.buildManager.Build(ctx, commitSHA)
	if err != nil {
		um.progressTracker.UpdateProgress(ctx, updateID, 4, "build", "failed", fmt.Sprintf("Build failed: %v", err))
		um.markUpdateFailed(ctx, updateID, fmt.Sprintf("build failed: %v", err))
		if um.config.AutoRollbackOnFailure && result.BackupPath != "" {
			if err := um.rollbackUpdate(ctx, updateID); err != nil {
				um.logger.Error("Automatic rollback failed after build error", "update_id", updateID, "error", err)
			}
		}
		return result, fmt.Errorf("build failed: %w", err)
	}
	um.progressTracker.UpdateProgress(ctx, updateID, 4, "build", "completed", "Build successful")

	// Step 5: Test (power mode only)
	if opts.Mode == ModePower {
		um.progressTracker.UpdateProgress(ctx, updateID, 5, "test", "in_progress", "Running tests...")
		if err := um.buildManager.Test(ctx); err != nil {
			um.progressTracker.UpdateProgress(ctx, updateID, 5, "test", "failed", fmt.Sprintf("Tests failed: %v", err))
			um.markUpdateFailed(ctx, updateID, fmt.Sprintf("test failed: %v", err))
			if um.config.AutoRollbackOnFailure && result.BackupPath != "" {
				if err := um.rollbackUpdate(ctx, updateID); err != nil {
					um.logger.Error("Automatic rollback failed after test error", "update_id", updateID, "error", err)
				}
			}
			return result, fmt.Errorf("test failed: %w", err)
		}
		um.progressTracker.UpdateProgress(ctx, updateID, 5, "test", "completed", "Tests passed")
	} else {
		result.StepsCompleted = 4
	}

	// Step 6: Deploy
	um.progressTracker.UpdateProgress(ctx, updateID, 6, "deploy", "in_progress", "Deploying new binary...")
	if err := um.deployManager.Deploy(ctx, binPath); err != nil {
		um.progressTracker.UpdateProgress(ctx, updateID, 6, "deploy", "failed", fmt.Sprintf("Deploy failed: %v", err))
		um.markUpdateFailed(ctx, updateID, fmt.Sprintf("deploy failed: %v", err))
		if um.config.AutoRollbackOnFailure && result.BackupPath != "" {
			if err := um.rollbackUpdate(ctx, updateID); err != nil {
				um.logger.Error("Automatic rollback failed after deploy error", "update_id", updateID, "error", err)
			}
		}
		return result, fmt.Errorf("deploy failed: %w", err)
	}
	um.progressTracker.UpdateProgress(ctx, updateID, 6, "deploy", "completed", "Deployment successful")

	// Step 7: Verify
	um.progressTracker.UpdateProgress(ctx, updateID, 7, "verify", "in_progress", "Verifying deployment...")
	if um.config.RequireHealthCheck {
		healthStatus := um.doctor.Run(ctx)
		if healthStatus == nil || len(healthStatus.Checks) == 0 {
			um.progressTracker.UpdateProgress(ctx, updateID, 7, "verify", "failed", "Health check returned no results")
			um.markUpdateFailed(ctx, updateID, "health check failed: no results")
			if um.config.AutoRollbackOnFailure && result.BackupPath != "" {
				if err := um.rollbackUpdate(ctx, updateID); err != nil {
					um.logger.Error("Automatic rollback failed after health-check error", "update_id", updateID, "error", err)
				}
			}
			return result, fmt.Errorf("health check failed: no results")
		}
		result.HealthStatus = healthStatus

		// Check if any checks failed
		if healthStatus.Failed > 0 {
			um.progressTracker.UpdateProgress(ctx, updateID, 7, "verify", "failed", fmt.Sprintf("Health check failed: %d checks failed", healthStatus.Failed))
			um.markUpdateFailed(ctx, updateID, fmt.Sprintf("health check failed: %d checks failed", healthStatus.Failed))
			if um.config.AutoRollbackOnFailure && result.BackupPath != "" {
				if err := um.rollbackUpdate(ctx, updateID); err != nil {
					um.logger.Error("Automatic rollback failed after health-check error", "update_id", updateID, "error", err)
				}
			}
			return result, fmt.Errorf("health check failed: %d checks failed", healthStatus.Failed)
		}
	}
	um.progressTracker.UpdateProgress(ctx, updateID, 7, "verify", "completed", "Verification successful")

	// Step 8: Cleanup
	um.progressTracker.UpdateProgress(ctx, updateID, 8, "cleanup", "in_progress", "Cleaning up...")
	um.buildManager.Cleanup(ctx)
	um.progressTracker.UpdateProgress(ctx, updateID, 8, "cleanup", "completed", "Cleanup complete")

	// Mark as completed
	duration := time.Since(startTime)
	um.markUpdateCompleted(ctx, updateID, duration, commitSHA)
	result.Success = true
	result.Duration = duration
	result.StepsCompleted = 8
	// result.RollbackAvailable is already set (or left false) during the
	// backup step based on whether markRollbackAvailable actually persisted
	// the state. Do not override it here.

	return result, nil
}

// Helper methods

func (um *UpdateManager) getCurrentVersion(ctx context.Context) (string, string, error) {
	version, err := um.versionManager.GetCurrentVersion(ctx)
	if err != nil {
		return "", "", err
	}
	return version.Version, version.Commit, nil
}

func (um *UpdateManager) createUpdateHistory(ctx context.Context, opts UpdateOptions, fromVersion, fromCommit string) (int64, error) {
	query := `
	INSERT INTO update_history (update_type, from_version, from_commit, status, started_by, pr_number, branch_name, backup_path, rollback_available)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	var prNumber *int
	if opts.TargetType == TargetTypePR {
		// Parse PR number from target
		pn := 0
		fmt.Sscanf(opts.Target, "%d", &pn)
		if pn > 0 {
			prNumber = &pn
		}
	}

	result, err := um.db.ExecContext(ctx, query,
		string(opts.TargetType),
		fromVersion,
		fromCommit,
		StatusPending,
		opts.Username,
		prNumber,
		func() *string {
			if opts.TargetType == TargetTypeBranch {
				return &opts.Target
			}
			return nil
		}(),
		nil,
		false,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (um *UpdateManager) markUpdateCompleted(ctx context.Context, updateID int64, duration time.Duration, toCommit string) error {
	query := `
	UPDATE update_history
	SET status = ?, completed_at = ?, duration_seconds = ?, to_commit = ?
	WHERE id = ?
	`
	_, err := um.db.ExecContext(ctx, query,
		StatusCompleted,
		time.Now(),
		int(duration.Seconds()),
		toCommit,
		updateID,
	)
	return err
}

func (um *UpdateManager) markUpdateFailed(ctx context.Context, updateID int64, errorMsg string) error {
	query := `
	UPDATE update_history
	SET status = ?, completed_at = ?, error_message = ?
	WHERE id = ?
	`
	_, err := um.db.ExecContext(ctx, query,
		StatusFailed,
		time.Now(),
		errorMsg,
		updateID,
	)
	return err
}

// rollbackUpdate restores the binary recorded in the pre-update snapshot for
// updateID and restarts the service. It is safe against concurrent callers:
// the first caller atomically claims the rollback by flipping
// rollback_available from 1 to 0, and any concurrent caller observes zero
// rows affected and returns an error. If any step after the claim fails,
// the claim is released so an operator can retry.
//
// Once the claim is taken, the rollback runs on a detached context with a
// generous timeout so that an admin closing their browser mid-rollback
// cannot kill the restore, restart, or finalise steps and leave the system
// in a half-rolled-back state.
func (um *UpdateManager) rollbackUpdate(ctx context.Context, updateID int64) error {
	// Atomically claim the rollback. Only updates in a terminal state
	// (failed/completed) with a live backup are eligible — this prevents
	// rolling back an update that's still in progress, and prevents two
	// admins from rolling back the same update concurrently. The claim
	// itself honours the caller's context: cancellation before we've
	// committed to anything is fine.
	claim, err := um.db.ExecContext(ctx, `
		UPDATE update_history
		SET rollback_available = 0
		WHERE id = ? AND rollback_available = 1 AND status IN (?, ?)
	`, updateID, StatusFailed, StatusCompleted)
	if err != nil {
		return fmt.Errorf("failed to claim rollback: %w", err)
	}
	claimed, err := claim.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rollback claim: %w", err)
	}
	if claimed == 0 {
		return fmt.Errorf("rollback not available for update %d", updateID)
	}

	// Detach from the caller's context for every subsequent step. Once we
	// have the claim, the rollback must run to completion (or fail cleanly
	// and release the claim) regardless of whether the admin stays connected.
	rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelRollback()

	// Release the claim on any failure below so the operator can retry. Use
	// a second detached context so it survives even if rollbackCtx has been
	// cancelled/timed-out by the time we reach the defer.
	success := false
	defer func() {
		if success {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, relErr := um.db.ExecContext(releaseCtx, `
			UPDATE update_history
			SET rollback_available = 1
			WHERE id = ?
		`, updateID); relErr != nil {
			um.logger.Error("Failed to release rollback claim after error", "update_id", updateID, "error", relErr)
		}
	}()

	backupPath, err := um.getRollbackBackupPath(rollbackCtx, updateID)
	if err != nil {
		return err
	}
	if backupPath == "" {
		return fmt.Errorf("rollback backup not available for update %d", updateID)
	}

	um.logger.Warn("Initiating rollback", "update_id", updateID, "backup_path", backupPath)
	if err := um.backupManager.RestoreFromBackup(rollbackCtx, backupPath); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	// Restart the service so the restored binary actually takes effect.
	// A failed restart means the running process is still on the failed
	// build even though the binary on disk is correct, so we must NOT
	// mark the update as rolled back in that case — the caller needs to
	// know the rollback is only half-done and investigate.
	//
	// SkipServiceRestart is for tests and for production environments
	// where service lifecycle is managed externally (k8s, nomad, etc.);
	// in those cases the operator is responsible for the restart.
	if um.deployManager != nil && !um.config.SkipServiceRestart {
		if err := um.deployManager.RestartService(rollbackCtx); err != nil {
			return fmt.Errorf("service restart after rollback failed: %w", err)
		}
	}

	if _, err := um.db.ExecContext(rollbackCtx, `
		UPDATE update_history
		SET status = ?, completed_at = ?
		WHERE id = ?
	`, StatusRolledBack, time.Now(), updateID); err != nil {
		return fmt.Errorf("failed to mark update rolled back: %w", err)
	}

	success = true
	return nil
}

// RollbackUpdate restores a previously backed-up version for the given update.
func (um *UpdateManager) RollbackUpdate(ctx context.Context, updateID int64) error {
	return um.rollbackUpdate(ctx, updateID)
}

func (um *UpdateManager) markRollbackAvailable(ctx context.Context, updateID int64, backupPath string) error {
	query := `
	UPDATE update_history
	SET backup_path = ?, rollback_available = 1
	WHERE id = ?
	`
	_, err := um.db.ExecContext(ctx, query, backupPath, updateID)
	return err
}

func (um *UpdateManager) recordRollbackSnapshot(ctx context.Context, updateID int64, version, commitSHA, backupPath string) error {
	query := `
	INSERT INTO rollback_snapshots (update_id, snapshot_type, version, commit_sha, binary_path, backup_path, config_snapshot, health_status)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := um.db.ExecContext(ctx, query,
		updateID,
		"pre_update",
		version,
		commitSHA,
		um.config.BinaryPath,
		backupPath,
		nil,
		nil,
	)
	return err
}

func (um *UpdateManager) getRollbackBackupPath(ctx context.Context, updateID int64) (string, error) {
	var backupPath sql.NullString
	err := um.db.QueryRowContext(ctx, `
		SELECT backup_path
		FROM rollback_snapshots
		WHERE update_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, updateID).Scan(&backupPath)
	if err == nil && backupPath.Valid && strings.TrimSpace(backupPath.String) != "" {
		return backupPath.String, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	var historyBackup sql.NullString
	if err := um.db.QueryRowContext(ctx, `SELECT backup_path FROM update_history WHERE id = ?`, updateID).Scan(&historyBackup); err != nil {
		return "", err
	}
	if historyBackup.Valid && strings.TrimSpace(historyBackup.String) != "" {
		return historyBackup.String, nil
	}
	return "", nil
}
