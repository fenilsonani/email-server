package userportal

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestCSRFBinding_AcceptsMultipartFormBody(t *testing.T) {
	resetUserPortalCSRFTokens()

	server := &Server{}
	handler := server.withCSRF(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	getReq := httptest.NewRequest(http.MethodGet, "/account/profile", nil)
	getReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-a"})
	getW := httptest.NewRecorder()
	handler(getW, getReq)

	token := getW.Header().Get("X-CSRF-Token")
	if len(token) == 0 {
		t.Fatalf("expected csrf token on GET")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("csrf_token", token); err != nil {
		t.Fatalf("WriteField(csrf_token) error = %v", err)
	}
	fileWriter, err := writer.CreateFormFile("avatar", "avatar.txt")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := fileWriter.Write([]byte(strings.Repeat("a", 4096))); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/account/profile", &body)
	postReq.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-a"})
	postReq.Header.Set("Content-Type", writer.FormDataContentType())
	postW := httptest.NewRecorder()
	handler(postW, postReq)

	if postW.Code != http.StatusOK {
		t.Fatalf("multipart POST status = %d, want %d, body=%q", postW.Code, http.StatusOK, postW.Body.String())
	}
}
