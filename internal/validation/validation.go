// Package validation provides input validation functions.
package validation

import (
	"errors"
	"regexp"
	"strings"
)

var (
	// ErrInvalidUsername is returned when username format is invalid
	ErrInvalidUsername = errors.New("invalid username: must be 1-64 characters and valid email local part")
	// ErrInvalidPassword is returned when password doesn't meet requirements
	ErrInvalidPassword = errors.New("invalid password: must be 8-128 characters")
	// ErrInvalidDomain is returned when domain name is invalid
	ErrInvalidDomain = errors.New("invalid domain: must be valid domain name")
)

const (
	// Password constraints (following NIST SP 800-63B recommendations)
	minPasswordLength = 8
	maxPasswordLength = 128

	// Username constraints (RFC 5321 local-part)
	minUsernameLength = 1
	maxUsernameLength = 64

	// Domain name constraints (RFC 1035)
	maxDomainLength = 253
)

var (
	// RFC 5321 compliant local-part pattern (simplified for common use cases)
	// Allows: alphanumeric, dot, hyphen, underscore, plus
	// Does not allow: leading/trailing dots, consecutive dots
	usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9._+-]*[a-zA-Z0-9])?$`)

	// RFC 1035 compliant domain name pattern
	// Labels: 1-63 chars, alphanumeric and hyphen, not starting/ending with hyphen
	domainPattern = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
)

// Username checks if a username meets format and length requirements
func Username(username string) error {
	username = strings.TrimSpace(username)

	if len(username) < minUsernameLength || len(username) > maxUsernameLength {
		return ErrInvalidUsername
	}

	if !usernamePattern.MatchString(username) {
		return ErrInvalidUsername
	}

	// Additional checks for common issues
	if strings.Contains(username, "..") {
		return ErrInvalidUsername // Consecutive dots not allowed
	}

	return nil
}

// Password checks if a password meets security requirements
func Password(password string) error {
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return ErrInvalidPassword
	}
	return nil
}

// Domain checks if a domain name is valid according to RFC 1035
func Domain(domain string) error {
	domain = strings.TrimSpace(strings.ToLower(domain))

	if len(domain) == 0 || len(domain) > maxDomainLength {
		return ErrInvalidDomain
	}

	if !domainPattern.MatchString(domain) {
		return ErrInvalidDomain
	}

	// Additional validation: check each label length (max 63 chars per RFC 1035)
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return ErrInvalidDomain
		}
	}

	return nil
}
