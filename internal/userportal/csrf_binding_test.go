package userportal

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func resetUserPortalCSRFTokens() {
	csrfTokensMu.Lock()
	defer csrfTokensMu.Unlock()
	csrfTokens = make(map[string]csrfTokenState)
}

func TestCSRFBinding_UsesSessionWhenPresent(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/account/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-a"})

	if got := server.csrfBinding(req); got != "session:session-a" {
		t.Fatalf("csrfBinding() = %q, want %q", got, "session:session-a")
	}
}
