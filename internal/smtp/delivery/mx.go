// Package delivery implements outbound email delivery.
package delivery

import (
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Common errors
var (
	ErrNoMXRecords  = errors.New("no MX records found")
	ErrInvalidDomain = errors.New("invalid domain")
)

// MXRecord represents a mail exchanger record.
type MXRecord struct {
	Host       string
	Preference uint16
	ExpiresAt  time.Time
}

// MXResolver resolves MX records with caching.
type MXResolver struct {
	cache    sync.Map // domain -> *cachedMX
	resolver *net.Resolver
	ttl      time.Duration
}

type cachedMX struct {
	records   []MXRecord
	expiresAt time.Time
}

// MXResolverConfig configures the MX resolver.
type MXResolverConfig struct {
	// CacheTTL is how long to cache MX records.
	CacheTTL time.Duration
	// Timeout is the DNS lookup timeout.
	Timeout time.Duration
}

// DefaultMXResolverConfig returns default configuration.
func DefaultMXResolverConfig() MXResolverConfig {
	return MXResolverConfig{
		CacheTTL: 5 * time.Minute,
		Timeout:  10 * time.Second,
	}
}

// NewMXResolver creates a new MX resolver.
func NewMXResolver(cfg MXResolverConfig) *MXResolver {
	return &MXResolver{
		resolver: &net.Resolver{
			PreferGo: true,
		},
		ttl: cfg.CacheTTL,
	}
}

// Lookup returns the MX records for a domain, sorted by preference.
func (r *MXResolver) Lookup(ctx context.Context, domain string) ([]MXRecord, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil, ErrInvalidDomain
	}

	// Check cache first
	if cached, ok := r.cache.Load(domain); ok {
		c := cached.(*cachedMX)
		if time.Now().Before(c.expiresAt) {
			return c.records, nil
		}
		// Cache expired, delete it
		r.cache.Delete(domain)
	}

	// Perform DNS lookup
	records, err := r.lookupMX(ctx, domain)
	if err != nil {
		return nil, err
	}

	// Cache the results
	expiresAt := time.Now().Add(r.ttl)
	for i := range records {
		records[i].ExpiresAt = expiresAt
	}

	r.cache.Store(domain, &cachedMX{
		records:   records,
		expiresAt: expiresAt,
	})

	return records, nil
}

// lookupMX performs the actual DNS lookup.
func (r *MXResolver) lookupMX(ctx context.Context, domain string) ([]MXRecord, error) {
	mxRecords, err := r.resolver.LookupMX(ctx, domain)
	if err != nil {
		// Check if it's a "no such host" error - try A record fallback
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			if dnsErr.IsNotFound {
				// Try A record fallback (RFC 5321)
				return r.lookupAFallback(ctx, domain)
			}
		}
		return nil, err
	}

	if len(mxRecords) == 0 {
		// No MX records, try A record fallback
		return r.lookupAFallback(ctx, domain)
	}

	records := make([]MXRecord, len(mxRecords))
	for i, mx := range mxRecords {
		records[i] = MXRecord{
			Host:       strings.TrimSuffix(mx.Host, "."),
			Preference: mx.Pref,
		}
	}

	// Sort by preference (lower is better)
	sort.Slice(records, func(i, j int) bool {
		return records[i].Preference < records[j].Preference
	})

	return records, nil
}

// lookupAFallback tries to use the domain's A record as a mail server.
// Per RFC 5321, if no MX records exist, the domain itself should be tried.
func (r *MXResolver) lookupAFallback(ctx context.Context, domain string) ([]MXRecord, error) {
	// Check if domain has an A record
	addrs, err := r.resolver.LookupHost(ctx, domain)
	if err != nil {
		return nil, ErrNoMXRecords
	}

	if len(addrs) == 0 {
		return nil, ErrNoMXRecords
	}

	// Use the domain itself as the mail server
	return []MXRecord{
		{
			Host:       domain,
			Preference: 0,
		},
	}, nil
}

// LookupWithFallback looks up MX records and returns IPs for each.
func (r *MXResolver) LookupWithFallback(ctx context.Context, domain string) ([]MXHost, error) {
	mxRecords, err := r.Lookup(ctx, domain)
	if err != nil {
		return nil, err
	}

	var hosts []MXHost
	for _, mx := range mxRecords {
		// Resolve the MX host to IP addresses
		addrs, err := r.resolver.LookupHost(ctx, mx.Host)
		if err != nil {
			continue // Skip this MX if we can't resolve it
		}

		// Prefer IPv4 addresses
		var ipv4, ipv6 []string
		for _, addr := range addrs {
			if ip := net.ParseIP(addr); ip != nil {
				if ip.To4() != nil {
					ipv4 = append(ipv4, addr)
				} else {
					ipv6 = append(ipv6, addr)
				}
			}
		}

		// IPv4 first, then IPv6
		allAddrs := append(ipv4, ipv6...)
		if len(allAddrs) > 0 {
			hosts = append(hosts, MXHost{
				Host:       mx.Host,
				Preference: mx.Preference,
				Addresses:  allAddrs,
			})
		}
	}

	if len(hosts) == 0 {
		return nil, ErrNoMXRecords
	}

	return hosts, nil
}

// MXHost represents a resolved MX host with its IP addresses.
type MXHost struct {
	Host       string
	Preference uint16
	Addresses  []string
}

// ClearCache clears the MX cache.
func (r *MXResolver) ClearCache() {
	r.cache.Range(func(key, value interface{}) bool {
		r.cache.Delete(key)
		return true
	})
}

// CacheStats returns cache statistics.
func (r *MXResolver) CacheStats() MXCacheStats {
	var stats MXCacheStats
	now := time.Now()

	r.cache.Range(func(key, value interface{}) bool {
		stats.TotalEntries++
		c := value.(*cachedMX)
		if now.Before(c.expiresAt) {
			stats.ValidEntries++
		} else {
			stats.ExpiredEntries++
		}
		return true
	})

	return stats
}

// MXCacheStats contains MX cache statistics.
type MXCacheStats struct {
	TotalEntries   int
	ValidEntries   int
	ExpiredEntries int
}
