// Package delivery implements outbound email delivery.
package delivery

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// STSMode represents the MTA-STS policy mode.
type STSMode string

const (
	STSModeNone    STSMode = "none"
	STSModeTesting STSMode = "testing"
	STSModeEnforce STSMode = "enforce"
)

// STSPolicy represents a cached MTA-STS policy.
type STSPolicy struct {
	Version   string    // STSv1
	Mode      STSMode   // none, testing, enforce
	MXHosts   []string  // Allowed MX hostnames (may include wildcards like *.example.com)
	MaxAge    int       // Policy max age in seconds
	FetchedAt time.Time // When the policy was fetched
	ExpiresAt time.Time // When the policy expires
	ID        string    // Policy ID from DNS TXT record
}

// STSResolver fetches and caches MTA-STS policies.
type STSResolver struct {
	cache      sync.Map // domain -> *STSPolicy
	httpClient *http.Client
	resolver   *net.Resolver
}

// STSResolverConfig configures the MTA-STS resolver.
type STSResolverConfig struct {
	// HTTPTimeout is the timeout for HTTP requests (default: 10s)
	HTTPTimeout time.Duration
	// DNSTimeout is the timeout for DNS lookups (default: 5s)
	DNSTimeout time.Duration
}

// DefaultSTSResolverConfig returns default configuration.
func DefaultSTSResolverConfig() STSResolverConfig {
	return STSResolverConfig{
		HTTPTimeout: 10 * time.Second,
		DNSTimeout:  5 * time.Second,
	}
}

// NewSTSResolver creates a new MTA-STS resolver.
func NewSTSResolver(cfg STSResolverConfig) *STSResolver {
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}

	return &STSResolver{
		httpClient: &http.Client{
			Timeout: cfg.HTTPTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
				DialContext: (&net.Dialer{
					Timeout: cfg.DNSTimeout,
				}).DialContext,
			},
			// Don't follow redirects - MTA-STS spec requires exact URL
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		resolver: &net.Resolver{
			PreferGo: true,
		},
	}
}

// GetPolicy fetches and caches the MTA-STS policy for a domain.
// Returns nil if no valid policy exists (no error - policy is optional).
func (r *STSResolver) GetPolicy(ctx context.Context, domain string) (*STSPolicy, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil, nil
	}

	// Check cache first
	if cached, ok := r.cache.Load(domain); ok {
		policy := cached.(*STSPolicy)
		if time.Now().Before(policy.ExpiresAt) {
			return policy, nil
		}
		// Cache expired, delete it
		r.cache.Delete(domain)
	}

	// Check for MTA-STS DNS record first
	policyID, err := r.lookupSTSRecord(ctx, domain)
	if err != nil || policyID == "" {
		// No MTA-STS record - domain doesn't support MTA-STS
		return nil, nil
	}

	// Fetch the policy
	policy, err := r.fetchPolicy(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch MTA-STS policy: %w", err)
	}

	policy.ID = policyID
	policy.FetchedAt = time.Now()
	policy.ExpiresAt = policy.FetchedAt.Add(time.Duration(policy.MaxAge) * time.Second)

	// Cache the policy
	r.cache.Store(domain, policy)

	return policy, nil
}

// lookupSTSRecord checks for the _mta-sts DNS TXT record.
// Returns the policy ID (v=STSv1; id=...) or empty string if not found.
func (r *STSResolver) lookupSTSRecord(ctx context.Context, domain string) (string, error) {
	txtRecords, err := r.resolver.LookupTXT(ctx, "_mta-sts."+domain)
	if err != nil {
		return "", nil // No record is not an error
	}

	for _, txt := range txtRecords {
		// MTA-STS record format: v=STSv1; id=20190425T135500
		txt = strings.TrimSpace(txt)
		if !strings.HasPrefix(txt, "v=STSv1") {
			continue
		}

		// Parse the id
		parts := strings.Split(txt, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "id=") {
				return strings.TrimPrefix(part, "id="), nil
			}
		}
	}

	return "", nil
}

// fetchPolicy fetches the MTA-STS policy from the well-known URL.
func (r *STSResolver) fetchPolicy(ctx context.Context, domain string) (*STSPolicy, error) {
	url := fmt.Sprintf("https://mta-sts.%s/.well-known/mta-sts.txt", domain)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/plain") {
		return nil, fmt.Errorf("unexpected content type: %s", contentType)
	}

	return r.parsePolicy(resp.Body)
}

// parsePolicy parses the MTA-STS policy text.
func (r *STSResolver) parsePolicy(body interface{ Read([]byte) (int, error) }) (*STSPolicy, error) {
	policy := &STSPolicy{
		MXHosts: make([]string, 0),
	}

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key: value
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "version":
			policy.Version = value
		case "mode":
			switch value {
			case "enforce":
				policy.Mode = STSModeEnforce
			case "testing":
				policy.Mode = STSModeTesting
			case "none":
				policy.Mode = STSModeNone
			default:
				return nil, fmt.Errorf("invalid mode: %s", value)
			}
		case "mx":
			policy.MXHosts = append(policy.MXHosts, value)
		case "max_age":
			var maxAge int
			if _, err := fmt.Sscanf(value, "%d", &maxAge); err != nil {
				return nil, fmt.Errorf("invalid max_age: %s", value)
			}
			policy.MaxAge = maxAge
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Validate required fields
	if policy.Version != "STSv1" {
		return nil, fmt.Errorf("invalid or missing version: %s", policy.Version)
	}
	if policy.Mode == "" {
		return nil, fmt.Errorf("missing mode")
	}
	if len(policy.MXHosts) == 0 {
		return nil, fmt.Errorf("no MX hosts specified")
	}
	if policy.MaxAge <= 0 {
		return nil, fmt.Errorf("invalid or missing max_age")
	}

	return policy, nil
}

// ValidateMX checks if an MX hostname is allowed by the policy.
// Supports wildcard matching (e.g., *.example.com matches mail.example.com).
func (p *STSPolicy) ValidateMX(mxHost string) bool {
	mxHost = strings.ToLower(strings.TrimSuffix(mxHost, "."))

	for _, allowed := range p.MXHosts {
		allowed = strings.ToLower(strings.TrimSpace(allowed))

		// Exact match
		if allowed == mxHost {
			return true
		}

		// Wildcard match (*.example.com)
		if strings.HasPrefix(allowed, "*.") {
			suffix := allowed[1:] // Remove the * but keep the .
			if strings.HasSuffix(mxHost, suffix) {
				// Make sure there's only one level of subdomain
				prefix := strings.TrimSuffix(mxHost, suffix)
				if !strings.Contains(prefix, ".") {
					return true
				}
			}
		}
	}

	return false
}

// ShouldEnforceTLS returns whether TLS must be enforced for this domain.
func (p *STSPolicy) ShouldEnforceTLS() bool {
	return p.Mode == STSModeEnforce
}

// ShouldReportFailure returns whether TLS failures should be reported.
func (p *STSPolicy) ShouldReportFailure() bool {
	return p.Mode == STSModeEnforce || p.Mode == STSModeTesting
}

// ClearCache clears the policy cache.
func (r *STSResolver) ClearCache() {
	r.cache.Range(func(key, _ interface{}) bool {
		r.cache.Delete(key)
		return true
	})
}

// CacheStats returns cache statistics.
func (r *STSResolver) CacheStats() STSCacheStats {
	var stats STSCacheStats
	now := time.Now()

	r.cache.Range(func(_, value interface{}) bool {
		stats.TotalEntries++
		policy := value.(*STSPolicy)
		if now.Before(policy.ExpiresAt) {
			stats.ValidEntries++
		} else {
			stats.ExpiredEntries++
		}
		return true
	})

	return stats
}

// STSCacheStats contains MTA-STS cache statistics.
type STSCacheStats struct {
	TotalEntries   int
	ValidEntries   int
	ExpiredEntries int
}
