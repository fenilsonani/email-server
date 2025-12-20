package maildir

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/storage"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestStore(t *testing.T) (*Store, func()) {
	// Create temp directory for maildir
	tmpDir, err := os.MkdirTemp("", "maildir_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create temp database
	dbPath := tmpDir + "/test.db"
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to open database: %v", err)
	}

	// Create schema
	schema := `
		CREATE TABLE domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			is_active BOOLEAN DEFAULT TRUE
		);

		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL REFERENCES domains(id),
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			quota_bytes INTEGER DEFAULT 1073741824,
			used_bytes INTEGER DEFAULT 0,
			is_active BOOLEAN DEFAULT TRUE,
			UNIQUE(domain_id, username)
		);

		CREATE TABLE mailboxes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			uidvalidity INTEGER NOT NULL,
			uidnext INTEGER NOT NULL DEFAULT 1,
			subscribed BOOLEAN DEFAULT TRUE,
			special_use TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, name)
		);

		CREATE TABLE messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
			uid INTEGER NOT NULL,
			maildir_key TEXT NOT NULL,
			size INTEGER NOT NULL,
			internal_date DATETIME NOT NULL,
			flags TEXT DEFAULT '',
			message_id TEXT,
			subject TEXT,
			from_address TEXT,
			to_addresses TEXT,
			in_reply_to TEXT,
			references_header TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(mailbox_id, uid)
		);

		-- Create test domain and user
		INSERT INTO domains (id, name) VALUES (1, 'test.com');
		INSERT INTO users (id, domain_id, username, password_hash) VALUES (1, 1, 'testuser', 'hash');
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create schema: %v", err)
	}

	maildirPath := tmpDir + "/maildir"
	store, err := NewStore(db, maildirPath)
	if err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create store: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestStore_CreateMailbox(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create INBOX
	mb, err := store.CreateMailbox(ctx, userID, "INBOX", "")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	if mb.Name != "INBOX" {
		t.Errorf("Expected name INBOX, got %s", mb.Name)
	}

	if mb.UserID != userID {
		t.Errorf("Expected userID %d, got %d", userID, mb.UserID)
	}

	if mb.UIDNext != 1 {
		t.Errorf("Expected UIDNext 1, got %d", mb.UIDNext)
	}

	// Create with special use
	sent, err := store.CreateMailbox(ctx, userID, "Sent", storage.SpecialUseSent)
	if err != nil {
		t.Fatalf("CreateMailbox for Sent failed: %v", err)
	}

	if sent.SpecialUse != storage.SpecialUseSent {
		t.Errorf("Expected special use %s, got %s", storage.SpecialUseSent, sent.SpecialUse)
	}
}

func TestStore_GetMailbox(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox first
	_, err := store.CreateMailbox(ctx, userID, "INBOX", "")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	// Get mailbox
	mb, err := store.GetMailbox(ctx, userID, "INBOX")
	if err != nil {
		t.Fatalf("GetMailbox failed: %v", err)
	}

	if mb.Name != "INBOX" {
		t.Errorf("Expected name INBOX, got %s", mb.Name)
	}

	// Get non-existent mailbox
	_, err = store.GetMailbox(ctx, userID, "NonExistent")
	if err == nil {
		t.Error("Expected error for non-existent mailbox")
	}
}

func TestStore_ListMailboxes(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create several mailboxes
	boxes := []string{"INBOX", "Sent", "Drafts", "Trash"}
	for _, name := range boxes {
		_, err := store.CreateMailbox(ctx, userID, name, "")
		if err != nil {
			t.Fatalf("CreateMailbox %s failed: %v", name, err)
		}
	}

	// List mailboxes
	result, err := store.ListMailboxes(ctx, userID)
	if err != nil {
		t.Fatalf("ListMailboxes failed: %v", err)
	}

	if len(result) != len(boxes) {
		t.Errorf("Expected %d mailboxes, got %d", len(boxes), len(result))
	}
}

func TestStore_RenameMailbox(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox
	_, err := store.CreateMailbox(ctx, userID, "OldName", "")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	// Rename
	err = store.RenameMailbox(ctx, userID, "OldName", "NewName")
	if err != nil {
		t.Fatalf("RenameMailbox failed: %v", err)
	}

	// Verify old name doesn't exist
	_, err = store.GetMailbox(ctx, userID, "OldName")
	if err == nil {
		t.Error("Old mailbox name should not exist")
	}

	// Verify new name exists
	mb, err := store.GetMailbox(ctx, userID, "NewName")
	if err != nil {
		t.Fatalf("GetMailbox with new name failed: %v", err)
	}

	if mb.Name != "NewName" {
		t.Errorf("Expected name NewName, got %s", mb.Name)
	}
}

func TestStore_DeleteMailbox(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox
	_, err := store.CreateMailbox(ctx, userID, "ToDelete", "")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	// Delete
	err = store.DeleteMailbox(ctx, userID, "ToDelete")
	if err != nil {
		t.Fatalf("DeleteMailbox failed: %v", err)
	}

	// Verify it doesn't exist
	_, err = store.GetMailbox(ctx, userID, "ToDelete")
	if err == nil {
		t.Error("Deleted mailbox should not exist")
	}
}

func TestStore_AppendMessage(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox
	mb, err := store.CreateMailbox(ctx, userID, "INBOX", "")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	// Append message
	msgContent := "From: sender@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nHello World"
	msg, err := store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader(msgContent))
	if err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}

	if msg.UID != 1 {
		t.Errorf("Expected UID 1, got %d", msg.UID)
	}

	if msg.Size != int64(len(msgContent)) {
		t.Errorf("Expected size %d, got %d", len(msgContent), msg.Size)
	}

	// Append another message
	msg2, err := store.AppendMessage(ctx, mb.ID, []storage.Flag{storage.FlagSeen}, time.Now(), strings.NewReader("Another message"))
	if err != nil {
		t.Fatalf("AppendMessage 2 failed: %v", err)
	}

	if msg2.UID != 2 {
		t.Errorf("Expected UID 2, got %d", msg2.UID)
	}
}

func TestStore_GetMessage(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox and append message
	mb, _ := store.CreateMailbox(ctx, userID, "INBOX", "")
	_, err := store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader("Test message"))
	if err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}

	// Get message
	msg, err := store.GetMessage(ctx, mb.ID, 1)
	if err != nil {
		t.Fatalf("GetMessage failed: %v", err)
	}

	if msg.UID != 1 {
		t.Errorf("Expected UID 1, got %d", msg.UID)
	}

	// Get non-existent message
	_, err = store.GetMessage(ctx, mb.ID, 999)
	if err == nil {
		t.Error("Expected error for non-existent message")
	}
}

func TestStore_GetMessageBody(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox and append message
	mb, err := store.CreateMailbox(ctx, userID, "INBOX", "")
	if err != nil {
		t.Fatalf("CreateMailbox failed: %v", err)
	}

	content := "Test message body content"
	msg, err := store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader(content))
	if err != nil {
		t.Fatalf("AppendMessage failed: %v", err)
	}

	// Get message body
	body, err := store.GetMessageBody(ctx, msg)
	if err != nil {
		t.Fatalf("GetMessageBody failed: %v", err)
	}
	defer body.Close()

	buf := make([]byte, 1024)
	n, _ := body.Read(buf)
	if string(buf[:n]) != content {
		t.Errorf("Expected body '%s', got '%s'", content, string(buf[:n]))
	}
}

func TestStore_ListMessages(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox and append messages
	mb, _ := store.CreateMailbox(ctx, userID, "INBOX", "")
	for i := 0; i < 5; i++ {
		store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader("Message"))
	}

	// List all messages
	messages, err := store.ListMessages(ctx, mb.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListMessages failed: %v", err)
	}

	if len(messages) != 5 {
		t.Errorf("Expected 5 messages, got %d", len(messages))
	}

	// List with range
	messages, err = store.ListMessages(ctx, mb.ID, 2, 4)
	if err != nil {
		t.Fatalf("ListMessages with range failed: %v", err)
	}

	if len(messages) != 3 {
		t.Errorf("Expected 3 messages in range, got %d", len(messages))
	}
}

func TestStore_UpdateFlags(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox and append message
	mb, _ := store.CreateMailbox(ctx, userID, "INBOX", "")
	store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader("Message"))

	// Add flag
	err := store.UpdateFlags(ctx, mb.ID, 1, []storage.Flag{storage.FlagSeen}, true)
	if err != nil {
		t.Fatalf("UpdateFlags (add) failed: %v", err)
	}

	// Verify flag was added
	msg, _ := store.GetMessage(ctx, mb.ID, 1)
	hasFlag := false
	for _, f := range msg.Flags {
		if f == storage.FlagSeen {
			hasFlag = true
			break
		}
	}
	if !hasFlag {
		t.Error("Expected message to have \\Seen flag")
	}

	// Remove flag
	err = store.UpdateFlags(ctx, mb.ID, 1, []storage.Flag{storage.FlagSeen}, false)
	if err != nil {
		t.Fatalf("UpdateFlags (remove) failed: %v", err)
	}

	// Verify flag was removed
	msg, _ = store.GetMessage(ctx, mb.ID, 1)
	hasFlag = false
	for _, f := range msg.Flags {
		if f == storage.FlagSeen {
			hasFlag = true
			break
		}
	}
	if hasFlag {
		t.Error("Expected message to not have \\Seen flag")
	}
}

func TestStore_SetFlags(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox and append message with initial flags
	mb, _ := store.CreateMailbox(ctx, userID, "INBOX", "")
	store.AppendMessage(ctx, mb.ID, []storage.Flag{storage.FlagSeen}, time.Now(), strings.NewReader("Message"))

	// Set new flags (replace all)
	newFlags := []storage.Flag{storage.FlagFlagged, storage.FlagAnswered}
	err := store.SetFlags(ctx, mb.ID, 1, newFlags)
	if err != nil {
		t.Fatalf("SetFlags failed: %v", err)
	}

	// Verify flags
	msg, _ := store.GetMessage(ctx, mb.ID, 1)
	if len(msg.Flags) != 2 {
		t.Errorf("Expected 2 flags, got %d", len(msg.Flags))
	}
}

func TestStore_CopyMessage(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create two mailboxes
	inbox, _ := store.CreateMailbox(ctx, userID, "INBOX", "")
	archive, _ := store.CreateMailbox(ctx, userID, "Archive", "")

	// Append message to inbox
	store.AppendMessage(ctx, inbox.ID, nil, time.Now(), strings.NewReader("Message to copy"))

	// Copy to archive
	copiedMsg, err := store.CopyMessage(ctx, inbox.ID, 1, archive.ID)
	if err != nil {
		t.Fatalf("CopyMessage failed: %v", err)
	}

	if copiedMsg.UID != 1 {
		t.Errorf("Expected copied message UID 1, got %d", copiedMsg.UID)
	}

	// Verify original still exists
	_, err = store.GetMessage(ctx, inbox.ID, 1)
	if err != nil {
		t.Error("Original message should still exist")
	}

	// Verify copy exists
	_, err = store.GetMessage(ctx, archive.ID, 1)
	if err != nil {
		t.Error("Copied message should exist in archive")
	}
}

func TestStore_MoveMessage(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create two mailboxes
	inbox, _ := store.CreateMailbox(ctx, userID, "INBOX", "")
	trash, _ := store.CreateMailbox(ctx, userID, "Trash", "")

	// Append message to inbox
	store.AppendMessage(ctx, inbox.ID, nil, time.Now(), strings.NewReader("Message to move"))

	// Move to trash
	movedMsg, err := store.MoveMessage(ctx, inbox.ID, 1, trash.ID)
	if err != nil {
		t.Fatalf("MoveMessage failed: %v", err)
	}

	if movedMsg.UID != 1 {
		t.Errorf("Expected moved message UID 1, got %d", movedMsg.UID)
	}

	// Verify original doesn't exist
	_, err = store.GetMessage(ctx, inbox.ID, 1)
	if err == nil {
		t.Error("Original message should not exist after move")
	}

	// Verify message exists in trash
	_, err = store.GetMessage(ctx, trash.ID, 1)
	if err != nil {
		t.Error("Moved message should exist in trash")
	}
}

func TestStore_ExpungeMailbox(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox and messages
	mb, _ := store.CreateMailbox(ctx, userID, "INBOX", "")
	store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader("Message 1"))
	store.AppendMessage(ctx, mb.ID, []storage.Flag{storage.FlagDeleted}, time.Now(), strings.NewReader("Message 2 - deleted"))
	store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader("Message 3"))

	// Mark message 1 as deleted
	store.UpdateFlags(ctx, mb.ID, 1, []storage.Flag{storage.FlagDeleted}, true)

	// Expunge
	expunged, err := store.ExpungeMailbox(ctx, mb.ID)
	if err != nil {
		t.Fatalf("ExpungeMailbox failed: %v", err)
	}

	if len(expunged) != 2 {
		t.Errorf("Expected 2 expunged messages, got %d", len(expunged))
	}

	// Verify remaining messages
	messages, _ := store.ListMessages(ctx, mb.ID, 0, 0)
	if len(messages) != 1 {
		t.Errorf("Expected 1 remaining message, got %d", len(messages))
	}
}

func TestStore_SearchMessages(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox and messages
	mb, _ := store.CreateMailbox(ctx, userID, "INBOX", "")
	store.AppendMessage(ctx, mb.ID, []storage.Flag{storage.FlagSeen}, time.Now(), strings.NewReader("Message 1"))
	store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader("Message 2"))
	store.AppendMessage(ctx, mb.ID, []storage.Flag{storage.FlagSeen, storage.FlagFlagged}, time.Now(), strings.NewReader("Message 3"))

	// Search for seen messages
	criteria := &storage.SearchCriteria{
		Flags: []storage.Flag{storage.FlagSeen},
	}
	uids, err := store.SearchMessages(ctx, mb.ID, criteria)
	if err != nil {
		t.Fatalf("SearchMessages failed: %v", err)
	}

	if len(uids) != 2 {
		t.Errorf("Expected 2 seen messages, got %d", len(uids))
	}

	// Search for unseen messages
	criteria = &storage.SearchCriteria{
		NotFlags: []storage.Flag{storage.FlagSeen},
	}
	uids, _ = store.SearchMessages(ctx, mb.ID, criteria)
	if len(uids) != 1 {
		t.Errorf("Expected 1 unseen message, got %d", len(uids))
	}
}

func TestStore_GetMailboxStats(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()
	userID := int64(1)

	// Create mailbox and messages
	mb, _ := store.CreateMailbox(ctx, userID, "INBOX", "")
	store.AppendMessage(ctx, mb.ID, []storage.Flag{storage.FlagSeen}, time.Now(), strings.NewReader("Seen"))
	store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader("Unseen 1"))
	store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader("Unseen 2"))

	// Get stats
	stats, err := store.GetMailboxStats(ctx, mb.ID)
	if err != nil {
		t.Fatalf("GetMailboxStats failed: %v", err)
	}

	if stats.Messages != 3 {
		t.Errorf("Expected 3 messages, got %d", stats.Messages)
	}

	if stats.Unseen != 2 {
		t.Errorf("Expected 2 unseen, got %d", stats.Unseen)
	}

	if stats.UIDNext != 4 {
		t.Errorf("Expected UIDNext 4, got %d", stats.UIDNext)
	}
}
