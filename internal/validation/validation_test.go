package validation

import (
	"strings"
	"testing"

	"github.com/fenilsonani/email-server/internal/testutil"
)

// TestUsername_ValidInputs tests valid username formats
func TestUsername_ValidInputs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		skipTest bool
	}{
		// Basic valid inputs
		{"single_char", "a", false, false},
		{"alphanumeric", "alice", false, false},
		{"with_numbers", "user123", false, false},
		{"with_dot", "alice.bob", false, false},
		{"with_dots_multiple", "first.middle.last", false, false},
		{"with_underscore", "alice_bob", false, false},
		{"with_hyphen", "alice-bob", false, false},
		{"with_plus", "alice+bob", false, false},
		{"mixed_chars", "alice.bob-123_test+tag", false, false},

		// Edge cases at boundaries
		{"exactly_1_char", "a", false, false},
		{"exactly_64_chars", testutil.VeryLongString(64), false, false},

		// Capital letters
		{"all_caps", "ALICE", false, false},
		{"mixed_case", "AliceBob", false, false},

		// Single domain-like format
		{"single_letter_dot_single_letter", "a.b", false, false},
	}

	for _, tt := range tests {
		if tt.skipTest {
			t.Run(tt.name, func(t *testing.T) { t.Skip("Skipping") })
			continue
		}

		t.Run(tt.name, func(t *testing.T) {
			err := Username(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Username(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestUsername_InvalidInputs tests invalid username formats
func TestUsername_InvalidInputs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Length violations
		{"empty_string", "", true},
		{"only_whitespace", "   ", true},
		{"65_chars", testutil.VeryLongString(65), true},
		{"too_long_128_chars", testutil.VeryLongString(128), true},
		{"way_too_long", testutil.VeryLongString(1000), true},

		// Invalid patterns
		{"leading_dot", ".alice", true},
		{"trailing_dot", "alice.", true},
		{"consecutive_dots", "alice..bob", true},
		{"three_consecutive_dots", "alice...bob", true},
		{"only_dots", "...", true},

		// Invalid characters
		{"space", "alice bob", true},
		{"space_in_middle", "alice bob", true},
		{"special_char_at", "alice@bob", true},
		{"special_char_exclamation", "alice!", true},
		{"special_char_hash", "alice#bob", true},
		{"special_char_dollar", "alice$bob", true},
		{"special_char_percent", "alice%bob", true},
		{"special_char_caret", "alice^bob", true},
		{"special_char_ampersand", "alice&bob", true},
		{"special_char_asterisk", "alice*bob", true},
		{"special_char_slash", "alice/bob", true},
		{"special_char_backslash", "alice\\bob", true},
		{"special_char_comma", "alice,bob", true},
		{"special_char_semicolon", "alice;bob", true},
		{"special_char_colon", "alice:bob", true},
		{"special_char_quote", `alice"bob`, true},
		{"special_char_single_quote", "alice'bob", true},

		// Format violations
		{"only_space", " ", true},
		{"tab_character", "alice\tbob", true},
		{"newline_character", "alice\nbob", true},
		{"carriage_return", "alice\rbob", true},

		// Edge cases
		{"two_chars_only_dot", ".", true},
		{"numeric_only_is_ok", "123", false}, // Numbers are allowed
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Username(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Username(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestUsername_SecurityVectors tests security-critical invalid inputs
func TestUsername_SecurityVectors(t *testing.T) {
	// SQL injection attempts - should all fail
	t.Run("sql_injection_vectors", func(t *testing.T) {
		for _, injection := range testutil.SQLInjectionStrings {
			t.Run("injection_attempt", func(t *testing.T) {
				err := Username(injection)
				if err == nil {
					t.Errorf("Username() accepted SQL injection: %q", injection)
				}
			})
		}
	})

	// Path traversal attempts - should all fail
	t.Run("path_traversal_vectors", func(t *testing.T) {
		for _, traversal := range testutil.PathTraversalStrings {
			t.Run("traversal_attempt", func(t *testing.T) {
				err := Username(traversal)
				if err == nil {
					t.Errorf("Username() accepted path traversal: %q", traversal)
				}
			})
		}
	})

	// Null byte injection - should all fail
	t.Run("null_byte_vectors", func(t *testing.T) {
		for _, nullByte := range testutil.NullByteStrings {
			t.Run("null_byte_attempt", func(t *testing.T) {
				err := Username(nullByte)
				if err == nil {
					t.Errorf("Username() accepted null byte injection: %q", nullByte)
				}
			})
		}
	})
}

// TestPassword_ValidInputs tests valid password inputs
func TestPassword_ValidInputs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Minimum length (8 chars)
		{"exactly_8_chars", "Abcd1234", false},
		{"exactly_8_simple", "password", false},

		// Common lengths
		{"16_chars", testutil.VeryLongPassword(16), false},
		{"32_chars", testutil.VeryLongPassword(32), false},
		{"64_chars", testutil.VeryLongPassword(64), false},
		{"100_chars", testutil.VeryLongPassword(100), false},

		// Maximum length (128 chars)
		{"exactly_128_chars", testutil.VeryLongPassword(128), false},

		// Simple passwords (all same)
		{"all_lowercase", "abcdefgh", false},
		{"all_uppercase", "ABCDEFGH", false},
		{"all_numbers", "12345678", false},
		{"all_special", "!@#$%^&*", false},

		// Mixed content
		{"mixed_case", "AbCdEfGh", false},
		{"mixed_case_and_numbers", "AbCd1234", false},
		{"mixed_with_special", "Pass@word!", false},
		{"with_spaces", "pass word 123", false},
		{"unicode_chars", "pässwörd123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Password(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Password(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestPassword_InvalidInputs tests invalid password inputs
func TestPassword_InvalidInputs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Too short
		{"empty_string", "", true},
		{"1_char", "a", true},
		{"7_chars", "abcdefg", true},

		// Too long
		{"129_chars", testutil.VeryLongPassword(129), true},
		{"200_chars", testutil.VeryLongPassword(200), true},
		{"1000_chars", testutil.VeryLongPassword(1000), true},

		// Edge cases (Password doesn't trim whitespace)
		{"space_padded_short", " short ", true}, // " short " = 7 chars (with spaces), too short
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Password(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Password(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestPassword_SecurityVectors tests security-critical invalid inputs
func TestPassword_SecurityVectors(t *testing.T) {
	// SQL injection attempts - short ones should fail (< 8 chars)
	t.Run("sql_injection_too_short", func(t *testing.T) {
		short := "'; DROP"
		err := Password(short)
		if err == nil {
			t.Errorf("Password() should reject short injection: %q", short)
		}
	})

	// Long SQL injection should fail if > 128 chars
	t.Run("sql_injection_too_long", func(t *testing.T) {
		long := strings.Repeat("'; DROP TABLE users; --", 10) // Much longer than 128
		err := Password(long)
		if err == nil {
			t.Errorf("Password() should reject long injection: %q", long[:50]+"...")
		}
	})

	// Null byte injection - should still work if 8-128 chars
	// (Password doesn't explicitly filter null bytes - that's done at application level)
	t.Run("null_byte_8_chars", func(t *testing.T) {
		pwd := "pass\x00word!" // 10 chars, valid length
		err := Password(pwd)
		if err != nil {
			t.Errorf("Password() with embedded null byte should be length-acceptable: %v", err)
		}
	})
}

// TestDomain_ValidInputs tests valid domain formats
func TestDomain_ValidInputs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Simple domains
		{"simple", "example.com", false},
		{"two_letter_tld", "example.co", false},
		{"three_letter_tld", "example.org", false},

		// Subdomains
		{"one_subdomain", "mail.example.com", false},
		{"two_subdomains", "server1.mail.example.com", false},
		{"three_subdomains", "mail.server.example.co.uk", false},

		// With numbers
		{"with_numbers", "example123.com", false},
		{"numbers_in_label", "mail123.example456.com", false},

		// With hyphens
		{"with_hyphen", "my-domain.com", false},
		{"hyphen_in_middle", "mail-server.example-domain.com", false},

		// Single label domains
		{"localhost_like", "localhost", false},
		{"single_label_numeric", "mail", false},

		// Uppercase (should be lowercased by function)
		{"uppercase", "EXAMPLE.COM", false},
		{"mixed_case", "ExAmPlE.CoM", false},

		// Long but valid
		{"long_labels", "verylonglabelname.anotherlonglabel.example.com", false},

		// Edge case: exactly 253 chars (max domain length)
		{"max_length_253", generateMaxLengthDomain(253), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Domain(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Domain(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestDomain_InvalidInputs tests invalid domain formats
func TestDomain_InvalidInputs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Empty/whitespace
		{"empty_string", "", true},
		{"only_whitespace", "   ", true},
		{"only_dot", ".", true},
		{"only_dots", "...", true},

		// Too long
		{"254_chars", generateMaxLengthDomain(254), true},
		{"300_chars", generateMaxLengthDomain(300), true},

		// Invalid characters
		{"space", "example .com", true},
		{"space_in_label", "exam ple.com", true},
		{"underscore", "example_.com", true},
		{"underscore_in_label", "ex_ample.com", true},
		{"special_chars_hash", "example#.com", true},
		{"special_chars_at", "example@.com", true},
		{"special_chars_slash", "example/.com", true},
		{"special_chars_backslash", "example\\.com", true},

		// Invalid label format
		{"label_starting_with_hyphen", "-example.com", true},
		{"label_ending_with_hyphen", "example-.com", true},
		{"label_too_long_64_chars", generateLabelTooLong(64) + ".com", true},

		// Invalid patterns
		{"trailing_dot_only", ".com", true},
		{"leading_dot", ".example.com", true},
		{"double_dot", "example..com", true},
		{"consecutive_hyphens_in_label", "ex--ample.com", false}, // Actually valid in RFC

		// Reserved/invalid TLDs (format-wise)
		{"no_tld", "example", false}, // Single label is technically valid
		{"numeric_only_label", "123.com", false}, // Actually valid

		// Whitespace variations
		// Domain function trims leading/trailing whitespace, so these are valid
		{"trailing_space", "example.com ", false},
		{"leading_space", " example.com", false},
		{"space_in_middle", "exa mple.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Domain(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Domain(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestDomain_SecurityVectors tests security-critical invalid inputs
func TestDomain_SecurityVectors(t *testing.T) {
	// SQL injection attempts
	t.Run("sql_injection_vectors", func(t *testing.T) {
		injections := []string{
			"example.com'; DROP TABLE users; --",
			"example.com' OR '1'='1",
			"example.com\"; DROP TABLE users; --",
		}
		for _, injection := range injections {
			t.Run("injection", func(t *testing.T) {
				err := Domain(injection)
				if err == nil {
					t.Errorf("Domain() accepted SQL injection: %q", injection)
				}
			})
		}
	})

	// Path traversal attempts
	t.Run("path_traversal_vectors", func(t *testing.T) {
		traversals := []string{
			"../../../etc/passwd",
			"..\\..\\..\\windows",
			"/etc/passwd",
		}
		for _, traversal := range traversals {
			t.Run("traversal", func(t *testing.T) {
				err := Domain(traversal)
				if err == nil {
					t.Errorf("Domain() accepted path traversal: %q", traversal)
				}
			})
		}
	})

	// Null byte injection
	t.Run("null_byte_vectors", func(t *testing.T) {
		nullBytes := []string{
			"example.com\x00.evil.com",
			"example.com\x00",
			"\x00example.com",
		}
		for _, nb := range nullBytes {
			t.Run("null_byte", func(t *testing.T) {
				err := Domain(nb)
				if err == nil {
					t.Errorf("Domain() accepted null byte: %q", nb)
				}
			})
		}
	})
}

// TestDomain_LabelValidation tests per-label constraints
func TestDomain_LabelValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Label length exactly 63 (max)
		{"label_exactly_63", strings.Repeat("a", 63) + ".com", false},

		// Label length 64 (too long)
		{"label_64", strings.Repeat("a", 64) + ".com", true},

		// Multiple labels with one too long
		{"first_label_ok_second_too_long", "ok." + strings.Repeat("b", 64) + ".com", true},

		// Empty label
		{"empty_label_double_dot", "example..com", true},

		// Single char labels
		{"single_char_labels", "a.b.c.d", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Domain(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Domain(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestErrorMessages verifies error messages
func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name        string
		validate    func() error
		expectError bool
		wantMessage string
	}{
		{"username_too_long", func() error { return Username(strings.Repeat("a", 65)) }, true, "invalid username"},
		{"password_too_short", func() error { return Password("short") }, true, "invalid password"},
		{"domain_invalid", func() error { return Domain("") }, true, "invalid domain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.validate()
			if (err != nil) != tt.expectError {
				t.Errorf("Expected error: %v, got: %v", tt.expectError, err)
			}
			if err != nil && tt.wantMessage != "" && !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("Error message %q does not contain %q", err.Error(), tt.wantMessage)
			}
		})
	}
}

// Helper functions

// generateMaxLengthDomain generates a domain of approximately the specified length
// For testing, when length > 253, it generates a domain that's definitely over the limit
func generateMaxLengthDomain(length int) string {
	if length <= 4 {
		return "a.co"
	}

	if length > 253 {
		// For lengths > 253, just create a single very long label that exceeds the limit
		// This will fail validation because it exceeds 253 chars total
		return strings.Repeat("a", length-4) + ".com"
	}

	// For lengths <= 253, create a valid domain structure
	// Pattern: "label1.label2...com" where each label is <= 63 chars
	remaining := length - 4 // minus ".com"

	var labels []string
	for remaining > 63 {
		labels = append(labels, strings.Repeat("a", 63))
		remaining -= 64 // 63 chars + 1 dot
	}

	if remaining > 0 {
		labels = append(labels, strings.Repeat("b", remaining-1)) // -1 for the dot
	}

	return strings.Join(labels, ".") + ".com"
}

// generateLabelTooLong generates a domain with a label exceeding 63 chars
func generateLabelTooLong(labelLen int) string {
	if labelLen <= 63 {
		labelLen = 64
	}
	return strings.Repeat("a", labelLen)
}

// Benchmark tests

// BenchmarkUsername benchmarks the Username function
func BenchmarkUsername(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Username("alice.bob")
	}
}

// BenchmarkPassword benchmarks the Password function
func BenchmarkPassword(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Password("P@ssw0rd123")
	}
}

// BenchmarkDomain benchmarks the Domain function
func BenchmarkDomain(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Domain("mail.example.com")
	}
}
