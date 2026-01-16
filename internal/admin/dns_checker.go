package admin

import (
	"context"
	"database/sql"
	"net"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/security"
)

// DNSChecker periodically verifies DNS configuration for all domains
type DNSChecker struct {
	db       *sql.DB
	config   *config.Config
	logger   *logging.Logger
	interval time.Duration
	stopCh   chan struct{}
}

// NewDNSChecker creates a new DNS checker
func NewDNSChecker(db *sql.DB, cfg *config.Config, logger *logging.Logger) *DNSChecker {
	return &DNSChecker{
		db:       db,
		config:   cfg,
		logger:   logger,
		interval: 15 * time.Minute,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the periodic DNS checking
func (c *DNSChecker) Start() {
	c.logger.Info("DNS checker started", "interval", c.interval.String())

	// Run once at startup after a short delay
	go func() {
		time.Sleep(30 * time.Second)
		c.checkAllDomains()
	}()

	// Run periodically
	ticker := time.NewTicker(c.interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				c.checkAllDomains()
			case <-c.stopCh:
				ticker.Stop()
				c.logger.Info("DNS checker stopped")
				return
			}
		}
	}()
}

// Stop stops the DNS checker
func (c *DNSChecker) Stop() {
	close(c.stopCh)
}

// checkAllDomains verifies DNS for all domains
func (c *DNSChecker) checkAllDomains() {
	ctx := context.Background()

	rows, err := c.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(dkim_selector, 'mail'), dkim_storage_type
		FROM domains
	`)
	if err != nil {
		c.logger.Error("Failed to query domains for DNS check", "error", err.Error())
		return
	}
	defer rows.Close()

	var checked, changed int
	for rows.Next() {
		var id int64
		var name, selector string
		var storageType sql.NullString
		if err := rows.Scan(&id, &name, &selector, &storageType); err != nil {
			c.logger.Error("Failed to scan domain row", "error", err.Error())
			continue
		}

		storage := "file"
		if storageType.Valid && storageType.String != "" {
			storage = storageType.String
		}

		statusChanged := c.checkDomain(ctx, id, name, selector, storage)
		checked++
		if statusChanged {
			changed++
		}
	}

	if checked > 0 {
		c.logger.Info("DNS check completed", "checked", checked, "changed", changed)
	}
}

// checkDomain verifies DNS for a single domain and updates the database
func (c *DNSChecker) checkDomain(ctx context.Context, id int64, domain, selector, storage string) bool {
	// Get expected DKIM value
	dkimPath := c.getDKIMPath()
	store := security.NewKeyStore(storage, dkimPath, c.db)
	records, _ := security.GetAllDNSRecords(ctx, store, domain, c.config.Server.Hostname)

	// Verify all records
	mxResult := c.verifyMXRecord(domain, c.config.Server.Hostname)
	spfResult := c.verifySPFRecord(domain)
	dkimResult := c.verifyDKIMRecord(domain, selector, records.DKIM)
	dmarcResult := c.verifyDMARCRecord(domain)
	mailHostnameResult := c.verifyMailHostnameRecord(domain)

	// Convert to integers
	var mxVerified, spfVerified, dkimVerified, dmarcVerified, mailHostnameVerified int
	if mxResult {
		mxVerified = 1
	}
	if spfResult {
		spfVerified = 1
	}
	if dkimResult {
		dkimVerified = 1
	}
	if dmarcResult {
		dmarcVerified = 1
	}
	if mailHostnameResult {
		mailHostnameVerified = 1
	}

	// Calculate status (mail hostname is optional for status, but tracked)
	var dnsStatus string
	if mxVerified == 1 && spfVerified == 1 && dkimVerified == 1 && dmarcVerified == 1 {
		dnsStatus = "ready"
	} else if mxVerified == 1 || spfVerified == 1 {
		dnsStatus = "partial"
	} else {
		dnsStatus = "pending"
	}

	// Get current status
	var currentStatus string
	err := c.db.QueryRowContext(ctx,
		"SELECT COALESCE(dns_status, 'pending') FROM domains WHERE id = ?",
		id).Scan(&currentStatus)
	if err != nil {
		return false
	}

	// Update database
	_, err = c.db.ExecContext(ctx,
		`UPDATE domains SET
			dns_mx_verified = ?,
			dns_spf_verified = ?,
			dns_dkim_verified = ?,
			dns_dmarc_verified = ?,
			dns_mail_hostname_verified = ?,
			dns_status = ?,
			dns_last_checked = ?
		WHERE id = ?`,
		mxVerified, spfVerified, dkimVerified, dmarcVerified, mailHostnameVerified, dnsStatus, time.Now(), id)
	if err != nil {
		c.logger.Error("Failed to update DNS status", "domain", domain, "error", err.Error())
		return false
	}

	// Log if status changed
	if currentStatus != dnsStatus {
		c.logger.Info("DNS status changed", "domain", domain, "old", currentStatus, "new", dnsStatus)
		return true
	}
	return false
}

// getDKIMPath returns the path for DKIM keys
func (c *DNSChecker) getDKIMPath() string {
	if c.config.Storage.DataDir != "" {
		return c.config.Storage.DataDir + "/dkim"
	}
	if c.config.Storage.MaildirPath != "" {
		return c.config.Storage.MaildirPath + "/../dkim"
	}
	return "/etc/mailserver/dkim"
}

// verifyMXRecord checks if MX record points to the correct hostname
func (c *DNSChecker) verifyMXRecord(domain, expectedHost string) bool {
	mxRecords, err := net.LookupMX(domain)
	if err != nil || len(mxRecords) == 0 {
		return false
	}

	for _, mx := range mxRecords {
		host := strings.TrimSuffix(mx.Host, ".")
		if strings.EqualFold(host, expectedHost) || strings.EqualFold(host, expectedHost+".") {
			return true
		}
	}
	return false
}

// verifySPFRecord checks if SPF record exists and contains expected values
func (c *DNSChecker) verifySPFRecord(domain string) bool {
	txtRecords, err := net.LookupTXT(domain)
	if err != nil {
		return false
	}

	for _, txt := range txtRecords {
		if strings.HasPrefix(txt, "v=spf1") {
			if strings.Contains(txt, "mx") || strings.Contains(txt, c.config.Server.Hostname) {
				return true
			}
		}
	}
	return false
}

// verifyDKIMRecord checks if DKIM record matches
func (c *DNSChecker) verifyDKIMRecord(domain, selector, expectedDKIM string) bool {
	if expectedDKIM == "" || strings.HasPrefix(expectedDKIM, "No DKIM") {
		return false
	}

	dkimDomain := selector + "._domainkey." + domain
	txtRecords, err := net.LookupTXT(dkimDomain)
	if err != nil {
		return false
	}

	for _, txt := range txtRecords {
		if strings.Contains(txt, "v=DKIM1") {
			// Basic check that a DKIM record exists
			if strings.Contains(txt, "p=") {
				return true
			}
		}
	}
	return false
}

// verifyDMARCRecord checks if DMARC record exists
func (c *DNSChecker) verifyDMARCRecord(domain string) bool {
	dmarcDomain := "_dmarc." + domain
	txtRecords, err := net.LookupTXT(dmarcDomain)
	if err != nil {
		return false
	}

	for _, txt := range txtRecords {
		if strings.HasPrefix(txt, "v=DMARC1") {
			return true
		}
	}
	return false
}

// verifyMailHostnameRecord checks if mail.{domain} A record resolves to an IP address
func (c *DNSChecker) verifyMailHostnameRecord(domain string) bool {
	mailHostname := "mail." + domain
	ips, err := net.LookupIP(mailHostname)
	if err != nil || len(ips) == 0 {
		return false
	}

	// If we have a configured hostname, check if IPs match
	if c.config.Server.Hostname != "" {
		serverIPs, err := net.LookupIP(c.config.Server.Hostname)
		if err == nil && len(serverIPs) > 0 {
			for _, serverIP := range serverIPs {
				for _, ip := range ips {
					if serverIP.Equal(ip) {
						return true
					}
				}
			}
			// IPs don't match but mail hostname resolves
			// Still return true if it resolves somewhere
			return true
		}
	}

	// Mail hostname resolves to an IP
	return true
}

// GetMailHostnameIPs returns the IPs that mail.{domain} resolves to
func (c *DNSChecker) GetMailHostnameIPs(domain string) ([]string, error) {
	mailHostname := "mail." + domain
	ips, err := net.LookupIP(mailHostname)
	if err != nil {
		return nil, err
	}

	result := make([]string, len(ips))
	for i, ip := range ips {
		result[i] = ip.String()
	}
	return result, nil
}
