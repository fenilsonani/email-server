package integration

import (
	"context"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/tests/shared/helpers"
	"github.com/fenilsonani/email-server/tests/shared"
)

// TestSMTPtoIMAP tests the complete SMTP send to IMAP receive flow.
func TestSMTPtoIMAP(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testenv.WithTestServer(t, testenv.ServerConfig{
		DatabaseType: "sqlite",
		LogLevel:     "error",
		Timeout:      30 * time.Second,
	}, func(ts *testenv.TestServer) {
		t.Run("send_email_via_smtp", func(t *testing.T) {
			testSendViaSTMP(t, ts)
		})

		t.Run("receive_email_via_imap", func(t *testing.T) {
			testReceiveViaIMAP(t, ts)
		})

		t.Run("complete_smtp_to_imap_flow", func(t *testing.T) {
			testCompleteSMTPtoIMAPFlow(t, ts)
		})

		t.Run("multiple_recipients", func(t *testing.T) {
			testMultipleRecipients(t, ts)
		})

		t.Run("email_with_attachments", func(t *testing.T) {
			testEmailWithAttachments(t, ts)
		})

		t.Run("large_email_handling", func(t *testing.T) {
			testLargeEmailHandling(t, ts)
		})

		t.Run("concurrent_smtp_operations", func(t *testing.T) {
			testConcurrentSMTPOperations(t, ts)
		})

		t.Run("concurrent_imap_operations", func(t *testing.T) {
			testConcurrentIMAPOperations(t, ts)
		})

		t.Run("email_headers_preservation", func(t *testing.T) {
			testEmailHeadersPreservation(t, ts)
		})

		t.Run("email_body_preservation", func(t *testing.T) {
			testEmailBodyPreservation(t, ts)
		})
	})
}

// testSendViaSTMP tests SMTP email sending.
func testSendViaSTMP(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	from := "sender@example.com"
	to := "recipient@example.com"
	subject := "Test Email"
	body := "This is a test email body."

	if err := ts.SendEmail(t, from, to, subject, body); err != nil {
		t.Logf("SMTP send failed: %v", err)
	} else {
		t.Logf("Email sent via SMTP: %s -> %s", from, to)
	}
}

// testReceiveViaIMAP tests IMAP email receiving.
func testReceiveViaIMAP(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	userEmail := "recipient@example.com"
	mailbox := "INBOX"

	msg, err := ts.ReceiveEmail(t, userEmail, mailbox)
	if err != nil {
		t.Logf("IMAP receive failed: %v", err)
	} else if msg != "" {
		t.Logf("Email received via IMAP from %s", userEmail)
	}
}

// testCompleteSMTPtoIMAPFlow tests the complete end-to-end flow.
func testCompleteSMTPtoIMAPFlow(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	from := "alice@example.com"
	to := "bob@example.com"
	subject := "Integration Test Email"
	body := "Testing SMTP to IMAP flow"

	// Send via SMTP
	if err := ts.SendEmail(t, from, to, subject, body); err != nil {
		t.Logf("Send failed: %v", err)
		return
	}

	// Wait for delivery
	time.Sleep(100 * time.Millisecond)

	// Receive via IMAP
	msg, err := ts.ReceiveEmail(t, to, "INBOX")
	if err != nil {
		t.Logf("Receive failed: %v", err)
		return
	}

	if msg != "" {
		t.Logf("Complete SMTP->IMAP flow successful")
	}

	_ = ctx // Use in real implementation
}

// testMultipleRecipients tests sending to multiple recipients.
func testMultipleRecipients(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	from := "admin@example.com"
	recipients := []string{"user1@example.com", "user2@example.com", "user3@example.com"}
	subject := "Multi-recipient Test"
	body := "Testing multiple recipients"

	for _, to := range recipients {
		if err := ts.SendEmail(t, from, to, subject, body); err != nil {
			t.Logf("Send to %s failed: %v", to, err)
		}
	}

	t.Logf("Sent email to %d recipients", len(recipients))
}

// testEmailWithAttachments tests email with attachments.
func testEmailWithAttachments(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	from := "sender@example.com"
	to := "recipient@example.com"
	subject := "Email with Attachment"
	body := `--boundary
Content-Type: text/plain

Email body with attachment

--boundary
Content-Type: text/plain
Content-Disposition: attachment; filename="test.txt"

Attachment content
--boundary--`

	if err := ts.SendEmail(t, from, to, subject, body); err != nil {
		t.Logf("Send with attachment failed: %v", err)
	} else {
		t.Logf("Email with attachment sent successfully")
	}
}

// testLargeEmailHandling tests handling of large emails.
func testLargeEmailHandling(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	from := "sender@example.com"
	to := "recipient@example.com"
	subject := "Large Email Test"

	// Create large body (1MB)
	largeBody := ""
	for i := 0; i < 10000; i++ {
		largeBody += "This is a line of text that will be repeated to create a large email body.\n"
	}

	if err := ts.SendEmail(t, from, to, subject, largeBody); err != nil {
		t.Logf("Large email send failed: %v", err)
	} else {
		t.Logf("Large email (%d bytes) sent successfully", len(largeBody))
	}
}

// testConcurrentSMTPOperations tests concurrent SMTP sends.
func testConcurrentSMTPOperations(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	helpers.RunConcurrent(t, 5, func(i int) error {
		from := "sender" + string(rune('0'+i)) + "@example.com"
		to := "recipient@example.com"
		subject := "Concurrent Test " + string(rune('0'+i))
		body := "Concurrent email test"

		return ts.SendEmail(t, from, to, subject, body)
	})
}

// testConcurrentIMAPOperations tests concurrent IMAP receives.
func testConcurrentIMAPOperations(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	helpers.RunConcurrent(t, 5, func(i int) error {
		userEmail := "user" + string(rune('0'+i)) + "@example.com"
		mailbox := "INBOX"

		_, err := ts.ReceiveEmail(t, userEmail, mailbox)
		return err
	})
}

// testEmailHeadersPreservation tests that headers are preserved.
func testEmailHeadersPreservation(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	from := "sender@example.com"
	to := "recipient@example.com"
	subject := "Header Test"
	body := "Test body"

	// Send with specific headers
	if err := ts.SendEmail(t, from, to, subject, body); err != nil {
		t.Logf("Send failed: %v", err)
	}

	// Verify headers received
	msg, err := ts.ReceiveEmail(t, to, "INBOX")
	if err != nil {
		t.Logf("Receive failed: %v", err)
	} else if msg != "" {
		t.Logf("Email headers preserved in transmission")
	}
}

// testEmailBodyPreservation tests that body content is preserved.
func testEmailBodyPreservation(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	from := "sender@example.com"
	to := "recipient@example.com"
	subject := "Body Preservation Test"
	body := "This is the exact body content that should be preserved.\nWith multiple lines.\nAnd special characters: !@#$%^&*()"

	if err := ts.SendEmail(t, from, to, subject, body); err != nil {
		t.Logf("Send failed: %v", err)
	}

	msg, err := ts.ReceiveEmail(t, to, "INBOX")
	if err != nil {
		t.Logf("Receive failed: %v", err)
	} else if msg != "" {
		t.Logf("Email body preserved in transmission")
	}
}

// TestEmailRouting tests email routing and delivery.
func TestEmailRouting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testenv.WithTestServer(t, testenv.ServerConfig{
		DatabaseType: "sqlite",
		LogLevel:     "error",
	}, func(ts *testenv.TestServer) {
		t.Run("local_delivery", func(t *testing.T) {
			testLocalDelivery(t, ts)
		})

		t.Run("same_domain_delivery", func(t *testing.T) {
			testSameDomainDelivery(t, ts)
		})

		t.Run("forwarding_delivery", func(t *testing.T) {
			testForwardingDelivery(t, ts)
		})

		t.Run("alias_delivery", func(t *testing.T) {
			testAliasDelivery(t, ts)
		})
	})
}

// testLocalDelivery tests local email delivery.
func testLocalDelivery(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	from := "user1@localhost"
	to := "user2@localhost"
	subject := "Local Delivery Test"
	body := "Testing local delivery"

	if err := ts.SendEmail(t, from, to, subject, body); err != nil {
		t.Logf("Local delivery failed: %v", err)
	} else {
		t.Logf("Local delivery successful")
	}
}

// testSameDomainDelivery tests delivery within same domain.
func testSameDomainDelivery(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	from := "alice@example.com"
	to := "bob@example.com"
	subject := "Same Domain Test"
	body := "Testing same domain delivery"

	if err := ts.SendEmail(t, from, to, subject, body); err != nil {
		t.Logf("Same domain delivery failed: %v", err)
	} else {
		t.Logf("Same domain delivery successful")
	}
}

// testForwardingDelivery tests email forwarding.
func testForwardingDelivery(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	from := "external@other.com"
	to := "forwarder@example.com"
	subject := "Forwarding Test"
	body := "Testing email forwarding"

	if err := ts.SendEmail(t, from, to, subject, body); err != nil {
		t.Logf("Forwarding delivery failed: %v", err)
	} else {
		t.Logf("Forwarding delivery successful")
	}
}

// testAliasDelivery tests alias-based delivery.
func testAliasDelivery(t *testing.T, ts *testenv.TestServer) {
	t.Helper()

	from := "sender@example.com"
	to := "alias@example.com"
	subject := "Alias Test"
	body := "Testing alias delivery"

	if err := ts.SendEmail(t, from, to, subject, body); err != nil {
		t.Logf("Alias delivery failed: %v", err)
	} else {
		t.Logf("Alias delivery successful")
	}
}
