package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/audit"
	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
	_ "github.com/mattn/go-sqlite3"
)

func setupRollbackHandlerTest(t *testing.T) (*Server, func(), int64, string, string) {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	schema := `
	CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		is_admin BOOLEAN DEFAULT FALSE
	);
	CREATE TABLE admin_sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL
	);
	CREATE TABLE update_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		update_type TEXT NOT NULL,
		from_version TEXT,
		to_version TEXT,
		from_commit TEXT,
		to_commit TEXT,
		pr_number INTEGER,
		branch_name TEXT,
		status TEXT NOT NULL,
		started_by TEXT NOT NULL,
		started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		completed_at DATETIME,
		duration_seconds INTEGER,
		backup_path TEXT,
		rollback_available BOOLEAN DEFAULT 1,
		error_message TEXT,
		metadata TEXT
	);
	CREATE TABLE rollback_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		update_id INTEGER NOT NULL,
		snapshot_type TEXT DEFAULT 'pre_update',
		version TEXT NOT NULL,
		commit_sha TEXT NOT NULL,
		binary_path TEXT NOT NULL,
		backup_path TEXT NOT NULL,
		config_snapshot TEXT,
		health_status TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME
	);
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		t.Fatalf("failed to create schema: %v", err)
	}

	logger := logging.Default()
	auditLogger, err := audit.NewLogger(db)
	if err != nil {
		db.Close()
		t.Fatalf("failed to create audit logger: %v", err)
	}

	buildDir := t.TempDir()
	binaryPath := filepath.Join(buildDir, "mailserver")
	backupDir := filepath.Join(buildDir, "backups", "pre-update-1")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		db.Close()
		t.Fatalf("failed to create backup dir: %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("new binary"), 0o750); err != nil {
		db.Close()
		t.Fatalf("failed to write binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "mailserver-binary"), []byte("old binary"), 0o750); err != nil {
		db.Close()
		t.Fatalf("failed to write backup binary: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO users (id, username, is_admin) VALUES (1, 'admin', 1)`); err != nil {
		db.Close()
		t.Fatalf("failed to insert admin user: %v", err)
	}
	token := strings.Repeat("a", 32)
	if _, err := db.Exec(`INSERT INTO admin_sessions (token, user_id, expires_at) VALUES (?, ?, ?)`, token, 1, time.Now().Add(time.Hour)); err != nil {
		db.Close()
		t.Fatalf("failed to insert session: %v", err)
	}
	result, err := db.Exec(`INSERT INTO update_history (update_type, from_version, to_version, from_commit, to_commit, status, started_by, backup_path, rollback_available) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`, "release", "v1.0.0", "v1.1.0", "oldcommit", "newcommit", "failed", "admin", backupDir)
	if err != nil {
		db.Close()
		t.Fatalf("failed to insert update history: %v", err)
	}
	updateID, err := result.LastInsertId()
	if err != nil {
		db.Close()
		t.Fatalf("failed to get update id: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO rollback_snapshots (update_id, snapshot_type, version, commit_sha, binary_path, backup_path) VALUES (?, 'pre_update', ?, ?, ?, ?)`, updateID, "v1.0.0", "oldcommit", binaryPath, backupDir); err != nil {
		db.Close()
		t.Fatalf("failed to insert rollback snapshot: %v", err)
	}

	cfg := &config.Config{
		Server: config.ServerConfig{Hostname: "mail.example.com"},
		Updater: config.UpdaterConfig{
			GitRepoURL:     "https://github.com/fenilsonani/email-server",
			BuildPath:      buildDir,
			BinaryPath:     binaryPath,
			SystemdService: "mailserver.service",
		},
	}
	server := &Server{
		db:          db,
		logger:      logger,
		auditLogger: auditLogger,
		config:      cfg,
	}

	cleanup := func() { db.Close() }
	return server, cleanup, updateID, token, binaryPath
}

func TestUpdateHandlersGetClientIP_TrustedLocalProxyOnly(t *testing.T) {
	t.Run("uses forwarded header from trusted localhost proxy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/system/update", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "198.51.100.8, 203.0.113.10")

		if got := getClientIP(req); got != "203.0.113.10" {
			t.Fatalf("getClientIP() = %q, want %q", got, "203.0.113.10")
		}
	})

	t.Run("ignores spoofed forwarded header from remote client", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/system/update", nil)
		req.RemoteAddr = "198.51.100.25:2525"
		req.Header.Set("X-Forwarded-For", "203.0.113.10")

		if got := getClientIP(req); got != "198.51.100.25" {
			t.Fatalf("getClientIP() = %q, want %q", got, "198.51.100.25")
		}
	})
}

func TestHandleRollbackUpdate(t *testing.T) {
	server, cleanup, updateID, token, binaryPath := setupRollbackHandlerTest(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/admin/system/update/rollback/1", nil)
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: token})
	req.URL.Path = "/admin/system/update/rollback/" + strconv.FormatInt(updateID, 10)
	w := httptest.NewRecorder()

	server.HandleRollbackUpdate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if success, _ := resp["success"].(bool); !success {
		t.Fatalf("expected success response, got %#v", resp)
	}
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("failed to read restored binary: %v", err)
	}
	if string(data) != "old binary" {
		t.Fatalf("binary not restored, got %q", string(data))
	}
	var status string
	var rollbackAvailable bool
	if err := server.db.QueryRow(`SELECT status, rollback_available FROM update_history WHERE id = ?`, updateID).Scan(&status, &rollbackAvailable); err != nil {
		t.Fatalf("failed to query update history: %v", err)
	}
	if status != "rolled_back" {
		t.Fatalf("status = %q, want rolled_back", status)
	}
	if rollbackAvailable {
		t.Fatalf("rollback_available = true, want false")
	}
}
