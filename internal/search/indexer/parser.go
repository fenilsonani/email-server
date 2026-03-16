package indexer

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/search"
	"github.com/microcosm-cc/bluemonday"
)

// EmailParser extracts searchable content from email messages.
type EmailParser struct {
	// htmlPolicy is used to strip HTML tags
	htmlPolicy *bluemonday.Policy
}

// NewEmailParser creates a new email parser.
func NewEmailParser() *EmailParser {
	// Create a policy that strips all HTML but keeps text content
	policy := bluemonday.StrictPolicy()
	return &EmailParser{
		htmlPolicy: policy,
	}
}

// ParseMessage parses an email message and extracts searchable content.
func (p *EmailParser) ParseMessage(r io.Reader, mailboxID int64, uid uint32, userID int64) (*search.EmailDocument, error) {
	// Read the message
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return nil, err
	}

	doc := &search.EmailDocument{
		ID:        search.FormatDocumentID(mailboxID, uid),
		UserID:    userID,
		MailboxID: mailboxID,
		UID:       uid,
	}

	// Extract headers
	doc.Subject = decodeHeader(msg.Header.Get("Subject"))
	doc.From = decodeHeader(msg.Header.Get("From"))
	doc.MessageID = msg.Header.Get("Message-ID")

	// Parse To addresses
	if toHeader := msg.Header.Get("To"); toHeader != "" {
		doc.To = parseAddressList(toHeader)
	}

	// Parse Cc addresses
	if ccHeader := msg.Header.Get("Cc"); ccHeader != "" {
		doc.Cc = parseAddressList(ccHeader)
	}

	// Parse date
	if dateStr := msg.Header.Get("Date"); dateStr != "" {
		if t, err := mail.ParseDate(dateStr); err == nil {
			doc.Date = t
		}
	}
	if doc.Date.IsZero() {
		doc.Date = time.Now()
	}

	// Extract body content
	bodyText, bodyHTML, err := p.extractBody(msg)
	if err != nil {
		// Non-fatal - we can still index headers
		bodyText = ""
		bodyHTML = ""
	}

	doc.BodyText = bodyText
	doc.BodyHTML = bodyHTML

	return doc, nil
}

// extractBody extracts plain text and HTML content from the message body.
func (p *EmailParser) extractBody(msg *mail.Message) (plainText, htmlText string, err error) {
	contentType := msg.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// Assume plain text if we can't parse content type
		body, err := io.ReadAll(io.LimitReader(msg.Body, maxBodySize))
		if err != nil {
			return "", "", err
		}
		return string(body), "", nil
	}

	switch {
	case strings.HasPrefix(mediaType, "text/plain"):
		body, err := io.ReadAll(io.LimitReader(msg.Body, maxBodySize))
		if err != nil {
			return "", "", err
		}
		return string(body), "", nil

	case strings.HasPrefix(mediaType, "text/html"):
		body, err := io.ReadAll(io.LimitReader(msg.Body, maxBodySize))
		if err != nil {
			return "", "", err
		}
		stripped := p.stripHTML(string(body))
		return "", stripped, nil

	case strings.HasPrefix(mediaType, "multipart/"):
		return p.parseMultipart(msg.Body, params["boundary"])

	default:
		// For other content types, try to read as text
		body, err := io.ReadAll(io.LimitReader(msg.Body, maxBodySize))
		if err != nil {
			return "", "", err
		}
		if isPrintable(body) {
			return string(body), "", nil
		}
		return "", "", nil
	}
}

// parseMultipart recursively parses multipart message content.
func (p *EmailParser) parseMultipart(r io.Reader, boundary string) (plainText, htmlText string, err error) {
	if boundary == "" {
		return "", "", nil
	}

	mr := multipart.NewReader(r, boundary)
	var plainParts []string
	var htmlParts []string

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		contentType := part.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "text/plain"
		}

		mediaType, params, _ := mime.ParseMediaType(contentType)

		switch {
		case strings.HasPrefix(mediaType, "text/plain"):
			body, err := io.ReadAll(io.LimitReader(part, maxBodySize))
			if err == nil {
				plainParts = append(plainParts, string(body))
			}

		case strings.HasPrefix(mediaType, "text/html"):
			body, err := io.ReadAll(io.LimitReader(part, maxBodySize))
			if err == nil {
				stripped := p.stripHTML(string(body))
				htmlParts = append(htmlParts, stripped)
			}

		case strings.HasPrefix(mediaType, "multipart/"):
			// Recursive multipart
			nestedPlain, nestedHTML, _ := p.parseMultipart(part, params["boundary"])
			if nestedPlain != "" {
				plainParts = append(plainParts, nestedPlain)
			}
			if nestedHTML != "" {
				htmlParts = append(htmlParts, nestedHTML)
			}
		}

		part.Close()
	}

	return strings.Join(plainParts, "\n"), strings.Join(htmlParts, "\n"), nil
}

// stripHTML removes HTML tags and returns plain text.
func (p *EmailParser) stripHTML(html string) string {
	// Use bluemonday to strip HTML
	text := p.htmlPolicy.Sanitize(html)

	// Decode HTML entities
	text = decodeHTMLEntities(text)

	// Normalize whitespace
	text = normalizeWhitespace(text)

	return text
}

// decodeHeader decodes RFC 2047 encoded headers.
func decodeHeader(header string) string {
	if header == "" {
		return ""
	}

	dec := new(mime.WordDecoder)
	decoded, err := dec.DecodeHeader(header)
	if err != nil {
		return header
	}
	return decoded
}

// parseAddressList parses a comma-separated list of email addresses.
func parseAddressList(header string) []string {
	addrs, err := mail.ParseAddressList(header)
	if err != nil {
		// Fallback: split by comma
		parts := strings.Split(header, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, p)
			}
		}
		return result
	}

	result := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		if addr.Name != "" {
			result = append(result, addr.Name+" <"+addr.Address+">")
		} else {
			result = append(result, addr.Address)
		}
	}
	return result
}

// decodeHTMLEntities decodes common HTML entities.
func decodeHTMLEntities(s string) string {
	replacements := []struct {
		entity string
		char   string
	}{
		{"&nbsp;", " "},
		{"&amp;", "&"},
		{"&lt;", "<"},
		{"&gt;", ">"},
		{"&quot;", "\""},
		{"&apos;", "'"},
		{"&#39;", "'"},
		{"&#x27;", "'"},
		{"&ndash;", "-"},
		{"&mdash;", "-"},
		{"&hellip;", "..."},
		{"&copy;", "(c)"},
		{"&reg;", "(R)"},
		{"&trade;", "(TM)"},
	}

	for _, r := range replacements {
		s = strings.ReplaceAll(s, r.entity, r.char)
	}

	// Handle numeric entities
	numericEntity := regexp.MustCompile(`&#(\d+);`)
	s = numericEntity.ReplaceAllStringFunc(s, func(match string) string {
		if ch, ok := parseASCIIEntity(match); ok {
			return string([]byte{ch})
		}
		return match
	})

	return s
}

func parseASCIIEntity(entity string) (byte, bool) {
	value := byte(0)
	digits := entity
	if len(digits) > 2 && digits[0] == '&' && digits[1] == '#' {
		digits = digits[2:]
	}
	if len(digits) > 0 && digits[len(digits)-1] == ';' {
		digits = digits[:len(digits)-1]
	}
	if digits == "" {
		return 0, false
	}

	for i := 0; i < len(digits); i++ {
		d := digits[i]
		if d < '0' || d > '9' {
			return 0, false
		}
		digit := d - '0'
		if value > 12 || (value == 12 && digit > 7) {
			return 0, false
		}
		value = value*10 + digit
	}
	return value, true
}

// parseEntityCode extracts the numeric code from an HTML entity.
func parseEntityCode(entity string, code *int) (int, error) {
	// Strip &# prefix and ; suffix
	s := entity
	if len(s) > 2 && s[0] == '&' && s[1] == '#' {
		s = s[2:]
	}
	if len(s) > 0 && s[len(s)-1] == ';' {
		s = s[:len(s)-1]
	}

	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, nil
		}
		n = n*10 + int(c-'0')
	}
	*code = n
	return 1, nil
}

// normalizeWhitespace collapses multiple whitespace into single spaces.
func normalizeWhitespace(s string) string {
	// Replace common whitespace patterns
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	// Use a buffer for efficient string building
	var buf bytes.Buffer
	buf.Grow(len(s))

	lastWasSpace := true // Start true to trim leading whitespace
	for i := 0; i < len(s); i++ {
		c := s[i]
		isSpace := c == ' ' || c == '\t' || c == '\n'

		if isSpace {
			if !lastWasSpace {
				buf.WriteByte(' ')
				lastWasSpace = true
			}
		} else {
			buf.WriteByte(c)
			lastWasSpace = false
		}
	}

	result := buf.String()

	// Trim trailing space
	if len(result) > 0 && result[len(result)-1] == ' ' {
		result = result[:len(result)-1]
	}

	return result
}

// isPrintable checks if the byte slice contains printable ASCII.
func isPrintable(data []byte) bool {
	if len(data) == 0 {
		return true
	}

	// Check first 1KB
	checkLen := len(data)
	if checkLen > 1024 {
		checkLen = 1024
	}

	nonPrintable := 0
	for i := 0; i < checkLen; i++ {
		c := data[i]
		if c < 32 && c != '\t' && c != '\n' && c != '\r' {
			nonPrintable++
		}
	}

	// Allow up to 5% non-printable characters
	return nonPrintable < checkLen/20
}

const (
	// maxBodySize is the maximum body size to index (1MB)
	maxBodySize = 1 * 1024 * 1024
)
