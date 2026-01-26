package updater

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
)

// DeployManager handles binary deployment and service restart
type DeployManager struct {
	config *config.UpdaterConfig
	logger *logging.Logger
}

// NewDeployManager creates a new DeployManager
func NewDeployManager(cfg *config.UpdaterConfig, logger *logging.Logger) *DeployManager {
	return &DeployManager{
		config: cfg,
		logger: logger,
	}
}

// Deploy replaces the existing binary and restarts the service
func (dm *DeployManager) Deploy(ctx context.Context, newBinaryPath string) error {
	targetPath := dm.config.BinaryPath

	// Verify the new binary is valid
	if _, err := os.Stat(newBinaryPath); err != nil {
		return fmt.Errorf("new binary not found: %w", err)
	}

	dm.logger.Info("Starting deployment",
		"new_binary", newBinaryPath,
		"target_path", targetPath)

	// Copy the new binary to the target location
	if err := dm.copyBinary(newBinaryPath, targetPath); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// Make the binary executable
	if err := os.Chmod(targetPath, 0755); err != nil {
		return fmt.Errorf("failed to chmod binary: %w", err)
	}

	// Restart the service
	if err := dm.restartService(ctx); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}

	// Wait a bit for the service to start
	time.Sleep(2 * time.Second)

	// Verify the service is running
	if err := dm.verifyService(ctx); err != nil {
		return fmt.Errorf("service verification failed: %w", err)
	}

	dm.logger.Info("Deployment completed successfully")
	return nil
}

// copyBinary copies the new binary to the target location
func (dm *DeployManager) copyBinary(src, dst string) error {
	// Create a backup of the current binary first
	if _, err := os.Stat(dst); err == nil {
		backupPath := dst + ".backup"
		if err := os.Rename(dst, backupPath); err != nil {
			dm.logger.Warn("Failed to create backup", "error", err)
		}
	}

	// Read the new binary
	srcData, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read source binary: %w", err)
	}

	// Write to the target location
	if err := os.WriteFile(dst, srcData, 0755); err != nil {
		return fmt.Errorf("failed to write target binary: %w", err)
	}

	dm.logger.Info("Binary copied", "src", src, "dst", dst)
	return nil
}

// restartService restarts the systemd service
func (dm *DeployManager) restartService(ctx context.Context) error {
	dm.logger.Info("Restarting service", "service", dm.config.SystemdService)

	cmd := exec.CommandContext(ctx, "systemctl", "restart", dm.config.SystemdService)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl restart failed: %w, output: %s", err, string(output))
	}

	return nil
}

// stopService stops the systemd service
func (dm *DeployManager) stopService(ctx context.Context) error {
	dm.logger.Info("Stopping service", "service", dm.config.SystemdService)

	cmd := exec.CommandContext(ctx, "systemctl", "stop", dm.config.SystemdService)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl stop failed: %w, output: %s", err, string(output))
	}

	return nil
}

// verifyService checks if the service is running
func (dm *DeployManager) verifyService(ctx context.Context) error {
	dm.logger.Info("Verifying service status", "service", dm.config.SystemdService)

	cmd := exec.CommandContext(ctx, "systemctl", "is-active", dm.config.SystemdService)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("service is not active: %w, output: %s", err, string(output))
	}

	return nil
}

// GetServiceStatus returns the current service status
func (dm *DeployManager) GetServiceStatus(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "show", "-p", "ActiveState", "--value", dm.config.SystemdService)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get service status: %w", err)
	}

	return string(output[:len(output)-1]), nil
}

// Rollback restores the backed-up binary and restarts the service
func (dm *DeployManager) Rollback(ctx context.Context) error {
	dm.logger.Warn("Rolling back to previous binary")

	targetPath := dm.config.BinaryPath
	backupPath := targetPath + ".backup"

	// Check if backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup binary not found: %s", backupPath)
	}

	// Stop the service
	if err := dm.stopService(ctx); err != nil {
		dm.logger.Warn("Failed to stop service", "error", err)
	}

	// Restore the backup
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	if err := os.WriteFile(targetPath, backupData, 0755); err != nil {
		return fmt.Errorf("failed to restore binary: %w", err)
	}

	// Restart the service
	if err := dm.restartService(ctx); err != nil {
		return fmt.Errorf("failed to restart service: %w", err)
	}

	dm.logger.Info("Rollback completed successfully")
	return nil
}

// CopyBinaryForBackup creates a copy of the current binary at the specified location
func (dm *DeployManager) CopyBinaryForBackup(ctx context.Context, backupPath string) error {
	sourcePath := dm.config.BinaryPath

	// Ensure backup directory exists
	if err := os.MkdirAll(filepath.Dir(backupPath), 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Read the current binary
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read source binary: %w", err)
	}

	// Write the backup
	if err := os.WriteFile(backupPath, data, 0755); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}

	return nil
}
