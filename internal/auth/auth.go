package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
func (a *Authenticator) Authenticate(ctx context.Context, email, password string) (*User, error) {
	username, domain, err := parseEmail(email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	// Look up user
	user, passwordHash, err := a.lookupUserWithPassword(ctx, username, domain)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if !user.IsActive {
		return nil, ErrUserDisabled
	}

	// Verify password
	if !VerifyPassword(password, passwordHash) {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

// LookupUser finds a user by email address
func (a *Authenticator) LookupUser(ctx context.Context, email string) (*User, error) {
	username, domain, err := parseEmail(email)
	if err != nil {
		return nil, err
	}

	user, _, err := a.lookupUserWithPassword(ctx, username, domain)
	return user, err
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
		return nil, err
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
		return false, err
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
		return false, err
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
	return false, err
}

// ResolveAlias resolves an alias to its destination(s)
// Returns the user ID if it's a local alias, or the external address
func (a *Authenticator) ResolveAlias(ctx context.Context, email string) (userID *int64, external *string, err error) {
	username, domain, err := parseEmail(email)
	if err != nil {
		return nil, nil, err
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
		return nil, nil, err
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
	var id int64
	err := a.db.QueryRowContext(ctx,
		"SELECT id FROM domains WHERE name = ? AND is_active = TRUE",
		name,
	).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrDomainNotFound
		}
		return 0, err
	}
	return id, nil
}

// CreateUser creates a new user account
func (a *Authenticator) CreateUser(ctx context.Context, username, password string, domainID int64) (*User, error) {
	// Hash password
	passwordHash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Get domain name
	var domainName string
	err = a.db.QueryRowContext(ctx, "SELECT name FROM domains WHERE id = ?", domainID).Scan(&domainName)
	if err != nil {
		return nil, ErrDomainNotFound
	}

	email := fmt.Sprintf("%s@%s", strings.ToLower(username), domainName)

	result, err := a.db.ExecContext(ctx, `
		INSERT INTO users (domain_id, username, password_hash, email, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, TRUE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, domainID, strings.ToLower(username), passwordHash, email)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &User{
		ID:       id,
		DomainID: domainID,
		Username: strings.ToLower(username),
		Domain:   domainName,
		Email:    email,
		IsActive: true,
	}, nil
}

// UpdatePassword updates a user's password
func (a *Authenticator) UpdatePassword(ctx context.Context, userID int64, password string) error {
	passwordHash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	_, err = a.db.ExecContext(ctx, `
		UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, passwordHash, userID)
	return err
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
		return nil, "", err
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
	return parts[0], parts[1], nil
}
