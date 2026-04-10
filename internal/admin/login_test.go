package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleLogin_RejectsDisallowedMethods locks in the explicit method
// gate added after the GET branch in handleLogin. Without it, PUT/PATCH/
// DELETE would silently fall through into the POST path and burn
// rate-limit budget on requests the form never produces.
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
