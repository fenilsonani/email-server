package api

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildEmailMessage_NoAttachments(t *testing.T) {
	msg := buildEmailMessage(
		"<test@example.com>",
		"sender@example.com",
		"recipient@example.com",
		"Test Subject",
		"<html><body>Hello</body></html>",
		"Hello",
		"",
		nil,
		nil,
	)

	// Should use multipart/alternative for text+HTML
	if !strings.Contains(msg, "multipart/alternative") {
		t.Error("Expected multipart/alternative for text+HTML message")
	}

	// Should NOT use multipart/mixed (no attachments)
	if strings.Contains(msg, "multipart/mixed") {
		t.Error("Should not use multipart/mixed without attachments")
	}

	// Check headers
	if !strings.Contains(msg, "From: sender@example.com") {
		t.Error("Missing From header")
	}
	if !strings.Contains(msg, "To: recipient@example.com") {
		t.Error("Missing To header")
	}
	if !strings.Contains(msg, "Subject: Test Subject") {
		t.Error("Missing Subject header")
	}
}

func TestBuildEmailMessage_WithAttachments(t *testing.T) {
	// Create a simple test attachment (base64 encoded "Hello World")
	content := base64.StdEncoding.EncodeToString([]byte("Hello World"))

	attachments := []Attachment{
		{
			Filename:    "test.txt",
			Content:     content,
			ContentType: "text/plain",
		},
	}

	msg := buildEmailMessage(
		"<test@example.com>",
		"sender@example.com",
		"recipient@example.com",
		"Test Subject",
		"<html><body>Hello</body></html>",
		"Hello",
		"",
		nil,
		attachments,
	)

	// Should use multipart/mixed as outer container
	if !strings.Contains(msg, "multipart/mixed") {
		t.Error("Expected multipart/mixed for message with attachments")
	}

	// Should have nested multipart/alternative for text+HTML
	if !strings.Contains(msg, "multipart/alternative") {
		t.Error("Expected nested multipart/alternative for text+HTML")
	}

	// Check attachment headers
	if !strings.Contains(msg, "Content-Disposition: attachment; filename=\"test.txt\"") {
		t.Error("Missing attachment Content-Disposition header")
	}
	if !strings.Contains(msg, "Content-Type: text/plain; name=\"test.txt\"") {
		t.Error("Missing attachment Content-Type header")
	}
	if !strings.Contains(msg, "Content-Transfer-Encoding: base64") {
		t.Error("Missing base64 transfer encoding")
	}

	// Check that attachment content is present
	if !strings.Contains(msg, content) {
		t.Error("Attachment content not found in message")
	}
}

func TestBuildEmailMessage_MultipleAttachments(t *testing.T) {
	attachments := []Attachment{
		{
			Filename:    "doc1.pdf",
			Content:     base64.StdEncoding.EncodeToString([]byte("PDF content")),
			ContentType: "application/pdf",
		},
		{
			Filename:    "image.png",
			Content:     base64.StdEncoding.EncodeToString([]byte("PNG content")),
			ContentType: "image/png",
		},
	}

	msg := buildEmailMessage(
		"<test@example.com>",
		"sender@example.com",
		"recipient@example.com",
		"Test Subject",
		"",
		"Hello",
		"",
		nil,
		attachments,
	)

	// Check both attachments are present
	if !strings.Contains(msg, "filename=\"doc1.pdf\"") {
		t.Error("First attachment not found")
	}
	if !strings.Contains(msg, "filename=\"image.png\"") {
		t.Error("Second attachment not found")
	}
	if !strings.Contains(msg, "application/pdf") {
		t.Error("PDF content type not found")
	}
	if !strings.Contains(msg, "image/png") {
		t.Error("PNG content type not found")
	}
}

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"document.pdf", "application/pdf"},
		{"image.jpg", "image/jpeg"},
		{"image.jpeg", "image/jpeg"},
		{"image.png", "image/png"},
		{"image.gif", "image/gif"},
		{"doc.doc", "application/msword"},
		{"doc.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"sheet.xls", "application/vnd.ms-excel"},
		{"sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"archive.zip", "application/zip"},
		{"file.txt", "text/plain"},
		{"data.csv", "text/csv"},
		{"data.json", "application/json"},
		{"unknown.xyz", "application/octet-stream"},
		{"UPPERCASE.PDF", "application/pdf"}, // Case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := detectContentType(tt.filename)
			if result != tt.expected {
				t.Errorf("detectContentType(%q) = %q, want %q", tt.filename, result, tt.expected)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal.txt", "normal.txt"},
		{"file with spaces.pdf", "file with spaces.pdf"},
		{"../../../etc/passwd", "passwd"},                // Path traversal
		{"/etc/passwd", "passwd"},                        // Absolute path
		{"file\"with\"quotes.txt", "filewithquotes.txt"}, // Quotes removed
		{"file\r\ninjection.txt", "fileinjection.txt"},   // Newlines removed
		{"file\ninjection.txt", "fileinjection.txt"},     // LF removed
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeFilename(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBuildEmailMessage_AttachmentSecurityHeaders(t *testing.T) {
	// Test that malicious filenames are sanitized
	attachments := []Attachment{
		{
			Filename:    "../../../etc/passwd",
			Content:     base64.StdEncoding.EncodeToString([]byte("test")),
			ContentType: "text/plain",
		},
	}

	msg := buildEmailMessage(
		"<test@example.com>",
		"sender@example.com",
		"recipient@example.com",
		"Test Subject",
		"",
		"Hello",
		"",
		nil,
		attachments,
	)

	// Should have sanitized filename
	if strings.Contains(msg, "../") {
		t.Error("Path traversal not sanitized")
	}
	if !strings.Contains(msg, "filename=\"passwd\"") {
		t.Error("Filename not properly sanitized")
	}
}

func TestBuildEmailMessage_HTMLOnlyWithAttachment(t *testing.T) {
	attachments := []Attachment{
		{
			Filename: "test.txt",
			Content:  base64.StdEncoding.EncodeToString([]byte("test")),
		},
	}

	msg := buildEmailMessage(
		"<test@example.com>",
		"sender@example.com",
		"recipient@example.com",
		"Test Subject",
		"<html><body>Hello</body></html>",
		"", // No text
		"",
		nil,
		attachments,
	)

	// Should use multipart/mixed
	if !strings.Contains(msg, "multipart/mixed") {
		t.Error("Expected multipart/mixed")
	}

	// Should NOT have nested multipart/alternative (only HTML, no text)
	// The body part should be text/html directly
	if strings.Contains(msg, "multipart/alternative") {
		t.Error("Should not have multipart/alternative with only HTML body")
	}
}

func TestBuildEmailMessage_TextOnlyWithAttachment(t *testing.T) {
	attachments := []Attachment{
		{
			Filename: "test.txt",
			Content:  base64.StdEncoding.EncodeToString([]byte("test")),
		},
	}

	msg := buildEmailMessage(
		"<test@example.com>",
		"sender@example.com",
		"recipient@example.com",
		"Test Subject",
		"", // No HTML
		"Hello plain text",
		"",
		nil,
		attachments,
	)

	// Should use multipart/mixed
	if !strings.Contains(msg, "multipart/mixed") {
		t.Error("Expected multipart/mixed")
	}

	// Body should be text/plain
	if !strings.Contains(msg, "text/plain") {
		t.Error("Expected text/plain content type")
	}
}

func TestBuildEmailMessage_AutoDetectContentType(t *testing.T) {
	// Test with empty ContentType - should auto-detect
	attachments := []Attachment{
		{
			Filename:    "document.pdf",
			Content:     base64.StdEncoding.EncodeToString([]byte("test")),
			ContentType: "", // Should auto-detect as application/pdf
		},
	}

	msg := buildEmailMessage(
		"<test@example.com>",
		"sender@example.com",
		"recipient@example.com",
		"Test Subject",
		"",
		"Hello",
		"",
		nil,
		attachments,
	)

	if !strings.Contains(msg, "Content-Type: application/pdf") {
		t.Error("Content-Type not auto-detected for PDF")
	}
}

func TestGenerateBoundary(t *testing.T) {
	// Generate multiple boundaries and ensure they're unique
	boundaries := make(map[string]bool)
	for i := 0; i < 100; i++ {
		b := generateBoundary()
		if boundaries[b] {
			t.Error("Generated duplicate boundary")
		}
		boundaries[b] = true

		// Check format
		if !strings.HasPrefix(b, "=_") {
			t.Errorf("Boundary should start with =_, got %q", b)
		}
	}
}

func TestValidateAttachments_BlockedExtensionsWithConfig(t *testing.T) {
	// Test with configured blocked extensions
	blockedExtensions := []string{".exe", ".bat", ".cmd"}

	blockedFiles := []string{"virus.exe", "script.bat", "hack.cmd"}

	for _, filename := range blockedFiles {
		t.Run(filename, func(t *testing.T) {
			attachments := []Attachment{
				{
					Filename: filename,
					Content:  base64.StdEncoding.EncodeToString([]byte("test")),
				},
			}

			err := validateAttachmentsWithConfig(attachments, blockedExtensions)
			if err == nil {
				t.Errorf("Expected error for blocked extension: %s", filename)
			}
			if !strings.Contains(err.Error(), "not allowed") {
				t.Errorf("Expected 'not allowed' error, got: %v", err)
			}
		})
	}
}

func TestValidateAttachments_NoBlockedExtensionsByDefault(t *testing.T) {
	// Without config, all extensions should be allowed
	attachments := []Attachment{
		{
			Filename: "script.exe",
			Content:  base64.StdEncoding.EncodeToString([]byte("test")),
		},
	}

	err := validateAttachments(attachments)
	if err != nil {
		t.Errorf("Should allow .exe by default without config, got: %v", err)
	}
}

func TestValidateAttachments_AllowedExtensions(t *testing.T) {
	allowedFiles := []string{
		"document.pdf", "image.jpg", "photo.png", "archive.zip",
		"spreadsheet.xlsx", "document.docx", "data.csv", "text.txt",
	}

	for _, filename := range allowedFiles {
		t.Run(filename, func(t *testing.T) {
			attachments := []Attachment{
				{
					Filename: filename,
					Content:  base64.StdEncoding.EncodeToString([]byte("test")),
				},
			}

			err := validateAttachments(attachments)
			if err != nil {
				t.Errorf("Unexpected error for allowed extension %s: %v", filename, err)
			}
		})
	}
}

func TestValidateAttachments_InvalidBase64(t *testing.T) {
	attachments := []Attachment{
		{
			Filename: "test.txt",
			Content:  "this is not valid base64!!!",
		},
	}

	err := validateAttachments(attachments)
	if err == nil {
		t.Error("Expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "invalid base64") {
		t.Errorf("Expected 'invalid base64' error, got: %v", err)
	}
}

func TestValidateAttachments_MissingFilename(t *testing.T) {
	attachments := []Attachment{
		{
			Filename: "",
			Content:  base64.StdEncoding.EncodeToString([]byte("test")),
		},
	}

	err := validateAttachments(attachments)
	if err == nil {
		t.Error("Expected error for missing filename")
	}
	if !strings.Contains(err.Error(), "filename is required") {
		t.Errorf("Expected 'filename is required' error, got: %v", err)
	}
}

func TestValidateAttachments_MissingContent(t *testing.T) {
	attachments := []Attachment{
		{
			Filename: "test.txt",
			Content:  "",
		},
	}

	err := validateAttachments(attachments)
	if err == nil {
		t.Error("Expected error for missing content")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Errorf("Expected 'content is required' error, got: %v", err)
	}
}

func TestValidateAttachments_TooManyAttachments(t *testing.T) {
	attachments := make([]Attachment, 11) // More than maxAttachmentCount
	for i := range attachments {
		attachments[i] = Attachment{
			Filename: "test.txt",
			Content:  base64.StdEncoding.EncodeToString([]byte("test")),
		}
	}

	err := validateAttachments(attachments)
	if err == nil {
		t.Error("Expected error for too many attachments")
	}
	if !strings.Contains(err.Error(), "too many attachments") {
		t.Errorf("Expected 'too many attachments' error, got: %v", err)
	}
}

func TestIsValidContentType(t *testing.T) {
	tests := []struct {
		contentType string
		valid       bool
	}{
		{"application/pdf", true},
		{"image/jpeg", true},
		{"text/plain", true},
		{"application/octet-stream", true},
		{"application/x-msdownload", true},  // Allowed (no blocking by default)
		{"application/x-executable", true},  // Allowed
		{"text/javascript", true},           // Allowed
		{"invalid", false},                  // No slash - invalid format
		{"", false},                         // Empty - invalid format
		{"/subtype", false},                 // Missing type
		{"type/", false},                    // Missing subtype
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			result := isValidContentType(tt.contentType)
			if result != tt.valid {
				t.Errorf("isValidContentType(%q) = %v, want %v", tt.contentType, result, tt.valid)
			}
		})
	}
}
