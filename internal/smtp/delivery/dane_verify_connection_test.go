package delivery

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"strings"
	"testing"

	"github.com/fenilsonani/email-server/internal/logging"
)

func TestDANEVerifyConnection(t *testing.T) {
	engine := &Engine{
		logger: logging.Default(),
	}

	cert := generateTestCert(t)
	certHash := sha256.Sum256(cert.Raw)
	tlsaRecord := TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorCert,
		MatchingType: TLSAMatchingSHA256,
		CertData:     certHash[:],
	}

	verify := engine.daneVerifyConnection(context.Background(), "mx.example.com", []TLSARecord{tlsaRecord})

	if err := verify(tls.ConnectionState{}); err == nil {
		t.Fatal("expected missing certificate state to fail")
	}

	if err := verify(tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}); err != nil {
		t.Fatalf("expected valid certificate to pass, got %v", err)
	}

	badRecord := TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorCert,
		MatchingType: TLSAMatchingSHA256,
		CertData:     []byte("bad hash"),
	}
	badVerify := engine.daneVerifyConnection(context.Background(), "mx.example.com", []TLSARecord{badRecord})
	if err := badVerify(tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}); err == nil || !strings.Contains(err.Error(), "DANE validation failed") {
		t.Fatalf("expected DANE validation failure, got %v", err)
	}
}

func TestShouldUseDANERequiresDNSSEC(t *testing.T) {
	record := TLSARecord{
		Usage:        TLSAUsageDANEEE,
		Selector:     TLSASelectorCert,
		MatchingType: TLSAMatchingSHA256,
		CertData:     []byte("hash"),
	}

	if shouldUseDANE([]TLSARecord{record}, false) {
		t.Fatal("shouldUseDANE() should reject TLSA records without DNSSEC validation")
	}
	if !shouldUseDANE([]TLSARecord{record}, true) {
		t.Fatal("shouldUseDANE() should accept DNSSEC-validated TLSA records")
	}
	if shouldUseDANE(nil, true) {
		t.Fatal("shouldUseDANE() should reject missing TLSA records")
	}
}

func TestTLSRequirementReason(t *testing.T) {
	enforcePolicy := &STSPolicy{Mode: STSModeEnforce}

	tests := []struct {
		name       string
		stsPolicy  *STSPolicy
		useDANE    bool
		requireTLS bool
		want       string
	}{
		{name: "none", want: ""},
		{name: "require tls", requireTLS: true, want: "STARTTLS required"},
		{name: "mta-sts", stsPolicy: enforcePolicy, want: "MTA-STS enforces TLS"},
		{name: "dane wins", stsPolicy: enforcePolicy, useDANE: true, requireTLS: true, want: "DANE requires TLS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tlsRequirementReason(tt.stsPolicy, tt.useDANE, tt.requireTLS); got != tt.want {
				t.Fatalf("tlsRequirementReason() = %q, want %q", got, tt.want)
			}
		})
	}
}
