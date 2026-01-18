// Package security provides email security features.
package security

import (
	"fmt"
	"strings"
)

// ResultValue represents an authentication result value.
type ResultValue string

// Standard result values per RFC 8601.
const (
	ResultNone      ResultValue = "none"
	ResultPass      ResultValue = "pass"
	ResultFail      ResultValue = "fail"
	ResultSoftfail  ResultValue = "softfail"
	ResultNeutral   ResultValue = "neutral"
	ResultTempError ResultValue = "temperror"
	ResultPermError ResultValue = "permerror"
	ResultPolicy    ResultValue = "policy"
)

// AuthenticationResults represents authentication results for a message.
type AuthenticationResults struct {
	AuthServID string      // Authentication service identifier (hostname)
	SPF        *SPFResult  // SPF check result
	DKIM       *DKIMResult // DKIM verification result
	DMARC      *DMARCResult // DMARC check result
	ARC        *ARCResult  // ARC chain validation result
}

// SPFResult holds SPF check results.
type SPFResult struct {
	Result   ResultValue
	Domain   string // SMTP MAIL FROM domain
	IP       string // Client IP address
	Reason   string // Optional reason
}

// DKIMResult holds DKIM verification results.
type DKIMResult struct {
	Result   ResultValue
	Domain   string // d= domain from signature
	Selector string // s= selector from signature
	Identity string // i= identity (if different from d=)
	Reason   string // Optional reason
}

// DMARCResult holds DMARC check results.
type DMARCResult struct {
	Result ResultValue
	Domain string // From header domain
	Policy string // Applied policy (none, quarantine, reject)
	Reason string // Optional reason
}

// ARCResult holds ARC chain validation results.
type ARCResult struct {
	Result   ResultValue
	Instance int    // Highest ARC instance validated
	Reason   string // Optional reason
}

// Format generates the Authentication-Results header value.
func (ar *AuthenticationResults) Format() string {
	var parts []string
	parts = append(parts, ar.AuthServID)

	if ar.SPF != nil {
		parts = append(parts, ar.formatSPF())
	}

	if ar.DKIM != nil {
		parts = append(parts, ar.formatDKIM())
	}

	if ar.DMARC != nil {
		parts = append(parts, ar.formatDMARC())
	}

	if ar.ARC != nil {
		parts = append(parts, ar.formatARC())
	}

	// If no results, add "none"
	if len(parts) == 1 {
		parts = append(parts, "none")
	}

	return strings.Join(parts, ";\r\n\t")
}

// formatSPF formats the SPF result.
func (ar *AuthenticationResults) formatSPF() string {
	result := fmt.Sprintf("spf=%s", ar.SPF.Result)

	if ar.SPF.Domain != "" {
		result += fmt.Sprintf(" smtp.mailfrom=%s", ar.SPF.Domain)
	}

	if ar.SPF.IP != "" {
		result += fmt.Sprintf(" smtp.client-ip=%s", ar.SPF.IP)
	}

	if ar.SPF.Reason != "" {
		result += fmt.Sprintf(" reason=\"%s\"", ar.SPF.Reason)
	}

	return result
}

// formatDKIM formats the DKIM result.
func (ar *AuthenticationResults) formatDKIM() string {
	result := fmt.Sprintf("dkim=%s", ar.DKIM.Result)

	if ar.DKIM.Domain != "" {
		result += fmt.Sprintf(" header.d=%s", ar.DKIM.Domain)
	}

	if ar.DKIM.Selector != "" {
		result += fmt.Sprintf(" header.s=%s", ar.DKIM.Selector)
	}

	if ar.DKIM.Identity != "" {
		result += fmt.Sprintf(" header.i=%s", ar.DKIM.Identity)
	}

	if ar.DKIM.Reason != "" {
		result += fmt.Sprintf(" reason=\"%s\"", ar.DKIM.Reason)
	}

	return result
}

// formatDMARC formats the DMARC result.
func (ar *AuthenticationResults) formatDMARC() string {
	result := fmt.Sprintf("dmarc=%s", ar.DMARC.Result)

	if ar.DMARC.Domain != "" {
		result += fmt.Sprintf(" header.from=%s", ar.DMARC.Domain)
	}

	if ar.DMARC.Policy != "" {
		result += fmt.Sprintf(" policy.applied=%s", ar.DMARC.Policy)
	}

	if ar.DMARC.Reason != "" {
		result += fmt.Sprintf(" reason=\"%s\"", ar.DMARC.Reason)
	}

	return result
}

// formatARC formats the ARC result.
func (ar *AuthenticationResults) formatARC() string {
	result := fmt.Sprintf("arc=%s", ar.ARC.Result)

	if ar.ARC.Instance > 0 {
		result += fmt.Sprintf(" header.i=%d", ar.ARC.Instance)
	}

	if ar.ARC.Reason != "" {
		result += fmt.Sprintf(" reason=\"%s\"", ar.ARC.Reason)
	}

	return result
}

// FormatARC generates the ARC-Authentication-Results header value.
// This is similar to Authentication-Results but for ARC sets.
func (ar *AuthenticationResults) FormatARC(instance int) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("i=%d", instance))
	parts = append(parts, ar.AuthServID)

	if ar.SPF != nil {
		parts = append(parts, ar.formatSPF())
	}

	if ar.DKIM != nil {
		parts = append(parts, ar.formatDKIM())
	}

	if ar.DMARC != nil {
		parts = append(parts, ar.formatDMARC())
	}

	if ar.ARC != nil {
		parts = append(parts, ar.formatARC())
	}

	// If no results, add "none"
	if len(parts) == 2 {
		parts = append(parts, "none")
	}

	return strings.Join(parts, ";\r\n\t")
}

// ParseAuthenticationResults parses an Authentication-Results header.
// This is a simplified parser that extracts key results.
func ParseAuthenticationResults(header string) (*AuthenticationResults, error) {
	ar := &AuthenticationResults{}

	// Split by semicolon
	parts := strings.Split(header, ";")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty header")
	}

	// First part is the authserv-id
	ar.AuthServID = strings.TrimSpace(parts[0])

	// Parse remaining parts
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Parse method=result
		if strings.HasPrefix(part, "spf=") {
			ar.SPF = parseSPFResult(part)
		} else if strings.HasPrefix(part, "dkim=") {
			ar.DKIM = parseDKIMResult(part)
		} else if strings.HasPrefix(part, "dmarc=") {
			ar.DMARC = parseDMARCResult(part)
		} else if strings.HasPrefix(part, "arc=") {
			ar.ARC = parseARCResult(part)
		}
	}

	return ar, nil
}

// parseSPFResult parses an SPF result string.
func parseSPFResult(s string) *SPFResult {
	result := &SPFResult{}

	// Extract result value
	if idx := strings.Index(s, "spf="); idx >= 0 {
		s = s[idx+4:]
		parts := strings.Fields(s)
		if len(parts) > 0 {
			result.Result = ResultValue(parts[0])
		}

		// Parse properties
		for _, prop := range parts[1:] {
			if strings.HasPrefix(prop, "smtp.mailfrom=") {
				result.Domain = strings.TrimPrefix(prop, "smtp.mailfrom=")
			} else if strings.HasPrefix(prop, "smtp.client-ip=") {
				result.IP = strings.TrimPrefix(prop, "smtp.client-ip=")
			}
		}
	}

	return result
}

// parseDKIMResult parses a DKIM result string.
func parseDKIMResult(s string) *DKIMResult {
	result := &DKIMResult{}

	if idx := strings.Index(s, "dkim="); idx >= 0 {
		s = s[idx+5:]
		parts := strings.Fields(s)
		if len(parts) > 0 {
			result.Result = ResultValue(parts[0])
		}

		for _, prop := range parts[1:] {
			if strings.HasPrefix(prop, "header.d=") {
				result.Domain = strings.TrimPrefix(prop, "header.d=")
			} else if strings.HasPrefix(prop, "header.s=") {
				result.Selector = strings.TrimPrefix(prop, "header.s=")
			} else if strings.HasPrefix(prop, "header.i=") {
				result.Identity = strings.TrimPrefix(prop, "header.i=")
			}
		}
	}

	return result
}

// parseDMARCResult parses a DMARC result string.
func parseDMARCResult(s string) *DMARCResult {
	result := &DMARCResult{}

	if idx := strings.Index(s, "dmarc="); idx >= 0 {
		s = s[idx+6:]
		parts := strings.Fields(s)
		if len(parts) > 0 {
			result.Result = ResultValue(parts[0])
		}

		for _, prop := range parts[1:] {
			if strings.HasPrefix(prop, "header.from=") {
				result.Domain = strings.TrimPrefix(prop, "header.from=")
			} else if strings.HasPrefix(prop, "policy.applied=") {
				result.Policy = strings.TrimPrefix(prop, "policy.applied=")
			}
		}
	}

	return result
}

// parseARCResult parses an ARC result string.
func parseARCResult(s string) *ARCResult {
	result := &ARCResult{}

	if idx := strings.Index(s, "arc="); idx >= 0 {
		s = s[idx+4:]
		parts := strings.Fields(s)
		if len(parts) > 0 {
			result.Result = ResultValue(parts[0])
		}

		for _, prop := range parts[1:] {
			if strings.HasPrefix(prop, "header.i=") {
				fmt.Sscanf(strings.TrimPrefix(prop, "header.i="), "%d", &result.Instance)
			}
		}
	}

	return result
}
