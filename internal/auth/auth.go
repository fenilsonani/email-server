package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrInvalidCredentials is returned when authentication fails
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrUserNotFound is returned when a user doesn't exist
	ErrUserNotFound = errors.New("user not found")
	// ErrUserDisabled is returned when a user account is disabled
	ErrUserDisabled = errors.New("user account is disabled")
	// ErrDomainNotFound is returned when a domain doesn't exist
	ErrDomainNotFound = errors.New("domain not found")
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

// User represents an authenticated user
type User struct {
	ID          int64
	DomainID    int64
	Username    string // local part of email
	Domain      string // domain part
	Email       string // full email address
	DisplayName string
	QuotaBytes  int64
	UsedBytes   int64
	IsActive    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Authenticator provides user authentication and lookup
type Authenticator struct {
	db *sql.DB
}

// NewAuthenticator creates a new Authenticator with the given database
func NewAuthenticator(db *sql.DB) *Authenticator {
	return &Authenticator{db: db}
}

// Authenticate validates credentials and returns user info
// NOTE: Rate limiting should be implemented at the HTTP/SMTP layer to prevent brute force attacks.
// Recommended approach: Use middleware with token bucket or sliding window algorithm.
// Example: Limit to 5 failed attempts per IP per 15 minutes, with exponential backoff.
// Consider implementing account lockout after 10 failed attempts within 1 hour.
func (a *Authenticator) Authenticate(ctx context.Context, email, password string) (*User, error) {
	// Basic email parsing (no password validation yet to avoid timing attacks)
	username, domain, err := parseEmail(email)
	if err != nil {
		return nil, ErrInvalidCredentials // Don't leak validation details
	}

	// Look up user
	user, passwordHash, err := a.lookupUserWithPassword(ctx, username, domain)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("authentication lookup failed: %w", err)
	}

	// Check if account is disabled BEFORE validating password
	// This prevents information leakage about account status
	if !user.IsActive {
		return nil, ErrUserDisabled
	}

	// Now validate password length (do this before expensive hash verification)
	if err := ValidatePassword(password); err != nil {
		return nil, ErrInvalidCredentials // Don't leak validation details
	}

	// Verify password hash (constant-time comparison)
	if !VerifyPassword(password, passwordHash) {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

// LookupUser finds a user by email address
func (a *Authenticator) LookupUser(ctx context.Context, email string) (*User, error) {
	username, domain, err := parseEmail(email)
	if err != nil {
		return nil, fmt.Errorf("invalid email format: %w", err)
	}

	user, _, err := a.lookupUserWithPassword(ctx, username, domain)
	if err != nil {
		// Return sentinel errors directly for API consumers
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("user lookup failed: %w", err)
	}
	return user, nil
}

// LookupUserByID finds a user by their ID
func (a *Authenticator) LookupUserByID(ctx context.Context, id int64) (*User, error) {
	query := `
		SELECT u.id, u.domain_id, u.username, d.name, u.display_name,
		       u.quota_bytes, u.used_bytes, u.is_active, u.created_at, u.updated_at
		FROM users u
		JOIN domains d ON u.domain_id = d.id
		WHERE u.id = ?
	`

	var user User
	var displayName sql.NullString
	err := a.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID, &user.DomainID, &user.Username, &user.Domain,
		&displayName, &user.QuotaBytes, &user.UsedBytes,
		&user.IsActive, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to query user by id %d: %w", id, err)
	}

	user.DisplayName = displayName.String
	user.Email = fmt.Sprintf("%s@%s", user.Username, user.Domain)
	return &user, nil
}

// ValidateAddress checks if an address is valid for local delivery
func (a *Authenticator) ValidateAddress(ctx context.Context, email string) (bool, error) {
	username, domain, err := parseEmail(email)
	if err != nil {
		return false, nil
	}

	// Check if domain exists
	var domainID int64
	err = a.db.QueryRowContext(ctx,
		"SELECT id FROM domains WHERE name = ? AND is_active = TRUE",
		domain,
	).Scan(&domainID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil // Domain not managed by us
		}
		return false, fmt.Errorf("failed to query domain %s: %w", domain, err)
	}

	// Check if user exists
	var userExists int
	err = a.db.QueryRowContext(ctx,
		"SELECT 1 FROM users WHERE domain_id = ? AND username = ? AND is_active = TRUE",
		domainID, username,
	).Scan(&userExists)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("failed to query user %s@%s: %w", username, domain, err)
	}

	// Check if alias exists
	var aliasExists int
	err = a.db.QueryRowContext(ctx,
		"SELECT 1 FROM aliases WHERE domain_id = ? AND source_address = ? AND is_active = TRUE",
		domainID, username,
	).Scan(&aliasExists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("failed to query alias %s@%s: %w", username, domain, err)
}

// ResolveAlias resolves an alias to its destination(s)
// Returns the user ID if it's a local alias, or the external address
func (a *Authenticator) ResolveAlias(ctx context.Context, email string) (userID *int64, external *string, err error) {
	username, domain, err := parseEmail(email)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid email format: %w", err)
	}

	query := `
		SELECT a.destination_user_id, a.destination_external
		FROM aliases a
		JOIN domains d ON a.domain_id = d.id
		WHERE d.name = ? AND a.source_address = ? AND a.is_active = TRUE
	`

	var destUserID sql.NullInt64
	var destExternal sql.NullString
	err = a.db.QueryRowContext(ctx, query, domain, username).Scan(&destUserID, &destExternal)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil // No alias
		}
		return nil, nil, fmt.Errorf("failed to resolve alias %s@%s: %w", username, domain, err)
	}

	if destUserID.Valid {
		return &destUserID.Int64, nil, nil
	}
	if destExternal.Valid {
		return nil, &destExternal.String, nil
	}
	return nil, nil, nil
}

// GetDomainID returns the ID for a domain name
func (a *Authenticator) GetDomainID(ctx context.Context, name string) (int64, error) {
	// Validate domain name format
	if err := ValidateDomain(name); err != nil {
		return 0, err
	}

	var id int64
	err := a.db.QueryRowContext(ctx,
		"SELECT id FROM domains WHERE name = ? AND is_active = TRUE",
		name,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrDomainNotFound
		}
		return 0, fmt.Errorf("failed to query domain %s: %w", name, err)
	}
	return id, nil
}

// CreateUser creates a new user account with full validation and transaction support
func (a *Authenticator) CreateUser(ctx context.Context, username, password string, domainID int64) (*User, error) {
	// Validate username format
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}

	// Validate password strength
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}

	// Normalize username to lowercase for consistency
	username = strings.ToLower(strings.TrimSpace(username))

	// Begin transaction for atomicity
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call even after commit

	// Verify domain exists and get domain name (within transaction)
	var domainName string
	err = tx.QueryRowContext(ctx, "SELECT name FROM domains WHERE id = ? AND is_active = TRUE", domainID).Scan(&domainName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDomainNotFound
		}
		return nil, fmt.Errorf("failed to query domain id %d: %w", domainID, err)
	}

	// Hash password
	passwordHash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Insert user
	result, err := tx.ExecContext(ctx, `
		INSERT INTO users (domain_id, username, password_hash, is_active, created_at, updated_at)
		VALUES (?, ?, ?, TRUE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, domainID, username, passwordHash)
	if err != nil {
		return nil, fmt.Errorf("failed to create user %s@%s: %w", username, domainName, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &User{
		ID:       id,
		DomainID: domainID,
		Username: username,
		Domain:   domainName,
		Email:    fmt.Sprintf("%s@%s", username, domainName),
		IsActive: true,
	}, nil
}

// UpdatePassword updates a user's password
func (a *Authenticator) UpdatePassword(ctx context.Context, userID int64, password string) error {
	// Validate password strength
	if err := validatePassword(password); err != nil {
		return err
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	result, err := a.db.ExecContext(ctx, `
		UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, passwordHash, userID)
	if err != nil {
		return fmt.Errorf("failed to update password for user id %d: %w", userID, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if affected == 0 {
		return ErrUserNotFound
	}

	return nil
}

// lookupUserWithPassword retrieves user info including password hash
func (a *Authenticator) lookupUserWithPassword(ctx context.Context, username, domain string) (*User, string, error) {
	query := `
		SELECT u.id, u.domain_id, u.username, d.name, u.password_hash, u.display_name,
		       u.quota_bytes, u.used_bytes, u.is_active, u.created_at, u.updated_at
		FROM users u
		JOIN domains d ON u.domain_id = d.id
		WHERE d.name = ? AND u.username = ?
	`

	var user User
	var passwordHash string
	var displayName sql.NullString

	err := a.db.QueryRowContext(ctx, query, domain, username).Scan(
		&user.ID, &user.DomainID, &user.Username, &user.Domain,
		&passwordHash, &displayName, &user.QuotaBytes, &user.UsedBytes,
		&user.IsActive, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrUserNotFound
		}
		return nil, "", fmt.Errorf("failed to lookup user %s@%s: %w", username, domain, err)
	}

	user.DisplayName = displayName.String
	user.Email = fmt.Sprintf("%s@%s", user.Username, user.Domain)
	return &user, passwordHash, nil
}

// parseEmail splits an email address into local part and domain
func parseEmail(email string) (username, domain string, err error) {
	email = strings.TrimSpace(strings.ToLower(email))
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid email address: %s", email)
	}

	username = parts[0]
	domain = parts[1]

	// Validate components
	if err := ValidateUsername(username); err != nil {
		return "", "", fmt.Errorf("invalid email address: %w", err)
	}
	if err := ValidateDomain(domain); err != nil {
		return "", "", fmt.Errorf("invalid email address: %w", err)
	}

	return username, domain, nil
}

// validateUsername is an internal helper for validation
func validateUsername(username string) error {
	return ValidateUsername(username)
}

// ValidateUsername checks if a username (email local part) is valid
// Username must be 1-64 characters and match RFC 5321 local-part rules
func ValidateUsername(username string) error {
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

// validatePassword is an internal helper for validation
func validatePassword(password string) error {
	return ValidatePassword(password)
}

// ValidatePassword checks if a password meets security requirements
// Password must be 8-128 characters following NIST SP 800-63B recommendations
func ValidatePassword(password string) error {
	if len(password) < minPasswordLength || len(password) > maxPasswordLength {
		return ErrInvalidPassword
	}
	return nil
}

// validateDomain is an internal helper for validation
func validateDomain(domain string) error {
	return ValidateDomain(domain)
}

// ValidateDomain checks if a domain name is valid according to RFC 1035
func ValidateDomain(domain string) error {
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
