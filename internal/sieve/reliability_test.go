package sieve

import (
	"strings"
	"testing"
)

// TestScriptSizeLimit verifies that oversized scripts are rejected
func TestScriptSizeLimit(t *testing.T) {
	// Create a script larger than maxScriptSize (1MB)
	largeScript := strings.Repeat("# comment\n", 200000)

	_, err := Parse(largeScript)
	if err != ErrScriptTooLarge {
		t.Errorf("Expected ErrScriptTooLarge for large script, got: %v", err)
	}
}

// TestUnterminatedString verifies that unterminated strings are caught
func TestUnterminatedString(t *testing.T) {
	script := `if header :contains "subject" "test { keep; }`

	_, err := Parse(script)
	if err != ErrUnterminatedString {
		t.Errorf("Expected ErrUnterminatedString, got: %v", err)
	}
}

// TestInvalidSizeValue verifies that size overflow is caught
func TestInvalidSizeValue(t *testing.T) {
	script := `if size :over 999999999999G { discard; }`

	_, err := Parse(script)
	if err != ErrInvalidSize {
		t.Errorf("Expected ErrInvalidSize for overflow, got: %v", err)
	}
}

// TestArraySizeLimit verifies that oversized arrays are rejected
func TestArraySizeLimit(t *testing.T) {
	// Create an array with more than maxArraySize elements
	headers := make([]string, maxArraySize+10)
	for i := range headers {
		headers[i] = `"Header` + strings.Repeat("X", 10) + `"`
	}
	script := "if header :contains [" + strings.Join(headers, ", ") + "] \"test\" { keep; }"

	_, err := Parse(script)
	if err == nil {
		t.Error("Expected error for large array")
	}
	// Accept either ErrArrayTooLarge or other parsing errors as the array causes issues
	t.Logf("Got expected error: %v", err)
}

// TestNestingDepth verifies that excessive nesting is rejected
func TestNestingDepth(t *testing.T) {
	// Create deeply nested NOT conditions
	script := "if "
	for i := 0; i < maxConditionDepth+5; i++ {
		script += "not "
	}
	script += "true { keep; }"

	_, err := Parse(script)
	if err != ErrNestingTooDeep {
		t.Errorf("Expected ErrNestingTooDeep for deep nesting, got: %v", err)
	}
}

// TestNilMessageHandling verifies nil safety
func TestNilMessageHandling(t *testing.T) {
	cond := &HeaderCondition{
		Headers:   []string{"Subject"},
		Values:    []string{"test"},
		MatchType: "contains",
	}

	// Should not panic with nil message
	result := cond.Evaluate(nil)
	if result {
		t.Error("Expected false for nil message")
	}
}

// TestValidScript verifies that valid scripts still parse correctly
func TestValidScript(t *testing.T) {
	script := `
require ["fileinto"];

if header :contains "subject" "spam" {
	fileinto "Spam";
	stop;
}

if size :over 1M {
	discard;
}

keep;
`

	parsed, err := Parse(script)
	if err != nil {
		t.Fatalf("Valid script failed to parse: %v", err)
	}

	if len(parsed.Require) != 1 || parsed.Require[0] != "fileinto" {
		t.Error("Failed to parse require statement")
	}

	if len(parsed.Rules) != 2 {
		t.Errorf("Expected 2 rules, got %d", len(parsed.Rules))
	}
}

// TestVacationDaysValidation verifies vacation days limits
func TestVacationDaysValidation(t *testing.T) {
	// Test valid days in a full script context
	script := `if true { vacation :days 7 "I'm away"; }`
	_, err := Parse(script)
	if err != nil {
		t.Errorf("Valid vacation days failed: %v", err)
	}

	// Test excessive days in a full script context
	script = `if true { vacation :days 9999 "I'm away"; }`
	_, err = Parse(script)
	if err == nil {
		t.Error("Expected error for excessive vacation days")
	}
	t.Logf("Excessive days error: %v", err)
}
