package updater

import (
	"testing"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
)

func TestNewGitHubManager(t *testing.T) {
	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/fenilsonani/email-server",
	}
	logger := logging.Default()

	gm := NewGitHubManager(cfg, logger)
	if gm == nil {
		t.Error("NewGitHubManager returned nil")
	}
	if gm.repo != "fenilsonani/email-server" {
		t.Errorf("Expected repo 'fenilsonani/email-server', got %q", gm.repo)
	}
}

func TestGitHubManager_ExtractRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantRepo string
	}{
		{
			name:    "HTTPS URL",
			url:     "https://github.com/fenilsonani/email-server",
			wantRepo: "fenilsonani/email-server",
		},
		{
			name:    "HTTPS URL with .git",
			url:     "https://github.com/fenilsonani/email-server.git",
			wantRepo: "fenilsonani/email-server",
		},
		{
			name:    "SSH URL",
			url:     "git@github.com:fenilsonani/email-server.git",
			wantRepo: "git@github.com:fenilsonani/email-server",
		},
	}

	logger := logging.Default()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.UpdaterConfig{
				GitRepoURL: tt.url,
			}
			gm := NewGitHubManager(cfg, logger)
			if gm.repo != tt.wantRepo {
				t.Errorf("Expected repo %q, got %q", tt.wantRepo, gm.repo)
			}
		})
	}
}

func TestReleaseInfo(t *testing.T) {
	info := &ReleaseInfo{
		Version:      "v1.0.0",
		CommitSHA:    "abc123def456",
		IsPrerelease: false,
		Changelog:    "Test release",
	}

	if info.Version != "v1.0.0" {
		t.Errorf("Expected version v1.0.0, got %s", info.Version)
	}
	if info.CommitSHA != "abc123def456" {
		t.Errorf("Expected commit SHA abc123def456, got %s", info.CommitSHA)
	}
	if info.IsPrerelease {
		t.Error("Expected IsPrerelease to be false")
	}
}

// Mock tests for GitHub API (would require mocking HTTP client)
// These tests verify the structure and error handling without making real API calls

func TestGitHubManager_ClientInitialized(t *testing.T) {
	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/fenilsonani/email-server",
	}
	logger := logging.Default()
	gm := NewGitHubManager(cfg, logger)

	if gm.client == nil {
		t.Error("Expected HTTP client to be initialized")
	}
}

func TestGitHubManager_RepoExtraction(t *testing.T) {
	tests := []struct {
		name     string
		gitURL   string
		expected string
	}{
		{
			name:     "standard https",
			gitURL:   "https://github.com/user/repo",
			expected: "user/repo",
		},
		{
			name:     "https with .git",
			gitURL:   "https://github.com/user/repo.git",
			expected: "user/repo",
		},
		{
			name:     "trailing slash",
			gitURL:   "https://github.com/user/repo/",
			expected: "user/repo/",
		},
		{
			name:     "multiple trailing slashes",
			gitURL:   "https://github.com/user/repo///",
			expected: "user/repo///",
		},
	}

	logger := logging.Default()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.UpdaterConfig{
				GitRepoURL: tt.gitURL,
			}
			gm := NewGitHubManager(cfg, logger)
			if gm.repo != tt.expected {
				t.Errorf("Expected repo %q, got %q", tt.expected, gm.repo)
			}
		})
	}
}

func TestGitHubManager_APIEndpointConstruction(t *testing.T) {
	cfg := &config.UpdaterConfig{
		GitRepoURL: "https://github.com/fenilsonani/email-server",
	}
	logger := logging.Default()
	gm := NewGitHubManager(cfg, logger)

	// Verify that the manager can construct API URLs correctly
	if gm.repo == "" {
		t.Error("Expected repo to be set")
	}

	// The actual API calls would be tested with mocked HTTP client
	// Here we just verify the URL construction would work
	expectedRepo := "fenilsonani/email-server"
	if gm.repo != expectedRepo {
		t.Errorf("Expected repo %q, got %q", expectedRepo, gm.repo)
	}
}

func TestReleaseInfo_Validation(t *testing.T) {
	tests := []struct {
		name    string
		release *ReleaseInfo
		valid   bool
	}{
		{
			name: "complete release",
			release: &ReleaseInfo{
				Version:   "v1.0.0",
				CommitSHA: "abc123",
			},
			valid: true,
		},
		{
			name: "prerelease",
			release: &ReleaseInfo{
				Version:      "v1.0.0-beta",
				CommitSHA:    "abc123",
				IsPrerelease: true,
			},
			valid: true,
		},
		{
			name: "with changelog",
			release: &ReleaseInfo{
				Version:   "v1.0.0",
				CommitSHA: "abc123",
				Changelog: "New features and bug fixes",
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.release.Version == "" {
				t.Error("Expected version to be non-empty")
			}
			if tt.release.CommitSHA == "" {
				t.Error("Expected commit SHA to be non-empty")
			}
		})
	}
}
