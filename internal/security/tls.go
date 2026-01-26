package security

import (
	"crypto/tls"
	"fmt"

	"github.com/fenilsonani/email-server/internal/config"
	"golang.org/x/crypto/acme/autocert"
)

// TLSManager handles TLS certificate management
type TLSManager struct {
	config      *config.Config
	certManager *autocert.Manager
	tlsConfig   *tls.Config
}

// NewTLSManager creates a new TLS manager
func NewTLSManager(cfg *config.Config) (*TLSManager, error) {
	manager := &TLSManager{config: cfg}

	if cfg.TLS.AutoTLS {
		// Use Let's Encrypt with autocert
		manager.certManager = &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.Server.Hostname),
			Cache:      autocert.DirCache(cfg.TLS.CacheDir),
			Email:      cfg.TLS.Email,
		}

		manager.tlsConfig = manager.certManager.TLSConfig()
	} else if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
		// Use provided certificates
		cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
		}

		manager.tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
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
