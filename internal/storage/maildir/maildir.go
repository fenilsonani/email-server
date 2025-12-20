package maildir

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-maildir"
	"github.com/fenilsonani/email-server/internal/storage"
)

// Store implements storage.MessageStore using Maildir format
type Store struct {
	db          *sql.DB
	basePath    string
	mu          sync.RWMutex
	maildirDirs map[int64]*maildir.Dir // userID -> maildir.Dir
}

// NewStore creates a new Maildir-based message store
func NewStore(db *sql.DB, basePath string) (*Store, error) {
	if err := os.MkdirAll(basePath, 0750); err != nil {
		return nil, fmt.Errorf("failed to create maildir base: %w", err)
	}

	return &Store{
		db:          db,
		basePath:    basePath,
		maildirDirs: make(map[int64]*maildir.Dir),
	}, nil
}

// getUserMaildirPath returns the path for a user's maildir
func (s *Store) getUserMaildirPath(userID int64, mailboxName string) string {
	// Convert mailbox name to safe filesystem path
	safeName := strings.ReplaceAll(mailboxName, "/", ".")
	return filepath.Join(s.basePath, fmt.Sprintf("user_%d", userID), safeName)
}

// ensureMaildir creates the maildir structure if it doesn't exist
func (s *Store) ensureMaildir(path string) (*maildir.Dir, error) {
	dir := maildir.Dir(path)

	// Create maildir directories
	for _, subdir := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(path, subdir), 0750); err != nil {
			return nil, fmt.Errorf("failed to create %s: %w", subdir, err)
		}
	}

	return &dir, nil
}

// CreateMailbox creates a new mailbox for a user
func (s *Store) CreateMailbox(ctx context.Context, userID int64, name string, specialUse storage.SpecialUse) (*storage.Mailbox, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate UID validity
	uidValidity := uint32(time.Now().Unix())

	// Insert into database
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO mailboxes (user_id, name, uidvalidity, uidnext, special_use, subscribed)
		 VALUES (?, ?, ?, 1, ?, TRUE)`,
		userID, name, uidValidity, string(specialUse),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create mailbox: %w", err)
	}

	id, _ := result.LastInsertId()

	// Create maildir on filesystem
	path := s.getUserMaildirPath(userID, name)
	if _, err := s.ensureMaildir(path); err != nil {
		// Rollback database insert
		s.db.ExecContext(ctx, "DELETE FROM mailboxes WHERE id = ?", id)
		return nil, err
	}

	return &storage.Mailbox{
		ID:          id,
		UserID:      userID,
		Name:        name,
		UIDValidity: uidValidity,
		UIDNext:     1,
		SpecialUse:  specialUse,
		Subscribed:  true,
		CreatedAt:   time.Now(),
	}, nil
}

// GetMailbox retrieves a mailbox by name
func (s *Store) GetMailbox(ctx context.Context, userID int64, name string) (*storage.Mailbox, error) {
	var mb storage.Mailbox
	var specialUse sql.NullString

	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, uidvalidity, uidnext, special_use, subscribed, created_at
		 FROM mailboxes WHERE user_id = ? AND name = ?`,
		userID, name,
	).Scan(&mb.ID, &mb.UserID, &mb.Name, &mb.UIDValidity, &mb.UIDNext,
		&specialUse, &mb.Subscribed, &mb.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("mailbox not found: %s", name)
		}
		return nil, err
	}

	mb.SpecialUse = storage.SpecialUse(specialUse.String)
	return &mb, nil
}

// GetMailboxByID retrieves a mailbox by ID
func (s *Store) GetMailboxByID(ctx context.Context, id int64) (*storage.Mailbox, error) {
	var mb storage.Mailbox
	var specialUse sql.NullString

	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, uidvalidity, uidnext, special_use, subscribed, created_at
		 FROM mailboxes WHERE id = ?`,
		id,
	).Scan(&mb.ID, &mb.UserID, &mb.Name, &mb.UIDValidity, &mb.UIDNext,
		&specialUse, &mb.Subscribed, &mb.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("mailbox not found: %d", id)
		}
		return nil, err
	}

	mb.SpecialUse = storage.SpecialUse(specialUse.String)
	return &mb, nil
}

// ListMailboxes returns all mailboxes for a user
func (s *Store) ListMailboxes(ctx context.Context, userID int64) ([]*storage.Mailbox, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, uidvalidity, uidnext, special_use, subscribed, created_at
		 FROM mailboxes WHERE user_id = ? ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mailboxes []*storage.Mailbox
	for rows.Next() {
		var mb storage.Mailbox
		var specialUse sql.NullString

		if err := rows.Scan(&mb.ID, &mb.UserID, &mb.Name, &mb.UIDValidity, &mb.UIDNext,
			&specialUse, &mb.Subscribed, &mb.CreatedAt); err != nil {
			return nil, err
		}

		mb.SpecialUse = storage.SpecialUse(specialUse.String)
		mailboxes = append(mailboxes, &mb)
	}

	return mailboxes, rows.Err()
}

// RenameMailbox renames a mailbox
func (s *Store) RenameMailbox(ctx context.Context, userID int64, oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update database
	result, err := s.db.ExecContext(ctx,
		"UPDATE mailboxes SET name = ? WHERE user_id = ? AND name = ?",
		newName, userID, oldName,
	)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("mailbox not found: %s", oldName)
	}

	// Rename on filesystem
	oldPath := s.getUserMaildirPath(userID, oldName)
	newPath := s.getUserMaildirPath(userID, newName)

	if err := os.Rename(oldPath, newPath); err != nil && !os.IsNotExist(err) {
		// Rollback database change
		s.db.ExecContext(ctx, "UPDATE mailboxes SET name = ? WHERE user_id = ? AND name = ?",
			oldName, userID, newName)
		return fmt.Errorf("failed to rename maildir: %w", err)
	}

	return nil
}

// DeleteMailbox removes a mailbox and all its messages
func (s *Store) DeleteMailbox(ctx context.Context, userID int64, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get mailbox ID first
	var mailboxID int64
	err := s.db.QueryRowContext(ctx,
		"SELECT id FROM mailboxes WHERE user_id = ? AND name = ?",
		userID, name,
	).Scan(&mailboxID)
	if err != nil {
		return fmt.Errorf("mailbox not found: %s", name)
	}

	// Delete messages from database (cascade should handle this)
	if _, err := s.db.ExecContext(ctx, "DELETE FROM mailboxes WHERE id = ?", mailboxID); err != nil {
		return err
	}

	// Remove from filesystem
	path := s.getUserMaildirPath(userID, name)
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove maildir: %w", err)
	}

	return nil
}

// SubscribeMailbox updates mailbox subscription status
func (s *Store) SubscribeMailbox(ctx context.Context, userID int64, name string, subscribed bool) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE mailboxes SET subscribed = ? WHERE user_id = ? AND name = ?",
		subscribed, userID, name,
	)
	return err
}

// AppendMessage stores a new message in the mailbox
func (s *Store) AppendMessage(ctx context.Context, mailboxID int64, flags []storage.Flag, date time.Time, body io.Reader) (*storage.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get mailbox info
	mb, err := s.GetMailboxByID(ctx, mailboxID)
	if err != nil {
		return nil, err
	}

	// Generate unique maildir key
	key := generateMaildirKey()

	// Get maildir path
	path := s.getUserMaildirPath(mb.UserID, mb.Name)
	dir, err := s.ensureMaildir(path)
	if err != nil {
		return nil, err
	}

	// Write to tmp first
	tmpPath := filepath.Join(path, "tmp", key)
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create tmp file: %w", err)
	}

	size, err := io.Copy(f, body)
	if err != nil {
		f.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to write message: %w", err)
	}
	f.Close()

	// Determine destination (new or cur based on \Seen flag)
	destDir := "new"
	for _, flag := range flags {
		if flag == storage.FlagSeen {
			destDir = "cur"
			break
		}
	}

	// Build maildir flags suffix
	flagSuffix := buildMaildirFlags(flags)
	finalKey := key
	if flagSuffix != "" {
		finalKey = key + ":2," + flagSuffix
	}

	// Move to destination
	destPath := filepath.Join(path, destDir, finalKey)
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("failed to move message: %w", err)
	}

	// Get next UID
	uid := mb.UIDNext

	// Update UID next
	_, err = s.db.ExecContext(ctx,
		"UPDATE mailboxes SET uidnext = uidnext + 1 WHERE id = ?",
		mailboxID,
	)
	if err != nil {
		// Try to clean up
		os.Remove(destPath)
		return nil, err
	}

	// Parse message headers for metadata (simplified)
	// In production, you'd parse the message properly
	flagsStr := flagsToString(flags)

	// Insert message metadata
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO messages (mailbox_id, uid, maildir_key, size, internal_date, flags)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		mailboxID, uid, finalKey, size, date, flagsStr,
	)
	if err != nil {
		os.Remove(destPath)
		return nil, err
	}

	msgID, _ := result.LastInsertId()

	// Notify IDLE listeners via the maildir
	dir.Unseen()

	return &storage.Message{
		ID:           msgID,
		MailboxID:    mailboxID,
		UID:          uid,
		MaildirKey:   finalKey,
		Size:         size,
		InternalDate: date,
		Flags:        flags,
		CreatedAt:    time.Now(),
	}, nil
}

// GetMessage retrieves message metadata by UID
func (s *Store) GetMessage(ctx context.Context, mailboxID int64, uid uint32) (*storage.Message, error) {
	var msg storage.Message
	var flagsStr string
	var messageID, subject, fromAddr, toAddrs sql.NullString

	err := s.db.QueryRowContext(ctx,
		`SELECT id, mailbox_id, uid, maildir_key, size, internal_date, flags,
		        message_id, subject, from_address, to_addresses, created_at
		 FROM messages WHERE mailbox_id = ? AND uid = ?`,
		mailboxID, uid,
	).Scan(&msg.ID, &msg.MailboxID, &msg.UID, &msg.MaildirKey, &msg.Size,
		&msg.InternalDate, &flagsStr, &messageID, &subject,
		&fromAddr, &toAddrs, &msg.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("message not found: %d", uid)
		}
		return nil, err
	}

	msg.MessageID = messageID.String
	msg.Subject = subject.String
	msg.From = fromAddr.String
	if toAddrs.Valid {
		json.Unmarshal([]byte(toAddrs.String), &msg.To)
	}
	msg.Flags = stringToFlags(flagsStr)
	return &msg, nil
}

// GetMessageBody retrieves the message content from the filesystem
func (s *Store) GetMessageBody(ctx context.Context, msg *storage.Message) (io.ReadCloser, error) {
	// Get mailbox to find path
	mb, err := s.GetMailboxByID(ctx, msg.MailboxID)
	if err != nil {
		return nil, err
	}

	path := s.getUserMaildirPath(mb.UserID, mb.Name)

	// Extract base key (without :2,FLAGS suffix)
	baseKey := msg.MaildirKey
	if idx := strings.Index(baseKey, ":2,"); idx >= 0 {
		baseKey = baseKey[:idx]
	}

	// Try cur first, then new - search by base key prefix
	for _, subdir := range []string{"cur", "new"} {
		subdirPath := filepath.Join(path, subdir)
		entries, err := os.ReadDir(subdirPath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			name := entry.Name()
			// Match if it's the exact key or starts with baseKey: or baseKey:2,
			if name == msg.MaildirKey || name == baseKey || strings.HasPrefix(name, baseKey+":") {
				fullPath := filepath.Join(subdirPath, name)
				if f, err := os.Open(fullPath); err == nil {
					return f, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("message file not found: %s", msg.MaildirKey)
}

// ListMessages returns messages in a UID range
func (s *Store) ListMessages(ctx context.Context, mailboxID int64, start, end uint32) ([]*storage.Message, error) {
	query := `SELECT id, mailbox_id, uid, maildir_key, size, internal_date, flags,
	                 message_id, subject, from_address, to_addresses, created_at
	          FROM messages WHERE mailbox_id = ?`

	args := []interface{}{mailboxID}
	if start > 0 {
		query += " AND uid >= ?"
		args = append(args, start)
	}
	if end > 0 {
		query += " AND uid <= ?"
		args = append(args, end)
	}
	query += " ORDER BY uid"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*storage.Message
	for rows.Next() {
		var msg storage.Message
		var flagsStr string
		var messageID, subject, fromAddr, toAddrs sql.NullString

		if err := rows.Scan(&msg.ID, &msg.MailboxID, &msg.UID, &msg.MaildirKey,
			&msg.Size, &msg.InternalDate, &flagsStr, &messageID,
			&subject, &fromAddr, &toAddrs, &msg.CreatedAt); err != nil {
			return nil, err
		}

		msg.MessageID = messageID.String
		msg.Subject = subject.String
		msg.From = fromAddr.String

		msg.Flags = stringToFlags(flagsStr)
		if toAddrs.Valid {
			json.Unmarshal([]byte(toAddrs.String), &msg.To)
		}
		messages = append(messages, &msg)
	}

	return messages, rows.Err()
}

// UpdateFlags adds or removes flags from a message
func (s *Store) UpdateFlags(ctx context.Context, mailboxID int64, uid uint32, flags []storage.Flag, add bool) error {
	msg, err := s.GetMessage(ctx, mailboxID, uid)
	if err != nil {
		return err
	}

	var newFlags []storage.Flag
	if add {
		// Add flags (avoiding duplicates)
		flagSet := make(map[storage.Flag]bool)
		for _, f := range msg.Flags {
			flagSet[f] = true
		}
		for _, f := range flags {
			flagSet[f] = true
		}
		for f := range flagSet {
			newFlags = append(newFlags, f)
		}
	} else {
		// Remove flags
		removeSet := make(map[storage.Flag]bool)
		for _, f := range flags {
			removeSet[f] = true
		}
		for _, f := range msg.Flags {
			if !removeSet[f] {
				newFlags = append(newFlags, f)
			}
		}
	}

	return s.SetFlags(ctx, mailboxID, uid, newFlags)
}

// SetFlags sets the exact flags for a message
func (s *Store) SetFlags(ctx context.Context, mailboxID int64, uid uint32, flags []storage.Flag) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg, err := s.GetMessage(ctx, mailboxID, uid)
	if err != nil {
		return err
	}

	// Update database
	flagsStr := flagsToString(flags)
	_, err = s.db.ExecContext(ctx,
		"UPDATE messages SET flags = ? WHERE mailbox_id = ? AND uid = ?",
		flagsStr, mailboxID, uid,
	)
	if err != nil {
		return err
	}

	// Update maildir filename with new flags
	mb, err := s.GetMailboxByID(ctx, mailboxID)
	if err != nil {
		return err
	}

	path := s.getUserMaildirPath(mb.UserID, mb.Name)

	// Find current file
	var oldPath string
	for _, subdir := range []string{"cur", "new"} {
		p := filepath.Join(path, subdir, msg.MaildirKey)
		if _, err := os.Stat(p); err == nil {
			oldPath = p
			break
		}
	}

	if oldPath == "" {
		return nil // File not found, skip rename
	}

	// Build new filename
	baseKey := msg.MaildirKey
	if idx := strings.Index(baseKey, ":2,"); idx >= 0 {
		baseKey = baseKey[:idx]
	}

	flagSuffix := buildMaildirFlags(flags)
	newKey := baseKey
	if flagSuffix != "" {
		newKey = baseKey + ":2," + flagSuffix
	}

	// Determine destination directory
	destDir := "new"
	for _, f := range flags {
		if f == storage.FlagSeen {
			destDir = "cur"
			break
		}
	}

	newPath := filepath.Join(path, destDir, newKey)

	if oldPath != newPath {
		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("failed to rename message: %w", err)
		}

		// Update maildir_key in database
		s.db.ExecContext(ctx,
			"UPDATE messages SET maildir_key = ? WHERE mailbox_id = ? AND uid = ?",
			newKey, mailboxID, uid,
		)
	}

	return nil
}

// DeleteMessage marks a message for deletion (sets \Deleted flag)
func (s *Store) DeleteMessage(ctx context.Context, mailboxID int64, uid uint32) error {
	return s.UpdateFlags(ctx, mailboxID, uid, []storage.Flag{storage.FlagDeleted}, true)
}

// CopyMessage copies a message to another mailbox
func (s *Store) CopyMessage(ctx context.Context, srcMailboxID int64, uid uint32, destMailboxID int64) (*storage.Message, error) {
	// Get source message
	srcMsg, err := s.GetMessage(ctx, srcMailboxID, uid)
	if err != nil {
		return nil, err
	}

	// Get message body
	body, err := s.GetMessageBody(ctx, srcMsg)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	// Remove \Recent flag for copy
	flags := make([]storage.Flag, 0, len(srcMsg.Flags))
	for _, f := range srcMsg.Flags {
		if f != storage.FlagRecent {
			flags = append(flags, f)
		}
	}

	// Append to destination
	return s.AppendMessage(ctx, destMailboxID, flags, srcMsg.InternalDate, body)
}

// MoveMessage moves a message to another mailbox
func (s *Store) MoveMessage(ctx context.Context, srcMailboxID int64, uid uint32, destMailboxID int64) (*storage.Message, error) {
	newMsg, err := s.CopyMessage(ctx, srcMailboxID, uid, destMailboxID)
	if err != nil {
		return nil, err
	}

	// Delete from source
	if err := s.expungeMessage(ctx, srcMailboxID, uid); err != nil {
		return newMsg, err // Return new message even if delete fails
	}

	return newMsg, nil
}

// ExpungeMailbox permanently removes messages marked \Deleted
func (s *Store) ExpungeMailbox(ctx context.Context, mailboxID int64) ([]uint32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find messages with \Deleted flag
	rows, err := s.db.QueryContext(ctx,
		"SELECT uid, maildir_key FROM messages WHERE mailbox_id = ? AND flags LIKE '%\\Deleted%'",
		mailboxID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	mb, err := s.GetMailboxByID(ctx, mailboxID)
	if err != nil {
		return nil, err
	}

	path := s.getUserMaildirPath(mb.UserID, mb.Name)

	var expunged []uint32
	for rows.Next() {
		var uid uint32
		var key string
		if err := rows.Scan(&uid, &key); err != nil {
			continue
		}

		// Remove file
		for _, subdir := range []string{"cur", "new"} {
			filePath := filepath.Join(path, subdir, key)
			os.Remove(filePath)
		}

		expunged = append(expunged, uid)
	}

	// Delete from database
	if len(expunged) > 0 {
		_, err = s.db.ExecContext(ctx,
			"DELETE FROM messages WHERE mailbox_id = ? AND flags LIKE '%\\Deleted%'",
			mailboxID,
		)
	}

	return expunged, err
}

// expungeMessage permanently removes a single message
func (s *Store) expungeMessage(ctx context.Context, mailboxID int64, uid uint32) error {
	msg, err := s.GetMessage(ctx, mailboxID, uid)
	if err != nil {
		return err
	}

	mb, err := s.GetMailboxByID(ctx, msg.MailboxID)
	if err != nil {
		return err
	}

	path := s.getUserMaildirPath(mb.UserID, mb.Name)

	// Remove file
	for _, subdir := range []string{"cur", "new"} {
		filePath := filepath.Join(path, subdir, msg.MaildirKey)
		os.Remove(filePath)
	}

	// Delete from database
	_, err = s.db.ExecContext(ctx, "DELETE FROM messages WHERE mailbox_id = ? AND uid = ?",
		mailboxID, uid)
	return err
}

// SearchMessages searches for messages matching criteria
func (s *Store) SearchMessages(ctx context.Context, mailboxID int64, criteria *storage.SearchCriteria) ([]uint32, error) {
	query := "SELECT uid FROM messages WHERE mailbox_id = ?"
	args := []interface{}{mailboxID}

	if criteria != nil {
		if criteria.Since != nil {
			query += " AND internal_date >= ?"
			args = append(args, criteria.Since)
		}
		if criteria.Before != nil {
			query += " AND internal_date < ?"
			args = append(args, criteria.Before)
		}
		if criteria.From != "" {
			query += " AND from_address LIKE ?"
			args = append(args, "%"+criteria.From+"%")
		}
		if criteria.To != "" {
			query += " AND to_addresses LIKE ?"
			args = append(args, "%"+criteria.To+"%")
		}
		if criteria.Subject != "" {
			query += " AND subject LIKE ?"
			args = append(args, "%"+criteria.Subject+"%")
		}
		if criteria.Larger > 0 {
			query += " AND size > ?"
			args = append(args, criteria.Larger)
		}
		if criteria.Smaller > 0 {
			query += " AND size < ?"
			args = append(args, criteria.Smaller)
		}
		for _, flag := range criteria.Flags {
			query += " AND flags LIKE ?"
			args = append(args, "%"+string(flag)+"%")
		}
		for _, flag := range criteria.NotFlags {
			query += " AND flags NOT LIKE ?"
			args = append(args, "%"+string(flag)+"%")
		}
	}

	query += " ORDER BY uid"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var uids []uint32
	for rows.Next() {
		var uid uint32
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		uids = append(uids, uid)
	}

	return uids, rows.Err()
}

// GetMailboxStats returns statistics for a mailbox
func (s *Store) GetMailboxStats(ctx context.Context, mailboxID int64) (*storage.MailboxStats, error) {
	mb, err := s.GetMailboxByID(ctx, mailboxID)
	if err != nil {
		return nil, err
	}

	var stats storage.MailboxStats
	stats.UIDValidity = mb.UIDValidity
	stats.UIDNext = mb.UIDNext

	// Count messages
	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM messages WHERE mailbox_id = ?",
		mailboxID,
	).Scan(&stats.Messages)
	if err != nil {
		return nil, err
	}

	// Count unseen
	err = s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM messages WHERE mailbox_id = ? AND flags NOT LIKE '%\\Seen%'",
		mailboxID,
	).Scan(&stats.Unseen)
	if err != nil {
		return nil, err
	}

	// Recent is typically messages in "new" directory
	// For simplicity, we count messages without \Seen flag
	stats.Recent = stats.Unseen

	return &stats, nil
}

// UpdateUserQuota updates the used quota for a user
func (s *Store) UpdateUserQuota(ctx context.Context, userID int64, deltaBytes int64) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE users SET used_bytes = used_bytes + ? WHERE id = ?",
		deltaBytes, userID,
	)
	return err
}

// Helper functions

func generateMaildirKey() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return fmt.Sprintf("%d.%s", time.Now().UnixNano(), hex.EncodeToString(buf))
}

func buildMaildirFlags(flags []storage.Flag) string {
	var result strings.Builder
	for _, f := range flags {
		switch f {
		case storage.FlagSeen:
			result.WriteRune('S')
		case storage.FlagAnswered:
			result.WriteRune('R')
		case storage.FlagFlagged:
			result.WriteRune('F')
		case storage.FlagDeleted:
			result.WriteRune('T')
		case storage.FlagDraft:
			result.WriteRune('D')
		}
	}
	return result.String()
}

func flagsToString(flags []storage.Flag) string {
	strs := make([]string, len(flags))
	for i, f := range flags {
		strs[i] = string(f)
	}
	return strings.Join(strs, ",")
}

func stringToFlags(s string) []storage.Flag {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	flags := make([]storage.Flag, len(parts))
	for i, p := range parts {
		flags[i] = storage.Flag(p)
	}
	return flags
}
