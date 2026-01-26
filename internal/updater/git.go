package updater

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
)

// GitManager handles Git operations
type GitManager struct {
	config *config.UpdaterConfig
	logger *logging.Logger
}

// NewGitManager creates a new GitManager
func NewGitManager(cfg *config.UpdaterConfig, logger *logging.Logger) *GitManager {
	return &GitManager{
		config: cfg,
		logger: logger,
	}
}

// Fetch performs Git operations to fetch a specific target
func (gm *GitManager) Fetch(ctx context.Context, targetType TargetType, target string) (string, error) {
	buildPath := gm.config.BuildPath

	// Ensure build path exists
	if err := os.MkdirAll(buildPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create build path: %w", err)
	}

	repoPath := filepath.Join(buildPath, "repo")

	// Check if repo already exists
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		// Clone the repository
		if err := gm.cloneRepository(ctx, repoPath); err != nil {
			return "", fmt.Errorf("failed to clone repository: %w", err)
		}
	} else {
		// Update existing repository
		if err := gm.pullRepository(ctx, repoPath); err != nil {
			gm.logger.Warn("Failed to pull repository, will continue anyway", "error", err)
		}
	}

	// Get the commit SHA for the target
	commitSHA, err := gm.checkoutTarget(ctx, repoPath, targetType, target)
	if err != nil {
		return "", fmt.Errorf("failed to checkout target: %w", err)
	}

	gm.logger.Info("Successfully fetched target",
		"target_type", targetType,
		"target", target,
		"commit_sha", commitSHA)

	return commitSHA, nil
}

// cloneRepository clones the repository
func (gm *GitManager) cloneRepository(ctx context.Context, repoPath string) error {
	gm.logger.Info("Cloning repository", "repo_url", gm.config.GitRepoURL, "path", repoPath)

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", gm.config.GitRepoURL, repoPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w, output: %s", err, string(output))
	}

	return nil
}

// pullRepository updates the repository
func (gm *GitManager) pullRepository(ctx context.Context, repoPath string) error {
	gm.logger.Info("Pulling latest changes", "repo_path", repoPath)

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "fetch", "origin")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %w, output: %s", err, string(output))
	}

	return nil
}

// checkoutTarget checks out the specified target
func (gm *GitManager) checkoutTarget(ctx context.Context, repoPath string, targetType TargetType, target string) (string, error) {
	var checkoutRef string

	switch targetType {
	case TargetTypeRelease:
		// For releases, check out by tag
		checkoutRef = target
	case TargetTypeBranch:
		// For branches, check out the remote branch
		checkoutRef = "origin/" + target
	case TargetTypeCommit:
		// For commits, check out by SHA
		checkoutRef = target
	case TargetTypePR:
		// For PRs, fetch the PR branch
		// Format: PR #123 - we need to extract the number
		prNum := target
		if strings.Contains(target, "#") {
			prNum = strings.TrimPrefix(target, "PR #")
			prNum = strings.TrimSuffix(prNum, ": "+strings.Join(strings.Split(target, ": ")[1:], ": "))
		}
		checkoutRef = fmt.Sprintf("refs/pull/%s/head", prNum)
	default:
		return "", fmt.Errorf("unknown target type: %s", targetType)
	}

	gm.logger.Info("Checking out target", "ref", checkoutRef)

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "checkout", checkoutRef)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git checkout failed: %w, output: %s", err, string(output))
	}

	// Get the current commit SHA
	commitSHA, err := gm.getHeadSHA(ctx, repoPath)
	if err != nil {
		return "", fmt.Errorf("failed to get commit SHA: %w", err)
	}

	return commitSHA, nil
}

// getHeadSHA returns the current HEAD commit SHA
func (gm *GitManager) getHeadSHA(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetCurrentBranch returns the current git branch
func (gm *GitManager) GetCurrentBranch(ctx context.Context) (string, error) {
	repoPath := filepath.Join(gm.config.BuildPath, "repo")
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// Clean removes the build directory
func (gm *GitManager) Clean(ctx context.Context) error {
	buildPath := gm.config.BuildPath
	if _, err := os.Stat(buildPath); os.IsNotExist(err) {
		return nil
	}

	gm.logger.Info("Cleaning up build path", "path", buildPath)
	return os.RemoveAll(buildPath)
}
