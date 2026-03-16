package setup

import "testing"

func TestValidateSetupConfig_NormalizesDomainAndHostname(t *testing.T) {
	cfg := &SetupConfig{
		Domain:     " Example.COM ",
		Hostname:   " Mail.Example.COM ",
		AdminEmail: "admin@example.com",
		TLSEmail:   "tls@example.com",
		DataDir:    "/var/lib/mailserver",
		ConfigDir:  "/etc/mailserver",
	}

	if err := validateSetupConfig(cfg); err != nil {
		t.Fatalf("validateSetupConfig() error = %v", err)
	}

	if cfg.Domain != "example.com" {
		t.Fatalf("cfg.Domain = %q, want %q", cfg.Domain, "example.com")
	}
	if cfg.Hostname != "mail.example.com" {
		t.Fatalf("cfg.Hostname = %q, want %q", cfg.Hostname, "mail.example.com")
	}
}
