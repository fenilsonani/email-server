package updater

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
)

// ProgressTracker manages update progress tracking in the database
type ProgressTracker struct {
	db     *sql.DB
	logger *logging.Logger
}

// NewProgressTracker creates a new ProgressTracker
func NewProgressTracker(db *sql.DB, logger *logging.Logger) *ProgressTracker {
	return &ProgressTracker{
		db:     db,
		logger: logger,
	}
}

// UpdateProgress records progress for a specific update step
func (pt *ProgressTracker) UpdateProgress(ctx context.Context, updateID int64, stepNumber int, stepName string, status string, message string) error {
	// First, try to get existing progress record
	query := `
	SELECT id FROM update_progress
	WHERE update_id = ? AND step_number = ?
	LIMIT 1
	`
	var existingID int64
	err := pt.db.QueryRowContext(ctx, query, updateID, stepNumber).Scan(&existingID)

	if err == nil {
		// Update existing progress
		return pt.updateExistingProgress(ctx, existingID, status, message)
	}

	// Insert new progress record
	return pt.insertNewProgress(ctx, updateID, stepNumber, stepName, status, message)
}

func (pt *ProgressTracker) insertNewProgress(ctx context.Context, updateID int64, stepNumber int, stepName string, status string, message string) error {
	query := `
	INSERT INTO update_progress (update_id, step_number, step_name, status, message, progress_percent, started_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	progressPercent := (stepNumber * 100) / len(UpdateSteps)
	_, err := pt.db.ExecContext(ctx, query,
		updateID,
		stepNumber,
		stepName,
		status,
		message,
		progressPercent,
		time.Now(),
	)
	return err
}

func (pt *ProgressTracker) updateExistingProgress(ctx context.Context, progressID int64, status string, message string) error {
	query := `
	UPDATE update_progress
	SET status = ?, message = ?, completed_at = ?
	WHERE id = ?
	`
	_, err := pt.db.ExecContext(ctx, query,
		status,
		message,
		func() *time.Time {
			if status == "completed" || status == "failed" {
				now := time.Now()
				return &now
			}
			return nil
		}(),
		progressID,
	)
	return err
}

// GetProgress retrieves the progress for an update
func (pt *ProgressTracker) GetProgress(ctx context.Context, updateID int64) ([]UpdateProgress, error) {
	query := `
	SELECT id, step_number, step_name, status, message, progress_percent, started_at, completed_at
	FROM update_progress
	WHERE update_id = ?
	ORDER BY step_number ASC
	`
	rows, err := pt.db.QueryContext(ctx, query, updateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var progress []UpdateProgress
	for rows.Next() {
		var p UpdateProgress
		var startedAt, completedAt *time.Time

		if err := rows.Scan(
			&p.ID, &p.StepNumber, &p.StepName, &p.Status,
			&p.Message, &p.ProgressPercent, &startedAt, &completedAt,
		); err != nil {
			return nil, err
		}

		if startedAt != nil {
			p.StartedAt = startedAt
		}
		if completedAt != nil {
			p.CompletedAt = completedAt
		}

		progress = append(progress, p)
	}

	return progress, rows.Err()
}

// UpdateProgress represents a single update step progress
type UpdateProgress struct {
	ID              int64
	StepNumber      int
	StepName        string
	Status          string
	Message         string
	ProgressPercent int
	StartedAt       *time.Time
	CompletedAt     *time.Time
}

// GetOverallProgress calculates the overall progress percentage
func (pt *ProgressTracker) GetOverallProgress(ctx context.Context, updateID int64) (int, error) {
	progress, err := pt.GetProgress(ctx, updateID)
	if err != nil {
		return 0, err
	}

	if len(progress) == 0 {
		return 0, nil
	}

	totalPercent := 0
	for _, p := range progress {
		totalPercent += p.ProgressPercent
	}

	return totalPercent / len(progress), nil
}

// GetUpdateStatus returns the overall status of an update
func (pt *ProgressTracker) GetUpdateStatus(ctx context.Context, updateID int64) (string, error) {
	query := `
	SELECT status FROM update_history WHERE id = ? LIMIT 1
	`
	var status string
	err := pt.db.QueryRowContext(ctx, query, updateID).Scan(&status)
	if err != nil {
		return "", fmt.Errorf("failed to get update status: %w", err)
	}
	return status, nil
}

// MarkStepCompleted marks a step as completed
func (pt *ProgressTracker) MarkStepCompleted(ctx context.Context, updateID int64, stepNumber int) error {
	query := `
	UPDATE update_progress
	SET status = 'completed', completed_at = ?
	WHERE update_id = ? AND step_number = ?
	`
	_, err := pt.db.ExecContext(ctx, query, time.Now(), updateID, stepNumber)
	return err
}

// MarkStepFailed marks a step as failed
func (pt *ProgressTracker) MarkStepFailed(ctx context.Context, updateID int64, stepNumber int, errorMsg string) error {
	query := `
	UPDATE update_progress
	SET status = 'failed', message = ?, completed_at = ?
	WHERE update_id = ? AND step_number = ?
	`
	_, err := pt.db.ExecContext(ctx, query, errorMsg, time.Now(), updateID, stepNumber)
	return err
}
