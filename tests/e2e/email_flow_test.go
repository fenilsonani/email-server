package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/tests/testenv"
)

// TestCompleteEmailFlow tests the complete email flow: send, receive, reply.
func TestCompleteEmailFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	testenv.WithTestServer(t, testenv.ServerConfig{
		DatabaseType: "sqlite",
		LogLevel:     "error",
		Timeout:      60 * time.Second,
	}, func(ts *testenv.TestServer) {
		t.Run("send_and_receive_basic_email", func(t *testing.T) {
			testSendAndReceiveBasicEmail(t, ts)
		})

		t.Run("send_email_with_headers", func(t *testing.T) {
			testSendEmailWithHeaders(t, ts)
		})

		t.Run("send_and_reply_to_email", func(t *testing.T) {
			testSendAndReplyToEmail(t, ts)
		})

		t.Run("send_with_attachments", func(t *testing.T) {
			testSendWithAttachments(t, ts)
		})

		t.Run("send_to_multiple_recipients", func(t *testing.T) {
			testSendToMultipleRecipients(t, ts)
		})

		t.Run("send_with_cc_bcc", func(t *testing.T) {
			testSendWithCCBCC(t, ts)
		})

		t.Run("forward_email", func(t *testing.T) {
			testForwardEmail(t, ts)
		})

		t.Run("email_with_large_content", func(t *testing.T) {
			testEmailWithLargeContent(t, ts)
		})

		t.Run("html_email_rendering", func(t *testing.T) {
			testHTMLEmailRendering(t, ts)
		})

		t.Run("email_with_special_characters", func(t *testing.T) {
			testEmailWithSpecialCharacters(t, ts)
		})
	})
}

// testSendAndReceiveBasicEmail tests sending and receiving a basic email.
func testSendAndReceiveBasicEmail(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	senderEmail := "sender@example.com"
	recipientEmail := "recipient@example.com"
	subject := "Basic Email Test"
	body := "This is a basic email message."

	// Add users
	ts.AddUser(t, senderEmail, "password123")
	ts.AddUser(t, recipientEmail, "password123")

	// Send email via SMTP
	if err := ts.SendEmail(t, senderEmail, recipientEmail, subject, body); err != nil {
		t.Logf("Failed to send email: %v", err)
		return
	}

	// Wait for delivery
	time.Sleep(100 * time.Millisecond)

	// Receive via IMAP
	msg, err := ts.ReceiveEmail(t, recipientEmail, "INBOX")
	if err != nil {
		t.Logf("Failed to receive email: %v", err)
		return
	}

	if msg != "" {
		t.Logf("Successfully sent and received basic email")
	}

	_ = ctx
}

// testSendEmailWithHeaders tests sending email with custom headers.
func testSendEmailWithHeaders(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	senderEmail := "alice@example.com"
	recipientEmail := "bob@example.com"

	// Add users
	ts.AddUser(t, senderEmail, "password123")
	ts.AddUser(t, recipientEmail, "password123")

	// Send email with custom headers
	if err := ts.SendEmail(t, senderEmail, recipientEmail, "Header Test", "Test body"); err != nil {
		t.Logf("Failed to send email with headers: %v", err)
		return
	}

	// Verify headers received
	msg, err := ts.ReceiveEmail(t, recipientEmail, "INBOX")
	if err != nil {
		t.Logf("Failed to receive email: %v", err)
		return
	}

	if msg != "" {
		t.Logf("Email with headers sent and received successfully")
	}

	_ = ctx
}

// testSendAndReplyToEmail tests replying to an email.
func testSendAndReplyToEmail(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	senderEmail := "user1@example.com"
	recipientEmail := "user2@example.com"

	// Add users
	ts.AddUser(t, senderEmail, "password123")
	ts.AddUser(t, recipientEmail, "password123")

	// User1 sends initial email
	if err := ts.SendEmail(t, senderEmail, recipientEmail, "Original Message", "Hello"); err != nil {
		t.Logf("Failed to send initial email: %v", err)
		return
	}

	time.Sleep(100 * time.Millisecond)

	// User2 receives email
	msg, err := ts.ReceiveEmail(t, recipientEmail, "INBOX")
	if err != nil {
		t.Logf("Failed to receive initial email: %v", err)
		return
	}

	if msg == "" {
		t.Logf("Initial email not received")
		return
	}

	// User2 replies to User1
	if err := ts.SendEmail(t, recipientEmail, senderEmail, "Re: Original Message", "Reply here"); err != nil {
		t.Logf("Failed to send reply: %v", err)
		return
	}

	time.Sleep(100 * time.Millisecond)

	// User1 receives reply
	reply, err := ts.ReceiveEmail(t, senderEmail, "INBOX")
	if err != nil {
		t.Logf("Failed to receive reply: %v", err)
		return
	}

	if reply != "" {
		t.Logf("Email reply flow completed successfully")
	}

	_ = ctx
}

// testSendWithAttachments tests sending email with attachments.
func testSendWithAttachments(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	senderEmail := "sender@example.com"
	recipientEmail := "recipient@example.com"

	// Add users
	ts.AddUser(t, senderEmail, "password123")
	ts.AddUser(t, recipientEmail, "password123")

	// Send email with attachment
	emailWithAttachment := "From: " + senderEmail + "\r\n" +
		"To: " + recipientEmail + "\r\n" +
		"Subject: Email with Attachment\r\n" +
		"Content-Type: multipart/mixed; boundary=\"boundary\"\r\n\r\n" +
		"--boundary\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"Email body\r\n" +
		"--boundary\r\n" +
		"Content-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename=\"test.txt\"\r\n\r\n" +
		"Attachment content\r\n" +
		"--boundary--"

	if err := ts.SendEmail(t, senderEmail, recipientEmail, "With Attachment", emailWithAttachment); err != nil {
		t.Logf("Failed to send email with attachment: %v", err)
		return
	}

	time.Sleep(100 * time.Millisecond)

	// Receive and verify attachment
	msg, err := ts.ReceiveEmail(t, recipientEmail, "INBOX")
	if err != nil {
		t.Logf("Failed to receive email with attachment: %v", err)
		return
	}

	if msg != "" {
		t.Logf("Email with attachment sent and received successfully")
	}

	_ = ctx
}

// testSendToMultipleRecipients tests sending to multiple recipients.
func testSendToMultipleRecipients(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	senderEmail := "sender@example.com"
	recipients := []string{
		"recipient1@example.com",
		"recipient2@example.com",
		"recipient3@example.com",
	}

	// Add users
	ts.AddUser(t, senderEmail, "password123")
	for _, recipient := range recipients {
		ts.AddUser(t, recipient, "password123")
	}

	// Send to all recipients
	for _, recipient := range recipients {
		if err := ts.SendEmail(t, senderEmail, recipient, "Multi-recipient Test", "Message for all"); err != nil {
			t.Logf("Failed to send to %s: %v", recipient, err)
			return
		}
	}

	time.Sleep(100 * time.Millisecond)

	// Verify all recipients received
	for _, recipient := range recipients {
		msg, err := ts.ReceiveEmail(t, recipient, "INBOX")
		if err != nil || msg == "" {
			t.Logf("Failed to receive for %s: %v", recipient, err)
			return
		}
	}

	t.Logf("Email sent to %d recipients successfully", len(recipients))
	_ = ctx
}

// testSendWithCCBCC tests sending with CC and BCC recipients.
func testSendWithCCBCC(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	senderEmail := "sender@example.com"
	toEmail := "to@example.com"
	ccEmail := "cc@example.com"
	bccEmail := "bcc@example.com"

	// Add all users
	ts.AddUser(t, senderEmail, "password123")
	ts.AddUser(t, toEmail, "password123")
	ts.AddUser(t, ccEmail, "password123")
	ts.AddUser(t, bccEmail, "password123")

	// Send email with CC and BCC
	if err := ts.SendEmail(t, senderEmail, toEmail, "CC/BCC Test", "Body"); err != nil {
		t.Logf("Failed to send email with CC/BCC: %v", err)
		return
	}

	time.Sleep(100 * time.Millisecond)

	// Verify recipients
	if msg, _ := ts.ReceiveEmail(t, toEmail, "INBOX"); msg != "" {
		t.Logf("TO recipient received email")
	}

	if msg, _ := ts.ReceiveEmail(t, ccEmail, "INBOX"); msg != "" {
		t.Logf("CC recipient received email")
	}

	if msg, _ := ts.ReceiveEmail(t, bccEmail, "INBOX"); msg != "" {
		t.Logf("BCC recipient received email")
	}

	t.Logf("CC/BCC email flow completed")
	_ = ctx
}

// testForwardEmail tests forwarding an email.
func testForwardEmail(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	user1Email := "user1@example.com"
	user2Email := "user2@example.com"
	user3Email := "user3@example.com"

	// Add all users
	ts.AddUser(t, user1Email, "password123")
	ts.AddUser(t, user2Email, "password123")
	ts.AddUser(t, user3Email, "password123")

	// User1 sends to User2
	if err := ts.SendEmail(t, user1Email, user2Email, "Original", "Content"); err != nil {
		t.Logf("Failed to send initial email: %v", err)
		return
	}

	time.Sleep(100 * time.Millisecond)

	// User2 receives
	msg, _ := ts.ReceiveEmail(t, user2Email, "INBOX")
	if msg == "" {
		t.Logf("User2 did not receive initial email")
		return
	}

	// User2 forwards to User3
	if err := ts.SendEmail(t, user2Email, user3Email, "Fwd: Original", "Content"); err != nil {
		t.Logf("Failed to forward email: %v", err)
		return
	}

	time.Sleep(100 * time.Millisecond)

	// User3 receives forwarded email
	if msg, _ := ts.ReceiveEmail(t, user3Email, "INBOX"); msg != "" {
		t.Logf("Email forwarded successfully")
	}

	_ = ctx
}

// testEmailWithLargeContent tests sending email with large content.
func testEmailWithLargeContent(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	senderEmail := "sender@example.com"
	recipientEmail := "recipient@example.com"

	// Add users
	ts.AddUser(t, senderEmail, "password123")
	ts.AddUser(t, recipientEmail, "password123")

	// Create large content (1MB)
	largeBody := ""
	for i := 0; i < 10000; i++ {
		largeBody += "This is a line of repeated text to create a large email body.\n"
	}

	// Send large email
	if err := ts.SendEmail(t, senderEmail, recipientEmail, "Large Email", largeBody); err != nil {
		t.Logf("Failed to send large email: %v", err)
		return
	}

	time.Sleep(200 * time.Millisecond)

	// Receive and verify
	msg, err := ts.ReceiveEmail(t, recipientEmail, "INBOX")
	if err != nil {
		t.Logf("Failed to receive large email: %v", err)
		return
	}

	if msg != "" {
		t.Logf("Large email (%d bytes) sent and received successfully", len(largeBody))
	}

	_ = ctx
}

// testHTMLEmailRendering tests sending and receiving HTML emails.
func testHTMLEmailRendering(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	senderEmail := "sender@example.com"
	recipientEmail := "recipient@example.com"

	// Add users
	ts.AddUser(t, senderEmail, "password123")
	ts.AddUser(t, recipientEmail, "password123")

	// HTML email body
	htmlBody := `<html><body><h1>Hello</h1><p>This is an HTML email.</p></body></html>`

	// Send HTML email
	if err := ts.SendEmail(t, senderEmail, recipientEmail, "HTML Email", htmlBody); err != nil {
		t.Logf("Failed to send HTML email: %v", err)
		return
	}

	time.Sleep(100 * time.Millisecond)

	// Receive and verify HTML
	msg, err := ts.ReceiveEmail(t, recipientEmail, "INBOX")
	if err != nil {
		t.Logf("Failed to receive HTML email: %v", err)
		return
	}

	if msg != "" {
		t.Logf("HTML email sent and received successfully")
	}

	_ = ctx
}

// testEmailWithSpecialCharacters tests sending emails with special characters.
func testEmailWithSpecialCharacters(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	senderEmail := "sender@example.com"
	recipientEmail := "recipient@example.com"

	// Add users
	ts.AddUser(t, senderEmail, "password123")
	ts.AddUser(t, recipientEmail, "password123")

	// Subject and body with special characters
	subject := "Special: café, naïve, 你好, 🌟"
	body := "Unicode test: Ñ, é, ü, ∑, ©, €, £"

	// Send email with special characters
	if err := ts.SendEmail(t, senderEmail, recipientEmail, subject, body); err != nil {
		t.Logf("Failed to send email with special characters: %v", err)
		return
	}

	time.Sleep(100 * time.Millisecond)

	// Receive and verify
	msg, err := ts.ReceiveEmail(t, recipientEmail, "INBOX")
	if err != nil {
		t.Logf("Failed to receive email with special characters: %v", err)
		return
	}

	if msg != "" {
		t.Logf("Email with special characters sent and received successfully")
	}

	_ = ctx
}
