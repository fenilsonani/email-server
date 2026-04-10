package userportal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLoginAttemptError covers the previously buggy
// `string(rune('0'+remaining))` formatting plus the message branches.
func TestLoginAttemptError(t *testing.T) {
	tests := []struct {
		name      string
		remaining int
		blocked   bool
		want      string
	}{
		{
			name:    "blocked overrides everything",
			blocked: true,
			want:    "Too many failed attempts. Account temporarily locked.",
		},
		{
			name:      "blocked overrides positive remaining",
			remaining: 5,
			blocked:   true,
			want:      "Too many failed attempts. Account temporarily locked.",
		},
		{
			name:      "one remaining warns about the next attempt",
			remaining: 1,
			want:      "Invalid credentials. 1 attempt remaining before temporary lockout.",
		},
		{
			name:      "two remaining renders as the integer literal, not a control char",
			remaining: 2,
			want:      "Invalid credentials. 2 attempts remaining before temporary lockout.",
		},
		// Regression for the original `string(rune('0'+remaining))` bug:
		// at remaining=10 the old code emitted ':' (the ASCII char one
		// past '9') and produced "Invalid credentials. : attempts remaining".
		{
			name:      "ten remaining no longer corrupts the message",
			remaining: 10,
			want:      "Invalid email or password",
		},
		{
			name:      "many remaining stays generic",
			remaining: 50,
			want:      "Invalid email or password",
		},
		{
			name:      "zero remaining without blocked stays generic",
			remaining: 0,
			want:      "Invalid email or password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loginAttemptError(tt.remaining, tt.blocked)
			if got != tt.want {
				t.Fatalf("loginAttemptError(%d, %v) = %q, want %q", tt.remaining, tt.blocked, got, tt.want)
			}
		})
	}
}

// TestHandleLogin_RejectsDisallowedMethods locks in the explicit method
// gate added after the GET branch. Without it, PUT/PATCH/DELETE silently
// fell through into the POST path and burned rate-limit budget on
// requests the form never produces.
func TestHandleLogin_RejectsDisallowedMethods(t *testing.T) {
	db := openUserPortalRouteTestDB(t)
	defer db.Close()

	server := newRouteTestServer(t, db)

	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/account/login", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			w := httptest.NewRecorder()

			server.handleLogin(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
			}
			if allow := w.Header().Get("Allow"); allow != "GET, POST" {
				t.Fatalf("Allow header = %q, want %q", allow, "GET, POST")
			}
		})
	}
}

// TestHandleLogin_ParseFormErrorRendersLoginPage exercises the change
// from `http.Error(w, "Bad request", ...)` to a re-render of the login
// template. A user posting an oversized body should still see the form
// with a friendly inline error rather than a bare 400.
func TestHandleLogin_ParseFormErrorRendersLoginPage(t *testing.T) {
	db := openUserPortalRouteTestDB(t)
	defer db.Close()

	server := newRouteTestServer(t, db)

	// Body just over the form-body limit so parseFormWithLimit rejects it.
	body := strings.NewReader(strings.Repeat("a", int(maxUserPortalFormBody)+10))
	req := httptest.NewRequest(http.MethodPost, "/account/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()

	server.handleLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (template re-render, not bare 400)", w.Code, http.StatusOK)
	}
	body2 := w.Body.String()
	if !strings.Contains(body2, "Could not read login form") {
		t.Fatalf("body did not contain inline error message: %s", body2)
	}
	if !strings.Contains(body2, `name="email"`) {
		t.Fatalf("body did not re-render the login form: %s", body2)
	}
}

// TestHandleLogin_RateLimitedPreservesEmailAndUsesRateLimitedBranch
// covers two fixes at once: the email field is now passed through to
// the template on the rate-limited path, and the dedicated
// `RateLimited` template branch is wired up so it can be styled
// distinctly from a generic `Error`.
func TestHandleLogin_RateLimitedPreservesEmailAndUsesRateLimitedBranch(t *testing.T) {
	db := openUserPortalRouteTestDB(t)
	defer db.Close()

	server := newRouteTestServer(t, db)

	// Trip the rate limiter for our test client by recording enough
	// failures to exceed the configured threshold. The default
	// NewRateLimiter is constructed in NewServer and we use the same
	// client IP normalization as the handler.
	clientIP := "127.0.0.1"
	for i := 0; i < 100; i++ {
		if server.rateLimiter.RecordFailure(clientIP) {
			break
		}
	}
	if !server.rateLimiter.IsBlocked(clientIP) {
		t.Fatalf("rate limiter did not block %q after 100 failures; check NewRateLimiter defaults", clientIP)
	}

	form := strings.NewReader("email=alice%40example.com&password=hunter2")
	req := httptest.NewRequest(http.MethodPost, "/account/login", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = clientIP + ":4242"
	w := httptest.NewRecorder()

	server.handleLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	// The dedicated rate-limited branch should render its message.
	if !strings.Contains(body, "Too many failed attempts") {
		t.Fatalf("rate-limited branch was not rendered: %s", body)
	}
	// The submitted email should be re-populated so the user doesn't
	// have to retype it after the lockout expires.
	if !strings.Contains(body, `value="alice@example.com"`) {
		t.Fatalf("email field was not preserved on rate-limited render: %s", body)
	}
}
