package search

import (
	"testing"
)

func TestFormatDocumentID(t *testing.T) {
	tests := []struct {
		mailboxID int64
		uid       uint32
		expected  string
	}{
		{1, 1, "1:1"},
		{123, 456, "123:456"},
		{0, 0, "0:0"},
		{999999, 100000, "999999:100000"},
	}

	for _, tt := range tests {
		result := FormatDocumentID(tt.mailboxID, tt.uid)
		if result != tt.expected {
			t.Errorf("FormatDocumentID(%d, %d) = %q, want %q",
				tt.mailboxID, tt.uid, result, tt.expected)
		}
	}
}

func TestParseDocumentID(t *testing.T) {
	tests := []struct {
		id          string
		wantMb      int64
		wantUID     uint32
		wantOK      bool
	}{
		{"1:1", 1, 1, true},
		{"123:456", 123, 456, true},
		{"0:0", 0, 0, true},
		{"999999:100000", 999999, 100000, true},
		{"invalid", 0, 0, false},
		{"", 0, 0, false},
		{":1", 0, 0, false},
		{"1:", 0, 0, false},
		{"a:b", 0, 0, false},
	}

	for _, tt := range tests {
		mb, uid, ok := ParseDocumentID(tt.id)
		if ok != tt.wantOK {
			t.Errorf("ParseDocumentID(%q) ok = %v, want %v", tt.id, ok, tt.wantOK)
			continue
		}
		if ok {
			if mb != tt.wantMb || uid != tt.wantUID {
				t.Errorf("ParseDocumentID(%q) = (%d, %d), want (%d, %d)",
					tt.id, mb, uid, tt.wantMb, tt.wantUID)
			}
		}
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		n        int64
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{123, "123"},
		{-1, "-1"},
		{-123, "-123"},
		{9223372036854775807, "9223372036854775807"},
	}

	for _, tt := range tests {
		result := itoa64(tt.n)
		if result != tt.expected {
			t.Errorf("itoa64(%d) = %q, want %q", tt.n, result, tt.expected)
		}
	}
}

func TestSearchQueryDefaults(t *testing.T) {
	sq := &SearchQuery{}

	if sq.Limit != 0 {
		t.Errorf("Default Limit should be 0, got %d", sq.Limit)
	}
	if sq.SortBy != "" {
		t.Errorf("Default SortBy should be empty, got %q", sq.SortBy)
	}
}

func TestEmailDocumentID(t *testing.T) {
	doc := &EmailDocument{
		MailboxID: 42,
		UID:       1001,
	}

	expectedID := "42:1001"
	doc.ID = FormatDocumentID(doc.MailboxID, doc.UID)

	if doc.ID != expectedID {
		t.Errorf("Document ID = %q, want %q", doc.ID, expectedID)
	}

	// Parse it back
	mb, uid, ok := ParseDocumentID(doc.ID)
	if !ok {
		t.Fatal("ParseDocumentID should succeed")
	}
	if mb != doc.MailboxID || uid != doc.UID {
		t.Errorf("Parsed values (%d, %d) don't match original (%d, %d)",
			mb, uid, doc.MailboxID, doc.UID)
	}
}

func TestConfigValidation(t *testing.T) {
	// Valid config
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig() should be valid, got error: %v", err)
	}

	// Disabled config
	cfg.Enabled = false
	if err := cfg.Validate(); err != nil {
		t.Errorf("Disabled config should be valid, got error: %v", err)
	}

	// Invalid engine
	cfg.Enabled = true
	cfg.Engine = "invalid"
	if err := cfg.Validate(); err == nil {
		t.Error("Invalid engine should cause validation error")
	}

	// Valid engines
	for _, engine := range []EngineType{EngineBleve, EngineSQLite, EnginePostgres, EngineAuto} {
		cfg.Engine = engine
		cfg.IndexPath = "/tmp/test"
		if err := cfg.Validate(); err != nil {
			t.Errorf("Engine %q should be valid, got error: %v", engine, err)
		}
	}
}

func TestConfigGetters(t *testing.T) {
	cfg := DefaultConfig()

	if d := cfg.GetFlushInterval(); d == 0 {
		t.Error("GetFlushInterval() should return non-zero duration")
	}

	if d := cfg.GetTimeout(); d == 0 {
		t.Error("GetTimeout() should return non-zero duration")
	}

	// Test with empty values
	cfg.FlushInterval = ""
	cfg.Timeout = ""

	if d := cfg.GetFlushInterval(); d == 0 {
		t.Error("GetFlushInterval() with empty string should return default")
	}

	if d := cfg.GetTimeout(); d == 0 {
		t.Error("GetTimeout() with empty string should return default")
	}
}
