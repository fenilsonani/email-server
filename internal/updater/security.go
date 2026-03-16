package updater

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fenilsonani/email-server/internal/config"
)

var (
	validSystemdServiceName = regexp.MustCompile(`^[A-Za-z0-9@_.-]+(?:\.service)?$`)
	validGitRefName         = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	validCommitSHA          = regexp.MustCompile(`^[a-fA-F0-9]{7,40}$`)
	validSSHGitRepo         = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+:[A-Za-z0-9._/-]+(?:\.git)?$`)
	validPRReference        = regexp.MustCompile(`^(?:PR #)?([0-9]+)(?:: .*)?$`)
)

func validateUpdaterConfig(cfg *config.UpdaterConfig) error {
	if cfg == nil {
		return fmt.Errorf("updater config is required")
	}
	if _, err := validateAbsolutePath(cfg.BuildPath, "build path"); err != nil {
		return err
	}
	if _, err := validateAbsolutePath(cfg.BinaryPath, "binary path"); err != nil {
		return err
	}
	if err := validateGitRepoURL(cfg.GitRepoURL); err != nil {
		return err
	}
	if err := validateSystemdServiceName(cfg.SystemdService); err != nil {
		return err
	}
	if cfg.MaxBackups < 0 {
		return fmt.Errorf("max backups cannot be negative")
	}
	return nil
}

func validateAbsolutePath(path, field string) (string, error) {
	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "." || cleanPath == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !filepath.IsAbs(cleanPath) {
		return "", fmt.Errorf("%s must be absolute", field)
	}
	return cleanPath, nil
}

func ensurePathWithinBase(basePath, targetPath, field string) (string, error) {
	basePath = filepath.Clean(basePath)
	targetPath = filepath.Clean(targetPath)

	relPath, err := filepath.Rel(basePath, targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to validate %s: %w", field, err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("%s escapes managed directory", field)
	}
	return targetPath, nil
}

func validateGitRepoURL(repoURL string) error {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return fmt.Errorf("git repo URL is required")
	}
	if validSSHGitRepo.MatchString(repoURL) {
		return nil
	}

	parsed, err := url.Parse(repoURL)
	if err != nil {
		return fmt.Errorf("invalid git repo URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("git repo URL must use https or ssh syntax")
	}
	if parsed.Host == "" || parsed.Path == "" || parsed.Path == "/" {
		return fmt.Errorf("git repo URL must include host and repository path")
	}
	return nil
}

func validateSystemdServiceName(serviceName string) error {
	serviceName = strings.TrimSpace(serviceName)
	if !validSystemdServiceName.MatchString(serviceName) {
		return fmt.Errorf("invalid systemd service name")
	}
	return nil
}

func normalizeTarget(targetType TargetType, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("target is required")
	}

	switch targetType {
	case TargetTypeRelease, TargetTypeBranch:
		if !validGitRefName.MatchString(target) {
			return "", fmt.Errorf("invalid git reference")
		}
		return target, nil
	case TargetTypeCommit:
		if !validCommitSHA.MatchString(target) {
			return "", fmt.Errorf("invalid commit SHA")
		}
		return strings.ToLower(target), nil
	case TargetTypePR:
		match := validPRReference.FindStringSubmatch(target)
		if match == nil {
			return "", fmt.Errorf("invalid pull request reference")
		}
		return match[1], nil
	default:
		return "", fmt.Errorf("unknown target type: %s", targetType)
	}
}
