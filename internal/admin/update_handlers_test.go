package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateHandlersGetClientIP_TrustedLocalProxyOnly(t *testing.T) {
	t.Run("uses forwarded header from trusted localhost proxy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/system/update", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "203.0.113.10, 127.0.0.1")

		if got := getClientIP(req); got != "203.0.113.10" {
			t.Fatalf("getClientIP() = %q, want %q", got, "203.0.113.10")
		}
	})

	t.Run("ignores spoofed forwarded header from remote client", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/system/update", nil)
		req.RemoteAddr = "198.51.100.25:2525"
		req.Header.Set("X-Forwarded-For", "203.0.113.10")

		if got := getClientIP(req); got != "198.51.100.25" {
			t.Fatalf("getClientIP() = %q, want %q", got, "198.51.100.25")
		}
	})
}
