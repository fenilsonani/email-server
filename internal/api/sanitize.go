package api

import (
	"fmt"
	"strings"
	"unicode"
)

// Maximum subject length per RFC 5322 (998 chars minus typical header overhead)
const MaxSubjectLength = 998

// Reserved headers that cannot be set via custom headers
var reservedHeaders = map[string]bool{
	"from":                      true,
	"to":                        true,
	"cc":                        true,
	"bcc":                       true,
	"subject":                   true,
	"date":                      true,
	"message-id":                true,
	"mime-version":              true,
	"content-type":              true,
	"content-transfer-encoding": true,
	"dkim-signature":            true,
	"domainkey-signature":       true,
	"received":                  true,
	"return-path":               true,
	"x-mailer":                  true,
	"reply-to":                  true, // Handled separately
}

// SanitizeHeaderValue removes CRLF characters to prevent header injection attacks.
// This is critical for preventing email header injection vulnerabilities.
func SanitizeHeaderValue(value string) string {
	// Remove CR, LF, and null characters that could be used for header injection
	var sb strings.Builder
	sb.Grow(len(value))

	for _, r := range value {
		switch r {
		case '\r', '\n', '\x00':
			// Skip these characters - they could be used for header injection
			continue
		default:
			sb.WriteRune(r)
		}
	}

	return sb.String()
}

// SanitizeSubject sanitizes and validates an email subject.
// Returns the sanitized subject and any validation error.
func SanitizeSubject(subject string) (string, error) {
	// Remove CRLF to prevent header injection
	sanitized := SanitizeHeaderValue(subject)

	// Check length limit (RFC 5322 recommends 78 chars per line, but allows up to 998)
	if len(sanitized) > MaxSubjectLength {
		return "", fmt.Errorf("subject exceeds maximum length of %d characters", MaxSubjectLength)
	}

	return sanitized, nil
}

// SanitizeEmailAddress sanitizes an email address for use in headers.
func SanitizeEmailAddress(email string) string {
	// Remove CRLF to prevent header injection
	sanitized := SanitizeHeaderValue(email)

	// Trim whitespace
	sanitized = strings.TrimSpace(sanitized)

	return sanitized
}

// ValidateCustomHeaders validates custom headers against reserved headers
// and sanitizes values to prevent header injection.
func ValidateCustomHeaders(headers map[string]string) (map[string]string, error) {
	if headers == nil {
		return nil, nil
	}

	sanitized := make(map[string]string, len(headers))

	for key, value := range headers {
		// Normalize key for comparison
		normalizedKey := strings.ToLower(strings.TrimSpace(key))

		// Check for reserved headers
		if reservedHeaders[normalizedKey] {
			return nil, fmt.Errorf("header '%s' is reserved and cannot be set", key)
		}

		// Validate header name format (must be valid HTTP token characters)
		if !isValidHeaderName(key) {
			return nil, fmt.Errorf("header name '%s' contains invalid characters", key)
		}

		// Sanitize the header value
		sanitizedValue := SanitizeHeaderValue(value)

		// Use the original key (preserving case)
		sanitized[key] = sanitizedValue
	}

	return sanitized, nil
}

// isValidHeaderName checks if a header name contains only valid characters.
// RFC 7230: token = 1*tchar
// tchar = "!" / "#" / "$" / "%" / "&" / "'" / "*" / "+" / "-" / "." /
//
//	"^" / "_" / "`" / "|" / "~" / DIGIT / ALPHA
func isValidHeaderName(name string) bool {
	if name == "" {
		return false
	}

	for _, r := range name {
		if !isTokenChar(r) {
			return false
		}
	}

	return true
}

// isTokenChar checks if a rune is a valid HTTP token character.
func isTokenChar(r rune) bool {
	// ALPHA / DIGIT
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}

	// Special characters allowed in tokens
	switch r {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	}

	return false
}

// SanitizeSendRequest sanitizes all header-related fields in a SendEmailRequest.
// Returns a validation error if any field fails validation.
func SanitizeSendRequest(req *SendEmailRequest) error {
	// Sanitize From
	req.From = SanitizeEmailAddress(req.From)

	// Sanitize To
	req.To = SanitizeEmailAddress(req.To)

	// Sanitize and validate Subject
	sanitizedSubject, err := SanitizeSubject(req.Subject)
	if err != nil {
		return NewValidationError("subject", err.Error())
	}
	req.Subject = sanitizedSubject

	// Sanitize ReplyTo if provided
	if req.ReplyTo != "" {
		req.ReplyTo = SanitizeEmailAddress(req.ReplyTo)
	}

	// Validate and sanitize custom headers
	if req.Headers != nil {
		sanitizedHeaders, err := ValidateCustomHeaders(req.Headers)
		if err != nil {
			return NewValidationError("headers", err.Error())
		}
		req.Headers = sanitizedHeaders
	}

	return nil
}

// ContainsCRLF checks if a string contains CRLF characters.
func ContainsCRLF(s string) bool {
	return strings.ContainsAny(s, "\r\n")
}

// IsValidEmailFormat performs basic email format validation.
func IsValidEmailFormat(email string) bool {
	// Must contain exactly one @ and have content before and after
	atIndex := strings.LastIndex(email, "@")
	if atIndex < 1 || atIndex == len(email)-1 {
		return false
	}

	local := email[:atIndex]
	domain := email[atIndex+1:]

	// Basic checks
	if len(local) == 0 || len(domain) == 0 {
		return false
	}

	// Domain must contain at least one dot (except localhost)
	if !strings.Contains(domain, ".") && domain != "localhost" {
		return false
	}

	return true
}
