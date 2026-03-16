package userportal

import "testing"

func TestIsValidCSRFToken(t *testing.T) {
	t.Run("valid hex token", func(t *testing.T) {
		token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		if !isValidCSRFToken(token) {
			t.Fatalf("expected token to be valid")
		}
	})

	t.Run("rejects non hex token", func(t *testing.T) {
		token := "zzzz456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		if isValidCSRFToken(token) {
			t.Fatalf("expected token to be invalid")
		}
	})

	t.Run("rejects short token", func(t *testing.T) {
		token := "abcd"
		if isValidCSRFToken(token) {
			t.Fatalf("expected short token to be invalid")
		}
	})
}
