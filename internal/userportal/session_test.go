package userportal

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
	_ "github.com/mattn/go-sqlite3"
)

func TestGetClientIP_TrustedProxyHandling(t *testing.T) {
	s := &Server{
		trustedProxies: map[string]bool{
			"127.0.0.1": true,
			"::1":       true,
		},
	}

	t.Run("trusted proxy uses forwarded header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/account/login", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "198.51.100.8, 203.0.113.10")

		if got := s.getClientIP(req); got != "203.0.113.10" {
			t.Fatalf("getClientIP() = %q, want %q", got, "203.0.113.10")
		}
	})

	t.Run("untrusted client ignores forwarded header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/account/login", nil)
		req.RemoteAddr = "198.51.100.25:2500"
		req.Header.Set("X-Forwarded-For", "203.0.113.10")

		if got := s.getClientIP(req); got != "198.51.100.25" {
			t.Fatalf("getClientIP() = %q, want %q", got, "198.51.100.25")
		}
	})
}

func TestValidateSession_BindsToStoredClientMetadata(t *testing.T) {
	db := openUserPortalTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	expiresAt := now.Add(2 * time.Hour)

	if _, err := db.Exec(`
		INSERT INTO user_sessions (token, user_id, created_at, expires_at, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "token-1", 42, now, expiresAt, "203.0.113.10", "Mozilla/5.0"); err != nil {
		t.Fatalf("failed to insert session: %v", err)
	}

	s := newTestUserPortalServer(t, db)

	req := httptest.NewRequest(http.MethodGet, "/account/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	userID, valid := s.validateSession(req, "token-1")
	if !valid || userID != 42 {
		t.Fatalf("validateSession() = (%d, %v), want (42, true)", userID, valid)
	}
}

func TestValidateSession_RejectsMismatchedClientMetadata(t *testing.T) {
	db := openUserPortalTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	expiresAt := now.Add(2 * time.Hour)

	if _, err := db.Exec(`
		INSERT INTO user_sessions (token, user_id, created_at, expires_at, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "token-2", 7, now, expiresAt, "203.0.113.10", "Mozilla/5.0"); err != nil {
		t.Fatalf("failed to insert session: %v", err)
	}

	s := newTestUserPortalServer(t, db)

	req := httptest.NewRequest(http.MethodGet, "/account/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.25")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	userID, valid := s.validateSession(req, "token-2")
	if valid || userID != 0 {
		t.Fatalf("validateSession() = (%d, %v), want (0, false)", userID, valid)
	}

	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_sessions WHERE token = ?`, "token-2").Scan(&remaining); err != nil {
		t.Fatalf("failed to query session count: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("session should be deleted on metadata mismatch, remaining=%d", remaining)
	}
}

func TestValidateSession_AcceptsLegacyStoredRemoteAddr(t *testing.T) {
	db := openUserPortalTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	expiresAt := now.Add(2 * time.Hour)

	if _, err := db.Exec(`
		INSERT INTO user_sessions (token, user_id, created_at, expires_at, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "token-legacy", 9, now, expiresAt, "203.0.113.10:4321", "Mozilla/5.0"); err != nil {
		t.Fatalf("failed to insert session: %v", err)
	}

	s := newTestUserPortalServer(t, db)

	req := httptest.NewRequest(http.MethodGet, "/account/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	userID, valid := s.validateSession(req, "token-legacy")
	if !valid || userID != 9 {
		t.Fatalf("validateSession() = (%d, %v), want (9, true)", userID, valid)
	}
}

func TestValidateSession_AcceptsLegacySessionWithoutBoundMetadata(t *testing.T) {
	db := openUserPortalTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	expiresAt := now.Add(2 * time.Hour)

	if _, err := db.Exec(`
		INSERT INTO user_sessions (token, user_id, created_at, expires_at, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "token-no-metadata", 11, now, expiresAt, "", ""); err != nil {
		t.Fatalf("failed to insert session: %v", err)
	}

	s := newTestUserPortalServer(t, db)

	req := httptest.NewRequest(http.MethodGet, "/account/", nil)
	req.RemoteAddr = "198.51.100.77:1234"
	req.Header.Set("User-Agent", "Mozilla/5.0")

	userID, valid := s.validateSession(req, "token-no-metadata")
	if !valid || userID != 11 {
		t.Fatalf("validateSession() = (%d, %v), want (11, true)", userID, valid)
	}
}

func TestValidateSession_RejectsMissingBoundUserAgent(t *testing.T) {
	db := openUserPortalTestDB(t)
	defer db.Close()

	now := time.Now().UTC()
	expiresAt := now.Add(2 * time.Hour)

	if _, err := db.Exec(`
		INSERT INTO user_sessions (token, user_id, created_at, expires_at, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "token-missing-ua", 13, now, expiresAt, "203.0.113.10", "Mozilla/5.0"); err != nil {
		t.Fatalf("failed to insert session: %v", err)
	}

	s := newTestUserPortalServer(t, db)

	req := httptest.NewRequest(http.MethodGet, "/account/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")

	userID, valid := s.validateSession(req, "token-missing-ua")
	if valid || userID != 0 {
		t.Fatalf("validateSession() = (%d, %v), want (0, false)", userID, valid)
	}
}

func TestCreateSession_StoresNormalizedClientMetadata(t *testing.T) {
	db := openUserPortalTestDB(t)
	defer db.Close()

	s := newTestUserPortalServer(t, db)

	req := httptest.NewRequest(http.MethodPost, "/account/login", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.8, 203.0.113.44")
	req.Header.Set("User-Agent", longUserAgent(300))

	token, err := s.createSession(context.Background(), 55, req)
	if err != nil {
		t.Fatalf("createSession() error = %v", err)
	}

	var storedIP, storedUserAgent string
	if err := db.QueryRow(`
		SELECT ip_address, user_agent FROM user_sessions WHERE token = ?
	`, token).Scan(&storedIP, &storedUserAgent); err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if storedIP != "203.0.113.44" {
		t.Fatalf("stored ip_address = %q, want %q", storedIP, "203.0.113.44")
	}
	if len(storedUserAgent) != 255 {
		t.Fatalf("stored user_agent length = %d, want 255", len(storedUserAgent))
	}
}

func TestCreateSession_IgnoresForwardedHeadersFromUntrustedSource(t *testing.T) {
	db := openUserPortalTestDB(t)
	defer db.Close()

	s := newTestUserPortalServer(t, db)

	req := httptest.NewRequest(http.MethodPost, "/account/login", nil)
	req.RemoteAddr = "198.51.100.90:2525"
	req.Header.Set("X-Forwarded-For", "203.0.113.44")
	req.Header.Set("User-Agent", "Mozilla/5.0")

	token, err := s.createSession(context.Background(), 56, req)
	if err != nil {
		t.Fatalf("createSession() error = %v", err)
	}

	var storedIP string
	if err := db.QueryRow(`SELECT ip_address FROM user_sessions WHERE token = ?`, token).Scan(&storedIP); err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if storedIP != "198.51.100.90" {
		t.Fatalf("stored ip_address = %q, want %q", storedIP, "198.51.100.90")
	}
}

func TestNormalizeIP_RejectsHostnamesWithPort(t *testing.T) {
	if got := normalizeIP("mail.example.com:587"); got != "" {
		t.Fatalf("normalizeIP() = %q, want empty string", got)
	}
}

func newTestUserPortalServer(t *testing.T, db *sql.DB) *Server {
	t.Helper()

	logger, err := logging.New(logging.DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	return &Server{
		db:     db,
		logger: logger,
		trustedProxies: map[string]bool{
			"127.0.0.1": true,
			"::1":       true,
		},
	}
}

func openUserPortalTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	schema := `
		CREATE TABLE user_sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			ip_address TEXT,
			user_agent TEXT
		);
	`
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		t.Fatalf("failed to create schema: %v", err)
	}

	return db
}

func longUserAgent(length int) string {
	buf := make([]byte, length)
	for i := range buf {
		buf[i] = 'a'
	}
	return string(buf)
}
