package delivery

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fenilsonani/email-server/internal/queue"
)

func TestNewBounceGenerator(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		wantPost string
	}{
		{"normal hostname", "mail.example.com", "postmaster@mail.example.com"},
		{"simple hostname", "localhost", "postmaster@localhost"},
		{"subdomain", "smtp.mail.example.com", "postmaster@smtp.mail.example.com"},
		{"empty hostname", "", "postmaster@"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bg := NewBounceGenerator(tt.hostname)
			if bg.postmaster != tt.wantPost {
				t.Errorf("postmaster = %v, want %v", bg.postmaster, tt.wantPost)
			}
			if bg.hostname != tt.hostname {
				t.Errorf("hostname = %v, want %v", bg.hostname, tt.hostname)
			}
			if bg.template == nil {
				t.Error("template should not be nil")
			}
		})
	}
}

func TestBounceGenerator_Generate(t *testing.T) {
	bg := NewBounceGenerator("mail.example.com")

	tests := []struct {
		name        string
		msg         *queue.Message
		err         error
		wantContain []string
		wantErr     bool
	}{
		{
			name: "basic bounce",
			msg: &queue.Message{
				Sender:     "sender@example.com",
				Recipients: []string{"recipient@other.com"},
			},
			err: errors.New("550 User not found"),
			wantContain: []string{
				"From: Mail Delivery System",
				"To: <sender@example.com>",
				"Subject: Undelivered Mail Returned to Sender",
				"recipient@other.com",
				"550 User not found",
				"Status: 5.1.1",
			},
		},
		{
			name: "multiple recipients",
			msg: &queue.Message{
				Sender:     "sender@example.com",
				Recipients: []string{"user1@other.com", "user2@other.com", "user3@other.com"},
			},
			err: errors.New("552 Mailbox full"),
			wantContain: []string{
				"user1@other.com, user2@other.com, user3@other.com",
				"Status: 5.2.2",
			},
		},
		{
			name: "553 error code",
			msg: &queue.Message{
				Sender:     "sender@example.com",
				Recipients: []string{"bad-syntax"},
			},
			err: errors.New("553 Bad mailbox syntax"),
			wantContain: []string{
				"Status: 5.1.3",
			},
		},
		{
			name: "554 error code",
			msg: &queue.Message{
				Sender:     "sender@example.com",
				Recipients: []string{"blocked@example.com"},
			},
			err: errors.New("554 Delivery not authorized"),
			wantContain: []string{
				"Status: 5.7.1",
			},
		},
		{
			name: "551 error code",
			msg: &queue.Message{
				Sender:     "sender@example.com",
				Recipients: []string{"moved@example.com"},
			},
			err: errors.New("551 User moved"),
			wantContain: []string{
				"Status: 5.1.6",
			},
		},
		{
			name: "unknown error code",
			msg: &queue.Message{
				Sender:     "sender@example.com",
				Recipients: []string{"user@example.com"},
			},
			err: errors.New("Connection timeout"),
			wantContain: []string{
				"Status: 5.0.0",
			},
		},
		{
			name: "empty sender",
			msg: &queue.Message{
				Sender:     "",
				Recipients: []string{"user@example.com"},
			},
			err: errors.New("550 Error"),
			wantContain: []string{
				"To: <>",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := bg.Generate(tt.msg, tt.err)
			if (err != nil) != tt.wantErr {
				t.Errorf("Generate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			body := string(result)
			for _, want := range tt.wantContain {
				if !strings.Contains(body, want) {
					t.Errorf("Generate() missing %q in output:\n%s", want, body)
				}
			}

			// Verify MIME structure
			if !strings.Contains(body, "MIME-Version: 1.0") {
				t.Error("Missing MIME-Version header")
			}
			if !strings.Contains(body, "multipart/report") {
				t.Error("Missing multipart/report content type")
			}
			if !strings.Contains(body, "Auto-Submitted: auto-replied") {
				t.Error("Missing Auto-Submitted header")
			}
		})
	}
}

func TestBounceGenerator_GenerateWithMessageFile(t *testing.T) {
	bg := NewBounceGenerator("mail.example.com")

	// Create temp message file
	tmpDir := t.TempDir()
	msgPath := filepath.Join(tmpDir, "test.eml")
	msgContent := "From: original@sender.com\r\nTo: recipient@example.com\r\nSubject: Test Subject\r\nMessage-ID: <123@test>\r\n\r\nBody content here"
	if err := os.WriteFile(msgPath, []byte(msgContent), 0644); err != nil {
		t.Fatal(err)
	}

	msg := &queue.Message{
		Sender:      "original@sender.com",
		Recipients:  []string{"recipient@example.com"},
		MessagePath: msgPath,
	}

	result, err := bg.Generate(msg, errors.New("550 User not found"))
	if err != nil {
		t.Fatal(err)
	}

	body := string(result)

	// Should contain original headers
	if !strings.Contains(body, "From: original@sender.com") {
		t.Error("Missing original From header in bounce")
	}
	if !strings.Contains(body, "Subject: Test Subject") {
		t.Error("Missing original Subject header in bounce")
	}
}

func TestBounceGenerator_GenerateWithLargeHeaders(t *testing.T) {
	bg := NewBounceGenerator("mail.example.com")

	// Create temp message file with very large headers
	tmpDir := t.TempDir()
	msgPath := filepath.Join(tmpDir, "large.eml")

	// Create headers larger than 4KB limit
	largeSubject := strings.Repeat("X", 5000)
	msgContent := "From: sender@example.com\r\nSubject: " + largeSubject + "\r\n\r\nBody"
	if err := os.WriteFile(msgPath, []byte(msgContent), 0644); err != nil {
		t.Fatal(err)
	}

	msg := &queue.Message{
		Sender:      "sender@example.com",
		Recipients:  []string{"recipient@example.com"},
		MessagePath: msgPath,
	}

	result, err := bg.Generate(msg, errors.New("550 Error"))
	if err != nil {
		t.Fatal(err)
	}

	body := string(result)

	// Should contain truncation marker
	if !strings.Contains(body, "[... truncated ...]") {
		t.Error("Large headers should be truncated")
	}
}

func TestBounceGenerator_GenerateMissingFile(t *testing.T) {
	bg := NewBounceGenerator("mail.example.com")

	msg := &queue.Message{
		Sender:      "sender@example.com",
		Recipients:  []string{"recipient@example.com"},
		MessagePath: "/nonexistent/path/message.eml",
	}

	// Should not fail, just skip original headers
	result, err := bg.Generate(msg, errors.New("550 Error"))
	if err != nil {
		t.Fatal(err)
	}

	if len(result) == 0 {
		t.Error("Should generate bounce even without message file")
	}
}

func TestShouldBounce(t *testing.T) {
	tests := []struct {
		sender string
		want   bool
	}{
		// Should NOT bounce
		{"", false},                        // null sender
		{"postmaster@example.com", false},  // postmaster
		{"POSTMASTER@example.com", false},  // case insensitive
		{"mailer-daemon@example.com", false},
		{"MAILER-DAEMON@example.com", false},
		{"noreply@example.com", false},
		{"no-reply@example.com", false},
		{"NoReply@example.com", false},
		{"No-Reply@example.com", false},

		// Should bounce
		{"user@example.com", true},
		{"admin@example.com", true},
		{"support@example.com", true},
		{"postmaster-backup@example.com", true}, // not a prefix
		{"john.postmaster@example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.sender, func(t *testing.T) {
			got := ShouldBounce(tt.sender)
			if got != tt.want {
				t.Errorf("ShouldBounce(%q) = %v, want %v", tt.sender, got, tt.want)
			}
		})
	}
}

func TestClassifyErrorCode(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, "5.0.0"},
		{errors.New("550 User not found"), "5.1.1"},
		{errors.New("551 User moved"), "5.1.6"},
		{errors.New("552 Mailbox full"), "5.2.2"},
		{errors.New("553 Invalid mailbox"), "5.1.3"},
		{errors.New("554 Transaction failed"), "5.7.1"},
		{errors.New("421 Service unavailable"), "5.0.0"},
		{errors.New("Connection refused"), "5.0.0"},
		{errors.New("timeout"), "5.0.0"},
	}

	for _, tt := range tests {
		name := "nil"
		if tt.err != nil {
			name = tt.err.Error()
		}
		t.Run(name, func(t *testing.T) {
			got := classifyErrorCode(tt.err)
			if got != tt.want {
				t.Errorf("classifyErrorCode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBounceGenerator_MessageIDUniqueness(t *testing.T) {
	bg := NewBounceGenerator("mail.example.com")
	msg := &queue.Message{
		Sender:     "sender@example.com",
		Recipients: []string{"recipient@example.com"},
	}

	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		result, err := bg.Generate(msg, errors.New("550 Error"))
		if err != nil {
			t.Fatal(err)
		}

		// Extract Message-ID
		body := string(result)
		start := strings.Index(body, "Message-ID: ")
		if start == -1 {
			t.Fatal("Missing Message-ID")
		}
		end := strings.Index(body[start:], "\n")
		msgID := body[start : start+end]

		if ids[msgID] {
			t.Errorf("Duplicate Message-ID: %s", msgID)
		}
		ids[msgID] = true
	}
}
