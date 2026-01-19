package doctor

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Formatter formats doctor results for output.
type Formatter interface {
	Format(results *Results) string
	FormatComparison(results *ComparisonResults) string
	FormatFixResults(results []FixResult) string
}

// TextFormatter formats results as colored terminal text.
type TextFormatter struct {
	Verbose   bool
	NoColor   bool
	ShowHelp  bool
}

// JSONFormatter formats results as JSON.
type JSONFormatter struct {
	Pretty bool
}

// MarkdownFormatter formats results as markdown.
type MarkdownFormatter struct {
	IncludeDetails bool
}

// Color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// Format formats the results as colored text.
func (f *TextFormatter) Format(results *Results) string {
	var sb strings.Builder

	line := strings.Repeat("\u2501", 50)

	sb.WriteString("\n")
	sb.WriteString(f.colorize(line, colorDim))
	sb.WriteString("\n")
	sb.WriteString(f.colorize("              MAIL SERVER DOCTOR", colorBold))
	sb.WriteString("\n")
	sb.WriteString(f.colorize(line, colorDim))
	sb.WriteString("\n\n")

	// Group checks by category
	byCategory := make(map[Category][]CheckResult)
	for _, check := range results.Checks {
		byCategory[check.Category] = append(byCategory[check.Category], check)
	}

	// Order categories
	categoryOrder := []Category{
		CategoryInfra,
		CategoryNetwork,
		CategorySecurity,
		CategoryDNS,
		CategoryConfig,
		CategoryQueue,
	}

	categoryNames := map[Category]string{
		CategoryInfra:    "INFRASTRUCTURE",
		CategoryNetwork:  "NETWORK",
		CategorySecurity: "SECURITY",
		CategoryDNS:      "DNS",
		CategoryConfig:   "CONFIGURATION",
		CategoryQueue:    "QUEUE",
	}

	for _, cat := range categoryOrder {
		checks := byCategory[cat]
		if len(checks) == 0 {
			continue
		}

		sb.WriteString(f.colorize(categoryNames[cat], colorBold))
		sb.WriteString("\n")

		for _, check := range checks {
			icon, color := f.statusIcon(check.Status)
			sb.WriteString(fmt.Sprintf("  %s%s%s %s", color, icon, colorReset, check.Name))

			if f.Verbose && check.Message != "" {
				sb.WriteString(fmt.Sprintf(" %s(%s)%s", colorDim, check.Message, colorReset))
			} else if check.Status != StatusPass && check.Message != "" {
				sb.WriteString(fmt.Sprintf("\n    %s", check.Message))
			}

			sb.WriteString("\n")

			if f.ShowHelp && check.Help != "" && check.Status != StatusPass {
				sb.WriteString(fmt.Sprintf("    %s\u2192 %s%s\n", colorYellow, check.Help, colorReset))
			}

			if check.FixID != "" && check.Status != StatusPass {
				sb.WriteString(fmt.Sprintf("    %s\u2192 Fix: mailserver doctor fix --fix %s%s\n", colorBlue, check.FixID, colorReset))
			}
		}

		sb.WriteString("\n")
	}

	// Summary
	sb.WriteString(f.colorize(line, colorDim))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Summary: %s%d passed%s, %s%d failed%s, %s%d warnings%s\n",
		colorGreen, results.Passed, colorReset,
		colorRed, results.Failed, colorReset,
		colorYellow, results.Warned, colorReset))

	if len(results.FixableIDs) > 0 {
		sb.WriteString(fmt.Sprintf("Fixable: %d issue(s)\n", len(results.FixableIDs)))
	}

	sb.WriteString(f.colorize(line, colorDim))
	sb.WriteString("\n")

	return sb.String()
}

// FormatComparison formats comparison results as colored text.
func (f *TextFormatter) FormatComparison(results *ComparisonResults) string {
	var sb strings.Builder

	line := strings.Repeat("\u2501", 50)

	sb.WriteString("\n")
	sb.WriteString(f.colorize(line, colorDim))
	sb.WriteString("\n")
	sb.WriteString(f.colorize("            CONFIG VS REALITY", colorBold))
	sb.WriteString("\n")
	sb.WriteString(f.colorize(line, colorDim))
	sb.WriteString("\n\n")

	for _, comp := range results.Comparisons {
		icon, color := f.statusIcon(comp.Status)
		sb.WriteString(fmt.Sprintf("  %s%s%s %s\n", color, icon, colorReset, comp.Name))

		if f.Verbose {
			sb.WriteString(fmt.Sprintf("    Config: %v\n", comp.ConfigValue))
			sb.WriteString(fmt.Sprintf("    Actual: %v\n", comp.ActualValue))
		} else if comp.Message != "" {
			sb.WriteString(fmt.Sprintf("    %s\n", comp.Message))
		}
	}

	sb.WriteString("\n")
	sb.WriteString(f.colorize(line, colorDim))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Summary: %s%d matched%s, %s%d mismatched%s, %s%d errors%s\n",
		colorGreen, results.Matched, colorReset,
		colorRed, results.Mismatched, colorReset,
		colorYellow, results.Errors, colorReset))
	sb.WriteString(f.colorize(line, colorDim))
	sb.WriteString("\n")

	return sb.String()
}

// FormatFixResults formats fix results as colored text.
func (f *TextFormatter) FormatFixResults(results []FixResult) string {
	var sb strings.Builder

	line := strings.Repeat("\u2501", 50)

	sb.WriteString("\n")
	sb.WriteString(f.colorize(line, colorDim))
	sb.WriteString("\n")
	sb.WriteString(f.colorize("              FIX RESULTS", colorBold))
	sb.WriteString("\n")
	sb.WriteString(f.colorize(line, colorDim))
	sb.WriteString("\n\n")

	for _, fix := range results {
		var icon, color string
		if fix.DryRun {
			icon = "\u2139"
			color = colorBlue
		} else if fix.Success {
			icon = "\u2713"
			color = colorGreen
		} else {
			icon = "\u2717"
			color = colorRed
		}

		sb.WriteString(fmt.Sprintf("  %s%s%s %s\n", color, icon, colorReset, fix.Description))
		sb.WriteString(fmt.Sprintf("    %s\n", fix.Message))
	}

	sb.WriteString("\n")
	sb.WriteString(f.colorize(line, colorDim))
	sb.WriteString("\n")

	return sb.String()
}

func (f *TextFormatter) colorize(s string, color string) string {
	if f.NoColor {
		return s
	}
	return color + s + colorReset
}

func (f *TextFormatter) statusIcon(status Status) (string, string) {
	if f.NoColor {
		switch status {
		case StatusPass:
			return "[OK]", ""
		case StatusFail:
			return "[FAIL]", ""
		case StatusWarn:
			return "[WARN]", ""
		default:
			return "[?]", ""
		}
	}

	switch status {
	case StatusPass:
		return "\u2713", colorGreen
	case StatusFail:
		return "\u2717", colorRed
	case StatusWarn:
		return "!", colorYellow
	default:
		return "?", colorBlue
	}
}

// Format formats the results as JSON.
func (f *JSONFormatter) Format(results *Results) string {
	var data []byte
	var err error

	if f.Pretty {
		data, err = json.MarshalIndent(results, "", "  ")
	} else {
		data, err = json.Marshal(results)
	}

	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(data)
}

// FormatComparison formats comparison results as JSON.
func (f *JSONFormatter) FormatComparison(results *ComparisonResults) string {
	var data []byte
	var err error

	if f.Pretty {
		data, err = json.MarshalIndent(results, "", "  ")
	} else {
		data, err = json.Marshal(results)
	}

	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(data)
}

// FormatFixResults formats fix results as JSON.
func (f *JSONFormatter) FormatFixResults(results []FixResult) string {
	var data []byte
	var err error

	if f.Pretty {
		data, err = json.MarshalIndent(results, "", "  ")
	} else {
		data, err = json.Marshal(results)
	}

	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(data)
}

// Format formats the results as markdown.
func (f *MarkdownFormatter) Format(results *Results) string {
	var sb strings.Builder

	sb.WriteString("# Mail Server Doctor Report\n\n")

	// Summary
	sb.WriteString("## Summary\n\n")
	sb.WriteString(fmt.Sprintf("- **Passed:** %d\n", results.Passed))
	sb.WriteString(fmt.Sprintf("- **Failed:** %d\n", results.Failed))
	sb.WriteString(fmt.Sprintf("- **Warnings:** %d\n", results.Warned))
	sb.WriteString(fmt.Sprintf("- **Status:** %s\n", map[bool]string{true: "Healthy", false: "Unhealthy"}[results.Healthy]))
	sb.WriteString(fmt.Sprintf("- **Duration:** %s\n\n", results.Duration))

	// Group checks by category
	byCategory := make(map[Category][]CheckResult)
	for _, check := range results.Checks {
		byCategory[check.Category] = append(byCategory[check.Category], check)
	}

	categoryOrder := []Category{
		CategoryInfra,
		CategoryNetwork,
		CategorySecurity,
		CategoryDNS,
		CategoryConfig,
		CategoryQueue,
	}

	categoryNames := map[Category]string{
		CategoryInfra:    "Infrastructure",
		CategoryNetwork:  "Network",
		CategorySecurity: "Security",
		CategoryDNS:      "DNS",
		CategoryConfig:   "Configuration",
		CategoryQueue:    "Queue",
	}

	for _, cat := range categoryOrder {
		checks := byCategory[cat]
		if len(checks) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("## %s\n\n", categoryNames[cat]))
		sb.WriteString("| Status | Check | Message |\n")
		sb.WriteString("|--------|-------|--------|\n")

		for _, check := range checks {
			icon := map[Status]string{
				StatusPass: ":white_check_mark:",
				StatusFail: ":x:",
				StatusWarn: ":warning:",
			}[check.Status]

			message := check.Message
			if check.Help != "" && check.Status != StatusPass {
				message += " _" + check.Help + "_"
			}

			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", icon, check.Name, message))
		}

		sb.WriteString("\n")
	}

	// Fixable issues
	if len(results.FixableIDs) > 0 {
		sb.WriteString("## Fixable Issues\n\n")
		for _, fixID := range results.FixableIDs {
			sb.WriteString(fmt.Sprintf("- `mailserver doctor fix --fix %s`\n", fixID))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// FormatComparison formats comparison results as markdown.
func (f *MarkdownFormatter) FormatComparison(results *ComparisonResults) string {
	var sb strings.Builder

	sb.WriteString("# Config vs Reality Comparison\n\n")

	sb.WriteString("## Summary\n\n")
	sb.WriteString(fmt.Sprintf("- **Matched:** %d\n", results.Matched))
	sb.WriteString(fmt.Sprintf("- **Mismatched:** %d\n", results.Mismatched))
	sb.WriteString(fmt.Sprintf("- **Errors:** %d\n\n", results.Errors))

	sb.WriteString("## Details\n\n")
	sb.WriteString("| Status | Item | Config | Actual | Message |\n")
	sb.WriteString("|--------|------|--------|--------|--------|\n")

	for _, comp := range results.Comparisons {
		icon := map[Status]string{
			StatusPass: ":white_check_mark:",
			StatusFail: ":x:",
			StatusWarn: ":warning:",
		}[comp.Status]

		configVal := fmt.Sprintf("%v", comp.ConfigValue)
		actualVal := fmt.Sprintf("%v", comp.ActualValue)

		// Truncate long values
		if len(configVal) > 30 {
			configVal = configVal[:27] + "..."
		}
		if len(actualVal) > 30 {
			actualVal = actualVal[:27] + "..."
		}

		sb.WriteString(fmt.Sprintf("| %s | %s | `%s` | `%s` | %s |\n",
			icon, comp.Name, configVal, actualVal, comp.Message))
	}

	sb.WriteString("\n")
	return sb.String()
}

// FormatFixResults formats fix results as markdown.
func (f *MarkdownFormatter) FormatFixResults(results []FixResult) string {
	var sb strings.Builder

	sb.WriteString("# Fix Results\n\n")

	sb.WriteString("| Status | Fix | Message |\n")
	sb.WriteString("|--------|-----|--------|\n")

	for _, fix := range results {
		var icon string
		if fix.DryRun {
			icon = ":information_source:"
		} else if fix.Success {
			icon = ":white_check_mark:"
		} else {
			icon = ":x:"
		}

		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", icon, fix.Description, fix.Message))
	}

	sb.WriteString("\n")
	return sb.String()
}

// GetFormatter returns a formatter based on the format name.
func GetFormatter(format string, verbose bool, noColor bool) Formatter {
	switch format {
	case "json":
		return &JSONFormatter{Pretty: true}
	case "markdown", "md":
		return &MarkdownFormatter{IncludeDetails: verbose}
	default:
		return &TextFormatter{
			Verbose:  verbose,
			NoColor:  noColor,
			ShowHelp: true,
		}
	}
}

// PrintResults prints formatted results to stdout.
func PrintResults(results *Results, format string, verbose bool, noColor bool) {
	formatter := GetFormatter(format, verbose, noColor)
	fmt.Print(formatter.Format(results))
}

// PrintComparison prints formatted comparison results to stdout.
func PrintComparison(results *ComparisonResults, format string, verbose bool, noColor bool) {
	formatter := GetFormatter(format, verbose, noColor)
	fmt.Print(formatter.FormatComparison(results))
}

// PrintFixResults prints formatted fix results to stdout.
func PrintFixResults(results []FixResult, format string, verbose bool, noColor bool) {
	formatter := GetFormatter(format, verbose, noColor)
	fmt.Print(formatter.FormatFixResults(results))
}

// CategoryList returns a sorted list of all categories.
func CategoryList() []Category {
	return []Category{
		CategoryInfra,
		CategoryNetwork,
		CategorySecurity,
		CategoryDNS,
		CategoryConfig,
		CategoryQueue,
	}
}

// ParseCategory parses a category string to Category type.
func ParseCategory(s string) (Category, bool) {
	categories := map[string]Category{
		"infrastructure": CategoryInfra,
		"infra":          CategoryInfra,
		"network":        CategoryNetwork,
		"net":            CategoryNetwork,
		"security":       CategorySecurity,
		"sec":            CategorySecurity,
		"dns":            CategoryDNS,
		"config":         CategoryConfig,
		"configuration":  CategoryConfig,
		"queue":          CategoryQueue,
	}

	cat, ok := categories[strings.ToLower(s)]
	return cat, ok
}

// SortChecksByStatus sorts checks with failures first, then warnings, then passes.
func SortChecksByStatus(checks []CheckResult) {
	priority := map[Status]int{
		StatusFail: 0,
		StatusWarn: 1,
		StatusPass: 2,
	}

	sort.SliceStable(checks, func(i, j int) bool {
		return priority[checks[i].Status] < priority[checks[j].Status]
	})
}
