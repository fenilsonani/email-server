package doctor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/storage/metadata"
)

// Infrastructure Checks

// checkDatabaseDriver checks database connection with driver detection.
func checkDatabaseDriver(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "database",
		Name:     "Database",
		Category: CategoryInfra,
		Details:  make(map[string]interface{}),
	}

	driver := cfg.Database.Driver
	if driver == "" {
		driver = "sqlite3"
	}
	result.Details["driver"] = driver

	var db metadata.Store
	var err error

	// Open database based on driver
	db, err = metadata.OpenFromConfig(cfg.Database)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Cannot open database: %v", err)
		result.Help = "Check database configuration in config.yaml"
		return result
	}
	defer db.Close()

	// Ping database
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := db.Ping(pingCtx); err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Database not responding: %v", err)
		result.Help = "Check if database service is running"
		return result
	}

	// Count tables
	var tableCount int
	if driver == "postgres" {
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
		`).Scan(&tableCount)
	} else {
		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'
		`).Scan(&tableCount)
	}

	if err != nil {
		result.Status = StatusWarn
		result.Message = "Connected but cannot count tables"
		return result
	}

	result.Details["tables"] = tableCount

	// Check if core tables exist
	var usersExists int
	if driver == "postgres" {
		db.QueryRowContext(ctx, `
			SELECT 1 FROM information_schema.tables WHERE table_name = 'users'
		`).Scan(&usersExists)
	} else {
		db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'
		`).Scan(&usersExists)
	}

	if usersExists == 0 {
		result.Status = StatusFail
		result.Message = "Database tables missing"
		result.Help = "Run: mailserver migrate"
		result.FixID = "migrations-pending"
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("%s connected, %d tables", strings.Title(driver), tableCount)
	return result
}

// checkDiskSpace checks available disk space.
func checkDiskSpace(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "disk-space",
		Name:     "Disk Space",
		Category: CategoryInfra,
		Details:  make(map[string]interface{}),
	}

	path := cfg.Storage.MaildirPath
	if path == "" {
		path = cfg.Storage.DataDir
	}

	// Use df command for cross-platform compatibility
	cmd := exec.CommandContext(ctx, "df", "-BG", path)
	output, err := cmd.Output()
	if err != nil {
		// Try macOS format
		cmd = exec.CommandContext(ctx, "df", "-g", path)
		output, err = cmd.Output()
		if err != nil {
			result.Status = StatusWarn
			result.Message = "Could not check disk space"
			return result
		}
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		result.Status = StatusWarn
		result.Message = "Could not parse disk space"
		return result
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		result.Status = StatusWarn
		result.Message = "Could not parse disk space"
		return result
	}

	availStr := strings.TrimSuffix(fields[3], "G")
	var freeGB int64
	fmt.Sscanf(availStr, "%d", &freeGB)

	usedPercentStr := strings.TrimSuffix(fields[4], "%")
	var usedPercent int64
	fmt.Sscanf(usedPercentStr, "%d", &usedPercent)

	result.Details["free_gb"] = freeGB
	result.Details["used_percent"] = usedPercent
	result.Details["path"] = path

	if freeGB < 1 {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Only %d GB free (%d%% used)", freeGB, usedPercent)
		result.Help = "Free up disk space or add storage"
		return result
	} else if usedPercent > 90 {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("%d GB free (%d%% used) - critically low", freeGB, usedPercent)
		result.Help = "Free up disk space immediately"
		return result
	} else if usedPercent > 80 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("%d GB free (%d%% used)", freeGB, usedPercent)
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("%d GB free (%d%% used)", freeGB, usedPercent)
	return result
}

// checkMaildirPermissions checks maildir directory permissions.
func checkMaildirPermissions(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "maildir-permissions",
		Name:     "Maildir Permissions",
		Category: CategoryInfra,
		Details:  make(map[string]interface{}),
	}

	path := cfg.Storage.MaildirPath
	result.Details["path"] = path

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		result.Status = StatusFail
		result.Message = "Maildir directory does not exist"
		result.Help = fmt.Sprintf("Create: mkdir -p %s", path)
		result.FixID = "maildir-missing"
		return result
	}

	if !info.IsDir() {
		result.Status = StatusFail
		result.Message = "Maildir path is not a directory"
		return result
	}

	// Check write permission
	testFile := path + "/.write_test"
	f, err := os.Create(testFile)
	if err != nil {
		result.Status = StatusFail
		result.Message = "Maildir is not writable"
		result.Help = fmt.Sprintf("Fix permissions: chown mailserver:mailserver %s", path)
		result.FixID = "maildir-permissions"
		return result
	}
	f.Close()
	os.Remove(testFile)

	mode := info.Mode().Perm()
	result.Details["mode"] = fmt.Sprintf("%04o", mode)

	// Check for world-readable/writable (security risk)
	if mode&0007 != 0 {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Maildir is world-accessible (%04o) - security risk!", mode)
		result.Help = fmt.Sprintf("Fix: chmod 0750 %s", path)
		result.FixID = "maildir-permissions"
		return result
	}

	// 0750 or 0700 are acceptable
	if mode&0070 != 0 && mode&0020 != 0 {
		// Group write is risky
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("Maildir has group-write permission (%04o)", mode)
		result.Help = fmt.Sprintf("Consider: chmod 0750 %s", path)
		result.FixID = "maildir-permissions"
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("Maildir permissions OK (%04o)", mode)
	return result
}

// checkMemoryUsage checks memory and goroutine stats.
func checkMemoryUsage(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "memory-usage",
		Name:     "Memory Usage",
		Category: CategoryInfra,
		Details:  make(map[string]interface{}),
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	heapMB := m.HeapAlloc / 1024 / 1024
	sysMB := m.Sys / 1024 / 1024
	goroutines := runtime.NumGoroutine()

	result.Details["heap_mb"] = heapMB
	result.Details["sys_mb"] = sysMB
	result.Details["goroutines"] = goroutines
	result.Details["gc_cycles"] = m.NumGC

	// Warning thresholds
	if heapMB > 500 || goroutines > 10000 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("Heap: %d MB, Goroutines: %d", heapMB, goroutines)
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("Heap: %d MB, Goroutines: %d", heapMB, goroutines)
	return result
}

// Network Checks

// checkServiceRunning checks if mail services are listening.
func checkServiceRunning(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "services-running",
		Name:     "Mail Services",
		Category: CategoryNetwork,
		Details:  make(map[string]interface{}),
	}

	ports := map[string]int{
		"SMTP":       cfg.Server.SMTPPort,
		"Submission": cfg.Server.SubmissionPort,
		"IMAP":       cfg.Server.IMAPPort,
	}

	running := 0
	total := len(ports)
	runningPorts := make([]string, 0)
	failedPorts := make([]string, 0)

	for name, port := range ports {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
		if err == nil {
			conn.Close()
			running++
			runningPorts = append(runningPorts, name)
		} else {
			failedPorts = append(failedPorts, name)
		}
	}

	result.Details["running"] = runningPorts
	result.Details["failed"] = failedPorts

	if running == total {
		result.Status = StatusPass
		result.Message = "All services are listening"
		return result
	} else if running > 0 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("%d/%d services running (missing: %s)", running, total, strings.Join(failedPorts, ", "))
		result.Help = "Check: systemctl status mailserver"
		return result
	}

	result.Status = StatusFail
	result.Message = "Mail server is not running"
	result.Help = "Start with: systemctl start mailserver"
	result.FixID = "mailserver-down"
	return result
}

// checkHealthEndpoint checks the health endpoint.
func checkHealthEndpoint(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "health-endpoint",
		Name:     "Health Endpoint",
		Category: CategoryNetwork,
		Details:  make(map[string]interface{}),
	}

	if !cfg.Admin.Enabled {
		result.Status = StatusWarn
		result.Message = "Admin server not enabled"
		return result
	}

	url := fmt.Sprintf("http://%s:%d/health", cfg.Admin.Listen, cfg.Admin.Port)
	result.Details["url"] = url

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		result.Status = StatusFail
		result.Message = "Cannot reach health endpoint"
		result.Help = "Check if admin server is running"
		result.FixID = "mailserver-restart"
		return result
	}
	defer resp.Body.Close()

	result.Details["status_code"] = resp.StatusCode

	if resp.StatusCode == 200 {
		result.Status = StatusPass
		result.Message = "Health endpoint responding OK"
		return result
	}

	result.Status = StatusWarn
	result.Message = fmt.Sprintf("Health endpoint returned %d", resp.StatusCode)
	return result
}

// checkRedisConnection checks Redis connectivity.
func checkRedisConnection(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "redis",
		Name:     "Redis",
		Category: CategoryNetwork,
		Details:  make(map[string]interface{}),
	}

	result.Details["url"] = cfg.Queue.RedisURL
	result.Details["mode"] = cfg.Queue.Mode

	if q == nil {
		// Try to connect directly
		var d net.Dialer
		connCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		conn, err := d.DialContext(connCtx, "tcp", "localhost:6379")
		if err != nil {
			result.Status = StatusFail
			result.Message = "Redis not reachable"
			result.Help = "Check: systemctl status redis"
			result.FixID = "redis-down"
			return result
		}
		conn.Close()

		result.Status = StatusPass
		result.Message = "Redis is running"
		return result
	}

	// Use queue client for ping
	start := time.Now()
	err := q.Client().Ping(ctx).Err()
	latency := time.Since(start)

	result.Details["latency_ms"] = float64(latency.Microseconds()) / 1000

	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Redis ping failed: %v", err)
		result.Help = "Check Redis connection settings"
		result.FixID = "redis-restart"
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("Redis connected (latency: %.2fms)", float64(latency.Microseconds())/1000)
	return result
}

// checkOutboundSMTP checks if outbound port 25 is reachable.
func checkOutboundSMTP(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "outbound-smtp",
		Name:     "Outbound SMTP",
		Category: CategoryNetwork,
		Details:  make(map[string]interface{}),
	}

	// Try to connect to a well-known mail server
	testServers := []string{
		"gmail-smtp-in.l.google.com:25",
		"outlook-com.olc.protection.outlook.com:25",
	}

	for _, server := range testServers {
		conn, err := net.DialTimeout("tcp", server, 5*time.Second)
		if err == nil {
			conn.Close()
			result.Status = StatusPass
			result.Message = "Outbound port 25 is open"
			result.Details["test_server"] = server
			return result
		}
	}

	result.Status = StatusWarn
	result.Message = "Outbound port 25 may be blocked"
	result.Help = "Contact hosting provider to unblock port 25, or use a relay host"
	return result
}

// Security Checks

// checkTLSCertificates checks TLS certificate configuration.
func checkTLSCertificates(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "tls-certificates",
		Name:     "TLS Certificates",
		Category: CategorySecurity,
		Details:  make(map[string]interface{}),
	}

	certFile := cfg.TLS.CertFile
	keyFile := cfg.TLS.KeyFile

	if certFile == "" || keyFile == "" {
		if cfg.TLS.AutoTLS {
			result.Status = StatusPass
			result.Message = "Using auto TLS (Let's Encrypt)"
			result.Details["auto_tls"] = true
			return result
		}
		result.Status = StatusFail
		result.Message = "TLS not configured"
		result.Help = "Configure tls.cert_file and tls.key_file"
		return result
	}

	result.Details["cert_file"] = certFile
	result.Details["key_file"] = keyFile

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Cannot load certificates: %v", err)
		result.Help = "Check certificate and key file paths and permissions"
		return result
	}

	result.Details["certificates"] = len(cert.Certificate)
	result.Status = StatusPass
	result.Message = "Certificates loaded successfully"
	return result
}

// checkCertExpiry checks certificate expiration.
func checkCertExpiry(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "cert-expiry",
		Name:     "Certificate Expiry",
		Category: CategorySecurity,
		Details:  make(map[string]interface{}),
	}

	certFile := cfg.TLS.CertFile
	if certFile == "" {
		if cfg.TLS.AutoTLS {
			result.Status = StatusPass
			result.Message = "Auto TLS handles renewal automatically"
			return result
		}
		result.Status = StatusWarn
		result.Message = "No certificate configured"
		return result
	}

	// Read certificate
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Cannot read certificate: %v", err)
		return result
	}

	// Parse certificate
	block, _ := parsePEMBlock(certPEM)
	if block == nil {
		result.Status = StatusFail
		result.Message = "Cannot parse certificate PEM"
		return result
	}

	cert, err := x509.ParseCertificate(block)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Cannot parse certificate: %v", err)
		return result
	}

	daysUntilExpiry := int(time.Until(cert.NotAfter).Hours() / 24)
	result.Details["expires"] = cert.NotAfter.Format("2006-01-02")
	result.Details["days_until_expiry"] = daysUntilExpiry
	result.Details["common_name"] = cert.Subject.CommonName

	if daysUntilExpiry <= 0 {
		result.Status = StatusFail
		result.Message = "Certificate has expired!"
		result.Help = "Renew certificate immediately"
		result.FixID = "cert-expired"
		return result
	} else if daysUntilExpiry <= 7 {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Certificate expires in %d days!", daysUntilExpiry)
		result.Help = "Renew certificate: certbot renew"
		return result
	} else if daysUntilExpiry <= 14 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("Certificate expires in %d days", daysUntilExpiry)
		result.Help = "Consider renewing soon"
		return result
	} else if daysUntilExpiry <= 30 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("Certificate expires in %d days", daysUntilExpiry)
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("Certificate valid for %d days", daysUntilExpiry)
	return result
}

// parsePEMBlock extracts the first certificate from PEM data.
func parsePEMBlock(data []byte) ([]byte, []byte) {
	const pemBegin = "-----BEGIN CERTIFICATE-----"
	const pemEnd = "-----END CERTIFICATE-----"

	s := string(data)
	start := strings.Index(s, pemBegin)
	if start < 0 {
		return nil, nil
	}
	s = s[start+len(pemBegin):]
	end := strings.Index(s, pemEnd)
	if end < 0 {
		return nil, nil
	}

	// Base64 decode
	base64Data := strings.ReplaceAll(s[:end], "\n", "")
	base64Data = strings.ReplaceAll(base64Data, "\r", "")
	base64Data = strings.TrimSpace(base64Data)

	decoded := make([]byte, len(base64Data))
	n := base64Decode(decoded, []byte(base64Data))
	if n == 0 {
		return nil, nil
	}

	return decoded[:n], data[start+len(pemBegin)+end+len(pemEnd):]
}

// base64Decode is a simple base64 decoder.
func base64Decode(dst, src []byte) int {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	decode := make([]byte, 256)
	for i := range decode {
		decode[i] = 0xFF
	}
	for i := 0; i < len(alphabet); i++ {
		decode[alphabet[i]] = byte(i)
	}
	decode['='] = 0

	di := 0
	for len(src) >= 4 {
		// Check for padding before processing
		pad := 0
		if src[3] == '=' {
			pad++
		}
		if src[2] == '=' {
			pad++
		}

		v := uint32(decode[src[0]])<<18 | uint32(decode[src[1]])<<12 | uint32(decode[src[2]])<<6 | uint32(decode[src[3]])
		dst[di+0] = byte(v >> 16)
		if pad < 2 {
			dst[di+1] = byte(v >> 8)
		}
		if pad < 1 {
			dst[di+2] = byte(v)
		}
		di += 3 - pad
		src = src[4:]
	}
	return di
}

// checkAllDomainsDKIM checks DKIM keys for all domains.
func checkAllDomainsDKIM(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "dkim-keys",
		Name:     "DKIM Keys",
		Category: CategorySecurity,
		Details:  make(map[string]interface{}),
	}

	if len(cfg.Domains) == 0 {
		result.Status = StatusWarn
		result.Message = "No domains configured"
		return result
	}

	domainStatus := make(map[string]string)
	missing := make([]string, 0)

	for _, domain := range cfg.Domains {
		if domain.DKIMKeyFile == "" {
			domainStatus[domain.Name] = "not configured"
			missing = append(missing, domain.Name)
			continue
		}

		if _, err := os.Stat(domain.DKIMKeyFile); os.IsNotExist(err) {
			domainStatus[domain.Name] = "file missing"
			missing = append(missing, domain.Name)
			continue
		}

		domainStatus[domain.Name] = "ok"
	}

	result.Details["domains"] = domainStatus

	if len(missing) > 0 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("DKIM missing for: %s", strings.Join(missing, ", "))
		result.Help = "Run: mailserver dkim generate --domain <domain>"
		if len(missing) == 1 {
			result.FixID = fmt.Sprintf("dkim-missing-%s", missing[0])
		}
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("DKIM configured for %d domain(s)", len(cfg.Domains))
	return result
}

// DNS Checks

// checkAllDomainsDNS checks DNS records for all domains.
func checkAllDomainsDNS(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "dns-records",
		Name:     "DNS Records",
		Category: CategoryDNS,
		Details:  make(map[string]interface{}),
	}

	if len(cfg.Domains) == 0 {
		result.Status = StatusWarn
		result.Message = "No domains configured"
		return result
	}

	domainIssues := make(map[string][]string)
	allGood := true

	for _, domain := range cfg.Domains {
		issues := checkDomainDNSRecords(domain.Name)
		if len(issues) > 0 {
			domainIssues[domain.Name] = issues
			allGood = false
		}
	}

	result.Details["domain_issues"] = domainIssues

	if !allGood {
		issueCount := 0
		for _, issues := range domainIssues {
			issueCount += len(issues)
		}
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("%d DNS issue(s) found across domains", issueCount)
		result.Help = "Run: mailserver dns check <domain>"
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("DNS records OK for %d domain(s)", len(cfg.Domains))
	return result
}

// checkDomainDNSRecords checks DNS for a single domain.
func checkDomainDNSRecords(domain string) []string {
	issues := []string{}

	// Check MX
	mxRecords, err := net.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		issues = append(issues, "MX record missing")
	}

	// Check SPF
	txtRecords, _ := net.LookupTXT(domain)
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
	dmarcRecords, _ := net.LookupTXT("_dmarc." + domain)
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

	return issues
}

// Config Checks

// checkConfigPorts verifies config ports match listening ports.
func checkConfigPorts(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "config-ports",
		Name:     "Port Configuration",
		Category: CategoryConfig,
		Details:  make(map[string]interface{}),
	}

	mismatches := make([]string, 0)
	portChecks := map[string]int{
		"SMTP":       cfg.Server.SMTPPort,
		"Submission": cfg.Server.SubmissionPort,
		"IMAP":       cfg.Server.IMAPPort,
	}

	for name, port := range portChecks {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("%s:%d", name, port))
		} else {
			conn.Close()
		}
	}

	result.Details["configured_ports"] = portChecks
	result.Details["mismatches"] = mismatches

	if len(mismatches) > 0 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("Ports not listening: %s", strings.Join(mismatches, ", "))
		result.Help = "Verify server is running and ports match configuration"
		return result
	}

	result.Status = StatusPass
	result.Message = "All configured ports are listening"
	return result
}

// Queue Checks

// checkQueueHealth checks queue pending/failed counts.
func checkQueueHealth(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "queue-health",
		Name:     "Queue Health",
		Category: CategoryQueue,
		Details:  make(map[string]interface{}),
	}

	if q == nil {
		result.Status = StatusWarn
		result.Message = "Queue not available"
		return result
	}

	stats, err := q.Stats(ctx)
	if err != nil {
		result.Status = StatusFail
		result.Message = fmt.Sprintf("Cannot get queue stats: %v", err)
		return result
	}

	result.Details["pending"] = stats.Pending
	result.Details["processing"] = stats.Processing
	result.Details["sent"] = stats.Sent
	result.Details["failed"] = stats.Failed
	result.Details["total_enqueued"] = stats.TotalEnqueued
	result.Details["total_sent"] = stats.TotalSent
	result.Details["total_failed"] = stats.TotalFailed

	if stats.Failed > 100 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("High failed message count: %d", stats.Failed)
		result.Help = "Review failed messages: mailserver queue list --status failed"
		return result
	}

	if stats.Pending > 1000 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("High pending message count: %d", stats.Pending)
		return result
	}

	result.Status = StatusPass
	result.Message = fmt.Sprintf("Pending: %d, Processing: %d, Failed: %d", stats.Pending, stats.Processing, stats.Failed)
	return result
}

// checkQueueStale checks for messages stuck in processing too long.
func checkQueueStale(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "queue-stale",
		Name:     "Stale Messages",
		Category: CategoryQueue,
		Details:  make(map[string]interface{}),
	}

	if q == nil {
		result.Status = StatusWarn
		result.Message = "Queue not available"
		return result
	}

	// Check for messages in processing for > 1 hour
	staleThreshold := time.Hour
	processing, err := q.ProcessingCount(ctx)
	if err != nil {
		result.Status = StatusWarn
		result.Message = "Cannot check processing queue"
		return result
	}

	result.Details["processing"] = processing
	result.Details["stale_threshold"] = staleThreshold.String()

	if processing > 0 {
		// Try to recover stale messages
		recovered, err := q.RecoverStale(ctx, staleThreshold)
		if err != nil {
			result.Status = StatusWarn
			result.Message = fmt.Sprintf("%d messages in processing, recovery failed", processing)
			return result
		}

		if recovered > 0 {
			result.Status = StatusWarn
			result.Message = fmt.Sprintf("Recovered %d stale messages", recovered)
			result.Details["recovered"] = recovered
			result.FixID = "queue-stale"
			return result
		}
	}

	// Also check for very old pending messages (> 24h)
	pending, err := q.ListPending(ctx, 100)
	if err != nil {
		result.Status = StatusPass
		result.Message = "No stale messages detected"
		return result
	}

	staleCount := 0
	for _, msg := range pending {
		if time.Since(msg.CreatedAt) > 24*time.Hour {
			staleCount++
		}
	}

	result.Details["stale_pending"] = staleCount

	if staleCount > 0 {
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("%d messages pending > 24 hours", staleCount)
		result.Help = "Review with: mailserver queue list --status pending"
		return result
	}

	result.Status = StatusPass
	result.Message = "No stale messages"
	return result
}

// checkDatabasePermissions checks database file permissions for security.
func checkDatabasePermissions(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "db-permissions",
		Name:     "Database Permissions",
		Category: CategorySecurity,
		Details:  make(map[string]interface{}),
	}

	dbPath := cfg.Storage.DatabasePath
	if dbPath == "" {
		result.Status = StatusWarn
		result.Message = "Database path not configured"
		return result
	}

	result.Details["path"] = dbPath

	// Check all database files (.db, .db-wal, .db-shm)
	files := []string{dbPath, dbPath + "-wal", dbPath + "-shm"}
	worstStatus := StatusPass
	var issues []string

	for _, path := range files {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue // WAL/SHM may not exist yet
		}
		if err != nil {
			continue
		}

		mode := info.Mode().Perm()
		result.Details[filepath.Base(path)+"_mode"] = fmt.Sprintf("%04o", mode)

		// Check if mode is too permissive (> 0640)
		if mode&0007 != 0 {
			// World-readable or world-writable
			worstStatus = StatusFail
			issues = append(issues, fmt.Sprintf("%s is world-accessible (%04o)", filepath.Base(path), mode))
		} else if mode > 0640 {
			if worstStatus != StatusFail {
				worstStatus = StatusWarn
			}
			issues = append(issues, fmt.Sprintf("%s has permissive mode (%04o)", filepath.Base(path), mode))
		}
	}

	if len(issues) > 0 {
		result.Status = worstStatus
		result.Message = strings.Join(issues, "; ")
		result.Help = fmt.Sprintf("Fix: chmod 640 %s %s-wal %s-shm", dbPath, dbPath, dbPath)
		result.FixID = "db-permissions"
		return result
	}

	result.Status = StatusPass
	result.Message = "Database file permissions OK"
	return result
}

// checkTLSALPN verifies ALPN is properly configured for mail protocols.
// This catches the bug where mail servers incorrectly advertise ALPN protocols,
// causing connection failures with clients like Apple Mail.
func checkTLSALPN(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) CheckResult {
	result := CheckResult{
		ID:       "tls-alpn",
		Name:     "TLS ALPN Configuration",
		Category: CategorySecurity,
		Details:  make(map[string]interface{}),
	}

	// Skip if TLS is not configured
	if cfg.TLS.CertFile == "" && !cfg.TLS.AutoTLS {
		result.Status = StatusPass
		result.Message = "TLS not configured, ALPN check skipped"
		return result
	}

	// Test IMAPS port for ALPN handling
	imapsPort := cfg.Server.IMAPSPort
	if imapsPort == 0 {
		imapsPort = 993
	}

	result.Details["imaps_port"] = imapsPort

	// Connect with ALPN enabled (simulating Apple Mail behavior)
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 5 * time.Second},
		"tcp",
		fmt.Sprintf("127.0.0.1:%d", imapsPort),
		&tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"imap"}, // Simulate Apple Mail ALPN
		},
	)

	if err != nil {
		// Check if the error is specifically about ALPN
		errStr := err.Error()
		if strings.Contains(errStr, "no application protocol") ||
			strings.Contains(errStr, "unsupported application protocols") {
			result.Status = StatusFail
			result.Message = "IMAP server incorrectly rejecting ALPN - Apple Mail will fail"
			result.Help = "Ensure IMAP server uses MailTLSConfig() which disables ALPN"
			result.Details["error"] = errStr
			return result
		}

		// Connection failed for other reasons (service not running, etc.)
		result.Status = StatusWarn
		result.Message = fmt.Sprintf("Could not test IMAPS: %v", err)
		result.Help = "Ensure IMAPS server is running on port " + fmt.Sprintf("%d", imapsPort)
		return result
	}
	defer conn.Close()

	// Connection succeeded - ALPN is properly handled
	result.Details["negotiated_protocol"] = conn.ConnectionState().NegotiatedProtocol
	result.Details["alpn_handled"] = true

	result.Status = StatusPass
	result.Message = "TLS ALPN correctly configured for mail protocols"
	return result
}
