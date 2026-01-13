package delivery

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

func TestBuildTLSAName(t *testing.T) {
	tests := []struct {
		host     string
		port     int
		proto    string
		expected string
	}{
		{"mail.example.com", 25, "tcp", "_25._tcp.mail.example.com."},
		{"mx.example.org", 587, "tcp", "_587._tcp.mx.example.org."},
		{"mail.example.com.", 25, "tcp", "_25._tcp.mail.example.com."},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := buildTLSAName(tt.host, tt.port, tt.proto)
			if got != tt.expected {
				t.Errorf("buildTLSAName(%q, %d, %q) = %q, want %q",
					tt.host, tt.port, tt.proto, got, tt.expected)
			}
		})
	}
}

func TestTLSAUsage_String(t *testing.T) {
	tests := []struct {
		usage    TLSAUsage
		expected string
	}{
		{TLSAUsagePKIXTA, "PKIX-TA"},
		{TLSAUsagePKIXEE, "PKIX-EE"},
		{TLSAUsageDANETA, "DANE-TA"},
		{TLSAUsageDANEEE, "DANE-EE"},
		{TLSAUsage(99), "99"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.usage.String(); got != tt.expected {
				t.Errorf("TLSAUsage(%d).String() = %q, want %q", tt.usage, got, tt.expected)
			}
		})
	}
}

func TestMatchData(t *testing.T) {
	testData := []byte("test certificate data")
	sha256Hash := sha256.Sum256(testData)
	sha512Hash := sha512.Sum512(testData)

	tests := []struct {
		name         string
		data         []byte
		expected     []byte
		matchingType TLSAMatchingType
		wantMatch    bool
	}{
		{
			name:         "full match - exact",
			data:         testData,
			expected:     testData,
			matchingType: TLSAMatchingFull,
			wantMatch:    true,
		},
		{
			name:         "full match - mismatch",
			data:         testData,
			expected:     []byte("different data"),
			matchingType: TLSAMatchingFull,
			wantMatch:    false,
		},
		{
			name:         "sha256 match - exact",
			data:         testData,
			expected:     sha256Hash[:],
			matchingType: TLSAMatchingSHA256,
			wantMatch:    true,
		},
		{
			name:         "sha256 match - mismatch",
			data:         testData,
			expected:     sha512Hash[:32], // Wrong hash
			matchingType: TLSAMatchingSHA256,
			wantMatch:    false,
		},
		{
			name:         "sha512 match - exact",
			data:         testData,
			expected:     sha512Hash[:],
			matchingType: TLSAMatchingSHA512,
			wantMatch:    true,
		},
		{
			name:         "sha512 match - mismatch",
			data:         testData,
			expected:     sha256Hash[:],
			matchingType: TLSAMatchingSHA512,
			wantMatch:    false,
		},
		{
			name:         "invalid matching type",
			data:         testData,
			expected:     testData,
			matchingType: TLSAMatchingType(99),
			wantMatch:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchData(tt.data, tt.expected, tt.matchingType)
			if got != tt.wantMatch {
				t.Errorf("matchData() = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

// generateTestCert creates a self-signed certificate for testing
func generateTestCert(t *testing.T) *x509.Certificate {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test.example.com",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	return cert
}

func TestValidateCertificate_DANEEE(t *testing.T) {
	cert := generateTestCert(t)

	// Create TLSA record with SHA-256 hash of full certificate
	certHash := sha256.Sum256(cert.Raw)
	tlsaRecord := TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorCert,
		MatchingType: TLSAMatchingSHA256,
		CertData:     certHash[:],
	}

	t.Run("valid DANE-EE with cert selector", func(t *testing.T) {
		result := ValidateCertificate(cert, nil, []TLSARecord{tlsaRecord})
		if !result.Valid {
			t.Errorf("expected valid result, got invalid: %v", result.Error)
		}
		if result.UsedRecord == nil {
			t.Error("expected UsedRecord to be set")
		}
	})

	t.Run("valid DANE-EE with SPKI selector", func(t *testing.T) {
		spkiHash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		spkiRecord := TLSARecord{
			Usage:        TLSAUsageDANEEE,
			Selector:     TLSASelectorSPKI,
			MatchingType: TLSAMatchingSHA256,
			CertData:     spkiHash[:],
		}
		result := ValidateCertificate(cert, nil, []TLSARecord{spkiRecord})
		if !result.Valid {
			t.Errorf("expected valid result, got invalid: %v", result.Error)
		}
	})

	t.Run("invalid DANE-EE - wrong hash", func(t *testing.T) {
		badRecord := TLSARecord{
			Usage:        TLSAUsageDANEEE,
			Selector:     TLSASelectorCert,
			MatchingType: TLSAMatchingSHA256,
			CertData:     make([]byte, 32), // Wrong hash
		}
		result := ValidateCertificate(cert, nil, []TLSARecord{badRecord})
		if result.Valid {
			t.Error("expected invalid result for wrong hash")
		}
	})

	t.Run("no TLSA records - passes by default", func(t *testing.T) {
		result := ValidateCertificate(cert, nil, nil)
		if !result.Valid {
			t.Error("expected valid result when no TLSA records")
		}
	})

	t.Run("multiple records - one matches", func(t *testing.T) {
		badRecord := TLSARecord{
			Usage:        TLSAUsageDANEEE,
			Selector:     TLSASelectorCert,
			MatchingType: TLSAMatchingSHA256,
			CertData:     make([]byte, 32),
		}
		result := ValidateCertificate(cert, nil, []TLSARecord{badRecord, tlsaRecord})
		if !result.Valid {
			t.Errorf("expected valid result when one record matches: %v", result.Error)
		}
	})
}

func TestValidateCertificate_DANETA(t *testing.T) {
	// Generate a CA cert and an end-entity cert
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate CA key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "Test CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create CA cert: %v", err)
	}
	caCert, _ := x509.ParseCertificate(caCertDER)

	// Create end-entity cert signed by CA
	eeKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	eeTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: "mail.example.com",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	eeCertDER, err := x509.CreateCertificate(rand.Reader, eeTemplate, caTemplate, &eeKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("failed to create EE cert: %v", err)
	}
	eeCert, _ := x509.ParseCertificate(eeCertDER)

	// Create TLSA record for the CA
	caHash := sha256.Sum256(caCert.Raw)
	taRecord := TLSARecord{
		Usage:        TLSAUsageDANETA,
		Selector:     TLSASelectorCert,
		MatchingType: TLSAMatchingSHA256,
		CertData:     caHash[:],
	}

	t.Run("valid DANE-TA with CA in chain", func(t *testing.T) {
		chain := []*x509.Certificate{caCert}
		result := ValidateCertificate(eeCert, chain, []TLSARecord{taRecord})
		if !result.Valid {
			t.Errorf("expected valid result, got invalid: %v", result.Error)
		}
	})

	t.Run("invalid DANE-TA - CA not in chain", func(t *testing.T) {
		result := ValidateCertificate(eeCert, nil, []TLSARecord{taRecord})
		if result.Valid {
			t.Error("expected invalid result when CA not in chain")
		}
	})
}

func TestDANEResolver_NewAndDefaults(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		cfg := DefaultDANEResolverConfig()
		if cfg.CacheTTL != 5*time.Minute {
			t.Errorf("CacheTTL = %v, want 5m", cfg.CacheTTL)
		}
		if cfg.Timeout != 5*time.Second {
			t.Errorf("Timeout = %v, want 5s", cfg.Timeout)
		}
	})

	t.Run("new resolver with defaults", func(t *testing.T) {
		resolver := NewDANEResolver(DefaultDANEResolverConfig())
		if resolver.dnsClient == nil {
			t.Error("dnsClient should not be nil")
		}
		if resolver.dnsServer == "" {
			t.Error("dnsServer should not be empty")
		}
	})

	t.Run("custom DNS server", func(t *testing.T) {
		cfg := DANEResolverConfig{
			DNSServer: "1.1.1.1",
		}
		resolver := NewDANEResolver(cfg)
		if resolver.dnsServer != "1.1.1.1:53" {
			t.Errorf("dnsServer = %q, want %q", resolver.dnsServer, "1.1.1.1:53")
		}
	})

	t.Run("custom DNS server with port", func(t *testing.T) {
		cfg := DANEResolverConfig{
			DNSServer: "1.1.1.1:5353",
		}
		resolver := NewDANEResolver(cfg)
		if resolver.dnsServer != "1.1.1.1:5353" {
			t.Errorf("dnsServer = %q, want %q", resolver.dnsServer, "1.1.1.1:5353")
		}
	})
}

func TestDANEResolver_Cache(t *testing.T) {
	resolver := NewDANEResolver(DefaultDANEResolverConfig())

	// Add entry to cache
	cached := &cachedTLSA{
		records: []TLSARecord{
			{Usage: TLSAUsageDANEEE, Selector: TLSASelectorCert, MatchingType: TLSAMatchingSHA256},
		},
		secure:    true,
		expiresAt: time.Now().Add(time.Hour),
	}
	resolver.cache.Store("_25._tcp.mail.example.com.", cached)

	// Check stats
	stats := resolver.CacheStats()
	if stats.TotalEntries != 1 {
		t.Errorf("TotalEntries = %d, want 1", stats.TotalEntries)
	}
	if stats.ValidEntries != 1 {
		t.Errorf("ValidEntries = %d, want 1", stats.ValidEntries)
	}
	if stats.SecureEntries != 1 {
		t.Errorf("SecureEntries = %d, want 1", stats.SecureEntries)
	}

	// Clear cache
	resolver.ClearCache()
	stats = resolver.CacheStats()
	if stats.TotalEntries != 0 {
		t.Errorf("TotalEntries after clear = %d, want 0", stats.TotalEntries)
	}
}

func TestDANEResolver_CacheExpiration(t *testing.T) {
	resolver := NewDANEResolver(DefaultDANEResolverConfig())

	// Add expired entry to cache
	cached := &cachedTLSA{
		records:   []TLSARecord{},
		secure:    false,
		expiresAt: time.Now().Add(-time.Hour), // Expired
	}
	resolver.cache.Store("_25._tcp.expired.example.com.", cached)

	stats := resolver.CacheStats()
	if stats.ExpiredEntries != 1 {
		t.Errorf("ExpiredEntries = %d, want 1", stats.ExpiredEntries)
	}
	if stats.ValidEntries != 0 {
		t.Errorf("ValidEntries = %d, want 0", stats.ValidEntries)
	}
}

func TestValidateCertificate_SHA512(t *testing.T) {
	cert := generateTestCert(t)

	certHash := sha512.Sum512(cert.Raw)
	tlsaRecord := TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorCert,
		MatchingType: TLSAMatchingSHA512,
		CertData:     certHash[:],
	}

	result := ValidateCertificate(cert, nil, []TLSARecord{tlsaRecord})
	if !result.Valid {
		t.Errorf("expected valid result with SHA-512, got invalid: %v", result.Error)
	}
}

func TestValidateCertificate_FullCert(t *testing.T) {
	cert := generateTestCert(t)

	tlsaRecord := TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorCert,
		MatchingType: TLSAMatchingFull,
		CertData:     cert.Raw,
	}

	result := ValidateCertificate(cert, nil, []TLSARecord{tlsaRecord})
	if !result.Valid {
		t.Errorf("expected valid result with full cert, got invalid: %v", result.Error)
	}
}
