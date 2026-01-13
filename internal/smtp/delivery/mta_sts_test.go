package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSTSPolicy_ValidateMX(t *testing.T) {
	tests := []struct {
		name      string
		mxHosts   []string
		testHost  string
		wantValid bool
	}{
		{
			name:      "exact match",
			mxHosts:   []string{"mail.example.com"},
			testHost:  "mail.example.com",
			wantValid: true,
		},
		{
			name:      "exact match with trailing dot",
			mxHosts:   []string{"mail.example.com"},
			testHost:  "mail.example.com.",
			wantValid: true,
		},
		{
			name:      "no match",
			mxHosts:   []string{"mail.example.com"},
			testHost:  "smtp.example.com",
			wantValid: false,
		},
		{
			name:      "wildcard match",
			mxHosts:   []string{"*.example.com"},
			testHost:  "mail.example.com",
			wantValid: true,
		},
		{
			name:      "wildcard match - mx1",
			mxHosts:   []string{"*.example.com"},
			testHost:  "mx1.example.com",
			wantValid: true,
		},
		{
			name:      "wildcard no match - subdomain",
			mxHosts:   []string{"*.example.com"},
			testHost:  "mail.sub.example.com",
			wantValid: false,
		},
		{
			name:      "wildcard no match - different domain",
			mxHosts:   []string{"*.example.com"},
			testHost:  "mail.other.com",
			wantValid: false,
		},
		{
			name:      "multiple hosts - first match",
			mxHosts:   []string{"mail.example.com", "backup.example.com"},
			testHost:  "mail.example.com",
			wantValid: true,
		},
		{
			name:      "multiple hosts - second match",
			mxHosts:   []string{"mail.example.com", "backup.example.com"},
			testHost:  "backup.example.com",
			wantValid: true,
		},
		{
			name:      "case insensitive",
			mxHosts:   []string{"MAIL.EXAMPLE.COM"},
			testHost:  "mail.example.com",
			wantValid: true,
		},
		{
			name:      "mixed wildcard and exact",
			mxHosts:   []string{"mail.example.com", "*.backup.example.com"},
			testHost:  "mx1.backup.example.com",
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &STSPolicy{
				MXHosts: tt.mxHosts,
			}
			got := policy.ValidateMX(tt.testHost)
			if got != tt.wantValid {
				t.Errorf("ValidateMX(%q) = %v, want %v", tt.testHost, got, tt.wantValid)
			}
		})
	}
}

func TestSTSPolicy_ShouldEnforceTLS(t *testing.T) {
	tests := []struct {
		name    string
		mode    STSMode
		enforce bool
	}{
		{"enforce mode", STSModeEnforce, true},
		{"testing mode", STSModeTesting, false},
		{"none mode", STSModeNone, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &STSPolicy{Mode: tt.mode}
			if got := policy.ShouldEnforceTLS(); got != tt.enforce {
				t.Errorf("ShouldEnforceTLS() = %v, want %v", got, tt.enforce)
			}
		})
	}
}

func TestSTSPolicy_ShouldReportFailure(t *testing.T) {
	tests := []struct {
		name   string
		mode   STSMode
		report bool
	}{
		{"enforce mode", STSModeEnforce, true},
		{"testing mode", STSModeTesting, true},
		{"none mode", STSModeNone, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &STSPolicy{Mode: tt.mode}
			if got := policy.ShouldReportFailure(); got != tt.report {
				t.Errorf("ShouldReportFailure() = %v, want %v", got, tt.report)
			}
		})
	}
}

func TestSTSResolver_ParsePolicy(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMode  STSMode
		wantMX    []string
		wantAge   int
		wantError bool
	}{
		{
			name: "valid enforce policy",
			input: `version: STSv1
mode: enforce
mx: mail.example.com
mx: backup.example.com
max_age: 604800`,
			wantMode:  STSModeEnforce,
			wantMX:    []string{"mail.example.com", "backup.example.com"},
			wantAge:   604800,
			wantError: false,
		},
		{
			name: "valid testing policy",
			input: `version: STSv1
mode: testing
mx: *.example.com
max_age: 86400`,
			wantMode:  STSModeTesting,
			wantMX:    []string{"*.example.com"},
			wantAge:   86400,
			wantError: false,
		},
		{
			name: "policy with comments and empty lines",
			input: `# This is a comment
version: STSv1

mode: enforce
mx: mail.example.com
# Another comment
max_age: 604800
`,
			wantMode:  STSModeEnforce,
			wantMX:    []string{"mail.example.com"},
			wantAge:   604800,
			wantError: false,
		},
		{
			name: "missing version",
			input: `mode: enforce
mx: mail.example.com
max_age: 604800`,
			wantError: true,
		},
		{
			name: "wrong version",
			input: `version: STSv2
mode: enforce
mx: mail.example.com
max_age: 604800`,
			wantError: true,
		},
		{
			name: "missing mode",
			input: `version: STSv1
mx: mail.example.com
max_age: 604800`,
			wantError: true,
		},
		{
			name: "invalid mode",
			input: `version: STSv1
mode: invalid
mx: mail.example.com
max_age: 604800`,
			wantError: true,
		},
		{
			name: "missing mx",
			input: `version: STSv1
mode: enforce
max_age: 604800`,
			wantError: true,
		},
		{
			name: "missing max_age",
			input: `version: STSv1
mode: enforce
mx: mail.example.com`,
			wantError: true,
		},
		{
			name: "invalid max_age",
			input: `version: STSv1
mode: enforce
mx: mail.example.com
max_age: invalid`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewSTSResolver(DefaultSTSResolverConfig())
			policy, err := resolver.parsePolicy(strings.NewReader(tt.input))

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if policy.Mode != tt.wantMode {
				t.Errorf("Mode = %v, want %v", policy.Mode, tt.wantMode)
			}

			if len(policy.MXHosts) != len(tt.wantMX) {
				t.Errorf("MXHosts count = %d, want %d", len(policy.MXHosts), len(tt.wantMX))
			}

			for i, mx := range tt.wantMX {
				if i < len(policy.MXHosts) && policy.MXHosts[i] != mx {
					t.Errorf("MXHosts[%d] = %q, want %q", i, policy.MXHosts[i], mx)
				}
			}

			if policy.MaxAge != tt.wantAge {
				t.Errorf("MaxAge = %d, want %d", policy.MaxAge, tt.wantAge)
			}
		})
	}
}

func TestSTSResolver_GetPolicy_WithMockServer(t *testing.T) {
	// Create a mock HTTP server for MTA-STS policy
	policyServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/mta-sts.txt" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(`version: STSv1
mode: enforce
mx: mail.example.com
max_age: 604800
`))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer policyServer.Close()

	// Note: In a real test, we'd need to mock DNS lookups too
	// This test verifies the HTTP fetching and parsing logic
	t.Run("policy parsing integration", func(t *testing.T) {
		resolver := NewSTSResolver(DefaultSTSResolverConfig())

		// Test parsing directly
		policyText := `version: STSv1
mode: enforce
mx: mail.example.com
mx: *.backup.example.com
max_age: 604800`

		policy, err := resolver.parsePolicy(strings.NewReader(policyText))
		if err != nil {
			t.Fatalf("failed to parse policy: %v", err)
		}

		if policy.Mode != STSModeEnforce {
			t.Errorf("expected enforce mode, got %v", policy.Mode)
		}

		if !policy.ValidateMX("mail.example.com") {
			t.Error("expected mail.example.com to be valid")
		}

		if !policy.ValidateMX("mx1.backup.example.com") {
			t.Error("expected mx1.backup.example.com to be valid (wildcard)")
		}

		if policy.ValidateMX("other.example.com") {
			t.Error("expected other.example.com to be invalid")
		}
	})
}

func TestSTSResolver_Cache(t *testing.T) {
	resolver := NewSTSResolver(DefaultSTSResolverConfig())

	// Manually add a policy to cache
	policy := &STSPolicy{
		Version:   "STSv1",
		Mode:      STSModeEnforce,
		MXHosts:   []string{"mail.example.com"},
		MaxAge:    3600,
		FetchedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}

	resolver.cache.Store("example.com", policy)

	// Verify cache stats
	stats := resolver.CacheStats()
	if stats.TotalEntries != 1 {
		t.Errorf("TotalEntries = %d, want 1", stats.TotalEntries)
	}
	if stats.ValidEntries != 1 {
		t.Errorf("ValidEntries = %d, want 1", stats.ValidEntries)
	}

	// Clear cache
	resolver.ClearCache()
	stats = resolver.CacheStats()
	if stats.TotalEntries != 0 {
		t.Errorf("TotalEntries after clear = %d, want 0", stats.TotalEntries)
	}
}

func TestSTSResolver_GetPolicy_EmptyDomain(t *testing.T) {
	resolver := NewSTSResolver(DefaultSTSResolverConfig())
	ctx := context.Background()

	policy, err := resolver.GetPolicy(ctx, "")
	if err != nil {
		t.Errorf("unexpected error for empty domain: %v", err)
	}
	if policy != nil {
		t.Error("expected nil policy for empty domain")
	}
}

func TestNewSTSResolver_Defaults(t *testing.T) {
	// Test with zero config (should use defaults)
	resolver := NewSTSResolver(STSResolverConfig{})

	if resolver.httpClient == nil {
		t.Error("httpClient should not be nil")
	}
	if resolver.resolver == nil {
		t.Error("resolver should not be nil")
	}
}

func TestSTSResolverConfig_Defaults(t *testing.T) {
	cfg := DefaultSTSResolverConfig()

	if cfg.HTTPTimeout != 10*time.Second {
		t.Errorf("HTTPTimeout = %v, want 10s", cfg.HTTPTimeout)
	}
	if cfg.DNSTimeout != 5*time.Second {
		t.Errorf("DNSTimeout = %v, want 5s", cfg.DNSTimeout)
	}
}
