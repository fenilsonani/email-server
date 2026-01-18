package admin

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/features"
	"github.com/fenilsonani/email-server/internal/logging"
	_ "github.com/mattn/go-sqlite3"
)

// testServer creates a minimal Server for testing
func setupTestServer(t *testing.T) (*Server, func()) {
	// Create temp database
	tmpFile, err := os.CreateTemp("", "admin_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()

	db, err := sql.Open("sqlite3", tmpFile.Name()+"?_foreign_keys=on")
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to open database: %v", err)
	}

	// Create required schema
	schema := `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			username TEXT UNIQUE,
			password_hash TEXT,
			is_admin BOOLEAN DEFAULT FALSE
		);
		CREATE TABLE domains (id INTEGER PRIMARY KEY, name TEXT, domain TEXT);
		CREATE TABLE admin_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token TEXT UNIQUE NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		CREATE TABLE screener_contacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			email TEXT,
			domain TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, email),
			UNIQUE(user_id, domain),
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		CREATE TABLE email_aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			domain_id INTEGER,
			alias_local TEXT NOT NULL,
			alias_address TEXT UNIQUE NOT NULL,
			description TEXT,
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at DATETIME,
			email_count INTEGER DEFAULT 0,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		CREATE TABLE vip_contacts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			email TEXT NOT NULL,
			name TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, email),
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		CREATE TABLE user_preferences (
			user_id INTEGER PRIMARY KEY,
			undo_send_delay INTEGER DEFAULT 10,
			screener_enabled BOOLEAN DEFAULT TRUE,
			tracker_blocking TEXT DEFAULT 'block',
			zones_enabled BOOLEAN DEFAULT FALSE,
			snooze_mark_unread BOOLEAN DEFAULT TRUE,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		INSERT INTO users (id, username, password_hash, is_admin) VALUES (1, 'testuser', 'hash', TRUE);
		INSERT INTO domains (id, name, domain) VALUES (1, 'example.com', 'example.com');
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to create schema: %v", err)
	}

	logger := logging.Default()
	featuresStore := features.NewStore(db)

	// Create minimal server
	server := &Server{
		db:            db,
		featuresStore: featuresStore,
		logger:        logger,
		templates:     make(map[string]*template.Template),
	}

	// Load templates for testing
	if err := loadTestTemplates(server); err != nil {
		db.Close()
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to load templates: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}

	return server, cleanup
}

// loadTestTemplates creates minimal templates for testing
func loadTestTemplates(s *Server) error {
	// Create a simple template for each page
	pages := []string{
		"features.html",
		"features_screener.html",
		"features_aliases.html",
		"features_alias_form.html",
		"features_vip.html",
		"features_vip_form.html",
		"features_preferences.html",
	}

	for _, page := range pages {
		tmpl, err := template.New(page).Parse(`<!DOCTYPE html><html><body>{{.Title}}</body></html>`)
		if err != nil {
			return err
		}
		s.templates[page] = tmpl
	}

	return nil
}

// createTestSession creates a session in the database and returns the token
func createTestSession(t *testing.T, db *sql.DB, userID int64) string {
	// Generate a valid hex token (32 hex chars = 16 bytes)
	token := "0123456789abcdef0123456789abcdef"
	expiresAt := time.Now().Add(24 * time.Hour)

	// Clear any existing sessions first
	db.Exec(`DELETE FROM admin_sessions WHERE user_id = ?`, userID)

	_, err := db.Exec(`
		INSERT INTO admin_sessions (user_id, token, expires_at)
		VALUES (?, ?, ?)
	`, userID, token, expiresAt)
	if err != nil {
		t.Fatalf("Failed to create test session: %v", err)
	}
	return token
}

// addSessionCookie adds a session cookie to the request
func addSessionCookie(req *http.Request, token string) {
	req.AddCookie(&http.Cookie{
		Name:  "admin_session",
		Value: token,
	})
}

// =============================================================================
// Features Overview Tests
// =============================================================================

func TestHandleFeatures_NoAuth(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/admin/features", nil)
	rec := httptest.NewRecorder()

	server.handleFeatures(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect to login, got status %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "/admin/login") {
		t.Errorf("Expected redirect to login, got %s", rec.Header().Get("Location"))
	}
}

func TestHandleFeatures_WithAuth(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	req := httptest.NewRequest(http.MethodGet, "/admin/features", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleFeatures(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected OK status, got %d", rec.Code)
	}
}

// =============================================================================
// Screener Tests
// =============================================================================

func TestHandleScreener_WithAuth(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	// Add test contact
	ctx := context.Background()
	server.featuresStore.ApproveContact(ctx, 1, "friend@example.com", "")

	req := httptest.NewRequest(http.MethodGet, "/admin/features/screener", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleScreener(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected OK status, got %d", rec.Code)
	}
}

func TestHandleScreenerAction_Approve(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)
	ctx := context.Background()

	// Add a pending contact directly to DB for testing
	_, err := server.db.ExecContext(ctx, `
		INSERT INTO screener_contacts (user_id, email, status)
		VALUES (1, 'pending@example.com', 'pending')
	`)
	if err != nil {
		t.Fatalf("Failed to insert test contact: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/features/screener/approve/1", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleScreenerAction(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect, got status %d", rec.Code)
	}
}

func TestHandleScreenerAction_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	req := httptest.NewRequest(http.MethodGet, "/admin/features/screener/approve/1", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleScreenerAction(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected MethodNotAllowed, got status %d", rec.Code)
	}
}

// =============================================================================
// Aliases Tests
// =============================================================================

func TestHandleAliases_WithAuth(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	// Create test alias
	ctx := context.Background()
	alias := &features.EmailAlias{
		UserID:       1,
		DomainID:     1,
		AliasLocal:   "test",
		AliasAddress: "test@example.com",
		Description:  "Test alias",
	}
	server.featuresStore.CreateAlias(ctx, alias)

	req := httptest.NewRequest(http.MethodGet, "/admin/features/aliases", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleAliases(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected OK status, got %d", rec.Code)
	}
}

func TestHandleAliasAdd_GET(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	req := httptest.NewRequest(http.MethodGet, "/admin/features/aliases/add", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleAliasAdd(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected OK status, got %d", rec.Code)
	}
}

func TestHandleAliasAdd_POST(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	form := url.Values{}
	form.Add("domain_id", "1")
	form.Add("description", "Shopping alias")

	req := httptest.NewRequest(http.MethodPost, "/admin/features/aliases/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleAliasAdd(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect after creation, got status %d", rec.Code)
	}

	// Verify alias was created
	aliases, _ := server.featuresStore.ListAliases(context.Background(), 1)
	if len(aliases) != 1 {
		t.Errorf("Expected 1 alias, got %d", len(aliases))
	}
}

func TestHandleAliasToggle(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	// Create test alias
	ctx := context.Background()
	alias := &features.EmailAlias{
		UserID:       1,
		DomainID:     1,
		AliasLocal:   "toggle-test",
		AliasAddress: "toggle-test@example.com",
	}
	server.featuresStore.CreateAlias(ctx, alias)

	req := httptest.NewRequest(http.MethodPost, "/admin/features/aliases/toggle/1", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleAliasToggle(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect, got status %d", rec.Code)
	}
}

func TestHandleAliasDelete(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	// Create test alias
	ctx := context.Background()
	alias := &features.EmailAlias{
		UserID:       1,
		DomainID:     1,
		AliasLocal:   "delete-test",
		AliasAddress: "delete-test@example.com",
	}
	server.featuresStore.CreateAlias(ctx, alias)

	req := httptest.NewRequest(http.MethodPost, "/admin/features/aliases/delete/1", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleAliasDelete(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect, got status %d", rec.Code)
	}

	// Verify alias was deleted
	aliases, _ := server.featuresStore.ListAliases(ctx, 1)
	if len(aliases) != 0 {
		t.Errorf("Expected 0 aliases after delete, got %d", len(aliases))
	}
}

// =============================================================================
// VIP Tests
// =============================================================================

func TestHandleVIP_WithAuth(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	// Add test VIP
	ctx := context.Background()
	vip := &features.VIPContact{
		UserID: 1,
		Email:  "vip@example.com",
		Name:   "Important Person",
	}
	server.featuresStore.AddVIP(ctx, vip)

	req := httptest.NewRequest(http.MethodGet, "/admin/features/vip", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleVIP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected OK status, got %d", rec.Code)
	}
}

func TestHandleVIPAdd_GET(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	req := httptest.NewRequest(http.MethodGet, "/admin/features/vip/add", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleVIPAdd(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected OK status, got %d", rec.Code)
	}
}

func TestHandleVIPAdd_POST(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	form := url.Values{}
	form.Add("email", "newvip@example.com")
	form.Add("name", "New VIP Person")

	req := httptest.NewRequest(http.MethodPost, "/admin/features/vip/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleVIPAdd(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect after creation, got status %d", rec.Code)
	}

	// Verify VIP was created
	vips, _ := server.featuresStore.ListVIPs(context.Background(), 1)
	if len(vips) != 1 {
		t.Errorf("Expected 1 VIP, got %d", len(vips))
	}
}

func TestHandleVIPRemove(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	// Create test VIP
	ctx := context.Background()
	vip := &features.VIPContact{
		UserID: 1,
		Email:  "todelete@example.com",
	}
	server.featuresStore.AddVIP(ctx, vip)

	req := httptest.NewRequest(http.MethodPost, "/admin/features/vip/delete/1", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleVIPRemove(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect, got status %d", rec.Code)
	}

	// Verify VIP was deleted
	vips, _ := server.featuresStore.ListVIPs(ctx, 1)
	if len(vips) != 0 {
		t.Errorf("Expected 0 VIPs after delete, got %d", len(vips))
	}
}

// =============================================================================
// Preferences Tests
// =============================================================================

func TestHandlePreferences_GET(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	req := httptest.NewRequest(http.MethodGet, "/admin/features/preferences", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handlePreferences(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected OK status, got %d", rec.Code)
	}
}

func TestHandlePreferences_POST(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	form := url.Values{}
	form.Add("undo_send_delay", "30")
	form.Add("screener_enabled", "on")
	form.Add("tracker_blocking", "proxy")
	form.Add("zones_enabled", "on")
	form.Add("snooze_mark_unread", "on")

	req := httptest.NewRequest(http.MethodPost, "/admin/features/preferences", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handlePreferences(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected OK status after save, got %d", rec.Code)
	}

	// Verify preferences were saved
	prefs, _ := server.featuresStore.GetPreferences(context.Background(), 1)
	if prefs.UndoSendDelay != 30 {
		t.Errorf("Expected UndoSendDelay 30, got %d", prefs.UndoSendDelay)
	}
	if prefs.TrackerBlocking != "proxy" {
		t.Errorf("Expected TrackerBlocking 'proxy', got '%s'", prefs.TrackerBlocking)
	}
	if !prefs.ZonesEnabled {
		t.Error("Expected ZonesEnabled to be true")
	}
}

// =============================================================================
// Features Disabled Tests
// =============================================================================

func TestHandleScreener_FeaturesDisabled(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)

	// Disable features store
	server.featuresStore = nil

	req := httptest.NewRequest(http.MethodGet, "/admin/features/screener", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleScreener(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected ServiceUnavailable when features disabled, got %d", rec.Code)
	}
}

func TestHandleAliases_FeaturesDisabled(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)
	server.featuresStore = nil

	req := httptest.NewRequest(http.MethodGet, "/admin/features/aliases", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleAliases(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected ServiceUnavailable when features disabled, got %d", rec.Code)
	}
}

func TestHandleVIP_FeaturesDisabled(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)
	server.featuresStore = nil

	req := httptest.NewRequest(http.MethodGet, "/admin/features/vip", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handleVIP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected ServiceUnavailable when features disabled, got %d", rec.Code)
	}
}

func TestHandlePreferences_FeaturesDisabled(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	token := createTestSession(t, server.db, 1)
	server.featuresStore = nil

	req := httptest.NewRequest(http.MethodGet, "/admin/features/preferences", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()

	server.handlePreferences(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected ServiceUnavailable when features disabled, got %d", rec.Code)
	}
}
