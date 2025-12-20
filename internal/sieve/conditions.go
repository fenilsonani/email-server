package sieve

import (
	"regexp"
	"strings"
)

// Condition is an interface for Sieve test conditions
type Condition interface {
	Evaluate(msg *Message) bool
}

// TrueCondition always matches
type TrueCondition struct{}

func (c *TrueCondition) Evaluate(msg *Message) bool {
	return true
}

// FalseCondition never matches
type FalseCondition struct{}

func (c *FalseCondition) Evaluate(msg *Message) bool {
	return false
}

// NotCondition negates another condition
type NotCondition struct {
	Condition Condition
}

func (c *NotCondition) Evaluate(msg *Message) bool {
	if c.Condition == nil {
		return true
	}
	return !c.Condition.Evaluate(msg)
}

// HeaderCondition matches message headers
type HeaderCondition struct {
	Headers     []string // Header names to check
	Values      []string // Values to match against
	MatchType   string   // "is", "contains", "matches"
	Comparator  string   // Comparison type (default i;ascii-casemap)
	IsAddress   bool     // true if this is an address test
	AddressPart string   // "localpart", "domain", "all" for address tests
}

func (c *HeaderCondition) Evaluate(msg *Message) bool {
	for _, headerName := range c.Headers {
		headerValues := c.getHeaderValues(msg, headerName)

		for _, headerValue := range headerValues {
			for _, testValue := range c.Values {
				if c.match(headerValue, testValue) {
					return true
				}
			}
		}
	}
	return false
}

func (c *HeaderCondition) getHeaderValues(msg *Message, headerName string) []string {
	normalizedName := strings.ToLower(headerName)

	// Check common headers first
	switch normalizedName {
	case "from":
		if c.IsAddress {
			return []string{extractAddressPart(msg.From, c.AddressPart)}
		}
		return []string{msg.From}
	case "to":
		if c.IsAddress {
			var parts []string
			for _, to := range msg.To {
				parts = append(parts, extractAddressPart(to, c.AddressPart))
			}
			return parts
		}
		return msg.To
	case "subject":
		return []string{msg.Subject}
	}

	// Check Headers map
	if vals, ok := msg.Headers[headerName]; ok {
		if c.IsAddress {
			var parts []string
			for _, v := range vals {
				parts = append(parts, extractAddressPart(v, c.AddressPart))
			}
			return parts
		}
		return vals
	}

	// Try case-insensitive lookup
	for k, vals := range msg.Headers {
		if strings.EqualFold(k, headerName) {
			if c.IsAddress {
				var parts []string
				for _, v := range vals {
					parts = append(parts, extractAddressPart(v, c.AddressPart))
				}
				return parts
			}
			return vals
		}
	}

	return nil
}

func (c *HeaderCondition) match(value, pattern string) bool {
	// Case-insensitive by default
	value = strings.ToLower(value)
	pattern = strings.ToLower(pattern)

	switch c.MatchType {
	case "is":
		return value == pattern
	case "contains":
		return strings.Contains(value, pattern)
	case "matches":
		// Convert Sieve glob pattern to regex
		re := globToRegex(pattern)
		matched, _ := regexp.MatchString(re, value)
		return matched
	default:
		return value == pattern
	}
}

// extractAddressPart extracts localpart, domain, or full address
func extractAddressPart(addr, part string) string {
	// Handle "Name <email@domain>" format
	if idx := strings.Index(addr, "<"); idx >= 0 {
		end := strings.Index(addr, ">")
		if end > idx {
			addr = addr[idx+1 : end]
		}
	}
	addr = strings.TrimSpace(addr)

	at := strings.LastIndex(addr, "@")
	if at < 0 {
		return addr
	}

	switch part {
	case "localpart":
		return addr[:at]
	case "domain":
		return addr[at+1:]
	default:
		return addr
	}
}

// globToRegex converts Sieve glob patterns to regex
func globToRegex(pattern string) string {
	// Escape regex special chars except * and ?
	result := regexp.QuoteMeta(pattern)
	// Convert * to .* and ? to .
	result = strings.ReplaceAll(result, `\*`, `.*`)
	result = strings.ReplaceAll(result, `\?`, `.`)
	return "^" + result + "$"
}

// SizeCondition matches message size
type SizeCondition struct {
	Size int64 // Size in bytes
	Over bool  // true for :over, false for :under
}

func (c *SizeCondition) Evaluate(msg *Message) bool {
	if c.Over {
		return msg.Size > c.Size
	}
	return msg.Size < c.Size
}

// ExistsCondition checks if headers exist
type ExistsCondition struct {
	Headers []string
}

func (c *ExistsCondition) Evaluate(msg *Message) bool {
	for _, headerName := range c.Headers {
		found := false

		// Check common headers
		switch strings.ToLower(headerName) {
		case "from":
			found = msg.From != ""
		case "to":
			found = len(msg.To) > 0
		case "subject":
			found = msg.Subject != ""
		default:
			// Check Headers map
			if _, ok := msg.Headers[headerName]; ok {
				found = true
			} else {
				// Case-insensitive check
				for k := range msg.Headers {
					if strings.EqualFold(k, headerName) {
						found = true
						break
					}
				}
			}
		}

		if !found {
			return false
		}
	}
	return true
}

// FromCondition is a shorthand for header :from
type FromCondition struct {
	Values    []string
	MatchType string
}

func (c *FromCondition) Evaluate(msg *Message) bool {
	hc := &HeaderCondition{
		Headers:   []string{"from"},
		Values:    c.Values,
		MatchType: c.MatchType,
		IsAddress: true,
	}
	return hc.Evaluate(msg)
}

// ToCondition is a shorthand for header :to
type ToCondition struct {
	Values    []string
	MatchType string
}

func (c *ToCondition) Evaluate(msg *Message) bool {
	hc := &HeaderCondition{
		Headers:   []string{"to"},
		Values:    c.Values,
		MatchType: c.MatchType,
		IsAddress: true,
	}
	return hc.Evaluate(msg)
}

// SubjectCondition is a shorthand for header :subject
type SubjectCondition struct {
	Values    []string
	MatchType string
}

func (c *SubjectCondition) Evaluate(msg *Message) bool {
	hc := &HeaderCondition{
		Headers:   []string{"subject"},
		Values:    c.Values,
		MatchType: c.MatchType,
	}
	return hc.Evaluate(msg)
}
