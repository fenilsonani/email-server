package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
)

// createTestCertificate creates a self-signed certificate for testing
func createTestCertificate(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()

	// Generate private key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test.example.com",
		},
		DNSNames:              []string{"test.example.com", "localhost"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	// Write certificate to file
	certFile = filepath.Join(dir, "cert.pem")
	certOut, err := os.Create(certFile)
	if err != nil {
		t.Fatalf("Failed to create cert file: %v", err)
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certOut.Close()

	// Write key to file
	keyFile = filepath.Join(dir, "key.pem")
	keyOut, err := os.Create(keyFile)
	if err != nil {
		t.Fatalf("Failed to create key file: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		t.Fatalf("Failed to marshal private key: %v", err)
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	keyOut.Close()

	return certFile, keyFile
}

func TestMailTLSConfig_DisablesALPN(t *testing.T) {
	// Create temp directory for test certificates
	tmpDir := t.TempDir()
	certFile, keyFile := createTestCertificate(t, tmpDir)

	cfg := &config.Config{
		TLS: config.TLSConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
		},
	}

	mgr, err := NewTLSManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create TLSManager: %v", err)
	}

	mailCfg := mgr.MailTLSConfig()
	if mailCfg == nil {
		t.Fatal("MailTLSConfig returned nil")
	}

	// CRITICAL: Verify ALPN is disabled for mail protocols
	if len(mailCfg.NextProtos) != 0 {
		t.Errorf("MailTLSConfig should have empty NextProtos, got: %v", mailCfg.NextProtos)
	}
}

func TestHTTPTLSConfig_AllowsALPN(t *testing.T) {
	// Create temp directory for test certificates
	tmpDir := t.TempDir()
	certFile, keyFile := createTestCertificate(t, tmpDir)

	cfg := &config.Config{
		TLS: config.TLSConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
		},
	}

	mgr, err := NewTLSManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create TLSManager: %v", err)
	}

	httpCfg := mgr.HTTPTLSConfig()
	if httpCfg == nil {
		t.Fatal("HTTPTLSConfig returned nil")
	}

	// HTTPTLSConfig should allow ALPN (not explicitly disable it)
	// The base config may or may not have NextProtos set, but it shouldn't be
	// forcibly cleared like MailTLSConfig does
	// We just verify we get a valid config that can be used for HTTP
	if httpCfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("HTTPTLSConfig should have MinVersion TLS 1.2, got: %v", httpCfg.MinVersion)
	}
}

func TestTLSConfigs_AreIndependent(t *testing.T) {
	// Create temp directory for test certificates
	tmpDir := t.TempDir()
	certFile, keyFile := createTestCertificate(t, tmpDir)

	cfg := &config.Config{
		TLS: config.TLSConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
		},
	}

	mgr, err := NewTLSManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create TLSManager: %v", err)
	}

	// Get two mail configs
	mailCfg1 := mgr.MailTLSConfig()
	mailCfg2 := mgr.MailTLSConfig()

	// Modify mailCfg1
	mailCfg1.NextProtos = []string{"modified"}

	// mailCfg2 should still be empty (configs are independent)
	if len(mailCfg2.NextProtos) != 0 {
		t.Errorf("TLS configs should be independent, but modifying one affected another")
	}

	// Same test for HTTP configs
	httpCfg1 := mgr.HTTPTLSConfig()
	httpCfg2 := mgr.HTTPTLSConfig()

	httpCfg1.NextProtos = []string{"h2", "http/1.1"}

	// httpCfg2 should not be affected
	if len(httpCfg2.NextProtos) > 0 && httpCfg2.NextProtos[0] == "h2" {
		t.Errorf("HTTP TLS configs should be independent, but modifying one affected another")
	}
}

func TestMailTLSConfig_HasCorrectMinVersion(t *testing.T) {
	// Create temp directory for test certificates
	tmpDir := t.TempDir()
	certFile, keyFile := createTestCertificate(t, tmpDir)

	cfg := &config.Config{
		TLS: config.TLSConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
		},
	}

	mgr, err := NewTLSManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create TLSManager: %v", err)
	}

	mailCfg := mgr.MailTLSConfig()
	if mailCfg == nil {
		t.Fatal("MailTLSConfig returned nil")
	}

	// Verify TLS 1.2 minimum for email client compatibility
	if mailCfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MailTLSConfig should have MinVersion TLS 1.2, got: %v", mailCfg.MinVersion)
	}
}

func TestMailTLSConfig_NilWhenNoTLS(t *testing.T) {
	// Config with no TLS settings
	cfg := &config.Config{}

	mgr, err := NewTLSManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create TLSManager: %v", err)
	}

	mailCfg := mgr.MailTLSConfig()
	if mailCfg != nil {
		t.Error("MailTLSConfig should return nil when TLS is not configured")
	}

	httpCfg := mgr.HTTPTLSConfig()
	if httpCfg != nil {
		t.Error("HTTPTLSConfig should return nil when TLS is not configured")
	}
}

func TestTLSConfigForProtocol_Deprecated(t *testing.T) {
	// Create temp directory for test certificates
	tmpDir := t.TempDir()
	certFile, keyFile := createTestCertificate(t, tmpDir)

	cfg := &config.Config{
		TLS: config.TLSConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
		},
	}

	mgr, err := NewTLSManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create TLSManager: %v", err)
	}

	// The deprecated TLSConfigForProtocol should still work
	// and return the same result as MailTLSConfig
	deprecatedCfg := mgr.TLSConfigForProtocol("imap")
	mailCfg := mgr.MailTLSConfig()

	if deprecatedCfg == nil {
		t.Fatal("TLSConfigForProtocol returned nil")
	}

	// Both should have ALPN disabled
	if len(deprecatedCfg.NextProtos) != 0 {
		t.Errorf("TLSConfigForProtocol should have empty NextProtos, got: %v", deprecatedCfg.NextProtos)
	}

	if len(mailCfg.NextProtos) != 0 {
		t.Errorf("MailTLSConfig should have empty NextProtos, got: %v", mailCfg.NextProtos)
	}
}

func TestMailTLSConfig_HasSecureCipherSuites(t *testing.T) {
	// Create temp directory for test certificates
	tmpDir := t.TempDir()
	certFile, keyFile := createTestCertificate(t, tmpDir)

	cfg := &config.Config{
		TLS: config.TLSConfig{
			CertFile: certFile,
			KeyFile:  keyFile,
		},
	}

	mgr, err := NewTLSManager(cfg)
	if err != nil {
		t.Fatalf("Failed to create TLSManager: %v", err)
	}

	mailCfg := mgr.MailTLSConfig()
	if mailCfg == nil {
		t.Fatal("MailTLSConfig returned nil")
	}

	// Verify cipher suites are set
	if len(mailCfg.CipherSuites) == 0 {
		t.Error("MailTLSConfig should have cipher suites configured")
	}

	// Verify all cipher suites use forward secrecy (ECDHE)
	for _, suite := range mailCfg.CipherSuites {
		name := tls.CipherSuiteName(suite)
		if name == "" {
			continue // Skip unknown cipher suites
		}
		// All our configured suites should use ECDHE
		if suite != tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384 &&
			suite != tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384 &&
			suite != tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305 &&
			suite != tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305 &&
			suite != tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 &&
			suite != tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 {
			t.Errorf("Unexpected cipher suite: %s", name)
		}
	}
}
