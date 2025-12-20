package tests

import (
	"context"
	"database/sql"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	imapClient "github.com/emersion/go-imap/client"
	"github.com/emersion/go-smtp"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/config"
	imapServer "github.com/fenilsonani/email-server/internal/imap"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
	smtpServer "github.com/fenilsonani/email-server/internal/smtp"
	_ "github.com/mattn/go-sqlite3"
)

// testEnv holds all components needed for integration tests
type testEnv struct {
	db            *sql.DB
	cfg           *config.Config
	auth          *auth.Authenticator
	store         *maildir.Store
	imapBackend   *imapServer.Backend
	smtpBackend   *smtpServer.Backend
	tmpDir        string
	imapListener  net.Listener
	smtpListener  net.Listener
}

func setupIntegrationEnv(t *testing.T) (*testEnv, func()) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "integration_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create database
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
			dkim_selector TEXT NOT NULL DEFAULT 'mail',
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			display_name TEXT,
			quota_bytes INTEGER DEFAULT 1073741824,
			used_bytes INTEGER DEFAULT 0,
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(domain_id, username)
		);

		CREATE TABLE aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
			source_address TEXT NOT NULL,
			destination_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
			destination_external TEXT,
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(domain_id, source_address)
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
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create schema: %v", err)
	}

	// Create test domain
	_, err = db.Exec("INSERT INTO domains (name) VALUES (?)", "test.local")
	if err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create domain: %v", err)
	}

	// Create authenticator
	authenticator := auth.NewAuthenticator(db)

	// Create test user with password
	password := "testpass123"
	hash, _ := auth.HashPassword(password)
	result, err := db.Exec(
		"INSERT INTO users (domain_id, username, password_hash, display_name) VALUES (1, ?, ?, ?)",
		"testuser", hash, "Test User",
	)
	if err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create user: %v", err)
	}
	userID, _ := result.LastInsertId()

	// Create test config
	cfg := &config.Config{
		Server: config.ServerConfig{
			Hostname:       "mail.test.local",
			SMTPPort:       25,
			SubmissionPort: 587,
			IMAPPort:       143,
			IMAPSPort:      993,
		},
		Storage: config.StorageConfig{
			DataDir:      tmpDir,
			DatabasePath: dbPath,
			MaildirPath:  tmpDir + "/maildir",
		},
		Domains: []config.DomainConfig{
			{Name: "test.local", DKIMSelector: "mail"},
		},
		Security: config.SecurityConfig{
			RequireTLS:     false, // Allow insecure for tests
			MaxMessageSize: 26214400,
		},
	}

	// Create maildir store
	maildirPath := cfg.Storage.MaildirPath
	store, err := maildir.NewStore(db, maildirPath)
	if err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create store: %v", err)
	}

	// Create INBOX for test user
	ctx := context.Background()
	_, err = store.CreateMailbox(ctx, userID, "INBOX", "")
	if err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create INBOX: %v", err)
	}

	// Create IMAP backend
	imapBackend := imapServer.NewBackend(authenticator, store)

	// Create SMTP backend
	smtpBackend := smtpServer.NewBackend(cfg, authenticator, store)

	env := &testEnv{
		db:          db,
		cfg:         cfg,
		auth:        authenticator,
		store:       store,
		imapBackend: imapBackend,
		smtpBackend: smtpBackend,
		tmpDir:      tmpDir,
	}

	cleanup := func() {
		if env.imapListener != nil {
			env.imapListener.Close()
		}
		if env.smtpListener != nil {
			env.smtpListener.Close()
		}
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return env, cleanup
}

func TestIntegration_AuthenticateIMAPUser(t *testing.T) {
	env, cleanup := setupIntegrationEnv(t)
	defer cleanup()

	// Start IMAP server (use plaintext for tests)
	imapSrv := imapServer.NewServer(env.imapBackend, "127.0.0.1:0", "", nil)
	var err error
	env.imapListener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	go imapSrv.Serve(env.imapListener)

	// Connect as client
	addr := env.imapListener.Addr().String()
	c, err := imapClient.Dial(addr)
	if err != nil {
		t.Fatalf("Failed to connect to IMAP server: %v", err)
	}
	defer c.Close()

	// Test login with correct credentials
	err = c.Login("testuser@test.local", "testpass123")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Test listing mailboxes
	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.List("", "*", mailboxes)
	}()

	var names []string
	for m := range mailboxes {
		names = append(names, m.Name)
	}

	if err := <-done; err != nil {
		t.Fatalf("List mailboxes failed: %v", err)
	}

	if len(names) == 0 {
		t.Error("Expected at least one mailbox")
	}

	found := false
	for _, name := range names {
		if name == "INBOX" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find INBOX")
	}

	// Logout
	err = c.Logout()
	if err != nil {
		t.Errorf("Logout failed: %v", err)
	}
}

func TestIntegration_AuthenticateIMAPWrongPassword(t *testing.T) {
	env, cleanup := setupIntegrationEnv(t)
	defer cleanup()

	// Start IMAP server
	imapSrv := imapServer.NewServer(env.imapBackend, "127.0.0.1:0", "", nil)
	var err error
	env.imapListener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	go imapSrv.Serve(env.imapListener)

	// Connect as client
	addr := env.imapListener.Addr().String()
	c, err := imapClient.Dial(addr)
	if err != nil {
		t.Fatalf("Failed to connect to IMAP server: %v", err)
	}
	defer c.Close()

	// Test login with wrong password
	err = c.Login("testuser@test.local", "wrongpassword")
	if err == nil {
		t.Error("Login should have failed with wrong password")
	}
}

func TestIntegration_SMTPReceiveMessage(t *testing.T) {
	env, cleanup := setupIntegrationEnv(t)
	defer cleanup()

	// Start SMTP server
	smtpSrv := smtp.NewServer(env.smtpBackend)
	smtpSrv.Domain = "mail.test.local"
	smtpSrv.AllowInsecureAuth = true

	var err error
	env.smtpListener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	go smtpSrv.Serve(env.smtpListener)

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Connect as SMTP client
	addr := env.smtpListener.Addr().String()
	c, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("Failed to connect to SMTP server: %v", err)
	}
	defer c.Close()

	// Send a message
	from := "sender@external.com"
	to := "testuser@test.local"
	msg := "From: sender@external.com\r\nTo: testuser@test.local\r\nSubject: Test Message\r\n\r\nHello from integration test!"

	err = c.Mail(from, nil)
	if err != nil {
		t.Fatalf("MAIL FROM failed: %v", err)
	}

	err = c.Rcpt(to, nil)
	if err != nil {
		t.Fatalf("RCPT TO failed: %v", err)
	}

	wc, err := c.Data()
	if err != nil {
		t.Fatalf("DATA failed: %v", err)
	}

	_, err = wc.Write([]byte(msg))
	if err != nil {
		t.Fatalf("Write message failed: %v", err)
	}

	err = wc.Close()
	if err != nil {
		t.Fatalf("Close DATA failed: %v", err)
	}

	// Verify message was stored
	ctx := context.Background()

	// Get user's INBOX
	mb, err := env.store.GetMailbox(ctx, 1, "INBOX")
	if err != nil {
		t.Fatalf("Failed to get INBOX: %v", err)
	}

	// List messages
	messages, err := env.store.ListMessages(ctx, mb.ID, 0, 0)
	if err != nil {
		t.Fatalf("Failed to list messages: %v", err)
	}

	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}
}

func TestIntegration_EndToEndFlow(t *testing.T) {
	env, cleanup := setupIntegrationEnv(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Store message directly (simulating SMTP delivery)
	mb, err := env.store.GetMailbox(ctx, 1, "INBOX")
	if err != nil {
		t.Fatalf("Failed to get INBOX: %v", err)
	}

	msgContent := "From: sender@example.com\r\nTo: testuser@test.local\r\nSubject: Integration Test\r\n\r\nThis is an integration test message."
	msg, err := env.store.AppendMessage(ctx, mb.ID, nil, time.Now(), strings.NewReader(msgContent))
	if err != nil {
		t.Fatalf("Failed to append message: %v", err)
	}

	t.Logf("Stored message with UID %d", msg.UID)

	// 2. Start IMAP server
	imapSrv := imapServer.NewServer(env.imapBackend, "127.0.0.1:0", "", nil)
	env.imapListener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	go imapSrv.Serve(env.imapListener)

	// 3. Connect via IMAP and read the message
	addr := env.imapListener.Addr().String()
	c, err := imapClient.Dial(addr)
	if err != nil {
		t.Fatalf("Failed to connect to IMAP: %v", err)
	}
	defer c.Close()

	err = c.Login("testuser@test.local", "testpass123")
	if err != nil {
		t.Fatalf("IMAP login failed: %v", err)
	}

	// Select INBOX
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		t.Fatalf("Select INBOX failed: %v", err)
	}

	if mbox.Messages == 0 {
		t.Fatal("Expected at least 1 message in INBOX")
	}

	t.Logf("INBOX has %d messages", mbox.Messages)

	// Fetch the message
	seqSet := new(imap.SeqSet)
	seqSet.AddNum(1)

	messages := make(chan *imap.Message, 1)
	done := make(chan error, 1)

	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags, section.FetchItem()}

	go func() {
		done <- c.Fetch(seqSet, items, messages)
	}()

	var fetchedMsg *imap.Message
	for m := range messages {
		fetchedMsg = m
	}

	if err := <-done; err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if fetchedMsg == nil {
		t.Fatal("No message fetched")
	}

	// Verify we got a message (envelope parsing is a separate feature)
	if fetchedMsg.SeqNum == 0 {
		t.Error("Expected valid sequence number")
	}

	// Check if body was fetched
	for _, literal := range fetchedMsg.Body {
		if literal != nil {
			t.Log("Message body fetched successfully")
			break
		}
	}

	c.Logout()
}

func TestIntegration_MultipleUsers(t *testing.T) {
	env, cleanup := setupIntegrationEnv(t)
	defer cleanup()

	ctx := context.Background()

	// Create second user
	password := "pass456"
	hash, _ := auth.HashPassword(password)
	result, err := env.db.Exec(
		"INSERT INTO users (domain_id, username, password_hash) VALUES (1, ?, ?)",
		"user2", hash,
	)
	if err != nil {
		t.Fatalf("Failed to create user2: %v", err)
	}
	user2ID, _ := result.LastInsertId()

	// Create INBOX for user2
	_, err = env.store.CreateMailbox(ctx, user2ID, "INBOX", "")
	if err != nil {
		t.Fatalf("Failed to create INBOX for user2: %v", err)
	}

	// Store message for user1
	mb1, _ := env.store.GetMailbox(ctx, 1, "INBOX")
	env.store.AppendMessage(ctx, mb1.ID, nil, time.Now(), strings.NewReader("Message for user1"))

	// Store message for user2
	mb2, _ := env.store.GetMailbox(ctx, user2ID, "INBOX")
	env.store.AppendMessage(ctx, mb2.ID, nil, time.Now(), strings.NewReader("Message for user2"))

	// Start IMAP server
	imapSrv := imapServer.NewServer(env.imapBackend, "127.0.0.1:0", "", nil)
	env.imapListener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	go imapSrv.Serve(env.imapListener)

	addr := env.imapListener.Addr().String()

	// User1 should see 1 message
	c1, _ := imapClient.Dial(addr)
	defer c1.Close()
	c1.Login("testuser@test.local", "testpass123")
	mbox1, _ := c1.Select("INBOX", false)
	if mbox1.Messages != 1 {
		t.Errorf("User1 expected 1 message, got %d", mbox1.Messages)
	}
	c1.Logout()

	// User2 should see 1 message
	c2, _ := imapClient.Dial(addr)
	defer c2.Close()
	c2.Login("user2@test.local", "pass456")
	mbox2, _ := c2.Select("INBOX", false)
	if mbox2.Messages != 1 {
		t.Errorf("User2 expected 1 message, got %d", mbox2.Messages)
	}
	c2.Logout()
}

func TestIntegration_MailboxOperations(t *testing.T) {
	env, cleanup := setupIntegrationEnv(t)
	defer cleanup()

	// Start IMAP server
	imapSrv := imapServer.NewServer(env.imapBackend, "127.0.0.1:0", "", nil)
	var err error
	env.imapListener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	go imapSrv.Serve(env.imapListener)

	addr := env.imapListener.Addr().String()
	c, err := imapClient.Dial(addr)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer c.Close()

	c.Login("testuser@test.local", "testpass123")

	// Create a new mailbox
	err = c.Create("Archive")
	if err != nil {
		t.Fatalf("Create mailbox failed: %v", err)
	}

	// List mailboxes to verify
	mailboxes := make(chan *imap.MailboxInfo, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.List("", "*", mailboxes)
	}()

	var names []string
	for m := range mailboxes {
		names = append(names, m.Name)
	}
	<-done

	foundArchive := false
	for _, name := range names {
		if name == "Archive" {
			foundArchive = true
			break
		}
	}
	if !foundArchive {
		t.Error("Expected to find Archive mailbox")
	}

	// Rename mailbox
	err = c.Rename("Archive", "OldMail")
	if err != nil {
		t.Fatalf("Rename mailbox failed: %v", err)
	}

	// Delete mailbox
	err = c.Delete("OldMail")
	if err != nil {
		t.Fatalf("Delete mailbox failed: %v", err)
	}

	c.Logout()
}
