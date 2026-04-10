package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleLogin_RejectsDisallowedMethods locks in the explicit method
// gate at the very top of handleLogin. Without it, PUT/PATCH/DELETE would
// silently fall through into the POST path and burn rate-limit budget on
// requests the form never produces.
func TestHandleLogin_RejectsDisallowedMethods(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			server := &Server{rateLimiter: DefaultRateLimiter()}

			req := httptest.NewRequest(method, "/admin/login", nil)
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

// TestHandleLogin_MethodGateBeatsRateLimit covers the round-2 fix where
// the method gate was moved above the rate-limit branch. A blocked IP
// sending a disallowed method should still get a 405 + Allow contract,
// not the rate-limit page.
func TestHandleLogin_MethodGateBeatsRateLimit(t *testing.T) {
	server := &Server{rateLimiter: DefaultRateLimiter()}

	// Trip the rate limiter for the test client.
	clientIP := "127.0.0.1"
	for i := 0; i < 100; i++ {
		if server.rateLimiter.RecordFailure(clientIP) {
			break
		}
	}
	if !server.rateLimiter.IsBlocked(clientIP) {
		t.Fatalf("rate limiter did not block %q after 100 failures", clientIP)
	}

	req := httptest.NewRequest(http.MethodPut, "/admin/login", nil)
	req.RemoteAddr = clientIP + ":4242"
	w := httptest.NewRecorder()

	server.handleLogin(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d (blocked client must still see 405 for disallowed method)", w.Code, http.StatusMethodNotAllowed)
	}
	if allow := w.Header().Get("Allow"); allow != "GET, POST" {
		t.Fatalf("Allow header = %q, want %q", allow, "GET, POST")
	}
}
