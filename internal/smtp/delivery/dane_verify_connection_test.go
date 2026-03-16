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
