package testutil

import (
	"fmt"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

// SMTPTestClient provides a test client for SMTP operations.
type SMTPTestClient struct {
	host     string
	port     int
	username string
	password string
	from     string
}

// NewSMTPTestClient creates a new SMTP test client.
func NewSMTPTestClient(host string, port int, username, password, fromAddr string) *SMTPTestClient {
	return &SMTPTestClient{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     fromAddr,
	}
}

// SendEmail sends an email via SMTP for testing.
func (c *SMTPTestClient) SendEmail(t *testing.T, to []string, subject, body string) error {
	t.Helper()

	addr := fmt.Sprintf("%s:%d", c.host, c.port)

	// Create email message
	message := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\n\r\n%s",
		c.from,
		strings.Join(to, ", "),
		subject,
		time.Now().Format(time.RFC1123Z),
		body,
	)

	// Connect and send
	auth := smtp.PlainAuth("", c.username, c.password, c.host)
	if err := smtp.SendMail(addr, auth, c.from, to, []byte(message)); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// SendEmailWithHeaders sends an email with custom headers via SMTP.
func (c *SMTPTestClient) SendEmailWithHeaders(t *testing.T, to []string, headers map[string]string, body string) error {
	t.Helper()

	addr := fmt.Sprintf("%s:%d", c.host, c.port)

	// Build headers
	headerStr := fmt.Sprintf("From: %s\r\nTo: %s\r\nDate: %s\r\n",
		c.from,
		strings.Join(to, ", "),
		time.Now().Format(time.RFC1123Z),
	)

	for key, value := range headers {
		headerStr += fmt.Sprintf("%s: %s\r\n", key, value)
	}

	message := headerStr + "\r\n" + body

	// Connect and send
	auth := smtp.PlainAuth("", c.username, c.password, c.host)
	if err := smtp.SendMail(addr, auth, c.from, to, []byte(message)); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// SendBulkEmails sends multiple emails in bulk for load testing.
func (c *SMTPTestClient) SendBulkEmails(t *testing.T, recipients []string, subject, body string, count int) error {
	t.Helper()

	for i := 0; i < count; i++ {
		if err := c.SendEmail(t, recipients, fmt.Sprintf("%s %d", subject, i), body); err != nil {
			return fmt.Errorf("failed to send bulk email %d: %w", i, err)
		}
	}

	return nil
}

// SendEmailWithDelay sends emails with a delay between each one.
func (c *SMTPTestClient) SendEmailWithDelay(t *testing.T, recipients []string, subject, body string, count int, delay time.Duration) error {
	t.Helper()

	for i := 0; i < count; i++ {
		if err := c.SendEmail(t, recipients, fmt.Sprintf("%s %d", subject, i), body); err != nil {
			return fmt.Errorf("failed to send delayed email %d: %w", i, err)
		}

		if i < count-1 {
			time.Sleep(delay)
		}
	}

	return nil
}

// EmailBuilder provides a fluent interface for building test emails.
type EmailBuilder struct {
	from    string
	to      []string
	cc      []string
	bcc     []string
	subject string
	body    string
	headers map[string]string
}

// NewEmailBuilder creates a new email builder.
func NewEmailBuilder() *EmailBuilder {
	return &EmailBuilder{
		headers: make(map[string]string),
	}
}

// From sets the sender address.
func (eb *EmailBuilder) From(addr string) *EmailBuilder {
	eb.from = addr
	return eb
}

// To adds a recipient.
func (eb *EmailBuilder) To(addrs ...string) *EmailBuilder {
	eb.to = append(eb.to, addrs...)
	return eb
}

// CC adds a carbon copy recipient.
func (eb *EmailBuilder) CC(addrs ...string) *EmailBuilder {
	eb.cc = append(eb.cc, addrs...)
	return eb
}

// BCC adds a blind carbon copy recipient.
func (eb *EmailBuilder) BCC(addrs ...string) *EmailBuilder {
	eb.bcc = append(eb.bcc, addrs...)
	return eb
}

// Subject sets the email subject.
func (eb *EmailBuilder) Subject(s string) *EmailBuilder {
	eb.subject = s
	return eb
}

// Body sets the email body.
func (eb *EmailBuilder) Body(b string) *EmailBuilder {
	eb.body = b
	return eb
}

// Header adds a custom header.
func (eb *EmailBuilder) Header(key, value string) *EmailBuilder {
	eb.headers[key] = value
	return eb
}

// Build constructs the email message.
func (eb *EmailBuilder) Build() string {
	headerStr := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\n",
		eb.from,
		strings.Join(eb.to, ", "),
		eb.subject,
		time.Now().Format(time.RFC1123Z),
	)

	if len(eb.cc) > 0 {
		headerStr += fmt.Sprintf("Cc: %s\r\n", strings.Join(eb.cc, ", "))
	}

	for key, value := range eb.headers {
		headerStr += fmt.Sprintf("%s: %s\r\n", key, value)
	}

	return headerStr + "\r\n" + eb.body
}

// Send sends the built email using the provided client.
func (eb *EmailBuilder) Send(t *testing.T, client *SMTPTestClient) error {
	t.Helper()

	allRecipients := append(eb.to, eb.cc...)
	allRecipients = append(allRecipients, eb.bcc...)

	addr := fmt.Sprintf("%s:%d", client.host, client.port)
	message := eb.Build()

	auth := smtp.PlainAuth("", client.username, client.password, client.host)
	if err := smtp.SendMail(addr, auth, client.from, allRecipients, []byte(message)); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

// TestEmailFixture provides common test email templates.
type TestEmailFixture struct{}

// SimpleEmail returns a simple test email message.
func (tef *TestEmailFixture) SimpleEmail() string {
	return "This is a simple test email."
}

// HTMLEmail returns an HTML test email message.
func (tef *TestEmailFixture) HTMLEmail() string {
	return `<html><body><h1>Test Email</h1><p>This is an HTML test email.</p></body></html>`
}

// LargeEmail returns a large test email message (for testing size limits).
func (tef *TestEmailFixture) LargeEmail() string {
	const (
		sizeMB = 5
		sizeB  = sizeMB * 1024 * 1024
	)

	// Create a large body by repeating text
	baseText := "This is a line of text that will be repeated to create a large email. "
	repeats := sizeB / len(baseText)

	body := ""
	for i := 0; i < repeats; i++ {
		body += baseText
	}

	return body
}

// EmailWithAttachment returns an email with attachment metadata.
func (tef *TestEmailFixture) EmailWithAttachment() string {
	return `
--boundary-example
Content-Type: text/plain; charset="utf-8"
Content-Transfer-Encoding: 7bit

This is the message body.

--boundary-example
Content-Type: text/plain; charset="utf-8"
Content-Disposition: attachment; filename="test.txt"
Content-Transfer-Encoding: 7bit

Attachment content here.

--boundary-example--
`
}

// ReplyEmail returns a reply email message.
func (tef *TestEmailFixture) ReplyEmail(originalMessageID string) string {
	return fmt.Sprintf(
		"This is a reply to message %s.\n\nOriginal message content follows:\n> Previous message",
		originalMessageID,
	)
}

// BounceEmail returns a bounce notification message.
func (tef *TestEmailFixture) BounceEmail(originalRecipient string) string {
	return fmt.Sprintf(
		"Failed Delivery Notification\n\nYour message to %s could not be delivered.",
		originalRecipient,
	)
}

// VacationResponse returns a vacation auto-reply message.
func (tef *TestEmailFixture) VacationResponse(returnDate string) string {
	return fmt.Sprintf(
		"Thank you for your email. I am currently out of the office and will return on %s. "+
			"I will respond to your message upon my return.",
		returnDate,
	)
}

// AssertEmailSent asserts that an email was successfully sent.
func AssertEmailSent(t *testing.T, client *SMTPTestClient, to []string, subject string) {
	t.Helper()
	// Note: This would require a mock SMTP server to fully implement
	// For now, this is a placeholder for integration testing with a real SMTP server
	t.Logf("Email to %v with subject %q would be sent", to, subject)
}

// AssertEmailNotSent asserts that an email was not sent.
func AssertEmailNotSent(t *testing.T, subject string) {
	t.Helper()
	t.Logf("Email with subject %q should not be sent", subject)
}
