package sieve

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Store handles Sieve script database operations
type Store struct {
	db *sql.DB
}

// NewStore creates a new Sieve script store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// GetActiveScript returns the active Sieve script for a user
func (s *Store) GetActiveScript(ctx context.Context, userID int64) (*Script, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store or database is nil")
	}

	script := &Script{}

	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, content, is_active, created_at, updated_at
		FROM sieve_scripts
		WHERE user_id = ? AND is_active = TRUE
		LIMIT 1
	`, userID).Scan(
		&script.ID, &script.UserID, &script.Name, &script.Content,
		&script.IsActive, &script.CreatedAt, &script.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return script, nil
}

// GetScript returns a specific Sieve script by name
func (s *Store) GetScript(ctx context.Context, userID int64, name string) (*Script, error) {
	script := &Script{}

	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, content, is_active, created_at, updated_at
		FROM sieve_scripts
		WHERE user_id = ? AND name = ?
	`, userID, name).Scan(
		&script.ID, &script.UserID, &script.Name, &script.Content,
		&script.IsActive, &script.CreatedAt, &script.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return script, nil
}

// ListScripts returns all Sieve scripts for a user
func (s *Store) ListScripts(ctx context.Context, userID int64) ([]*Script, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, content, is_active, created_at, updated_at
		FROM sieve_scripts
		WHERE user_id = ?
		ORDER BY name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scripts []*Script
	for rows.Next() {
		script := &Script{}
		err := rows.Scan(
			&script.ID, &script.UserID, &script.Name, &script.Content,
			&script.IsActive, &script.CreatedAt, &script.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		scripts = append(scripts, script)
	}

	return scripts, rows.Err()
}

// CreateScript creates a new Sieve script
func (s *Store) CreateScript(ctx context.Context, userID int64, name, content string) (*Script, error) {
	// Validate the script first
	_, err := Parse(content)
	if err != nil {
		return nil, err
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO sieve_scripts (user_id, name, content, is_active, created_at, updated_at)
		VALUES (?, ?, ?, FALSE, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, userID, name, content)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &Script{
		ID:        id,
		UserID:    userID,
		Name:      name,
		Content:   content,
		IsActive:  false,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

// UpdateScript updates an existing Sieve script
func (s *Store) UpdateScript(ctx context.Context, userID int64, name, content string) error {
	// Validate the script first
	_, err := Parse(content)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE sieve_scripts
		SET content = ?, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND name = ?
	`, content, userID, name)
	return err
}

// DeleteScript deletes a Sieve script
func (s *Store) DeleteScript(ctx context.Context, userID int64, name string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM sieve_scripts
		WHERE user_id = ? AND name = ?
	`, userID, name)
	return err
}

// SetActiveScript sets which script is active for a user (deactivates others)
func (s *Store) SetActiveScript(ctx context.Context, userID int64, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Deactivate all scripts for this user
	_, err = tx.ExecContext(ctx, `
		UPDATE sieve_scripts
		SET is_active = FALSE, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ?
	`, userID)
	if err != nil {
		return err
	}

	// Activate the specified script (if name is not empty)
	if name != "" {
		_, err = tx.ExecContext(ctx, `
			UPDATE sieve_scripts
			SET is_active = TRUE, updated_at = CURRENT_TIMESTAMP
			WHERE user_id = ? AND name = ?
		`, userID, name)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// RenameScript renames a Sieve script
func (s *Store) RenameScript(ctx context.Context, userID int64, oldName, newName string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sieve_scripts
		SET name = ?, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND name = ?
	`, newName, userID, oldName)
	return err
}

// ScriptExists checks if a script with the given name exists
func (s *Store) ScriptExists(ctx context.Context, userID int64, name string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sieve_scripts
		WHERE user_id = ? AND name = ?
	`, userID, name).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CountScripts returns the number of scripts for a user
func (s *Store) CountScripts(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sieve_scripts
		WHERE user_id = ?
	`, userID).Scan(&count)
	return count, err
}

// ValidateScript validates a Sieve script without storing it
func ValidateScript(content string) error {
	_, err := Parse(content)
	return err
}
