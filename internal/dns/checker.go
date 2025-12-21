package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

// CheckResult represents the result of a DNS check
type CheckResult struct {
	RecordType string
	Status     Status
	Expected   string
	Actual     string
	Message    string
}

// Status represents the check status
type Status string

const (
	StatusPass    Status = "PASS"
	StatusFail    Status = "FAIL"
	StatusWarning Status = "WARN"
	StatusMissing Status = "MISSING"
)

// Checker performs DNS checks for email configuration
type Checker struct {
	domain     string
	mailServer string
	resolver   *net.Resolver
}

var (
	// ErrInvalidDomain is returned when domain validation fails
	ErrInvalidDomain = errors.New("invalid domain name")
	// ErrInvalidMailServer is returned when mail server validation fails
	ErrInvalidMailServer = errors.New("invalid mail server name")
	// domainRegex validates domain names (RFC 1035)
	domainRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`)
)

// NewChecker creates a new DNS checker for the given domain
func NewChecker(domain, mailServer string) (*Checker, error) {
	// Validate inputs
	if domain == "" {
		return nil, fmt.Errorf("%w: domain cannot be empty", ErrInvalidDomain)
	}
	if mailServer == "" {
		return nil, fmt.Errorf("%w: mail server cannot be empty", ErrInvalidMailServer)
	}

	// Validate domain format
	if !domainRegex.MatchString(domain) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDomain, domain)
	}

	// Validate mail server format
	if !domainRegex.MatchString(mailServer) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMailServer, mailServer)
	}

	return &Checker{
		domain:     domain,
		mailServer: mailServer,
		resolver: &net.Resolver{
			PreferGo: true,
		},
	}, nil
}

// CheckAll runs all DNS checks
func (c *Checker) CheckAll(ctx context.Context) []CheckResult {
	var results []CheckResult

	results = append(results, c.CheckMX(ctx))
	results = append(results, c.CheckSPF(ctx))
	results = append(results, c.CheckDKIM(ctx))
	results = append(results, c.CheckDMARC(ctx))
	results = append(results, c.CheckPTR(ctx))

	return results
}

// CheckMX checks MX records
func (c *Checker) CheckMX(ctx context.Context) CheckResult {
	// Check parent context first
	if err := ctx.Err(); err != nil {
		return CheckResult{
			RecordType: "MX",
			Status:     StatusFail,
			Message:    fmt.Sprintf("Context error: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	records, err := c.resolver.LookupMX(ctx, c.domain)
	if err != nil {
		// Differentiate between timeout and DNS errors
		if ctx.Err() == context.DeadlineExceeded {
			return CheckResult{
				RecordType: "MX",
				Status:     StatusFail,
				Expected:   c.mailServer,
				Message:    "DNS lookup timeout",
			}
		}
		// Check for DNS-specific errors
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			if dnsErr.IsNotFound {
				return CheckResult{
					RecordType: "MX",
					Status:     StatusMissing,
					Expected:   c.mailServer,
					Message:    "No MX records found",
				}
			}
			return CheckResult{
				RecordType: "MX",
				Status:     StatusFail,
				Expected:   c.mailServer,
				Message:    fmt.Sprintf("DNS error: %v", dnsErr),
			}
		}
		return CheckResult{
			RecordType: "MX",
			Status:     StatusFail,
			Expected:   c.mailServer,
			Message:    fmt.Sprintf("Lookup failed: %v", err),
		}
	}

	for _, mx := range records {
		host := strings.TrimSuffix(mx.Host, ".")
		if strings.EqualFold(host, c.mailServer) {
			return CheckResult{
				RecordType: "MX",
				Status:     StatusPass,
				Expected:   c.mailServer,
				Actual:     host,
				Message:    fmt.Sprintf("MX record points to %s with priority %d", host, mx.Pref),
			}
		}
	}

	// MX exists but doesn't match expected
	var mxHosts []string
	for _, mx := range records {
		mxHosts = append(mxHosts, strings.TrimSuffix(mx.Host, "."))
	}

	return CheckResult{
		RecordType: "MX",
		Status:     StatusWarning,
		Expected:   c.mailServer,
		Actual:     strings.Join(mxHosts, ", "),
		Message:    "MX record found but doesn't match expected mail server",
	}
}

// CheckSPF checks SPF record
func (c *Checker) CheckSPF(ctx context.Context) CheckResult {
	// Check parent context first
	if err := ctx.Err(); err != nil {
		return CheckResult{
			RecordType: "SPF",
			Status:     StatusFail,
			Message:    fmt.Sprintf("Context error: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	records, err := c.resolver.LookupTXT(ctx, c.domain)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return CheckResult{
				RecordType: "SPF",
				Status:     StatusFail,
				Message:    "DNS lookup timeout",
			}
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return CheckResult{
				RecordType: "SPF",
				Status:     StatusMissing,
				Message:    "No TXT records found",
			}
		}
		return CheckResult{
			RecordType: "SPF",
			Status:     StatusFail,
			Message:    fmt.Sprintf("Failed to lookup TXT records: %v", err),
		}
	}

	for _, record := range records {
		if strings.HasPrefix(record, "v=spf1") {
			// Check if it includes our mail server
			if strings.Contains(record, "mx") ||
				strings.Contains(record, c.mailServer) ||
				strings.Contains(record, "a:"+c.mailServer) {
				return CheckResult{
					RecordType: "SPF",
					Status:     StatusPass,
					Actual:     record,
					Message:    "SPF record found and includes mail server",
				}
			}
			return CheckResult{
				RecordType: "SPF",
				Status:     StatusWarning,
				Actual:     record,
				Message:    "SPF record found but may not authorize mail server",
			}
		}
	}

	return CheckResult{
		RecordType: "SPF",
		Status:     StatusMissing,
		Message:    "No SPF record found",
	}
}

// CheckDKIM checks DKIM record
func (c *Checker) CheckDKIM(ctx context.Context) CheckResult {
	// Check parent context first
	if err := ctx.Err(); err != nil {
		return CheckResult{
			RecordType: "DKIM",
			Status:     StatusFail,
			Message:    fmt.Sprintf("Context error: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Check for common DKIM selector names
	selectors := []string{"mail", "default", "selector1", "dkim"}

	for _, selector := range selectors {
		// Check context on each iteration
		if ctx.Err() != nil {
			return CheckResult{
				RecordType: "DKIM",
				Status:     StatusFail,
				Message:    "DNS lookup timeout",
			}
		}

		dkimHost := fmt.Sprintf("%s._domainkey.%s", selector, c.domain)
		records, err := c.resolver.LookupTXT(ctx, dkimHost)
		if err != nil {
			// Only continue on NotFound errors, fail on others
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				continue
			}
			// For other errors (timeout, network), return failure
			if ctx.Err() == context.DeadlineExceeded {
				return CheckResult{
					RecordType: "DKIM",
					Status:     StatusFail,
					Message:    "DNS lookup timeout",
				}
			}
			continue // Continue checking other selectors
		}

		for _, record := range records {
			if strings.Contains(record, "v=DKIM1") || strings.Contains(record, "k=rsa") {
				return CheckResult{
					RecordType: "DKIM",
					Status:     StatusPass,
					Actual:     fmt.Sprintf("%s: %s...", selector, truncate(record, 50)),
					Message:    fmt.Sprintf("DKIM record found for selector '%s'", selector),
				}
			}
		}
	}

	return CheckResult{
		RecordType: "DKIM",
		Status:     StatusMissing,
		Message:    "No DKIM record found (checked: mail, default, selector1, dkim)",
	}
}

// CheckDMARC checks DMARC record
func (c *Checker) CheckDMARC(ctx context.Context) CheckResult {
	// Check parent context first
	if err := ctx.Err(); err != nil {
		return CheckResult{
			RecordType: "DMARC",
			Status:     StatusFail,
			Message:    fmt.Sprintf("Context error: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	dmarcHost := fmt.Sprintf("_dmarc.%s", c.domain)
	records, err := c.resolver.LookupTXT(ctx, dmarcHost)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return CheckResult{
				RecordType: "DMARC",
				Status:     StatusFail,
				Message:    "DNS lookup timeout",
			}
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return CheckResult{
				RecordType: "DMARC",
				Status:     StatusMissing,
				Message:    "No DMARC record found",
			}
		}
		return CheckResult{
			RecordType: "DMARC",
			Status:     StatusFail,
			Message:    fmt.Sprintf("DNS lookup failed: %v", err),
		}
	}

	for _, record := range records {
		if strings.HasPrefix(record, "v=DMARC1") {
			// Parse policy
			policy := "none"
			if strings.Contains(record, "p=reject") {
				policy = "reject"
			} else if strings.Contains(record, "p=quarantine") {
				policy = "quarantine"
			}

			status := StatusPass
			if policy == "none" {
				status = StatusWarning
			}

			return CheckResult{
				RecordType: "DMARC",
				Status:     status,
				Actual:     record,
				Message:    fmt.Sprintf("DMARC record found with policy: %s", policy),
			}
		}
	}

	return CheckResult{
		RecordType: "DMARC",
		Status:     StatusMissing,
		Message:    "No DMARC record found",
	}
}

// CheckPTR checks reverse DNS for mail server
func (c *Checker) CheckPTR(ctx context.Context) CheckResult {
	// Check parent context first
	if err := ctx.Err(); err != nil {
		return CheckResult{
			RecordType: "PTR",
			Status:     StatusFail,
			Message:    fmt.Sprintf("Context error: %v", err),
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// First, resolve the mail server to IP
	ips, err := c.resolver.LookupIPAddr(ctx, c.mailServer)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return CheckResult{
				RecordType: "PTR",
				Status:     StatusFail,
				Message:    "DNS lookup timeout",
			}
		}
		return CheckResult{
			RecordType: "PTR",
			Status:     StatusFail,
			Message:    fmt.Sprintf("Failed to resolve mail server %s: %v", c.mailServer, err),
		}
	}

	if len(ips) == 0 {
		return CheckResult{
			RecordType: "PTR",
			Status:     StatusFail,
			Message:    "Mail server has no A/AAAA record",
		}
	}

	// Check reverse DNS for first IP
	ip := ips[0].IP.String()
	names, err := c.resolver.LookupAddr(ctx, ip)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return CheckResult{
				RecordType: "PTR",
				Status:     StatusFail,
				Expected:   c.mailServer,
				Actual:     ip,
				Message:    "DNS lookup timeout",
			}
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return CheckResult{
				RecordType: "PTR",
				Status:     StatusWarning,
				Expected:   c.mailServer,
				Actual:     ip,
				Message:    "No PTR record found for mail server IP",
			}
		}
		return CheckResult{
			RecordType: "PTR",
			Status:     StatusFail,
			Expected:   c.mailServer,
			Actual:     ip,
			Message:    fmt.Sprintf("PTR lookup failed: %v", err),
		}
	}

	for _, name := range names {
		cleanName := strings.TrimSuffix(name, ".")
		if strings.EqualFold(cleanName, c.mailServer) {
			return CheckResult{
				RecordType: "PTR",
				Status:     StatusPass,
				Expected:   c.mailServer,
				Actual:     cleanName,
				Message:    fmt.Sprintf("PTR record for %s correctly points to %s", ip, cleanName),
			}
		}
	}

	return CheckResult{
		RecordType: "PTR",
		Status:     StatusWarning,
		Expected:   c.mailServer,
		Actual:     strings.Join(names, ", "),
		Message:    "PTR record exists but doesn't match mail server hostname",
	}
}

// truncate truncates a string to the given length
func truncate(s string, length int) string {
	if len(s) <= length {
		return s
	}
	return s[:length] + "..."
}
