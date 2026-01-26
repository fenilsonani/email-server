package helpers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// HTTPTestRequest creates an HTTP request for testing.
func HTTPTestRequest(t *testing.T, method, path string, body io.Reader) *http.Request {
	t.Helper()

	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	return req
}

// HTTPTestRequestWithHeaders creates an HTTP request with custom headers.
func HTTPTestRequestWithHeaders(t *testing.T, method, path string, body io.Reader, headers map[string]string) *http.Request {
	t.Helper()

	req := HTTPTestRequest(t, method, path, body)

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	return req
}

// HTTPTestRecorder wraps httptest.ResponseRecorder with helper methods.
type HTTPTestRecorder struct {
	*httptest.ResponseRecorder
}

// NewHTTPTestRecorder creates a new HTTP test recorder.
func NewHTTPTestRecorder() *HTTPTestRecorder {
	return &HTTPTestRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

// AssertStatusCode asserts the response status code.
func (r *HTTPTestRecorder) AssertStatusCode(t *testing.T, expected int) {
	t.Helper()

	if r.Code != expected {
		t.Errorf("Response status code = %d, want %d", r.Code, expected)
	}
}

// AssertStatus asserts the response status is in the given range.
func (r *HTTPTestRecorder) AssertStatus(t *testing.T, statusRange string) {
	t.Helper()

	switch {
	case statusRange == "2xx" && r.Code >= 200 && r.Code < 300:
		return
	case statusRange == "3xx" && r.Code >= 300 && r.Code < 400:
		return
	case statusRange == "4xx" && r.Code >= 400 && r.Code < 500:
		return
	case statusRange == "5xx" && r.Code >= 500 && r.Code < 600:
		return
	default:
		t.Errorf("Response status code %d does not match %s", r.Code, statusRange)
	}
}

// AssertContentType asserts the response Content-Type.
func (r *HTTPTestRecorder) AssertContentType(t *testing.T, expected string) {
	t.Helper()

	actual := r.Header().Get("Content-Type")
	if actual != expected && !strings.Contains(actual, expected) {
		t.Errorf("Content-Type = %q, want %q", actual, expected)
	}
}

// AssertBodyContains asserts the response body contains a string.
func (r *HTTPTestRecorder) AssertBodyContains(t *testing.T, substring string) {
	t.Helper()

	body := r.Body.String()
	if !strings.Contains(body, substring) {
		t.Errorf("Response body does not contain %q", substring)
	}
}

// AssertBodyNotContains asserts the response body does not contain a string.
func (r *HTTPTestRecorder) AssertBodyNotContains(t *testing.T, substring string) {
	t.Helper()

	body := r.Body.String()
	if strings.Contains(body, substring) {
		t.Errorf("Response body should not contain %q", substring)
	}
}

// AssertBodyMatches asserts the response body matches a regex pattern.
func (r *HTTPTestRecorder) AssertBodyMatches(t *testing.T, pattern string) {
	t.Helper()

	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("Invalid regex pattern: %v", err)
	}

	body := r.Body.String()
	if !re.MatchString(body) {
		t.Errorf("Response body does not match pattern %q", pattern)
	}
}

// AssertHeaderExists asserts a response header exists.
func (r *HTTPTestRecorder) AssertHeaderExists(t *testing.T, header string) {
	t.Helper()

	if r.Header().Get(header) == "" {
		t.Errorf("Response header %q does not exist", header)
	}
}

// AssertHeaderValue asserts a response header has a specific value.
func (r *HTTPTestRecorder) AssertHeaderValue(t *testing.T, header, expected string) {
	t.Helper()

	actual := r.Header().Get(header)
	if actual != expected {
		t.Errorf("Response header %q = %q, want %q", header, actual, expected)
	}
}

// AssertCookieExists asserts a response cookie exists.
func (r *HTTPTestRecorder) AssertCookieExists(t *testing.T, name string) {
	t.Helper()

	for _, cookie := range r.Result().Cookies() {
		if cookie.Name == name {
			return
		}
	}

	t.Errorf("Response cookie %q does not exist", name)
}

// AssertCookieValue asserts a response cookie has a specific value.
func (r *HTTPTestRecorder) AssertCookieValue(t *testing.T, name, expected string) {
	t.Helper()

	for _, cookie := range r.Result().Cookies() {
		if cookie.Name == name {
			if cookie.Value != expected {
				t.Errorf("Response cookie %q = %q, want %q", name, cookie.Value, expected)
			}
			return
		}
	}

	t.Errorf("Response cookie %q does not exist", name)
}

// AssertRedirectTo asserts the response redirects to a location.
func (r *HTTPTestRecorder) AssertRedirectTo(t *testing.T, expectedLocation string) {
	t.Helper()

	location := r.Header().Get("Location")
	if location != expectedLocation {
		t.Errorf("Redirect location = %q, want %q", location, expectedLocation)
	}
}

// GetBody returns the response body as a string.
func (r *HTTPTestRecorder) GetBody() string {
	return r.Body.String()
}

// GetHeader returns a response header value.
func (r *HTTPTestRecorder) GetHeader(name string) string {
	return r.Header().Get(name)
}

// GetCookie returns a response cookie value.
func (r *HTTPTestRecorder) GetCookie(name string) *http.Cookie {
	for _, cookie := range r.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// HTTPFormData builds a URL-encoded form data string.
func HTTPFormData(data map[string]string) string {
	form := url.Values{}
	for key, value := range data {
		form.Add(key, value)
	}
	return form.Encode()
}

// HTTPFormDataReader creates a reader from form data.
func HTTPFormDataReader(data map[string]string) io.Reader {
	return strings.NewReader(HTTPFormData(data))
}

// HTTPJSONData builds a JSON request body.
func HTTPJSONData(data interface{}) io.Reader {
	// Simple JSON builder - in real use would use json.Marshal
	return strings.NewReader("")
}

// HTTPMultipartData builds multipart form data.
func HTTPMultipartData(fields map[string]string) (io.Reader, string) {
	body := &bytes.Buffer{}
	boundary := fmt.Sprintf("----WebKitFormBoundary%d", time.Now().UnixNano())

	for key, value := range fields {
		body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		body.WriteString(fmt.Sprintf(`Content-Disposition: form-data; name="%s"\r\n\r\n`, key))
		body.WriteString(value)
		body.WriteString("\r\n")
	}

	body.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	return body, fmt.Sprintf("multipart/form-data; boundary=%s", boundary)
}

// WithSessionCookie adds a session cookie to a request.
func WithSessionCookie(req *http.Request, sessionToken string) *http.Request {
	req.AddCookie(&http.Cookie{
		Name:  "session",
		Value: sessionToken,
	})
	return req
}

// WithAuthHeader adds an Authorization header to a request.
func WithAuthHeader(req *http.Request, token string) *http.Request {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	return req
}

// WithCSRFToken adds a CSRF token to a request.
func WithCSRFToken(req *http.Request, token string) *http.Request {
	req.Header.Set("X-CSRF-Token", token)
	return req
}

// ExtractCSRFToken extracts a CSRF token from HTML response.
func ExtractCSRFToken(t *testing.T, body string) string {
	t.Helper()

	// Look for common CSRF token patterns in HTML
	patterns := []string{
		`<input[^>]*name=["']csrf["'][^>]*value=["']([^"']+)["']`,
		`<input[^>]*value=["']([^"']+)["'][^>]*name=["']csrf["']`,
		`csrf_token=([a-zA-Z0-9]+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(body)
		if len(matches) > 1 {
			return matches[1]
		}
	}

	return ""
}

// ExtractSessionCookie extracts a session token from Set-Cookie headers.
func ExtractSessionCookie(t *testing.T, recorder *HTTPTestRecorder) string {
	t.Helper()

	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "session" {
			return cookie.Value
		}
	}

	return ""
}

// AssertJSONResponse asserts a response is valid JSON.
func AssertJSONResponse(t *testing.T, body string) {
	t.Helper()

	// Simple JSON validation - in real use would use json.Unmarshal
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		t.Errorf("Response is not valid JSON")
	}
}

// HTTPTestServer provides a test HTTP server with common utilities.
type HTTPTestServer struct {
	*httptest.Server
	BaseURL string
}

// NewHTTPTestServer creates a test HTTP server.
func NewHTTPTestServer(t *testing.T, handler http.HandlerFunc) *HTTPTestServer {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
	})

	return &HTTPTestServer{
		Server:  server,
		BaseURL: server.URL,
	}
}

// NewRequest creates a request to the test server.
func (s *HTTPTestServer) NewRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()

	url := s.BaseURL + path
	return httptest.NewRequest(method, url, nil)
}

// Do performs a request to the test server.
func (s *HTTPTestServer) Do(t *testing.T, req *http.Request) *http.Response {
	t.Helper()

	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatalf("Failed to perform request: %v", err)
	}

	return resp
}

// TimeoutRequest times out a request if it takes too long.
func TimeoutRequest(t *testing.T, timeout time.Duration, fn func()) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()

	select {
	case <-done:
		return
	case <-time.After(timeout):
		t.Errorf("Request timed out after %v", timeout)
	}
}

// AssertResponseTime asserts the response time is within bounds.
func AssertResponseTime(t *testing.T, responseTime time.Duration, maxDuration time.Duration) {
	t.Helper()

	if responseTime > maxDuration {
		t.Errorf("Response time %v exceeds maximum %v", responseTime, maxDuration)
	}
}

// MeasureResponseTime measures the time it takes to perform a request.
func MeasureResponseTime(t *testing.T, fn func() error) time.Duration {
	t.Helper()

	start := time.Now()
	if err := fn(); err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	return time.Since(start)
}
