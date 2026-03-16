package updater

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
)

// BuildManager handles building the Go binary
type BuildManager struct {
	config *config.UpdaterConfig
	logger *logging.Logger
}

// NewBuildManager creates a new BuildManager
func NewBuildManager(cfg *config.UpdaterConfig, logger *logging.Logger) *BuildManager {
	return &BuildManager{
		config: cfg,
		logger: logger,
	}
}

// Build compiles the mailserver binary with version information injected
func (bm *BuildManager) Build(ctx context.Context, commitSHA string) (string, error) {
	if err := validateUpdaterConfig(bm.config); err != nil {
		return "", err
	}
	buildPath := filepath.Clean(bm.config.BuildPath)
	repoPath := filepath.Join(buildPath, "repo")
	outputPath := filepath.Join(buildPath, "mailserver")

	// Get version information
	version, buildTime := bm.getVersionInfo(commitSHA)

	// Prepare ldflags
	ldflags := fmt.Sprintf(
		"-X github.com/fenilsonani/email-server/internal/version.Version=%s -X github.com/fenilsonani/email-server/internal/version.Commit=%s -X github.com/fenilsonani/email-server/internal/version.BuildTime=%s",
		version,
		commitSHA[:8],
		buildTime,
	)

	bm.logger.Info("Building mailserver",
		"repo_path", repoPath,
		"output_path", outputPath,
		"version", version,
		"commit", commitSHA[:8])

	cmd := exec.CommandContext(ctx, "go", "build", // #nosec G204 -- command and working tree are validated managed paths
		"-ldflags", ldflags,
		"-o", outputPath,
		filepath.Join(repoPath, "cmd/mailserver/"),
	)

	// Set working directory to repo
	cmd.Dir = repoPath

	// Capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build failed: %w, output: %s", err, string(output))
	}

	// Verify the binary was created
	if _, err := os.Stat(outputPath); err != nil {
		return "", fmt.Errorf("binary not found after build: %w", err)
	}

	bm.logger.Info("Build successful", "output_path", outputPath)
	return outputPath, nil
}

// Test runs the Go tests
func (bm *BuildManager) Test(ctx context.Context) error {
	if err := validateUpdaterConfig(bm.config); err != nil {
		return err
	}
	repoPath := filepath.Join(filepath.Clean(bm.config.BuildPath), "repo")

	bm.logger.Info("Running tests", "repo_path", repoPath)

	cmd := exec.CommandContext(ctx, "go", "test", "./...") // #nosec G204 -- static command executed in validated managed path
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tests failed: %w, output: %s", err, string(output))
	}

	bm.logger.Info("Tests passed")
	return nil
}

// Vet runs go vet for code quality
func (bm *BuildManager) Vet(ctx context.Context) error {
	if err := validateUpdaterConfig(bm.config); err != nil {
		return err
	}
	repoPath := filepath.Join(filepath.Clean(bm.config.BuildPath), "repo")

	bm.logger.Info("Running go vet", "repo_path", repoPath)

	cmd := exec.CommandContext(ctx, "go", "vet", "./...") // #nosec G204 -- static command executed in validated managed path
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("vet failed: %w, output: %s", err, string(output))
	}

	bm.logger.Info("Vet passed")
	return nil
}

// Cleanup removes temporary build files
func (bm *BuildManager) Cleanup(ctx context.Context) {
	if err := validateUpdaterConfig(bm.config); err != nil {
		bm.logger.Warn("Skipping cleanup due to invalid updater config", "error", err)
		return
	}
	buildPath := filepath.Clean(bm.config.BuildPath)
	bm.logger.Info("Cleaning up build files", "path", buildPath)

	// Keep the binary but remove the repo directory
	repoPath := filepath.Join(buildPath, "repo")
	if err := os.RemoveAll(repoPath); err != nil {
		bm.logger.Warn("Failed to clean up repo", "path", repoPath, "error", err)
	}
}

// getVersionInfo generates version and build time strings
func (bm *BuildManager) getVersionInfo(commitSHA string) (string, string) {
	// Try to get tag from commit
	// For now, use commit SHA short form
	version := commitSHA[:8]

	// Get current time in RFC3339 format
	buildTime := time.Now().UTC().Format(time.RFC3339)

	return version, buildTime
}

// VerifyBinary checks if the compiled binary is valid
func (bm *BuildManager) VerifyBinary(ctx context.Context, binPath string) error {
	if _, err := validateAbsolutePath(binPath, "binary path"); err != nil {
		return err
	}
	// Check if file exists and is executable
	info, err := os.Stat(binPath)
	if err != nil {
		return fmt.Errorf("binary stat failed: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("binary path is a directory")
	}

	// Try to run version command
	cmd := exec.CommandContext(ctx, binPath, "version") // #nosec G204 -- binary path validated as absolute executable path
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("binary verification failed: %w, output: %s", err, string(output))
	}

	bm.logger.Info("Binary verified successfully", "output", strings.TrimSpace(string(output)))
	return nil
}
