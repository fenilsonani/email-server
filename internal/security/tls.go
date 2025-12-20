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
		manager.tlsConfig.MinVersion = tls.VersionTLS12
		manager.tlsConfig.PreferServerCipherSuites = true
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

// CertManager returns the autocert manager for HTTP-01 challenges
func (m *TLSManager) CertManager() *autocert.Manager {
	return m.certManager
}

// HasTLS returns true if TLS is configured
func (m *TLSManager) HasTLS() bool {
	return m.tlsConfig != nil
}
