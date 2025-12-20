// Package maildir provides Maildir format email storage.
package maildir

import (
	"bufio"
	"bytes"
	"io"
	"mime"
	"net/mail"
	"strings"
)

// MessageMetadata holds parsed message headers
type MessageMetadata struct {
	MessageID  string
	Subject    string
	From       string
	To         []string
	Cc         []string
	Date       string
	InReplyTo  string
	References string
}

// ParseMessageHeaders extracts metadata from message headers
// It reads only the headers (up to the first blank line) to minimize memory usage
func ParseMessageHeaders(r io.Reader) (*MessageMetadata, error) {
	br := bufio.NewReader(r)

	// Read headers until blank line
	var headerBuf bytes.Buffer
	for {
		line, err := br.ReadBytes('\n')
		if err != nil && err != io.EOF {
			break
		}

		// Check for end of headers (blank line)
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			break
		}

		headerBuf.Write(line)

		// Limit header size to prevent memory exhaustion (64KB max)
		if headerBuf.Len() > 64*1024 {
			break
		}

		if err == io.EOF {
			break
		}
	}

	// Parse headers using net/mail
	// We need to add a blank line to make it a valid message for parsing
	fullMsg := bytes.NewReader(append(headerBuf.Bytes(), '\r', '\n', '\r', '\n'))
	msg, err := mail.ReadMessage(fullMsg)
	if err != nil {
		// Return empty metadata on parse failure - don't fail the whole operation
		return &MessageMetadata{}, nil
	}

	meta := &MessageMetadata{
		MessageID:  cleanHeader(msg.Header.Get("Message-ID")),
		Subject:    decodeHeader(msg.Header.Get("Subject")),
		From:       extractEmailAddress(msg.Header.Get("From")),
		Date:       msg.Header.Get("Date"),
		InReplyTo:  cleanHeader(msg.Header.Get("In-Reply-To")),
		References: msg.Header.Get("References"),
	}

	// Parse To addresses
	if toHeader := msg.Header.Get("To"); toHeader != "" {
		meta.To = parseAddressList(toHeader)
	}

	// Parse Cc addresses
	if ccHeader := msg.Header.Get("Cc"); ccHeader != "" {
		meta.Cc = parseAddressList(ccHeader)
	}

	return meta, nil
}

// parseAddressList parses a comma-separated list of email addresses
func parseAddressList(header string) []string {
	addrs, err := mail.ParseAddressList(header)
	if err != nil {
		// Fallback: return the raw header as a single entry
		if header != "" {
			return []string{header}
		}
		return nil
	}

	result := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		result = append(result, addr.Address)
	}
	return result
}

// extractEmailAddress extracts just the email address from a From header
func extractEmailAddress(from string) string {
	if from == "" {
		return ""
	}
	addr, err := mail.ParseAddress(from)
	if err != nil {
		// Return as-is if parsing fails
		return from
	}
	return addr.Address
}

// decodeHeader decodes RFC 2047 encoded headers (e.g., =?UTF-8?B?...?=)
func decodeHeader(s string) string {
	if !strings.Contains(s, "=?") {
		return s
	}

	dec := new(mime.WordDecoder)
	decoded, err := dec.DecodeHeader(s)
	if err != nil {
		return s // Return original on decode failure
	}
	return decoded
}

// cleanHeader removes angle brackets and whitespace from header values
func cleanHeader(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	return s
}
