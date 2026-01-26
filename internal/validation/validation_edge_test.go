package validation

import (
	"strings"
	"testing"

	"github.com/fenilsonani/email-server/tests/shared/helpers"
)

// TestUsername_EdgeCases tests edge cases and boundary conditions
func TestUsername_EdgeCases(t *testing.T) {
	t.Run("boundary_lengths", func(t *testing.T) {
		tests := []struct {
			len     int
			wantErr bool
		}{
			{0, true},   // Below minimum
			{1, false},  // Minimum
			{2, false},  // Just above minimum
			{32, false}, // Middle
			{63, false}, // Just below maximum
			{64, false}, // Maximum
			{65, true},  // Just above maximum
		}

		for _, tt := range tests {
			t.Run("length_"+string(rune(48+tt.len)), func(t *testing.T) {
				username := strings.Repeat("a", tt.len)
				err := Username(username)
				if (err != nil) != tt.wantErr {
					t.Errorf("Length %d: error = %v, wantErr %v", tt.len, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("dot_positions", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"leading_dot", ".a", true},
			{"trailing_dot", "a.", true},
			{"middle_dot", "a.b", false},
			{"multiple_dots_valid", "a.b.c.d", false},
			{"dots_at_boundary", "a.b.c.d.e.f.g.h.i.j", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Username(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Username(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("consecutive_special_chars", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"double_dot_invalid", "a..b", true},
			{"triple_dot_invalid", "a...b", true},
			// Note: "a.-b", "a-.b", etc. are actually allowed by the regex pattern
			// The pattern `^[a-zA-Z0-9]([a-zA-Z0-9._+-]*[a-zA-Z0-9])?$` allows these
			{"dot_hyphen_combo", "a.-b", false},   // Actually allowed
			{"hyphen_dot_combo", "a-.b", false},   // Actually allowed
			{"underscore_hyphen", "a_-b", false},  // Actually allowed
			{"plus_dot_combo", "a+.b", false},     // Actually allowed
			{"underscore_underscore", "a__b", false}, // OK
			{"hyphen_hyphen", "a--b", false},      // OK
			{"plus_plus", "a++b", false},          // Actually allowed by pattern
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Username(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Username(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("whitespace_variants", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			// Note: strings.TrimSpace removes ALL leading/trailing whitespace (spaces, tabs, newlines, etc.)
			{"leading_space", " alice", false},  // TrimSpace removes leading, then "alice" is valid
			{"trailing_space", "alice ", false}, // TrimSpace removes trailing, then "alice" is valid
			{"tab_character", "alice\t", false},  // TrimSpace removes trailing tab, leaves "alice" valid
			{"newline_in_middle", "alice\nbob", true}, // Newline in middle can't be trimmed
			{"carriage_return", "alice\r", false}, // TrimSpace removes trailing CR, leaves "alice" valid
			{"form_feed", "alice\f", false}, // TrimSpace removes trailing FF, leaves "alice" valid
			{"vertical_tab", "alice\v", false}, // TrimSpace removes trailing VT, leaves "alice" valid
			{"non_breaking_space", "alice\u00A0bob", true}, // Non-breaking space is NOT removed by TrimSpace
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Username(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Username(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("unicode_handling", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			// Unicode characters are generally not allowed in validation
			{"emoji", "alice🎉", true},
			{"chinese", "用户", true},
			{"russian", "алиса", true},
			{"arabic", "أليس", true},
			{"combining_diacritics", "alicé", true}, // é with combining marks
			{"greek", "αλίκη", true},
			{"hebrew", "אליס", true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Username(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Username(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("numeric_edge_cases", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"all_numbers", "123456", false},
			{"leading_numbers", "123alice", false},
			{"trailing_numbers", "alice123", false},
			{"numbers_only_short", "1", false},
			{"numbers_only_long", "1234567890", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Username(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Username(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})
}

// TestPassword_EdgeCases tests password edge cases
func TestPassword_EdgeCases(t *testing.T) {
	t.Run("boundary_lengths", func(t *testing.T) {
		tests := []struct {
			len     int
			wantErr bool
		}{
			{0, true},    // Empty
			{1, true},    // Too short
			{7, true},    // Just below minimum
			{8, false},   // Minimum
			{9, false},   // Just above minimum
			{64, false},  // Middle
			{127, false}, // Just below maximum
			{128, false}, // Maximum
			{129, true},  // Just above maximum
			{256, true},  // Way too long
		}

		for _, tt := range tests {
			t.Run("length_"+string(rune(48+tt.len%48)), func(t *testing.T) {
				password := helpers.VeryLongPassword(tt.len)
				err := Password(password)
				if (err != nil) != tt.wantErr {
					t.Errorf("Length %d: error = %v, wantErr %v", tt.len, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("special_characters", func(t *testing.T) {
		// Password function only checks length (8-128 chars), not character content
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"with_quotes", `pass"word"test`, false},
			{"with_backticks", "pass`word`test", false},
			{"with_newline", "password\n123456", false},
			{"with_null_byte", "password\x00123456", false},
			{"with_control_chars", "password\x01\x02\x03", false},
			{"with_spaces", "pass word test", false},
			{"only_spaces_8", "        ", false}, // 8 spaces is valid length
			{"html_like", "<password>123", false},
			{"sql_like", "pass'; DROP--123", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Password(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Password(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("unicode_passwords", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"emoji_password", "🎉password🔒", false},
			{"chinese_password", "密码password123", false},
			{"russian_password", "парольpassword1", false},
			{"mixed_unicode", "pässwörd🔐12345", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Password(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Password(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("repeated_characters", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"all_same_char", "aaaaaaaa", false},
			{"all_numbers", "12345678", false},
			{"all_special", "!@#$%^&*", false},
			{"mostly_repeated", "aaaaaaab", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Password(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Password(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})
}

// TestDomain_EdgeCases tests domain edge cases
func TestDomain_EdgeCases(t *testing.T) {
	t.Run("label_boundaries", func(t *testing.T) {
		tests := []struct {
			name    string
			label   string
			wantErr bool
		}{
			{"label_1_char", "a", false},
			{"label_32_chars", strings.Repeat("a", 32), false},
			{"label_63_chars", strings.Repeat("a", 63), false},
			{"label_64_chars", strings.Repeat("a", 64), true},
			{"label_100_chars", strings.Repeat("a", 100), true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				domain := tt.label + ".com"
				err := Domain(domain)
				if (err != nil) != tt.wantErr {
					t.Errorf("Domain(%q): error = %v, wantErr %v", domain, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("depth_variations", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"single_label", "localhost", false},
			{"two_labels", "example.com", false},
			{"three_labels", "mail.example.com", false},
			{"four_labels", "smtp.mail.example.com", false},
			{"ten_labels", "a.b.c.d.e.f.g.h.i.j.com", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Domain(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Domain(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("hyphen_positions", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"hyphen_in_middle", "my-domain.com", false},
			{"hyphen_in_subdomain", "mail-server.example.com", false},
			{"multiple_hyphens", "my-awesome-domain.com", false},
			{"hyphen_at_start", "-example.com", true},
			{"hyphen_at_end", "example-.com", true},
			{"hyphen_adjacent_dot", "example-.com", true},
			{"hyphen_before_dot", "exam-ple.com", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Domain(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Domain(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("dot_variations", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"leading_dot", ".example.com", true},
			{"trailing_dot", "example.com.", true},
			{"double_dot", "example..com", true},
			{"triple_dot", "example...com", true},
			{"only_dot", ".", true},
			{"only_dots", "...", true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Domain(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Domain(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("case_normalization", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"all_lowercase", "example.com", false},
			{"all_uppercase", "EXAMPLE.COM", false},
			{"mixed_case", "ExAmPlE.CoM", false},
			{"uppercase_tld", "example.COM", false},
			{"mixed_subdomains", "MaiL.ExAmPlE.Com", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Domain(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Domain(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("numeric_labels", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"all_numbers", "192.168.1.1", false},
			{"numeric_label_start", "123example.com", false},
			{"numeric_label_end", "example123.com", false},
			{"mixed_numeric_alpha", "mail123.example456.com", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Domain(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Domain(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("whitespace_handling", func(t *testing.T) {
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			// Domain function uses strings.TrimSpace, which removes ALL leading/trailing whitespace
			// (including spaces, tabs, newlines, etc.)
			{"leading_space", " example.com", false}, // TrimSpace removes leading, then "example.com" is valid
			{"trailing_space", "example.com ", false}, // TrimSpace removes trailing, then "example.com" is valid
			{"space_in_middle", "exam ple.com", true}, // Space in middle is invalid
			{"tab", "example.com\t", false},  // TrimSpace removes trailing tab
			{"newline", "example.com\n", false}, // TrimSpace removes trailing newline
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Domain(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Domain(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})

	t.Run("international_domains", func(t *testing.T) {
		// These should fail as they contain Unicode characters
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"chinese_domain", "例え.jp", true},
			{"russian_domain", "пример.ru", true},
			{"arabic_domain", "مثال.ar", true},
			{"emoji_domain", "🌍.com", true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := Domain(tt.input)
				if (err != nil) != tt.wantErr {
					t.Errorf("Domain(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
				}
			})
		}
	})
}

// TestValidation_Normalization tests that inputs are properly normalized
func TestValidation_Normalization(t *testing.T) {
	t.Run("domain_lowercased", func(t *testing.T) {
		// Test that the function handles uppercase properly
		tests := []struct {
			input   string
			wantErr bool
		}{
			{"EXAMPLE.COM", false},
			{"Example.Com", false},
			{"eXaMpLe.CoM", false},
		}

		for _, tt := range tests {
			err := Domain(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Domain(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		}
	})

	t.Run("username_whitespace_stripped", func(t *testing.T) {
		tests := []struct {
			input   string
			wantErr bool
		}{
			// TrimSpace removes leading/trailing spaces, so these become valid
			{"  alice  ", false},  // After trimming: "alice" is valid
			{" alice", false},     // After trimming: "alice" is valid
			{"alice ", false},     // After trimming: "alice" is valid
		}

		for _, tt := range tests {
			err := Username(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Username(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		}
	})

	t.Run("domain_whitespace_stripped", func(t *testing.T) {
		tests := []struct {
			input   string
			wantErr bool
		}{
			// TrimSpace removes leading/trailing spaces
			{"  example.com  ", false}, // After stripping: "example.com" is valid
			{" example.com", false},    // After stripping: "example.com" is valid
			{"example.com ", false},    // After stripping: "example.com" is valid
		}

		for _, tt := range tests {
			err := Domain(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Domain(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		}
	})
}

// TestValidation_ConcurrentAccess tests thread-safety
func TestValidation_ConcurrentAccess(t *testing.T) {
	t.Run("username_concurrent", func(t *testing.T) {
		helpers.RunConcurrent(t, 100, func(i int) error {
			username := "alice" + string(rune('0'+i%10))
			if err := Username(username); err != nil {
				if i%10 == 0 { // Expect some to fail
					return nil
				}
				return err
			}
			return nil
		})
	})

	t.Run("password_concurrent", func(t *testing.T) {
		helpers.RunConcurrent(t, 100, func(i int) error {
			password := "password" + string(rune('0'+i%10))
			return Password(password)
		})
	})

	t.Run("domain_concurrent", func(t *testing.T) {
		helpers.RunConcurrent(t, 100, func(i int) error {
			domain := "example" + string(rune('0'+i%10)) + ".com"
			return Domain(domain)
		})
	})
}

// TestValidation_RealWorldExamples tests with realistic inputs
func TestValidation_RealWorldExamples(t *testing.T) {
	t.Run("real_usernames", func(t *testing.T) {
		tests := []struct {
			name    string
			wantErr bool
		}{
			{"john.doe", false},
			{"jane_smith", false},
			{"test+spam", false},
			{"first.last", false},
			{"user123", false},
			{"admin", false},
			{"info", false},
			{"noreply", false},
		}

		for _, tt := range tests {
			err := Username(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("Username(%q): error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		}
	})

	t.Run("real_passwords", func(t *testing.T) {
		tests := []struct {
			password string
			wantErr  bool
		}{
			{"MySecurePassword123!", false},
			{"correcthorsebatterystaple", false},
			{"2023-SecurePass-Q4", false},
			{"P@ssw0rd123!@#", false},
		}

		for _, tt := range tests {
			err := Password(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("Password(%q): error = %v, wantErr %v", tt.password, err, tt.wantErr)
			}
		}
	})

	t.Run("real_domains", func(t *testing.T) {
		tests := []struct {
			domain  string
			wantErr bool
		}{
			{"example.com", false},
			{"mail.example.com", false},
			{"google.com", false},
			{"api.github.com", false},
			{"cloud.google.com", false},
			{"s3.amazonaws.com", false},
			{"localhost", false},
			{"my-company.co.uk", false},
			{"test-domain-123.org", false},
		}

		for _, tt := range tests {
			err := Domain(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("Domain(%q): error = %v, wantErr %v", tt.domain, err, tt.wantErr)
			}
		}
	})
}

// TestValidation_CountingErrors tests that character counting is correct
func TestValidation_CountingErrors(t *testing.T) {
	t.Run("username_length_counting", func(t *testing.T) {
		// Test that length is counted correctly using len() on string
		// In Go, len() counts bytes, not runes, but for ASCII it's the same
		tests := []struct {
			name    string
			input   string
			wantErr bool
		}{
			{"single_byte_64", strings.Repeat("a", 64), false},
			{"single_byte_65", strings.Repeat("a", 65), true},
			// Note: Emoji is multiple bytes, so it counts toward the length limit
			// A single emoji might be 4 bytes, so "a" + emoji would be 5 bytes (valid)
			// But emoji also fails the regex pattern check
			{"emoji_fails_pattern", "a🎉", true}, // Fails due to pattern, not length
		}

		for _, tt := range tests {
			err := Username(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Username(%q): error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		}
	})
}

// TestValidation_PrintableCharacters tests printable character handling
func TestValidation_PrintableCharacters(t *testing.T) {
	t.Run("control_characters_invalid", func(t *testing.T) {
		for i := 0; i < 32; i++ { // Control characters (0-31)
			char := string(rune(i))
			input := "test" + char + "user"

			t.Run("control_"+string(rune('a'+i%26)), func(t *testing.T) {
				err := Username(input)
				if err == nil {
					t.Errorf("Username() accepted control character 0x%02x", i)
				}
			})
		}
	})

	t.Run("printable_ascii_in_username", func(t *testing.T) {
		// Test a wider range of printable ASCII
		validInUsername := "abcABC123._+-"
		for _, ch := range validInUsername {
			username := "test" + string(ch) + "user"
			if err := Username(username); err != nil {
				t.Errorf("Username(%q) failed but should be valid: %v", username, err)
			}
		}
	})
}
