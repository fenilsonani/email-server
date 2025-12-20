package security

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/emersion/go-msgauth/dkim"
)

// DKIMSigner handles DKIM signing for outbound messages
type DKIMSigner struct {
	domain     string
	selector   string
	privateKey *rsa.PrivateKey
}

// NewDKIMSigner creates a new DKIM signer for a domain
func NewDKIMSigner(domain, selector, keyPath string) (*DKIMSigner, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read DKIM key: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	var privateKey *rsa.PrivateKey

	// Try PKCS#1 format first
	privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS#8 format
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("key is not an RSA private key")
		}
	}

	return &DKIMSigner{
		domain:     domain,
		selector:   selector,
		privateKey: privateKey,
	}, nil
}

// Sign adds a DKIM signature to an email message
// It reads the message from r and writes the signed message to w
func (s *DKIMSigner) Sign(w io.Writer, r io.Reader) error {
	options := &dkim.SignOptions{
		Domain:   s.domain,
		Selector: s.selector,
		Signer:   s.privateKey,
		Hash:     crypto.SHA256,
		HeaderKeys: []string{
			"From",
			"To",
			"Subject",
			"Date",
			"Message-ID",
			"Content-Type",
			"MIME-Version",
		},
	}

	return dkim.Sign(w, r, options)
}

// DKIMSignerPool manages DKIM signers for multiple domains
type DKIMSignerPool struct {
	signers map[string]*DKIMSigner
}

// NewDKIMSignerPool creates a new pool of DKIM signers
func NewDKIMSignerPool() *DKIMSignerPool {
	return &DKIMSignerPool{
		signers: make(map[string]*DKIMSigner),
	}
}

// AddSigner adds a DKIM signer for a domain
func (p *DKIMSignerPool) AddSigner(domain, selector, keyPath string) error {
	signer, err := NewDKIMSigner(domain, selector, keyPath)
	if err != nil {
		return err
	}
	p.signers[strings.ToLower(domain)] = signer
	return nil
}

// GetSigner returns the DKIM signer for a domain
func (p *DKIMSignerPool) GetSigner(domain string) *DKIMSigner {
	return p.signers[strings.ToLower(domain)]
}

// Sign signs a message using the appropriate domain signer
func (p *DKIMSignerPool) Sign(domain string, w io.Writer, r io.Reader) error {
	signer := p.GetSigner(domain)
	if signer == nil {
		return fmt.Errorf("no DKIM signer for domain: %s", domain)
	}
	return signer.Sign(w, r)
}

// GenerateDKIMKey generates a new RSA key pair for DKIM signing
func GenerateDKIMKey(bits int) (*rsa.PrivateKey, error) {
	if bits < 1024 {
		bits = 2048 // Default to 2048 bits
	}
	return rsa.GenerateKey(rand.Reader, bits)
}

// FormatDKIMPublicKey formats the public key for DNS TXT record
func FormatDKIMPublicKey(key *rsa.PublicKey) (string, error) {
	pubBytes, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return "", err
	}

	// Base64 encode and format for DNS
	block := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	}

	pemData := pem.EncodeToMemory(block)

	// Remove PEM headers and newlines
	pubStr := string(pemData)
	pubStr = strings.ReplaceAll(pubStr, "-----BEGIN PUBLIC KEY-----", "")
	pubStr = strings.ReplaceAll(pubStr, "-----END PUBLIC KEY-----", "")
	pubStr = strings.ReplaceAll(pubStr, "\n", "")

	return fmt.Sprintf("v=DKIM1; k=rsa; p=%s", pubStr), nil
}

// GenerateDNSRecords generates the recommended DNS records for email
type DNSRecords struct {
	DKIM  string // _domainkey TXT record
	SPF   string // @ TXT record for SPF
	DMARC string // _dmarc TXT record
	MX    string // MX record
}

// GenerateDNSRecords creates DNS record templates for a domain
func GenerateDNSRecords(domain, hostname, selector string, dkimPubKey *rsa.PublicKey) (*DNSRecords, error) {
	records := &DNSRecords{}

	// DKIM record
	if dkimPubKey != nil {
		dkimTxt, err := FormatDKIMPublicKey(dkimPubKey)
		if err != nil {
			return nil, err
		}
		records.DKIM = fmt.Sprintf("%s._domainkey.%s TXT \"%s\"", selector, domain, dkimTxt)
	}

	// SPF record
	records.SPF = fmt.Sprintf("@ TXT \"v=spf1 mx a:%s -all\"", hostname)

	// DMARC record
	records.DMARC = fmt.Sprintf("_dmarc.%s TXT \"v=DMARC1; p=quarantine; rua=mailto:postmaster@%s\"", domain, domain)

	// MX record
	records.MX = fmt.Sprintf("@ MX 10 %s", hostname)

	return records, nil
}
