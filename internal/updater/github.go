package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
)

// GitHubManager handles GitHub API operations
type GitHubManager struct {
	config *config.UpdaterConfig
	logger *logging.Logger
	client *http.Client
	repo   string
}

// NewGitHubManager creates a new GitHubManager
func NewGitHubManager(cfg *config.UpdaterConfig, logger *logging.Logger) *GitHubManager {
	// Extract repo from URL (https://github.com/owner/repo)
	repo := strings.TrimSuffix(strings.TrimPrefix(cfg.GitRepoURL, "https://github.com/"), ".git")

	return &GitHubManager{
		config: cfg,
		logger: logger,
		client: &http.Client{Timeout: 30 * time.Second},
		repo:   repo,
	}
}

// GetReleases fetches releases from GitHub
func (gm *GitHubManager) GetReleases(ctx context.Context, includePrerelease bool) ([]ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases", gm.repo)
	releases := []ReleaseInfo{}

	resp, err := gm.makeRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api error: %d - %s", resp.StatusCode, string(body))
	}

	var ghReleases []struct {
		TagName     string `json:"tag_name"`
		CommitSHA   string `json:"target_commitish"`
		PublishedAt string `json:"published_at"`
		Prerelease  bool   `json:"prerelease"`
		Body        string `json:"body"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ghReleases); err != nil {
		return nil, fmt.Errorf("failed to decode releases: %w", err)
	}

	for _, rel := range ghReleases {
		if !includePrerelease && rel.Prerelease {
			continue
		}

		pubTime, _ := time.Parse(time.RFC3339, rel.PublishedAt)
		releases = append(releases, ReleaseInfo{
			Version:      rel.TagName,
			CommitSHA:    rel.CommitSHA,
			PublishedAt:  pubTime,
			IsPrerelease: rel.Prerelease,
			Changelog:    rel.Body,
		})
	}

	return releases, nil
}

// GetBranch fetches branch information from GitHub
func (gm *GitHubManager) GetBranch(ctx context.Context, branchName string) (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/branches/%s", gm.repo, branchName)

	resp, err := gm.makeRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api error: %d - %s", resp.StatusCode, string(body))
	}

	var ghBranch struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ghBranch); err != nil {
		return nil, fmt.Errorf("failed to decode branch: %w", err)
	}

	return &ReleaseInfo{
		Version:   branchName,
		CommitSHA: ghBranch.Commit.SHA,
	}, nil
}

// GetPullRequest fetches PR information from GitHub
func (gm *GitHubManager) GetPullRequest(ctx context.Context, prNumber int) (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/pulls/%d", gm.repo, prNumber)

	resp, err := gm.makeRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api error: %d - %s", resp.StatusCode, string(body))
	}

	var ghPR struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Head  struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ghPR); err != nil {
		return nil, fmt.Errorf("failed to decode PR: %w", err)
	}

	return &ReleaseInfo{
		Version:   fmt.Sprintf("PR #%d: %s", prNumber, ghPR.Title),
		CommitSHA: ghPR.Head.SHA,
		Changelog: ghPR.Body,
	}, nil
}

// GetCommit fetches commit information from GitHub
func (gm *GitHubManager) GetCommit(ctx context.Context, commitSHA string) (*ReleaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/commits/%s", gm.repo, commitSHA)

	resp, err := gm.makeRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api error: %d - %s", resp.StatusCode, string(body))
	}

	var ghCommit struct {
		SHA    string `json:"sha"`
		Commit struct {
			Author struct {
				Date string `json:"date"`
			} `json:"author"`
			Message string `json:"message"`
		} `json:"commit"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ghCommit); err != nil {
		return nil, fmt.Errorf("failed to decode commit: %w", err)
	}

	pubTime, _ := time.Parse(time.RFC3339, ghCommit.Commit.Author.Date)
	return &ReleaseInfo{
		Version:    ghCommit.SHA[:8],
		CommitSHA:  ghCommit.SHA,
		PublishedAt: pubTime,
		Changelog:   ghCommit.Commit.Message,
	}, nil
}

// makeRequest makes an HTTP request to the GitHub API
func (gm *GitHubManager) makeRequest(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "email-server-updater/1.0")

	return gm.client.Do(req)
}
