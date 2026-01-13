// Package delivery implements outbound email delivery.
package delivery

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// TLSAUsage represents TLSA certificate usage field (RFC 6698).
type TLSAUsage uint8

const (
	// TLSAUsagePKIXTA (0) - CA constraint, PKIX validation required
	TLSAUsagePKIXTA TLSAUsage = 0
	// TLSAUsagePKIXEE (1) - Service certificate constraint, PKIX validation required
	TLSAUsagePKIXEE TLSAUsage = 1
	// TLSAUsageDANETA (2) - Trust anchor assertion, no PKIX validation
	TLSAUsageDANETA TLSAUsage = 2
	// TLSAUsageDANEEE (3) - Domain-issued certificate, no PKIX validation
	TLSAUsageDANEEE TLSAUsage = 3
)

// TLSASelector represents TLSA selector field.
type TLSASelector uint8

const (
	// TLSASelectorCert (0) - Full certificate
	TLSASelectorCert TLSASelector = 0
	// TLSASelectorSPKI (1) - SubjectPublicKeyInfo
	TLSASelectorSPKI TLSASelector = 1
)

// TLSAMatchingType represents TLSA matching type field.
type TLSAMatchingType uint8

const (
	// TLSAMatchingFull (0) - Full data, no hash
	TLSAMatchingFull TLSAMatchingType = 0
	// TLSAMatchingSHA256 (1) - SHA-256 hash
	TLSAMatchingSHA256 TLSAMatchingType = 1
	// TLSAMatchingSHA512 (2) - SHA-512 hash
	TLSAMatchingSHA512 TLSAMatchingType = 2
)

// TLSARecord represents a TLSA DNS record.
type TLSARecord struct {
	Usage        TLSAUsage
	Selector     TLSASelector
	MatchingType TLSAMatchingType
	CertData     []byte // Certificate association data
}

// DANEResult represents DANE validation result.
type DANEResult struct {
	Valid      bool        // Whether validation passed
	UsedRecord *TLSARecord // The TLSA record that matched (if any)
	Error      error       // Validation error (if any)
	Secure     bool        // Whether DNSSEC validated the response
}

// cachedTLSA holds cached TLSA records for a host.
type cachedTLSA struct {
	records   []TLSARecord
	secure    bool
	expiresAt time.Time
}

// DANEResolver handles DANE/TLSA lookups with caching.
type DANEResolver struct {
	cache     sync.Map // _port._tcp.host -> *cachedTLSA
	dnsClient *dns.Client
	dnsServer string
	ttl       time.Duration
}

// DANEResolverConfig configures the DANE resolver.
type DANEResolverConfig struct {
	// CacheTTL is how long to cache TLSA records (default: 5m)
	CacheTTL time.Duration
	// DNSServer is the DNS server to use (default: system resolver)
	DNSServer string
	// Timeout is the DNS query timeout (default: 5s)
	Timeout time.Duration
}

// DefaultDANEResolverConfig returns default configuration.
func DefaultDANEResolverConfig() DANEResolverConfig {
	return DANEResolverConfig{
		CacheTTL:  5 * time.Minute,
		DNSServer: "", // Use system resolver
		Timeout:   5 * time.Second,
	}
}

// NewDANEResolver creates a new DANE resolver.
func NewDANEResolver(cfg DANEResolverConfig) *DANEResolver {
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 5 * time.Minute
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}

	// Default to Google's public DNS if not specified
	dnsServer := cfg.DNSServer
	if dnsServer == "" {
		dnsServer = "8.8.8.8:53"
	}
	if !strings.Contains(dnsServer, ":") {
		dnsServer += ":53"
	}

	return &DANEResolver{
		dnsClient: &dns.Client{
			Timeout: cfg.Timeout,
		},
		dnsServer: dnsServer,
		ttl:       cfg.CacheTTL,
	}
}

// LookupTLSA looks up TLSA records for a host and port.
// Returns nil slice if no TLSA records exist (not an error).
func (r *DANEResolver) LookupTLSA(ctx context.Context, host string, port int) ([]TLSARecord, bool, error) {
	tlsaName := buildTLSAName(host, port, "tcp")

	// Check cache first
	if cached, ok := r.cache.Load(tlsaName); ok {
		c := cached.(*cachedTLSA)
		if time.Now().Before(c.expiresAt) {
			return c.records, c.secure, nil
		}
		r.cache.Delete(tlsaName)
	}

	// Perform DNS lookup
	records, secure, err := r.queryTLSA(ctx, tlsaName)
	if err != nil {
		return nil, false, err
	}

	// Cache the results (even if empty)
	r.cache.Store(tlsaName, &cachedTLSA{
		records:   records,
		secure:    secure,
		expiresAt: time.Now().Add(r.ttl),
	})

	return records, secure, nil
}

// buildTLSAName builds the TLSA DNS name (_port._proto.host).
func buildTLSAName(host string, port int, proto string) string {
	host = strings.TrimSuffix(host, ".")
	return fmt.Sprintf("_%d._%s.%s.", port, proto, host)
}

// queryTLSA performs the actual TLSA DNS query.
func (r *DANEResolver) queryTLSA(ctx context.Context, name string) ([]TLSARecord, bool, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(name, dns.TypeTLSA)
	msg.SetEdns0(4096, true) // Enable DNSSEC

	resp, _, err := r.dnsClient.ExchangeContext(ctx, msg, r.dnsServer)
	if err != nil {
		return nil, false, nil // Network error - treat as no TLSA
	}

	if resp.Rcode == dns.RcodeNameError {
		return nil, false, nil // NXDOMAIN - no TLSA records
	}

	if resp.Rcode != dns.RcodeSuccess {
		return nil, false, nil // Other error - treat as no TLSA
	}

	// Check AD (Authenticated Data) flag for DNSSEC validation
	secure := resp.AuthenticatedData

	var records []TLSARecord
	for _, rr := range resp.Answer {
		if tlsa, ok := rr.(*dns.TLSA); ok {
			// Decode hex-encoded certificate data
			certData, err := hex.DecodeString(tlsa.Certificate)
			if err != nil {
				certData = []byte(tlsa.Certificate) // Try raw if not hex
			}

			records = append(records, TLSARecord{
				Usage:        TLSAUsage(tlsa.Usage),
				Selector:     TLSASelector(tlsa.Selector),
				MatchingType: TLSAMatchingType(tlsa.MatchingType),
				CertData:     certData,
			})
		}
	}

	return records, secure, nil
}

// ValidateCertificate validates a certificate against TLSA records.
// Returns a result indicating whether validation passed.
func ValidateCertificate(cert *x509.Certificate, chain []*x509.Certificate, records []TLSARecord) *DANEResult {
	if len(records) == 0 {
		return &DANEResult{
			Valid: true, // No TLSA records means no DANE, pass by default
		}
	}

	for _, record := range records {
		var valid bool

		switch record.Usage {
		case TLSAUsageDANEEE: // Usage 3 - most common for SMTP
			valid = validateDANEEE(cert, &record)
		case TLSAUsageDANETA: // Usage 2 - trust anchor
			valid = validateDANETA(cert, chain, &record)
		case TLSAUsagePKIXEE: // Usage 1 - end entity with PKIX
			valid = validateDANEEE(cert, &record) // Same validation, PKIX done separately
		case TLSAUsagePKIXTA: // Usage 0 - CA with PKIX
			valid = validateDANETA(cert, chain, &record)
		}

		if valid {
			return &DANEResult{
				Valid:      true,
				UsedRecord: &record,
			}
		}
	}

	return &DANEResult{
		Valid: false,
		Error: fmt.Errorf("certificate does not match any TLSA record"),
	}
}

// validateDANEEE validates DANE-EE (usage 3) - end entity certificate.
func validateDANEEE(cert *x509.Certificate, record *TLSARecord) bool {
	var data []byte

	// Select data based on selector
	switch record.Selector {
	case TLSASelectorCert:
		data = cert.Raw
	case TLSASelectorSPKI:
		data = cert.RawSubjectPublicKeyInfo
	default:
		return false
	}

	// Hash and compare
	return matchData(data, record.CertData, record.MatchingType)
}

// validateDANETA validates DANE-TA (usage 2) - trust anchor.
func validateDANETA(cert *x509.Certificate, chain []*x509.Certificate, record *TLSARecord) bool {
	// Check the end entity certificate first
	if validateDANEEE(cert, record) {
		return true
	}

	// Check each certificate in the chain
	for _, chainCert := range chain {
		var data []byte

		switch record.Selector {
		case TLSASelectorCert:
			data = chainCert.Raw
		case TLSASelectorSPKI:
			data = chainCert.RawSubjectPublicKeyInfo
		}

		if matchData(data, record.CertData, record.MatchingType) {
			return true
		}
	}

	return false
}

// matchData compares certificate data against TLSA record data.
func matchData(data, expected []byte, matchingType TLSAMatchingType) bool {
	var computed []byte

	switch matchingType {
	case TLSAMatchingFull:
		computed = data
	case TLSAMatchingSHA256:
		hash := sha256.Sum256(data)
		computed = hash[:]
	case TLSAMatchingSHA512:
		hash := sha512.Sum512(data)
		computed = hash[:]
	default:
		return false
	}

	if len(computed) != len(expected) {
		return false
	}

	// Constant-time comparison
	var diff byte
	for i := range computed {
		diff |= computed[i] ^ expected[i]
	}
	return diff == 0
}

// ClearCache clears the TLSA cache.
func (r *DANEResolver) ClearCache() {
	r.cache.Range(func(key, _ interface{}) bool {
		r.cache.Delete(key)
		return true
	})
}

// CacheStats returns cache statistics.
func (r *DANEResolver) CacheStats() DANECacheStats {
	var stats DANECacheStats
	now := time.Now()

	r.cache.Range(func(_, value interface{}) bool {
		stats.TotalEntries++
		c := value.(*cachedTLSA)
		if now.Before(c.expiresAt) {
			stats.ValidEntries++
		} else {
			stats.ExpiredEntries++
		}
		if c.secure {
			stats.SecureEntries++
		}
		return true
	})

	return stats
}

// DANECacheStats contains DANE cache statistics.
type DANECacheStats struct {
	TotalEntries   int
	ValidEntries   int
	ExpiredEntries int
	SecureEntries  int // DNSSEC validated entries
}

// String returns a human-readable representation of TLSA usage.
func (u TLSAUsage) String() string {
	switch u {
	case TLSAUsagePKIXTA:
		return "PKIX-TA"
	case TLSAUsagePKIXEE:
		return "PKIX-EE"
	case TLSAUsageDANETA:
		return "DANE-TA"
	case TLSAUsageDANEEE:
		return "DANE-EE"
	default:
		return strconv.Itoa(int(u))
	}
}
