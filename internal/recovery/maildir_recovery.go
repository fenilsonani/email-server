package recovery

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// RecoverMaildirEmails scans the maildir directory and rebuilds the email index in the database.
// This is useful when emails exist on disk but are missing from the database (e.g., after partial backup restore).
func RecoverMaildirEmails(ctx context.Context, db *sql.DB, maildirBasePath string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	logger.Info("Starting maildir recovery", "basePath", maildirBasePath)

	// Get all users from database
	rows, err := db.QueryContext(ctx, "SELECT id FROM users ORDER BY id")
	if err != nil {
		return fmt.Errorf("failed to query users: %w", err)
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

	logger.Info("Found users to recover", "count", len(userIDs))

	totalRecovered := 0

	// Process each user's maildir
	for _, userID := range userIDs {
		userDir := filepath.Join(maildirBasePath, fmt.Sprintf("user_%d", userID))

		// Check if user directory exists
		if _, err := os.Stat(userDir); os.IsNotExist(err) {
			logger.Debug("User directory not found", "user_id", userID, "path", userDir)
			continue
		}

		// List mailboxes for this user
		entries, err := os.ReadDir(userDir)
		if err != nil {
			logger.Warn("Failed to read user directory", "user_id", userID, "error", err)
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			mailboxName := entry.Name()

			// Get or create mailbox in database
			mailboxID, err := getOrCreateMailbox(ctx, db, userID, mailboxName)
			if err != nil {
				logger.Warn("Failed to get/create mailbox", "user_id", userID, "mailbox", mailboxName, "error", err)
				continue
			}

			// Scan mailbox for messages
			recovered, err := recoverMailboxMessages(ctx, db, userDir, mailboxName, mailboxID, logger)
			if err != nil {
				logger.Warn("Failed to recover mailbox messages", "user_id", userID, "mailbox", mailboxName, "error", err)
				continue
			}

			if recovered > 0 {
				logger.Info("Recovered messages", "user_id", userID, "mailbox", mailboxName, "count", recovered)
				totalRecovered += recovered
			}
		}
	}

	logger.Info("Maildir recovery completed", "totalRecovered", totalRecovered)
	return nil
}

// getOrCreateMailbox gets the mailbox ID or creates it if it doesn't exist
func getOrCreateMailbox(ctx context.Context, db *sql.DB, userID int64, mailboxName string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, "SELECT id FROM mailboxes WHERE user_id = ? AND name = ?", userID, mailboxName).Scan(&id)
	if err == nil {
		return id, nil // Mailbox exists
	}

	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("query error: %w", err)
	}

	// Create mailbox
	result, err := db.ExecContext(ctx,
		`INSERT INTO mailboxes (user_id, name, uidvalidity, uidnext, subscribed)
		 VALUES (?, ?, ?, 1, TRUE)`,
		userID, mailboxName, generateUIDValidity(),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to create mailbox: %w", err)
	}

	id, err = result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return id, nil
}

// recoverMailboxMessages scans a mailbox directory and adds missing messages to the database
func recoverMailboxMessages(ctx context.Context, db *sql.DB, userDir, mailboxName string, mailboxID int64, logger *slog.Logger) (int, error) {
	mailboxPath := filepath.Join(userDir, mailboxName)
	var recovered int

	// Scan both 'cur' and 'new' directories (messages in 'tmp' are incomplete)
	for _, subdir := range []string{"cur", "new"} {
		subdirPath := filepath.Join(mailboxPath, subdir)

		entries, err := os.ReadDir(subdirPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Directory doesn't exist, skip
			}
			return recovered, fmt.Errorf("failed to read directory %s: %w", subdirPath, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			filename := entry.Name()

			// Check if this message is already in the database
			var exists bool
			err := db.QueryRowContext(ctx, "SELECT COUNT(*) > 0 FROM messages WHERE mailbox_id = ? AND maildir_key = ?", mailboxID, filename).Scan(&exists)
			if err != nil {
				logger.Warn("Failed to check message existence", "filename", filename, "error", err)
				continue
			}

			if exists {
				continue // Message already indexed
			}

			// Get file size
			fileInfo, err := os.Stat(filepath.Join(subdirPath, filename))
			if err != nil {
				logger.Warn("Failed to stat file", "filename", filename, "error", err)
				continue
			}

			// Extract flags from filename (maildir format: key:2,FLAGS)
			flags := ""
			if strings.Contains(filename, ":2,") {
				parts := strings.Split(filename, ":2,")
				if len(parts) > 1 {
					flags = parseMaildirFlags(parts[1])
				}
			}

			// Get the current UIDNext for this mailbox
			var uidNext int64
			err = db.QueryRowContext(ctx, "SELECT uidnext FROM mailboxes WHERE id = ?", mailboxID).Scan(&uidNext)
			if err != nil {
				logger.Warn("Failed to get mailbox uidnext", "mailbox_id", mailboxID, "error", err)
				continue
			}

			// Add message to database
			_, err = db.ExecContext(ctx,
				`INSERT INTO messages (mailbox_id, uid, maildir_key, size, internal_date, flags)
				 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
				mailboxID, uidNext, filename, fileInfo.Size(), flags,
			)
			if err != nil {
				logger.Warn("Failed to insert message", "filename", filename, "error", err)
				continue
			}

			// Update mailbox uidnext
			_, err = db.ExecContext(ctx, "UPDATE mailboxes SET uidnext = uidnext + 1 WHERE id = ?", mailboxID)
			if err != nil {
				logger.Warn("Failed to update uidnext", "mailbox_id", mailboxID, "error", err)
				// Continue anyway - message is indexed even if uidnext update failed
			}

			recovered++
		}
	}

	return recovered, nil
}

// parseMaildirFlags converts maildir flag string (e.g., "S" for \Seen) to comma-separated flag list
func parseMaildirFlags(flagStr string) string {
	var flags []string

	if strings.Contains(flagStr, "S") {
		flags = append(flags, `\Seen`)
	}
	if strings.Contains(flagStr, "R") {
		flags = append(flags, `\Answered`)
	}
	if strings.Contains(flagStr, "F") {
		flags = append(flags, `\Flagged`)
	}
	if strings.Contains(flagStr, "T") {
		flags = append(flags, `\Deleted`)
	}
	if strings.Contains(flagStr, "D") {
		flags = append(flags, `\Draft`)
	}

	return strings.Join(flags, ",")
}

// generateUIDValidity generates a UID validity value for a new mailbox
func generateUIDValidity() uint32 {
	return uint32(os.Getpid())
}
