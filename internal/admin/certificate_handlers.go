package admin

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math"
	"net/http"
	"os"
	"time"
)

// CertificateStatus represents the status of a single certificate
type CertificateStatus struct {
	Domain      string    `json:"domain"`
	Issuer      string    `json:"issuer"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	DaysUntilExpiry float64 `json:"days_until_expiry"`
	IsExpired   bool      `json:"is_expired"`
	Status      string    `json:"status"` // "valid", "expiring_soon", "expired"
}

// CertificatesResponse is the response for certificate status endpoints
type CertificatesResponse struct {
	Success      bool                   `json:"success"`
	Message      string                 `json:"message,omitempty"`
	Certificates []CertificateStatus    `json:"certificates,omitempty"`
}

// handleCertificatesStatus returns the status of all configured certificates
func (s *Server) handleCertificatesStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := CertificatesResponse{
		Success:      true,
		Certificates: []CertificateStatus{},
	}

	// Check server hostname certificate
	if s.config.TLS.CertFile != "" && s.config.TLS.KeyFile != "" {
		status := getCertificateStatus(s.config.TLS.CertFile, s.config.Server.Hostname)
		response.Certificates = append(response.Certificates, status)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCertificatesExpiry returns days until expiry for all certificates
func (s *Server) handleCertificatesExpiry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type ExpiryInfo struct {
		Domain              string  `json:"domain"`
		DaysUntilExpiry     float64 `json:"days_until_expiry"`
		RenewalRecommended  bool    `json:"renewal_recommended"`
	}

	var expiryData []ExpiryInfo

	if s.config.TLS.CertFile != "" && s.config.TLS.KeyFile != "" {
		cert, err := loadCertificate(s.config.TLS.CertFile)
		if err == nil {
			daysUntil := time.Until(cert.NotAfter).Hours() / 24

			expiryData = append(expiryData, ExpiryInfo{
				Domain:             cert.Subject.CommonName,
				DaysUntilExpiry:    math.Round(daysUntil*100) / 100,
				RenewalRecommended: daysUntil < 30, // Recommend renewal within 30 days
			})

			// Check SANs
			for _, san := range cert.DNSNames {
				expiryData = append(expiryData, ExpiryInfo{
					Domain:             san,
					DaysUntilExpiry:    math.Round(daysUntil*100) / 100,
					RenewalRecommended: daysUntil < 30,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data":    expiryData,
	})
}

// handleCertificatesReload triggers a manual certificate reload
func (s *Server) handleCertificatesReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Note: tlsManager is not currently passed to the admin server
	// This endpoint will be fully implemented when TLSManager is made available
	response := map[string]interface{}{
		"success": false,
		"message": "Certificate reload requires TLS manager integration",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(response)
}

// handleCertificatesRenew triggers a certificate renewal
func (s *Server) handleCertificatesRenew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// This would trigger certbot or the renewal service
	// Implementation depends on the deployment mode (AutoTLS vs manual)
	response := map[string]interface{}{
		"success": false,
		"message": "Certificate renewal requires certbot integration",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(response)
}

// Helper functions

// getCertificateStatus reads a certificate file and returns its status
func getCertificateStatus(certPath, domain string) CertificateStatus {
	status := CertificateStatus{
		Domain: domain,
		Status: "unknown",
	}

	cert, err := loadCertificate(certPath)
	if err != nil {
		status.Status = "error"
		return status
	}

	status.Issuer = cert.Issuer.String()
	status.NotBefore = cert.NotBefore
	status.NotAfter = cert.NotAfter

	daysUntil := time.Until(cert.NotAfter).Hours() / 24
	status.DaysUntilExpiry = math.Round(daysUntil*100) / 100
	status.IsExpired = time.Now().After(cert.NotAfter)

	if status.IsExpired {
		status.Status = "expired"
	} else if daysUntil < 30 {
		status.Status = "expiring_soon"
	} else {
		status.Status = "valid"
	}

	return status
}

// loadCertificate reads and parses a certificate file
func loadCertificate(certPath string) (*x509.Certificate, error) {
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate: %w", err)
	}

	// Parse PEM block
	block, _ := pem.Decode(certData)
	if block == nil {
		return nil, fmt.Errorf("failed to parse PEM certificate")
	}

	// Parse X.509 certificate
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse x509 certificate: %w", err)
	}

	return cert, nil
}

// RegisterCertificateRoutes registers certificate management API routes
func (s *Server) RegisterCertificateRoutes() {
	// Note: These routes need to be registered in the mux during server initialization
	// This is a helper function to organize the handler registration
}
