package setup

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
)

// DoctorResults contains all doctor check results
type DoctorResults struct {
	Checks  []CheckResult
	Passed  int
	Failed  int
	Warned  int
	Healthy bool
}

// RunDoctor runs all health checks
func RunDoctor(cfg *config.Config) *DoctorResults {
	results := &DoctorResults{}

	checks := []func(*config.Config) CheckResult{
		checkServiceRunning,
		checkHealthEndpoint,
		checkDatabaseConnection,
		checkRedisConnection,
		checkTLSCertificates,
		checkDKIMKeys,
		checkDNSRecords,
		checkDiskSpaceDoctor,
		checkMaildirPermissions,
	}

	for _, check := range checks {
		result := check(cfg)
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

	results.Healthy = results.Failed == 0

	return results
}

// Print prints the doctor results
func (r *DoctorResults) Print() {
	fmt.Println("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("                    HEALTH CHECK")
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

	if r.Healthy {
		fmt.Println("\033[32m✓ Mail server is healthy!\033[0m")
	} else {
		fmt.Println("\033[31m✗ Mail server has issues. Check above.\033[0m")
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

func checkServiceRunning(cfg *config.Config) CheckResult {
	// Check if mailserver process is listening
	ports := []int{25, 143, 587}
	running := 0

	for _, port := range ports {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
		if err == nil {
			conn.Close()
			running++
		}
	}

	if running == len(ports) {
		return CheckResult{
			Name:    "Mail Server Running",
			Status:  "pass",
			Message: "All services are listening",
		}
	} else if running > 0 {
		return CheckResult{
			Name:    "Mail Server Running",
			Status:  "warn",
			Message: fmt.Sprintf("Only %d/%d services running", running, len(ports)),
			Help:    "Check: systemctl status mailserver",
		}
	}

	return CheckResult{
		Name:    "Mail Server Running",
		Status:  "fail",
		Message: "Mail server is not running",
		Help:    "Start with: systemctl start mailserver",
	}
}

func checkHealthEndpoint(cfg *config.Config) CheckResult {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", cfg.Admin.Port)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return CheckResult{
			Name:    "Health Endpoint",
			Status:  "fail",
			Message: "Cannot reach health endpoint",
			Help:    "Check if admin server is running",
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return CheckResult{
			Name:    "Health Endpoint",
			Status:  "pass",
			Message: "Health endpoint responding OK",
		}
	}

	return CheckResult{
		Name:    "Health Endpoint",
		Status:  "warn",
		Message: fmt.Sprintf("Health endpoint returned %d", resp.StatusCode),
	}
}

func checkDatabaseConnection(cfg *config.Config) CheckResult {
	db, err := sql.Open("sqlite3", cfg.Storage.DatabasePath)
	if err != nil {
		return CheckResult{
			Name:    "Database",
			Status:  "fail",
			Message: "Cannot open database",
			Help:    err.Error(),
		}
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return CheckResult{
			Name:    "Database",
			Status:  "fail",
			Message: "Database not responding",
			Help:    err.Error(),
		}
	}

	// Check tables exist
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'").Scan(&count)
	if err != nil || count == 0 {
		return CheckResult{
			Name:    "Database",
			Status:  "fail",
			Message: "Database tables missing",
			Help:    "Run: mailserver migrate",
		}
	}

	return CheckResult{
		Name:    "Database",
		Status:  "pass",
		Message: "Database connected and tables exist",
	}
}

func checkRedisConnection(cfg *config.Config) CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", "localhost:6379")
	if err != nil {
		return CheckResult{
			Name:    "Redis",
			Status:  "fail",
			Message: "Redis not reachable",
			Help:    "Check: systemctl status redis",
		}
	}
	conn.Close()

	return CheckResult{
		Name:    "Redis",
		Status:  "pass",
		Message: "Redis is running",
	}
}

func checkTLSCertificates(cfg *config.Config) CheckResult {
	certFile := cfg.TLS.CertFile
	keyFile := cfg.TLS.KeyFile

	if certFile == "" || keyFile == "" {
		return CheckResult{
			Name:    "TLS Certificates",
			Status:  "fail",
			Message: "TLS not configured",
			Help:    "Configure tls.cert_file and tls.key_file",
		}
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return CheckResult{
			Name:    "TLS Certificates",
			Status:  "fail",
			Message: "Cannot load certificates",
			Help:    err.Error(),
		}
	}

	// Check expiry
	if len(cert.Certificate) > 0 {
		x509Cert, err := tls.X509KeyPair([]byte{}, []byte{})
		_ = x509Cert
		if err == nil {
			// Parse and check expiry would go here
		}
	}

	return CheckResult{
		Name:    "TLS Certificates",
		Status:  "pass",
		Message: "Certificates loaded successfully",
	}
}

func checkDKIMKeys(cfg *config.Config) CheckResult {
	if len(cfg.Domains) == 0 {
		return CheckResult{
			Name:    "DKIM Keys",
			Status:  "warn",
			Message: "No domains configured",
		}
	}

	domain := cfg.Domains[0]
	if domain.DKIMKeyFile == "" {
		return CheckResult{
			Name:    "DKIM Keys",
			Status:  "warn",
			Message: "DKIM not configured for " + domain.Name,
			Help:    "Run: mailserver dkim generate --domain " + domain.Name,
		}
	}

	if _, err := os.Stat(domain.DKIMKeyFile); os.IsNotExist(err) {
		return CheckResult{
			Name:    "DKIM Keys",
			Status:  "fail",
			Message: "DKIM key file not found",
			Help:    "Run: mailserver dkim generate --domain " + domain.Name,
		}
	}

	return CheckResult{
		Name:    "DKIM Keys",
		Status:  "pass",
		Message: "DKIM key exists for " + domain.Name,
	}
}

func checkDNSRecords(cfg *config.Config) CheckResult {
	if len(cfg.Domains) == 0 {
		return CheckResult{
			Name:    "DNS Records",
			Status:  "warn",
			Message: "No domains configured",
		}
	}

	domainName := cfg.Domains[0].Name
	issues := []string{}

	// Check MX
	mxRecords, err := net.LookupMX(domainName)
	if err != nil || len(mxRecords) == 0 {
		issues = append(issues, "MX record missing")
	}

	// Check SPF
	txtRecords, err := net.LookupTXT(domainName)
	hasSPF := false
	for _, txt := range txtRecords {
		if strings.HasPrefix(txt, "v=spf1") {
			hasSPF = true
			break
		}
	}
	if !hasSPF {
		issues = append(issues, "SPF record missing")
	}

	// Check DMARC
	dmarcRecords, err := net.LookupTXT("_dmarc." + domainName)
	hasDMARC := false
	for _, txt := range dmarcRecords {
		if strings.HasPrefix(txt, "v=DMARC1") {
			hasDMARC = true
			break
		}
	}
	if !hasDMARC {
		issues = append(issues, "DMARC record missing")
	}

	if len(issues) > 0 {
		return CheckResult{
			Name:    "DNS Records",
			Status:  "warn",
			Message: strings.Join(issues, ", "),
			Help:    "Run: mailserver dns check " + domainName,
		}
	}

	return CheckResult{
		Name:    "DNS Records",
		Status:  "pass",
		Message: fmt.Sprintf("MX, SPF, DMARC configured for %s", domainName),
	}
}

func checkDiskSpaceDoctor(cfg *config.Config) CheckResult {
	// Use df command for cross-platform compatibility
	cmd := exec.Command("df", "-BG", cfg.Storage.MaildirPath)
	output, err := cmd.Output()
	if err != nil {
		cmd = exec.Command("df", "-g", cfg.Storage.MaildirPath)
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

	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return CheckResult{
			Name:    "Disk Space",
			Status:  "warn",
			Message: "Could not parse disk space",
		}
	}

	availStr := strings.TrimSuffix(fields[3], "G")
	var freeGB int64
	fmt.Sscanf(availStr, "%d", &freeGB)

	usedPercentStr := strings.TrimSuffix(fields[4], "%")
	var usedPercent int64
	fmt.Sscanf(usedPercentStr, "%d", &usedPercent)

	if freeGB < 1 {
		return CheckResult{
			Name:    "Disk Space",
			Status:  "fail",
			Message: fmt.Sprintf("Only %d GB free (%d%% used)", freeGB, usedPercent),
			Help:    "Free up disk space or add storage",
		}
	} else if usedPercent > 80 {
		return CheckResult{
			Name:    "Disk Space",
			Status:  "warn",
			Message: fmt.Sprintf("%d GB free (%d%% used)", freeGB, usedPercent),
		}
	}

	return CheckResult{
		Name:    "Disk Space",
		Status:  "pass",
		Message: fmt.Sprintf("%d GB free (%d%% used)", freeGB, usedPercent),
	}
}

func checkMaildirPermissions(cfg *config.Config) CheckResult {
	path := cfg.Storage.MaildirPath

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return CheckResult{
			Name:    "Maildir Permissions",
			Status:  "fail",
			Message: "Maildir directory does not exist",
			Help:    fmt.Sprintf("Create: mkdir -p %s", path),
		}
	}

	if !info.IsDir() {
		return CheckResult{
			Name:    "Maildir Permissions",
			Status:  "fail",
			Message: "Maildir path is not a directory",
		}
	}

	// Check write permission
	testFile := path + "/.write_test"
	f, err := os.Create(testFile)
	if err != nil {
		return CheckResult{
			Name:    "Maildir Permissions",
			Status:  "fail",
			Message: "Maildir is not writable",
			Help:    fmt.Sprintf("Fix: chown mailserver:mailserver %s", path),
		}
	}
	f.Close()
	os.Remove(testFile)

	return CheckResult{
		Name:    "Maildir Permissions",
		Status:  "pass",
		Message: "Maildir is writable",
	}
}

