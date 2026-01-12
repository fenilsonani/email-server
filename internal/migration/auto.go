// Package migration provides automatic database migration tools.
package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AutoMigrator handles automatic database migrations.
// It detects existing data and migrates automatically when switching databases.
type AutoMigrator struct {
	sourceDB *sql.DB
	targetDB *sql.DB
	logger   Logger
}

// Logger interface for migration logging.
type Logger interface {
	Info(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Error(msg string, args ...interface{})
}

// DefaultLogger implements Logger using fmt.
type DefaultLogger struct{}

func (l DefaultLogger) Info(msg string, args ...interface{})  { fmt.Printf("[INFO] "+msg+"\n", args...) }
func (l DefaultLogger) Warn(msg string, args ...interface{})  { fmt.Printf("[WARN] "+msg+"\n", args...) }
func (l DefaultLogger) Error(msg string, args ...interface{}) { fmt.Printf("[ERROR] "+msg+"\n", args...) }

// MigrationResult holds the result of a migration.
type MigrationResult struct {
	Success     bool              `json:"success"`
	Duration    time.Duration     `json:"duration"`
	TablesCount int               `json:"tables_count"`
	RowsCopied  map[string]int64  `json:"rows_copied"`
	Errors      []string          `json:"errors,omitempty"`
	StartTime   time.Time         `json:"start_time"`
	EndTime     time.Time         `json:"end_time"`
}

// NewAutoMigrator creates a new auto migrator.
func NewAutoMigrator(source, target *sql.DB, logger Logger) *AutoMigrator {
	if logger == nil {
		logger = DefaultLogger{}
	}
	return &AutoMigrator{
		sourceDB: source,
		targetDB: target,
		logger:   logger,
	}
}

// DetectAndMigrate automatically detects if migration is needed and performs it.
// This is safe to call on every startup - it only migrates if needed.
func (m *AutoMigrator) DetectAndMigrate(ctx context.Context) (*MigrationResult, error) {
	result := &MigrationResult{
		StartTime:  time.Now(),
		RowsCopied: make(map[string]int64),
	}

	// Check if target already has data
	targetHasData, err := m.hasData(ctx, m.targetDB)
	if err != nil {
		return nil, fmt.Errorf("failed to check target database: %w", err)
	}

	if targetHasData {
		m.logger.Info("Target database already has data, skipping migration")
		result.Success = true
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, nil
	}

	// Check if source has data to migrate
	sourceHasData, err := m.hasData(ctx, m.sourceDB)
	if err != nil {
		return nil, fmt.Errorf("failed to check source database: %w", err)
	}

	if !sourceHasData {
		m.logger.Info("Source database is empty, no migration needed")
		result.Success = true
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		return result, nil
	}

	// Perform migration
	m.logger.Info("Starting automatic data migration...")
	return m.migrate(ctx, result)
}

// migrate performs the actual migration.
func (m *AutoMigrator) migrate(ctx context.Context, result *MigrationResult) (*MigrationResult, error) {
	// Tables to migrate in order (respecting foreign keys)
	tables := []string{
		"domains",
		"users",
		"aliases",
		"mailboxes",
		"messages",
		"subscriptions",
		"sessions",
		"calendars",
		"calendar_events",
		"addressbooks",
		"contacts",
		"sieve_scripts",
		"vacation_responses",
		"auth_log",
		"delivery_log",
		"api_keys",
		"email_templates",
		"sent_emails",
		"webhooks",
		"webhook_deliveries",
		"totp_trusted_devices",
		"outbound_queue",
	}

	result.TablesCount = len(tables)

	for _, table := range tables {
		count, err := m.migrateTable(ctx, table)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", table, err))
			m.logger.Warn("Failed to migrate table %s: %v", table, err)
			continue
		}
		result.RowsCopied[table] = count
		if count > 0 {
			m.logger.Info("Migrated %d rows from %s", count, table)
		}
	}

	result.Success = len(result.Errors) == 0
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	if result.Success {
		m.logger.Info("Migration completed successfully in %v", result.Duration)
	} else {
		m.logger.Warn("Migration completed with %d errors in %v", len(result.Errors), result.Duration)
	}

	return result, nil
}

// migrateTable migrates a single table.
func (m *AutoMigrator) migrateTable(ctx context.Context, table string) (int64, error) {
	// Check if source table exists and has data
	var count int64
	err := m.sourceDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
	if err != nil {
		// Table doesn't exist, skip
		return 0, nil
	}

	if count == 0 {
		return 0, nil
	}

	// Get column names
	columns, err := m.getColumns(ctx, m.sourceDB, table)
	if err != nil {
		return 0, fmt.Errorf("failed to get columns: %w", err)
	}

	if len(columns) == 0 {
		return 0, nil
	}

	// Read all data
	rows, err := m.sourceDB.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s", table))
	if err != nil {
		return 0, fmt.Errorf("failed to query source: %w", err)
	}
	defer rows.Close()

	// Prepare insert statement
	insertSQL := m.buildInsertSQL(table, columns)

	// Begin transaction
	tx, err := m.targetDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, insertSQL)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// Copy rows
	var copied int64
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return copied, fmt.Errorf("failed to scan row: %w", err)
		}

		_, err := stmt.ExecContext(ctx, values...)
		if err != nil {
			// Skip duplicates (may happen if partial migration)
			continue
		}
		copied++
	}

	if err := rows.Err(); err != nil {
		return copied, fmt.Errorf("row iteration error: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return copied, fmt.Errorf("failed to commit: %w", err)
	}

	return copied, nil
}

func (m *AutoMigrator) hasData(ctx context.Context, db *sql.DB) (bool, error) {
	// Check if any core tables have data
	tables := []string{"domains", "users"}

	for _, table := range tables {
		var count int64
		err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
		if err != nil {
			// Table doesn't exist yet
			continue
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (m *AutoMigrator) getColumns(ctx context.Context, db *sql.DB, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s LIMIT 0", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return rows.Columns()
}

func (m *AutoMigrator) buildInsertSQL(table string, columns []string) string {
	placeholders := ""
	columnList := ""
	for i, col := range columns {
		if i > 0 {
			placeholders += ", "
			columnList += ", "
		}
		columnList += col
		placeholders += fmt.Sprintf("$%d", i+1) // PostgreSQL placeholders
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING", table, columnList, placeholders)
}

// BackupDatabase creates a backup of the database.
func BackupDatabase(dbPath, backupDir string) (string, error) {
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("backup-%s.db", timestamp))

	// Read source
	data, err := os.ReadFile(dbPath)
	if err != nil {
		return "", fmt.Errorf("failed to read database: %w", err)
	}

	// Write backup
	if err := os.WriteFile(backupPath, data, 0640); err != nil {
		return "", fmt.Errorf("failed to write backup: %w", err)
	}

	return backupPath, nil
}

// VerifyMigration verifies data integrity after migration.
func (m *AutoMigrator) VerifyMigration(ctx context.Context) (*VerificationResult, error) {
	result := &VerificationResult{
		Tables:    make(map[string]TableVerification),
		Timestamp: time.Now(),
	}

	tables := []string{"domains", "users", "mailboxes", "messages"}

	for _, table := range tables {
		var sourceCount, targetCount int64

		err := m.sourceDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&sourceCount)
		if err != nil {
			sourceCount = -1 // Table doesn't exist
		}

		err = m.targetDB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&targetCount)
		if err != nil {
			targetCount = -1 // Table doesn't exist
		}

		result.Tables[table] = TableVerification{
			SourceRows: sourceCount,
			TargetRows: targetCount,
			Match:      sourceCount == targetCount,
		}

		if sourceCount != targetCount && sourceCount > 0 {
			result.HasMismatch = true
		}
	}

	result.Success = !result.HasMismatch
	return result, nil
}

// VerificationResult holds migration verification results.
type VerificationResult struct {
	Success     bool                        `json:"success"`
	HasMismatch bool                        `json:"has_mismatch"`
	Tables      map[string]TableVerification `json:"tables"`
	Timestamp   time.Time                   `json:"timestamp"`
}

// TableVerification holds verification for a single table.
type TableVerification struct {
	SourceRows int64 `json:"source_rows"`
	TargetRows int64 `json:"target_rows"`
	Match      bool  `json:"match"`
}

// ExportToJSON exports migration result to a JSON file.
func (r *MigrationResult) ExportToJSON(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0640)
}
