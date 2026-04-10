package userportal

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fenilsonani/email-server/internal/logging"
	_ "github.com/mattn/go-sqlite3"
)

func TestHandleForwarding_RejectsInvalidEmail(t *testing.T) {
	db := openPortalValidationTestDB(t)
	defer db.Close()

	s := newPortalValidationTestServer(t, db)
	req := httptest.NewRequest(http.MethodPost, "/account/forwarding", strings.NewReader("forward_to=not-an-email&keep_copy=on&is_active=on"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userIDContextKey, int64(1)))
	w := httptest.NewRecorder()

	s.handleForwarding(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "Enter a valid forwarding email address") {
		t.Fatalf("response body did not include validation error: %s", w.Body.String())
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_forwarding WHERE user_id = 1`).Scan(&count); err != nil {
		t.Fatalf("failed to query forwarding row count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no forwarding row, found %d", count)
	}
}

func TestHandleVacation_RejectsEndBeforeStart(t *testing.T) {
	db := openPortalValidationTestDB(t)
	defer db.Close()

	s := newPortalValidationTestServer(t, db)
	req := httptest.NewRequest(http.MethodPost, "/account/vacation", strings.NewReader("subject=Out+of+office&message=Away&start_date=2026-04-10&end_date=2026-04-05&is_active=on"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), userIDContextKey, int64(1)))
	w := httptest.NewRecorder()

	s.handleVacation(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), "End date must be after the start date") {
		t.Fatalf("response body did not include validation error: %s", w.Body.String())
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM vacation_responses WHERE user_id = 1`).Scan(&count); err != nil {
		t.Fatalf("failed to query vacation row count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no vacation row, found %d", count)
	}
}

func newPortalValidationTestServer(t *testing.T, db *sql.DB) *Server {
	t.Helper()

	logger, err := logging.New(logging.DefaultConfig())
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	tmpl := template.Must(template.New("forwarding.html").Parse(`{{.Error}}{{.Success}}`))
	vacationTmpl := template.Must(template.New("vacation.html").Parse(`{{.Error}}{{.Success}}`))

	return &Server{
		db:     db,
		logger: logger,
		templates: map[string]*template.Template{
			"forwarding.html": tmpl,
			"vacation.html":   vacationTmpl,
		},
	}
}

func openPortalValidationTestDB(t *testing.T) *sql.DB {
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
			name TEXT NOT NULL UNIQUE
		);
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL,
			username TEXT NOT NULL,
			display_name TEXT,
			is_active BOOLEAN DEFAULT TRUE,
			quota_bytes INTEGER DEFAULT 0,
			used_bytes INTEGER DEFAULT 0
		);
		CREATE TABLE user_forwarding (
			user_id INTEGER PRIMARY KEY,
			forward_to TEXT,
			keep_copy BOOLEAN,
			is_active BOOLEAN,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE vacation_responses (
			user_id INTEGER PRIMARY KEY,
			subject TEXT,
			message TEXT,
			start_date DATETIME,
			end_date DATETIME,
			is_active BOOLEAN
		);
		INSERT INTO domains (id, name) VALUES (1, 'example.com');
		INSERT INTO users (id, domain_id, username, display_name, is_active, quota_bytes, used_bytes)
		VALUES (1, 1, 'alice', 'Alice', 1, 1024, 128);
	`
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		t.Fatalf("failed to create schema: %v", err)
	}

	return db
}
