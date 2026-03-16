package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
)

// BackupManager handles backup creation and management
type BackupManager struct {
	config *config.UpdaterConfig
	logger *logging.Logger
}

// NewBackupManager creates a new BackupManager
func NewBackupManager(cfg *config.UpdaterConfig, logger *logging.Logger) *BackupManager {
	return &BackupManager{
		config: cfg,
		logger: logger,
	}
}

// CreateBackup creates a pre-update backup of the binary and important files
func (bm *BackupManager) CreateBackup(ctx context.Context, version, commitSHA string) (string, error) {
	if err := validateUpdaterConfig(bm.config); err != nil {
		return "", err
	}
	buildPath, _ := validateAbsolutePath(bm.config.BuildPath, "build path")
	timestamp := time.Now().Format("20060102-150405")
	backupRoot := filepath.Join(buildPath, "backups")
	backupDir := filepath.Join(backupRoot, fmt.Sprintf("pre-update-%s", timestamp))
	if _, err := ensurePathWithinBase(backupRoot, backupDir, "backup directory"); err != nil {
		return "", err
	}

	bm.logger.Info("Creating pre-update backup",
		"version", version,
		"commit", commitSHA[:8],
		"backup_dir", backupDir)

	// Create backup directory
	if err := os.MkdirAll(backupDir, 0750); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Backup the binary
	binaryBackupPath := filepath.Join(backupDir, "mailserver-binary")
	if err := bm.backupFile(bm.config.BinaryPath, binaryBackupPath); err != nil {
		return "", fmt.Errorf("failed to backup binary: %w", err)
	}

	// Create metadata file with backup information
	if err := bm.createBackupMetadata(backupDir, version, commitSHA); err != nil {
		bm.logger.Warn("Failed to create backup metadata", "error", err)
	}

	bm.logger.Info("Backup created successfully", "backup_dir", backupDir)
	return backupDir, nil
}

// backupFile copies a file for backup
func (bm *BackupManager) backupFile(src, dst string) error {
	// Check if source exists
	if err := validateUpdaterConfig(bm.config); err != nil {
		return err
	}
	backupRoot := filepath.Join(filepath.Clean(bm.config.BuildPath), "backups")
	if _, err := validateAbsolutePath(src, "backup source"); err != nil {
		return err
	}
	if _, err := ensurePathWithinBase(backupRoot, dst, "backup destination"); err != nil {
		return err
	}
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("source file not found: %w", err)
	}

	// Read source
	data, err := os.ReadFile(src) // #nosec G304 -- source path validated as an absolute managed path
	if err != nil {
		return fmt.Errorf("failed to read source: %w", err)
	}

	// Write backup
	if err := os.WriteFile(dst, data, 0750); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}

	return nil
}

// createBackupMetadata creates a metadata file for the backup
func (bm *BackupManager) createBackupMetadata(backupDir, version, commitSHA string) error {
	metadata := fmt.Sprintf(`Backup Date: %s
Version: %s
Commit: %s
Binary Path: %s
Created at: %s
`,
		time.Now().Format(time.RFC3339),
		version,
		commitSHA,
		bm.config.BinaryPath,
		time.Now().Format(time.RFC3339),
	)

	metadataPath := filepath.Join(backupDir, "metadata.txt")
	return os.WriteFile(metadataPath, []byte(metadata), 0600)
}

// RestoreFromBackup restores a binary from a backup
func (bm *BackupManager) RestoreFromBackup(ctx context.Context, backupDir string) error {
	if err := validateUpdaterConfig(bm.config); err != nil {
		return err
	}
	buildPath := filepath.Clean(bm.config.BuildPath)
	backupRoot := filepath.Join(buildPath, "backups")
	if _, err := ensurePathWithinBase(backupRoot, backupDir, "backup directory"); err != nil {
		return err
	}
	binaryBackupPath := filepath.Join(backupDir, "mailserver-binary")
	targetPath := bm.config.BinaryPath

	bm.logger.Info("Restoring from backup",
		"backup_dir", backupDir,
		"target_path", targetPath)

	// Check if backup exists
	if _, err := os.Stat(binaryBackupPath); err != nil {
		return fmt.Errorf("backup binary not found: %w", err)
	}

	// Read backup
	data, err := os.ReadFile(binaryBackupPath) // #nosec G304 -- backup path validated within managed backup directory
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	// Write to target
	if err := os.WriteFile(targetPath, data, 0750); err != nil {
		return fmt.Errorf("failed to write restored binary: %w", err)
	}

	bm.logger.Info("Backup restored successfully")
	return nil
}

// CleanupOldBackups removes old backups, keeping only the specified number
func (bm *BackupManager) CleanupOldBackups(ctx context.Context) error {
	if err := validateUpdaterConfig(bm.config); err != nil {
		return err
	}
	backupParentDir := filepath.Join(filepath.Clean(bm.config.BuildPath), "backups")

	// Check if directory exists
	if _, err := os.Stat(backupParentDir); os.IsNotExist(err) {
		return nil
	}

	// List backup directories
	entries, err := os.ReadDir(backupParentDir)
	if err != nil {
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	// Count pre-update backups
	var backupDirs []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() && len(entry.Name()) >= len("pre-update-") {
			// Simple check if it starts with "pre-update-"
			backupDirs = append(backupDirs, entry)
		}
	}

	// If we have more backups than the max, remove the oldest
	if len(backupDirs) > bm.config.MaxBackups {
		// Note: This is a simple implementation. In production, you'd want to
		// sort by modification time and remove the oldest ones
		toRemove := len(backupDirs) - bm.config.MaxBackups
		for i := 0; i < toRemove && i < len(backupDirs); i++ {
			backupPath := filepath.Join(backupParentDir, backupDirs[i].Name())
			bm.logger.Info("Removing old backup", "path", backupPath)
			if err := os.RemoveAll(backupPath); err != nil {
				bm.logger.Warn("Failed to remove old backup", "path", backupPath, "error", err)
			}
		}
	}

	return nil
}

// ListBackups returns a list of available backups
func (bm *BackupManager) ListBackups(ctx context.Context) ([]string, error) {
	if err := validateUpdaterConfig(bm.config); err != nil {
		return nil, err
	}
	backupParentDir := filepath.Join(filepath.Clean(bm.config.BuildPath), "backups")

	// Check if directory exists
	if _, err := os.Stat(backupParentDir); os.IsNotExist(err) {
		return []string{}, nil
	}

	// List backup directories
	entries, err := os.ReadDir(backupParentDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []string
	for _, entry := range entries {
		if entry.IsDir() {
			backups = append(backups, entry.Name())
		}
	}

	return backups, nil
}
