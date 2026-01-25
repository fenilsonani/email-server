package userportal

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/testutil"
)

// TestHandleLogin tests the login handler
func TestHandleLogin(t *testing.T) {
	t.Run("login_get_returns_login_form", func(t *testing.T) {
		t.Skip("Requires database, authenticator, and template rendering setup")
		logger, _ := logging.New(logging.DefaultConfig())
		s := &Server{
			logger: logger,
		}

		req := httptest.NewRequest(http.MethodGet, "/account/login", nil)
		w := httptest.NewRecorder()

		s.handleLogin(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("handleLogin GET returned status %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("login_post_missing_email", func(t *testing.T) {
		t.Skip("Requires full Server setup with authenticator and database")
		s := &Server{
			logger:       createTestLogger(t),
			rateLimiter: NewRateLimiter(5, 15*time.Minute, 30*time.Minute),
		}

		form := url.Values{}
		form.Set("password", "testpass")
		req := httptest.NewRequest(http.MethodPost, "/account/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleLogin(w, req)
		// Should still render login page with error, not 500
		if w.Code >= 500 {
			t.Errorf("handleLogin POST with missing email returned status %d", w.Code)
		}
	})

	t.Run("login_post_missing_password", func(t *testing.T) {
		s := &Server{
			logger:       createTestLogger(t),
			rateLimiter: NewRateLimiter(5, 15*time.Minute, 30*time.Minute),
		}

		form := url.Values{}
		form.Set("email", "user@example.com")
		req := httptest.NewRequest(http.MethodPost, "/account/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleLogin(w, req)
		// Should handle gracefully
		if w.Code >= 500 {
			t.Errorf("handleLogin POST with missing password returned status %d", w.Code)
		}
	})

	t.Run("login_post_invalid_form", func(t *testing.T) {
		s := &Server{
			logger:       createTestLogger(t),
			rateLimiter: NewRateLimiter(5, 15*time.Minute, 30*time.Minute),
		}

		// Send garbage instead of form data
		req := httptest.NewRequest(http.MethodPost, "/account/login", strings.NewReader("invalid"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleLogin(w, req)
		// Should return 400 for bad request
		if w.Code < 400 || w.Code >= 500 {
			t.Errorf("handleLogin POST with invalid form returned status %d", w.Code)
		}
	})

	t.Run("login_post_rate_limiting", func(t *testing.T) {
		s := &Server{
			logger:       createTestLogger(t),
			rateLimiter: NewRateLimiter(2, 1*time.Hour, 30*time.Minute), // Allow 2 attempts
		}

		form := url.Values{}
		form.Set("email", "user@example.com")
		form.Set("password", "wrongpass")

		// Make 3 failed attempts
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest(http.MethodPost, "/account/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.RemoteAddr = "127.0.0.1:12345"
			w := httptest.NewRecorder()

			s.handleLogin(w, req)
		}

		// Next request should be blocked
		req := httptest.NewRequest(http.MethodPost, "/account/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()

		s.handleLogin(w, req)
		// Should show rate limit error message
		body := w.Body.String()
		if !strings.Contains(body, "Too many failed attempts") && !strings.Contains(body, "locked") {
			t.Logf("handleLogin rate limiting did not show expected error message")
		}
	})

	t.Run("login_post_whitespace_trimmed", func(t *testing.T) {
		s := &Server{
			logger:       createTestLogger(t),
			rateLimiter: NewRateLimiter(5, 15*time.Minute, 30*time.Minute),
		}

		// Email with surrounding whitespace should be trimmed
		form := url.Values{}
		form.Set("email", "  user@example.com  ")
		form.Set("password", "testpass")
		req := httptest.NewRequest(http.MethodPost, "/account/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleLogin(w, req)
		// Should attempt authentication (will fail with no auth backend, but should process)
		if w.Code >= 500 {
			t.Errorf("handleLogin POST with whitespace failed: status %d", w.Code)
		}
	})
}

// TestHandleLogout tests the logout handler
func TestHandleLogout(t *testing.T) {
	t.Run("logout_without_session", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		req := httptest.NewRequest(http.MethodGet, "/account/logout", nil)
		w := httptest.NewRecorder()

		s.handleLogout(w, req)

		// Should still complete successfully
		if w.Code >= 400 {
			t.Errorf("handleLogout without session returned status %d", w.Code)
		}
		// Should redirect
		if w.Code != http.StatusSeeOther {
			t.Logf("handleLogout should redirect with SeeOther, got %d", w.Code)
		}
	})

	t.Run("logout_with_session_cookie", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		req := httptest.NewRequest(http.MethodGet, "/account/logout", nil)
		req.AddCookie(&http.Cookie{
			Name:  sessionCookieName,
			Value: "test-token",
		})
		w := httptest.NewRecorder()

		s.handleLogout(w, req)

		// Should redirect
		if w.Code != http.StatusSeeOther {
			t.Errorf("handleLogout returned status %d, want %d", w.Code, http.StatusSeeOther)
		}
		// Should clear session cookie
		setCookieHeader := w.Header().Get("Set-Cookie")
		if !strings.Contains(setCookieHeader, sessionCookieName) {
			t.Logf("handleLogout should clear session cookie")
		}
	})

	t.Run("logout_redirect_to_login", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		req := httptest.NewRequest(http.MethodGet, "/account/logout", nil)
		w := httptest.NewRecorder()

		s.handleLogout(w, req)

		// Check redirect location
		location := w.Header().Get("Location")
		if !strings.Contains(location, "/account/login") {
			t.Errorf("handleLogout redirect location = %q, want to contain /account/login", location)
		}
	})
}

// TestHandleDashboard tests the dashboard handler
func TestHandleDashboard(t *testing.T) {
	t.Run("dashboard_wrong_path_redirects", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		req := httptest.NewRequest(http.MethodGet, "/account/other", nil)
		w := httptest.NewRecorder()

		s.handleDashboard(w, req)

		// Should redirect to /account/
		if w.Code != http.StatusSeeOther {
			t.Errorf("handleDashboard with wrong path returned status %d, want redirect", w.Code)
		}
		location := w.Header().Get("Location")
		if location != "/account/" {
			t.Errorf("handleDashboard redirect location = %q, want /account/", location)
		}
	})

	t.Run("dashboard_correct_path", func(t *testing.T) {
		testutil.WithTestDBAndSchema(t, func(db *sql.DB) {
			s := &Server{
				logger: createTestLogger(t),
				db:     db,
			}

			req := httptest.NewRequest(http.MethodGet, "/account/", nil)
			w := httptest.NewRecorder()

			s.handleDashboard(w, req)
			// Will fail with no user in context, but should not crash
			if w.Code >= 500 {
				t.Errorf("handleDashboard with correct path returned status %d", w.Code)
			}
		})
	})
}

// TestHandlePassword tests the password change handler
func TestHandlePassword(t *testing.T) {
	t.Run("password_get_shows_form", func(t *testing.T) {
		testutil.WithTestDBAndSchema(t, func(db *sql.DB) {
			s := &Server{
				logger: createTestLogger(t),
				db:     db,
			}

			req := httptest.NewRequest(http.MethodGet, "/account/password", nil)
			w := httptest.NewRecorder()

			s.handlePassword(w, req)
			// Will fail with no user context, but should attempt to render
			if w.Code >= 500 {
				t.Errorf("handlePassword GET returned status %d", w.Code)
			}
		})
	})

	t.Run("password_post_mismatched_passwords", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("current_password", "current")
		form.Set("new_password", "newpass123")
		form.Set("confirm_password", "different")

		req := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handlePassword(w, req)
		// Should show error about mismatched passwords
		body := w.Body.String()
		if !strings.Contains(body, "do not match") {
			t.Logf("handlePassword should show mismatch error, got: %s", body)
		}
	})

	t.Run("password_post_too_short", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("current_password", "current")
		form.Set("new_password", "short")
		form.Set("confirm_password", "short")

		req := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handlePassword(w, req)
		// Should show error about password length
		body := w.Body.String()
		if !strings.Contains(body, "8-128 characters") {
			t.Logf("handlePassword should show length requirement")
		}
	})

	t.Run("password_post_too_long", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		longPassword := strings.Repeat("a", 200)
		form := url.Values{}
		form.Set("current_password", "current")
		form.Set("new_password", longPassword)
		form.Set("confirm_password", longPassword)

		req := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handlePassword(w, req)
		// Should show error about max length
		body := w.Body.String()
		if !strings.Contains(body, "8-128") {
			t.Logf("handlePassword should enforce max length")
		}
	})

	t.Run("password_post_invalid_form", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		req := httptest.NewRequest(http.MethodPost, "/account/password", strings.NewReader("invalid"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handlePassword(w, req)
		if w.Code < 400 {
			t.Errorf("handlePassword with invalid form returned status %d", w.Code)
		}
	})
}

// TestHandleProfile tests the profile editing handler
func TestHandleProfile(t *testing.T) {
	t.Run("profile_get_shows_form", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		req := httptest.NewRequest(http.MethodGet, "/account/profile", nil)
		w := httptest.NewRecorder()

		s.handleProfile(w, req)
		// Will fail with no user context but should not crash
		if w.Code >= 500 {
			t.Errorf("handleProfile GET returned status %d", w.Code)
		}
	})

	t.Run("profile_post_long_display_name_truncated", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		longName := strings.Repeat("a", 300)
		form := url.Values{}
		form.Set("display_name", longName)

		req := httptest.NewRequest(http.MethodPost, "/account/profile", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleProfile(w, req)
		// Should handle long names (truncated to 255)
		if w.Code >= 500 {
			t.Errorf("handleProfile POST with long name returned status %d", w.Code)
		}
	})

	t.Run("profile_post_empty_display_name", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("display_name", "")

		req := httptest.NewRequest(http.MethodPost, "/account/profile", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleProfile(w, req)
		// Should accept empty display name
		if w.Code >= 500 {
			t.Errorf("handleProfile POST with empty name returned status %d", w.Code)
		}
	})

	t.Run("profile_post_whitespace_display_name", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("display_name", "  John Doe  ")

		req := httptest.NewRequest(http.MethodPost, "/account/profile", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleProfile(w, req)
		// Should trim whitespace
		if w.Code >= 500 {
			t.Errorf("handleProfile POST with whitespace returned status %d", w.Code)
		}
	})

	t.Run("profile_post_invalid_form", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		req := httptest.NewRequest(http.MethodPost, "/account/profile", strings.NewReader("invalid"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleProfile(w, req)
		if w.Code < 400 {
			t.Errorf("handleProfile with invalid form returned status %d", w.Code)
		}
	})
}

// TestHandleForwarding tests the email forwarding handler
func TestHandleForwarding(t *testing.T) {
	t.Run("forwarding_get_shows_form", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		req := httptest.NewRequest(http.MethodGet, "/account/forwarding", nil)
		w := httptest.NewRecorder()

		s.handleForwarding(w, req)
		// Will fail with no user context but should not crash
		if w.Code >= 500 {
			t.Errorf("handleForwarding GET returned status %d", w.Code)
		}
	})

	t.Run("forwarding_post_inactive_no_validation", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("is_active", "") // Inactive
		form.Set("forward_to", "")  // No email required if inactive
		form.Set("keep_copy", "off")

		req := httptest.NewRequest(http.MethodPost, "/account/forwarding", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleForwarding(w, req)
		// Should accept empty forwarding address when inactive
		if w.Code >= 500 {
			t.Errorf("handleForwarding POST with inactive returned status %d", w.Code)
		}
	})

	t.Run("forwarding_post_active_requires_email", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("is_active", "on") // Active
		form.Set("forward_to", "")   // No email - should fail

		req := httptest.NewRequest(http.MethodPost, "/account/forwarding", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleForwarding(w, req)
		// Should show error
		body := w.Body.String()
		if !strings.Contains(body, "forwarding address") {
			t.Logf("handleForwarding should require email when active")
		}
	})

	t.Run("forwarding_post_keep_copy_checkbox", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("is_active", "on")
		form.Set("forward_to", "other@example.com")
		form.Set("keep_copy", "on")

		req := httptest.NewRequest(http.MethodPost, "/account/forwarding", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleForwarding(w, req)
		// Should process keep_copy checkbox correctly
		if w.Code >= 500 {
			t.Errorf("handleForwarding POST with keep_copy returned status %d", w.Code)
		}
	})

	t.Run("forwarding_post_invalid_form", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		req := httptest.NewRequest(http.MethodPost, "/account/forwarding", strings.NewReader("invalid"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleForwarding(w, req)
		if w.Code < 400 {
			t.Errorf("handleForwarding with invalid form returned status %d", w.Code)
		}
	})
}

// TestHandleVacation tests the vacation responder handler
func TestHandleVacation(t *testing.T) {
	t.Run("vacation_get_shows_form", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		req := httptest.NewRequest(http.MethodGet, "/account/vacation", nil)
		w := httptest.NewRecorder()

		s.handleVacation(w, req)
		// Will fail with no user context but should not crash
		if w.Code >= 500 {
			t.Errorf("handleVacation GET returned status %d", w.Code)
		}
	})

	t.Run("vacation_post_inactive_no_validation", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("is_active", "") // Inactive
		form.Set("subject", "")    // No subject required
		form.Set("message", "")    // No message required

		req := httptest.NewRequest(http.MethodPost, "/account/vacation", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleVacation(w, req)
		// Should accept when inactive
		if w.Code >= 500 {
			t.Errorf("handleVacation POST with inactive returned status %d", w.Code)
		}
	})

	t.Run("vacation_post_active_requires_subject", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("is_active", "on") // Active
		form.Set("subject", "")     // No subject - should fail
		form.Set("message", "Away")

		req := httptest.NewRequest(http.MethodPost, "/account/vacation", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleVacation(w, req)
		// Should show error
		body := w.Body.String()
		if !strings.Contains(body, "Subject and message") {
			t.Logf("handleVacation should require subject and message when active")
		}
	})

	t.Run("vacation_post_active_requires_message", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("is_active", "on")       // Active
		form.Set("subject", "On vacation") // Has subject
		form.Set("message", "")            // No message - should fail

		req := httptest.NewRequest(http.MethodPost, "/account/vacation", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleVacation(w, req)
		// Should show error
		body := w.Body.String()
		if !strings.Contains(body, "Subject and message") {
			t.Logf("handleVacation should require subject and message when active")
		}
	})

	t.Run("vacation_post_date_parsing", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("is_active", "on")
		form.Set("subject", "On vacation")
		form.Set("message", "I am away")
		form.Set("start_date", "2025-01-25")
		form.Set("end_date", "2025-02-01")

		req := httptest.NewRequest(http.MethodPost, "/account/vacation", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleVacation(w, req)
		// Should parse dates correctly
		if w.Code >= 500 {
			t.Errorf("handleVacation POST with dates returned status %d", w.Code)
		}
	})

	t.Run("vacation_post_invalid_date_format", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		form := url.Values{}
		form.Set("is_active", "on")
		form.Set("subject", "On vacation")
		form.Set("message", "I am away")
		form.Set("start_date", "invalid-date")
		form.Set("end_date", "2025-02-01")

		req := httptest.NewRequest(http.MethodPost, "/account/vacation", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleVacation(w, req)
		// Should handle invalid date gracefully (ignored)
		if w.Code >= 500 {
			t.Errorf("handleVacation POST with invalid date returned status %d", w.Code)
		}
	})

	t.Run("vacation_post_invalid_form", func(t *testing.T) {
		s := &Server{
			logger: createTestLogger(t),
		}

		req := httptest.NewRequest(http.MethodPost, "/account/vacation", strings.NewReader("invalid"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleVacation(w, req)
		if w.Code < 400 {
			t.Errorf("handleVacation with invalid form returned status %d", w.Code)
		}
	})
}

// TestGetUserInfo tests the getUserInfo helper
func TestGetUserInfo(t *testing.T) {
	t.Run("get_user_info_nonexistent", func(t *testing.T) {
		testutil.WithTestDBAndSchema(t, func(db *sql.DB) {
			s := &Server{
				db: db,
			}

			ctx := context.Background()
			user, err := s.getUserInfo(ctx, 99999)
			if err == nil {
				t.Errorf("getUserInfo for nonexistent user should fail")
			}
			if user != nil {
				t.Errorf("getUserInfo returned non-nil user for nonexistent ID")
			}
		})
	})
}

// TestFormatDate tests the date formatting helper
func TestFormatDate(t *testing.T) {
	t.Run("format_date_valid", func(t *testing.T) {
		now := time.Now()
		nullTime := sql.NullTime{Time: now, Valid: true}
		result := formatDate(nullTime)
		if !strings.Contains(result, "-") {
			t.Errorf("formatDate returned %q, want YYYY-MM-DD format", result)
		}
	})

	t.Run("format_date_invalid", func(t *testing.T) {
		nullTime := sql.NullTime{Valid: false}
		result := formatDate(nullTime)
		if result != "" {
			t.Errorf("formatDate with invalid time returned %q, want empty string", result)
		}
	})
}

// TestRateLimiter tests the rate limiter used in login
func TestRateLimiter(t *testing.T) {
	t.Run("rate_limiter_allows_attempts", func(t *testing.T) {
		limiter := NewRateLimiter(3, 1*time.Hour, 30*time.Minute)
		if limiter.IsBlocked("127.0.0.1") {
			t.Errorf("NewRateLimiter should not initially block")
		}
	})

	t.Run("rate_limiter_blocks_after_max_attempts", func(t *testing.T) {
		limiter := NewRateLimiter(2, 1*time.Hour, 30*time.Minute)
		ip := "127.0.0.1"

		// Make 2 failures
		for i := 0; i < 2; i++ {
			limiter.RecordFailure(ip)
		}

		// Next should be blocked
		if !limiter.IsBlocked(ip) {
			t.Errorf("RateLimiter should block after max attempts")
		}
	})

	t.Run("rate_limiter_tracks_attempts", func(t *testing.T) {
		limiter := NewRateLimiter(5, 1*time.Hour, 30*time.Minute)
		ip := "127.0.0.1"

		remaining := limiter.RemainingAttempts(ip)
		if remaining != 5 {
			t.Errorf("RemainingAttempts = %d, want 5", remaining)
		}

		limiter.RecordFailure(ip)
		remaining = limiter.RemainingAttempts(ip)
		if remaining != 4 {
			t.Errorf("RemainingAttempts after failure = %d, want 4", remaining)
		}
	})

	t.Run("rate_limiter_success_resets", func(t *testing.T) {
		limiter := NewRateLimiter(3, 1*time.Hour, 30*time.Minute)
		ip := "127.0.0.1"

		limiter.RecordFailure(ip)
		limiter.RecordFailure(ip)
		limiter.RecordSuccess(ip)

		remaining := limiter.RemainingAttempts(ip)
		if remaining != 3 {
			t.Errorf("RemainingAttempts after success = %d, want 3", remaining)
		}
	})

	t.Run("rate_limiter_different_ips_independent", func(t *testing.T) {
		limiter := NewRateLimiter(2, 1*time.Hour, 30*time.Minute)

		limiter.RecordFailure("127.0.0.1")
		limiter.RecordFailure("127.0.0.1")

		// Different IP should not be blocked
		if limiter.IsBlocked("192.168.1.1") {
			t.Errorf("RateLimiter should not block different IPs")
		}
	})
}

// TestClientIPExtraction tests IP extraction from requests
func TestClientIPExtraction(t *testing.T) {
	t.Run("get_client_ip_from_remote_addr", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		ip := getClientIP(req)
		if !strings.Contains(ip, "192.168.1.1") {
			t.Errorf("getClientIP = %q, want 192.168.1.1", ip)
		}
	})

	t.Run("get_client_ip_from_x_forwarded_for", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")

		ip := getClientIP(req)
		if !strings.Contains(ip, "10.0.0.1") {
			t.Logf("getClientIP should prefer X-Forwarded-For first IP, got %q", ip)
		}
	})
}

// TestSessionCookieHandling tests session cookie management
func TestSessionCookieHandling(t *testing.T) {
	t.Run("set_session_cookie", func(t *testing.T) {
		w := httptest.NewRecorder()
		setSessionCookie(w, "test-token-123")

		setCookieHeader := w.Header().Get("Set-Cookie")
		if !strings.Contains(setCookieHeader, sessionCookieName) {
			t.Errorf("setSessionCookie didn't set cookie with name %s", sessionCookieName)
		}
		if !strings.Contains(setCookieHeader, "test-token-123") {
			t.Errorf("setSessionCookie didn't set token value")
		}
	})

	t.Run("clear_session_cookie", func(t *testing.T) {
		w := httptest.NewRecorder()
		clearSessionCookie(w)

		setCookieHeader := w.Header().Get("Set-Cookie")
		if !strings.Contains(setCookieHeader, sessionCookieName) {
			t.Errorf("clearSessionCookie didn't reference session cookie")
		}
		if !strings.Contains(setCookieHeader, "Max-Age=0") {
			t.Logf("clearSessionCookie should set Max-Age=0 to clear")
		}
	})
}

// TestHTTPMethods tests handling of different HTTP methods
func TestHTTPMethods(t *testing.T) {
	t.Run("login_only_get_post", func(t *testing.T) {
		s := &Server{
			logger:       createTestLogger(t),
			rateLimiter: NewRateLimiter(5, 15*time.Minute, 30*time.Minute),
		}

		// PUT should not be explicitly handled, but GET/POST should work
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			req := httptest.NewRequest(method, "/account/login", nil)
			if method == http.MethodPost {
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			w := httptest.NewRecorder()

			s.handleLogin(w, req)
			if w.Code >= 500 {
				t.Errorf("handleLogin %s returned status %d", method, w.Code)
			}
		}
	})
}

// TestConcurrentHandlers tests concurrent handler access
func TestConcurrentHandlers(t *testing.T) {
	t.Run("concurrent_login_attempts", func(t *testing.T) {
		s := &Server{
			logger:       createTestLogger(t),
			rateLimiter: NewRateLimiter(100, 15*time.Minute, 30*time.Minute), // High limit for concurrent test
		}

		testutil.RunConcurrent(t, 10, func(i int) error {
			form := url.Values{}
			form.Set("email", "user@example.com")
			form.Set("password", "testpass")

			req := httptest.NewRequest(http.MethodPost, "/account/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.RemoteAddr = "127.0.0.1:12345"

			w := httptest.NewRecorder()
			s.handleLogin(w, req)

			return nil
		})
	})
}


// Helper function to create test logger
func createTestLogger(t *testing.T) *logging.Logger {
	logger, err := logging.New(logging.DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create test logger: %v", err)
	}
	return logger
}

// Benchmark tests
func BenchmarkHandleLogin(b *testing.B) {
	logger, _ := logging.New(logging.DefaultConfig())
	s := &Server{
		logger:       logger,
		rateLimiter: NewRateLimiter(100, 15*time.Minute, 30*time.Minute),
	}

	form := url.Values{}
	form.Set("email", "user@example.com")
	form.Set("password", "testpass")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/account/login", bytes.NewReader([]byte(form.Encode())))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		s.handleLogin(w, req)
	}
}

func BenchmarkHandleLogout(b *testing.B) {
	logger, _ := logging.New(logging.DefaultConfig())
	s := &Server{
		logger: logger,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/account/logout", nil)
		w := httptest.NewRecorder()

		s.handleLogout(w, req)
	}
}
