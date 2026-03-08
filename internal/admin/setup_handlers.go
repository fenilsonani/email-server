package admin

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/fenilsonani/email-server/internal/auth"
)

// handleAPISetupStatus returns whether setup is needed.
func (s *Server) handleAPISetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Check if any domains exist (if so, setup is complete)
	var domainCount int
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM domains").Scan(&domainCount)

	var userCount int
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users").Scan(&userCount)

	needsSetup := domainCount == 0 && userCount == 0

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"needs_setup":  needsSetup,
		"domain_count": domainCount,
		"user_count":   userCount,
	})
}

// handleAPISetupCheckDNS validates DNS records for a domain.
func (s *Server) handleAPISetupCheckDNS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Domain       string `json:"domain"`
		MailHostname string `json:"mail_hostname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Domain == "" {
		s.jsonError(w, http.StatusBadRequest, "Domain is required")
		return
	}
	if req.MailHostname == "" {
		req.MailHostname = "mail." + req.Domain
	}

	results := map[string]interface{}{}

	// Check MX records
	mxRecords, err := net.LookupMX(req.Domain)
	if err != nil || len(mxRecords) == 0 {
		results["mx"] = map[string]interface{}{
			"status":   "missing",
			"expected": req.MailHostname,
			"found":    nil,
		}
	} else {
		mxHosts := make([]string, len(mxRecords))
		for i, mx := range mxRecords {
			mxHosts[i] = strings.TrimSuffix(mx.Host, ".")
		}
		found := false
		for _, h := range mxHosts {
			if h == req.MailHostname {
				found = true
				break
			}
		}
		status := "ok"
		if !found {
			status = "mismatch"
		}
		results["mx"] = map[string]interface{}{
			"status":   status,
			"expected": req.MailHostname,
			"found":    mxHosts,
		}
	}

	// Check A record for mail hostname
	ips, err := net.LookupHost(req.MailHostname)
	if err != nil || len(ips) == 0 {
		results["a_record"] = map[string]interface{}{
			"status": "missing",
			"host":   req.MailHostname,
		}
	} else {
		results["a_record"] = map[string]interface{}{
			"status": "ok",
			"host":   req.MailHostname,
			"ips":    ips,
		}
	}

	// Check SPF (TXT record)
	txtRecords, _ := net.LookupTXT(req.Domain)
	spfFound := false
	for _, txt := range txtRecords {
		if strings.HasPrefix(txt, "v=spf1") {
			spfFound = true
			results["spf"] = map[string]interface{}{
				"status": "ok",
				"record": txt,
			}
			break
		}
	}
	if !spfFound {
		results["spf"] = map[string]interface{}{
			"status":   "missing",
			"expected": "v=spf1 mx -all",
		}
	}

	// Check DMARC
	dmarcRecords, _ := net.LookupTXT("_dmarc." + req.Domain)
	dmarcFound := false
	for _, txt := range dmarcRecords {
		if strings.HasPrefix(txt, "v=DMARC1") {
			dmarcFound = true
			results["dmarc"] = map[string]interface{}{
				"status": "ok",
				"record": txt,
			}
			break
		}
	}
	if !dmarcFound {
		results["dmarc"] = map[string]interface{}{
			"status":   "missing",
			"expected": "v=DMARC1; p=quarantine; rua=mailto:postmaster@" + req.Domain,
		}
	}

	s.jsonResponse(w, http.StatusOK, results)
}

// handleAPISetupPreflight runs system checks.
func (s *Server) handleAPISetupPreflight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	checks := map[string]interface{}{}

	// Check port 25 (SMTP)
	if ln, err := net.Listen("tcp", ":25"); err == nil {
		ln.Close()
		checks["port_25"] = map[string]interface{}{"status": "available", "service": "SMTP"}
	} else {
		checks["port_25"] = map[string]interface{}{"status": "in_use", "service": "SMTP", "error": err.Error()}
	}

	// Check port 587 (Submission)
	if ln, err := net.Listen("tcp", ":587"); err == nil {
		ln.Close()
		checks["port_587"] = map[string]interface{}{"status": "available", "service": "SMTP Submission"}
	} else {
		checks["port_587"] = map[string]interface{}{"status": "in_use", "service": "SMTP Submission", "error": err.Error()}
	}

	// Check port 993 (IMAPS)
	if ln, err := net.Listen("tcp", ":993"); err == nil {
		ln.Close()
		checks["port_993"] = map[string]interface{}{"status": "available", "service": "IMAPS"}
	} else {
		checks["port_993"] = map[string]interface{}{"status": "in_use", "service": "IMAPS", "error": err.Error()}
	}

	// Check Redis connectivity
	if s.queue != nil {
		checks["redis"] = map[string]interface{}{"status": "connected"}
	} else {
		checks["redis"] = map[string]interface{}{"status": "not_connected"}
	}

	// Database check
	if err := s.db.Ping(); err != nil {
		checks["database"] = map[string]interface{}{"status": "error", "error": err.Error()}
	} else {
		checks["database"] = map[string]interface{}{"status": "connected"}
	}

	s.jsonResponse(w, http.StatusOK, checks)
}

// handleAPISetupInstall executes the initial setup.
func (s *Server) handleAPISetupInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Domain       string `json:"domain"`
		MailHostname string `json:"mail_hostname"`
		AdminEmail   string `json:"admin_email"`
		AdminPass    string `json:"admin_password"`
		Preset       string `json:"preset"`
		TLSMode      string `json:"tls_mode"`
		TLSEmail     string `json:"tls_email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Domain == "" || req.AdminEmail == "" || req.AdminPass == "" {
		s.jsonError(w, http.StatusBadRequest, "Domain, admin email, and admin password are required")
		return
	}
	if req.Preset == "" {
		req.Preset = "full"
	}
	if req.MailHostname == "" {
		req.MailHostname = "mail." + req.Domain
	}

	// 1. Create domain
	res, err := s.db.ExecContext(r.Context(), `
		INSERT INTO domains (name, dkim_selector, is_active) VALUES (?, 'mail', 1)
	`, req.Domain)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to create domain: "+err.Error())
		return
	}
	domainID, _ := res.LastInsertId()

	// 2. Create admin user
	parts := strings.SplitN(req.AdminEmail, "@", 2)
	username := parts[0]

	passwordHash, err := auth.HashPassword(req.AdminPass)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	userRes, err := s.db.ExecContext(r.Context(), `
		INSERT INTO users (domain_id, username, password_hash, is_admin, is_active)
		VALUES (?, ?, ?, 1, 1)
	`, domainID, username, passwordHash)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to create admin user: "+err.Error())
		return
	}
	userID, _ := userRes.LastInsertId()

	// 3. Create default org
	if s.orgStore != nil {
		org, err := s.orgStore.Create(r.Context(), req.Domain, userID, req.Preset)
		if err != nil {
			s.logger.Warn("Failed to create default org during setup", "error", err.Error())
		} else {
			// Assign domain and user to org
			s.db.ExecContext(r.Context(), "UPDATE domains SET org_id = ? WHERE id = ?", org.ID, domainID)
			s.db.ExecContext(r.Context(), "UPDATE users SET org_id = ? WHERE id = ?", org.ID, userID)
		}
	}

	// 4. Create default mailboxes for admin
	mailboxes := []struct {
		name       string
		specialUse string
	}{
		{"INBOX", ""},
		{"Sent", "\\Sent"},
		{"Drafts", "\\Drafts"},
		{"Trash", "\\Trash"},
		{"Junk", "\\Junk"},
		{"Archive", "\\Archive"},
	}
	for _, mb := range mailboxes {
		s.db.ExecContext(r.Context(), `
			INSERT OR IGNORE INTO mailboxes (user_id, name, uidvalidity, uidnext, special_use)
			VALUES (?, ?, abs(random() % 2147483647) + 1, 1, ?)
		`, userID, mb.name, mb.specialUse)
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"domain_id":     domainID,
		"user_id":       userID,
		"admin_email":   req.AdminEmail,
		"preset":        req.Preset,
		"mail_hostname": req.MailHostname,
	})
}
