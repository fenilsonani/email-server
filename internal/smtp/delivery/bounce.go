// Package delivery implements outbound email delivery with circuit breakers and retry logic.
package delivery

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/fenilsonani/email-server/internal/queue"
)

// BounceGenerator creates Delivery Status Notifications (bounces)
type BounceGenerator struct {
	hostname   string
	postmaster string
	template   *template.Template
}

// NewBounceGenerator creates a new bounce message generator
func NewBounceGenerator(hostname string) *BounceGenerator {
	tmpl := template.Must(template.New("bounce").Parse(bounceTemplate))
	return &BounceGenerator{
		hostname:   hostname,
		postmaster: "postmaster@" + hostname,
		template:   tmpl,
	}
}

// BounceData contains data for the bounce message template
type BounceData struct {
	MessageID       string
	Date            string
	From            string
	To              string
	OriginalSender  string
	FailedRecipient string
	ErrorCode       string
	ErrorMessage    string
	Hostname        string
	OriginalHeaders string
}

// Generate creates a DSN for a failed delivery
func (g *BounceGenerator) Generate(msg *queue.Message, failureErr error) ([]byte, error) {
	// Extract original headers if message file exists
	originalHeaders := ""
	if msg.MessagePath != "" {
		if content, err := os.ReadFile(msg.MessagePath); err == nil {
			// Extract just the headers (up to first blank line)
			if idx := bytes.Index(content, []byte("\r\n\r\n")); idx > 0 {
				originalHeaders = string(content[:idx])
			} else if idx := bytes.Index(content, []byte("\n\n")); idx > 0 {
				originalHeaders = string(content[:idx])
			}
			// Limit header size to prevent huge bounces
			if len(originalHeaders) > 4096 {
				originalHeaders = originalHeaders[:4096] + "\n[... truncated ...]"
			}
		}
	}

	// Classify error code
	errorCode := classifyErrorCode(failureErr)

	data := BounceData{
		MessageID:       fmt.Sprintf("<%d.bounce@%s>", time.Now().UnixNano(), g.hostname),
		Date:            time.Now().Format(time.RFC1123Z),
		From:            g.postmaster,
		To:              msg.Sender,
		OriginalSender:  msg.Sender,
		FailedRecipient: strings.Join(msg.Recipients, ", "),
		ErrorCode:       errorCode,
		ErrorMessage:    failureErr.Error(),
		Hostname:        g.hostname,
		OriginalHeaders: originalHeaders,
	}

	var buf bytes.Buffer
	if err := g.template.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to generate bounce: %w", err)
	}

	return buf.Bytes(), nil
}

// ShouldBounce returns true if a bounce should be generated for this message
// Prevents bounce loops by not bouncing null senders or system addresses
func ShouldBounce(sender string) bool {
	if sender == "" {
		return false // Null sender (already a bounce)
	}
	sender = strings.ToLower(sender)
	if strings.HasPrefix(sender, "postmaster@") ||
		strings.HasPrefix(sender, "mailer-daemon@") ||
		strings.HasPrefix(sender, "noreply@") ||
		strings.HasPrefix(sender, "no-reply@") {
		return false
	}
	return true
}

// classifyErrorCode determines the DSN error code from the error
func classifyErrorCode(err error) string {
	if err == nil {
		return "5.0.0"
	}
	errStr := err.Error()

	// Map common SMTP codes to enhanced codes
	switch {
	case strings.Contains(errStr, "550"):
		return "5.1.1" // Bad destination mailbox
	case strings.Contains(errStr, "551"):
		return "5.1.6" // Destination mailbox moved
	case strings.Contains(errStr, "552"):
		return "5.2.2" // Mailbox full
	case strings.Contains(errStr, "553"):
		return "5.1.3" // Bad destination mailbox syntax
	case strings.Contains(errStr, "554"):
		return "5.7.1" // Delivery not authorized
	default:
		return "5.0.0" // Other permanent failure
	}
}

const bounceTemplate = `From: Mail Delivery System <{{.From}}>
To: <{{.To}}>
Subject: Undelivered Mail Returned to Sender
Date: {{.Date}}
Message-ID: {{.MessageID}}
MIME-Version: 1.0
Content-Type: multipart/report; report-type=delivery-status; boundary="=_bounce_boundary"
Auto-Submitted: auto-replied

--=_bounce_boundary
Content-Type: text/plain; charset=utf-8

This is the mail delivery system at {{.Hostname}}.

I'm sorry to inform you that your message could not be delivered to one or
more recipients. The following address(es) failed:

    {{.FailedRecipient}}

Error: {{.ErrorMessage}}

If this problem persists, please contact your mail administrator.

This is a permanent error; the message will not be retried.

--=_bounce_boundary
Content-Type: message/delivery-status

Reporting-MTA: dns; {{.Hostname}}
Arrival-Date: {{.Date}}

Final-Recipient: rfc822; {{.FailedRecipient}}
Action: failed
Status: {{.ErrorCode}}
Diagnostic-Code: smtp; {{.ErrorMessage}}

--=_bounce_boundary
Content-Type: text/rfc822-headers

{{.OriginalHeaders}}

--=_bounce_boundary--
`
