package updater

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestDBForProgress(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	schema := `
	CREATE TABLE update_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		status TEXT NOT NULL,
		started_at DATETIME DEFAULT CURRENT_TIMESTAMP
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
	`

	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("Failed to create test schema: %v", err)
	}

	// Insert a dummy update
	_, err = db.Exec("INSERT INTO update_history (status) VALUES (?)", "in_progress")
	if err != nil {
		t.Fatalf("Failed to insert test update: %v", err)
	}

	return db
}

func TestNewProgressTracker(t *testing.T) {
	db := setupTestDBForProgress(t)
	defer db.Close()

	logger := logging.Default()
	pt := NewProgressTracker(db, logger)

	if pt == nil {
		t.Error("NewProgressTracker returned nil")
	}
	if pt.logger != logger {
		t.Error("Logger not set correctly")
	}
}

func TestUpdateProgress_InsertNew(t *testing.T) {
	db := setupTestDBForProgress(t)
	defer db.Close()

	logger := logging.Default()
	pt := NewProgressTracker(db, logger)

	ctx := context.Background()
	updateID := int64(1)

	err := pt.UpdateProgress(ctx, updateID, 1, "validate", "in_progress", "Validating prerequisites")
	if err != nil {
		t.Fatalf("Failed to update progress: %v", err)
	}

	// Verify the record was created
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM update_progress WHERE update_id = ? AND step_number = ?", updateID, 1).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query progress: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 record, got %d", count)
	}
}

func TestUpdateProgress_UpdateExisting(t *testing.T) {
	db := setupTestDBForProgress(t)
	defer db.Close()

	logger := logging.Default()
	pt := NewProgressTracker(db, logger)

	ctx := context.Background()
	updateID := int64(1)

	// Insert first
	err := pt.UpdateProgress(ctx, updateID, 1, "validate", "in_progress", "Validating")
	if err != nil {
		t.Fatalf("Failed to insert progress: %v", err)
	}

	// Update existing
	err = pt.UpdateProgress(ctx, updateID, 1, "validate", "completed", "Validation complete")
	if err != nil {
		t.Fatalf("Failed to update progress: %v", err)
	}

	// Verify still only one record
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM update_progress WHERE update_id = ? AND step_number = ?", updateID, 1).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query progress: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 record, got %d", count)
	}

	// Verify status was updated
	var status string
	err = db.QueryRowContext(ctx, "SELECT status FROM update_progress WHERE update_id = ? AND step_number = ?", updateID, 1).Scan(&status)
	if err != nil {
		t.Fatalf("Failed to query status: %v", err)
	}
	if status != "completed" {
		t.Errorf("Expected status 'completed', got %q", status)
	}
}

func TestGetProgress(t *testing.T) {
	db := setupTestDBForProgress(t)
	defer db.Close()

	logger := logging.Default()
	pt := NewProgressTracker(db, logger)

	ctx := context.Background()
	updateID := int64(1)

	// Insert multiple progress records
	for i := 1; i <= 3; i++ {
		err := pt.UpdateProgress(ctx, updateID, i, "step"+string(rune(48+i)), "pending", "")
		if err != nil {
			t.Fatalf("Failed to insert progress: %v", err)
		}
	}

	// Get progress
	progress, err := pt.GetProgress(ctx, updateID)
	if err != nil {
		t.Fatalf("Failed to get progress: %v", err)
	}

	if len(progress) != 3 {
		t.Errorf("Expected 3 progress records, got %d", len(progress))
	}

	for i, p := range progress {
		if p.StepNumber != i+1 {
			t.Errorf("Step %d: expected step number %d, got %d", i, i+1, p.StepNumber)
		}
	}
}

func TestGetOverallProgress(t *testing.T) {
	db := setupTestDBForProgress(t)
	defer db.Close()

	logger := logging.Default()
	pt := NewProgressTracker(db, logger)

	ctx := context.Background()
	updateID := int64(1)

	// Insert progress records
	for i := 1; i <= 4; i++ {
		err := pt.UpdateProgress(ctx, updateID, i, "step"+string(rune(48+i)), "pending", "")
		if err != nil {
			t.Fatalf("Failed to insert progress: %v", err)
		}
	}

	// Get overall progress
	progress, err := pt.GetOverallProgress(ctx, updateID)
	if err != nil {
		t.Fatalf("Failed to get overall progress: %v", err)
	}

	// Progress should be non-zero (average of all steps)
	if progress <= 0 {
		t.Errorf("Expected positive progress, got %d", progress)
	}
	if progress > 100 {
		t.Errorf("Expected progress <= 100, got %d", progress)
	}
}

func TestGetUpdateStatus(t *testing.T) {
	db := setupTestDBForProgress(t)
	defer db.Close()

	logger := logging.Default()
	pt := NewProgressTracker(db, logger)

	ctx := context.Background()

	// Get status of the dummy update
	status, err := pt.GetUpdateStatus(ctx, 1)
	if err != nil {
		t.Fatalf("Failed to get update status: %v", err)
	}

	if status != "in_progress" {
		t.Errorf("Expected status 'in_progress', got %q", status)
	}
}

func TestMarkStepCompleted(t *testing.T) {
	db := setupTestDBForProgress(t)
	defer db.Close()

	logger := logging.Default()
	pt := NewProgressTracker(db, logger)

	ctx := context.Background()
	updateID := int64(1)

	// Insert a progress record
	err := pt.UpdateProgress(ctx, updateID, 1, "validate", "in_progress", "Validating")
	if err != nil {
		t.Fatalf("Failed to insert progress: %v", err)
	}

	// Mark as completed
	err = pt.MarkStepCompleted(ctx, updateID, 1)
	if err != nil {
		t.Fatalf("Failed to mark step completed: %v", err)
	}

	// Verify status was updated
	var status string
	err = db.QueryRowContext(ctx, "SELECT status FROM update_progress WHERE update_id = ? AND step_number = ?", updateID, 1).Scan(&status)
	if err != nil {
		t.Fatalf("Failed to query status: %v", err)
	}
	if status != "completed" {
		t.Errorf("Expected status 'completed', got %q", status)
	}

	// Verify completed_at was set
	var completedAt sql.NullTime
	err = db.QueryRowContext(ctx, "SELECT completed_at FROM update_progress WHERE update_id = ? AND step_number = ?", updateID, 1).Scan(&completedAt)
	if err != nil {
		t.Fatalf("Failed to query completed_at: %v", err)
	}
	if !completedAt.Valid {
		t.Error("Expected completed_at to be set")
	}
}

func TestMarkStepFailed(t *testing.T) {
	db := setupTestDBForProgress(t)
	defer db.Close()

	logger := logging.Default()
	pt := NewProgressTracker(db, logger)

	ctx := context.Background()
	updateID := int64(1)

	// Insert a progress record
	err := pt.UpdateProgress(ctx, updateID, 1, "validate", "in_progress", "Validating")
	if err != nil {
		t.Fatalf("Failed to insert progress: %v", err)
	}

	// Mark as failed
	err = pt.MarkStepFailed(ctx, updateID, 1, "Validation failed: disk space low")
	if err != nil {
		t.Fatalf("Failed to mark step failed: %v", err)
	}

	// Verify status was updated
	var status string
	var message string
	err = db.QueryRowContext(ctx, "SELECT status, message FROM update_progress WHERE update_id = ? AND step_number = ?", updateID, 1).Scan(&status, &message)
	if err != nil {
		t.Fatalf("Failed to query status: %v", err)
	}
	if status != "failed" {
		t.Errorf("Expected status 'failed', got %q", status)
	}
	if message != "Validation failed: disk space low" {
		t.Errorf("Expected error message, got %q", message)
	}
}

func TestGetOverallProgress_NoRecords(t *testing.T) {
	db := setupTestDBForProgress(t)
	defer db.Close()

	logger := logging.Default()
	pt := NewProgressTracker(db, logger)

	ctx := context.Background()

	// Get progress for non-existent update
	progress, err := pt.GetOverallProgress(ctx, 999)
	if err != nil {
		t.Fatalf("Failed to get overall progress: %v", err)
	}

	if progress != 0 {
		t.Errorf("Expected progress 0 for non-existent update, got %d", progress)
	}
}

func TestUpdateProgressType(t *testing.T) {
	now := time.Now()
	progress := &UpdateProgress{
		ID:              1,
		StepNumber:      1,
		StepName:        "validate",
		Status:          "completed",
		Message:         "Test message",
		ProgressPercent: 50,
		StartedAt:       &now,
		CompletedAt:     &now,
	}

	if progress.ID != 1 {
		t.Errorf("Expected ID 1, got %d", progress.ID)
	}
	if progress.StepNumber != 1 {
		t.Errorf("Expected step number 1, got %d", progress.StepNumber)
	}
	if progress.Status != "completed" {
		t.Errorf("Expected status 'completed', got %q", progress.Status)
	}
	if progress.ProgressPercent != 50 {
		t.Errorf("Expected progress 50, got %d", progress.ProgressPercent)
	}
}
