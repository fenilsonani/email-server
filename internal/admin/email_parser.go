package admin

import (
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// ParsedEmail represents a parsed email message
type ParsedEmail struct {
	Headers     map[string]string
	TextBody    string
	HTMLBody    string
	Attachments []EmailAttachment
}

// EmailAttachment represents an email attachment
type EmailAttachment struct {
	Filename    string
	ContentType string
	Size        int64
}

// parseMessageContent parses an email message from a reader
func parseMessageContent(r io.Reader) (*ParsedEmail, error) {
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return nil, err
	}

	parsed := &ParsedEmail{Headers: make(map[string]string)}

	// Extract common headers
	parsed.Headers["From"] = msg.Header.Get("From")
	parsed.Headers["To"] = msg.Header.Get("To")
	parsed.Headers["Cc"] = msg.Header.Get("Cc")
	parsed.Headers["Subject"] = msg.Header.Get("Subject")
	parsed.Headers["Date"] = msg.Header.Get("Date")
	parsed.Headers["Message-ID"] = msg.Header.Get("Message-ID")

	// Parse content type
	contentType := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// If parsing fails, treat as plain text
		mediaType = "text/plain"
	}

	// Handle multipart messages
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary != "" {
			mr := multipart.NewReader(msg.Body, boundary)
			if err := parseMultipart(mr, parsed); err != nil {
				return parsed, nil // Return what we have so far
			}
		}
	} else if strings.Contains(mediaType, "text/") {
		// Single part text message
		body, err := io.ReadAll(io.LimitReader(msg.Body, 256*1024)) // Limit to 256KB
		if err != nil {
			return parsed, nil
		}
		if strings.Contains(mediaType, "text/html") {
			parsed.HTMLBody = string(body)
		} else {
			parsed.TextBody = string(body)
		}
	}

	return parsed, nil
}

// parseMultipart recursively parses multipart email bodies
func parseMultipart(mr *multipart.Reader, parsed *ParsedEmail) error {
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		partType := part.Header.Get("Content-Type")
		mediaType, _, _ := mime.ParseMediaType(partType)

		// Handle nested multipart
		if strings.HasPrefix(mediaType, "multipart/") {
			// Skip nested multipart for simplicity
			continue
		}

		// Check if it's an attachment
		disposition := part.Header.Get("Content-Disposition")
		if strings.HasPrefix(disposition, "attachment") || part.FileName() != "" {
			// It's an attachment
			parsed.Attachments = append(parsed.Attachments, EmailAttachment{
				Filename:    part.FileName(),
				ContentType: mediaType,
			})
			continue
		}

		// Read body content (limit to 256KB per part)
		body, err := io.ReadAll(io.LimitReader(part, 256*1024))
		if err != nil {
			continue
		}

		// Determine if it's text or HTML
		if strings.Contains(mediaType, "text/plain") {
			if parsed.TextBody == "" {
				parsed.TextBody = string(body)
			}
		} else if strings.Contains(mediaType, "text/html") {
			if parsed.HTMLBody == "" {
				parsed.HTMLBody = string(body)
			}
		}
	}

	return nil
}

// sanitizeHTML sanitizes HTML content to prevent XSS attacks
func sanitizeHTML(html string) string {
	// Create a strict policy that only allows safe text formatting
	p := bluemonday.StrictPolicy()

	// Allow basic text formatting tags
	p.AllowElements("p", "br", "div", "span", "strong", "em", "b", "i", "u", "h1", "h2", "h3", "h4", "h5", "h6")

	// Allow lists
	p.AllowElements("ul", "ol", "li")

	// Allow basic styling (but no inline styles to prevent CSS injection)
	p.AllowAttrs("class").Matching(bluemonday.SpaceSeparatedTokens).OnElements("p", "div", "span")

	// Sanitize and return
	return p.Sanitize(html)
}
