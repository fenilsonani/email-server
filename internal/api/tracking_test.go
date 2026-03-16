package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetClientIP_TrustedLocalProxyOnly(t *testing.T) {
	t.Run("uses forwarded header from trusted localhost proxy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/t/o/abc", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "198.51.100.8, 203.0.113.10")

		if got := getClientIP(req); got != "203.0.113.10" {
			t.Fatalf("getClientIP() = %q, want %q", got, "203.0.113.10")
		}
	})

	t.Run("ignores spoofed forwarded header from remote client", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/t/o/abc", nil)
		req.RemoteAddr = "198.51.100.25:2525"
		req.Header.Set("X-Forwarded-For", "203.0.113.10")

		if got := getClientIP(req); got != "198.51.100.25" {
			t.Fatalf("getClientIP() = %q, want %q", got, "198.51.100.25")
		}
	})

	t.Run("uses forwarded header from trusted ipv6 localhost proxy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/t/o/abc", nil)
		req.RemoteAddr = "[::1]:1234"
		req.Header.Set("X-Forwarded-For", "198.51.100.9, 203.0.113.11")

		if got := getClientIP(req); got != "203.0.113.11" {
			t.Fatalf("getClientIP() = %q, want %q", got, "203.0.113.11")
		}
	})

	t.Run("falls back to x-real-ip when forwarded-for is absent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/t/o/abc", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Real-IP", "203.0.113.12")

		if got := getClientIP(req); got != "203.0.113.12" {
			t.Fatalf("getClientIP() = %q, want %q", got, "203.0.113.12")
		}
	})

	t.Run("ignores malformed forwarded headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/t/o/abc", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", "not-an-ip, also-bad")

		if got := getClientIP(req); got != "127.0.0.1" {
			t.Fatalf("getClientIP() = %q, want %q", got, "127.0.0.1")
		}
	})
}
