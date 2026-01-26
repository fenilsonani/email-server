package updater

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/version"
)

// VersionInfo contains version and commit information
type VersionInfo struct {
	Version   string
	Commit    string
	BuildTime string
	GoVersion string
}

// ReleaseInfo contains information about a GitHub release
type ReleaseInfo struct {
	Version     string
	CommitSHA   string
	PublishedAt time.Time
	IsPrerelease bool
	Changelog   string
}

// VersionManager handles version detection and comparison
type VersionManager struct {
	db     *sql.DB
	config *config.UpdaterConfig
	logger *logging.Logger
	github *GitHubManager
}

// NewVersionManager creates a new VersionManager
func NewVersionManager(db *sql.DB, cfg *config.UpdaterConfig, logger *logging.Logger) *VersionManager {
	return &VersionManager{
		db:     db,
		config: cfg,
		logger: logger,
		github: NewGitHubManager(cfg, logger),
	}
}

// GetCurrentVersion returns the currently running version
func (vm *VersionManager) GetCurrentVersion(ctx context.Context) (*VersionInfo, error) {
	info := version.Get()
	return &VersionInfo{
		Version:   info.Version,
		Commit:    info.Commit,
		BuildTime: info.BuildTime,
		GoVersion: info.GoVersion,
	}, nil
}

// GetAvailableReleases fetches available releases from GitHub
func (vm *VersionManager) GetAvailableReleases(ctx context.Context, includePrerelease bool) ([]ReleaseInfo, error) {
	releases, err := vm.github.GetReleases(ctx, includePrerelease)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases: %w", err)
	}
	return releases, nil
}

// GetLatestRelease returns the latest stable release
func (vm *VersionManager) GetLatestRelease(ctx context.Context) (*ReleaseInfo, error) {
	releases, err := vm.GetAvailableReleases(ctx, false)
	if err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases available")
	}
	return &releases[0], nil
}

// CheckForUpdates checks if a new version is available
func (vm *VersionManager) CheckForUpdates(ctx context.Context) (*ReleaseInfo, error) {
	currentVersion, err := vm.GetCurrentVersion(ctx)
	if err != nil {
		return nil, err
	}

	latest, err := vm.GetLatestRelease(ctx)
	if err != nil {
		return nil, err
	}

	// Compare versions (simple string comparison for now)
	// In production, would use semver comparison
	if latest.CommitSHA != currentVersion.Commit {
		return latest, nil
	}

	return nil, nil
}

// GetBranch fetches information about a specific Git branch
func (vm *VersionManager) GetBranch(ctx context.Context, branchName string) (*ReleaseInfo, error) {
	branch, err := vm.github.GetBranch(ctx, branchName)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch branch: %w", err)
	}
	return branch, nil
}

// GetPullRequest fetches information about a specific PR
func (vm *VersionManager) GetPullRequest(ctx context.Context, prNumber int) (*ReleaseInfo, error) {
	pr, err := vm.github.GetPullRequest(ctx, prNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pull request: %w", err)
	}
	return pr, nil
}

// GetCommit fetches information about a specific commit
func (vm *VersionManager) GetCommit(ctx context.Context, commitSHA string) (*ReleaseInfo, error) {
	commit, err := vm.github.GetCommit(ctx, commitSHA)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch commit: %w", err)
	}
	return commit, nil
}

// CacheRelease caches release information in the database
func (vm *VersionManager) CacheRelease(ctx context.Context, versionType, versionName string, info *ReleaseInfo) error {
	query := `
	INSERT OR REPLACE INTO version_cache (version_type, version_name, commit_sha, published_at, is_prerelease, changelog, cached_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	_, err := vm.db.ExecContext(ctx, query,
		versionType,
		versionName,
		info.CommitSHA,
		info.PublishedAt,
		info.IsPrerelease,
		info.Changelog,
		time.Now(),
	)
	return err
}

// GetCachedRelease retrieves cached release information
func (vm *VersionManager) GetCachedRelease(ctx context.Context, versionType, versionName string) (*ReleaseInfo, error) {
	query := `
	SELECT commit_sha, published_at, is_prerelease, changelog
	FROM version_cache
	WHERE version_type = ? AND version_name = ?
	LIMIT 1
	`
	var commitSHA string
	var publishedAt sql.NullTime
	var isPrerelease bool
	var changelog sql.NullString

	err := vm.db.QueryRowContext(ctx, query, versionType, versionName).Scan(
		&commitSHA, &publishedAt, &isPrerelease, &changelog,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &ReleaseInfo{
		Version:      versionName,
		CommitSHA:    commitSHA,
		PublishedAt:  publishedAt.Time,
		IsPrerelease: isPrerelease,
		Changelog:    changelog.String,
	}, nil
}
