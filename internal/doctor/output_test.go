package doctor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTextFormatter(t *testing.T) {
	formatter := &TextFormatter{
		Verbose:  false,
		NoColor:  false,
		ShowHelp: true,
	}

	results := &Results{
		Checks: []CheckResult{
			{ID: "test1", Name: "Test 1", Category: CategoryInfra, Status: StatusPass, Message: "OK"},
			{ID: "test2", Name: "Test 2", Category: CategoryNetwork, Status: StatusFail, Message: "Failed", Help: "Fix it"},
			{ID: "test3", Name: "Test 3", Category: CategorySecurity, Status: StatusWarn, Message: "Warning"},
		},
		Passed:   1,
		Failed:   1,
		Warned:   1,
		Healthy:  false,
		Duration: time.Second,
	}

	output := formatter.Format(results)

	if output == "" {
		t.Error("Format() should return non-empty output")
	}

	// Check for expected content
	if !strings.Contains(output, "MAIL SERVER DOCTOR") {
		t.Error("Output should contain header")
	}

	if !strings.Contains(output, "Test 1") {
		t.Error("Output should contain check names")
	}

	if !strings.Contains(output, "Summary") || !strings.Contains(output, "1 passed") {
		t.Error("Output should contain summary")
	}
}

func TestTextFormatterNoColor(t *testing.T) {
	formatter := &TextFormatter{
		NoColor: true,
	}

	results := &Results{
		Checks: []CheckResult{
			{ID: "test", Name: "Test", Category: CategoryInfra, Status: StatusPass},
		},
		Passed: 1,
	}

	output := formatter.Format(results)

	// Should contain text-based icons instead of colored unicode
	if !strings.Contains(output, "[OK]") && !strings.Contains(output, "Test") {
		t.Error("Output should contain text-based status indicators when NoColor is true")
	}
}

func TestTextFormatterVerbose(t *testing.T) {
	formatter := &TextFormatter{
		Verbose: true,
		NoColor: true,
	}

	results := &Results{
		Checks: []CheckResult{
			{ID: "test", Name: "Test", Category: CategoryInfra, Status: StatusPass, Message: "Detailed message"},
		},
		Passed: 1,
	}

	output := formatter.Format(results)

	if !strings.Contains(output, "Detailed message") {
		t.Error("Verbose output should include messages for passing checks")
	}
}

func TestTextFormatterComparison(t *testing.T) {
	formatter := &TextFormatter{
		Verbose: true,
		NoColor: true,
	}

	results := &ComparisonResults{
		Comparisons: []Comparison{
			{Name: "Test", ConfigValue: 25, ActualValue: "listening", Status: StatusPass, Message: "OK"},
		},
		Matched: 1,
	}

	output := formatter.FormatComparison(results)

	if output == "" {
		t.Error("FormatComparison() should return non-empty output")
	}

	if !strings.Contains(output, "CONFIG VS REALITY") {
		t.Error("Output should contain header")
	}
}

func TestTextFormatterFixResults(t *testing.T) {
	formatter := &TextFormatter{
		NoColor: true,
	}

	results := []FixResult{
		{FixID: "test1", Description: "Fix 1", Success: true, Message: "Applied"},
		{FixID: "test2", Description: "Fix 2", Success: false, Message: "Failed"},
		{FixID: "test3", Description: "Fix 3", Success: true, Message: "Would do", DryRun: true},
	}

	output := formatter.FormatFixResults(results)

	if output == "" {
		t.Error("FormatFixResults() should return non-empty output")
	}

	if !strings.Contains(output, "FIX RESULTS") {
		t.Error("Output should contain header")
	}
}

func TestJSONFormatter(t *testing.T) {
	formatter := &JSONFormatter{Pretty: false}

	results := &Results{
		Checks: []CheckResult{
			{ID: "test", Name: "Test", Category: CategoryInfra, Status: StatusPass},
		},
		Passed:  1,
		Healthy: true,
	}

	output := formatter.Format(results)

	// Should be valid JSON
	var parsed Results
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("Output is not valid JSON: %v", err)
	}

	if parsed.Passed != 1 {
		t.Errorf("Parsed Passed = %d, want 1", parsed.Passed)
	}
}

func TestJSONFormatterPretty(t *testing.T) {
	formatter := &JSONFormatter{Pretty: true}

	results := &Results{
		Checks: []CheckResult{
			{ID: "test", Name: "Test", Category: CategoryInfra, Status: StatusPass},
		},
		Passed: 1,
	}

	output := formatter.Format(results)

	// Pretty output should contain newlines
	if !strings.Contains(output, "\n") {
		t.Error("Pretty output should contain newlines")
	}
}

func TestJSONFormatterComparison(t *testing.T) {
	formatter := &JSONFormatter{Pretty: false}

	results := &ComparisonResults{
		Comparisons: []Comparison{
			{Name: "Test", ConfigValue: 25, Status: StatusPass},
		},
		Matched: 1,
	}

	output := formatter.FormatComparison(results)

	var parsed ComparisonResults
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("Output is not valid JSON: %v", err)
	}
}

func TestJSONFormatterFixResults(t *testing.T) {
	formatter := &JSONFormatter{Pretty: false}

	results := []FixResult{
		{FixID: "test", Description: "Test Fix", Success: true},
	}

	output := formatter.FormatFixResults(results)

	var parsed []FixResult
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("Output is not valid JSON: %v", err)
	}

	if len(parsed) != 1 {
		t.Errorf("Parsed length = %d, want 1", len(parsed))
	}
}

func TestMarkdownFormatter(t *testing.T) {
	formatter := &MarkdownFormatter{IncludeDetails: true}

	results := &Results{
		Checks: []CheckResult{
			{ID: "test1", Name: "Test 1", Category: CategoryInfra, Status: StatusPass, Message: "OK"},
			{ID: "test2", Name: "Test 2", Category: CategoryNetwork, Status: StatusFail, Message: "Failed"},
		},
		Passed:   1,
		Failed:   1,
		Healthy:  false,
		Duration: time.Second,
	}

	output := formatter.Format(results)

	if output == "" {
		t.Error("Format() should return non-empty output")
	}

	// Check markdown structure
	if !strings.Contains(output, "# Mail Server Doctor Report") {
		t.Error("Output should contain markdown header")
	}

	if !strings.Contains(output, "## Summary") {
		t.Error("Output should contain summary section")
	}

	if !strings.Contains(output, "|") {
		t.Error("Output should contain markdown tables")
	}
}

func TestMarkdownFormatterComparison(t *testing.T) {
	formatter := &MarkdownFormatter{}

	results := &ComparisonResults{
		Comparisons: []Comparison{
			{Name: "Test", ConfigValue: "value", ActualValue: "actual", Status: StatusPass, Message: "OK"},
		},
		Matched: 1,
	}

	output := formatter.FormatComparison(results)

	if !strings.Contains(output, "# Config vs Reality") {
		t.Error("Output should contain header")
	}

	if !strings.Contains(output, "| Status |") {
		t.Error("Output should contain table header")
	}
}

func TestMarkdownFormatterFixResults(t *testing.T) {
	formatter := &MarkdownFormatter{}

	results := []FixResult{
		{FixID: "test", Description: "Test Fix", Success: true, Message: "Applied"},
	}

	output := formatter.FormatFixResults(results)

	if !strings.Contains(output, "# Fix Results") {
		t.Error("Output should contain header")
	}
}

func TestGetFormatter(t *testing.T) {
	tests := []struct {
		format   string
		expected string
	}{
		{"text", "*doctor.TextFormatter"},
		{"json", "*doctor.JSONFormatter"},
		{"markdown", "*doctor.MarkdownFormatter"},
		{"md", "*doctor.MarkdownFormatter"},
		{"unknown", "*doctor.TextFormatter"}, // Default to text
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			formatter := GetFormatter(tt.format, false, false)
			typeName := strings.TrimPrefix(strings.TrimSuffix(getTypeName(formatter), "}"), "{")

			if !strings.Contains(typeName, strings.TrimPrefix(tt.expected, "*doctor.")) {
				t.Errorf("GetFormatter(%q) type = %v, want %v", tt.format, typeName, tt.expected)
			}
		})
	}
}

func getTypeName(v interface{}) string {
	switch v.(type) {
	case *TextFormatter:
		return "TextFormatter"
	case *JSONFormatter:
		return "JSONFormatter"
	case *MarkdownFormatter:
		return "MarkdownFormatter"
	default:
		return "unknown"
	}
}

func TestParseCategory(t *testing.T) {
	tests := []struct {
		input    string
		expected Category
		ok       bool
	}{
		{"infrastructure", CategoryInfra, true},
		{"infra", CategoryInfra, true},
		{"network", CategoryNetwork, true},
		{"net", CategoryNetwork, true},
		{"security", CategorySecurity, true},
		{"sec", CategorySecurity, true},
		{"dns", CategoryDNS, true},
		{"config", CategoryConfig, true},
		{"configuration", CategoryConfig, true},
		{"queue", CategoryQueue, true},
		{"unknown", "", false},
		{"", "", false},
		{"INFRASTRUCTURE", CategoryInfra, true}, // Case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cat, ok := ParseCategory(tt.input)

			if ok != tt.ok {
				t.Errorf("ParseCategory(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}

			if ok && cat != tt.expected {
				t.Errorf("ParseCategory(%q) = %v, want %v", tt.input, cat, tt.expected)
			}
		})
	}
}

func TestCategoryList(t *testing.T) {
	categories := CategoryList()

	if len(categories) != 6 {
		t.Errorf("CategoryList() length = %d, want 6", len(categories))
	}

	// Check all expected categories are present
	expected := map[Category]bool{
		CategoryInfra:    false,
		CategoryNetwork:  false,
		CategorySecurity: false,
		CategoryDNS:      false,
		CategoryConfig:   false,
		CategoryQueue:    false,
	}

	for _, cat := range categories {
		if _, ok := expected[cat]; !ok {
			t.Errorf("Unexpected category: %v", cat)
		}
		expected[cat] = true
	}

	for cat, found := range expected {
		if !found {
			t.Errorf("Missing category: %v", cat)
		}
	}
}

func TestSortChecksByStatus(t *testing.T) {
	checks := []CheckResult{
		{ID: "pass1", Status: StatusPass},
		{ID: "fail1", Status: StatusFail},
		{ID: "warn1", Status: StatusWarn},
		{ID: "pass2", Status: StatusPass},
		{ID: "fail2", Status: StatusFail},
	}

	SortChecksByStatus(checks)

	// Failures should be first
	if checks[0].Status != StatusFail || checks[1].Status != StatusFail {
		t.Error("Failures should be first")
	}

	// Then warnings
	if checks[2].Status != StatusWarn {
		t.Error("Warnings should be after failures")
	}

	// Then passes
	if checks[3].Status != StatusPass || checks[4].Status != StatusPass {
		t.Error("Passes should be last")
	}
}

func TestStatusIcon(t *testing.T) {
	formatter := &TextFormatter{NoColor: false}

	// With color
	tests := []struct {
		status Status
	}{
		{StatusPass},
		{StatusFail},
		{StatusWarn},
	}

	for _, tt := range tests {
		icon, color := formatter.statusIcon(tt.status)
		if icon == "" {
			t.Errorf("statusIcon(%v) icon is empty", tt.status)
		}
		if color == "" {
			t.Errorf("statusIcon(%v) color is empty", tt.status)
		}
	}

	// Without color
	formatter.NoColor = true
	for _, tt := range tests {
		icon, color := formatter.statusIcon(tt.status)
		if icon == "" {
			t.Errorf("statusIcon(%v) icon is empty (no color)", tt.status)
		}
		if color != "" {
			t.Errorf("statusIcon(%v) should not have color when NoColor=true", tt.status)
		}
	}
}

// Edge case tests

func TestFormatterWithEmptyResults(t *testing.T) {
	formatters := []Formatter{
		&TextFormatter{},
		&JSONFormatter{},
		&MarkdownFormatter{},
	}

	results := &Results{
		Checks: []CheckResult{},
	}

	for _, formatter := range formatters {
		output := formatter.Format(results)
		if output == "" {
			t.Error("Format() should return output even with empty results")
		}
	}
}

func TestFormatterWithNilDetails(t *testing.T) {
	formatter := &TextFormatter{Verbose: true}

	results := &Results{
		Checks: []CheckResult{
			{ID: "test", Name: "Test", Status: StatusPass, Details: nil},
		},
	}

	// Should not panic
	output := formatter.Format(results)
	if output == "" {
		t.Error("Should handle nil details")
	}
}

func TestFormatterWithSpecialCharacters(t *testing.T) {
	formatter := &TextFormatter{}

	results := &Results{
		Checks: []CheckResult{
			{ID: "test", Name: "Test <>&\"'", Status: StatusPass, Message: "Message with <>&\"'"},
		},
	}

	output := formatter.Format(results)
	if output == "" {
		t.Error("Should handle special characters")
	}
}

func TestJSONFormatterWithSpecialCharacters(t *testing.T) {
	formatter := &JSONFormatter{}

	results := &Results{
		Checks: []CheckResult{
			{ID: "test", Name: "Test \"quotes\"", Status: StatusPass, Message: "Line1\nLine2"},
		},
	}

	output := formatter.Format(results)

	// Should be valid JSON
	var parsed Results
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Errorf("Output is not valid JSON: %v", err)
	}
}

func TestMarkdownFormatterWithLongValues(t *testing.T) {
	formatter := &MarkdownFormatter{}

	results := &ComparisonResults{
		Comparisons: []Comparison{
			{
				Name:        "Test",
				ConfigValue: strings.Repeat("a", 100), // Long value
				ActualValue: strings.Repeat("b", 100),
				Status:      StatusPass,
			},
		},
	}

	output := formatter.FormatComparison(results)

	// Long values should be truncated
	if strings.Contains(output, strings.Repeat("a", 100)) {
		t.Error("Long values should be truncated in markdown tables")
	}
}
