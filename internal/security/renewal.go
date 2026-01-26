package security

import (
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
)

// RenewalService monitors and manages certificate renewals
type RenewalService struct {
	checkInterval     time.Duration
	renewalDaysBefore int
	renewalHookScript string
	mu                sync.RWMutex
	lastCheckTime     time.Time
	lastCheckError    error
	checkCount        int64
}

// NewRenewalService creates a new certificate renewal service
func NewRenewalService(checkInterval time.Duration, renewalDaysBefore int, hookScript string) *RenewalService {
	return &RenewalService{
		checkInterval:     checkInterval,
		renewalDaysBefore: renewalDaysBefore,
		renewalHookScript: hookScript,
	}
}

// Run starts the renewal monitoring service
// It runs in a loop checking for certificate expiry at regular intervals
// The context is used to signal when to stop the service
func (r *RenewalService) Run(ctx context.Context, tlsManager *TLSManager) {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkAndRenew(ctx, tlsManager)
		}
	}
}

// checkAndRenew checks certificate expiry and triggers renewal if needed
func (r *RenewalService) checkAndRenew(ctx context.Context, tlsManager *TLSManager) {
	r.mu.Lock()
	r.lastCheckTime = time.Now()
	r.checkCount++
	r.mu.Unlock()

	// For AutoTLS, renewal is automatic via autocert manager
	// For manual mode, we check file expiry and execute renewal hooks
	if !tlsManager.isManualCertMode {
		return // AutoTLS handles renewal automatically
	}

	// Check certificate expiry in manual mode
	expiry := tlsManager.GetCertificateExpiry()
	if expiry == 0 {
		return // No certificate loaded yet
	}

	expiryTime := time.Unix(expiry, 0)
	daysUntilExpiry := time.Until(expiryTime).Hours() / 24

	// Check if renewal is needed
	if daysUntilExpiry > float64(r.renewalDaysBefore) {
		return // Not yet time to renew
	}

	// Time to renew - execute renewal hook
	if r.renewalHookScript != "" {
		r.executeRenewalHook(ctx)
	}
}

// executeRenewalHook runs the renewal hook script
func (r *RenewalService) executeRenewalHook(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Execute the hook script with timeout
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.renewalHookScript)

	// Capture output for logging
	output, err := cmd.CombinedOutput()
	if err != nil {
		r.lastCheckError = fmt.Errorf("renewal hook failed: %w (output: %s)", err, string(output))
	} else {
		r.lastCheckError = nil
	}
}

// GetLastCheckTime returns when the last renewal check occurred
func (r *RenewalService) GetLastCheckTime() time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastCheckTime
}

// GetLastError returns the last error from renewal check
func (r *RenewalService) GetLastError() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastCheckError
}

// GetCheckCount returns the number of checks performed
func (r *RenewalService) GetCheckCount() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.checkCount
}

// CheckCertificateExpiry checks the expiry of a certificate file
// Returns days until expiry, or 0 if file cannot be read
func CheckCertificateExpiry(certPath string) (float64, error) {
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read certificate file: %w", err)
	}

	// Parse PEM certificate
	cert, err := x509.ParseCertificate(certData)
	if err != nil {
		return 0, fmt.Errorf("failed to parse certificate: %w", err)
	}

	daysUntilExpiry := time.Until(cert.NotAfter).Hours() / 24
	return daysUntilExpiry, nil
}

// GetMultipleCertificateExpiry checks expiry for multiple certificate files
// Returns a map of cert path to days until expiry
func GetMultipleCertificateExpiry(certPaths []string) map[string]float64 {
	result := make(map[string]float64)
	for _, path := range certPaths {
		if days, err := CheckCertificateExpiry(path); err == nil {
			result[path] = days
		} else {
			result[path] = -1 // Indicate error
		}
	}
	return result
}
