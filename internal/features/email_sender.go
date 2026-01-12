package features

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DeliveryEnqueuer is the interface for enqueueing emails for delivery
type DeliveryEnqueuer interface {
	Enqueue(ctx context.Context, sender string, recipients []string, messagePath string) error
}

// QueueEmailSender implements EmailSender by writing to disk and enqueueing for delivery
type QueueEmailSender struct {
	enqueuer  DeliveryEnqueuer
	queuePath string
}

// NewQueueEmailSender creates an email sender that uses the delivery queue
func NewQueueEmailSender(enqueuer DeliveryEnqueuer, queuePath string) *QueueEmailSender {
	return &QueueEmailSender{
		enqueuer:  enqueuer,
		queuePath: queuePath,
	}
}

// SendEmail sends an email by writing to queue and enqueueing for delivery
func (q *QueueEmailSender) SendEmail(ctx context.Context, from string, to []string, subject, body, htmlBody string, headers map[string]string) error {
	// Build the email message
	var msg bytes.Buffer

	// Generate Message-ID
	idBytes := make([]byte, 12)
	rand.Read(idBytes)
	messageID := fmt.Sprintf("<%s@scheduled>", hex.EncodeToString(idBytes))

	// Add headers
	msg.WriteString(fmt.Sprintf("Message-ID: %s\r\n", messageID))
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	msg.WriteString("MIME-Version: 1.0\r\n")

	// Add custom headers
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}

	// Handle body
	if htmlBody != "" && body != "" {
		// Multipart message
		boundary := "----=_NextPart_" + fmt.Sprintf("%d", time.Now().UnixNano())
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
		msg.WriteString("\r\n")

		// Plain text part
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		msg.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		msg.WriteString(body)
		msg.WriteString("\r\n")

		// HTML part
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/html; charset=utf-8\r\n")
		msg.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		msg.WriteString(htmlBody)
		msg.WriteString("\r\n")

		msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else if htmlBody != "" {
		msg.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
		msg.WriteString(htmlBody)
	} else {
		msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
		msg.WriteString(body)
	}

	// Write message to queue directory
	filename := fmt.Sprintf("%d_%s.eml", time.Now().UnixNano(), hex.EncodeToString(idBytes[:6]))
	messagePath := filepath.Join(q.queuePath, filename)

	if err := os.WriteFile(messagePath, msg.Bytes(), 0600); err != nil {
		return fmt.Errorf("failed to write message to queue: %w", err)
	}

	// Enqueue for delivery
	if err := q.enqueuer.Enqueue(ctx, from, to, messagePath); err != nil {
		// Clean up file on failure
		os.Remove(messagePath)
		return fmt.Errorf("failed to enqueue message: %w", err)
	}

	return nil
}

// SMTPEmailSender implements EmailSender using direct SMTP
type SMTPEmailSender struct {
	host     string
	port     int
	username string
	password string
	useTLS   bool
}

// NewSMTPEmailSender creates a new SMTP-based email sender
func NewSMTPEmailSender(host string, port int, username, password string, useTLS bool) *SMTPEmailSender {
	return &SMTPEmailSender{
		host:     host,
		port:     port,
		username: username,
		password: password,
		useTLS:   useTLS,
	}
}

// NewLocalEmailSender creates an email sender for local delivery (localhost:25)
func NewLocalEmailSender() *SMTPEmailSender {
	return &SMTPEmailSender{
		host:   "localhost",
		port:   25,
		useTLS: false,
	}
}

// SendEmail sends an email via SMTP
func (s *SMTPEmailSender) SendEmail(ctx context.Context, from string, to []string, subject, body, htmlBody string, headers map[string]string) error {
	// Build the email message
	var msg bytes.Buffer

	// Add headers
	msg.WriteString(fmt.Sprintf("From: %s\r\n", from))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	msg.WriteString("MIME-Version: 1.0\r\n")

	// Add custom headers
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}

	// Handle body
	if htmlBody != "" && body != "" {
		// Multipart message
		boundary := "----=_NextPart_" + fmt.Sprintf("%d", time.Now().UnixNano())
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
		msg.WriteString("\r\n")

		// Plain text part
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		msg.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		msg.WriteString(body)
		msg.WriteString("\r\n")

		// HTML part
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/html; charset=utf-8\r\n")
		msg.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
		msg.WriteString(htmlBody)
		msg.WriteString("\r\n")

		msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else if htmlBody != "" {
		msg.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
		msg.WriteString(htmlBody)
	} else {
		msg.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
		msg.WriteString(body)
	}

	// Connect to SMTP server
	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	var conn net.Conn
	var err error

	// Create connection with context timeout
	dialer := &net.Dialer{
		Timeout: 30 * time.Second,
	}

	if s.useTLS {
		tlsConfig := &tls.Config{
			ServerName: s.host,
		}
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}
	defer conn.Close()

	// Create SMTP client
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	// Upgrade to TLS if not already using TLS and STARTTLS is available
	if !s.useTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{
				ServerName: s.host,
			}
			// Skip certificate verification for localhost (self-signed certs)
			if s.host == "localhost" || s.host == "127.0.0.1" {
				tlsConfig.InsecureSkipVerify = true
			}
			if err := client.StartTLS(tlsConfig); err != nil {
				// Continue without TLS for localhost
				if s.host != "localhost" && s.host != "127.0.0.1" {
					return fmt.Errorf("failed to start TLS: %w", err)
				}
			}
		}
	}

	// Authenticate if credentials provided
	if s.username != "" && s.password != "" {
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	// Set sender
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	// Set recipients
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("failed to set recipient %s: %w", recipient, err)
		}
	}

	// Send message body
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to start data: %w", err)
	}

	if _, err := w.Write(msg.Bytes()); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to close data: %w", err)
	}

	return client.Quit()
}
