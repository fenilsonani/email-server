package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCSRFBinding_BindsAuthenticatedTokensToSession(t *testing.T) {
	resetAdminCSRFTokens()

	server := &Server{rateLimiter: DefaultRateLimiter()}
	handler := server.withCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	getReq := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	getReq.RemoteAddr = "127.0.0.1:1234"
	getReq.AddCookie(&http.Cookie{Name: "admin_session", Value: "session-a"})
	getW := httptest.NewRecorder()
	handler.ServeHTTP(getW, getReq)

	token := getW.Header().Get("X-CSRF-Token")
	if len(token) == 0 {
		t.Fatalf("expected csrf token on GET")
	}

	postReq := httptest.NewRequest(http.MethodPost, "/admin/test", nil)
	postReq.RemoteAddr = "127.0.0.1:1234"
	postReq.Header.Set("X-CSRF-Token", token)
	postReq.AddCookie(&http.Cookie{Name: "admin_session", Value: "session-b"})
	postW := httptest.NewRecorder()
	handler.ServeHTTP(postW, postReq)

	if postW.Code != http.StatusForbidden {
		t.Fatalf("cross-session POST status = %d, want %d", postW.Code, http.StatusForbidden)
	}
}

func TestCSRFBinding_BindsAnonymousTokensToClientIP(t *testing.T) {
	resetAdminCSRFTokens()

	server := &Server{rateLimiter: DefaultRateLimiter()}
	handler := server.withCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	getReq := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	getReq.RemoteAddr = "127.0.0.1:1234"
	getReq.Header.Set("X-Forwarded-For", "203.0.113.10")
	getW := httptest.NewRecorder()
	handler.ServeHTTP(getW, getReq)

	token := getW.Header().Get("X-CSRF-Token")
	if len(token) == 0 {
		t.Fatalf("expected csrf token on GET")
	}

	postReq := httptest.NewRequest(http.MethodPost, "/admin/login", nil)
	postReq.RemoteAddr = "127.0.0.1:1234"
	postReq.Header.Set("X-Forwarded-For", "198.51.100.25")
	postReq.Header.Set("X-CSRF-Token", token)
	postW := httptest.NewRecorder()
	handler.ServeHTTP(postW, postReq)

	if postW.Code != http.StatusForbidden {
		t.Fatalf("cross-client POST status = %d, want %d", postW.Code, http.StatusForbidden)
	}
}

func TestCSRFBinding_RejectsOversizedFormBody(t *testing.T) {
	resetAdminCSRFTokens()

	server := &Server{rateLimiter: DefaultRateLimiter()}
	handler := server.withCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	getReq := httptest.NewRequest(http.MethodGet, "/admin/test", nil)
	getReq.RemoteAddr = "127.0.0.1:1234"
	getW := httptest.NewRecorder()
	handler.ServeHTTP(getW, getReq)

	token := getW.Header().Get("X-CSRF-Token")
	if len(token) == 0 {
		t.Fatalf("expected csrf token on GET")
	}

	form := url.Values{
		"csrf_token": {token},
		"padding":    {strings.Repeat("a", int(maxAdminFormBody))},
	}
	postReq := httptest.NewRequest(http.MethodPost, "/admin/test", strings.NewReader(form.Encode()))
	postReq.RemoteAddr = "127.0.0.1:1234"
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postW := httptest.NewRecorder()
	handler.ServeHTTP(postW, postReq)

	if postW.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized POST status = %d, want %d", postW.Code, http.StatusRequestEntityTooLarge)
	}
}

func resetAdminCSRFTokens() {
	csrfTokensMu.Lock()
	defer csrfTokensMu.Unlock()
	csrfTokens = make(map[string]csrfTokenState)
}
