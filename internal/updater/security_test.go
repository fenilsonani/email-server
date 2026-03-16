package updater

import (
	"testing"

	"github.com/fenilsonani/email-server/internal/config"
)

func TestValidateUpdaterConfig(t *testing.T) {
	cfg := &config.UpdaterConfig{
		GitRepoURL:     "https://github.com/fenilsonani/email-server",
		BuildPath:      "/tmp/mailserver-build",
		BinaryPath:     "/usr/local/bin/mailserver",
		SystemdService: "mailserver.service",
		MaxBackups:     5,
	}

	if err := validateUpdaterConfig(cfg); err != nil {
		t.Fatalf("validateUpdaterConfig() error = %v", err)
	}
}

func TestValidateUpdaterConfigRejectsRelativePaths(t *testing.T) {
	cfg := &config.UpdaterConfig{
		GitRepoURL:     "https://github.com/fenilsonani/email-server",
		BuildPath:      "tmp/mailserver-build",
		BinaryPath:     "/usr/local/bin/mailserver",
		SystemdService: "mailserver.service",
	}

	if err := validateUpdaterConfig(cfg); err == nil {
		t.Fatal("validateUpdaterConfig() succeeded for relative build path")
	}
}

func TestNormalizeTarget(t *testing.T) {
	tests := []struct {
		name       string
		targetType TargetType
		target     string
		want       string
		wantErr    bool
	}{
		{name: "release", targetType: TargetTypeRelease, target: "v1.2.3", want: "v1.2.3"},
		{name: "branch", targetType: TargetTypeBranch, target: "release/1.2", want: "release/1.2"},
		{name: "commit", targetType: TargetTypeCommit, target: "ABCDEF1234", want: "abcdef1234"},
		{name: "pull request label", targetType: TargetTypePR, target: "PR #42: hardening", want: "42"},
		{name: "reject bad ref", targetType: TargetTypeBranch, target: "main;rm -rf /", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeTarget(tt.targetType, tt.target)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeTarget() succeeded unexpectedly")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeTarget() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}
