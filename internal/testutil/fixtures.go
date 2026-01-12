// Package testutil provides test utilities and helpers.
package testutil

import (
	"strings"
)

// Test email fixtures

// MinimalEmail is the smallest valid email.
const MinimalEmail = `From: test@example.com
To: user@example.com
Subject: Test

Body
`

// EmailWithLongLine creates an email with a line exceeding the limit.
func EmailWithLongLine(lineLength int) string {
	longLine := strings.Repeat("a", lineLength)
	return `From: test@example.com
To: user@example.com
Subject: ` + longLine + `

Body
`
}

// EmailWithNestedMIME creates a deeply nested MIME structure.
func EmailWithNestedMIME(depth int) string {
	var sb strings.Builder
	sb.WriteString(`From: test@example.com
To: user@example.com
Subject: Nested MIME Test
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="boundary0"

`)
	for i := 0; i < depth; i++ {
		sb.WriteString("--boundary")
		sb.WriteString(string(rune('0' + (i % 10))))
		sb.WriteString("\r\nContent-Type: multipart/mixed; boundary=\"boundary")
		sb.WriteString(string(rune('0' + ((i + 1) % 10))))
		sb.WriteString("\"\r\n\r\n")
	}
	sb.WriteString("--boundary")
	sb.WriteString(string(rune('0' + (depth % 10))))
	sb.WriteString("\r\nContent-Type: text/plain\r\n\r\nInner content\r\n")
	for i := depth; i >= 0; i-- {
		sb.WriteString("--boundary")
		sb.WriteString(string(rune('0' + (i % 10))))
		sb.WriteString("--\r\n")
	}
	return sb.String()
}

// EmailWithMalformedHeaders creates an email with invalid headers.
func EmailWithMalformedHeaders(headerType string) string {
	switch headerType {
	case "missing_from":
		return `To: user@example.com
Subject: No From Header

Body
`
	case "missing_to":
		return `From: test@example.com
Subject: No To Header

Body
`
	case "binary_in_subject":
		return "From: test@example.com\r\nTo: user@example.com\r\nSubject: Test\x00\x01\x02\r\n\r\nBody\r\n"
	case "invalid_date":
		return `From: test@example.com
To: user@example.com
Subject: Test
Date: not-a-valid-date

Body
`
	case "duplicate_from":
		return `From: test1@example.com
From: test2@example.com
To: user@example.com
Subject: Duplicate From

Body
`
	default:
		return MinimalEmail
	}
}

// LargeEmail creates an email of approximately the specified size in bytes.
func LargeEmail(sizeBytes int) string {
	header := `From: test@example.com
To: user@example.com
Subject: Large Email Test
MIME-Version: 1.0
Content-Type: text/plain

`
	headerLen := len(header)
	bodyLen := sizeBytes - headerLen
	if bodyLen < 0 {
		bodyLen = 0
	}
	return header + strings.Repeat("X", bodyLen)
}

// UnicodeEmail creates an email with Unicode content.
const UnicodeEmail = `From: test@example.com
To: user@example.com
Subject: Unicode Test: こんにちは 你好 مرحبا 🎉
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8

Hello in multiple languages:
Japanese: こんにちは
Chinese: 你好
Arabic: مرحبا
Emoji: 🎉🚀💻
Russian: Привет
Greek: Γεια σου
`

// SQL injection test strings
var SQLInjectionStrings = []string{
	"'; DROP TABLE users; --",
	"' OR '1'='1",
	"1; SELECT * FROM users",
	"admin'--",
	"' UNION SELECT password FROM users WHERE '1'='1",
	"1' AND '1'='1",
	"') OR ('1'='1",
	"'; EXEC xp_cmdshell('dir'); --",
	"' OR 1=1#",
	"admin' /*",
}

// Path traversal test strings
var PathTraversalStrings = []string{
	"../../../etc/passwd",
	"..\\..\\..\\windows\\system32\\config\\sam",
	"....//....//....//etc/passwd",
	"%2e%2e%2f%2e%2e%2f%2e%2e%2fetc/passwd",
	"..%252f..%252f..%252fetc/passwd",
	"/etc/passwd",
	"./../../etc/passwd",
	"file:///etc/passwd",
	"..\\..\\..\\..\\..\\..\\etc\\passwd",
	"....\\....\\....\\etc\\passwd",
}

// Null byte injection test strings
var NullByteStrings = []string{
	"test\x00.txt",
	"user\x00admin",
	"file.txt\x00.jpg",
	"\x00admin",
	"test\x00",
}

// Unicode usernames for testing
var UnicodeUsernames = []string{
	"用户@example.com",
	"пользователь@example.com",
	"مستخدم@example.com",
	"ユーザー@example.com",
	"🎉user@example.com",
	"tëst@example.com",
	"naïve@example.com",
}

// Very long strings for testing limits
func VeryLongString(length int) string {
	return strings.Repeat("a", length)
}

// VeryLongPassword creates a password of specified length.
func VeryLongPassword(length int) string {
	return strings.Repeat("P@ssw0rd!", length/9+1)[:length]
}

// MXRecordScenarios for delivery testing
type MXRecordScenario struct {
	Domain   string
	MXHosts  []string
	Expected string
}

var MXRecordScenarios = []MXRecordScenario{
	{
		Domain:   "nomx.example.com",
		MXHosts:  nil,
		Expected: "no_mx",
	},
	{
		Domain:   "singlemx.example.com",
		MXHosts:  []string{"mx1.example.com"},
		Expected: "single_mx",
	},
	{
		Domain:   "multiplemx.example.com",
		MXHosts:  []string{"mx1.example.com", "mx2.example.com", "mx3.example.com"},
		Expected: "multiple_mx",
	},
}

// SMTP response codes for testing
type SMTPResponse struct {
	Code    int
	Message string
}

var SMTPResponses = map[string]SMTPResponse{
	"ok":              {250, "OK"},
	"greylist":        {451, "Greylisted, try again later"},
	"temp_fail":       {421, "Service temporarily unavailable"},
	"perm_fail":       {550, "Mailbox not found"},
	"quota_exceeded":  {552, "Quota exceeded"},
	"relay_denied":    {554, "Relay access denied"},
	"rate_limited":    {450, "Rate limit exceeded"},
	"auth_required":   {530, "Authentication required"},
	"invalid_address": {501, "Invalid address"},
}

// RateLimitScenario for testing rate limiting
type RateLimitScenario struct {
	Name         string
	Requests     int
	Window       int // seconds
	ExpectBlock  bool
	Description  string
}

var RateLimitScenarios = []RateLimitScenario{
	{
		Name:        "under_limit",
		Requests:    3,
		Window:      60,
		ExpectBlock: false,
		Description: "Requests under the limit should pass",
	},
	{
		Name:        "at_limit",
		Requests:    5,
		Window:      60,
		ExpectBlock: false,
		Description: "Requests at exactly the limit should pass",
	},
	{
		Name:        "over_limit",
		Requests:    6,
		Window:      60,
		ExpectBlock: true,
		Description: "Requests over the limit should be blocked",
	},
}
