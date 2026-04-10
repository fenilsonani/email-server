package api

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// setupAPIKeyTestServer builds an in-memory SQLite Server with an api_keys
// table populated with one valid key. Returns the server, the bare key
// string the caller can pass to validateAPIKey, and the row ID so tests
// can mutate the row before calling validateAPIKey.
func setupAPIKeyTestServer(t *testing.T, scopesJSON string) (*Server, string, int64) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL,
			key_hash TEXT NOT NULL,
			key_prefix TEXT NOT NULL,
			name TEXT NOT NULL,
			scopes TEXT NOT NULL,
			is_active BOOLEAN DEFAULT TRUE,
			rate_limit_per_hour INTEGER DEFAULT 1000,
			last_used_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME,
			key_salt TEXT
		)
	`); err != nil {
		t.Fatalf("failed to create api_keys table: %v", err)
	}

	fullKey, prefix, hash, salt, err := GenerateAPIKey(true)
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}

	res, err := db.Exec(`
		INSERT INTO api_keys (domain_id, key_hash, key_prefix, name, scopes, is_active, rate_limit_per_hour, key_salt)
		VALUES (1, ?, ?, 'test-key', ?, TRUE, 1000, ?)
	`, hash, prefix, scopesJSON, salt)
	if err != nil {
		t.Fatalf("failed to insert api key row: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}

	return &Server{db: db}, fullKey, id
}

// TestValidateAPIKey_RejectsMalformedScopes locks in the fix for the
// previously-silent json.Unmarshal at middleware.go:144. Before the fix
// a corrupted scopes column would silently authenticate the request
// with apiKey.Scopes left as a nil slice — every downstream "permission
// denied" was impossible to root-cause. Now it returns a wrapped error
// so the request fails loudly and the corruption is observable in logs.
func TestValidateAPIKey_RejectsMalformedScopes(t *testing.T) {
	server, fullKey, _ := setupAPIKeyTestServer(t, "this-is-not-valid-json")

	got, err := server.validateAPIKey(context.Background(), fullKey)
	if err == nil {
		t.Fatalf("validateAPIKey() returned nil error for malformed scopes; got key with scopes=%v", got.Scopes)
	}
	if got != nil {
		t.Fatalf("validateAPIKey() returned non-nil APIKey on error: %#v", got)
	}
	if !strings.Contains(err.Error(), "malformed scopes") {
		t.Fatalf("error message %q does not mention 'malformed scopes'", err.Error())
	}
}

// TestValidateAPIKey_AcceptsValidScopes confirms the success path still
// works after the error-handling change.
func TestValidateAPIKey_AcceptsValidScopes(t *testing.T) {
	server, fullKey, _ := setupAPIKeyTestServer(t, `["email:send","email:read"]`)

	apiKey, err := server.validateAPIKey(context.Background(), fullKey)
	if err != nil {
		t.Fatalf("validateAPIKey() error = %v, want nil", err)
	}
	if apiKey == nil {
		t.Fatal("validateAPIKey() returned nil APIKey")
	}
	if len(apiKey.Scopes) != 2 || apiKey.Scopes[0] != "email:send" || apiKey.Scopes[1] != "email:read" {
		t.Fatalf("scopes mismatch: got %#v, want [email:send email:read]", apiKey.Scopes)
	}
}

// TestValidateAPIKey_AcceptsEmptyScopes confirms a key with no scopes
// (the "" sentinel that skips Unmarshal entirely) still authenticates.
func TestValidateAPIKey_AcceptsEmptyScopes(t *testing.T) {
	server, fullKey, _ := setupAPIKeyTestServer(t, "")

	apiKey, err := server.validateAPIKey(context.Background(), fullKey)
	if err != nil {
		t.Fatalf("validateAPIKey() error = %v, want nil", err)
	}
	if len(apiKey.Scopes) != 0 {
		t.Fatalf("expected empty scopes, got %#v", apiKey.Scopes)
	}
}

