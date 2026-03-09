// Package org provides multi-organization management for the email server.
package org

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Organization represents a tenant organization.
type Organization struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	OwnerUserID int64     `json:"owner_user_id"`
	Preset      string    `json:"preset"`
	Settings    Settings  `json:"settings"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Settings holds org-specific feature flags and limits.
type Settings struct {
	MaxUsers   int  `json:"max_users,omitempty"`
	MaxDomains int  `json:"max_domains,omitempty"`
	MaxAPIKeys int  `json:"max_api_keys,omitempty"`
	TrackingEnabled bool `json:"tracking_enabled,omitempty"`
}

// Member represents an organization member.
type Member struct {
	ID        int64     `json:"id"`
	OrgID     int64     `json:"org_id"`
	UserID    int64     `json:"user_id"`
	Role      string    `json:"role"`
	Username  string    `json:"username,omitempty"`
	Email     string    `json:"email,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Store provides organization CRUD operations.
type Store struct {
	db *sql.DB
}

// NewStore creates a new org store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

var slugRe = regexp.MustCompile(`[^a-z0-9-]`)

// slugify converts a name to a URL-safe slug.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRe.ReplaceAllString(s, "-")
	s = regexp.MustCompile(`-+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// Create creates a new organization.
func (s *Store) Create(ctx context.Context, name string, ownerUserID int64, preset string) (*Organization, error) {
	if name == "" {
		return nil, fmt.Errorf("organization name is required")
	}
	if preset == "" {
		preset = "full"
	}
	if _, ok := Presets[preset]; !ok {
		return nil, fmt.Errorf("invalid preset: %s", preset)
	}

	slug := slugify(name)
	if slug == "" {
		return nil, fmt.Errorf("organization name must contain alphanumeric characters")
	}

	settings, _ := json.Marshal(Settings{})
	now := time.Now()

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO organizations (name, slug, owner_user_id, preset, settings, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, slug, ownerUserID, preset, string(settings), now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("organization slug '%s' already exists", slug)
		}
		return nil, fmt.Errorf("failed to create organization: %w", err)
	}

	id, _ := res.LastInsertId()

	// Add owner as member
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, 'owner')`,
		id, ownerUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to add owner as member: %w", err)
	}

	return &Organization{
		ID:          id,
		Name:        name,
		Slug:        slug,
		OwnerUserID: ownerUserID,
		Preset:      preset,
		Settings:    Settings{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// Get returns an organization by ID.
func (s *Store) Get(ctx context.Context, id int64) (*Organization, error) {
	org := &Organization{}
	var settingsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, owner_user_id, preset, settings, created_at, updated_at
		 FROM organizations WHERE id = ?`, id,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.OwnerUserID, &org.Preset, &settingsJSON, &org.CreatedAt, &org.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("organization not found")
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(settingsJSON), &org.Settings)
	return org, nil
}

// GetBySlug returns an organization by slug.
func (s *Store) GetBySlug(ctx context.Context, slug string) (*Organization, error) {
	org := &Organization{}
	var settingsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, slug, owner_user_id, preset, settings, created_at, updated_at
		 FROM organizations WHERE slug = ?`, slug,
	).Scan(&org.ID, &org.Name, &org.Slug, &org.OwnerUserID, &org.Preset, &settingsJSON, &org.CreatedAt, &org.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("organization not found")
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(settingsJSON), &org.Settings)
	return org, nil
}

// List returns all organizations.
func (s *Store) List(ctx context.Context) ([]Organization, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, slug, owner_user_id, preset, settings, created_at, updated_at
		 FROM organizations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []Organization
	for rows.Next() {
		var o Organization
		var settingsJSON string
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.OwnerUserID, &o.Preset, &settingsJSON, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(settingsJSON), &o.Settings)
		orgs = append(orgs, o)
	}
	return orgs, nil
}

// ListByUser returns all organizations a user belongs to.
func (s *Store) ListByUser(ctx context.Context, userID int64) ([]Organization, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT o.id, o.name, o.slug, o.owner_user_id, o.preset, o.settings, o.created_at, o.updated_at
		 FROM organizations o
		 JOIN org_members m ON o.id = m.org_id
		 WHERE m.user_id = ?
		 ORDER BY o.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []Organization
	for rows.Next() {
		var o Organization
		var settingsJSON string
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.OwnerUserID, &o.Preset, &settingsJSON, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(settingsJSON), &o.Settings)
		orgs = append(orgs, o)
	}
	return orgs, nil
}

// Update updates an organization's name, preset, and settings.
func (s *Store) Update(ctx context.Context, id int64, name, preset string, settings *Settings) error {
	if preset != "" {
		if _, ok := Presets[preset]; !ok {
			return fmt.Errorf("invalid preset: %s", preset)
		}
	}

	setClauses := []string{"updated_at = ?"}
	args := []interface{}{time.Now()}

	if name != "" {
		setClauses = append(setClauses, "name = ?", "slug = ?")
		args = append(args, name, slugify(name))
	}
	if preset != "" {
		setClauses = append(setClauses, "preset = ?")
		args = append(args, preset)
	}
	if settings != nil {
		settingsJSON, _ := json.Marshal(settings)
		setClauses = append(setClauses, "settings = ?")
		args = append(args, string(settingsJSON))
	}

	args = append(args, id)
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf("UPDATE organizations SET %s WHERE id = ?", strings.Join(setClauses, ", ")),
		args...,
	)
	return err
}

// Delete deletes an organization. Cannot delete the last org.
func (s *Store) Delete(ctx context.Context, id int64) error {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM organizations").Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return fmt.Errorf("cannot delete the last organization")
	}

	_, err := s.db.ExecContext(ctx, "DELETE FROM organizations WHERE id = ?", id)
	return err
}

// AddMember adds a user to an organization.
func (s *Store) AddMember(ctx context.Context, orgID, userID int64, role string) error {
	if role == "" {
		role = "member"
	}
	if role != "owner" && role != "admin" && role != "member" {
		return fmt.Errorf("invalid role: %s (must be owner, admin, or member)", role)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?)
		 ON CONFLICT(org_id, user_id) DO UPDATE SET role = ?`,
		orgID, userID, role, role,
	)
	return err
}

// RemoveMember removes a user from an organization. Cannot remove the owner.
func (s *Store) RemoveMember(ctx context.Context, orgID, userID int64) error {
	var role string
	err := s.db.QueryRowContext(ctx,
		"SELECT role FROM org_members WHERE org_id = ? AND user_id = ?",
		orgID, userID,
	).Scan(&role)
	if err == sql.ErrNoRows {
		return fmt.Errorf("user is not a member of this organization")
	}
	if err != nil {
		return err
	}
	if role == "owner" {
		return fmt.Errorf("cannot remove the organization owner")
	}

	_, err = s.db.ExecContext(ctx,
		"DELETE FROM org_members WHERE org_id = ? AND user_id = ?",
		orgID, userID,
	)
	return err
}

// UpdateMemberRole changes a member's role.
func (s *Store) UpdateMemberRole(ctx context.Context, orgID, userID int64, newRole string) error {
	if newRole != "owner" && newRole != "admin" && newRole != "member" {
		return fmt.Errorf("invalid role: %s", newRole)
	}

	result, err := s.db.ExecContext(ctx,
		"UPDATE org_members SET role = ? WHERE org_id = ? AND user_id = ?",
		newRole, orgID, userID,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("member not found")
	}
	return nil
}

// ListMembers returns all members of an organization.
func (s *Store) ListMembers(ctx context.Context, orgID int64) ([]Member, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.org_id, m.user_id, m.role, u.username, d.name, m.created_at
		 FROM org_members m
		 JOIN users u ON m.user_id = u.id
		 JOIN domains d ON u.domain_id = d.id
		 WHERE m.org_id = ?
		 ORDER BY m.role, u.username`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		var domain string
		if err := rows.Scan(&m.ID, &m.OrgID, &m.UserID, &m.Role, &m.Username, &domain, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Email = m.Username + "@" + domain
		members = append(members, m)
	}
	return members, nil
}

// GetUserRole returns the user's role in an organization.
func (s *Store) GetUserRole(ctx context.Context, orgID, userID int64) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx,
		"SELECT role FROM org_members WHERE org_id = ? AND user_id = ?",
		orgID, userID,
	).Scan(&role)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("user is not a member of this organization")
	}
	return role, err
}

// GetDefaultOrg returns the default organization (id=1).
func (s *Store) GetDefaultOrg(ctx context.Context) (*Organization, error) {
	return s.Get(ctx, 1)
}

// EnsureDefaultOrg creates the default org if it doesn't exist.
func (s *Store) EnsureDefaultOrg(ctx context.Context) error {
	_, err := s.Get(ctx, 1)
	if err == nil {
		return nil // already exists
	}

	// Find the first admin user
	var ownerID int64
	err = s.db.QueryRowContext(ctx,
		"SELECT id FROM users WHERE is_admin = 1 ORDER BY id LIMIT 1",
	).Scan(&ownerID)
	if err != nil {
		ownerID = 1 // fallback
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO organizations (id, name, slug, owner_user_id, preset, settings)
		 VALUES (1, 'Default', 'default', ?, 'full', '{}')`, ownerID)
	return err
}
