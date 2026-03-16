package userportal

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
	_ "github.com/mattn/go-sqlite3"
)

func TestDashboardRoute_InjectsCSRFTokens(t *testing.T) {
	db := openUserPortalRouteTestDB(t)
	defer db.Close()

	createRouteTestUser(t, db)
	createRouteTestSession(t, db, "dashboard-token", 1, "203.0.113.10", "PortalVerifier/1.0")

	server := newRouteTestServer(t, db)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/account/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	req.Header.Set("User-Agent", "PortalVerifier/1.0")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "dashboard-token"})

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want %d", w.Code, http.StatusOK)
	}

	token := w.Header().Get("X-CSRF-Token")
	if len(token) != 64 {
		t.Fatalf("X-CSRF-Token length = %d, want 64", len(token))
	}

	body := w.Body.String()
	if !contains(body, `name="csrf_token" value="`+token+`"`) {
		t.Fatalf("dashboard body did not include logout csrf token")
	}
}

func TestLogoutRoute_RequiresPostAndValidCSRFToken(t *testing.T) {
	db := openUserPortalRouteTestDB(t)
	defer db.Close()

	createRouteTestUser(t, db)
	server := newRouteTestServer(t, db)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	t.Run("get is rejected and session remains", func(t *testing.T) {
		createRouteTestSession(t, db, "logout-get-token", 1, "203.0.113.10", "PortalVerifier/1.0")

		req := httptest.NewRequest(http.MethodGet, "/account/logout", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.10")
		req.Header.Set("User-Agent", "PortalVerifier/1.0")
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "logout-get-token"})

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET logout status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
		}
		assertSessionCount(t, db, "logout-get-token", 1)
	})

	t.Run("post without csrf is rejected and session remains", func(t *testing.T) {
		createRouteTestSession(t, db, "logout-no-csrf-token", 1, "203.0.113.10", "PortalVerifier/1.0")

		req := httptest.NewRequest(http.MethodPost, "/account/logout", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.10")
		req.Header.Set("User-Agent", "PortalVerifier/1.0")
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "logout-no-csrf-token"})

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("POST logout without csrf status = %d, want %d", w.Code, http.StatusForbidden)
		}
		assertSessionCount(t, db, "logout-no-csrf-token", 1)
	})

	t.Run("post with csrf deletes session and redirects", func(t *testing.T) {
		createRouteTestSession(t, db, "logout-csrf-token", 1, "203.0.113.10", "PortalVerifier/1.0")

		getReq := httptest.NewRequest(http.MethodGet, "/account/", nil)
		getReq.RemoteAddr = "127.0.0.1:1234"
		getReq.Header.Set("X-Forwarded-For", "203.0.113.10")
		getReq.Header.Set("User-Agent", "PortalVerifier/1.0")
		getReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "logout-csrf-token"})

		getW := httptest.NewRecorder()
		mux.ServeHTTP(getW, getReq)

		csrfToken := getW.Header().Get("X-CSRF-Token")
		if len(csrfToken) != 64 {
			t.Fatalf("dashboard csrf token length = %d, want 64", len(csrfToken))
		}

		postReq := httptest.NewRequest(http.MethodPost, "/account/logout?csrf_token="+csrfToken, nil)
		postReq.RemoteAddr = "127.0.0.1:1234"
		postReq.Header.Set("X-Forwarded-For", "203.0.113.10")
		postReq.Header.Set("User-Agent", "PortalVerifier/1.0")
		postReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "logout-csrf-token"})

		postW := httptest.NewRecorder()
		mux.ServeHTTP(postW, postReq)

		if postW.Code != http.StatusSeeOther {
			t.Fatalf("POST logout with csrf status = %d, want %d", postW.Code, http.StatusSeeOther)
		}
		if location := postW.Header().Get("Location"); location != "/account/login" {
			t.Fatalf("logout redirect location = %q, want %q", location, "/account/login")
		}
		assertSessionCount(t, db, "logout-csrf-token", 0)
	})

	t.Run("csrf token from another session is rejected", func(t *testing.T) {
		createRouteTestSession(t, db, "logout-session-a", 1, "203.0.113.10", "PortalVerifier/1.0")
		createRouteTestSession(t, db, "logout-session-b", 1, "203.0.113.10", "PortalVerifier/1.0")

		getReq := httptest.NewRequest(http.MethodGet, "/account/", nil)
		getReq.RemoteAddr = "127.0.0.1:1234"
		getReq.Header.Set("X-Forwarded-For", "203.0.113.10")
		getReq.Header.Set("User-Agent", "PortalVerifier/1.0")
		getReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "logout-session-a"})

		getW := httptest.NewRecorder()
		mux.ServeHTTP(getW, getReq)

		csrfToken := getW.Header().Get("X-CSRF-Token")
		if len(csrfToken) != 64 {
			t.Fatalf("session a csrf token length = %d, want 64", len(csrfToken))
		}

		postReq := httptest.NewRequest(http.MethodPost, "/account/logout?csrf_token="+csrfToken, nil)
		postReq.RemoteAddr = "127.0.0.1:1234"
		postReq.Header.Set("X-Forwarded-For", "203.0.113.10")
		postReq.Header.Set("User-Agent", "PortalVerifier/1.0")
		postReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "logout-session-b"})

		postW := httptest.NewRecorder()
		mux.ServeHTTP(postW, postReq)

		if postW.Code != http.StatusForbidden {
			t.Fatalf("cross-session logout status = %d, want %d", postW.Code, http.StatusForbidden)
		}
		assertSessionCount(t, db, "logout-session-a", 1)
		assertSessionCount(t, db, "logout-session-b", 1)
	})
}

func newRouteTestServer(t *testing.T, db *sql.DB) *Server {
	t.Helper()

	logger, err := logging.New(logging.DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	server, err := NewServer(db, nil, nil, logger)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	return server
}

func openUserPortalRouteTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	schema := `
		CREATE TABLE domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL,
			mail_hostname TEXT,
			is_primary BOOLEAN DEFAULT FALSE
		);
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL,
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL DEFAULT '',
			display_name TEXT,
			quota_bytes INTEGER DEFAULT 0,
			used_bytes INTEGER DEFAULT 0,
			is_active BOOLEAN DEFAULT TRUE,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE user_sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			ip_address TEXT,
			user_agent TEXT
		);
		CREATE TABLE user_forwarding (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL UNIQUE,
			forward_to TEXT,
			keep_copy BOOLEAN DEFAULT TRUE,
			is_active BOOLEAN DEFAULT FALSE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE sieve_scripts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			is_active BOOLEAN DEFAULT FALSE
		);
		CREATE TABLE user_vacation (
			user_id INTEGER PRIMARY KEY,
			subject TEXT,
			start_date DATETIME,
			end_date DATETIME
		);
		CREATE TABLE vacation_responses (
			user_id INTEGER PRIMARY KEY,
			subject TEXT,
			message TEXT,
			start_date DATETIME,
			end_date DATETIME,
			is_active BOOLEAN DEFAULT FALSE
		);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		t.Fatalf("failed to create schema: %v", err)
	}

	return db
}

func createRouteTestUser(t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.Exec(`INSERT INTO domains (id, name, mail_hostname, is_primary) VALUES (1, 'example.com', 'mail.example.com', TRUE)`); err != nil {
		t.Fatalf("failed to insert domain: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, domain_id, username, display_name, quota_bytes, used_bytes, is_active) VALUES (1, 1, 'portal', 'Portal User', 1073741824, 0, TRUE)`); err != nil {
		t.Fatalf("failed to insert user: %v", err)
	}
}

func createRouteTestSession(t *testing.T, db *sql.DB, token string, userID int64, ipAddress, userAgent string) {
	t.Helper()

	if _, err := db.Exec(`DELETE FROM user_sessions WHERE token = ?`, token); err != nil {
		t.Fatalf("failed to clear session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO user_sessions (token, user_id, expires_at, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?)
	`, token, userID, time.Now().Add(2*time.Hour), ipAddress, userAgent); err != nil {
		t.Fatalf("failed to insert session: %v", err)
	}
}

func assertSessionCount(t *testing.T, db *sql.DB, token string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_sessions WHERE token = ?`, token).Scan(&got); err != nil {
		t.Fatalf("failed to query session count: %v", err)
	}
	if got != want {
		t.Fatalf("session count for %q = %d, want %d", token, got, want)
	}
}

func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
