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

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
	_ "github.com/mattn/go-sqlite3"
)

// setupRBACTestServer creates a server with full RBAC schema for testing
func setupRBACTestServer(t *testing.T) (*Server, func()) {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "rbac_test_*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	tmpFile.Close()

	db, err := sql.Open("sqlite3", tmpFile.Name()+"?_foreign_keys=on")
	if err != nil {
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to open database: %v", err)
	}

	schema := `
		CREATE TABLE domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			is_active BOOLEAN DEFAULT TRUE,
			is_primary BOOLEAN DEFAULT FALSE,
			mail_hostname TEXT,
			dkim_selector TEXT DEFAULT 'mail',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			domain_id INTEGER NOT NULL REFERENCES domains(id),
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			display_name TEXT,
			is_admin BOOLEAN DEFAULT FALSE,
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(username, domain_id)
		);

		CREATE TABLE admin_sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL
		);

		CREATE TABLE roles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE permissions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT
		);

		CREATE TABLE role_permissions (
			role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
			permission_id INTEGER NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
			PRIMARY KEY (role_id, permission_id)
		);

		CREATE TABLE user_roles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
			domain_id INTEGER REFERENCES domains(id) ON DELETE CASCADE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX idx_user_roles_unique ON user_roles(user_id, role_id, COALESCE(domain_id, 0));

		-- Seed roles
		INSERT INTO roles (name, description) VALUES
			('super_admin', 'Full access'),
			('domain_admin', 'Domain-scoped access'),
			('support', 'Read-only + password reset');

		-- Seed permissions
		INSERT INTO permissions (name, description) VALUES
			('users.create', 'Create users'),
			('users.read', 'View users'),
			('users.update', 'Edit users'),
			('users.delete', 'Delete users'),
			('users.password', 'Reset passwords'),
			('domains.create', 'Add domains'),
			('domains.read', 'View domains'),
			('domains.update', 'Edit domains'),
			('domains.delete', 'Delete domains'),
			('aliases.manage', 'Manage aliases'),
			('lists.manage', 'Manage lists'),
			('logs.view', 'View logs'),
			('audit.view', 'View audit logs'),
			('settings.manage', 'Manage settings'),
			('features.manage', 'Manage features'),
			('queue.manage', 'Manage queue');

		-- super_admin gets all permissions
		INSERT INTO role_permissions (role_id, permission_id)
			SELECT r.id, p.id FROM roles r, permissions p WHERE r.name = 'super_admin';

		-- domain_admin gets scoped permissions
		INSERT INTO role_permissions (role_id, permission_id)
			SELECT r.id, p.id FROM roles r, permissions p
			WHERE r.name = 'domain_admin' AND p.name IN (
				'users.create', 'users.read', 'users.update', 'users.delete', 'users.password',
				'domains.read', 'aliases.manage', 'lists.manage', 'logs.view', 'features.manage'
			);

		-- support gets read-only permissions
		INSERT INTO role_permissions (role_id, permission_id)
			SELECT r.id, p.id FROM roles r, permissions p
			WHERE r.name = 'support' AND p.name IN (
				'users.read', 'users.password', 'domains.read', 'logs.view', 'audit.view'
			);

		-- Seed domains
		INSERT INTO domains (id, name, is_primary) VALUES (1, 'example.com', TRUE);
		INSERT INTO domains (id, name) VALUES (2, 'other.com');
		INSERT INTO domains (id, name) VALUES (3, 'third.com');

		-- Seed users
		INSERT INTO users (id, domain_id, username, password_hash, is_admin) VALUES
			(1, 1, 'superadmin', 'hash', TRUE),
			(2, 1, 'domainadmin', 'hash', TRUE),
			(3, 1, 'support', 'hash', TRUE),
			(4, 2, 'otheruser', 'hash', FALSE),
			(5, 1, 'regularuser', 'hash', FALSE),
			(6, 3, 'thirduser', 'hash', FALSE);

		-- Assign roles
		INSERT INTO user_roles (user_id, role_id)
			SELECT 1, id FROM roles WHERE name = 'super_admin';
		INSERT INTO user_roles (user_id, role_id, domain_id)
			SELECT 2, id, 1 FROM roles WHERE name = 'domain_admin';
		INSERT INTO user_roles (user_id, role_id)
			SELECT 3, id FROM roles WHERE name = 'support';
	`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		os.Remove(tmpFile.Name())
		t.Fatalf("Failed to create schema: %v", err)
	}

	logger := logging.Default()
	server := &Server{
		db:        db,
		logger:    logger,
		templates: make(map[string]*template.Template),
		config: &config.Config{
			Server: config.ServerConfig{
				Hostname: "mail.example.com",
			},
		},
	}

	// Load test templates
	pages := []string{
		"users.html", "user_form.html", "user_edit.html",
		"login.html", "dashboard.html",
	}
	for _, page := range pages {
		tmpl, _ := template.New(page).Parse(`<!DOCTYPE html><html><body>{{.Title}}</body></html>`)
		server.templates[page] = tmpl
	}

	cleanup := func() {
		db.Close()
		os.Remove(tmpFile.Name())
	}

	return server, cleanup
}

func createRBACSession(t *testing.T, db *sql.DB, userID int64) string {
	t.Helper()
	token := "abcdef0123456789abcdef0123456789"
	// Use unique token per user to avoid conflicts
	token = token[:30] + strings.Repeat("0", 2-len(string(rune('0'+userID%10)))) + string(rune('0'+userID%10)) + "f"
	if len(token) < 32 {
		token = token + strings.Repeat("0", 32-len(token))
	}
	token = token[:32]

	expiresAt := time.Now().Add(24 * time.Hour)
	db.Exec(`DELETE FROM admin_sessions WHERE user_id = ?`, userID)
	_, err := db.Exec(`INSERT INTO admin_sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, expiresAt)
	if err != nil {
		t.Fatalf("Failed to create session for user %d: %v", userID, err)
	}

	// Also populate the in-memory cache
	sessionCacheMu.Lock()
	sessionCache[token] = &session{
		userID:    userID,
		createdAt: time.Now(),
		expiresAt: expiresAt,
	}
	sessionCacheMu.Unlock()

	return token
}

// =============================================================================
// AdminUser unit tests
// =============================================================================

func TestAdminUser_HasPermission(t *testing.T) {
	user := &AdminUser{
		ID:          1,
		Email:       "admin@example.com",
		Role:        "super_admin",
		Permissions: []string{"users.read", "users.create", "domains.read"},
	}

	tests := []struct {
		perm string
		want bool
	}{
		{"users.read", true},
		{"users.create", true},
		{"domains.read", true},
		{"domains.delete", false},
		{"settings.manage", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.perm, func(t *testing.T) {
			got := user.HasPermission(tt.perm)
			if got != tt.want {
				t.Errorf("HasPermission(%q) = %v, want %v", tt.perm, got, tt.want)
			}
		})
	}
}

func TestAdminUser_HasDomainAccess(t *testing.T) {
	t.Run("global_access", func(t *testing.T) {
		user := &AdminUser{
			ID:        1,
			Role:      "super_admin",
			DomainIDs: nil, // nil means global
		}
		if !user.HasDomainAccess(1) {
			t.Error("Super admin should have access to any domain")
		}
		if !user.HasDomainAccess(999) {
			t.Error("Super admin should have access to any domain")
		}
	})

	t.Run("scoped_access", func(t *testing.T) {
		user := &AdminUser{
			ID:        2,
			Role:      "domain_admin",
			DomainIDs: []int64{1, 3},
		}
		if !user.HasDomainAccess(1) {
			t.Error("Domain admin should have access to assigned domain 1")
		}
		if !user.HasDomainAccess(3) {
			t.Error("Domain admin should have access to assigned domain 3")
		}
		if user.HasDomainAccess(2) {
			t.Error("Domain admin should NOT have access to unassigned domain 2")
		}
		if user.HasDomainAccess(999) {
			t.Error("Domain admin should NOT have access to unassigned domain 999")
		}
	})

	t.Run("empty_domain_ids_means_global", func(t *testing.T) {
		user := &AdminUser{
			ID:        3,
			Role:      "support",
			DomainIDs: []int64{},
		}
		// Empty slice (not nil) means global
		if !user.HasDomainAccess(1) {
			t.Error("User with empty DomainIDs should have global access")
		}
	})
}

func TestGetAdminUser_NotInContext(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	user := GetAdminUser(req)
	if user != nil {
		t.Error("GetAdminUser should return nil when not in context")
	}
}

func TestGetAdminUser_InContext(t *testing.T) {
	expected := &AdminUser{ID: 42, Email: "test@example.com", Role: "super_admin"}
	req := httptest.NewRequest("GET", "/", nil)
	ctx := context.WithValue(req.Context(), adminUserContextKey, expected)
	req = req.WithContext(ctx)

	got := GetAdminUser(req)
	if got == nil {
		t.Fatal("GetAdminUser should return user from context")
	}
	if got.ID != expected.ID {
		t.Errorf("Got user ID %d, want %d", got.ID, expected.ID)
	}
	if got.Role != expected.Role {
		t.Errorf("Got role %q, want %q", got.Role, expected.Role)
	}
}

// =============================================================================
// loadAdminUser tests
// =============================================================================

func TestLoadAdminUser_SuperAdmin(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	user, err := server.loadAdminUser(context.Background(), 1) // superadmin
	if err != nil {
		t.Fatalf("loadAdminUser failed: %v", err)
	}
	if user == nil {
		t.Fatal("Expected non-nil user for super_admin")
	}
	if user.Role != "super_admin" {
		t.Errorf("Role = %q, want super_admin", user.Role)
	}
	if user.Email != "superadmin@example.com" {
		t.Errorf("Email = %q, want superadmin@example.com", user.Email)
	}
	if len(user.Permissions) != 16 {
		t.Errorf("Permissions count = %d, want 16 (all)", len(user.Permissions))
	}
	if len(user.DomainIDs) != 0 {
		t.Errorf("DomainIDs should be empty for super_admin, got %v", user.DomainIDs)
	}
}

func TestLoadAdminUser_DomainAdmin(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	user, err := server.loadAdminUser(context.Background(), 2) // domainadmin
	if err != nil {
		t.Fatalf("loadAdminUser failed: %v", err)
	}
	if user == nil {
		t.Fatal("Expected non-nil user for domain_admin")
	}
	if user.Role != "domain_admin" {
		t.Errorf("Role = %q, want domain_admin", user.Role)
	}

	// domain_admin should have 10 permissions
	if len(user.Permissions) != 10 {
		t.Errorf("Permissions count = %d, want 10", len(user.Permissions))
	}

	// Should have users.create but not domains.delete
	if !user.HasPermission("users.create") {
		t.Error("domain_admin should have users.create")
	}
	if user.HasPermission("domains.delete") {
		t.Error("domain_admin should NOT have domains.delete")
	}
	if user.HasPermission("settings.manage") {
		t.Error("domain_admin should NOT have settings.manage")
	}

	// Should be scoped to domain 1
	if len(user.DomainIDs) != 1 {
		t.Errorf("DomainIDs count = %d, want 1", len(user.DomainIDs))
	}
	if len(user.DomainIDs) > 0 && user.DomainIDs[0] != 1 {
		t.Errorf("DomainID = %d, want 1", user.DomainIDs[0])
	}
}

func TestLoadAdminUser_Support(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	user, err := server.loadAdminUser(context.Background(), 3) // support
	if err != nil {
		t.Fatalf("loadAdminUser failed: %v", err)
	}
	if user == nil {
		t.Fatal("Expected non-nil user for support")
	}
	if user.Role != "support" {
		t.Errorf("Role = %q, want support", user.Role)
	}

	// support should have 5 permissions
	if len(user.Permissions) != 5 {
		t.Errorf("Permissions count = %d, want 5", len(user.Permissions))
	}

	// Should have users.read but not users.create
	if !user.HasPermission("users.read") {
		t.Error("support should have users.read")
	}
	if !user.HasPermission("audit.view") {
		t.Error("support should have audit.view")
	}
	if user.HasPermission("users.create") {
		t.Error("support should NOT have users.create")
	}
	if user.HasPermission("users.delete") {
		t.Error("support should NOT have users.delete")
	}
}

func TestLoadAdminUser_RegularUserDenied(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	user, err := server.loadAdminUser(context.Background(), 5) // regularuser (no role, not admin)
	if err != nil {
		t.Fatalf("loadAdminUser error: %v", err)
	}
	if user != nil {
		t.Error("Regular user without is_admin or role should return nil")
	}
}

func TestLoadAdminUser_NonExistentUser(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	user, err := server.loadAdminUser(context.Background(), 999)
	if err == nil && user != nil {
		t.Error("Non-existent user should return nil or error")
	}
}

func TestLoadAdminUser_LegacyIsAdminFallback(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	// Create a user with is_admin=TRUE but no role in user_roles
	server.db.Exec(`INSERT INTO users (id, domain_id, username, password_hash, is_admin) VALUES (100, 1, 'legacyadmin', 'hash', TRUE)`)

	user, err := server.loadAdminUser(context.Background(), 100)
	if err != nil {
		t.Fatalf("loadAdminUser failed: %v", err)
	}
	if user == nil {
		t.Fatal("Legacy is_admin user should be loaded")
	}
	if user.Role != "super_admin" {
		t.Errorf("Legacy admin role = %q, want super_admin", user.Role)
	}
	// Should have fallback permissions
	if len(user.Permissions) == 0 {
		t.Error("Legacy admin should have fallback permissions")
	}
}

// =============================================================================
// requirePermission middleware tests
// =============================================================================

func TestRequirePermission_Allowed(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	called := false
	handler := server.requirePermission("users.read", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/admin/users", nil)
	ctx := context.WithValue(req.Context(), adminUserContextKey, &AdminUser{
		ID:          1,
		Role:        "super_admin",
		Permissions: []string{"users.read", "users.create"},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if !called {
		t.Error("Handler should have been called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want 200", rec.Code)
	}
}

func TestRequirePermission_Denied(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	called := false
	handler := server.requirePermission("settings.manage", func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/admin/settings", nil)
	ctx := context.WithValue(req.Context(), adminUserContextKey, &AdminUser{
		ID:          3,
		Role:        "support",
		Permissions: []string{"users.read", "logs.view"},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if called {
		t.Error("Handler should NOT have been called when permission denied")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want 403", rec.Code)
	}
}

func TestRequirePermission_NoUser(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	handler := server.requirePermission("users.read", func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not be called")
	})

	req := httptest.NewRequest("GET", "/admin/users", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want 403", rec.Code)
	}
}

// =============================================================================
// withAuth integration tests
// =============================================================================

func TestWithAuth_LoadsAdminUserIntoContext(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	token := createRBACSession(t, server.db, 1) // superadmin

	var capturedUser *AdminUser
	handler := server.withAuth(func(w http.ResponseWriter, r *http.Request) {
		capturedUser = GetAdminUser(r)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/admin/", nil)
	req.Host = "mail.example.com"
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: token})
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d, want 200", rec.Code)
	}
	if capturedUser == nil {
		t.Fatal("AdminUser should be in context after withAuth")
	}
	if capturedUser.Role != "super_admin" {
		t.Errorf("Role = %q, want super_admin", capturedUser.Role)
	}
	if capturedUser.Email != "superadmin@example.com" {
		t.Errorf("Email = %q, want superadmin@example.com", capturedUser.Email)
	}
}

func TestWithAuth_RejectsRegularUser(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	token := createRBACSession(t, server.db, 5) // regularuser (not admin)

	handler := server.withAuth(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for non-admin user")
	})

	req := httptest.NewRequest("GET", "/admin/", nil)
	req.Host = "mail.example.com"
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: token})
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Status = %d, want 303 redirect", rec.Code)
	}
}

func TestWithAuth_RejectsNoCookie(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	handler := server.withAuth(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without session")
	})

	req := httptest.NewRequest("GET", "/admin/", nil)
	req.Host = "mail.example.com"
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Status = %d, want 303 redirect", rec.Code)
	}
}

// =============================================================================
// assignUserRole tests
// =============================================================================

func TestAssignUserRole_SuperAdmin(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Assign super_admin role to regularuser (5)
	server.assignUserRole(ctx, 5, "super_admin", 0)

	// Verify assignment
	var count int
	server.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_roles ur JOIN roles r ON ur.role_id = r.id
		 WHERE ur.user_id = 5 AND r.name = 'super_admin'`).Scan(&count)
	if count != 1 {
		t.Errorf("Expected 1 role assignment, got %d", count)
	}
}

func TestAssignUserRole_DomainAdmin(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Assign domain_admin role to regularuser (5) scoped to domain 2
	server.assignUserRole(ctx, 5, "domain_admin", 2)

	// Verify assignment with domain scope
	var domainID sql.NullInt64
	err := server.db.QueryRowContext(ctx,
		`SELECT ur.domain_id FROM user_roles ur JOIN roles r ON ur.role_id = r.id
		 WHERE ur.user_id = 5 AND r.name = 'domain_admin'`).Scan(&domainID)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if !domainID.Valid || domainID.Int64 != 2 {
		t.Errorf("Expected domain_id=2, got %v", domainID)
	}
}

func TestAssignUserRole_InvalidRole(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Assign invalid role - should silently fail
	server.assignUserRole(ctx, 5, "nonexistent_role", 0)

	var count int
	server.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_roles WHERE user_id = 5`).Scan(&count)
	if count != 0 {
		t.Errorf("Expected no role assignments for invalid role, got %d", count)
	}
}

// =============================================================================
// Handler integration tests with roles
// =============================================================================

func TestHandleUserEdit_RoleDropdown_SuperAdminOnly(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	// Create a template that renders role info
	tmpl, _ := template.New("user_edit.html").Parse(
		`Role:{{.CurrentRole}} AdminRole:{{if .AdminUser}}{{.AdminUser.Role}}{{end}}`)
	server.templates["user_edit.html"] = tmpl

	t.Run("super_admin_sees_role_dropdown", func(t *testing.T) {
		token := createRBACSession(t, server.db, 1) // superadmin

		req := httptest.NewRequest("GET", "/admin/users/edit/4", nil)
		req.Host = "mail.example.com"
		req.AddCookie(&http.Cookie{Name: "admin_session", Value: token})

		// Set admin user in context
		adminUser, _ := server.loadAdminUser(context.Background(), 1)
		ctx := context.WithValue(req.Context(), adminUserContextKey, adminUser)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		server.handleUserEdit(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "AdminRole:super_admin") {
			t.Errorf("Super admin should see admin role info, got: %s", body)
		}
	})
}

func TestHandleUserEdit_DomainAdmin_AccessDenied(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	tmpl, _ := template.New("user_edit.html").Parse(`ok`)
	server.templates["user_edit.html"] = tmpl

	// domain_admin (user 2) scoped to domain 1 tries to edit user 4 (domain 2)
	token := createRBACSession(t, server.db, 2)

	req := httptest.NewRequest("GET", "/admin/users/edit/4", nil)
	req.Host = "mail.example.com"
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: token})

	adminUser, _ := server.loadAdminUser(context.Background(), 2)
	ctx := context.WithValue(req.Context(), adminUserContextKey, adminUser)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	server.handleUserEdit(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Domain admin editing user outside scope: status = %d, want 403", rec.Code)
	}
}

func TestHandleUserEdit_PostUpdateRole(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	token := createRBACSession(t, server.db, 1) // superadmin

	// POST to change role
	form := url.Values{}
	form.Set("csrf_token", "unused")
	form.Set("role", "support")

	req := httptest.NewRequest("POST", "/admin/users/edit/5", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "mail.example.com"
	req.AddCookie(&http.Cookie{Name: "admin_session", Value: token})

	adminUser, _ := server.loadAdminUser(context.Background(), 1)
	ctx := context.WithValue(req.Context(), adminUserContextKey, adminUser)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	server.handleUserEdit(rec, req)

	// Should redirect on success
	if rec.Code != http.StatusSeeOther {
		t.Errorf("Status = %d, want 303 redirect", rec.Code)
	}

	// Verify role was assigned
	var roleName string
	err := server.db.QueryRowContext(context.Background(),
		`SELECT r.name FROM user_roles ur JOIN roles r ON ur.role_id = r.id WHERE ur.user_id = 5`).Scan(&roleName)
	if err != nil {
		t.Fatalf("Failed to query role: %v", err)
	}
	if roleName != "support" {
		t.Errorf("Role = %q, want support", roleName)
	}
}

// =============================================================================
// Role permission boundary tests
// =============================================================================

func TestRolePermissionBoundaries(t *testing.T) {
	server, cleanup := setupRBACTestServer(t)
	defer cleanup()

	ctx := context.Background()

	tests := []struct {
		name       string
		userID     int64
		wantPerms  []string
		denyPerms  []string
	}{
		{
			name:      "super_admin_has_all",
			userID:    1,
			wantPerms: []string{"users.create", "users.delete", "domains.create", "domains.delete", "settings.manage", "queue.manage"},
			denyPerms: []string{},
		},
		{
			name:      "domain_admin_scoped",
			userID:    2,
			wantPerms: []string{"users.create", "users.read", "aliases.manage"},
			denyPerms: []string{"domains.create", "domains.delete", "settings.manage", "queue.manage", "audit.view"},
		},
		{
			name:      "support_readonly",
			userID:    3,
			wantPerms: []string{"users.read", "users.password", "logs.view", "audit.view"},
			denyPerms: []string{"users.create", "users.delete", "domains.create", "aliases.manage", "settings.manage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := server.loadAdminUser(ctx, tt.userID)
			if err != nil || user == nil {
				t.Fatalf("Failed to load user %d: %v", tt.userID, err)
			}

			for _, perm := range tt.wantPerms {
				if !user.HasPermission(perm) {
					t.Errorf("User %d should have %q", tt.userID, perm)
				}
			}
			for _, perm := range tt.denyPerms {
				if user.HasPermission(perm) {
					t.Errorf("User %d should NOT have %q", tt.userID, perm)
				}
			}
		})
	}
}
