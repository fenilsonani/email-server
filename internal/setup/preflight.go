package setup

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// CheckResult represents the result of a single check
type CheckResult struct {
	Name    string
	Status  string // "pass", "fail", "warn"
	Message string
	Help    string
}

// PreflightResults contains all preflight check results
type PreflightResults struct {
	Checks  []CheckResult
	Passed  int
	Failed  int
	Warned  int
	Ready   bool
}

// RunPreflight runs all preflight checks
func RunPreflight() *PreflightResults {
	return RunPreflightWithOptions(false)
}

// RunPreflightWithOptions runs preflight checks with optional force mode
func RunPreflightWithOptions(force bool) *PreflightResults {
	results := &PreflightResults{}

	checks := []func() CheckResult{
		checkRoot,
		checkOS,
		checkMemory,
		checkDiskSpace,
		checkPort25,
		checkPort143,
		checkPort465,
		checkPort587,
		checkPort993,
		checkPort25Outbound,
		checkRedis,
		checkGo,
		checkSystemd,
	}

	for _, check := range checks {
		result := check()
		results.Checks = append(results.Checks, result)

		switch result.Status {
		case "pass":
			results.Passed++
		case "fail":
			results.Failed++
		case "warn":
			results.Warned++
		}
	}

	// In force mode, only critical checks (ports, redis) block setup
	if force {
		results.Ready = true
		// Check only critical items
		for _, check := range results.Checks {
			if check.Status == "fail" {
				// These are critical even in force mode
				if strings.Contains(check.Name, "Port") ||
					strings.Contains(check.Name, "Redis") ||
					strings.Contains(check.Name, "Disk") {
					results.Ready = false
					break
				}
			}
		}
	} else {
		results.Ready = results.Failed == 0
	}

	return results
}

// PrintResults prints the preflight results
func (r *PreflightResults) Print() {
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("                    PREFLIGHT CHECK")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	for _, check := range r.Checks {
		icon := "✓"
		color := "\033[32m" // green
		if check.Status == "fail" {
			icon = "✗"
			color = "\033[31m" // red
		} else if check.Status == "warn" {
			icon = "!"
			color = "\033[33m" // yellow
		}
		reset := "\033[0m"

		fmt.Printf("%s%s%s %s\n", color, icon, reset, check.Name)
		if check.Message != "" {
			fmt.Printf("  %s\n", check.Message)
		}
		if check.Status == "fail" && check.Help != "" {
			fmt.Printf("  → %s\n", check.Help)
		}
		fmt.Println()
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Results: %d passed, %d failed, %d warnings\n", r.Passed, r.Failed, r.Warned)

	if r.Ready {
		fmt.Println("\033[32m✓ Server is ready for mail server setup!\033[0m")
		fmt.Println("\nRun: mailserver setup")
	} else {
		fmt.Println("\033[31m✗ Server is not ready. Fix the issues above first.\033[0m")
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

func checkRoot() CheckResult {
	if os.Geteuid() == 0 {
		return CheckResult{
			Name:    "Running as root",
			Status:  "pass",
			Message: "Root access available for system setup",
		}
	}
	return CheckResult{
		Name:    "Running as root",
		Status:  "fail",
		Message: "Not running as root",
		Help:    "Run with sudo or as root user",
	}
}

func checkOS() CheckResult {
	if runtime.GOOS != "linux" {
		return CheckResult{
			Name:    "Operating System",
			Status:  "fail",
			Message: fmt.Sprintf("Detected: %s", runtime.GOOS),
			Help:    "Mail server requires Linux",
		}
	}

	// Check for systemd-based distro
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return CheckResult{
			Name:    "Operating System",
			Status:  "pass",
			Message: "Linux with systemd detected",
		}
	}

	return CheckResult{
		Name:    "Operating System",
		Status:  "warn",
		Message: "Linux detected but systemd not found",
		Help:    "Manual service setup may be required",
	}
}

func checkMemory() CheckResult {
	// Use /proc/meminfo on Linux, skip on other platforms
	if runtime.GOOS != "linux" {
		return CheckResult{
			Name:    "Memory",
			Status:  "pass",
			Message: "Memory check skipped (non-Linux)",
		}
	}

	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return CheckResult{
			Name:    "Memory",
			Status:  "warn",
			Message: "Could not check memory",
		}
	}

	var totalKB int64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fmt.Sscanf(line, "MemTotal: %d kB", &totalKB)
			break
		}
	}

	totalMB := totalKB / 1024
	if totalMB >= 512 {
		return CheckResult{
			Name:    "Memory",
			Status:  "pass",
			Message: fmt.Sprintf("%d MB available (minimum 512 MB)", totalMB),
		}
	}

	return CheckResult{
		Name:    "Memory",
		Status:  "fail",
		Message: fmt.Sprintf("Only %d MB available", totalMB),
		Help:    "Minimum 512 MB RAM required",
	}
}

func checkDiskSpace() CheckResult {
	// Use df command for cross-platform compatibility
	cmd := exec.Command("df", "-BG", "/")
	output, err := cmd.Output()
	if err != nil {
		// Try without -BG for macOS
		cmd = exec.Command("df", "-g", "/")
		output, err = cmd.Output()
		if err != nil {
			return CheckResult{
				Name:    "Disk Space",
				Status:  "warn",
				Message: "Could not check disk space",
			}
		}
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return CheckResult{
			Name:    "Disk Space",
			Status:  "warn",
			Message: "Could not parse disk space",
		}
	}

	// Parse the available space (4th column typically)
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return CheckResult{
			Name:    "Disk Space",
			Status:  "warn",
			Message: "Could not parse disk space",
		}
	}

	availStr := strings.TrimSuffix(fields[3], "G")
	var freeGB int64
	fmt.Sscanf(availStr, "%d", &freeGB)

	if freeGB >= 1 {
		return CheckResult{
			Name:    "Disk Space",
			Status:  "pass",
			Message: fmt.Sprintf("%d GB free (minimum 1 GB)", freeGB),
		}
	}

	return CheckResult{
		Name:    "Disk Space",
		Status:  "fail",
		Message: fmt.Sprintf("Only %d GB free", freeGB),
		Help:    "Minimum 1 GB free space required",
	}
}

func checkPortAvailable(port int, name string) CheckResult {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		// Check if it's already in use by our service
		if strings.Contains(err.Error(), "address already in use") {
			return CheckResult{
				Name:    fmt.Sprintf("Port %d (%s)", port, name),
				Status:  "warn",
				Message: "Port already in use",
				Help:    "Make sure no other service is using this port",
			}
		}
		return CheckResult{
			Name:    fmt.Sprintf("Port %d (%s)", port, name),
			Status:  "fail",
			Message: err.Error(),
			Help:    "Check firewall or other services",
		}
	}
	ln.Close()

	return CheckResult{
		Name:    fmt.Sprintf("Port %d (%s)", port, name),
		Status:  "pass",
		Message: "Available",
	}
}

func checkPort25() CheckResult {
	return checkPortAvailable(25, "SMTP")
}

func checkPort143() CheckResult {
	return checkPortAvailable(143, "IMAP")
}

func checkPort465() CheckResult {
	return checkPortAvailable(465, "SMTPS")
}

func checkPort587() CheckResult {
	return checkPortAvailable(587, "Submission")
}

func checkPort993() CheckResult {
	return checkPortAvailable(993, "IMAPS")
}

func checkPort25Outbound() CheckResult {
	// Try to connect to a known mail server on port 25
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", "gmail-smtp-in.l.google.com:25")
	if err != nil {
		return CheckResult{
			Name:    "Outbound Port 25",
			Status:  "fail",
			Message: "Cannot connect to external mail servers",
			Help:    "Your provider may block port 25. Contact them to unblock it.",
		}
	}
	conn.Close()

	return CheckResult{
		Name:    "Outbound Port 25",
		Status:  "pass",
		Message: "Can connect to external mail servers",
	}
}

func checkRedis() CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", "localhost:6379")
	if err != nil {
		return CheckResult{
			Name:    "Redis",
			Status:  "fail",
			Message: "Redis not reachable on localhost:6379",
			Help:    "Install Redis: apt install redis-server",
		}
	}
	conn.Close()

	return CheckResult{
		Name:    "Redis",
		Status:  "pass",
		Message: "Redis is running",
	}
}

func checkGo() CheckResult {
	cmd := exec.Command("go", "version")
	output, err := cmd.Output()
	if err != nil {
		return CheckResult{
			Name:    "Go Compiler",
			Status:  "fail",
			Message: "Go not installed",
			Help:    "Install Go: https://golang.org/dl/",
		}
	}

	version := strings.TrimSpace(string(output))
	return CheckResult{
		Name:    "Go Compiler",
		Status:  "pass",
		Message: version,
	}
}

func checkSystemd() CheckResult {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return CheckResult{
			Name:    "Systemd",
			Status:  "fail",
			Message: "Systemd not detected",
			Help:    "This installer requires systemd",
		}
	}

	return CheckResult{
		Name:    "Systemd",
		Status:  "pass",
		Message: "Systemd is available",
	}
}
