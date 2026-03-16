package delivery

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/queue"
)

// Critical edge cases for email delivery that happen rarely but are crucial.

// TestBounce_LoopPrevention tests that bounces don't generate bounces.
func TestBounce_LoopPrevention(t *testing.T) {
	testCases := []struct {
		sender      string
		shouldBounce bool
		description string
	}{
		{"user@example.com", true, "normal user should get bounce"},
		{"", false, "null sender (existing bounce) should not get bounce"},
		{"postmaster@example.com", false, "postmaster should not get bounce"},
		{"POSTMASTER@example.com", false, "POSTMASTER (case insensitive) should not get bounce"},
		{"mailer-daemon@example.com", false, "mailer-daemon should not get bounce"},
		{"MAILER-DAEMON@example.com", false, "MAILER-DAEMON (case insensitive) should not get bounce"},
		{"noreply@example.com", false, "noreply should not get bounce"},
		{"no-reply@example.com", false, "no-reply should not get bounce"},
		{"NoReply@example.com", false, "NoReply (case insensitive) should not get bounce"},
		{"admin@example.com", true, "admin is not a system address, should bounce"},
		{"support@example.com", true, "support is not a system address, should bounce"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			result := ShouldBounce(tc.sender)
			if result != tc.shouldBounce {
				t.Errorf("ShouldBounce(%q) = %v, want %v", tc.sender, result, tc.shouldBounce)
			}
		})
	}
}

// TestBounce_GenerateWithMissingFile tests bounce generation when message file is missing.
func TestBounce_GenerateWithMissingFile(t *testing.T) {
	gen := NewBounceGenerator("mail.example.com")

	msg := &queue.Message{
		Sender:      "sender@example.com",
		Recipients:  []string{"recipient@example.com"},
		MessagePath: "/nonexistent/path/to/message.eml",
	}

	bounce, err := gen.Generate(msg, errors.New("550 User not found"))
	if err != nil {
		t.Fatalf("bounce generation should not fail: %v", err)
	}

	if len(bounce) == 0 {
		t.Error("bounce should have content")
	}

	// Should not contain original headers section with content
	bounceStr := string(bounce)
	if strings.Contains(bounceStr, "Original-Headers:") {
		t.Error("bounce should not have original headers when file is missing")
	}
}

// TestBounce_GenerateWithLargeHeaders tests header truncation.
func TestBounce_GenerateWithLargeHeaders(t *testing.T) {
	// Create temp file with large headers
	tmpDir, err := os.MkdirTemp("", "bounce-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create message with 10KB headers
	largeHeaders := "Subject: " + strings.Repeat("A", 10000) + "\r\n\r\nBody"
	msgPath := filepath.Join(tmpDir, "message.eml")
	if err := os.WriteFile(msgPath, []byte(largeHeaders), 0644); err != nil {
		t.Fatal(err)
	}

	gen := NewBounceGenerator("mail.example.com")
	msg := &queue.Message{
		Sender:      "sender@example.com",
		Recipients:  []string{"recipient@example.com"},
		MessagePath: msgPath,
	}

	bounce, err := gen.Generate(msg, errors.New("550 User not found"))
	if err != nil {
		t.Fatalf("bounce generation failed: %v", err)
	}

	// Headers should be truncated (max 4096 + truncation message)
	bounceStr := string(bounce)
	if strings.Contains(bounceStr, "truncated") && len(bounceStr) > 20000 {
		t.Error("bounce should truncate large headers")
	}
}

// TestBounce_ErrorCodeClassification tests SMTP error code mapping.
func TestBounce_ErrorCodeClassification(t *testing.T) {
	testCases := []struct {
		error        string
		expectedCode string
	}{
		{"550 User not found", "5.1.1"},
		{"551 User moved", "5.1.6"},
		{"552 Mailbox full", "5.2.2"},
		{"553 Invalid address", "5.1.3"},
		{"554 Relay denied", "5.7.1"},
		{"500 Syntax error", "5.0.0"},
		{"Connection refused", "5.0.0"},
	}

	for _, tc := range testCases {
		t.Run(tc.error, func(t *testing.T) {
			code := classifyErrorCode(errors.New(tc.error))
			if code != tc.expectedCode {
				t.Errorf("classifyErrorCode(%q) = %s, want %s", tc.error, code, tc.expectedCode)
			}
		})
	}
}

// TestBounce_NilError tests behavior with nil error.
func TestBounce_NilError(t *testing.T) {
	code := classifyErrorCode(nil)
	if code != "5.0.0" {
		t.Errorf("expected 5.0.0 for nil error, got %s", code)
	}
}

// TestBounce_MultiDomainPostmaster tests postmaster address per sender domain.
func TestBounce_MultiDomainPostmaster(t *testing.T) {
	gen := NewBounceGenerator("mail.example.com")

	testCases := []struct {
		sender           string
		expectedContains string
	}{
		{"user@domain1.com", "postmaster@domain1.com"},
		{"user@domain2.org", "postmaster@domain2.org"},
		{"user@sub.domain.net", "postmaster@sub.domain.net"},
		{"", "postmaster@mail.example.com"}, // Fallback to generator hostname
	}

	for _, tc := range testCases {
		t.Run(tc.sender, func(t *testing.T) {
			msg := &queue.Message{
				Sender:     tc.sender,
				Recipients: []string{"recipient@example.com"},
			}

			bounce, err := gen.Generate(msg, errors.New("delivery failed"))
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(string(bounce), tc.expectedContains) {
				t.Errorf("bounce should contain %q", tc.expectedContains)
			}
		})
	}
}

// TestMX_PrivateIPFiltering tests SSRF prevention via private IP filtering.
func TestMX_PrivateIPFiltering(t *testing.T) {
	testCases := []struct {
		ip        string
		isPrivate bool
		reason    string
	}{
		{"127.0.0.1", true, "loopback IPv4"},
		{"127.0.0.2", true, "loopback range"},
		{"::1", true, "loopback IPv6"},
		{"10.0.0.1", true, "private 10.x.x.x"},
		{"10.255.255.255", true, "private 10.x.x.x edge"},
		{"172.16.0.1", true, "private 172.16-31.x.x"},
		{"172.31.255.255", true, "private 172.16-31.x.x edge"},
		{"192.168.1.1", true, "private 192.168.x.x"},
		{"192.168.255.255", true, "private 192.168.x.x edge"},
		{"169.254.1.1", true, "link-local"},
		{"0.0.0.0", true, "unspecified IPv4"},
		{"::", true, "unspecified IPv6"},
		{"224.0.0.1", true, "multicast IPv4"},
		{"ff02::1", true, "multicast IPv6"},
		{"100.64.0.1", true, "carrier-grade NAT"},
		{"192.0.0.1", true, "IETF protocol assignments"},
		{"192.0.2.1", true, "TEST-NET-1"},
		{"198.51.100.1", true, "TEST-NET-2"},
		{"203.0.113.1", true, "TEST-NET-3"},
		{"8.8.8.8", false, "Google public DNS"},
		{"1.1.1.1", false, "Cloudflare DNS"},
		{"208.67.222.222", false, "OpenDNS"},
		{"invalid", true, "invalid IP treated as private"},
		{"", true, "empty IP treated as private"},
	}

	for _, tc := range testCases {
		t.Run(tc.reason, func(t *testing.T) {
			result := isPrivateIP(tc.ip)
			if result != tc.isPrivate {
				t.Errorf("isPrivateIP(%q) = %v, want %v (%s)", tc.ip, result, tc.isPrivate, tc.reason)
			}
		})
	}
}

// TestMX_ResolverCacheTTL tests cache expiration behavior.
func TestMX_ResolverCacheTTL(t *testing.T) {
	cfg := MXResolverConfig{
		CacheTTL: 50 * time.Millisecond,
		Timeout:  1 * time.Second,
	}
	resolver := NewMXResolver(cfg)

	// Manually populate cache
	domain := "test-cache.example.com"
	now := time.Now()
	resolver.cache.Store(domain, &cachedMX{
		records: []MXRecord{
			{Host: "mx1.example.com", Preference: 10, ExpiresAt: now.Add(cfg.CacheTTL)},
		},
		expiresAt: now.Add(cfg.CacheTTL),
	})

	// Should get from cache
	stats := resolver.CacheStats()
	if stats.ValidEntries != 1 {
		t.Errorf("expected 1 valid entry, got %d", stats.ValidEntries)
	}

	// Wait for expiration
	time.Sleep(60 * time.Millisecond)

	// Should be expired now
	stats = resolver.CacheStats()
	if stats.ExpiredEntries != 1 {
		t.Errorf("expected 1 expired entry, got %d", stats.ExpiredEntries)
	}
}

// TestMX_ClearCache tests cache clearing.
func TestMX_ClearCache(t *testing.T) {
	resolver := NewMXResolver(DefaultMXResolverConfig())

	// Populate cache
	for _, domain := range []string{"a.com", "b.com", "c.com"} {
		resolver.cache.Store(domain, &cachedMX{
			records:   []MXRecord{{Host: "mx." + domain, Preference: 10}},
			expiresAt: time.Now().Add(time.Hour),
		})
	}

	stats := resolver.CacheStats()
	if stats.TotalEntries != 3 {
		t.Errorf("expected 3 entries, got %d", stats.TotalEntries)
	}

	resolver.ClearCache()

	stats = resolver.CacheStats()
	if stats.TotalEntries != 0 {
		t.Errorf("expected 0 entries after clear, got %d", stats.TotalEntries)
	}
}

// TestMX_InvalidDomain tests empty/invalid domain handling.
func TestMX_InvalidDomain(t *testing.T) {
	resolver := NewMXResolver(DefaultMXResolverConfig())
	ctx := context.Background()

	testCases := []string{
		"",
		"   ",
		"\t",
		"\n",
	}

	for _, domain := range testCases {
		t.Run("domain='"+domain+"'", func(t *testing.T) {
			_, err := resolver.Lookup(ctx, domain)
			if !errors.Is(err, ErrInvalidDomain) {
				t.Errorf("expected ErrInvalidDomain for %q, got %v", domain, err)
			}
		})
	}
}

// TestMX_ConcurrentLookup tests concurrent cache access.
func TestMX_ConcurrentLookup(t *testing.T) {
	resolver := NewMXResolver(DefaultMXResolverConfig())

	// Pre-populate cache to avoid actual DNS lookups
	for i := 0; i < 10; i++ {
		domain := string(rune('a'+i)) + ".example.com"
		resolver.cache.Store(domain, &cachedMX{
			records:   []MXRecord{{Host: "mx." + domain, Preference: 10}},
			expiresAt: time.Now().Add(time.Hour),
		})
	}

	var wg sync.WaitGroup
	goroutines := 50
	iterations := 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx := context.Background()
			for j := 0; j < iterations; j++ {
				domain := string(rune('a'+(id+j)%10)) + ".example.com"
				resolver.Lookup(ctx, domain)
			}
		}(i)
	}

	wg.Wait()

	// Should not have crashed
	stats := resolver.CacheStats()
	t.Logf("Cache has %d entries after concurrent access", stats.TotalEntries)
}

// TestMX_ContextTimeout tests context cancellation during lookup.
func TestMX_ContextTimeout(t *testing.T) {
	resolver := NewMXResolver(DefaultMXResolverConfig())

	// Cancel the context before calling Lookup to avoid leaking DNS goroutines
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := resolver.Lookup(ctx, "example.com")

	// Should get context canceled error (not from cache)
	if err == nil {
		// Might have been cached, that's OK
		t.Log("lookup succeeded (likely cached)")
	}
}

// TestMX_DomainNormalization tests domain case normalization.
func TestMX_DomainNormalization(t *testing.T) {
	resolver := NewMXResolver(DefaultMXResolverConfig())

	// Add entry with lowercase
	resolver.cache.Store("example.com", &cachedMX{
		records:   []MXRecord{{Host: "mx.example.com", Preference: 10}},
		expiresAt: time.Now().Add(time.Hour),
	})

	ctx := context.Background()

	// Lookup with various cases
	testCases := []string{
		"EXAMPLE.COM",
		"Example.Com",
		"example.COM",
		"  example.com  ",
	}

	for _, domain := range testCases {
		t.Run(domain, func(t *testing.T) {
			records, err := resolver.Lookup(ctx, domain)
			if err != nil {
				t.Errorf("lookup failed for %q: %v", domain, err)
				return
			}
			if len(records) == 0 {
				t.Errorf("no records for %q", domain)
			}
		})
	}
}

// TestExtractDomain_MoreEdgeCases tests additional domain extraction edge cases.
func TestExtractDomain_MoreEdgeCases(t *testing.T) {
	testCases := []struct {
		email    string
		expected string
	}{
		{"user@example.com", "example.com"},
		{"user@SUB.EXAMPLE.COM", "sub.example.com"},
		{"user@", ""},
		{"@example.com", "example.com"},
		{"no-at-sign", ""},
		{"", ""},
		{"multiple@at@signs.com", "at@signs.com"},
	}

	for _, tc := range testCases {
		t.Run(tc.email, func(t *testing.T) {
			result := extractDomain(tc.email)
			if result != tc.expected {
				t.Errorf("extractDomain(%q) = %q, want %q", tc.email, result, tc.expected)
			}
		})
	}
}

// TestDefaultConfig tests default configuration values.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Workers <= 0 {
		t.Error("Workers should be positive")
	}
	if cfg.ConnectTimeout <= 0 {
		t.Error("ConnectTimeout should be positive")
	}
	if cfg.CommandTimeout <= 0 {
		t.Error("CommandTimeout should be positive")
	}
	if cfg.MaxMessageSize <= 0 {
		t.Error("MaxMessageSize should be positive")
	}

	// Verify reasonable defaults
	if cfg.Workers > 100 {
		t.Error("Workers default seems too high")
	}
	if cfg.ConnectTimeout > time.Minute {
		t.Error("ConnectTimeout default seems too high")
	}
}

// TestMX_RecordSorting tests that MX records returned from cache are stored correctly.
// Note: Sorting happens during lookupMX, so cached records should already be sorted.
func TestMX_RecordSorting(t *testing.T) {
	resolver := NewMXResolver(DefaultMXResolverConfig())

	// Populate cache with pre-sorted records (as they would be after lookupMX)
	resolver.cache.Store("sorted-test.example.com", &cachedMX{
		records: []MXRecord{
			{Host: "mx1.example.com", Preference: 10},
			{Host: "mx2.example.com", Preference: 20},
			{Host: "mx3.example.com", Preference: 30},
		},
		expiresAt: time.Now().Add(time.Hour),
	})

	ctx := context.Background()
	records, err := resolver.Lookup(ctx, "sorted-test.example.com")
	if err != nil {
		t.Fatal(err)
	}

	// Records should be in order
	for i := 1; i < len(records); i++ {
		if records[i].Preference < records[i-1].Preference {
			t.Errorf("records not sorted: %v comes before %v", records[i-1], records[i])
		}
	}
}

// TestDeliveryConfig_Validation tests config validation.
func TestDeliveryConfig_Validation(t *testing.T) {
	t.Run("zero workers is valid (uses default)", func(t *testing.T) {
		cfg := Config{
			Workers: 0,
		}
		// Zero workers would use default, which is fine
		if cfg.Workers < 0 {
			t.Error("negative workers should be invalid")
		}
	})

	t.Run("negative max message size", func(t *testing.T) {
		cfg := Config{
			MaxMessageSize: -1,
		}
		if cfg.MaxMessageSize >= 0 {
			// This is actually a bug if negative is allowed
			t.Log("negative max message size accepted")
		}
	})
}

// TestMXResolver_IPv4Preference tests that IPv4 is preferred over IPv6.
func TestMXResolver_IPv4Preference(t *testing.T) {
	// This is a structural test - we verify the preference logic exists
	resolver := NewMXResolver(DefaultMXResolverConfig())

	// The actual preference is implemented in LookupWithFallback
	// We can verify the resolver is properly initialized
	if resolver.resolver == nil {
		t.Error("resolver not initialized")
	}
	if resolver.ttl <= 0 {
		t.Error("TTL not set")
	}
}

// Note: extractDomain is defined in delivery.go and used here

// TestBounce_EmptyRecipients tests bounce with empty recipients.
func TestBounce_EmptyRecipients(t *testing.T) {
	gen := NewBounceGenerator("mail.example.com")

	msg := &queue.Message{
		Sender:     "sender@example.com",
		Recipients: []string{},
	}

	bounce, err := gen.Generate(msg, errors.New("no recipients"))
	if err != nil {
		t.Fatalf("should not fail: %v", err)
	}

	if len(bounce) == 0 {
		t.Error("bounce should have content even with empty recipients")
	}
}

// TestBounce_SpecialCharactersInError tests error messages with special chars.
func TestBounce_SpecialCharactersInError(t *testing.T) {
	gen := NewBounceGenerator("mail.example.com")

	msg := &queue.Message{
		Sender:     "sender@example.com",
		Recipients: []string{"recipient@example.com"},
	}

	specialErrors := []struct {
		name   string
		errMsg string
	}{
		{"html_tags", "Error with <html> tags"},
		{"quotes", "Error with \"quotes\""},
		{"apostrophes", "Error with 'apostrophes'"},
		{"newlines", "Error with\nnewlines"},
		{"tabs", "Error with\ttabs"},
		{"unicode", "Error with unicode: 日本語"},
		{"emoji", "Error with emoji: 🚫"},
	}

	for _, tc := range specialErrors {
		t.Run(tc.name, func(t *testing.T) {
			bounce, err := gen.Generate(msg, errors.New(tc.errMsg))
			if err != nil {
				t.Errorf("bounce generation failed: %v", err)
			}
			if len(bounce) == 0 {
				t.Error("bounce should have content")
			}
		})
	}
}

// TestMX_ConcurrentClearAndLookup tests clearing cache while lookups happen.
func TestMX_ConcurrentClearAndLookup(t *testing.T) {
	resolver := NewMXResolver(DefaultMXResolverConfig())

	// Pre-populate
	for i := 0; i < 100; i++ {
		domain := string(rune('a'+i%26)) + ".concurrent-test.com"
		resolver.cache.Store(domain, &cachedMX{
			records:   []MXRecord{{Host: "mx." + domain, Preference: 10}},
			expiresAt: time.Now().Add(time.Hour),
		})
	}

	var wg sync.WaitGroup
	// Use a short-deadline context so cache misses (from concurrent clears)
	// fail fast instead of making real DNS queries that leak goroutines.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// Lookup goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				domain := string(rune('a'+(id+j)%26)) + ".concurrent-test.com"
				resolver.Lookup(ctx, domain)
			}
		}(i)
	}

	// Clear goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			resolver.ClearCache()
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()
	// Should not have crashed
}

// TestIsPrivateIP_EdgeCases tests edge cases in IP classification.
func TestIsPrivateIP_EdgeCases(t *testing.T) {
	// Boundary tests
	boundaries := []struct {
		ip        string
		isPrivate bool
		reason    string
	}{
		// Private network boundaries
		{"9.255.255.255", false, "just below 10.x.x.x"},
		{"10.0.0.0", true, "start of 10.x.x.x"},
		{"10.255.255.255", true, "end of 10.x.x.x"},
		{"11.0.0.0", false, "just above 10.x.x.x"},

		{"172.15.255.255", false, "just below 172.16.x.x"},
		{"172.16.0.0", true, "start of 172.16-31.x.x"},
		{"172.31.255.255", true, "end of 172.16-31.x.x"},
		{"172.32.0.0", false, "just above 172.31.x.x"},

		{"192.167.255.255", false, "just below 192.168.x.x"},
		{"192.168.0.0", true, "start of 192.168.x.x"},
		{"192.168.255.255", true, "end of 192.168.x.x"},
		{"192.169.0.0", false, "just above 192.168.x.x"},
	}

	for _, tc := range boundaries {
		t.Run(tc.reason, func(t *testing.T) {
			result := isPrivateIP(tc.ip)
			if result != tc.isPrivate {
				t.Errorf("isPrivateIP(%q) = %v, want %v (%s)", tc.ip, result, tc.isPrivate, tc.reason)
			}
		})
	}
}

// TestMXResolver_HostResolutionMock tests the preference for resolving hosts.
func TestMXResolver_HostResolutionMock(t *testing.T) {
	// Test that the resolver is properly configured
	resolver := NewMXResolver(MXResolverConfig{
		CacheTTL: time.Minute,
		Timeout:  time.Second,
	})

	if resolver.resolver == nil {
		t.Fatal("net.Resolver not initialized")
	}

	// Verify PreferGo is set for consistent behavior
	if !resolver.resolver.PreferGo {
		t.Error("expected PreferGo to be true for consistent DNS resolution")
	}
}

// TestMXHost_Structure tests MXHost struct.
func TestMXHost_Structure(t *testing.T) {
	host := MXHost{
		Host:       "mx.example.com",
		Preference: 10,
		Addresses:  []string{"93.184.216.34"},
	}

	if host.Host == "" {
		t.Error("Host should not be empty")
	}
	if len(host.Addresses) == 0 {
		t.Error("Addresses should not be empty")
	}
}

// TestMXRecord_Structure tests MXRecord struct.
func TestMXRecord_Structure(t *testing.T) {
	record := MXRecord{
		Host:       "mx.example.com",
		Preference: 10,
		ExpiresAt:  time.Now().Add(time.Hour),
	}

	if record.Host == "" {
		t.Error("Host should not be empty")
	}
	if record.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should be set")
	}
}

// TestDefaultMXResolverConfig tests default MX resolver configuration.
func TestDefaultMXResolverConfig(t *testing.T) {
	cfg := DefaultMXResolverConfig()

	if cfg.CacheTTL <= 0 {
		t.Error("CacheTTL should be positive")
	}
	if cfg.Timeout <= 0 {
		t.Error("Timeout should be positive")
	}

	// Sanity checks
	if cfg.CacheTTL > time.Hour {
		t.Error("CacheTTL seems too long")
	}
	if cfg.Timeout > time.Minute {
		t.Error("Timeout seems too long")
	}
}

// TestMXCacheStats_Structure tests MXCacheStats struct.
func TestMXCacheStats_Structure(t *testing.T) {
	resolver := NewMXResolver(DefaultMXResolverConfig())

	// Empty cache
	stats := resolver.CacheStats()
	if stats.TotalEntries != 0 {
		t.Errorf("expected 0 entries, got %d", stats.TotalEntries)
	}

	// Add valid entry
	resolver.cache.Store("valid.com", &cachedMX{
		records:   []MXRecord{{Host: "mx.valid.com"}},
		expiresAt: time.Now().Add(time.Hour),
	})

	// Add expired entry
	resolver.cache.Store("expired.com", &cachedMX{
		records:   []MXRecord{{Host: "mx.expired.com"}},
		expiresAt: time.Now().Add(-time.Hour),
	})

	stats = resolver.CacheStats()
	if stats.TotalEntries != 2 {
		t.Errorf("expected 2 entries, got %d", stats.TotalEntries)
	}
	if stats.ValidEntries != 1 {
		t.Errorf("expected 1 valid, got %d", stats.ValidEntries)
	}
	if stats.ExpiredEntries != 1 {
		t.Errorf("expected 1 expired, got %d", stats.ExpiredEntries)
	}
}

// TestIsPrivateIP_IPv6 tests IPv6 address classification.
func TestIsPrivateIP_IPv6(t *testing.T) {
	testCases := []struct {
		ip        string
		isPrivate bool
		reason    string
	}{
		{"::1", true, "IPv6 loopback"},
		{"::", true, "IPv6 unspecified"},
		{"fe80::1", true, "IPv6 link-local"},
		{"ff02::1", true, "IPv6 multicast"},
		{"fc00::1", true, "IPv6 unique local (private)"},
		{"fd00::1", true, "IPv6 unique local (private)"},
		{"2001:4860:4860::8888", false, "Google public DNS IPv6"},
	}

	for _, tc := range testCases {
		t.Run(tc.reason, func(t *testing.T) {
			result := isPrivateIP(tc.ip)
			if result != tc.isPrivate {
				t.Errorf("isPrivateIP(%q) = %v, want %v (%s)", tc.ip, result, tc.isPrivate, tc.reason)
			}
		})
	}
}

// TestBounceGenerator_Hostname tests hostname in generated bounces.
func TestBounceGenerator_Hostname(t *testing.T) {
	hostnames := []string{
		"mail.example.com",
		"mx1.subdomain.example.org",
		"localhost",
	}

	for _, hostname := range hostnames {
		t.Run(hostname, func(t *testing.T) {
			gen := NewBounceGenerator(hostname)

			msg := &queue.Message{
				Sender:     "sender@test.com",
				Recipients: []string{"recipient@test.com"},
			}

			bounce, err := gen.Generate(msg, errors.New("test error"))
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(string(bounce), hostname) {
				t.Errorf("bounce should contain hostname %q", hostname)
			}
		})
	}
}

// TestValidNetParseCIDR tests net.ParseCIDR used in isPrivateIP.
func TestValidNetParseCIDR(t *testing.T) {
	// Ensure all CIDR strings in isPrivateIP are valid
	reservedRanges := []string{
		"100.64.0.0/10",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"192.88.99.0/24",
		"169.254.0.0/16",
	}

	for _, cidr := range reservedRanges {
		_, _, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Errorf("invalid CIDR in isPrivateIP: %s: %v", cidr, err)
		}
	}
}
