package api

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Create suppression_list table
	_, err = db.Exec(`
		CREATE TABLE suppression_list (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL,
			email TEXT NOT NULL,
			reason TEXT NOT NULL CHECK(reason IN ('hard_bounce', 'unsubscribe', 'complaint', 'manual')),
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(domain_id, email)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create suppression_list table: %v", err)
	}

	return db
}

func TestSuppressionService_Add(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewSuppressionService(db)
	ctx := context.Background()

	// Add a suppression
	sup, err := svc.Add(ctx, 1, "bounce@example.com", SuppressionHardBounce)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if sup.Email != "bounce@example.com" {
		t.Errorf("Email mismatch: got %s", sup.Email)
	}
	if sup.Reason != SuppressionHardBounce {
		t.Errorf("Reason mismatch: got %s", sup.Reason)
	}
	if sup.DomainID != 1 {
		t.Errorf("DomainID mismatch: got %d", sup.DomainID)
	}
}

func TestSuppressionService_AddNormalizesEmail(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewSuppressionService(db)
	ctx := context.Background()

	// Add with uppercase and whitespace
	_, err := svc.Add(ctx, 1, "  USER@EXAMPLE.COM  ", SuppressionManual)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Check that it's stored normalized
	suppressed, sup, err := svc.IsSuppressed(ctx, 1, "user@example.com")
	if err != nil {
		t.Fatalf("IsSuppressed failed: %v", err)
	}
	if !suppressed {
		t.Error("Expected email to be suppressed")
	}
	if sup.Email != "user@example.com" {
		t.Errorf("Email not normalized: got %s", sup.Email)
	}
}

func TestSuppressionService_IsSuppressed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewSuppressionService(db)
	ctx := context.Background()

	// Add a suppression
	_, err := svc.Add(ctx, 1, "blocked@example.com", SuppressionUnsubscribe)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Check suppressed email
	suppressed, sup, err := svc.IsSuppressed(ctx, 1, "blocked@example.com")
	if err != nil {
		t.Fatalf("IsSuppressed failed: %v", err)
	}
	if !suppressed {
		t.Error("Expected email to be suppressed")
	}
	if sup.Reason != SuppressionUnsubscribe {
		t.Errorf("Wrong reason: got %s", sup.Reason)
	}

	// Check non-suppressed email
	suppressed, _, err = svc.IsSuppressed(ctx, 1, "allowed@example.com")
	if err != nil {
		t.Fatalf("IsSuppressed failed: %v", err)
	}
	if suppressed {
		t.Error("Expected email to not be suppressed")
	}
}

func TestSuppressionService_Remove(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewSuppressionService(db)
	ctx := context.Background()

	// Add and then remove
	_, err := svc.Add(ctx, 1, "remove@example.com", SuppressionManual)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	err = svc.Remove(ctx, 1, "remove@example.com")
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify removed
	suppressed, _, err := svc.IsSuppressed(ctx, 1, "remove@example.com")
	if err != nil {
		t.Fatalf("IsSuppressed failed: %v", err)
	}
	if suppressed {
		t.Error("Email should no longer be suppressed")
	}
}

func TestSuppressionService_RemoveNonExistent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewSuppressionService(db)
	ctx := context.Background()

	err := svc.Remove(ctx, 1, "nonexistent@example.com")
	if err != sql.ErrNoRows {
		t.Errorf("Expected sql.ErrNoRows, got %v", err)
	}
}

func TestSuppressionService_DomainIsolation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewSuppressionService(db)
	ctx := context.Background()

	// Add suppression for domain 1
	_, err := svc.Add(ctx, 1, "user@example.com", SuppressionHardBounce)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Check domain 1 - should be suppressed
	suppressed, _, err := svc.IsSuppressed(ctx, 1, "user@example.com")
	if err != nil {
		t.Fatalf("IsSuppressed failed: %v", err)
	}
	if !suppressed {
		t.Error("Email should be suppressed for domain 1")
	}

	// Check domain 2 - should NOT be suppressed
	suppressed, _, err = svc.IsSuppressed(ctx, 2, "user@example.com")
	if err != nil {
		t.Fatalf("IsSuppressed failed: %v", err)
	}
	if suppressed {
		t.Error("Email should not be suppressed for domain 2")
	}
}

func TestSuppressionService_List(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewSuppressionService(db)
	ctx := context.Background()

	// Add multiple suppressions
	emails := []string{"a@test.com", "b@test.com", "c@test.com", "d@test.com", "e@test.com"}
	for _, email := range emails {
		_, err := svc.Add(ctx, 1, email, SuppressionManual)
		if err != nil {
			t.Fatalf("Add failed: %v", err)
		}
	}

	// List first page
	suppressions, total, err := svc.List(ctx, 1, 1, 3)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if total != 5 {
		t.Errorf("Total mismatch: got %d, want 5", total)
	}
	if len(suppressions) != 3 {
		t.Errorf("Page size mismatch: got %d, want 3", len(suppressions))
	}

	// List second page
	suppressions, _, err = svc.List(ctx, 1, 2, 3)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(suppressions) != 2 {
		t.Errorf("Second page size mismatch: got %d, want 2", len(suppressions))
	}
}

func TestSuppressionService_AddUpdatesReason(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewSuppressionService(db)
	ctx := context.Background()

	// Add with one reason
	_, err := svc.Add(ctx, 1, "update@example.com", SuppressionHardBounce)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Add again with different reason (should update)
	_, err = svc.Add(ctx, 1, "update@example.com", SuppressionComplaint)
	if err != nil {
		t.Fatalf("Second Add failed: %v", err)
	}

	// Check reason was updated
	_, sup, err := svc.IsSuppressed(ctx, 1, "update@example.com")
	if err != nil {
		t.Fatalf("IsSuppressed failed: %v", err)
	}
	if sup.Reason != SuppressionComplaint {
		t.Errorf("Reason not updated: got %s, want %s", sup.Reason, SuppressionComplaint)
	}
}

func TestIsValidSuppressionReason(t *testing.T) {
	tests := []struct {
		reason string
		valid  bool
	}{
		{SuppressionHardBounce, true},
		{SuppressionUnsubscribe, true},
		{SuppressionComplaint, true},
		{SuppressionManual, true},
		{"invalid", false},
		{"", false},
		{"soft_bounce", false},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			result := IsValidSuppressionReason(tt.reason)
			if result != tt.valid {
				t.Errorf("IsValidSuppressionReason(%q) = %v, want %v", tt.reason, result, tt.valid)
			}
		})
	}
}

func TestSuppressionService_InvalidReason(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	svc := NewSuppressionService(db)
	ctx := context.Background()

	_, err := svc.Add(ctx, 1, "test@example.com", "invalid_reason")
	if err == nil {
		t.Error("Expected error for invalid reason")
	}
}
