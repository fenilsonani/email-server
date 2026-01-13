package security

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"
)

func generateARCTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	return key
}

func TestNewARCSigner(t *testing.T) {
	key := generateARCTestKey(t)

	t.Run("with authServID", func(t *testing.T) {
		signer := NewARCSigner("example.com", "arc", key, "mail.example.com")
		if signer.domain != "example.com" {
			t.Errorf("domain = %q, want %q", signer.domain, "example.com")
		}
		if signer.selector != "arc" {
			t.Errorf("selector = %q, want %q", signer.selector, "arc")
		}
		if signer.authServID != "mail.example.com" {
			t.Errorf("authServID = %q, want %q", signer.authServID, "mail.example.com")
		}
	})

	t.Run("without authServID defaults to domain", func(t *testing.T) {
		signer := NewARCSigner("example.com", "arc", key, "")
		if signer.authServID != "example.com" {
			t.Errorf("authServID = %q, want %q", signer.authServID, "example.com")
		}
	})
}

func TestARCSignerPool(t *testing.T) {
	pool := NewARCSignerPool()
	key := generateARCTestKey(t)

	t.Run("add and get signer", func(t *testing.T) {
		pool.AddSigner("example.com", "arc", key, "mail.example.com")

		signer := pool.GetSigner("example.com")
		if signer == nil {
			t.Fatal("expected signer, got nil")
		}
		if signer.domain != "example.com" {
			t.Errorf("domain = %q, want %q", signer.domain, "example.com")
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		signer := pool.GetSigner("EXAMPLE.COM")
		if signer == nil {
			t.Fatal("expected signer for uppercase domain, got nil")
		}
	})

	t.Run("non-existent domain", func(t *testing.T) {
		signer := pool.GetSigner("nonexistent.com")
		if signer != nil {
			t.Error("expected nil for non-existent domain")
		}
	})
}

func TestARCSigner_Sign(t *testing.T) {
	key := generateARCTestKey(t)
	signer := NewARCSigner("example.com", "arc", key, "mail.example.com")

	message := []byte("From: sender@example.com\r\n" +
		"To: recipient@example.org\r\n" +
		"Subject: Test\r\n" +
		"Date: Mon, 1 Jan 2024 00:00:00 +0000\r\n" +
		"Message-ID: <test@example.com>\r\n" +
		"\r\n" +
		"This is a test message.\r\n")

	t.Run("first instance", func(t *testing.T) {
		opts := ARCSignOptions{
			Instance:        1,
			ChainValidation: ARCChainNone,
			Timestamp:       time.Unix(1704067200, 0),
		}

		signed, err := signer.Sign(message, opts)
		if err != nil {
			t.Fatalf("Sign() error: %v", err)
		}

		signedStr := string(signed)

		// Check ARC-Seal header
		if !strings.Contains(signedStr, "ARC-Seal:") {
			t.Error("missing ARC-Seal header")
		}
		if !strings.Contains(signedStr, "i=1") {
			t.Error("ARC-Seal missing i=1")
		}
		if !strings.Contains(signedStr, "cv=none") {
			t.Error("ARC-Seal missing cv=none")
		}

		// Check ARC-Message-Signature header
		if !strings.Contains(signedStr, "ARC-Message-Signature:") {
			t.Error("missing ARC-Message-Signature header")
		}
		if !strings.Contains(signedStr, "d=example.com") {
			t.Error("ARC-Message-Signature missing domain")
		}
		if !strings.Contains(signedStr, "s=arc") {
			t.Error("ARC-Message-Signature missing selector")
		}

		// Check ARC-Authentication-Results header
		if !strings.Contains(signedStr, "ARC-Authentication-Results:") {
			t.Error("missing ARC-Authentication-Results header")
		}

		// Verify original message is preserved
		if !strings.Contains(signedStr, "This is a test message.") {
			t.Error("original message body not preserved")
		}
	})

	t.Run("second instance with pass", func(t *testing.T) {
		opts := ARCSignOptions{
			Instance:        2,
			ChainValidation: ARCChainPass,
			Timestamp:       time.Unix(1704067200, 0),
		}

		signed, err := signer.Sign(message, opts)
		if err != nil {
			t.Fatalf("Sign() error: %v", err)
		}

		signedStr := string(signed)
		if !strings.Contains(signedStr, "i=2") {
			t.Error("missing i=2")
		}
		if !strings.Contains(signedStr, "cv=pass") {
			t.Error("missing cv=pass")
		}
	})

	t.Run("with auth results", func(t *testing.T) {
		opts := ARCSignOptions{
			Instance:        1,
			ChainValidation: ARCChainNone,
			AuthResults: &AuthenticationResults{
				AuthServID: "mail.example.com",
				SPF:        &SPFResult{Result: ResultPass, Domain: "example.com"},
				DKIM:       &DKIMResult{Result: ResultPass, Domain: "example.com", Selector: "mail"},
			},
		}

		signed, err := signer.Sign(message, opts)
		if err != nil {
			t.Fatalf("Sign() error: %v", err)
		}

		signedStr := string(signed)
		if !strings.Contains(signedStr, "spf=pass") {
			t.Error("missing SPF result in ARC-Authentication-Results")
		}
		if !strings.Contains(signedStr, "dkim=pass") {
			t.Error("missing DKIM result in ARC-Authentication-Results")
		}
	})

	t.Run("invalid instance 2 with none chain validation", func(t *testing.T) {
		opts := ARCSignOptions{
			Instance:        2,
			ChainValidation: ARCChainNone,
		}

		_, err := signer.Sign(message, opts)
		if err == nil {
			t.Error("expected error for instance 2 with cv=none")
		}
	})
}

func TestExtractARCSets(t *testing.T) {
	t.Run("no ARC headers", func(t *testing.T) {
		message := []byte("From: sender@example.com\r\n" +
			"To: recipient@example.org\r\n" +
			"\r\n" +
			"Body\r\n")

		sets, err := ExtractARCSets(message)
		if err != nil {
			t.Fatalf("ExtractARCSets() error: %v", err)
		}
		if len(sets) != 0 {
			t.Errorf("expected 0 sets, got %d", len(sets))
		}
	})

	t.Run("single ARC set", func(t *testing.T) {
		message := []byte("ARC-Seal: i=1; a=rsa-sha256; cv=none; d=example.com; s=arc; b=abc\r\n" +
			"ARC-Message-Signature: i=1; a=rsa-sha256; d=example.com; s=arc; h=from:to; bh=xyz; b=def\r\n" +
			"ARC-Authentication-Results: i=1; example.com; spf=pass\r\n" +
			"From: sender@example.com\r\n" +
			"\r\n" +
			"Body\r\n")

		sets, err := ExtractARCSets(message)
		if err != nil {
			t.Fatalf("ExtractARCSets() error: %v", err)
		}
		if len(sets) != 1 {
			t.Fatalf("expected 1 set, got %d", len(sets))
		}
		if sets[0].Instance != 1 {
			t.Errorf("Instance = %d, want 1", sets[0].Instance)
		}
		if sets[0].Seal == "" {
			t.Error("Seal should not be empty")
		}
		if sets[0].MessageSignature == "" {
			t.Error("MessageSignature should not be empty")
		}
		if sets[0].AuthenticationResults == "" {
			t.Error("AuthenticationResults should not be empty")
		}
	})

	t.Run("multiple ARC sets", func(t *testing.T) {
		message := []byte("ARC-Seal: i=2; a=rsa-sha256; cv=pass; d=example.org; s=arc; b=ghi\r\n" +
			"ARC-Message-Signature: i=2; a=rsa-sha256; d=example.org; s=arc; h=from:to; bh=uvw; b=jkl\r\n" +
			"ARC-Authentication-Results: i=2; example.org; arc=pass\r\n" +
			"ARC-Seal: i=1; a=rsa-sha256; cv=none; d=example.com; s=arc; b=abc\r\n" +
			"ARC-Message-Signature: i=1; a=rsa-sha256; d=example.com; s=arc; h=from:to; bh=xyz; b=def\r\n" +
			"ARC-Authentication-Results: i=1; example.com; spf=pass\r\n" +
			"From: sender@example.com\r\n" +
			"\r\n" +
			"Body\r\n")

		sets, err := ExtractARCSets(message)
		if err != nil {
			t.Fatalf("ExtractARCSets() error: %v", err)
		}
		if len(sets) != 2 {
			t.Fatalf("expected 2 sets, got %d", len(sets))
		}
	})
}

func TestGetNextInstance(t *testing.T) {
	t.Run("no existing ARC", func(t *testing.T) {
		message := []byte("From: sender@example.com\r\n\r\nBody\r\n")
		next := GetNextInstance(message)
		if next != 1 {
			t.Errorf("GetNextInstance() = %d, want 1", next)
		}
	})

	t.Run("with existing ARC", func(t *testing.T) {
		message := []byte("ARC-Seal: i=1; cv=none; b=abc\r\n" +
			"ARC-Message-Signature: i=1; b=def\r\n" +
			"ARC-Authentication-Results: i=1; example.com\r\n" +
			"From: sender@example.com\r\n\r\nBody\r\n")
		next := GetNextInstance(message)
		if next != 2 {
			t.Errorf("GetNextInstance() = %d, want 2", next)
		}
	})
}

func TestValidateARCChain(t *testing.T) {
	t.Run("no ARC headers", func(t *testing.T) {
		message := []byte("From: sender@example.com\r\n\r\nBody\r\n")
		result := ValidateARCChain(message)
		if result != ARCChainNone {
			t.Errorf("ValidateARCChain() = %v, want %v", result, ARCChainNone)
		}
	})

	t.Run("complete ARC set with cv=none", func(t *testing.T) {
		message := []byte("ARC-Seal: i=1; a=rsa-sha256; cv=none; d=example.com; s=arc; b=abc\r\n" +
			"ARC-Message-Signature: i=1; a=rsa-sha256; d=example.com; s=arc; b=def\r\n" +
			"ARC-Authentication-Results: i=1; example.com; spf=pass\r\n" +
			"From: sender@example.com\r\n\r\nBody\r\n")
		result := ValidateARCChain(message)
		if result != ARCChainPass {
			t.Errorf("ValidateARCChain() = %v, want %v", result, ARCChainPass)
		}
	})

	t.Run("ARC set with cv=pass", func(t *testing.T) {
		message := []byte("ARC-Seal: i=2; a=rsa-sha256; cv=pass; d=example.org; s=arc; b=ghi\r\n" +
			"ARC-Message-Signature: i=2; a=rsa-sha256; d=example.org; s=arc; b=jkl\r\n" +
			"ARC-Authentication-Results: i=2; example.org; arc=pass\r\n" +
			"ARC-Seal: i=1; a=rsa-sha256; cv=none; d=example.com; s=arc; b=abc\r\n" +
			"ARC-Message-Signature: i=1; a=rsa-sha256; d=example.com; s=arc; b=def\r\n" +
			"ARC-Authentication-Results: i=1; example.com; spf=pass\r\n" +
			"From: sender@example.com\r\n\r\nBody\r\n")
		result := ValidateARCChain(message)
		if result != ARCChainPass {
			t.Errorf("ValidateARCChain() = %v, want %v", result, ARCChainPass)
		}
	})

	t.Run("ARC set with cv=fail", func(t *testing.T) {
		message := []byte("ARC-Seal: i=1; a=rsa-sha256; cv=fail; d=example.com; s=arc; b=abc\r\n" +
			"ARC-Message-Signature: i=1; a=rsa-sha256; d=example.com; s=arc; b=def\r\n" +
			"ARC-Authentication-Results: i=1; example.com; spf=fail\r\n" +
			"From: sender@example.com\r\n\r\nBody\r\n")
		result := ValidateARCChain(message)
		if result != ARCChainFail {
			t.Errorf("ValidateARCChain() = %v, want %v", result, ARCChainFail)
		}
	})
}

func TestARCVerifier_Verify(t *testing.T) {
	verifier := &ARCVerifier{}

	t.Run("no ARC headers", func(t *testing.T) {
		message := []byte("From: sender@example.com\r\n\r\nBody\r\n")
		result := verifier.Verify(message)
		if !result.ChainValid {
			t.Error("expected ChainValid for no ARC headers")
		}
		if result.InstanceCount != 0 {
			t.Errorf("InstanceCount = %d, want 0", result.InstanceCount)
		}
	})

	t.Run("valid single ARC set", func(t *testing.T) {
		message := []byte("ARC-Seal: i=1; a=rsa-sha256; cv=none; d=example.com; s=arc; b=abc\r\n" +
			"ARC-Message-Signature: i=1; a=rsa-sha256; d=example.com; s=arc; b=def\r\n" +
			"ARC-Authentication-Results: i=1; example.com; spf=pass\r\n" +
			"From: sender@example.com\r\n\r\nBody\r\n")
		result := verifier.Verify(message)
		if !result.ChainValid {
			t.Errorf("expected ChainValid, got error: %v", result.Error)
		}
		if result.InstanceCount != 1 {
			t.Errorf("InstanceCount = %d, want 1", result.InstanceCount)
		}
	})

	t.Run("sequential instances", func(t *testing.T) {
		message := []byte("ARC-Seal: i=2; cv=pass; b=ghi\r\n" +
			"ARC-Message-Signature: i=2; b=jkl\r\n" +
			"ARC-Authentication-Results: i=2; example.org\r\n" +
			"ARC-Seal: i=1; cv=none; b=abc\r\n" +
			"ARC-Message-Signature: i=1; b=def\r\n" +
			"ARC-Authentication-Results: i=1; example.com\r\n" +
			"From: sender@example.com\r\n\r\nBody\r\n")
		result := verifier.Verify(message)
		if !result.ChainValid {
			t.Errorf("expected ChainValid for sequential instances: %v", result.Error)
		}
		if result.InstanceCount != 2 {
			t.Errorf("InstanceCount = %d, want 2", result.InstanceCount)
		}
	})
}

func TestRelaxedBodyCanonicalization(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple body",
			input:    "Hello world\r\n",
			expected: "Hello world\r\n",
		},
		{
			name:     "multiple spaces",
			input:    "Hello    world\r\n",
			expected: "Hello world\r\n",
		},
		{
			name:     "trailing whitespace",
			input:    "Hello world   \r\n",
			expected: "Hello world\r\n",
		},
		{
			name:     "tabs and spaces",
			input:    "Hello\t\t  world\r\n",
			expected: "Hello world\r\n",
		},
		{
			name:     "trailing empty lines",
			input:    "Hello world\r\n\r\n\r\n",
			expected: "Hello world\r\n",
		},
		{
			name:     "no trailing CRLF",
			input:    "Hello world",
			expected: "Hello world\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(relaxedBodyCanonicalization([]byte(tt.input)))
			if got != tt.expected {
				t.Errorf("relaxedBodyCanonicalization(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRelaxedHeaderCanonicalization(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		value    string
		expected string
	}{
		{
			name:     "simple header",
			header:   "From",
			value:    "sender@example.com",
			expected: "from:sender@example.com\r\n",
		},
		{
			name:     "header with extra spaces",
			header:   "Subject",
			value:    "  Hello   world  ",
			expected: "subject:Hello world\r\n",
		},
		{
			name:     "uppercase header",
			header:   "MESSAGE-ID",
			value:    "<test@example.com>",
			expected: "message-id:<test@example.com>\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := relaxedHeaderCanonicalization(tt.header, tt.value)
			if got != tt.expected {
				t.Errorf("relaxedHeaderCanonicalization(%q, %q) = %q, want %q",
					tt.header, tt.value, got, tt.expected)
			}
		})
	}
}

func TestParseMessage(t *testing.T) {
	t.Run("standard CRLF", func(t *testing.T) {
		message := []byte("From: sender@example.com\r\nTo: recipient@example.org\r\n\r\nBody content\r\n")
		headers, body := parseMessage(message)

		if len(headers) != 2 {
			t.Errorf("expected 2 headers, got %d", len(headers))
		}
		if string(body) != "Body content\r\n" {
			t.Errorf("body = %q, want %q", string(body), "Body content\r\n")
		}
	})

	t.Run("LF only", func(t *testing.T) {
		message := []byte("From: sender@example.com\nTo: recipient@example.org\n\nBody content\n")
		headers, body := parseMessage(message)

		if len(headers) != 2 {
			t.Errorf("expected 2 headers, got %d", len(headers))
		}
		if string(body) != "Body content\n" {
			t.Errorf("body = %q, want %q", string(body), "Body content\n")
		}
	})

	t.Run("no body separator", func(t *testing.T) {
		message := []byte("From: sender@example.com")
		headers, body := parseMessage(message)

		if headers != nil {
			t.Errorf("expected nil headers for no separator, got %v", headers)
		}
		if string(body) != "From: sender@example.com" {
			t.Errorf("body = %q, want entire message", string(body))
		}
	})
}

func TestGetHeader(t *testing.T) {
	headers := []string{
		"From: sender@example.com",
		"To: recipient@example.org",
		"Subject: Hello World",
		"X-Custom: value1",
	}

	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{"existing header", "From", "sender@example.com"},
		{"case insensitive", "FROM", "sender@example.com"},
		{"another header", "Subject", "Hello World"},
		{"custom header", "X-Custom", "value1"},
		{"non-existent", "X-Missing", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getHeader(headers, tt.header)
			if got != tt.expected {
				t.Errorf("getHeader(%q) = %q, want %q", tt.header, got, tt.expected)
			}
		})
	}
}
