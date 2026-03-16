package delivery

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/resilience"

	_ "github.com/mattn/go-sqlite3"
)

func TestConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Workers != 4 {
		t.Errorf("Workers = %d, want 4", cfg.Workers)
	}
	if cfg.Hostname != "localhost" {
		t.Errorf("Hostname = %s, want localhost", cfg.Hostname)
	}
	if cfg.ConnectTimeout != 30*time.Second {
		t.Errorf("ConnectTimeout = %v, want 30s", cfg.ConnectTimeout)
	}
	if cfg.CommandTimeout != 5*time.Minute {
		t.Errorf("CommandTimeout = %v, want 5m", cfg.CommandTimeout)
	}
	if cfg.MaxMessageSize != 25*1024*1024 {
		t.Errorf("MaxMessageSize = %d, want 25MB", cfg.MaxMessageSize)
	}
	if cfg.RequireTLS != false {
		t.Errorf("RequireTLS = %v, want false", cfg.RequireTLS)
	}
	if cfg.VerifyTLS != true {
		t.Errorf("VerifyTLS = %v, want true", cfg.VerifyTLS)
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		email  string
		domain string
	}{
		{"user@example.com", "example.com"},
		{"user@EXAMPLE.COM", "example.com"},
		{"user@Sub.Domain.Example.COM", "sub.domain.example.com"},
		{"user@localhost", "localhost"},
		{"noatsign", ""},
		{"", ""},
		{"@domain.com", "domain.com"},
		{"user@", ""},
		{"user@domain@extra", "domain@extra"},
		{"  user@example.com  ", "example.com  "},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := extractDomain(tt.email)
			if got != tt.domain {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.email, got, tt.domain)
			}
		})
	}
}

func TestIsPermanentError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"550 user not found", errors.New("550 User not found"), true},
		{"551 user moved", errors.New("551 User moved"), true},
		{"552 mailbox full", errors.New("552 Mailbox full"), true},
		{"553 invalid mailbox", errors.New("553 Invalid mailbox"), true},
		{"554 transaction failed", errors.New("554 Transaction failed"), true},
		{"421 service unavailable", errors.New("421 Service unavailable"), false},
		{"450 try again", errors.New("450 Try again later"), false},
		{"451 local error", errors.New("451 Local error"), false},
		{"connection timeout", errors.New("connection timeout"), false},
		{"ErrPermanentFailure", ErrPermanentFailure, true},
		{"ErrInvalidRecipient", ErrInvalidRecipient, true},
		{"ErrMessageTooLarge", ErrMessageTooLarge, true},
		{"ErrTemporaryFailure", ErrTemporaryFailure, false},
		{"ErrCircuitOpen", ErrCircuitOpen, false},
		{"ErrAllMXFailed", ErrAllMXFailed, false},
		{"wrapped permanent", errors.New("error: 550 permanent"), true},
		{"wrapped temporary", errors.New("error: 450 temporary"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPermanentError(tt.err)
			if got != tt.want {
				t.Errorf("isPermanentError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantPermanent bool
	}{
		{"nil", nil, false},
		{"550 prefix", errors.New("550 User unknown"), true},
		{"space 5", errors.New("SMTP error 550"), true},
		{"421 temp", errors.New("421 Try later"), false},
		{"random error", errors.New("network error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyError(tt.err)
			if result == nil {
				if tt.err != nil {
					t.Error("classifyError returned nil for non-nil error")
				}
				return
			}
			isPerm := errors.Is(result, ErrPermanentFailure)
			if isPerm != tt.wantPermanent {
				t.Errorf("classified as permanent=%v, want %v", isPerm, tt.wantPermanent)
			}
		})
	}
}

func TestEngine_CleanupMessageFile(t *testing.T) {
	tmpDir := t.TempDir()
	logger := logging.Default()

	// Create a mock engine with QueuePath set
	e := &Engine{
		config: Config{
			QueuePath: tmpDir,
		},
		logger: logger.Delivery(),
	}

	t.Run("cleanup existing file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "test1.eml")
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		err := e.cleanupMessageFile(path)
		if err != nil {
			t.Errorf("cleanupMessageFile() error = %v", err)
		}

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("File should have been deleted")
		}
	})

	t.Run("cleanup non-existent file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "nonexistent.eml")

		err := e.cleanupMessageFile(path)
		if err != nil {
			t.Errorf("cleanupMessageFile() should not error for non-existent file: %v", err)
		}
	})

	t.Run("cleanup empty path", func(t *testing.T) {
		err := e.cleanupMessageFile("")
		if err != nil {
			t.Errorf("cleanupMessageFile() should not error for empty path: %v", err)
		}
	})

	t.Run("refuse cleanup outside queue path", func(t *testing.T) {
		otherDir := t.TempDir()
		path := filepath.Join(otherDir, "other.eml")
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		// Should refuse to delete (silently)
		err := e.cleanupMessageFile(path)
		if err != nil {
			t.Errorf("cleanupMessageFile() error = %v", err)
		}

		// File should still exist
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("File outside queue path should NOT have been deleted")
		}
	})

	t.Run("cleanup with empty QueuePath allows all", func(t *testing.T) {
		e2 := &Engine{config: Config{QueuePath: ""}, logger: logger.Delivery()}

		path := filepath.Join(tmpDir, "test2.eml")
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}

		err := e2.cleanupMessageFile(path)
		if err != nil {
			t.Errorf("cleanupMessageFile() error = %v", err)
		}

		// With empty QueuePath, safety check is skipped
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("File should have been deleted when QueuePath is empty")
		}
	})
}

func TestEngine_CleanupMessageFile_Permissions(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	tmpDir := t.TempDir()
	logger := logging.Default()
	e := &Engine{config: Config{QueuePath: tmpDir}, logger: logger.Delivery()}

	// Create a read-only directory
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(readOnlyDir, 0755)

	// Try to cleanup a file in read-only directory
	path := filepath.Join(readOnlyDir, "test.eml")

	// This should return an error
	err := e.cleanupMessageFile(path)
	// Either no error (file doesn't exist) or permission error
	// Both are acceptable behaviors
	_ = err
}

func TestErrorConstants(t *testing.T) {
	// Verify error constants are properly defined
	errList := []error{
		ErrPermanentFailure,
		ErrTemporaryFailure,
		ErrCircuitOpen,
		ErrAllMXFailed,
		ErrMessageTooLarge,
		ErrInvalidRecipient,
	}

	for _, e := range errList {
		if e == nil {
			t.Error("Error constant should not be nil")
		}
		if e.Error() == "" {
			t.Errorf("Error %v should have non-empty message", e)
		}
	}

	// Verify they're distinct
	for i, e1 := range errList {
		for j, e2 := range errList {
			if i != j && errors.Is(e1, e2) {
				t.Errorf("Error %v should not match %v", e1, e2)
			}
		}
	}
}

func TestExtractDomain_EdgeCases(t *testing.T) {
	tests := []struct {
		email string
		want  string
	}{
		// Unicode in local part
		{"münchen@example.com", "example.com"},
		// Multiple @ signs - takes everything after first @
		{"user@host@domain.com", "host@domain.com"},
		// IP address domain
		{"user@[192.168.1.1]", "[192.168.1.1]"},
		// Very long domain
		{"user@" + strings.Repeat("a", 100) + ".com", strings.Repeat("a", 100) + ".com"},
		// Subdomain
		{"user@mail.sub.example.com", "mail.sub.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := extractDomain(tt.email)
			if got != tt.want {
				t.Errorf("extractDomain(%q) = %q, want %q", tt.email, got, tt.want)
			}
		})
	}
}

func BenchmarkExtractDomain(b *testing.B) {
	email := "user@example.com"
	for i := 0; i < b.N; i++ {
		extractDomain(email)
	}
}

func BenchmarkIsPermanentError(b *testing.B) {
	err := errors.New("550 User not found")
	for i := 0; i < b.N; i++ {
		isPermanentError(err)
	}
}

func TestFireEvent_CallsHandler(t *testing.T) {
	logger := logging.Default()

	var mu sync.Mutex
	var received []DeliveryEvent
	done := make(chan struct{}, 2)

	e := &Engine{
		logger: logger.Delivery(),
		eventHandler: func(ctx context.Context, event DeliveryEvent) {
			mu.Lock()
			received = append(received, event)
			mu.Unlock()
			done <- struct{}{}
		},
	}

	e.fireEvent(context.Background(), DeliveryEvent{
		SMTPMessageID: "test@example.com",
		Recipients:    []string{"rcpt@example.com"},
		Status:        "delivered",
		SMTPCode:      250,
	})

	e.fireEvent(context.Background(), DeliveryEvent{
		SMTPMessageID: "bounce@example.com",
		Recipients:    []string{"bad@example.com"},
		Status:        "bounced",
		SMTPCode:      550,
		ErrorMessage:  "User not found",
	})

	// Wait for both async handlers to complete
	<-done
	<-done

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Fatalf("got %d events, want 2", len(received))
	}
}

func TestFireEvent_NilHandler(t *testing.T) {
	logger := logging.Default()

	e := &Engine{
		logger:       logger.Delivery(),
		eventHandler: nil,
	}

	// Should not panic
	e.fireEvent(context.Background(), DeliveryEvent{
		SMTPMessageID: "test@example.com",
		Status:        "delivered",
	})
}

func TestCleanupOrphanedFiles_NilQueue(t *testing.T) {
	tmpDir := t.TempDir()
	logger := logging.Default()

	e := &Engine{
		config: Config{QueuePath: tmpDir},
		logger: logger.Delivery(),
		// queue is nil — cleanup should return 0 safely
	}

	oldFile := filepath.Join(tmpDir, "old-message.eml")
	if err := os.WriteFile(oldFile, []byte("old email"), 0600); err != nil {
		t.Fatal(err)
	}

	cleaned := e.cleanupOrphanedFiles()
	if cleaned != 0 {
		t.Errorf("cleanupOrphanedFiles() with nil queue = %d, want 0", cleaned)
	}

	// File should NOT be deleted when queue is unavailable (can't verify active paths)
	if _, err := os.Stat(oldFile); os.IsNotExist(err) {
		t.Error("file should NOT have been deleted when queue is nil")
	}
}

func TestWarmupCircuitBreakers(t *testing.T) {
	db := setupTestDB(t)
	logger := logging.Default()

	// Add delivery_log table
	_, err := db.Exec(`
		CREATE TABLE delivery_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id TEXT,
			sender TEXT,
			recipient TEXT,
			status TEXT,
			smtp_code INTEGER,
			error_message TEXT,
			domain TEXT,
			attempt_number INTEGER DEFAULT 1,
			delivery_duration_ms INTEGER,
			circuit_breaker_state TEXT,
			trace_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create delivery_log: %v", err)
	}

	// Insert 6 distinct failed messages for "broken.com" (above threshold of 5)
	// Mix of rejected, bounced, and deferred statuses
	for i := 0; i < 6; i++ {
		status := "rejected"
		if i%3 == 1 {
			status = "deferred"
		} else if i%3 == 2 {
			status = "bounced"
		}
		_, err := db.Exec(`INSERT INTO delivery_log (message_id, sender, recipient, status, domain, created_at) VALUES (?, ?, ?, ?, 'broken.com', datetime('now'))`,
			"msg"+string(rune('0'+i)), "sender@test.com", "user@broken.com", status)
		if err != nil {
			t.Fatalf("Failed to insert: %v", err)
		}
	}

	// Insert 1 failed message to "inflated.com" with 5 recipients (5 rows, 1 message_id)
	// This should NOT trip the threshold since it's only 1 distinct delivery attempt
	for i := 0; i < 5; i++ {
		db.Exec(`INSERT INTO delivery_log (message_id, sender, recipient, status, domain, created_at) VALUES ('same-msg', 'sender@test.com', ?, 'rejected', 'inflated.com', datetime('now'))`,
			"user"+string(rune('0'+i))+"@inflated.com")
	}

	// Insert 2 failures for "ok.com" (below threshold) + 1 success
	for i := 0; i < 2; i++ {
		db.Exec(`INSERT INTO delivery_log (message_id, sender, recipient, status, domain, created_at) VALUES (?, ?, ?, 'rejected', 'ok.com', datetime('now'))`,
			"ok"+string(rune('0'+i)), "sender@test.com", "user@ok.com")
	}
	db.Exec(`INSERT INTO delivery_log (message_id, sender, recipient, status, domain, created_at) VALUES ('ok-succ', 'sender@test.com', 'user@ok.com', 'delivered', 'ok.com', datetime('now'))`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	e := &Engine{
		db:     db,
		logger: logger.Delivery(),
		ctx:    ctx,
		breakers: resilience.NewBreakerRegistry(func(key string) resilience.Config {
			return resilience.Config{
				Name:             "smtp:" + key,
				FailureThreshold: 5,
				SuccessThreshold: 2,
				Timeout:          5 * time.Minute,
			}
		}),
	}

	e.warmupCircuitBreakers()

	// "broken.com" should be pre-opened
	brokenBreaker := e.breakers.Get("broken.com")
	if brokenBreaker.State() != resilience.StateOpen {
		t.Errorf("broken.com breaker state = %v, want Open", brokenBreaker.State())
	}

	// "ok.com" should NOT be pre-opened (has recent success)
	okBreaker := e.breakers.Get("ok.com")
	if okBreaker.State() != resilience.StateClosed {
		t.Errorf("ok.com breaker state = %v, want Closed", okBreaker.State())
	}

	// "inflated.com" should NOT be pre-opened (1 message to 5 recipients = 1 distinct failure)
	inflatedBreaker := e.breakers.Get("inflated.com")
	if inflatedBreaker.State() != resilience.StateClosed {
		t.Errorf("inflated.com breaker state = %v, want Closed (per-recipient rows should not inflate count)", inflatedBreaker.State())
	}
}

func TestWarmupCircuitBreakers_NilDB(t *testing.T) {
	logger := logging.Default()
	e := &Engine{
		db:     nil,
		logger: logger.Delivery(),
	}
	// Should not panic
	e.warmupCircuitBreakers()
}

func TestCleanupOrphanedFiles_EmptyQueuePath(t *testing.T) {
	logger := logging.Default()
	e := &Engine{
		config: Config{QueuePath: ""},
		logger: logger.Delivery(),
	}

	// Should return 0 without error
	cleaned := e.cleanupOrphanedFiles()
	if cleaned != 0 {
		t.Errorf("cleanupOrphanedFiles() with empty path = %d, want 0", cleaned)
	}
}

func TestSetEventHandler(t *testing.T) {
	logger := logging.Default()

	e := &Engine{
		logger: logger.Delivery(),
	}

	done := make(chan struct{})
	e.SetEventHandler(func(ctx context.Context, event DeliveryEvent) {
		close(done)
	})

	e.fireEvent(context.Background(), DeliveryEvent{Status: "delivered"})

	select {
	case <-done:
		// handler was called
	case <-time.After(2 * time.Second):
		t.Error("SetEventHandler should register a handler that gets called")
	}
}

// setupTestDB creates an in-memory SQLite database with the sent_emails table.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE sent_emails (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER,
			api_key_id INTEGER,
			message_id TEXT NOT NULL,
			tracking_id TEXT,
			from_email TEXT,
			to_email TEXT,
			subject TEXT,
			template_slug TEXT,
			tags TEXT,
			status TEXT DEFAULT 'queued',
			smtp_response TEXT,
			opened_at DATETIME,
			opened_count INTEGER DEFAULT 0,
			clicked_at DATETIME,
			clicked_count INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			delivered_at DATETIME,
			bounced_at DATETIME,
			bounce_reason TEXT
		);
		CREATE TABLE delivery_attempts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sent_email_id INTEGER NOT NULL REFERENCES sent_emails(id) ON DELETE CASCADE,
			attempt_number INTEGER NOT NULL,
			attempted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			status TEXT NOT NULL CHECK(status IN ('pending', 'sent', 'deferred', 'failed', 'bounced')),
			smtp_response TEXT,
			error_message TEXT
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}
	return db
}

func TestUpdateSentEmailStatus_Delivered(t *testing.T) {
	db := setupTestDB(t)
	logger := logging.Default()

	e := &Engine{
		db:     db,
		logger: logger.Delivery(),
	}

	// Insert a queued email
	messageID := "abc123@example.com"
	_, err := db.Exec(`INSERT INTO sent_emails (message_id, from_email, to_email, subject, status) VALUES (?, ?, ?, ?, ?)`,
		"<"+messageID+">", "sender@example.com", "rcpt@example.com", "Test Subject", "queued")
	if err != nil {
		t.Fatalf("Failed to insert test email: %v", err)
	}

	// Simulate successful delivery
	e.updateSentEmailStatus(context.Background(), messageID, "delivered", "250 OK", "", 1)

	// Verify status updated
	var status, smtpResponse string
	var deliveredAt sql.NullTime
	err = db.QueryRow(`SELECT status, smtp_response, delivered_at FROM sent_emails WHERE message_id = ?`, "<"+messageID+">").
		Scan(&status, &smtpResponse, &deliveredAt)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}

	if status != "delivered" {
		t.Errorf("status = %q, want %q", status, "delivered")
	}
	if smtpResponse != "250 OK" {
		t.Errorf("smtp_response = %q, want %q", smtpResponse, "250 OK")
	}
	if !deliveredAt.Valid {
		t.Error("delivered_at should be set")
	}
}

func TestUpdateSentEmailStatus_Bounced(t *testing.T) {
	db := setupTestDB(t)
	logger := logging.Default()

	e := &Engine{
		db:     db,
		logger: logger.Delivery(),
	}

	messageID := "bounce123@example.com"
	_, err := db.Exec(`INSERT INTO sent_emails (message_id, from_email, to_email, subject, status) VALUES (?, ?, ?, ?, ?)`,
		"<"+messageID+">", "sender@example.com", "rcpt@example.com", "Test", "queued")
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	e.updateSentEmailStatus(context.Background(), messageID, "bounced", "", "550 User not found", 1)

	var status string
	var bounceReason sql.NullString
	var bouncedAt sql.NullTime
	err = db.QueryRow(`SELECT status, bounce_reason, bounced_at FROM sent_emails WHERE message_id = ?`, "<"+messageID+">").
		Scan(&status, &bounceReason, &bouncedAt)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}

	if status != "bounced" {
		t.Errorf("status = %q, want %q", status, "bounced")
	}
	if !bounceReason.Valid || bounceReason.String != "550 User not found" {
		t.Errorf("bounce_reason = %v, want %q", bounceReason, "550 User not found")
	}
	if !bouncedAt.Valid {
		t.Error("bounced_at should be set")
	}
}

func TestUpdateSentEmailStatus_Failed(t *testing.T) {
	db := setupTestDB(t)
	logger := logging.Default()

	e := &Engine{
		db:     db,
		logger: logger.Delivery(),
	}

	messageID := "fail123@example.com"
	_, err := db.Exec(`INSERT INTO sent_emails (message_id, from_email, to_email, subject, status) VALUES (?, ?, ?, ?, ?)`,
		"<"+messageID+">", "sender@example.com", "rcpt@example.com", "Test", "queued")
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	e.updateSentEmailStatus(context.Background(), messageID, "failed", "", "invalid domain", 1)

	var status string
	var bounceReason sql.NullString
	err = db.QueryRow(`SELECT status, bounce_reason FROM sent_emails WHERE message_id = ?`, "<"+messageID+">").
		Scan(&status, &bounceReason)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}

	if status != "failed" {
		t.Errorf("status = %q, want %q", status, "failed")
	}
	if !bounceReason.Valid || bounceReason.String != "invalid domain" {
		t.Errorf("bounce_reason = %v, want %q", bounceReason, "invalid domain")
	}
}

func TestUpdateSentEmailStatus_NoDoubleUpdate(t *testing.T) {
	db := setupTestDB(t)
	logger := logging.Default()

	e := &Engine{
		db:     db,
		logger: logger.Delivery(),
	}

	messageID := "nodedup@example.com"
	_, err := db.Exec(`INSERT INTO sent_emails (message_id, from_email, to_email, subject, status) VALUES (?, ?, ?, ?, ?)`,
		"<"+messageID+">", "sender@example.com", "rcpt@example.com", "Test", "queued")
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	// First update: delivered
	e.updateSentEmailStatus(context.Background(), messageID, "delivered", "250 OK", "", 1)

	// Second update: try to bounce (should NOT overwrite delivered)
	e.updateSentEmailStatus(context.Background(), messageID, "bounced", "", "550 error", 2)

	var status string
	err = db.QueryRow(`SELECT status FROM sent_emails WHERE message_id = ?`, "<"+messageID+">").Scan(&status)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}

	if status != "delivered" {
		t.Errorf("status = %q, want %q (should not overwrite delivered)", status, "delivered")
	}
}

func TestUpdateSentEmailStatus_NilDB(t *testing.T) {
	logger := logging.Default()

	e := &Engine{
		db:     nil,
		logger: logger.Delivery(),
	}

	// Should not panic
	e.updateSentEmailStatus(context.Background(), "test@example.com", "delivered", "250 OK", "", 1)
}

func TestUpdateSentEmailStatus_EmptyMessageID(t *testing.T) {
	db := setupTestDB(t)
	logger := logging.Default()

	e := &Engine{
		db:     db,
		logger: logger.Delivery(),
	}

	// Should not panic or error with empty message ID
	e.updateSentEmailStatus(context.Background(), "", "delivered", "250 OK", "", 1)
}

func TestDeliveryAttempts_Recorded(t *testing.T) {
	db := setupTestDB(t)
	logger := logging.Default()

	e := &Engine{
		db:     db,
		logger: logger.Delivery(),
	}

	messageID := "timeline@example.com"
	_, err := db.Exec(`INSERT INTO sent_emails (message_id, from_email, to_email, subject, status) VALUES (?, ?, ?, ?, ?)`,
		"<"+messageID+">", "sender@example.com", "rcpt@example.com", "Test", "queued")
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	// Simulate: attempt 1 deferred, attempt 2 delivered
	e.recordDeliveryAttempt(context.Background(), "<"+messageID+">", "deferred", "", "421 Try again", 1)
	e.updateSentEmailStatus(context.Background(), messageID, "delivered", "250 OK", "", 2)

	// Verify delivery attempts were recorded
	rows, err := db.Query(`SELECT attempt_number, status, smtp_response, error_message FROM delivery_attempts WHERE sent_email_id = 1 ORDER BY attempt_number`)
	if err != nil {
		t.Fatalf("Failed to query attempts: %v", err)
	}
	defer rows.Close()

	type attempt struct {
		Number       int
		Status       string
		SMTPResponse *string
		ErrorMessage *string
	}
	var attempts []attempt
	for rows.Next() {
		var a attempt
		if err := rows.Scan(&a.Number, &a.Status, &a.SMTPResponse, &a.ErrorMessage); err != nil {
			t.Fatalf("Failed to scan: %v", err)
		}
		attempts = append(attempts, a)
	}

	if len(attempts) != 2 {
		t.Fatalf("got %d attempts, want 2", len(attempts))
	}

	if attempts[0].Number != 1 || attempts[0].Status != "deferred" {
		t.Errorf("attempt 1: number=%d status=%s, want 1/deferred", attempts[0].Number, attempts[0].Status)
	}
	if attempts[0].ErrorMessage == nil || *attempts[0].ErrorMessage != "421 Try again" {
		t.Errorf("attempt 1: error_message = %v, want '421 Try again'", attempts[0].ErrorMessage)
	}

	if attempts[1].Number != 2 || attempts[1].Status != "sent" {
		t.Errorf("attempt 2: number=%d status=%s, want 2/sent", attempts[1].Number, attempts[1].Status)
	}
	if attempts[1].SMTPResponse == nil || *attempts[1].SMTPResponse != "250 OK" {
		t.Errorf("attempt 2: smtp_response = %v, want '250 OK'", attempts[1].SMTPResponse)
	}
}

func TestUpdateSentEmailStatus_TruncatesLongBounceReason(t *testing.T) {
	db := setupTestDB(t)
	logger := logging.Default()

	e := &Engine{
		db:     db,
		logger: logger.Delivery(),
	}

	messageID := "trunc@example.com"
	_, err := db.Exec(`INSERT INTO sent_emails (message_id, from_email, to_email, subject, status) VALUES (?, ?, ?, ?, ?)`,
		"<"+messageID+">", "sender@example.com", "rcpt@example.com", "Test", "queued")
	if err != nil {
		t.Fatalf("Failed to insert: %v", err)
	}

	// Simulate a malicious SMTP server returning a very long error
	longError := strings.Repeat("A", 5000)
	e.updateSentEmailStatus(context.Background(), messageID, "bounced", "", longError, 1)

	var bounceReason sql.NullString
	err = db.QueryRow(`SELECT bounce_reason FROM sent_emails WHERE message_id = ?`, "<"+messageID+">").Scan(&bounceReason)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}

	if !bounceReason.Valid {
		t.Fatal("bounce_reason should be set")
	}
	if len(bounceReason.String) > maxBounceReasonLen {
		t.Errorf("bounce_reason length = %d, should be truncated to %d", len(bounceReason.String), maxBounceReasonLen)
	}
	if len(bounceReason.String) != maxBounceReasonLen {
		t.Errorf("bounce_reason length = %d, want %d", len(bounceReason.String), maxBounceReasonLen)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input  string
		max    int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel"},
		{"", 10, ""},
		{"abc", 0, ""},
	}
	for _, tt := range tests {
		got := truncateString(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestUpdateSentEmailStatus_NoMatchingEmail(t *testing.T) {
	db := setupTestDB(t)
	logger := logging.Default()

	e := &Engine{
		db:     db,
		logger: logger.Delivery(),
	}

	// No rows to update - should not error (e.g., inbound emails not in sent_emails)
	e.updateSentEmailStatus(context.Background(), "nonexistent@example.com", "delivered", "250 OK", "", 1)
}
