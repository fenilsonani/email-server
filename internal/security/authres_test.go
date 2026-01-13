package security

import (
	"strings"
	"testing"
)

func TestAuthenticationResults_Format(t *testing.T) {
	t.Run("all results", func(t *testing.T) {
		ar := &AuthenticationResults{
			AuthServID: "mail.example.com",
			SPF: &SPFResult{
				Result: ResultPass,
				Domain: "example.com",
				IP:     "192.0.2.1",
			},
			DKIM: &DKIMResult{
				Result:   ResultPass,
				Domain:   "example.com",
				Selector: "mail",
			},
			DMARC: &DMARCResult{
				Result: ResultPass,
				Domain: "example.com",
				Policy: "reject",
			},
		}

		formatted := ar.Format()

		if !strings.HasPrefix(formatted, "mail.example.com") {
			t.Error("should start with authserv-id")
		}
		if !strings.Contains(formatted, "spf=pass") {
			t.Error("missing SPF result")
		}
		if !strings.Contains(formatted, "smtp.mailfrom=example.com") {
			t.Error("missing SPF domain")
		}
		if !strings.Contains(formatted, "smtp.client-ip=192.0.2.1") {
			t.Error("missing SPF IP")
		}
		if !strings.Contains(formatted, "dkim=pass") {
			t.Error("missing DKIM result")
		}
		if !strings.Contains(formatted, "header.d=example.com") {
			t.Error("missing DKIM domain")
		}
		if !strings.Contains(formatted, "header.s=mail") {
			t.Error("missing DKIM selector")
		}
		if !strings.Contains(formatted, "dmarc=pass") {
			t.Error("missing DMARC result")
		}
		if !strings.Contains(formatted, "header.from=example.com") {
			t.Error("missing DMARC domain")
		}
	})

	t.Run("SPF only", func(t *testing.T) {
		ar := &AuthenticationResults{
			AuthServID: "mail.example.com",
			SPF: &SPFResult{
				Result: ResultFail,
				Domain: "spammer.com",
			},
		}

		formatted := ar.Format()
		if !strings.Contains(formatted, "spf=fail") {
			t.Error("missing SPF fail result")
		}
		if strings.Contains(formatted, "dkim=") {
			t.Error("should not contain DKIM when not set")
		}
	})

	t.Run("no results adds none", func(t *testing.T) {
		ar := &AuthenticationResults{
			AuthServID: "mail.example.com",
		}

		formatted := ar.Format()
		if !strings.Contains(formatted, "none") {
			t.Error("should contain 'none' when no results")
		}
	})

	t.Run("with ARC result", func(t *testing.T) {
		ar := &AuthenticationResults{
			AuthServID: "mail.example.com",
			ARC: &ARCResult{
				Result:   ResultPass,
				Instance: 2,
			},
		}

		formatted := ar.Format()
		if !strings.Contains(formatted, "arc=pass") {
			t.Error("missing ARC result")
		}
		if !strings.Contains(formatted, "header.i=2") {
			t.Error("missing ARC instance")
		}
	})

	t.Run("with reason", func(t *testing.T) {
		ar := &AuthenticationResults{
			AuthServID: "mail.example.com",
			SPF: &SPFResult{
				Result: ResultSoftfail,
				Domain: "example.com",
				Reason: "sender not authorized",
			},
		}

		formatted := ar.Format()
		if !strings.Contains(formatted, "spf=softfail") {
			t.Error("missing SPF softfail")
		}
		if !strings.Contains(formatted, `reason="sender not authorized"`) {
			t.Error("missing reason")
		}
	})
}

func TestAuthenticationResults_FormatARC(t *testing.T) {
	ar := &AuthenticationResults{
		AuthServID: "mail.example.com",
		SPF: &SPFResult{
			Result: ResultPass,
			Domain: "example.com",
		},
		DKIM: &DKIMResult{
			Result:   ResultPass,
			Domain:   "example.com",
			Selector: "mail",
		},
	}

	t.Run("instance 1", func(t *testing.T) {
		formatted := ar.FormatARC(1)
		if !strings.HasPrefix(formatted, "i=1") {
			t.Errorf("should start with i=1, got: %s", formatted)
		}
		if !strings.Contains(formatted, "mail.example.com") {
			t.Error("missing authserv-id")
		}
		if !strings.Contains(formatted, "spf=pass") {
			t.Error("missing SPF result")
		}
	})

	t.Run("instance 5", func(t *testing.T) {
		formatted := ar.FormatARC(5)
		if !strings.HasPrefix(formatted, "i=5") {
			t.Errorf("should start with i=5, got: %s", formatted)
		}
	})
}

func TestParseAuthenticationResults(t *testing.T) {
	t.Run("full header", func(t *testing.T) {
		header := "mail.example.com; spf=pass smtp.mailfrom=example.com smtp.client-ip=192.0.2.1; dkim=pass header.d=example.com header.s=mail; dmarc=pass header.from=example.com"

		ar, err := ParseAuthenticationResults(header)
		if err != nil {
			t.Fatalf("ParseAuthenticationResults() error: %v", err)
		}

		if ar.AuthServID != "mail.example.com" {
			t.Errorf("AuthServID = %q, want %q", ar.AuthServID, "mail.example.com")
		}

		if ar.SPF == nil {
			t.Fatal("SPF should not be nil")
		}
		if ar.SPF.Result != ResultPass {
			t.Errorf("SPF.Result = %v, want %v", ar.SPF.Result, ResultPass)
		}
		if ar.SPF.Domain != "example.com" {
			t.Errorf("SPF.Domain = %q, want %q", ar.SPF.Domain, "example.com")
		}
		if ar.SPF.IP != "192.0.2.1" {
			t.Errorf("SPF.IP = %q, want %q", ar.SPF.IP, "192.0.2.1")
		}

		if ar.DKIM == nil {
			t.Fatal("DKIM should not be nil")
		}
		if ar.DKIM.Result != ResultPass {
			t.Errorf("DKIM.Result = %v, want %v", ar.DKIM.Result, ResultPass)
		}
		if ar.DKIM.Domain != "example.com" {
			t.Errorf("DKIM.Domain = %q, want %q", ar.DKIM.Domain, "example.com")
		}
		if ar.DKIM.Selector != "mail" {
			t.Errorf("DKIM.Selector = %q, want %q", ar.DKIM.Selector, "mail")
		}

		if ar.DMARC == nil {
			t.Fatal("DMARC should not be nil")
		}
		if ar.DMARC.Result != ResultPass {
			t.Errorf("DMARC.Result = %v, want %v", ar.DMARC.Result, ResultPass)
		}
		if ar.DMARC.Domain != "example.com" {
			t.Errorf("DMARC.Domain = %q, want %q", ar.DMARC.Domain, "example.com")
		}
	})

	t.Run("SPF only", func(t *testing.T) {
		header := "mx.example.org; spf=fail smtp.mailfrom=spammer.com"

		ar, err := ParseAuthenticationResults(header)
		if err != nil {
			t.Fatalf("ParseAuthenticationResults() error: %v", err)
		}

		if ar.SPF == nil {
			t.Fatal("SPF should not be nil")
		}
		if ar.SPF.Result != ResultFail {
			t.Errorf("SPF.Result = %v, want %v", ar.SPF.Result, ResultFail)
		}
		if ar.DKIM != nil {
			t.Error("DKIM should be nil")
		}
	})

	t.Run("with ARC", func(t *testing.T) {
		header := "mail.example.com; arc=pass header.i=3"

		ar, err := ParseAuthenticationResults(header)
		if err != nil {
			t.Fatalf("ParseAuthenticationResults() error: %v", err)
		}

		if ar.ARC == nil {
			t.Fatal("ARC should not be nil")
		}
		if ar.ARC.Result != ResultPass {
			t.Errorf("ARC.Result = %v, want %v", ar.ARC.Result, ResultPass)
		}
		if ar.ARC.Instance != 3 {
			t.Errorf("ARC.Instance = %d, want 3", ar.ARC.Instance)
		}
	})

	t.Run("empty header", func(t *testing.T) {
		ar, err := ParseAuthenticationResults("")
		// Empty header returns an AuthenticationResults with empty AuthServID
		if err != nil {
			t.Logf("got error (acceptable): %v", err)
		} else if ar.AuthServID != "" {
			t.Error("expected empty AuthServID for empty header")
		}
	})

	t.Run("various result values", func(t *testing.T) {
		tests := []struct {
			result   string
			expected ResultValue
		}{
			{"spf=pass", ResultPass},
			{"spf=fail", ResultFail},
			{"spf=softfail", ResultSoftfail},
			{"spf=neutral", ResultNeutral},
			{"spf=none", ResultNone},
			{"spf=temperror", ResultTempError},
			{"spf=permerror", ResultPermError},
		}

		for _, tt := range tests {
			t.Run(string(tt.expected), func(t *testing.T) {
				header := "mail.example.com; " + tt.result
				ar, err := ParseAuthenticationResults(header)
				if err != nil {
					t.Fatalf("ParseAuthenticationResults() error: %v", err)
				}
				if ar.SPF.Result != tt.expected {
					t.Errorf("Result = %v, want %v", ar.SPF.Result, tt.expected)
				}
			})
		}
	})
}

func TestResultValue_Constants(t *testing.T) {
	// Verify constant values match expected strings
	tests := []struct {
		value    ResultValue
		expected string
	}{
		{ResultNone, "none"},
		{ResultPass, "pass"},
		{ResultFail, "fail"},
		{ResultSoftfail, "softfail"},
		{ResultNeutral, "neutral"},
		{ResultTempError, "temperror"},
		{ResultPermError, "permerror"},
		{ResultPolicy, "policy"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.value) != tt.expected {
				t.Errorf("ResultValue = %q, want %q", tt.value, tt.expected)
			}
		})
	}
}

func TestSPFResult(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		ar := &AuthenticationResults{
			AuthServID: "test",
			SPF: &SPFResult{
				Result: ResultPass,
				Domain: "example.com",
				IP:     "192.0.2.1",
				Reason: "sender allowed",
			},
		}
		formatted := ar.Format()

		if !strings.Contains(formatted, "spf=pass") {
			t.Error("missing result")
		}
		if !strings.Contains(formatted, "smtp.mailfrom=example.com") {
			t.Error("missing domain")
		}
		if !strings.Contains(formatted, "smtp.client-ip=192.0.2.1") {
			t.Error("missing IP")
		}
		if !strings.Contains(formatted, `reason="sender allowed"`) {
			t.Error("missing reason")
		}
	})

	t.Run("minimal fields", func(t *testing.T) {
		ar := &AuthenticationResults{
			AuthServID: "test",
			SPF: &SPFResult{
				Result: ResultNone,
			},
		}
		formatted := ar.Format()

		if !strings.Contains(formatted, "spf=none") {
			t.Error("missing result")
		}
		if strings.Contains(formatted, "smtp.mailfrom=") {
			t.Error("should not contain empty mailfrom")
		}
	})
}

func TestDKIMResult(t *testing.T) {
	t.Run("with identity", func(t *testing.T) {
		ar := &AuthenticationResults{
			AuthServID: "test",
			DKIM: &DKIMResult{
				Result:   ResultPass,
				Domain:   "example.com",
				Selector: "mail",
				Identity: "@example.com",
			},
		}
		formatted := ar.Format()

		if !strings.Contains(formatted, "dkim=pass") {
			t.Error("missing result")
		}
		if !strings.Contains(formatted, "header.d=example.com") {
			t.Error("missing domain")
		}
		if !strings.Contains(formatted, "header.s=mail") {
			t.Error("missing selector")
		}
		if !strings.Contains(formatted, "header.i=@example.com") {
			t.Error("missing identity")
		}
	})
}

func TestDMARCResult(t *testing.T) {
	t.Run("with policy", func(t *testing.T) {
		ar := &AuthenticationResults{
			AuthServID: "test",
			DMARC: &DMARCResult{
				Result: ResultFail,
				Domain: "spammer.com",
				Policy: "reject",
				Reason: "alignment failed",
			},
		}
		formatted := ar.Format()

		if !strings.Contains(formatted, "dmarc=fail") {
			t.Error("missing result")
		}
		if !strings.Contains(formatted, "header.from=spammer.com") {
			t.Error("missing domain")
		}
		if !strings.Contains(formatted, "policy.applied=reject") {
			t.Error("missing policy")
		}
		if !strings.Contains(formatted, `reason="alignment failed"`) {
			t.Error("missing reason")
		}
	})
}

func TestARCResult_Format(t *testing.T) {
	ar := &AuthenticationResults{
		AuthServID: "test",
		ARC: &ARCResult{
			Result:   ResultPass,
			Instance: 5,
			Reason:   "chain valid",
		},
	}
	formatted := ar.Format()

	if !strings.Contains(formatted, "arc=pass") {
		t.Error("missing result")
	}
	if !strings.Contains(formatted, "header.i=5") {
		t.Error("missing instance")
	}
	if !strings.Contains(formatted, `reason="chain valid"`) {
		t.Error("missing reason")
	}
}

func TestAuthenticationResults_RoundTrip(t *testing.T) {
	// Create an AuthenticationResults, format it, parse it back, and verify
	original := &AuthenticationResults{
		AuthServID: "mail.example.com",
		SPF: &SPFResult{
			Result: ResultPass,
			Domain: "example.com",
			IP:     "192.0.2.1",
		},
		DKIM: &DKIMResult{
			Result:   ResultPass,
			Domain:   "example.com",
			Selector: "mail",
		},
		DMARC: &DMARCResult{
			Result: ResultPass,
			Domain: "example.com",
		},
	}

	formatted := original.Format()

	// Parse expects a single-line format with ; separators
	// Our Format() uses CRLF+tab for folding, so normalize
	normalized := strings.ReplaceAll(formatted, "\r\n\t", " ")

	parsed, err := ParseAuthenticationResults(normalized)
	if err != nil {
		t.Fatalf("ParseAuthenticationResults() error: %v", err)
	}

	// Verify key fields match
	if parsed.AuthServID != original.AuthServID {
		t.Errorf("AuthServID = %q, want %q", parsed.AuthServID, original.AuthServID)
	}
	if parsed.SPF == nil || parsed.SPF.Result != original.SPF.Result {
		t.Error("SPF result mismatch")
	}
	if parsed.DKIM == nil || parsed.DKIM.Result != original.DKIM.Result {
		t.Error("DKIM result mismatch")
	}
	if parsed.DMARC == nil || parsed.DMARC.Result != original.DMARC.Result {
		t.Error("DMARC result mismatch")
	}
}
