package greylist

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var dbCounter int

func setupTestDB(t *testing.T) *sql.DB {
	// Use unique file-based database for each test to avoid cross-test pollution
	dbCounter++
	dbPath := fmt.Sprintf("file:testdb_%d?mode=memory&cache=shared", dbCounter)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	// Set connection pool to 1 to avoid issues with in-memory databases
	db.SetMaxOpenConns(1)
	return db
}

func TestNew(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gl, err := New(db, DefaultConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if gl == nil {
		t.Fatal("New() returned nil")
	}

	if !gl.IsEnabled() {
		t.Error("Greylister should be enabled by default")
	}

	// Verify table was created
	var tableName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='greylist'").Scan(&tableName)
	if err != nil {
		t.Errorf("greylist table was not created: %v", err)
	}
}

func TestNewNilDB(t *testing.T) {
	gl, err := New(nil, DefaultConfig())
	if err != nil {
		t.Errorf("New(nil) should not return error, got: %v", err)
	}
	if gl != nil {
		t.Error("New(nil) should return nil greylister")
	}
}

func TestNewDisabled(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := DefaultConfig()
	cfg.Enabled = false

	gl, err := New(db, cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if gl.IsEnabled() {
		t.Error("Greylister should be disabled when Enabled=false")
	}
}

func TestCheckNewTriplet(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gl, err := New(db, Config{
		Enabled:  true,
		MinDelay: 1 * time.Second, // Short delay for testing
		MaxAge:   24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// First check should defer (firstTime=true)
	allow, firstTime, err := gl.Check(ctx, "192.168.1.1", "sender@example.com", "recipient@example.com")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if allow {
		t.Error("New triplet should be deferred")
	}
	if !firstTime {
		t.Error("New triplet should be marked as firstTime")
	}
}

func TestCheckAfterDelay(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gl, err := New(db, Config{
		Enabled:  true,
		MinDelay: 50 * time.Millisecond, // Very short delay for testing
		MaxAge:   24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	senderIP := "10.20.30.40"
	sender := "delay-sender@example.com"
	recipient := "delay-recipient@example.com"

	// First check - should defer
	allow, firstTime, err := gl.Check(ctx, senderIP, sender, recipient)
	if err != nil {
		t.Fatalf("First Check() error = %v", err)
	}
	if allow {
		t.Error("First check should be deferred")
	}
	if !firstTime {
		t.Error("First check should be firstTime=true")
	}

	// Wait for delay to pass
	time.Sleep(100 * time.Millisecond)

	// Check after delay - should pass
	allow, _, err = gl.Check(ctx, senderIP, sender, recipient)
	if err != nil {
		t.Fatalf("Check after delay error = %v", err)
	}
	if !allow {
		t.Error("Check after delay should be allowed")
	}
}

func TestCheckPassed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gl, err := New(db, Config{
		Enabled:  true,
		MinDelay: 100 * time.Millisecond,
		MaxAge:   24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	senderIP := "192.168.1.1"
	sender := "sender@example.com"
	recipient := "recipient@example.com"

	// First check - defer
	gl.Check(ctx, senderIP, sender, recipient)

	// Wait and pass
	time.Sleep(150 * time.Millisecond)
	gl.Check(ctx, senderIP, sender, recipient)

	// Subsequent checks should pass immediately
	allow, _, err := gl.Check(ctx, senderIP, sender, recipient)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !allow {
		t.Error("Previously passed triplet should be allowed immediately")
	}
}

func TestCheckDisabled(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	cfg := DefaultConfig()
	cfg.Enabled = false

	gl, err := New(db, cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// When disabled, should always allow
	allow, firstTime, err := gl.Check(ctx, "192.168.1.1", "sender@example.com", "recipient@example.com")
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !allow {
		t.Error("Disabled greylister should always allow")
	}
	if firstTime {
		t.Error("Disabled greylister should return firstTime=false")
	}
}

func TestCheckNilGreylister(t *testing.T) {
	var gl *Greylister
	ctx := context.Background()

	allow, firstTime, err := gl.Check(ctx, "192.168.1.1", "sender@example.com", "recipient@example.com")
	if err != nil {
		t.Errorf("Check on nil greylister should not error: %v", err)
	}
	if !allow {
		t.Error("Nil greylister should allow")
	}
	if firstTime {
		t.Error("Nil greylister should return firstTime=false")
	}
}

func TestNormalizeIP(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"IPv4 single", "192.168.1.100", "192.168.1.0"},
		{"IPv4 with port", "192.168.1.100:12345", "192.168.1.0"},
		{"IPv4 different subnet", "10.0.5.100", "10.0.5.0"},
		{"IPv6", "2001:db8::1", "2001:db8::"},
		{"localhost v4", "127.0.0.1", "127.0.0.0"},
		{"invalid", "not-an-ip", "not-an-ip"}, // Returns as-is if invalid
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeIP(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeIP(%s) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDifferentTriplets(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gl, err := New(db, Config{
		Enabled:  true,
		MinDelay: 1 * time.Second,
		MaxAge:   24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	triplets := []struct {
		ip        string
		sender    string
		recipient string
	}{
		{"192.168.1.1", "sender1@example.com", "recipient@example.com"},
		{"192.168.1.1", "sender2@example.com", "recipient@example.com"},
		{"192.168.2.1", "sender1@example.com", "recipient@example.com"},
		{"192.168.1.1", "sender1@example.com", "other@example.com"},
	}

	for _, triplet := range triplets {
		allow, firstTime, err := gl.Check(ctx, triplet.ip, triplet.sender, triplet.recipient)
		if err != nil {
			t.Errorf("Check() error = %v", err)
		}
		if allow {
			t.Errorf("New triplet (%s, %s, %s) should be deferred", triplet.ip, triplet.sender, triplet.recipient)
		}
		if !firstTime {
			t.Errorf("New triplet should be firstTime=true")
		}
	}

	// Verify we have 4 entries in the greylist
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM greylist").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count: %v", err)
	}
	if count != 4 {
		t.Errorf("Expected 4 greylist entries, got %d", count)
	}
}

func TestStats(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gl, err := New(db, Config{
		Enabled:  true,
		MinDelay: 50 * time.Millisecond,
		MaxAge:   24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Add some entries
	gl.Check(ctx, "192.168.1.1", "sender1@example.com", "recipient@example.com")
	gl.Check(ctx, "192.168.1.1", "sender2@example.com", "recipient@example.com")

	// Wait and pass one
	time.Sleep(100 * time.Millisecond)
	gl.Check(ctx, "192.168.1.1", "sender1@example.com", "recipient@example.com")

	total, passed, pending, err := gl.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}

	if total != 2 {
		t.Errorf("Stats total = %d, want 2", total)
	}
	if passed != 1 {
		t.Errorf("Stats passed = %d, want 1", passed)
	}
	if pending != 1 {
		t.Errorf("Stats pending = %d, want 1", pending)
	}
}

func TestCleanup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Create greylister but we'll manipulate timestamps directly
	gl, err := New(db, Config{
		Enabled:  true,
		MinDelay: 50 * time.Millisecond,
		MaxAge:   1 * time.Second, // Short for testing
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// Add an entry with old timestamp directly
	oldTime := time.Now().Add(-2 * time.Second).Format("2006-01-02 15:04:05")
	_, err = db.Exec(
		"INSERT INTO greylist (sender_ip, sender, recipient, first_seen, passed, last_seen) VALUES (?, ?, ?, ?, FALSE, ?)",
		"192.168.100.0", "cleanup-sender@example.com", "cleanup-recipient@example.com", oldTime, oldTime,
	)
	if err != nil {
		t.Fatalf("Failed to insert test entry: %v", err)
	}

	// Verify entry exists
	var count int
	db.QueryRow("SELECT COUNT(*) FROM greylist WHERE sender = 'cleanup-sender@example.com'").Scan(&count)
	if count != 1 {
		t.Fatalf("Expected 1 entry before cleanup, got %d", count)
	}

	// Run cleanup - should remove the old entry
	err = gl.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	// Verify entry was removed
	db.QueryRow("SELECT COUNT(*) FROM greylist WHERE sender = 'cleanup-sender@example.com'").Scan(&count)
	if count != 0 {
		t.Errorf("Expected 0 entries after cleanup, got %d", count)
	}
}

func TestCaseSensitivity(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gl, err := New(db, Config{
		Enabled:  true,
		MinDelay: 1 * time.Second,
		MaxAge:   24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()

	// First check with lowercase
	gl.Check(ctx, "192.168.1.1", "sender@example.com", "recipient@example.com")

	// Second check with different case - should be same triplet
	_, firstTime, _ := gl.Check(ctx, "192.168.1.1", "SENDER@EXAMPLE.COM", "RECIPIENT@EXAMPLE.COM")

	if firstTime {
		t.Error("Different case should be treated as same triplet")
	}

	// Verify only one entry
	var count int
	db.QueryRow("SELECT COUNT(*) FROM greylist").Scan(&count)
	if count != 1 {
		t.Errorf("Expected 1 entry (case-insensitive), got %d", count)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Enabled {
		t.Error("Default config should be enabled")
	}

	if cfg.MinDelay != 5*time.Minute {
		t.Errorf("Default MinDelay = %v, want 5m", cfg.MinDelay)
	}

	if cfg.MaxAge != 35*24*time.Hour {
		t.Errorf("Default MaxAge = %v, want 35d", cfg.MaxAge)
	}
}

func TestIsEnabled(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Nil greylister
	var nilGL *Greylister
	if nilGL.IsEnabled() {
		t.Error("Nil greylister should not be enabled")
	}

	// Disabled greylister
	disabledGL, _ := New(db, Config{Enabled: false})
	if disabledGL.IsEnabled() {
		t.Error("Disabled greylister should not be enabled")
	}

	// Enabled greylister
	enabledGL, _ := New(db, Config{Enabled: true})
	if !enabledGL.IsEnabled() {
		t.Error("Enabled greylister should be enabled")
	}
}

func TestConcurrentChecks(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	gl, err := New(db, Config{
		Enabled:  true,
		MinDelay: 1 * time.Second,
		MaxAge:   24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	numGoroutines := 10

	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			_, _, err := gl.Check(ctx, "192.168.1.1", "sender@example.com", "recipient@example.com")
			if err != nil {
				t.Errorf("Concurrent Check() error = %v", err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Should still have just one entry
	var count int
	db.QueryRow("SELECT COUNT(*) FROM greylist").Scan(&count)
	if count != 1 {
		t.Errorf("Expected 1 entry after concurrent checks, got %d", count)
	}
}
