package maildir

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestParseMessageHeaders(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *MessageMetadata
		wantErr bool
	}{
		{
			name: "basic headers",
			input: "From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: Test Subject\r\n" +
				"Message-ID: <123@example.com>\r\n" +
				"Date: Mon, 20 Dec 2025 10:00:00 -0500\r\n" +
				"\r\n" +
				"Body content",
			want: &MessageMetadata{
				From:      "sender@example.com",
				To:        []string{"recipient@example.com"},
				Subject:   "Test Subject",
				MessageID: "123@example.com",
				Date:      "Mon, 20 Dec 2025 10:00:00 -0500",
			},
		},
		{
			name: "multiple recipients",
			input: "From: sender@example.com\r\n" +
				"To: user1@example.com, user2@example.com, user3@example.com\r\n" +
				"Subject: Multi\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"user1@example.com", "user2@example.com", "user3@example.com"},
				Subject: "Multi",
			},
		},
		{
			name: "with CC",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Cc: cc1@example.com, cc2@example.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				To:   []string{"to@example.com"},
				Cc:   []string{"cc1@example.com", "cc2@example.com"},
			},
		},
		{
			name: "RFC 2047 UTF-8 Base64 subject",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Subject: =?UTF-8?B?SGVsbG8gV29ybGQ=?=\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "Hello World",
			},
		},
		{
			name: "RFC 2047 UTF-8 Quoted-Printable subject",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Subject: =?UTF-8?Q?Hello_World?=\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "Hello World",
			},
		},
		{
			name: "RFC 2047 ISO-8859-1 Quoted-Printable",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Subject: =?ISO-8859-1?Q?caf=E9?=\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "café",
			},
		},
		{
			name: "RFC 2047 multiple encoded words",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Subject: =?UTF-8?B?SGVsbG8=?= =?UTF-8?B?V29ybGQ=?=\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "HelloWorld",
			},
		},
		{
			name: "From with display name",
			input: "From: \"John Doe\" <john@example.com>\r\n" +
				"To: to@example.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "john@example.com",
				To:   []string{"to@example.com"},
			},
		},
		{
			name: "From with display name no quotes",
			input: "From: John Doe <john@example.com>\r\n" +
				"To: to@example.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "john@example.com",
				To:   []string{"to@example.com"},
			},
		},
		{
			name: "To with display names",
			input: "From: sender@example.com\r\n" +
				"To: \"Alice\" <alice@example.com>, \"Bob\" <bob@example.com>\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				To:   []string{"alice@example.com", "bob@example.com"},
			},
		},
		{
			name: "Cc with display names",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Cc: \"Charlie\" <charlie@example.com>, David <david@example.com>\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				To:   []string{"to@example.com"},
				Cc:   []string{"charlie@example.com", "david@example.com"},
			},
		},
		{
			name: "In-Reply-To and References",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"In-Reply-To: <original@example.com>\r\n" +
				"References: <thread1@example.com> <thread2@example.com>\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:       "sender@example.com",
				To:         []string{"to@example.com"},
				InReplyTo:  "original@example.com",
				References: "<thread1@example.com> <thread2@example.com>",
			},
		},
		{
			name:  "empty input",
			input: "",
			want:  &MessageMetadata{},
		},
		{
			name:  "only blank line",
			input: "\r\n",
			want:  &MessageMetadata{},
		},
		{
			name: "no blank line separator",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Subject: No blank line",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "No blank line",
			},
		},
		{
			name: "LF only line endings",
			input: "From: sender@example.com\n" +
				"To: to@example.com\n" +
				"Subject: LF only\n" +
				"\n" +
				"Body",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "LF only",
			},
		},
		{
			name: "folded header",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Subject: This is a very long subject line that has been\r\n" +
				" folded according to RFC 5322\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "This is a very long subject line that has been folded according to RFC 5322",
			},
		},
		{
			name: "folded header with tab",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Subject: Line one\r\n" +
				"\tcontinued with tab\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "Line one continued with tab",
			},
		},
		{
			name: "malformed From header",
			input: "From: not-an-email\r\n" +
				"To: to@example.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "not-an-email", // Returns as-is
				To:   []string{"to@example.com"},
			},
		},
		{
			name: "malformed To list",
			input: "From: sender@example.com\r\n" +
				"To: this, is, not, valid\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				To:   []string{"this, is, not, valid"}, // Fallback to raw
			},
		},
		{
			name: "Message-ID with angle brackets",
			input: "From: sender@example.com\r\n" +
				"Message-ID: <unique-id@host.com>\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:      "sender@example.com",
				MessageID: "unique-id@host.com", // Brackets stripped
			},
		},
		{
			name: "Message-ID without angle brackets",
			input: "From: sender@example.com\r\n" +
				"Message-ID: plain-id@host.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:      "sender@example.com",
				MessageID: "plain-id@host.com",
			},
		},
		{
			name: "case insensitive headers",
			input: "FROM: sender@example.com\r\n" +
				"TO: to@example.com\r\n" +
				"SUBJECT: Case Test\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "Case Test",
			},
		},
		{
			name: "mixed case headers",
			input: "from: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"SuBjEcT: Mixed Case\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "Mixed Case",
			},
		},
		{
			name: "unicode in display name",
			input: "From: \"日本語\" <japanese@example.com>\r\n" +
				"To: to@example.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "japanese@example.com",
				To:   []string{"to@example.com"},
			},
		},
		{
			name: "empty subject",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Subject: \r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "",
			},
		},
		{
			name: "whitespace-only subject",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Subject:    \r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "",
			},
		},
		{
			name: "missing From header",
			input: "To: to@example.com\r\n" +
				"Subject: No From\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "",
				To:      []string{"to@example.com"},
				Subject: "No From",
			},
		},
		{
			name: "missing To header",
			input: "From: sender@example.com\r\n" +
				"Subject: No To\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      nil,
				Subject: "No To",
			},
		},
		{
			name: "missing Subject header",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "",
			},
		},
		{
			name: "missing Message-ID",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Subject: Test\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:      "sender@example.com",
				To:        []string{"to@example.com"},
				Subject:   "Test",
				MessageID: "",
			},
		},
		{
			name: "all headers missing",
			input: "\r\n",
			want:  &MessageMetadata{},
		},
		{
			name: "various date formats - RFC 5322",
			input: "From: sender@example.com\r\n" +
				"Date: Mon, 20 Dec 2025 10:00:00 -0500\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				Date: "Mon, 20 Dec 2025 10:00:00 -0500",
			},
		},
		{
			name: "date with timezone name",
			input: "From: sender@example.com\r\n" +
				"Date: Mon, 20 Dec 2025 10:00:00 EST\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				Date: "Mon, 20 Dec 2025 10:00:00 EST",
			},
		},
		{
			name: "date without day name",
			input: "From: sender@example.com\r\n" +
				"Date: 20 Dec 2025 10:00:00 +0000\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				Date: "20 Dec 2025 10:00:00 +0000",
			},
		},
		{
			name: "multiple To recipients mixed format",
			input: "From: sender@example.com\r\n" +
				"To: plain@example.com, \"With Name\" <name@example.com>, <bracket@example.com>\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				To:   []string{"plain@example.com", "name@example.com", "bracket@example.com"},
			},
		},
		{
			name: "invalid RFC 2047 encoding",
			input: "From: sender@example.com\r\n" +
				"Subject: =?INVALID?X?broken?=\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				Subject: "=?INVALID?X?broken?=", // Returns as-is
			},
		},
		{
			name: "RFC 2047 with incomplete encoding",
			input: "From: sender@example.com\r\n" +
				"Subject: =?UTF-8?B?incomplete\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				Subject: "=?UTF-8?B?incomplete", // Returns as-is
			},
		},
		{
			name: "empty To header",
			input: "From: sender@example.com\r\n" +
				"To: \r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				To:   nil,
			},
		},
		{
			name: "empty Cc header",
			input: "From: sender@example.com\r\n" +
				"Cc: \r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				Cc:   nil,
			},
		},
		{
			name: "References with multiple message IDs",
			input: "From: sender@example.com\r\n" +
				"References: <msg1@example.com> <msg2@example.com> <msg3@example.com>\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:       "sender@example.com",
				References: "<msg1@example.com> <msg2@example.com> <msg3@example.com>",
			},
		},
		{
			name: "In-Reply-To without angle brackets",
			input: "From: sender@example.com\r\n" +
				"In-Reply-To: plain-id@example.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:      "sender@example.com",
				InReplyTo: "plain-id@example.com",
			},
		},
		{
			name: "header with leading spaces in value",
			input: "From:    sender@example.com\r\n" +
				"To:    to@example.com\r\n" +
				"Subject:    Spaces\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "Spaces",
			},
		},
		{
			name: "header with tabs in value",
			input: "From:\tsender@example.com\r\n" +
				"To:\tto@example.com\r\n" +
				"Subject:\tTabs\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "Tabs",
			},
		},
		{
			name: "email with comments in address",
			input: "From: sender@example.com (Sender Name)\r\n" +
				"To: to@example.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				To:   []string{"to@example.com"},
			},
		},
		{
			name: "complex address with RFC 2047 in display name",
			input: "From: =?UTF-8?B?Sm9obiBEb2U=?= <john@example.com>\r\n" +
				"To: to@example.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "john@example.com",
				To:   []string{"to@example.com"},
			},
		},
		{
			name: "multiple Cc recipients",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Cc: cc1@example.com, cc2@example.com, cc3@example.com, cc4@example.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				To:   []string{"to@example.com"},
				Cc:   []string{"cc1@example.com", "cc2@example.com", "cc3@example.com", "cc4@example.com"},
			},
		},
		{
			name: "header only with no body",
			input: "From: sender@example.com\r\n" +
				"To: to@example.com\r\n" +
				"Subject: No body\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				To:      []string{"to@example.com"},
				Subject: "No body",
			},
		},
		{
			name: "long multiline folded subject",
			input: "From: sender@example.com\r\n" +
				"Subject: This is a very long subject that spans\r\n" +
				" multiple lines and continues here\r\n" +
				" and even more here to test folding\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				Subject: "This is a very long subject that spans multiple lines and continues here and even more here to test folding",
			},
		},
		{
			name: "special characters in subject",
			input: "From: sender@example.com\r\n" +
				"Subject: Special chars: !@#$%^&*()_+-=[]{}|;':\",./<>?\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "sender@example.com",
				Subject: "Special chars: !@#$%^&*()_+-=[]{}|;':\",./<>?",
			},
		},
		{
			name: "email with angle brackets only",
			input: "From: <sender@example.com>\r\n" +
				"To: <to@example.com>\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				To:   []string{"to@example.com"},
			},
		},
		{
			name: "duplicate headers - first wins for single value",
			input: "From: first@example.com\r\n" +
				"From: second@example.com\r\n" +
				"Subject: First\r\n" +
				"Subject: Second\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From:    "first@example.com",
				Subject: "First",
			},
		},
		{
			name: "unknown headers ignored",
			input: "From: sender@example.com\r\n" +
				"X-Custom-Header: custom value\r\n" +
				"X-Another: another\r\n" +
				"To: to@example.com\r\n" +
				"\r\n",
			want: &MessageMetadata{
				From: "sender@example.com",
				To:   []string{"to@example.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMessageHeaders(strings.NewReader(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMessageHeaders() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got.From != tt.want.From {
				t.Errorf("From = %q, want %q", got.From, tt.want.From)
			}
			if got.Subject != tt.want.Subject {
				t.Errorf("Subject = %q, want %q", got.Subject, tt.want.Subject)
			}
			if got.MessageID != tt.want.MessageID {
				t.Errorf("MessageID = %q, want %q", got.MessageID, tt.want.MessageID)
			}
			if got.Date != tt.want.Date {
				t.Errorf("Date = %q, want %q", got.Date, tt.want.Date)
			}
			if got.InReplyTo != tt.want.InReplyTo {
				t.Errorf("InReplyTo = %q, want %q", got.InReplyTo, tt.want.InReplyTo)
			}
			if got.References != tt.want.References {
				t.Errorf("References = %q, want %q", got.References, tt.want.References)
			}
			if !stringSliceEqual(got.To, tt.want.To) {
				t.Errorf("To = %v, want %v", got.To, tt.want.To)
			}
			if !stringSliceEqual(got.Cc, tt.want.Cc) {
				t.Errorf("Cc = %v, want %v", got.Cc, tt.want.Cc)
			}
		})
	}
}

func TestParseMessageHeaders_LargeInput(t *testing.T) {
	t.Run("subject exceeds 64KB limit", func(t *testing.T) {
		// Create headers larger than 64KB limit
		largeValue := strings.Repeat("X", 70*1024)
		input := "From: sender@example.com\r\n" +
			"Subject: " + largeValue + "\r\n" +
			"\r\n"

		got, err := ParseMessageHeaders(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseMessageHeaders() unexpected error: %v", err)
		}

		// Should truncate and not crash
		if got.From != "sender@example.com" {
			t.Errorf("From should still be parsed, got %q", got.From)
		}
	})

	t.Run("headers exactly at 64KB limit", func(t *testing.T) {
		// Create headers that are exactly 64KB
		baseHeaders := "From: sender@example.com\r\nSubject: "
		remaining := (64 * 1024) - len(baseHeaders) - 4 // -4 for \r\n\r\n
		largeValue := strings.Repeat("Y", remaining)
		input := baseHeaders + largeValue + "\r\n\r\n"

		got, err := ParseMessageHeaders(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseMessageHeaders() unexpected error: %v", err)
		}

		if got.From != "sender@example.com" {
			t.Errorf("From = %q, want sender@example.com", got.From)
		}
	})

	t.Run("many headers totaling over 64KB", func(t *testing.T) {
		var builder strings.Builder
		builder.WriteString("From: sender@example.com\r\n")
		builder.WriteString("To: to@example.com\r\n")

		// Add many custom headers to exceed 64KB
		for i := 0; i < 2000; i++ {
			builder.WriteString("X-Custom-")
			builder.WriteString(strings.Repeat("A", 50))
			builder.WriteString(": value\r\n")
		}
		builder.WriteString("\r\n")

		got, err := ParseMessageHeaders(strings.NewReader(builder.String()))
		if err != nil {
			t.Fatalf("ParseMessageHeaders() unexpected error: %v", err)
		}

		// Should still parse what it can
		if got.From != "sender@example.com" {
			t.Errorf("From should still be parsed")
		}
	})
}

func TestParseMessageHeaders_ReaderError(t *testing.T) {
	t.Run("reader errors mid-stream", func(t *testing.T) {
		// Create a reader that errors after some bytes
		r := &errorReader{data: []byte("From: test@example.com\r\n"), errorAfter: 10}

		got, err := ParseMessageHeaders(r)
		// Should handle gracefully - returns empty metadata on error
		if err != nil {
			t.Errorf("ParseMessageHeaders() should not return error, got: %v", err)
		}
		if got == nil {
			t.Fatal("Should return non-nil metadata")
		}
	})

	t.Run("reader errors immediately", func(t *testing.T) {
		r := &errorReader{data: []byte("From: test@example.com\r\n"), errorAfter: 0}

		got, err := ParseMessageHeaders(r)
		if err != nil {
			t.Errorf("ParseMessageHeaders() should not return error, got: %v", err)
		}
		if got == nil {
			t.Fatal("Should return non-nil metadata")
		}
		// Should return empty metadata
		if got.From != "" {
			t.Errorf("From should be empty on read error, got %q", got.From)
		}
	})

	t.Run("EOF before blank line", func(t *testing.T) {
		input := "From: sender@example.com\r\nTo: to@example.com"
		got, err := ParseMessageHeaders(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseMessageHeaders() unexpected error: %v", err)
		}
		if got.From != "sender@example.com" {
			t.Errorf("From = %q, want sender@example.com", got.From)
		}
	})
}

type errorReader struct {
	data       []byte
	pos        int
	errorAfter int
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	if r.pos >= r.errorAfter {
		return 0, io.ErrUnexpectedEOF
	}
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	if r.pos >= r.errorAfter && r.errorAfter > 0 {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

func TestDecodeHeader(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text no encoding",
			input: "Plain text",
			want:  "Plain text",
		},
		{
			name:  "UTF-8 Base64 simple",
			input: "=?UTF-8?B?SGVsbG8=?=",
			want:  "Hello",
		},
		{
			name:  "UTF-8 Quoted-Printable with underscores",
			input: "=?UTF-8?Q?Hello_World?=",
			want:  "Hello World",
		},
		{
			name:  "ISO-8859-1 Quoted-Printable",
			input: "=?ISO-8859-1?Q?caf=E9?=",
			want:  "café",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no encoding marker",
			input: "No encoding here",
			want:  "No encoding here",
		},
		{
			name:  "invalid encoding type",
			input: "=?INVALID?X?broken?=",
			want:  "=?INVALID?X?broken?=", // Returns as-is
		},
		{
			name:  "incomplete encoded word",
			input: "=?UTF-8?B?incomplete",
			want:  "=?UTF-8?B?incomplete", // Returns as-is
		},
		{
			name:  "multiple encoded words",
			input: "=?UTF-8?B?SGVsbG8=?= =?UTF-8?B?V29ybGQ=?=",
			want:  "HelloWorld",
		},
		{
			name:  "mixed encoded and plain text",
			input: "Start =?UTF-8?B?TWlkZGxl?= End",
			want:  "Start Middle End",
		},
		{
			name:  "UTF-8 Base64 with special chars",
			input: "=?UTF-8?B?4p2k77iP?=",
			want:  "❤️",
		},
		{
			name:  "Quoted-Printable equals sign",
			input: "=?UTF-8?Q?Price=3D100?=",
			want:  "Price=100",
		},
		{
			name:  "Base64 empty",
			input: "=?UTF-8?B??=",
			want:  "",
		},
		{
			name:  "lowercase encoding type",
			input: "=?utf-8?b?SGVsbG8=?=",
			want:  "Hello",
		},
		{
			name:  "Windows-1252 encoding (unsupported, returned as-is)",
			input: "=?windows-1252?Q?Test?=",
			want:  "=?windows-1252?Q?Test?=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodeHeader(tt.input)
			if got != tt.want {
				t.Errorf("decodeHeader(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractEmailAddress(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple email",
			input: "simple@example.com",
			want:  "simple@example.com",
		},
		{
			name:  "email with display name in quotes",
			input: "\"John Doe\" <john@example.com>",
			want:  "john@example.com",
		},
		{
			name:  "email with brackets only",
			input: "<bracketed@example.com>",
			want:  "bracketed@example.com",
		},
		{
			name:  "email with display name no quotes",
			input: "John Doe <john@example.com>",
			want:  "john@example.com",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "not an email",
			input: "not-an-email",
			want:  "not-an-email",
		},
		{
			name:  "email with spaces",
			input: "   spaces@example.com   ",
			want:  "spaces@example.com",
		},
		{
			name:  "email with comment",
			input: "sender@example.com (Sender Name)",
			want:  "sender@example.com",
		},
		{
			name:  "complex RFC 5322 address",
			input: "\"Very Unusual Name\" <unusual@example.com>",
			want:  "unusual@example.com",
		},
		{
			name:  "email with unicode display name",
			input: "日本語 <japanese@example.com>",
			want:  "japanese@example.com",
		},
		{
			name:  "malformed with only brackets",
			input: "<>",
			want:  "<>",
		},
		{
			name:  "email with subdomain",
			input: "user@mail.example.com",
			want:  "user@mail.example.com",
		},
		{
			name:  "email with plus addressing",
			input: "user+tag@example.com",
			want:  "user+tag@example.com",
		},
		{
			name:  "email with hyphen in domain",
			input: "user@my-domain.com",
			want:  "user@my-domain.com",
		},
		{
			name:  "email with numbers",
			input: "user123@example456.com",
			want:  "user123@example456.com",
		},
		{
			name:  "email with dots in local part",
			input: "first.last@example.com",
			want:  "first.last@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractEmailAddress(tt.input)
			if got != tt.want {
				t.Errorf("extractEmailAddress(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseAddressList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single address",
			input: "user@example.com",
			want:  []string{"user@example.com"},
		},
		{
			name:  "two addresses",
			input: "user1@example.com, user2@example.com",
			want:  []string{"user1@example.com", "user2@example.com"},
		},
		{
			name:  "three addresses",
			input: "user1@example.com, user2@example.com, user3@example.com",
			want:  []string{"user1@example.com", "user2@example.com", "user3@example.com"},
		},
		{
			name:  "addresses with display names",
			input: "\"John\" <john@example.com>, \"Jane\" <jane@example.com>",
			want:  []string{"john@example.com", "jane@example.com"},
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "invalid address list",
			input: "invalid address list",
			want:  []string{"invalid address list"}, // Fallback
		},
		{
			name:  "mixed formats",
			input: "plain@example.com, \"Name\" <name@example.com>, <bracket@example.com>",
			want:  []string{"plain@example.com", "name@example.com", "bracket@example.com"},
		},
		{
			name:  "single address with display name",
			input: "John Doe <john@example.com>",
			want:  []string{"john@example.com"},
		},
		{
			name:  "addresses with spaces",
			input: "user1@example.com , user2@example.com , user3@example.com",
			want:  []string{"user1@example.com", "user2@example.com", "user3@example.com"},
		},
		{
			name:  "addresses with comments",
			input: "user1@example.com (User One), user2@example.com (User Two)",
			want:  []string{"user1@example.com", "user2@example.com"},
		},
		{
			name:  "address with semicolon separator - invalid",
			input: "user1@example.com; user2@example.com",
			want:  []string{"user1@example.com; user2@example.com"}, // Falls back to raw
		},
		{
			name:  "only commas",
			input: ", , ,",
			want:  []string{", , ,"}, // Fallback
		},
		{
			name:  "addresses with unicode display names",
			input: "日本語 <jp@example.com>, Français <fr@example.com>",
			want:  []string{"jp@example.com", "fr@example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAddressList(tt.input)
			if !stringSliceEqual(got, tt.want) {
				t.Errorf("parseAddressList(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCleanHeader(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "value with angle brackets",
			input: "<value>",
			want:  "value",
		},
		{
			name:  "value with brackets and spaces",
			input: "  <value>  ",
			want:  "value",
		},
		{
			name:  "plain value",
			input: "value",
			want:  "value",
		},
		{
			name:  "empty brackets",
			input: "<>",
			want:  "",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only spaces",
			input: "  ",
			want:  "",
		},
		{
			name:  "no closing bracket",
			input: "<no-close",
			want:  "no-close",
		},
		{
			name:  "no opening bracket",
			input: "no-open>",
			want:  "no-open",
		},
		{
			name:  "multiple brackets",
			input: "<<value>>",
			want:  "<value>",
		},
		{
			name:  "message ID",
			input: "<abc123@example.com>",
			want:  "abc123@example.com",
		},
		{
			name:  "leading spaces no brackets",
			input: "  value",
			want:  "value",
		},
		{
			name:  "trailing spaces no brackets",
			input: "value  ",
			want:  "value",
		},
		{
			name:  "tabs and spaces",
			input: "\t <value> \t",
			want:  "value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanHeader(tt.input)
			if got != tt.want {
				t.Errorf("cleanHeader(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseMessageHeaders_EdgeCases(t *testing.T) {
	t.Run("null bytes in header", func(t *testing.T) {
		input := "From: sender@example.com\r\nSubject: Test\x00Null\r\n\r\n"
		got, err := ParseMessageHeaders(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseMessageHeaders() error: %v", err)
		}
		// Should handle gracefully
		if got == nil {
			t.Fatal("Expected non-nil metadata")
		}
	})

	t.Run("very long single line", func(t *testing.T) {
		longSubject := strings.Repeat("A", 10000)
		input := "From: sender@example.com\r\nSubject: " + longSubject + "\r\n\r\n"
		got, err := ParseMessageHeaders(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseMessageHeaders() error: %v", err)
		}
		if got.From != "sender@example.com" {
			t.Errorf("From = %q, want sender@example.com", got.From)
		}
	})

	t.Run("headers with no colon", func(t *testing.T) {
		input := "From: sender@example.com\r\nInvalidHeaderLine\r\nSubject: Test\r\n\r\n"
		got, err := ParseMessageHeaders(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseMessageHeaders() error: %v", err)
		}
		// Parser behavior with malformed headers is implementation-defined
		// Just verify no crash occurs
		_ = got
	})

	t.Run("only body no headers", func(t *testing.T) {
		input := "\r\nThis is just body content"
		got, err := ParseMessageHeaders(strings.NewReader(input))
		if err != nil {
			t.Fatalf("ParseMessageHeaders() error: %v", err)
		}
		if got.From != "" {
			t.Errorf("From should be empty, got %q", got.From)
		}
	})
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func BenchmarkParseMessageHeaders(b *testing.B) {
	input := "From: \"John Doe\" <john@example.com>\r\n" +
		"To: recipient1@example.com, recipient2@example.com\r\n" +
		"Cc: cc@example.com\r\n" +
		"Subject: =?UTF-8?B?VGVzdCBTdWJqZWN0?=\r\n" +
		"Message-ID: <12345@example.com>\r\n" +
		"Date: Mon, 20 Dec 2025 10:00:00 -0500\r\n" +
		"In-Reply-To: <original@example.com>\r\n" +
		"\r\n" +
		"Body content here that should be ignored"

	data := []byte(input)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseMessageHeaders(bytes.NewReader(data))
	}
}

func BenchmarkParseMessageHeaders_Simple(b *testing.B) {
	input := "From: sender@example.com\r\nTo: to@example.com\r\nSubject: Test\r\n\r\n"
	data := []byte(input)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseMessageHeaders(bytes.NewReader(data))
	}
}

func BenchmarkParseMessageHeaders_LargeHeaders(b *testing.B) {
	var builder strings.Builder
	builder.WriteString("From: sender@example.com\r\n")
	builder.WriteString("To: ")
	for i := 0; i < 100; i++ {
		if i > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString("user")
		builder.WriteString(string(rune('0' + (i % 10))))
		builder.WriteString("@example.com")
	}
	builder.WriteString("\r\nSubject: Test\r\n\r\n")

	data := []byte(builder.String())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ParseMessageHeaders(bytes.NewReader(data))
	}
}

func BenchmarkDecodeHeader(b *testing.B) {
	tests := []struct {
		name  string
		input string
	}{
		{"plain", "Plain text subject"},
		{"utf8_base64", "=?UTF-8?B?VGhpcyBpcyBhIHRlc3Qgc3ViamVjdA==?="},
		{"utf8_qp", "=?UTF-8?Q?This_is_a_test_subject?="},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				decodeHeader(tt.input)
			}
		})
	}
}

func BenchmarkExtractEmailAddress(b *testing.B) {
	tests := []struct {
		name  string
		input string
	}{
		{"plain", "user@example.com"},
		{"with_name", "\"John Doe\" <john@example.com>"},
		{"brackets", "<user@example.com>"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				extractEmailAddress(tt.input)
			}
		})
	}
}

func BenchmarkParseAddressList(b *testing.B) {
	tests := []struct {
		name  string
		input string
	}{
		{"single", "user@example.com"},
		{"multiple", "user1@example.com, user2@example.com, user3@example.com"},
		{"with_names", "\"User One\" <user1@example.com>, \"User Two\" <user2@example.com>"},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				parseAddressList(tt.input)
			}
		})
	}
}
