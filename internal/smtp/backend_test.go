package smtp

import (
	"sync"
	"testing"
)

func TestGenerateID(t *testing.T) {
	t.Run("uniqueness", func(t *testing.T) {
		ids := make(map[string]bool)
		for i := 0; i < 10000; i++ {
			id := generateID()
			if ids[id] {
				t.Errorf("Duplicate ID generated: %s", id)
			}
			ids[id] = true
		}
	})

	t.Run("format", func(t *testing.T) {
		id := generateID()

		// Should be 32 hex characters (16 bytes * 2)
		if len(id) != 32 {
			t.Errorf("ID length = %d, want 32", len(id))
		}

		// Should be valid hex
		for _, c := range id {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
				t.Errorf("ID contains non-hex character: %c", c)
			}
		}
	})

	t.Run("concurrent", func(t *testing.T) {
		ids := sync.Map{}
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					id := generateID()
					if _, loaded := ids.LoadOrStore(id, true); loaded {
						t.Errorf("Duplicate concurrent ID: %s", id)
					}
				}
			}()
		}
		wg.Wait()
	})
}

func TestParseAddress(t *testing.T) {
	tests := []struct {
		addr   string
		local  string
		domain string
	}{
		{"user@example.com", "user", "example.com"},
		{"USER@EXAMPLE.COM", "user", "example.com"},
		{"<user@example.com>", "user", "example.com"},
		{"user@sub.example.com", "user", "sub.example.com"},
		{"user+tag@example.com", "user+tag", "example.com"},
		{"user.name@example.com", "user.name", "example.com"},
		{"noatsign", "noatsign", ""},
		{"", "", ""},
		{"@domain.com", "", "domain.com"},
		{"user@", "user", ""},
		{"<>", "", ""},
		{"<<user@example.com>>", "<user", "example.com>"},
		{"user@@example.com", "user", "@example.com"},
		{"Multi Part User@example.com", "multi part user", "example.com"},
		{"<User.Name+Tag@Example.COM>", "user.name+tag", "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			local, domain := parseAddress(tt.addr)
			if local != tt.local {
				t.Errorf("parseAddress(%q) local = %q, want %q", tt.addr, local, tt.local)
			}
			if domain != tt.domain {
				t.Errorf("parseAddress(%q) domain = %q, want %q", tt.addr, domain, tt.domain)
			}
		})
	}
}

func BenchmarkGenerateID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		generateID()
	}
}

func BenchmarkParseAddress(b *testing.B) {
	addr := "user@example.com"
	for i := 0; i < b.N; i++ {
		parseAddress(addr)
	}
}
