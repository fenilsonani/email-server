package maildir

import (
	"context"
	"fmt"
	"sync"

	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/storage"
)

// EnsureUserMailboxes ensures all users have their required default mailboxes.
// This is useful for migrating users who were created before certain mailbox types were added.
func (s *Store) EnsureUserMailboxes(ctx context.Context, logger *logging.Logger) error {
	// Get all users
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM users ORDER BY id")
	if err != nil {
		return fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()

	var userIDs []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return fmt.Errorf("failed to scan user ID: %w", err)
		}
		userIDs = append(userIDs, userID)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating users: %w", err)
	}

	// Define the mailboxes that should exist for every user
	requiredMailboxes := []struct {
		name       string
		specialUse storage.SpecialUse
	}{
		{"INBOX", ""},
		{"Drafts", storage.SpecialUseDrafts},
		{"Sent", storage.SpecialUseSent},
		{"Junk", storage.SpecialUseJunk},
		{"Trash", storage.SpecialUseTrash},
		{"Archive", storage.SpecialUseArchive},
		{"Screener", ""}, // For first-contact filtering feature
	}

	// Track progress
	var wg sync.WaitGroup
	errChan := make(chan error, len(userIDs))
	const maxConcurrentUsers = 10
	semaphore := make(chan struct{}, maxConcurrentUsers)

	for _, userID := range userIDs {
		wg.Add(1)
		go func(uid int64) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := s.ensureUserHasMailboxes(ctx, uid, requiredMailboxes); err != nil {
				errChan <- fmt.Errorf("user %d: %w", uid, err)
				if logger != nil {
					logger.Error("Failed to ensure mailboxes for user", "user_id", uid, "error", err)
				}
				return
			}

			if logger != nil {
				logger.Debug("Ensured mailboxes for user", "user_id", uid)
			}
		}(userID)
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors ensuring user mailboxes: %v", len(errors), errors[0])
	}

	return nil
}

// ensureUserHasMailboxes creates any missing default mailboxes for a user.
// NOTE: This function must NOT hold s.mu because CreateMailbox acquires it.
// Go's sync.Mutex is not reentrant — locking here and inside CreateMailbox
// would cause a deadlock. SQLite serializes writes, so the existence check
// is safe without the mutex.
func (s *Store) ensureUserHasMailboxes(ctx context.Context, userID int64, requiredMailboxes []struct {
	name       string
	specialUse storage.SpecialUse
}) error {
	for _, mb := range requiredMailboxes {
		// Check if mailbox exists
		var exists bool
		err := s.db.QueryRowContext(ctx,
			"SELECT COUNT(*) > 0 FROM mailboxes WHERE user_id = ? AND name = ?",
			userID, mb.name,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check if mailbox %s exists: %w", mb.name, err)
		}

		if !exists {
			// CreateMailbox acquires s.mu internally
			if _, err := s.CreateMailbox(ctx, userID, mb.name, mb.specialUse); err != nil {
				return fmt.Errorf("failed to create mailbox %s: %w", mb.name, err)
			}
		}
	}

	return nil
}
