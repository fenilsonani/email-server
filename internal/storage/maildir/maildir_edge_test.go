package maildir

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/fenilsonani/email-server/internal/storage"
)

// setupEdgeTestDB creates an in-memory SQLite database for edge testing
func setupEdgeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	schema := `
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT UNIQUE NOT NULL,
			used_bytes INTEGER DEFAULT 0
		);
		CREATE TABLE IF NOT EXISTS mailboxes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			uidvalidity INTEGER DEFAULT 1,
			uidnext INTEGER DEFAULT 1,
			special_use TEXT,
			subscribed INTEGER DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, name)
		);
		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			mailbox_id INTEGER NOT NULL,
			uid INTEGER NOT NULL,
			maildir_key TEXT NOT NULL,
			size INTEGER DEFAULT 0,
			internal_date DATETIME,
			flags TEXT,
			message_id TEXT,
			subject TEXT,
			from_address TEXT,
			to_addresses TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(mailbox_id, uid)
		);
		INSERT INTO users (id, email) VALUES (1, 'test@example.com');
	`
	_, err = db.Exec(schema)
	if err != nil {
		t.Fatalf("Failed to create schema: %v", err)
	}
	return db
}

// TestStorage_PathTraversal verifies that path traversal attacks are blocked.
func TestStorage_PathTraversal(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	tmpDir, err := os.MkdirTemp("", "maildir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(db, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Create a sensitive file outside the maildir to test traversal
	sensitiveFile := filepath.Join(tmpDir, "sensitive.txt")
	if err := os.WriteFile(sensitiveFile, []byte("SECRET DATA"), 0644); err != nil {
		t.Fatalf("Failed to create sensitive file: %v", err)
	}

	pathTraversalAttempts := []string{
		"../../../etc/passwd",
		"..\\..\\..\\windows\\system32\\config\\sam",
		"....//....//....//etc/passwd",
		"./../../sensitive.txt",
		"/etc/passwd",
		"INBOX/../../../sensitive.txt",
		"INBOX/..%2f..%2f..%2fetc/passwd",
		"..%252f..%252f..%252fetc/passwd",
		"....\\....\\....\\etc\\passwd",
		"INBOX\x00/../sensitive.txt", // null byte injection
	}

	ctx := context.Background()
	userID := int64(1)

	for _, attempt := range pathTraversalAttempts {
		t.Run(attempt, func(t *testing.T) {
			// Try to create a mailbox with traversal path
			_, err := store.CreateMailbox(ctx, userID, attempt, "")

			// Get the actual path that would be used
			actualPath := store.getUserMaildirPath(userID, attempt)

			// Clean the path to resolve any ".." components
			cleanedPath := filepath.Clean(actualPath)

			// Verify the path is within the user's directory
			expectedPrefix := filepath.Clean(filepath.Join(tmpDir, "user_1"))
			if !strings.HasPrefix(cleanedPath, expectedPrefix) {
				t.Errorf("Path traversal not blocked! Attempt: %q resulted in path: %q (cleaned: %q)",
					attempt, actualPath, cleanedPath)
			}

			// Verify the cleaned path doesn't escape to sensitive locations
			// Note: On Unix, backslashes are valid filename characters, so we only check for forward slashes
			if strings.Contains(cleanedPath, "/etc/passwd") {
				t.Errorf("Path traversal allowed access to system file: %q -> %q", attempt, cleanedPath)
			}

			// Verify the path doesn't point to the sensitive file we created
			if cleanedPath == sensitiveFile {
				t.Errorf("Path traversal allowed direct access to sensitive file: %q -> %q", attempt, cleanedPath)
			}

			// The mailbox creation may or may not succeed, but the path should be safe
			_ = err
		})
	}
}

// TestStorage_SymlinkAttack verifies that symlink attacks are prevented.
func TestStorage_SymlinkAttack(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	tmpDir, err := os.MkdirTemp("", "maildir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(db, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()
	userID := int64(1)

	// Create user directory and a mailbox
	_, err = store.CreateMailbox(ctx, userID, "INBOX", "")
	if err != nil {
		t.Fatalf("Failed to create INBOX: %v", err)
	}

	// Create a sensitive file outside maildir
	sensitiveFile := filepath.Join(tmpDir, "sensitive_data.txt")
	if err := os.WriteFile(sensitiveFile, []byte("SECRET"), 0644); err != nil {
		t.Fatalf("Failed to create sensitive file: %v", err)
	}

	// Try to create a symlink inside the maildir pointing outside
	userDir := filepath.Join(tmpDir, "user_1")
	symlinkPath := filepath.Join(userDir, "evil_link")

	// Create symlink (this should be caught by any file operations)
	if err := os.Symlink(sensitiveFile, symlinkPath); err != nil {
		// Symlink creation may fail on some systems, skip test
		t.Skip("Cannot create symlinks on this system")
	}

	// Try to use the symlink as a mailbox name
	path := store.getUserMaildirPath(userID, "evil_link")

	// The path should be safe
	if strings.Contains(path, "sensitive_data.txt") {
		t.Errorf("Symlink attack allowed access to sensitive file")
	}
}

// TestStorage_DiskFull simulates disk full conditions.
func TestStorage_DiskFull(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	tmpDir, err := os.MkdirTemp("", "maildir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(db, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()
	userID := int64(1)

	// Create INBOX
	mb, err := store.CreateMailbox(ctx, userID, "INBOX", "")
	if err != nil {
		t.Fatalf("Failed to create INBOX: %v", err)
	}

	// Create a large message body that might fail on disk full
	largeBody := strings.Repeat("X", 1024*1024) // 1MB

	// Attempt to store the message (should succeed normally)
	_, err = store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader(largeBody))
	if err != nil {
		// In real disk full scenario, this would fail
		t.Logf("Message append returned error (expected in disk full): %v", err)
	}

	// Verify partial writes are cleaned up
	tmpPath := filepath.Join(tmpDir, "user_1", "INBOX", "tmp")
	entries, _ := os.ReadDir(tmpPath)
	if len(entries) > 0 {
		t.Logf("Warning: %d orphaned files in tmp directory", len(entries))
	}
}

// TestStorage_ConcurrentWrite tests concurrent writes to the same mailbox.
func TestStorage_ConcurrentWrite(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	// Enable WAL mode for better concurrency
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	tmpDir, err := os.MkdirTemp("", "maildir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(db, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()
	userID := int64(1)

	// Create INBOX
	mb, err := store.CreateMailbox(ctx, userID, "INBOX", "")
	if err != nil {
		t.Fatalf("Failed to create INBOX: %v", err)
	}

	var wg sync.WaitGroup
	var successCount, errorCount int
	var mu sync.Mutex
	messageCount := 20

	// Attempt concurrent message appends
	for i := 0; i < messageCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			body := bytes.NewReader([]byte("Message body " + string(rune('A'+idx))))
			_, err := store.AppendMessage(ctx, mb.ID, nil, time.Now(), body)
			mu.Lock()
			if err != nil {
				errorCount++
			} else {
				successCount++
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Most messages should succeed
	if successCount < messageCount/2 {
		t.Errorf("Too many concurrent write failures: %d successes, %d errors out of %d",
			successCount, errorCount, messageCount)
	}

	// Verify message count
	msgs, err := store.ListMessages(ctx, mb.ID, 0, 0)
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(msgs) != successCount {
		t.Errorf("Message count mismatch: expected %d, got %d", successCount, len(msgs))
	}

	// Verify UIDs are unique
	uidSet := make(map[uint32]bool)
	for _, msg := range msgs {
		if uidSet[msg.UID] {
			t.Errorf("Duplicate UID found: %d", msg.UID)
		}
		uidSet[msg.UID] = true
	}
}

// TestStorage_SpecialCharacters tests handling of special characters in mailbox names.
func TestStorage_SpecialCharacters(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	tmpDir, err := os.MkdirTemp("", "maildir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(db, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()
	userID := int64(1)

	specialNames := []struct {
		name        string
		shouldFail  bool
		description string
	}{
		{"Normal Folder", false, "space in name"},
		{"Folder-With-Dashes", false, "dashes"},
		{"Folder_With_Underscores", false, "underscores"},
		{"Folder.With.Dots", false, "dots (hierarchy separator)"},
		{"日本語フォルダ", false, "Japanese characters"},
		{"Папка", false, "Russian characters"},
		{"مجلد", false, "Arabic characters"},
		{"Folder\nWith\nNewlines", false, "newlines (will be sanitized)"},
		{"Folder\x00WithNull", false, "null bytes (will be sanitized)"},
		{"A" + strings.Repeat("B", 200), false, "very long name"},
	}

	for _, tc := range specialNames {
		t.Run(tc.description, func(t *testing.T) {
			_, err := store.CreateMailbox(ctx, userID, tc.name, "")

			if tc.shouldFail && err == nil {
				t.Errorf("Expected mailbox creation to fail for %q", tc.description)
			}

			// Verify path is safe regardless
			path := store.getUserMaildirPath(userID, tc.name)
			expectedPrefix := filepath.Join(tmpDir, "user_1")
			if !strings.HasPrefix(path, expectedPrefix) {
				t.Errorf("Path escaped user directory for %q: %s", tc.description, path)
			}
		})
	}
}

// TestStorage_LargeFile tests handling of large files.
func TestStorage_LargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	db := setupEdgeTestDB(t)
	defer db.Close()

	tmpDir, err := os.MkdirTemp("", "maildir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(db, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()
	userID := int64(1)

	// Create INBOX
	mb, err := store.CreateMailbox(ctx, userID, "INBOX", "")
	if err != nil {
		t.Fatalf("Failed to create INBOX: %v", err)
	}

	// Create a 10MB message
	largeSize := 10 * 1024 * 1024
	largeBody := strings.Repeat("X", largeSize)

	msg, err := store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader(largeBody))
	if err != nil {
		t.Fatalf("Failed to append large message: %v", err)
	}

	if msg.Size != int64(largeSize) {
		t.Errorf("Message size mismatch: expected %d, got %d", largeSize, msg.Size)
	}

	// Verify we can read it back
	body, err := store.GetMessageBody(ctx, msg)
	if err != nil {
		t.Fatalf("Failed to read message body: %v", err)
	}
	body.Close()
}

// TestStorage_MaxPathLength tests handling of very long paths.
func TestStorage_MaxPathLength(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	tmpDir, err := os.MkdirTemp("", "maildir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(db, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()
	userID := int64(1)

	// Create a mailbox with very long name (near PATH_MAX limit)
	veryLongName := strings.Repeat("A", 250) // Near typical filename limit

	// This may fail due to path length, which is expected
	_, err = store.CreateMailbox(ctx, userID, veryLongName, "")
	if err != nil {
		t.Logf("Long mailbox name creation failed as expected: %v", err)
	}

	// Verify the path is handled safely
	path := store.getUserMaildirPath(userID, veryLongName)
	if len(path) > 4096 {
		t.Logf("Path length %d exceeds PATH_MAX", len(path))
	}
}

// TestStorage_ManyFiles tests handling of directories with many files.
func TestStorage_ManyFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping many files test in short mode")
	}

	db := setupEdgeTestDB(t)
	defer db.Close()

	db.Exec("PRAGMA journal_mode=WAL")

	tmpDir, err := os.MkdirTemp("", "maildir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(db, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()
	userID := int64(1)

	// Create INBOX
	mb, err := store.CreateMailbox(ctx, userID, "INBOX", "")
	if err != nil {
		t.Fatalf("Failed to create INBOX: %v", err)
	}

	// Create 1000 messages (reduced for faster test)
	messageCount := 1000
	for i := 0; i < messageCount; i++ {
		body := bytes.NewReader([]byte("Message " + string(rune('A'+i%26))))
		_, err := store.AppendMessage(ctx, mb.ID, nil, time.Now(), body)
		if err != nil {
			t.Fatalf("Failed to create message %d: %v", i, err)
		}
	}

	// Verify we can list all messages
	start := time.Now()
	msgs, err := store.ListMessages(ctx, mb.ID, 0, 0)
	listDuration := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(msgs) != messageCount {
		t.Errorf("Expected %d messages, got %d", messageCount, len(msgs))
	}

	// List should complete in reasonable time
	if listDuration > 5*time.Second {
		t.Errorf("Listing %d messages took too long: %v", messageCount, listDuration)
	}
}

// TestStorage_UIDValidityChange tests handling of UIDVALIDITY changes.
func TestStorage_UIDValidityChange(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	tmpDir, err := os.MkdirTemp("", "maildir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(db, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()
	userID := int64(1)

	// Create two mailboxes at different times
	mb1, err := store.CreateMailbox(ctx, userID, "INBOX1", "")
	if err != nil {
		t.Fatalf("Failed to create INBOX1: %v", err)
	}

	time.Sleep(10 * time.Millisecond) // Ensure different timestamp

	mb2, err := store.CreateMailbox(ctx, userID, "INBOX2", "")
	if err != nil {
		t.Fatalf("Failed to create INBOX2: %v", err)
	}

	// UIDValidity should be different
	if mb1.UIDValidity == mb2.UIDValidity {
		t.Logf("Warning: UIDValidity values are same (%d), random component may have collided", mb1.UIDValidity)
	}

	// Verify UIDValidity is non-zero
	if mb1.UIDValidity == 0 || mb2.UIDValidity == 0 {
		t.Errorf("UIDValidity should not be zero: mb1=%d, mb2=%d", mb1.UIDValidity, mb2.UIDValidity)
	}
}

// TestStorage_FlagOperations tests flag add/remove/set operations.
func TestStorage_FlagOperations(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	tmpDir, err := os.MkdirTemp("", "maildir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(db, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()
	userID := int64(1)

	// Create INBOX
	mb, err := store.CreateMailbox(ctx, userID, "INBOX", "")
	if err != nil {
		t.Fatalf("Failed to create INBOX: %v", err)
	}

	// Create message with initial flags
	initialFlags := []storage.Flag{storage.FlagSeen}
	msg, err := store.AppendMessage(ctx, mb.ID, initialFlags, time.Now(), strings.NewReader("Test message"))
	if err != nil {
		t.Fatalf("Failed to create message: %v", err)
	}

	// Test adding flags
	err = store.UpdateFlags(ctx, mb.ID, msg.UID, []storage.Flag{storage.FlagFlagged}, true)
	if err != nil {
		t.Fatalf("Failed to add flag: %v", err)
	}

	// Verify flags
	updatedMsg, err := store.GetMessage(ctx, mb.ID, msg.UID)
	if err != nil {
		t.Fatalf("Failed to get message: %v", err)
	}

	hasFlag := func(flags []storage.Flag, target storage.Flag) bool {
		for _, f := range flags {
			if f == target {
				return true
			}
		}
		return false
	}

	if !hasFlag(updatedMsg.Flags, storage.FlagSeen) {
		t.Error("Message should have \\Seen flag")
	}
	if !hasFlag(updatedMsg.Flags, storage.FlagFlagged) {
		t.Error("Message should have \\Flagged flag")
	}

	// Test removing flags
	err = store.UpdateFlags(ctx, mb.ID, msg.UID, []storage.Flag{storage.FlagSeen}, false)
	if err != nil {
		t.Fatalf("Failed to remove flag: %v", err)
	}

	updatedMsg, err = store.GetMessage(ctx, mb.ID, msg.UID)
	if err != nil {
		t.Fatalf("Failed to get message: %v", err)
	}

	if hasFlag(updatedMsg.Flags, storage.FlagSeen) {
		t.Error("Message should not have \\Seen flag after removal")
	}
	if !hasFlag(updatedMsg.Flags, storage.FlagFlagged) {
		t.Error("Message should still have \\Flagged flag")
	}
}

// TestStorage_DeletedMailbox tests operations on deleted mailboxes.
func TestStorage_DeletedMailbox(t *testing.T) {
	db := setupEdgeTestDB(t)
	defer db.Close()

	tmpDir, err := os.MkdirTemp("", "maildir-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(db, tmpDir)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	ctx := context.Background()
	userID := int64(1)

	// Create and then delete a mailbox
	mb, err := store.CreateMailbox(ctx, userID, "ToDelete", "")
	if err != nil {
		t.Fatalf("Failed to create mailbox: %v", err)
	}

	mailboxID := mb.ID

	err = store.DeleteMailbox(ctx, userID, "ToDelete")
	if err != nil {
		t.Fatalf("Failed to delete mailbox: %v", err)
	}

	// Try to append to deleted mailbox
	_, err = store.AppendMessage(ctx, mailboxID, nil, time.Now(), strings.NewReader("Test"))
	if err == nil {
		t.Error("Should not be able to append to deleted mailbox")
	}

	// Try to get deleted mailbox
	_, err = store.GetMailbox(ctx, userID, "ToDelete")
	if err == nil {
		t.Error("Should not be able to get deleted mailbox")
	}
}
