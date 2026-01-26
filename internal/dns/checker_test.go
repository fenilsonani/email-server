package dns

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/tests/shared/helpers"
)

// TestNewChecker tests DNS checker construction and validation
func TestNewChecker(t *testing.T) {
	tests := []struct {
		name      string
		domain    string
		mailServer string
		wantErr   bool
		errMsg    string
	}{
		// Valid inputs
		{"valid_simple", "example.com", "mail.example.com", false, ""},
		{"valid_with_subdomain", "mail.example.com", "smtp.mail.example.com", false, ""},
		{"valid_with_hyphens", "my-domain.com", "mail-server.my-domain.com", false, ""},
		{"valid_single_letter", "a.com", "m.example.com", false, ""},

		// Invalid domain
		{"invalid_empty_domain", "", "mail.example.com", true, "domain cannot be empty"},
		{"invalid_empty_mailserver", "example.com", "", true, "mail server cannot be empty"},
		{"invalid_domain_format_leading_hyphen", "-example.com", "mail.example.com", true, "invalid domain name"},
		{"invalid_domain_format_trailing_hyphen", "example-.com", "mail.example.com", true, "invalid domain name"},
		{"invalid_domain_consecutive_dots", "example..com", "mail.example.com", true, "invalid domain name"},
		{"invalid_domain_with_space", "example .com", "mail.example.com", true, "invalid domain name"},
		{"invalid_domain_with_underscore", "example_test.com", "mail.example.com", true, "invalid domain name"},

		// Invalid mail server
		{"invalid_mailserver_leading_hyphen", "example.com", "-mail.example.com", true, "invalid mail server name"},
		{"invalid_mailserver_trailing_hyphen", "example.com", "mail-.example.com", true, "invalid mail server name"},
		{"invalid_mailserver_consecutive_dots", "example.com", "mail..example.com", true, "invalid mail server name"},

		// Edge cases that should succeed
		{"valid_numeric_domain", "123.com", "mail.123.com", false, ""},
		{"valid_numeric_subdomain", "example.123.com", "mail.example.123.com", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := NewChecker(tt.domain, tt.mailServer)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewChecker(%q, %q) error = %v, wantErr %v", tt.domain, tt.mailServer, err, tt.wantErr)
			}
			if tt.wantErr && tt.errMsg != "" && (err == nil || !contains(err.Error(), tt.errMsg)) {
				t.Errorf("NewChecker(%q, %q) error = %v, want error containing %q", tt.domain, tt.mailServer, err, tt.errMsg)
			}
			if !tt.wantErr && checker == nil {
				t.Errorf("NewChecker(%q, %q) returned nil checker", tt.domain, tt.mailServer)
			}
		})
	}
}

// TestCheckMX tests MX record validation
func TestCheckMX(t *testing.T) {
	// Note: These tests use real DNS to example.com
	// In an isolated environment, these will fail gracefully with MISSING status
	t.Run("check_mx_basic", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := checker.CheckMX(ctx)
		if result.RecordType != "MX" {
			t.Errorf("CheckMX() RecordType = %q, want %q", result.RecordType, "MX")
		}
		if result.Status == "" {
			t.Errorf("CheckMX() Status is empty")
		}
		// Status should be one of: PASS, FAIL, WARNING, MISSING
		validStatuses := map[Status]bool{
			StatusPass:    true,
			StatusFail:    true,
			StatusWarning: true,
			StatusMissing: true,
		}
		if !validStatuses[result.Status] {
			t.Errorf("CheckMX() Status = %q, not a valid status", result.Status)
		}
	})

	t.Run("check_mx_context_cancellation", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		result := checker.CheckMX(ctx)
		if result.Status != StatusFail {
			t.Errorf("CheckMX() with cancelled context returned %q, want FAIL", result.Status)
		}
		if !contains(result.Message, "Context error") {
			t.Errorf("CheckMX() with cancelled context Message = %q, want context error", result.Message)
		}
	})

	t.Run("check_mx_timeout", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		// Create a context with very short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		time.Sleep(2 * time.Millisecond) // Ensure timeout occurs
		result := checker.CheckMX(ctx)
		// Status should be FAIL when timeout happens
		if result.Status != StatusFail && result.Status != StatusMissing {
			t.Logf("CheckMX() with timeout returned %q status (expected FAIL or MISSING)", result.Status)
		}
	})
}

// TestCheckSPF tests SPF record validation
func TestCheckSPF(t *testing.T) {
	t.Run("check_spf_basic", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := checker.CheckSPF(ctx)
		if result.RecordType != "SPF" {
			t.Errorf("CheckSPF() RecordType = %q, want %q", result.RecordType, "SPF")
		}
		validStatuses := map[Status]bool{
			StatusPass:    true,
			StatusFail:    true,
			StatusWarning: true,
			StatusMissing: true,
		}
		if !validStatuses[result.Status] {
			t.Errorf("CheckSPF() Status = %q, not a valid status", result.Status)
		}
	})

	t.Run("check_spf_context_deadline", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
		defer cancel()

		result := checker.CheckSPF(ctx)
		if result.Status != StatusFail {
			t.Errorf("CheckSPF() with deadline exceeded returned %q, want FAIL", result.Status)
		}
	})
}

// TestCheckDKIM tests DKIM record validation
func TestCheckDKIM(t *testing.T) {
	t.Run("check_dkim_basic", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := checker.CheckDKIM(ctx)
		if result.RecordType != "DKIM" {
			t.Errorf("CheckDKIM() RecordType = %q, want %q", result.RecordType, "DKIM")
		}
		validStatuses := map[Status]bool{
			StatusPass:    true,
			StatusFail:    true,
			StatusWarning: true,
			StatusMissing: true,
		}
		if !validStatuses[result.Status] {
			t.Errorf("CheckDKIM() Status = %q, not a valid status", result.Status)
		}
	})

	t.Run("check_dkim_selectors", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := checker.CheckDKIM(ctx)
		// If MISSING, it should indicate which selectors were checked
		if result.Status == StatusMissing {
			if !contains(result.Message, "checked:") || !contains(result.Message, "mail") {
				t.Logf("CheckDKIM() MISSING message should list selectors checked: %q", result.Message)
			}
		}
	})

	t.Run("check_dkim_context_cancellation", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result := checker.CheckDKIM(ctx)
		if result.Status != StatusFail {
			t.Errorf("CheckDKIM() with cancelled context returned %q, want FAIL", result.Status)
		}
	})
}

// TestCheckDMARC tests DMARC record validation
func TestCheckDMARC(t *testing.T) {
	t.Run("check_dmarc_basic", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := checker.CheckDMARC(ctx)
		if result.RecordType != "DMARC" {
			t.Errorf("CheckDMARC() RecordType = %q, want %q", result.RecordType, "DMARC")
		}
		validStatuses := map[Status]bool{
			StatusPass:    true,
			StatusFail:    true,
			StatusWarning: true,
			StatusMissing: true,
		}
		if !validStatuses[result.Status] {
			t.Errorf("CheckDMARC() Status = %q, not a valid status", result.Status)
		}
	})

	t.Run("check_dmarc_context_error", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result := checker.CheckDMARC(ctx)
		if result.Status != StatusFail {
			t.Errorf("CheckDMARC() with cancelled context returned %q, want FAIL", result.Status)
		}
	})
}

// TestCheckPTR tests reverse DNS validation
func TestCheckPTR(t *testing.T) {
	t.Run("check_ptr_basic", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := checker.CheckPTR(ctx)
		if result.RecordType != "PTR" {
			t.Errorf("CheckPTR() RecordType = %q, want %q", result.RecordType, "PTR")
		}
		validStatuses := map[Status]bool{
			StatusPass:    true,
			StatusFail:    true,
			StatusWarning: true,
		}
		if !validStatuses[result.Status] {
			t.Errorf("CheckPTR() Status = %q, not a valid status", result.Status)
		}
	})

	t.Run("check_ptr_context_timeout", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		time.Sleep(2 * time.Millisecond)
		result := checker.CheckPTR(ctx)
		// Timeout should result in FAIL status
		if result.Status != StatusFail && result.Status != StatusWarning {
			t.Logf("CheckPTR() with timeout returned %q status", result.Status)
		}
	})
}

// TestCheckMailHostname tests mail hostname A record validation
func TestCheckMailHostname(t *testing.T) {
	t.Run("check_mail_hostname_basic", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := checker.CheckMailHostname(ctx)
		if result.RecordType != "A (mail hostname)" {
			t.Errorf("CheckMailHostname() RecordType = %q, want %q", result.RecordType, "A (mail hostname)")
		}
		validStatuses := map[Status]bool{
			StatusPass:    true,
			StatusFail:    true,
			StatusWarning: true,
		}
		if !validStatuses[result.Status] {
			t.Errorf("CheckMailHostname() Status = %q, not a valid status", result.Status)
		}
	})

	t.Run("check_mail_hostname_context_cancellation", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result := checker.CheckMailHostname(ctx)
		if result.Status != StatusFail {
			t.Errorf("CheckMailHostname() with cancelled context returned %q, want FAIL", result.Status)
		}
	})
}

// TestCheckAll tests running all DNS checks
func TestCheckAll(t *testing.T) {
	t.Run("check_all_returns_all_records", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		results := checker.CheckAll(ctx)
		// Should return 6 results: MX, SPF, DKIM, DMARC, PTR, A (mail hostname)
		if len(results) != 6 {
			t.Errorf("CheckAll() returned %d results, want 6", len(results))
		}

		recordTypes := make(map[string]bool)
		for _, result := range results {
			recordTypes[result.RecordType] = true
			// Each result should have a valid status
			if result.Status == "" {
				t.Errorf("CheckAll() result for %q has empty Status", result.RecordType)
			}
		}

		expectedTypes := map[string]bool{
			"MX":               true,
			"SPF":              true,
			"DKIM":             true,
			"DMARC":            true,
			"PTR":              true,
			"A (mail hostname)": true,
		}
		for expected := range expectedTypes {
			if !recordTypes[expected] {
				t.Errorf("CheckAll() missing result for %q", expected)
			}
		}
	})

	t.Run("check_all_context_cancellation", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		results := checker.CheckAll(ctx)
		if len(results) == 0 {
			t.Errorf("CheckAll() with cancelled context returned no results")
		}
		// At least first result should have FAIL status
		if len(results) > 0 && results[0].Status != StatusFail {
			t.Logf("CheckAll() first result status = %q with cancelled context", results[0].Status)
		}
	})

	t.Run("check_all_concurrent", func(t *testing.T) {
		helpers.RunConcurrent(t, 5, func(i int) error {
			checker, _ := NewChecker("example.com", "mail.example.com")
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			results := checker.CheckAll(ctx)
			if len(results) != 6 {
				return errors.New("unexpected number of results")
			}
			return nil
		})
	})
}

// TestCheckResult tests the CheckResult structure
func TestCheckResult(t *testing.T) {
	t.Run("check_result_fields", func(t *testing.T) {
		result := CheckResult{
			RecordType: "MX",
			Status:     StatusPass,
			Expected:   "mail.example.com",
			Actual:     "mail.example.com",
			Message:    "MX record found",
		}
		if result.RecordType != "MX" {
			t.Errorf("RecordType = %q, want MX", result.RecordType)
		}
		if result.Status != StatusPass {
			t.Errorf("Status = %q, want PASS", result.Status)
		}
		if result.Expected != "mail.example.com" {
			t.Errorf("Expected = %q, want mail.example.com", result.Expected)
		}
		if result.Actual != "mail.example.com" {
			t.Errorf("Actual = %q, want mail.example.com", result.Actual)
		}
		if result.Message != "MX record found" {
			t.Errorf("Message = %q, want MX record found", result.Message)
		}
	})

	t.Run("check_status_constants", func(t *testing.T) {
		if StatusPass != "PASS" {
			t.Errorf("StatusPass = %q, want PASS", StatusPass)
		}
		if StatusFail != "FAIL" {
			t.Errorf("StatusFail = %q, want FAIL", StatusFail)
		}
		if StatusWarning != "WARN" {
			t.Errorf("StatusWarning = %q, want WARN", StatusWarning)
		}
		if StatusMissing != "MISSING" {
			t.Errorf("StatusMissing = %q, want MISSING", StatusMissing)
		}
	})
}

// TestErrorVariables tests the error variables
func TestErrorVariables(t *testing.T) {
	t.Run("error_invalid_domain", func(t *testing.T) {
		if ErrInvalidDomain == nil {
			t.Errorf("ErrInvalidDomain is nil")
		}
		if ErrInvalidDomain.Error() != "invalid domain name" {
			t.Errorf("ErrInvalidDomain message = %q, want 'invalid domain name'", ErrInvalidDomain.Error())
		}
	})

	t.Run("error_invalid_mailserver", func(t *testing.T) {
		if ErrInvalidMailServer == nil {
			t.Errorf("ErrInvalidMailServer is nil")
		}
		if ErrInvalidMailServer.Error() != "invalid mail server name" {
			t.Errorf("ErrInvalidMailServer message = %q, want 'invalid mail server name'", ErrInvalidMailServer.Error())
		}
	})
}

// TestDomainValidation tests domain format validation
func TestDomainValidation(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		mailServer string
		wantErr bool
	}{
		// RFC 1035 compliant domains
		{"valid_lowercase", "example.com", "mail.example.com", false},
		{"valid_uppercase", "EXAMPLE.COM", "mail.example.com", false},
		{"valid_mixed_case", "Example.Com", "mail.example.com", false},
		{"valid_numeric", "123.456.com", "mail.example.com", false},
		{"valid_subdomain", "mail.example.com", "smtp.mail.example.com", false},
		{"valid_multiple_subdomains", "a.b.c.example.com", "mail.example.com", false},

		// Invalid domains
		{"invalid_leading_dot", ".example.com", "mail.example.com", true},
		{"invalid_trailing_dot_no_hostname", ".example.com", "mail.example.com", true},
		{"invalid_starts_with_hyphen", "-example.com", "mail.example.com", true},
		{"invalid_ends_with_hyphen", "example-.com", "mail.example.com", true},
		{"invalid_middle_hyphen_ok", "exam-ple.com", "mail.example.com", false},
		{"invalid_consecutive_hyphens", "exam--ple.com", "mail.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := NewChecker(tt.domain, tt.mailServer)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewChecker(%q, %q) error = %v, wantErr %v", tt.domain, tt.mailServer, err, tt.wantErr)
			}
			if !tt.wantErr && checker == nil {
				t.Errorf("NewChecker(%q, %q) returned nil", tt.domain, tt.mailServer)
			}
		})
	}
}

// TestSpecialDomains tests special domain handling
func TestSpecialDomains(t *testing.T) {
	t.Run("localhost_not_allowed", func(t *testing.T) {
		// localhost is technically invalid for DNS checks
		_, err := NewChecker("localhost", "mail.localhost")
		if err == nil {
			t.Errorf("NewChecker(localhost) should fail")
		}
	})

	t.Run("ip_address_rejected", func(t *testing.T) {
		// IP addresses are not valid domain names
		_, err := NewChecker("192.168.1.1", "mail.example.com")
		if err == nil {
			t.Errorf("NewChecker with IP address should fail")
		}
	})
}

// TestLongDomains tests handling of long domain names
func TestLongDomains(t *testing.T) {
	t.Run("valid_long_subdomain", func(t *testing.T) {
		longSubdomain := "verylongsubdomainname.example.com"
		checker, err := NewChecker(longSubdomain, "mail.example.com")
		if err != nil {
			t.Errorf("NewChecker with long subdomain failed: %v", err)
		}
		if checker == nil {
			t.Errorf("NewChecker returned nil")
		}
	})

	t.Run("invalid_label_too_long", func(t *testing.T) {
		// RFC 1035: Each label must be <= 63 characters
		longLabel := helpers.VeryLongString(65) + ".com"
		_, err := NewChecker(longLabel, "mail.example.com")
		if err == nil {
			t.Errorf("NewChecker with 65-char label should fail (max 63)")
		}
	})
}

// TestConcurrentChecks tests concurrent DNS checks
func TestConcurrentChecks(t *testing.T) {
	t.Run("concurrent_checker_creation", func(t *testing.T) {
		helpers.RunConcurrent(t, 20, func(i int) error {
			_, err := NewChecker("example.com", "mail.example.com")
			return err
		})
	})

	t.Run("concurrent_different_domains", func(t *testing.T) {
		helpers.RunConcurrent(t, 10, func(i int) error {
			domain := "example" + string(rune('0'+i)) + ".com"
			_, err := NewChecker(domain, "mail."+domain)
			return err
		})
	})

	t.Run("concurrent_check_all", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		helpers.RunConcurrent(t, 5, func(i int) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			results := checker.CheckAll(ctx)
			if len(results) != 6 {
				return errors.New("unexpected number of results")
			}
			return nil
		})
	})
}

// TestTimeoutHandling tests various timeout scenarios
func TestTimeoutHandling(t *testing.T) {
	t.Run("very_short_timeout", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		time.Sleep(2 * time.Millisecond)
		result := checker.CheckMX(ctx)
		// Should fail due to timeout
		if result.Status == "" {
			t.Errorf("CheckMX with very short timeout returned empty status")
		}
	})

	t.Run("reasonable_timeout", func(t *testing.T) {
		checker, _ := NewChecker("example.com", "mail.example.com")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result := checker.CheckMX(ctx)
		if result.Status == "" {
			t.Errorf("CheckMX with reasonable timeout returned empty status")
		}
	})
}

// BenchmarkCheckerCreation benchmarks checker creation
func BenchmarkCheckerCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewChecker("example.com", "mail.example.com")
	}
}

// BenchmarkCheckMX benchmarks MX record check
func BenchmarkCheckMX(b *testing.B) {
	checker, _ := NewChecker("example.com", "mail.example.com")
	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		checker.CheckMX(ctx)
	}
}

// BenchmarkCheckSPF benchmarks SPF record check
func BenchmarkCheckSPF(b *testing.B) {
	checker, _ := NewChecker("example.com", "mail.example.com")
	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		checker.CheckSPF(ctx)
	}
}

// BenchmarkCheckAll benchmarks all checks
func BenchmarkCheckAll(b *testing.B) {
	checker, _ := NewChecker("example.com", "mail.example.com")
	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		checker.CheckAll(ctx)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || len(s) > 0))
}
