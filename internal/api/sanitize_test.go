package api

import (
	"strings"
	"testing"
)

func TestSanitizeHeaderValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal value",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "value with CRLF",
			input:    "Hello\r\nWorld",
			expected: "HelloWorld",
		},
		{
			name:     "value with CR only",
			input:    "Hello\rWorld",
			expected: "HelloWorld",
		},
		{
			name:     "value with LF only",
			input:    "Hello\nWorld",
			expected: "HelloWorld",
		},
		{
			name:     "value with null byte",
			input:    "Hello\x00World",
			expected: "HelloWorld",
		},
		{
			name:     "header injection attempt",
			input:    "value\r\nX-Injected: malicious",
			expected: "valueX-Injected: malicious",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "unicode preserved",
			input:    "Hello 世界 🌍",
			expected: "Hello 世界 🌍",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeHeaderValue(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeHeaderValue(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeSubject(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{
			name:     "normal subject",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "subject with CRLF injection",
			input:    "Subject\r\nBcc: attacker@evil.com",
			expected: "SubjectBcc: attacker@evil.com",
		},
		{
			name:        "subject too long",
			input:       strings.Repeat("a", 1000),
			expectError: true,
		},
		{
			name:     "max length subject",
			input:    strings.Repeat("a", 998),
			expected: strings.Repeat("a", 998),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SanitizeSubject(tt.input)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("SanitizeSubject(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeEmailAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal email",
			input:    "user@example.com",
			expected: "user@example.com",
		},
		{
			name:     "email with whitespace",
			input:    "  user@example.com  ",
			expected: "user@example.com",
		},
		{
			name:     "email with injection",
			input:    "user@example.com\r\nBcc: attacker@evil.com",
			expected: "user@example.comBcc: attacker@evil.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeEmailAddress(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeEmailAddress(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateCustomHeaders(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid custom headers",
			headers: map[string]string{
				"X-Custom-Header": "value",
				"X-Another":       "value2",
			},
			expectError: false,
		},
		{
			name: "reserved header From",
			headers: map[string]string{
				"From": "attacker@evil.com",
			},
			expectError: true,
			errorMsg:    "reserved",
		},
		{
			name: "reserved header DKIM-Signature",
			headers: map[string]string{
				"DKIM-Signature": "forged",
			},
			expectError: true,
			errorMsg:    "reserved",
		},
		{
			name: "reserved header case insensitive",
			headers: map[string]string{
				"FROM": "attacker@evil.com",
			},
			expectError: true,
			errorMsg:    "reserved",
		},
		{
			name: "invalid header name with space",
			headers: map[string]string{
				"X Invalid": "value",
			},
			expectError: true,
			errorMsg:    "invalid",
		},
		{
			name: "header value sanitized",
			headers: map[string]string{
				"X-Custom": "value\r\nX-Injected: bad",
			},
			expectError: false,
		},
		{
			name:        "nil headers",
			headers:     nil,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ValidateCustomHeaders(tt.headers)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errorMsg != "" && !strings.Contains(strings.ToLower(err.Error()), tt.errorMsg) {
					t.Errorf("Error %q doesn't contain %q", err.Error(), tt.errorMsg)
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			// Check that values are sanitized
			if result != nil {
				for k, v := range result {
					if ContainsCRLF(v) {
						t.Errorf("Header %q value still contains CRLF", k)
					}
				}
			}
		})
	}
}

func TestIsValidHeaderName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"valid alphanumeric", "X-Custom-Header", true},
		{"valid with numbers", "X-Header123", true},
		{"valid with special", "X-Header_Test", true},
		{"empty string", "", false},
		{"with space", "X Header", false},
		{"with colon", "X:Header", false},
		{"with bracket", "X[Header]", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidHeaderName(tt.input)
			if result != tt.expected {
				t.Errorf("isValidHeaderName(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsValidEmailFormat(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		expected bool
	}{
		{"valid email", "user@example.com", true},
		{"valid with subdomain", "user@mail.example.com", true},
		{"valid localhost", "user@localhost", true},
		{"missing @", "userexample.com", false},
		{"missing local part", "@example.com", false},
		{"missing domain", "user@", false},
		{"no dot in domain", "user@example", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidEmailFormat(tt.email)
			if result != tt.expected {
				t.Errorf("IsValidEmailFormat(%q) = %v, want %v", tt.email, result, tt.expected)
			}
		})
	}
}

func TestSanitizeSendRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         *SendEmailRequest
		expectError bool
		errorField  string
	}{
		{
			name: "valid request",
			req: &SendEmailRequest{
				From:    "sender@example.com",
				To:      "recipient@example.com",
				Subject: "Test Subject",
			},
			expectError: false,
		},
		{
			name: "request with injection attempts",
			req: &SendEmailRequest{
				From:    "sender@example.com\r\nBcc: attacker@evil.com",
				To:      "recipient@example.com",
				Subject: "Subject\r\nX-Injected: value",
			},
			expectError: false, // Should sanitize, not error
		},
		{
			name: "subject too long",
			req: &SendEmailRequest{
				From:    "sender@example.com",
				To:      "recipient@example.com",
				Subject: strings.Repeat("a", 1000),
			},
			expectError: true,
			errorField:  "subject",
		},
		{
			name: "reserved custom header",
			req: &SendEmailRequest{
				From:    "sender@example.com",
				To:      "recipient@example.com",
				Subject: "Test",
				Headers: map[string]string{
					"From": "forged@evil.com",
				},
			},
			expectError: true,
			errorField:  "headers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SanitizeSendRequest(tt.req)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errorField != "" {
					if validErr, ok := err.(*ValidationError); ok {
						if validErr.Field != tt.errorField {
							t.Errorf("Error field = %q, want %q", validErr.Field, tt.errorField)
						}
					}
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			// Verify sanitization occurred
			if ContainsCRLF(tt.req.From) {
				t.Error("From still contains CRLF")
			}
			if ContainsCRLF(tt.req.To) {
				t.Error("To still contains CRLF")
			}
			if ContainsCRLF(tt.req.Subject) {
				t.Error("Subject still contains CRLF")
			}
		})
	}
}

func TestContainsCRLF(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"normal", false},
		{"with\rCR", true},
		{"with\nLF", true},
		{"with\r\nCRLF", true},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ContainsCRLF(tt.input)
			if result != tt.expected {
				t.Errorf("ContainsCRLF(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
