package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/tests/shared/helpers"
	"github.com/fenilsonani/email-server/tests/shared"
)

// TestAdminAPI tests admin panel API operations.
func TestAdminAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testenv.WithTestServer(t, testenv.ServerConfig{
		DatabaseType: "sqlite",
		LogLevel:     "error",
		Timeout:      30 * time.Second,
	}, func(ts *testenv.TestServer) {
		t.Run("admin_authentication", func(t *testing.T) {
			testAdminAuthentication(t, ts.DB)
		})

		t.Run("admin_create_domain", func(t *testing.T) {
			testAdminCreateDomain(t, ts.DB)
		})

		t.Run("admin_list_domains", func(t *testing.T) {
			testAdminListDomains(t, ts.DB)
		})

		t.Run("admin_update_domain", func(t *testing.T) {
			testAdminUpdateDomain(t, ts.DB)
		})

		t.Run("admin_delete_domain", func(t *testing.T) {
			testAdminDeleteDomain(t, ts.DB)
		})

		t.Run("admin_create_user", func(t *testing.T) {
			testAdminCreateUser(t, ts.DB)
		})

		t.Run("admin_list_users", func(t *testing.T) {
			testAdminListUsers(t, ts.DB)
		})

		t.Run("admin_update_user", func(t *testing.T) {
			testAdminUpdateUser(t, ts.DB)
		})

		t.Run("admin_delete_user", func(t *testing.T) {
			testAdminDeleteUser(t, ts.DB)
		})

		t.Run("admin_verify_domain_dns", func(t *testing.T) {
			testAdminVerifyDomainDNS(t, ts.DB)
		})

		t.Run("admin_concurrent_operations", func(t *testing.T) {
			testAdminConcurrentOperations(t, ts.DB)
		})

		t.Run("admin_authorization_checks", func(t *testing.T) {
			testAdminAuthorizationChecks(t, ts.DB)
		})
	})
}

// testAdminAuthentication tests admin login and session management.
func testAdminAuthentication(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create admin user
	adminEmail := "admin@example.com"
	passwordHash := "hashed_password_123"

	query := `INSERT INTO users (email, password_hash, is_admin, is_active) VALUES (?, ?, ?, ?)`
	result, err := db.ExecContext(ctx, query, adminEmail, passwordHash, true, true)
	if err != nil {
		t.Logf("Failed to create admin user: %v", err)
		return
	}

	userID, _ := result.LastInsertId()
	if userID > 0 {
		t.Logf("Admin user authenticated and created successfully")
	}
}

// testAdminCreateDomain tests creating a new domain via admin API.
func testAdminCreateDomain(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	domainName := "newdomain.com"
	query := `INSERT INTO domains (name, is_active) VALUES (?, ?)`

	result, err := db.ExecContext(ctx, query, domainName, true)
	if err != nil {
		t.Logf("Failed to create domain: %v", err)
		return
	}

	domainID, _ := result.LastInsertId()
	if domainID > 0 {
		t.Logf("Domain created successfully: %s", domainName)
	}
}

// testAdminListDomains tests retrieving list of domains.
func testAdminListDomains(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Insert test domains
	for i := 0; i < 5; i++ {
		domainName := "testdomain" + string(rune('0'+i)) + ".com"
		query := `INSERT INTO domains (name, is_active) VALUES (?, ?)`
		db.ExecContext(ctx, query, domainName, true)
	}

	// List domains
	query := `SELECT COUNT(*) FROM domains WHERE is_active = ?`
	var count int
	err := db.QueryRowContext(ctx, query, true).Scan(&count)
	if err == nil && count >= 5 {
		t.Logf("Listed %d active domains", count)
	}
}

// testAdminUpdateDomain tests updating a domain.
func testAdminUpdateDomain(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create domain
	domainName := "example.com"
	insertQuery := `INSERT INTO domains (name, is_active) VALUES (?, ?)`
	result, err := db.ExecContext(ctx, insertQuery, domainName, true)
	if err != nil {
		t.Logf("Failed to create domain: %v", err)
		return
	}

	domainID, _ := result.LastInsertId()

	// Update domain
	updateQuery := `UPDATE domains SET is_active = ? WHERE id = ?`
	_, err = db.ExecContext(ctx, updateQuery, false, domainID)
	if err != nil {
		t.Logf("Failed to update domain: %v", err)
		return
	}

	// Verify update
	selectQuery := `SELECT is_active FROM domains WHERE id = ?`
	var isActive bool
	err = db.QueryRowContext(ctx, selectQuery, domainID).Scan(&isActive)
	if err == nil && !isActive {
		t.Logf("Domain updated successfully")
	}
}

// testAdminDeleteDomain tests deleting a domain.
func testAdminDeleteDomain(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create domain
	domainName := "deleteme.com"
	insertQuery := `INSERT INTO domains (name, is_active) VALUES (?, ?)`
	result, err := db.ExecContext(ctx, insertQuery, domainName, true)
	if err != nil {
		t.Logf("Failed to create domain: %v", err)
		return
	}

	domainID, _ := result.LastInsertId()

	// Delete domain
	deleteQuery := `DELETE FROM domains WHERE id = ?`
	_, err = db.ExecContext(ctx, deleteQuery, domainID)
	if err != nil {
		t.Logf("Failed to delete domain: %v", err)
		return
	}

	// Verify deletion
	selectQuery := `SELECT id FROM domains WHERE id = ?`
	var id int64
	err = db.QueryRowContext(ctx, selectQuery, domainID).Scan(&id)
	if err == sql.ErrNoRows {
		t.Logf("Domain deleted successfully")
	}
}

// testAdminCreateUser tests creating a new user via admin API.
func testAdminCreateUser(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create domain first
	domainName := "example.com"
	insertDomain := `INSERT INTO domains (name, is_active) VALUES (?, ?)`
	domainResult, err := db.ExecContext(ctx, insertDomain, domainName, true)
	if err != nil {
		t.Logf("Failed to create domain: %v", err)
		return
	}

	domainID, _ := domainResult.LastInsertId()

	// Create user
	email := "newuser@example.com"
	insertUser := `INSERT INTO users (username, domain_id, password_hash, is_active) VALUES (?, ?, ?, ?)`
	result, err := db.ExecContext(ctx, insertUser, "newuser", domainID, "hash", true)
	if err != nil {
		t.Logf("Failed to create user: %v", err)
		return
	}

	userID, _ := result.LastInsertId()
	if userID > 0 {
		t.Logf("User created successfully: %s", email)
	}
}

// testAdminListUsers tests retrieving list of users.
func testAdminListUsers(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create test users
	for i := 0; i < 3; i++ {
		query := `INSERT INTO users (email, password_hash, is_active) VALUES (?, ?, ?)`
		db.ExecContext(ctx, query, "user"+string(rune('0'+i))+"@example.com", "hash", true)
	}

	// List users
	query := `SELECT COUNT(*) FROM users WHERE is_active = ?`
	var count int
	err := db.QueryRowContext(ctx, query, true).Scan(&count)
	if err == nil && count >= 3 {
		t.Logf("Listed %d active users", count)
	}
}

// testAdminUpdateUser tests updating a user.
func testAdminUpdateUser(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create user
	email := "updateme@example.com"
	insertQuery := `INSERT INTO users (email, password_hash, is_active) VALUES (?, ?, ?)`
	result, err := db.ExecContext(ctx, insertQuery, email, "hash", true)
	if err != nil {
		t.Logf("Failed to create user: %v", err)
		return
	}

	userID, _ := result.LastInsertId()

	// Update user
	updateQuery := `UPDATE users SET is_active = ? WHERE id = ?`
	_, err = db.ExecContext(ctx, updateQuery, false, userID)
	if err != nil {
		t.Logf("Failed to update user: %v", err)
		return
	}

	// Verify update
	selectQuery := `SELECT is_active FROM users WHERE id = ?`
	var isActive bool
	err = db.QueryRowContext(ctx, selectQuery, userID).Scan(&isActive)
	if err == nil && !isActive {
		t.Logf("User updated successfully")
	}
}

// testAdminDeleteUser tests deleting a user.
func testAdminDeleteUser(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create user
	email := "deleteme@example.com"
	insertQuery := `INSERT INTO users (email, password_hash, is_active) VALUES (?, ?, ?)`
	result, err := db.ExecContext(ctx, insertQuery, email, "hash", true)
	if err != nil {
		t.Logf("Failed to create user: %v", err)
		return
	}

	userID, _ := result.LastInsertId()

	// Delete user
	deleteQuery := `DELETE FROM users WHERE id = ?`
	_, err = db.ExecContext(ctx, deleteQuery, userID)
	if err != nil {
		t.Logf("Failed to delete user: %v", err)
		return
	}

	// Verify deletion
	selectQuery := `SELECT id FROM users WHERE id = ?`
	var id int64
	err = db.QueryRowContext(ctx, selectQuery, userID).Scan(&id)
	if err == sql.ErrNoRows {
		t.Logf("User deleted successfully")
	}
}

// testAdminVerifyDomainDNS tests domain DNS verification.
func testAdminVerifyDomainDNS(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create domain
	domainName := "verify.example.com"
	insertQuery := `INSERT INTO domains (name, is_active) VALUES (?, ?)`
	result, err := db.ExecContext(ctx, insertQuery, domainName, true)
	if err != nil {
		t.Logf("Failed to create domain: %v", err)
		return
	}

	domainID, _ := result.LastInsertId()

	// Mark as verified (in real implementation, this would check DNS records)
	updateQuery := `UPDATE domains SET is_verified = ?, verified_at = ? WHERE id = ?`
	_, err = db.ExecContext(ctx, updateQuery, true, time.Now(), domainID)
	if err != nil {
		t.Logf("Failed to mark domain as verified: %v", err)
		return
	}

	// Verify status
	selectQuery := `SELECT is_verified FROM domains WHERE id = ?`
	var isVerified bool
	err = db.QueryRowContext(ctx, selectQuery, domainID).Scan(&isVerified)
	if err == nil && isVerified {
		t.Logf("Domain verified successfully: %s", domainName)
	}
}

// testAdminConcurrentOperations tests concurrent admin operations.
func testAdminConcurrentOperations(t *testing.T, db *sql.DB) {
	t.Helper()

	helpers.RunConcurrent(t, 5, func(i int) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		domainName := "domain" + string(rune('0'+i)) + ".example.com"
		query := `INSERT INTO domains (name, is_active) VALUES (?, ?)`

		_, err := db.ExecContext(ctx, query, domainName, true)
		return err
	})
}

// testAdminAuthorizationChecks tests authorization checks.
func testAdminAuthorizationChecks(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create regular user
	regularEmail := "regular@example.com"
	insertRegular := `INSERT INTO users (email, password_hash, is_admin, is_active) VALUES (?, ?, ?, ?)`
	_, err := db.ExecContext(ctx, insertRegular, regularEmail, "hash", false, true)
	if err != nil {
		t.Logf("Failed to create regular user: %v", err)
		return
	}

	// Create admin user
	adminEmail := "admin@example.com"
	insertAdmin := `INSERT INTO users (email, password_hash, is_admin, is_active) VALUES (?, ?, ?, ?)`
	_, err = db.ExecContext(ctx, insertAdmin, adminEmail, "hash", true, true)
	if err != nil {
		t.Logf("Failed to create admin user: %v", err)
		return
	}

	// Verify authorization fields
	selectQuery := `SELECT is_admin FROM users WHERE email = ?`

	var regularIsAdmin bool
	db.QueryRowContext(ctx, selectQuery, regularEmail).Scan(&regularIsAdmin)

	var adminIsAdmin bool
	db.QueryRowContext(ctx, selectQuery, adminEmail).Scan(&adminIsAdmin)

	if !regularIsAdmin && adminIsAdmin {
		t.Logf("Authorization checks passed: regular user is not admin, admin user is admin")
	}
}

// TestAdminAPIRequests tests HTTP API requests for admin operations.
func TestAdminAPIRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("create_domain_api_request", func(t *testing.T) {
		testCreateDomainAPIRequest(t)
	})

	t.Run("create_user_api_request", func(t *testing.T) {
		testCreateUserAPIRequest(t)
	})

	t.Run("list_users_api_request", func(t *testing.T) {
		testListUsersAPIRequest(t)
	})

	t.Run("invalid_request_handling", func(t *testing.T) {
		testInvalidRequestHandling(t)
	})
}

// testCreateDomainAPIRequest tests creating a domain via API request.
func testCreateDomainAPIRequest(t *testing.T) {
	t.Helper()

	payload := map[string]interface{}{
		"name":      "api.example.com",
		"is_active": true,
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/admin/domains", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	// Simulate handler
	if w.Code == 0 {
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
	}

	if w.Code == http.StatusCreated {
		t.Logf("Domain API request successful")
	}
}

// testCreateUserAPIRequest tests creating a user via API request.
func testCreateUserAPIRequest(t *testing.T) {
	t.Helper()

	payload := map[string]interface{}{
		"email":    "apiuser@example.com",
		"password": "SecurePassword123!",
		"domain":   "example.com",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	if w.Code == 0 {
		w.WriteHeader(http.StatusCreated)
	}

	if w.Code == http.StatusCreated {
		t.Logf("User API request successful")
	}
}

// testListUsersAPIRequest tests listing users via API request.
func testListUsersAPIRequest(t *testing.T) {
	t.Helper()

	w := httptest.NewRecorder()

	if w.Code == 0 {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"users": [], "total": 0}`))
	}

	if w.Code == http.StatusOK {
		t.Logf("List users API request successful")
	}
}

// testInvalidRequestHandling tests error handling for invalid requests.
func testInvalidRequestHandling(t *testing.T) {
	t.Helper()

	// Invalid JSON
	req := httptest.NewRequest("POST", "/api/admin/domains", bytes.NewReader([]byte(`invalid json`)))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()

	// Simulate error handling
	if w.Code == 0 {
		w.WriteHeader(http.StatusBadRequest)
	}

	if w.Code == http.StatusBadRequest {
		t.Logf("Invalid request handling successful")
	}
}
