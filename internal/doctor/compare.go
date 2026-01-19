package doctor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/queue"
)

// Comparison represents a comparison between config and reality.
type Comparison struct {
	Name        string      `json:"name"`
	ConfigPath  string      `json:"config_path"`
	ConfigValue interface{} `json:"config_value"`
	ActualValue interface{} `json:"actual_value,omitempty"`
	Matches     bool        `json:"matches"`
	Message     string      `json:"message"`
	Status      Status      `json:"status"`
}

// ComparisonResults holds all comparison results.
type ComparisonResults struct {
	Comparisons []Comparison  `json:"comparisons"`
	Matched     int           `json:"matched"`
	Mismatched  int           `json:"mismatched"`
	Errors      int           `json:"errors"`
	Duration    time.Duration `json:"duration"`
}

// CompareConfigToReality compares configuration values against actual runtime state.
func CompareConfigToReality(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) *ComparisonResults {
	startTime := time.Now()
	results := &ComparisonResults{
		Comparisons: make([]Comparison, 0),
	}

	// Port comparisons
	results.Comparisons = append(results.Comparisons, comparePort("SMTP", "server.smtp_port", cfg.Server.SMTPPort))
	results.Comparisons = append(results.Comparisons, comparePort("Submission", "server.submission_port", cfg.Server.SubmissionPort))
	results.Comparisons = append(results.Comparisons, comparePort("SMTPS", "server.smtps_port", cfg.Server.SMTPSPort))
	results.Comparisons = append(results.Comparisons, comparePort("IMAP", "server.imap_port", cfg.Server.IMAPPort))
	results.Comparisons = append(results.Comparisons, comparePort("IMAPS", "server.imaps_port", cfg.Server.IMAPSPort))

	// Redis comparison
	if q != nil {
		results.Comparisons = append(results.Comparisons, compareRedis(ctx, cfg, q))
	}

	// TLS certificate comparison
	if cfg.TLS.CertFile != "" {
		results.Comparisons = append(results.Comparisons, compareTLSCert(cfg))
	}

	// Domain DNS comparisons
	for _, domain := range cfg.Domains {
		results.Comparisons = append(results.Comparisons, compareDomainDNS(domain.Name))
		if domain.DKIMKeyFile != "" {
			results.Comparisons = append(results.Comparisons, compareDKIMKey(domain.Name, domain.DKIMKeyFile, domain.DKIMSelector))
		}
	}

	// Storage path comparisons
	results.Comparisons = append(results.Comparisons, compareDirectory("Maildir", "storage.maildir_path", cfg.Storage.MaildirPath))
	results.Comparisons = append(results.Comparisons, compareDirectory("Data Directory", "storage.data_dir", cfg.Storage.DataDir))

	// Admin server comparison
	if cfg.Admin.Enabled {
		results.Comparisons = append(results.Comparisons, compareAdminServer(cfg))
	}

	// Count results
	for _, comp := range results.Comparisons {
		switch comp.Status {
		case StatusPass:
			results.Matched++
		case StatusFail:
			results.Mismatched++
		case StatusWarn:
			results.Errors++
		}
	}

	results.Duration = time.Since(startTime)
	return results
}

// comparePort checks if a port is listening.
func comparePort(name, configPath string, port int) Comparison {
	comp := Comparison{
		Name:        fmt.Sprintf("%s Port", name),
		ConfigPath:  configPath,
		ConfigValue: port,
	}

	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		comp.ActualValue = "not listening"
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = fmt.Sprintf("Port %d configured but not listening", port)
		return comp
	}
	conn.Close()

	comp.ActualValue = "listening"
	comp.Matches = true
	comp.Status = StatusPass
	comp.Message = fmt.Sprintf("Port %d is listening as configured", port)
	return comp
}

// compareRedis checks Redis connection.
func compareRedis(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) Comparison {
	comp := Comparison{
		Name:        "Redis Connection",
		ConfigPath:  "queue.redis_url",
		ConfigValue: cfg.Queue.RedisURL,
	}

	start := time.Now()
	err := q.Client().Ping(ctx).Err()
	latency := time.Since(start)

	if err != nil {
		comp.ActualValue = fmt.Sprintf("error: %v", err)
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = "Redis connection failed"
		return comp
	}

	comp.ActualValue = fmt.Sprintf("connected (%.2fms)", float64(latency.Microseconds())/1000)
	comp.Matches = true
	comp.Status = StatusPass
	comp.Message = "Redis connection successful"
	return comp
}

// compareTLSCert checks TLS certificate validity.
func compareTLSCert(cfg *config.Config) Comparison {
	comp := Comparison{
		Name:        "TLS Certificate",
		ConfigPath:  "tls.cert_file",
		ConfigValue: cfg.TLS.CertFile,
	}

	// Load certificate
	cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
	if err != nil {
		comp.ActualValue = fmt.Sprintf("load error: %v", err)
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = "Certificate cannot be loaded"
		return comp
	}

	// Parse to check validity
	if len(cert.Certificate) == 0 {
		comp.ActualValue = "no certificates"
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = "No certificates in file"
		return comp
	}

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		comp.ActualValue = fmt.Sprintf("parse error: %v", err)
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = "Certificate cannot be parsed"
		return comp
	}

	now := time.Now()
	if now.After(x509Cert.NotAfter) {
		comp.ActualValue = fmt.Sprintf("expired: %s", x509Cert.NotAfter.Format("2006-01-02"))
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = "Certificate has expired"
		return comp
	}

	if now.Before(x509Cert.NotBefore) {
		comp.ActualValue = fmt.Sprintf("not yet valid: %s", x509Cert.NotBefore.Format("2006-01-02"))
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = "Certificate not yet valid"
		return comp
	}

	daysUntilExpiry := int(time.Until(x509Cert.NotAfter).Hours() / 24)
	comp.ActualValue = fmt.Sprintf("valid until %s (%d days)", x509Cert.NotAfter.Format("2006-01-02"), daysUntilExpiry)
	comp.Matches = true
	comp.Status = StatusPass
	comp.Message = "Certificate is valid"
	return comp
}

// compareDomainDNS checks if domain has MX records.
func compareDomainDNS(domain string) Comparison {
	comp := Comparison{
		Name:        fmt.Sprintf("Domain DNS (%s)", domain),
		ConfigPath:  "domains[].name",
		ConfigValue: domain,
	}

	mxRecords, err := net.LookupMX(domain)
	if err != nil {
		comp.ActualValue = "no MX records"
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = fmt.Sprintf("Domain %s has no MX record", domain)
		return comp
	}

	if len(mxRecords) == 0 {
		comp.ActualValue = "no MX records"
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = fmt.Sprintf("Domain %s has no MX record", domain)
		return comp
	}

	mxHosts := make([]string, len(mxRecords))
	for i, mx := range mxRecords {
		mxHosts[i] = strings.TrimSuffix(mx.Host, ".")
	}

	comp.ActualValue = strings.Join(mxHosts, ", ")
	comp.Matches = true
	comp.Status = StatusPass
	comp.Message = fmt.Sprintf("MX: %s", strings.Join(mxHosts, ", "))
	return comp
}

// compareDKIMKey checks if DKIM key file exists and is readable.
func compareDKIMKey(domain, keyFile, selector string) Comparison {
	comp := Comparison{
		Name:        fmt.Sprintf("DKIM Key (%s)", domain),
		ConfigPath:  "domains[].dkim_key_file",
		ConfigValue: keyFile,
	}

	info, err := os.Stat(keyFile)
	if os.IsNotExist(err) {
		comp.ActualValue = "file missing"
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = "DKIM key file does not exist"
		return comp
	}

	if err != nil {
		comp.ActualValue = fmt.Sprintf("error: %v", err)
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = "Cannot access DKIM key file"
		return comp
	}

	// Check if file is readable
	f, err := os.Open(keyFile)
	if err != nil {
		comp.ActualValue = "not readable"
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = "DKIM key file is not readable"
		return comp
	}
	f.Close()

	// Check DNS record
	dkimRecord := fmt.Sprintf("%s._domainkey.%s", selector, domain)
	txtRecords, _ := net.LookupTXT(dkimRecord)
	hasDKIMDNS := false
	for _, txt := range txtRecords {
		if strings.Contains(txt, "v=DKIM1") {
			hasDKIMDNS = true
			break
		}
	}

	if !hasDKIMDNS {
		comp.ActualValue = fmt.Sprintf("file exists (%d bytes), DNS record missing", info.Size())
		comp.Matches = false
		comp.Status = StatusWarn
		comp.Message = "DKIM key exists but DNS record not found"
		return comp
	}

	comp.ActualValue = fmt.Sprintf("file exists (%d bytes), DNS configured", info.Size())
	comp.Matches = true
	comp.Status = StatusPass
	comp.Message = "DKIM key and DNS record configured"
	return comp
}

// compareDirectory checks if a directory exists and is accessible.
func compareDirectory(name, configPath, path string) Comparison {
	comp := Comparison{
		Name:        name,
		ConfigPath:  configPath,
		ConfigValue: path,
	}

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		comp.ActualValue = "does not exist"
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = fmt.Sprintf("%s directory does not exist", name)
		return comp
	}

	if err != nil {
		comp.ActualValue = fmt.Sprintf("error: %v", err)
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = fmt.Sprintf("Cannot access %s directory", name)
		return comp
	}

	if !info.IsDir() {
		comp.ActualValue = "not a directory"
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = fmt.Sprintf("%s path is not a directory", name)
		return comp
	}

	comp.ActualValue = fmt.Sprintf("exists (mode: %04o)", info.Mode().Perm())
	comp.Matches = true
	comp.Status = StatusPass
	comp.Message = fmt.Sprintf("%s directory exists", name)
	return comp
}

// compareAdminServer checks if admin server is running.
func compareAdminServer(cfg *config.Config) Comparison {
	comp := Comparison{
		Name:        "Admin Server",
		ConfigPath:  "admin.port",
		ConfigValue: cfg.Admin.Port,
	}

	addr := net.JoinHostPort(cfg.Admin.Listen, fmt.Sprintf("%d", cfg.Admin.Port))
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		comp.ActualValue = "not listening"
		comp.Matches = false
		comp.Status = StatusFail
		comp.Message = "Admin server not responding"
		return comp
	}
	conn.Close()

	comp.ActualValue = "listening"
	comp.Matches = true
	comp.Status = StatusPass
	comp.Message = fmt.Sprintf("Admin server listening on %s", addr)
	return comp
}
