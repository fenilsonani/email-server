package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/fenilsonani/email-server/internal/config"
	"golang.org/x/crypto/acme/autocert"
)

// TLSManager handles TLS certificate management with hot-reloading support
type TLSManager struct {
	config      *config.Config
	certManager *autocert.Manager
	tlsConfig   *tls.Config

	// Hot reload support
	mu               sync.RWMutex
	certificates     atomic.Value // *[]*tls.Certificate
	certsByName      map[string]*tls.Certificate
	lastLoadedTime   int64
	loadError        error
	isManualCertMode bool
}

// NewTLSManager creates a new TLS manager
func NewTLSManager(cfg *config.Config) (*TLSManager, error) {
	manager := &TLSManager{
		config:       cfg,
		certsByName:  make(map[string]*tls.Certificate),
		isManualCertMode: false,
	}

	if cfg.TLS.AutoTLS {
		// Use Let's Encrypt with autocert
		// Build list of all domains to whitelist
		domains := []string{cfg.Server.Hostname}
		for _, d := range cfg.Domains {
			domains = append(domains, d.Name)
		}
		if len(cfg.TLS.Domains) > 0 {
			domains = append(domains, cfg.TLS.Domains...)
		}

		manager.certManager = &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(domains...),
			Cache:      autocert.DirCache(cfg.TLS.CacheDir),
			Email:      cfg.TLS.Email,
		}

		// Create base TLS config using autocert manager
		manager.tlsConfig = manager.certManager.TLSConfig()

		// Override GetCertificate to support hot reload
		manager.tlsConfig.GetCertificate = manager.GetCertificate
	} else if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
		// Use provided certificates with hot-reload capability
		manager.isManualCertMode = true

		// Load initial certificates
		if err := manager.ReloadCertificates(); err != nil {
			return nil, fmt.Errorf("failed to load initial TLS certificate: %w", err)
		}

		manager.tlsConfig = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			GetCertificate: manager.GetCertificate,
		}
	}

	// Set secure defaults if TLS is configured
	if manager.tlsConfig != nil {
		// Use TLS 1.2 as minimum for email client compatibility (Apple Mail, Outlook, etc.)
		// TLS 1.3 is preferred but many email clients still require TLS 1.2 support
		// The cipher suites below ensure TLS 1.2 connections use only secure algorithms
		manager.tlsConfig.MinVersion = tls.VersionTLS12

		// Secure cipher suites for TLS 1.2 connections
		// TLS 1.3 uses its own fixed cipher suites (these are ignored for 1.3)
		// All suites use ECDHE for forward secrecy and AEAD ciphers (GCM/ChaCha20)
		manager.tlsConfig.CipherSuites = []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		}
	}

	return manager, nil
}

// TLSConfig returns the TLS configuration
func (m *TLSManager) TLSConfig() *tls.Config {
	return m.tlsConfig
}

// TLSConfigForProtocol returns a TLS configuration for the specified protocol.
// Note: ALPN is not used for IMAP/SMTP/POP3 as these protocols don't support it.
// ALPN is only used for HTTP-based protocols (h2, http/1.1).
func (m *TLSManager) TLSConfigForProtocol(protocol string) *tls.Config {
	if m.tlsConfig == nil {
		return nil
	}

	// Clone the base config
	cfg := m.tlsConfig.Clone()

	// Don't set ALPN for mail protocols (imap, smtp, pop3) as they don't use it.
	// ALPN is only for HTTP-based protocols. Mail protocols use plain TLS negotiation.
	cfg.NextProtos = []string{}

	return cfg
}

// CertManager returns the autocert manager for HTTP-01 challenges
func (m *TLSManager) CertManager() *autocert.Manager {
	return m.certManager
}

// HasTLS returns true if TLS is configured
func (m *TLSManager) HasTLS() bool {
	return m.tlsConfig != nil
}

// ReloadCertificates reloads TLS certificates from disk
// This allows hot-reloading certificates without restarting the service
func (m *TLSManager) ReloadCertificates() error {
	// Only works for manual certificate mode
	if !m.isManualCertMode {
		if m.certManager != nil {
			// AutoTLS handles renewals automatically, but we can trigger cleanup/refresh
			return nil
		}
		return fmt.Errorf("certificate reload only supported in manual certificate mode")
	}

	// Load new certificate from disk
	cert, err := tls.LoadX509KeyPair(m.config.TLS.CertFile, m.config.TLS.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to load TLS certificate from disk: %w", err)
	}

	// Parse certificate to get domain information
	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Update the certificate in thread-safe manner
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store certificate by domain names
	if x509Cert.Subject.CommonName != "" {
		m.certsByName[x509Cert.Subject.CommonName] = &cert
	}
	for _, name := range x509Cert.DNSNames {
		m.certsByName[name] = &cert
	}

	m.lastLoadedTime = int64(x509Cert.NotAfter.Unix())
	m.loadError = nil

	return nil
}

// GetCertificate returns a certificate for the given client hello
// This is called for every TLS connection and supports domain-based cert selection
func (m *TLSManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	// For AutoTLS, delegate to autocert manager
	if m.certManager != nil {
		return m.certManager.GetCertificate(hello)
	}

	// For manual mode, look up certificate by domain
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Try to find certificate for the requested domain
	if hello.ServerName != "" {
		if cert, ok := m.certsByName[hello.ServerName]; ok {
			return cert, nil
		}
	}

	// Fallback to first available certificate
	for _, cert := range m.certsByName {
		return cert, nil
	}

	// If no certificate found, return error
	return nil, fmt.Errorf("no certificate available")
}

// GetCertificateExpiry returns the expiration timestamp of the current certificate
// Returns 0 if no certificate is loaded or in AutoTLS mode
func (m *TLSManager) GetCertificateExpiry() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastLoadedTime
}

// GetLoadError returns any error that occurred during certificate loading
func (m *TLSManager) GetLoadError() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.loadError
}
