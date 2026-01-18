package migration

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func createTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "migration-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to open database: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

func createTestSchema(db *sql.DB) error {
	schema := `
		CREATE TABLE IF NOT EXISTS domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER,
			username TEXT NOT NULL,
			email TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (domain_id) REFERENCES domains(id)
		);
	`
	_, err := db.Exec(schema)
	return err
}

func insertTestData(db *sql.DB) error {
	_, err := db.Exec(`INSERT INTO domains (name) VALUES ('example.com'), ('test.com')`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		INSERT INTO users (domain_id, username, email) VALUES
		(1, 'alice', 'alice@example.com'),
		(1, 'bob', 'bob@example.com'),
		(2, 'charlie', 'charlie@test.com')
	`)
	return err
}

func TestNewAutoMigrator(t *testing.T) {
	source, cleanupSource := createTestDB(t)
	defer cleanupSource()

	target, cleanupTarget := createTestDB(t)
	defer cleanupTarget()

	migrator := NewAutoMigrator(source, target, nil)
	if migrator == nil {
		t.Fatal("NewAutoMigrator returned nil")
	}

	// Logger should default to DefaultLogger
	if migrator.logger == nil {
		t.Error("Logger should not be nil")
	}
}

func TestDefaultLogger(t *testing.T) {
	logger := DefaultLogger{}

	// These should not panic
	logger.Info("test info %s", "arg")
	logger.Warn("test warn %s", "arg")
	logger.Error("test error %s", "arg")
}

func TestAutoMigrator_DetectAndMigrate_EmptySource(t *testing.T) {
	source, cleanupSource := createTestDB(t)
	defer cleanupSource()

	target, cleanupTarget := createTestDB(t)
	defer cleanupTarget()

	// Create schema but no data
	if err := createTestSchema(source); err != nil {
		t.Fatalf("Failed to create source schema: %v", err)
	}
	if err := createTestSchema(target); err != nil {
		t.Fatalf("Failed to create target schema: %v", err)
	}

	migrator := NewAutoMigrator(source, target, nil)
	result, err := migrator.DetectAndMigrate(context.Background())

	if err != nil {
		t.Fatalf("DetectAndMigrate failed: %v", err)
	}

	if !result.Success {
		t.Error("Migration should succeed for empty source")
	}

	// No rows should be copied
	for table, count := range result.RowsCopied {
		if count > 0 {
			t.Errorf("Table %s should have 0 rows copied, got %d", table, count)
		}
	}
}

func TestAutoMigrator_DetectAndMigrate_TargetHasData(t *testing.T) {
	source, cleanupSource := createTestDB(t)
	defer cleanupSource()

	target, cleanupTarget := createTestDB(t)
	defer cleanupTarget()

	// Create schema and data in both
	if err := createTestSchema(source); err != nil {
		t.Fatalf("Failed to create source schema: %v", err)
	}
	if err := createTestSchema(target); err != nil {
		t.Fatalf("Failed to create target schema: %v", err)
	}
	if err := insertTestData(source); err != nil {
		t.Fatalf("Failed to insert source data: %v", err)
	}
	if err := insertTestData(target); err != nil {
		t.Fatalf("Failed to insert target data: %v", err)
	}

	migrator := NewAutoMigrator(source, target, nil)
	result, err := migrator.DetectAndMigrate(context.Background())

	if err != nil {
		t.Fatalf("DetectAndMigrate failed: %v", err)
	}

	if !result.Success {
		t.Error("Migration should succeed (skip) when target has data")
	}

	// No rows should be copied since target already has data
	totalCopied := int64(0)
	for _, count := range result.RowsCopied {
		totalCopied += count
	}
	if totalCopied > 0 {
		t.Errorf("No rows should be copied when target has data, got %d", totalCopied)
	}
}

func TestAutoMigrator_DetectAndMigrate_WithData(t *testing.T) {
	source, cleanupSource := createTestDB(t)
	defer cleanupSource()

	target, cleanupTarget := createTestDB(t)
	defer cleanupTarget()

	// Create schema and data in source only
	if err := createTestSchema(source); err != nil {
		t.Fatalf("Failed to create source schema: %v", err)
	}
	if err := createTestSchema(target); err != nil {
		t.Fatalf("Failed to create target schema: %v", err)
	}
	if err := insertTestData(source); err != nil {
		t.Fatalf("Failed to insert source data: %v", err)
	}

	migrator := NewAutoMigrator(source, target, nil)
	result, err := migrator.DetectAndMigrate(context.Background())

	if err != nil {
		t.Fatalf("DetectAndMigrate failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Migration should succeed, errors: %v", result.Errors)
	}

	// Check that data was migrated
	if result.RowsCopied["domains"] != 2 {
		t.Errorf("Expected 2 domains copied, got %d", result.RowsCopied["domains"])
	}

	if result.RowsCopied["users"] != 3 {
		t.Errorf("Expected 3 users copied, got %d", result.RowsCopied["users"])
	}

	// Verify data in target
	var domainCount int
	if err := target.QueryRow("SELECT COUNT(*) FROM domains").Scan(&domainCount); err != nil {
		t.Fatalf("Failed to count domains: %v", err)
	}
	if domainCount != 2 {
		t.Errorf("Expected 2 domains in target, got %d", domainCount)
	}
}

func TestAutoMigrator_VerifyMigration(t *testing.T) {
	source, cleanupSource := createTestDB(t)
	defer cleanupSource()

	target, cleanupTarget := createTestDB(t)
	defer cleanupTarget()

	// Create matching data
	if err := createTestSchema(source); err != nil {
		t.Fatalf("Failed to create source schema: %v", err)
	}
	if err := createTestSchema(target); err != nil {
		t.Fatalf("Failed to create target schema: %v", err)
	}
	if err := insertTestData(source); err != nil {
		t.Fatalf("Failed to insert source data: %v", err)
	}
	if err := insertTestData(target); err != nil {
		t.Fatalf("Failed to insert target data: %v", err)
	}

	migrator := NewAutoMigrator(source, target, nil)
	result, err := migrator.VerifyMigration(context.Background())

	if err != nil {
		t.Fatalf("VerifyMigration failed: %v", err)
	}

	if !result.Success {
		t.Error("Verification should succeed when data matches")
	}

	if result.HasMismatch {
		t.Error("Should not have mismatch when data is identical")
	}

	// Check table verifications
	if tv, ok := result.Tables["domains"]; ok {
		if !tv.Match {
			t.Errorf("Domains should match: source=%d, target=%d", tv.SourceRows, tv.TargetRows)
		}
	}

	if tv, ok := result.Tables["users"]; ok {
		if !tv.Match {
			t.Errorf("Users should match: source=%d, target=%d", tv.SourceRows, tv.TargetRows)
		}
	}
}

func TestAutoMigrator_VerifyMigration_Mismatch(t *testing.T) {
	source, cleanupSource := createTestDB(t)
	defer cleanupSource()

	target, cleanupTarget := createTestDB(t)
	defer cleanupTarget()

	// Create different data
	if err := createTestSchema(source); err != nil {
		t.Fatalf("Failed to create source schema: %v", err)
	}
	if err := createTestSchema(target); err != nil {
		t.Fatalf("Failed to create target schema: %v", err)
	}
	if err := insertTestData(source); err != nil {
		t.Fatalf("Failed to insert source data: %v", err)
	}
	// Don't insert in target - mismatch

	migrator := NewAutoMigrator(source, target, nil)
	result, err := migrator.VerifyMigration(context.Background())

	if err != nil {
		t.Fatalf("VerifyMigration failed: %v", err)
	}

	if result.Success {
		t.Error("Verification should fail when data mismatches")
	}

	if !result.HasMismatch {
		t.Error("Should have mismatch when data differs")
	}
}

func TestBackupDatabase(t *testing.T) {
	// Create source database with data
	tmpDir, err := os.MkdirTemp("", "backup-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "source.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	if err := createTestSchema(db); err != nil {
		t.Fatalf("Failed to create schema: %v", err)
	}
	if err := insertTestData(db); err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}
	db.Close()

	// Create backup
	backupDir := filepath.Join(tmpDir, "backups")
	backupPath, err := BackupDatabase(dbPath, backupDir)
	if err != nil {
		t.Fatalf("BackupDatabase failed: %v", err)
	}

	// Verify backup exists
	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("Backup file should exist: %v", err)
	}

	// Verify backup contains data
	backupDB, err := sql.Open("sqlite3", backupPath)
	if err != nil {
		t.Fatalf("Failed to open backup: %v", err)
	}
	defer backupDB.Close()

	var count int
	if err := backupDB.QueryRow("SELECT COUNT(*) FROM domains").Scan(&count); err != nil {
		t.Fatalf("Failed to query backup: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 domains in backup, got %d", count)
	}
}

func TestMigrationResult_Duration(t *testing.T) {
	source, cleanupSource := createTestDB(t)
	defer cleanupSource()

	target, cleanupTarget := createTestDB(t)
	defer cleanupTarget()

	if err := createTestSchema(source); err != nil {
		t.Fatalf("Failed to create source schema: %v", err)
	}
	if err := createTestSchema(target); err != nil {
		t.Fatalf("Failed to create target schema: %v", err)
	}

	migrator := NewAutoMigrator(source, target, nil)
	result, err := migrator.DetectAndMigrate(context.Background())

	if err != nil {
		t.Fatalf("DetectAndMigrate failed: %v", err)
	}

	if result.Duration < 0 {
		t.Error("Duration should be non-negative")
	}

	if result.StartTime.IsZero() {
		t.Error("StartTime should be set")
	}

	if result.EndTime.IsZero() {
		t.Error("EndTime should be set")
	}

	if result.EndTime.Before(result.StartTime) {
		t.Error("EndTime should be after StartTime")
	}
}

func TestMigrationResult_ExportToJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "json-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	result := &MigrationResult{
		Success:     true,
		Duration:    5 * time.Second,
		TablesCount: 3,
		RowsCopied: map[string]int64{
			"domains": 2,
			"users":   5,
		},
		StartTime: time.Now().Add(-5 * time.Second),
		EndTime:   time.Now(),
	}

	jsonPath := filepath.Join(tmpDir, "result.json")
	if err := result.ExportToJSON(jsonPath); err != nil {
		t.Fatalf("ExportToJSON failed: %v", err)
	}

	// Verify file exists and is valid JSON
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("Failed to read JSON file: %v", err)
	}

	if len(data) == 0 {
		t.Error("JSON file should not be empty")
	}

	// Should contain expected fields
	jsonStr := string(data)
	if !contains(jsonStr, `"success": true`) && !contains(jsonStr, `"success":true`) {
		t.Error("JSON should contain success field")
	}
}

func TestVerificationResult(t *testing.T) {
	result := &VerificationResult{
		Success:     true,
		HasMismatch: false,
		Tables: map[string]TableVerification{
			"domains": {SourceRows: 10, TargetRows: 10, Match: true},
			"users":   {SourceRows: 100, TargetRows: 100, Match: true},
		},
		Timestamp: time.Now(),
	}

	if !result.Success {
		t.Error("Result should be successful")
	}

	if result.HasMismatch {
		t.Error("Result should not have mismatch")
	}

	if len(result.Tables) != 2 {
		t.Errorf("Expected 2 tables, got %d", len(result.Tables))
	}
}

func TestAutoMigrator_hasData(t *testing.T) {
	db, cleanup := createTestDB(t)
	defer cleanup()

	if err := createTestSchema(db); err != nil {
		t.Fatalf("Failed to create schema: %v", err)
	}

	migrator := NewAutoMigrator(db, db, nil)

	// Empty database
	hasData, err := migrator.hasData(context.Background(), db)
	if err != nil {
		t.Fatalf("hasData failed: %v", err)
	}
	if hasData {
		t.Error("Empty database should not have data")
	}

	// With data
	if err := insertTestData(db); err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	hasData, err = migrator.hasData(context.Background(), db)
	if err != nil {
		t.Fatalf("hasData failed: %v", err)
	}
	if !hasData {
		t.Error("Database with data should return true")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
