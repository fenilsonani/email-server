package security

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

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
	store   DKIMKeyStore
	mu      sync.RWMutex
}

// NewDKIMSignerPool creates a new pool of DKIM signers
func NewDKIMSignerPool() *DKIMSignerPool {
	return &DKIMSignerPool{
		signers: make(map[string]*DKIMSigner),
	}
}

// NewDKIMSignerPoolWithStore creates a new pool with a key store for dynamic loading
func NewDKIMSignerPoolWithStore(store DKIMKeyStore) *DKIMSignerPool {
	return &DKIMSignerPool{
		signers: make(map[string]*DKIMSigner),
		store:   store,
	}
}

// SetStore sets the key store for dynamic key loading
func (p *DKIMSignerPool) SetStore(store DKIMKeyStore) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.store = store
}

// AddSigner adds a DKIM signer for a domain from a key file
func (p *DKIMSignerPool) AddSigner(domain, selector, keyPath string) error {
	signer, err := NewDKIMSigner(domain, selector, keyPath)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.signers[strings.ToLower(domain)] = signer
	p.mu.Unlock()
	return nil
}

// AddSignerFromStore loads a signer from the configured key store
func (p *DKIMSignerPool) AddSignerFromStore(ctx context.Context, domain string) error {
	if p.store == nil {
		return fmt.Errorf("no key store configured")
	}

	privateKey, selector, err := p.store.LoadKey(ctx, domain)
	if err != nil {
		return fmt.Errorf("failed to load key from store: %w", err)
	}

	signer := &DKIMSigner{
		domain:     domain,
		selector:   selector,
		privateKey: privateKey,
	}

	p.mu.Lock()
	p.signers[strings.ToLower(domain)] = signer
	p.mu.Unlock()
	return nil
}

// AddSignerWithKey adds a signer with an existing key (for in-memory key management)
func (p *DKIMSignerPool) AddSignerWithKey(domain, selector string, privateKey *rsa.PrivateKey) {
	signer := &DKIMSigner{
		domain:     domain,
		selector:   selector,
		privateKey: privateKey,
	}
	p.mu.Lock()
	p.signers[strings.ToLower(domain)] = signer
	p.mu.Unlock()
}

// ReloadSigner reloads a specific domain's signer from the store
func (p *DKIMSignerPool) ReloadSigner(ctx context.Context, domain string) error {
	return p.AddSignerFromStore(ctx, domain)
}

// RemoveSigner removes a domain's signer
func (p *DKIMSignerPool) RemoveSigner(domain string) {
	p.mu.Lock()
	delete(p.signers, strings.ToLower(domain))
	p.mu.Unlock()
}

// GetSigner returns the DKIM signer for a domain
func (p *DKIMSignerPool) GetSigner(domain string) *DKIMSigner {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.signers[strings.ToLower(domain)]
}

// HasSigner checks if a signer exists for a domain
func (p *DKIMSignerPool) HasSigner(domain string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	_, exists := p.signers[strings.ToLower(domain)]
	return exists
}

// ListDomains returns all domains with loaded signers
func (p *DKIMSignerPool) ListDomains() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	domains := make([]string, 0, len(p.signers))
	for domain := range p.signers {
		domains = append(domains, domain)
	}
	return domains
}

// Sign signs a message using the appropriate domain signer
func (p *DKIMSignerPool) Sign(domain string, w io.Writer, r io.Reader) error {
	signer := p.GetSigner(domain)
	if signer == nil {
		return fmt.Errorf("no DKIM signer for domain: %s", domain)
	}
	return signer.Sign(w, r)
}

// LoadAllFromStore loads signers for all domains that have keys in the store
func (p *DKIMSignerPool) LoadAllFromStore(ctx context.Context) error {
	if p.store == nil {
		return fmt.Errorf("no key store configured")
	}

	domains, err := p.store.ListDomains(ctx)
	if err != nil {
		return fmt.Errorf("failed to list domains: %w", err)
	}

	var lastErr error
	for _, meta := range domains {
		if meta.HasKey {
			if err := p.AddSignerFromStore(ctx, meta.Domain); err != nil {
				lastErr = err
			}
		}
	}

	return lastErr
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

// GenerateAndSaveKey generates a new DKIM key and saves it to the store
func GenerateAndSaveKey(ctx context.Context, store DKIMKeyStore, domain, selector string, bits int) (*rsa.PrivateKey, error) {
	if bits < 2048 {
		bits = 2048
	}

	algorithm := fmt.Sprintf("RSA-%d", bits)

	privateKey, err := GenerateDKIMKey(bits)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	if err := store.SaveKey(ctx, domain, privateKey, selector, algorithm); err != nil {
		return nil, fmt.Errorf("failed to save key: %w", err)
	}

	return privateKey, nil
}

// GetDNSRecord returns the complete DKIM DNS TXT record for a domain
func GetDNSRecord(ctx context.Context, store DKIMKeyStore, domain string) (string, string, error) {
	meta, err := store.GetKeyMetadata(ctx, domain)
	if err != nil {
		return "", "", fmt.Errorf("failed to get key metadata: %w", err)
	}

	if !meta.HasKey {
		return "", "", fmt.Errorf("no DKIM key found for domain: %s", domain)
	}

	dnsValue, err := store.GetPublicKeyDNS(ctx, domain)
	if err != nil {
		return "", "", fmt.Errorf("failed to get public key: %w", err)
	}

	// DNS record name
	recordName := fmt.Sprintf("%s._domainkey.%s", meta.Selector, domain)

	return recordName, dnsValue, nil
}

// RotateKey generates a new DKIM key with a new selector
func RotateKey(ctx context.Context, store DKIMKeyStore, domain string, bits int) (string, *rsa.PrivateKey, error) {
	if bits < 2048 {
		bits = 2048
	}

	// Check if domain exists / has key metadata
	_, err := store.GetKeyMetadata(ctx, domain)
	if err != nil {
		// If no existing key, just generate with default selector
		key, err := GenerateAndSaveKey(ctx, store, domain, "mail", bits)
		return "mail", key, err
	}

	// Generate new selector based on timestamp
	newSelector := fmt.Sprintf("mail%d", time.Now().Unix())

	// Generate new key with new selector
	key, err := GenerateAndSaveKey(ctx, store, domain, newSelector, bits)
	if err != nil {
		return "", nil, err
	}

	return newSelector, key, nil
}

// ValidateKeyPair checks if the stored key is valid
func ValidateKeyPair(ctx context.Context, store DKIMKeyStore, domain string) error {
	privateKey, _, err := store.LoadKey(ctx, domain)
	if err != nil {
		return fmt.Errorf("failed to load key: %w", err)
	}

	// Validate the key
	if err := privateKey.Validate(); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}

	return nil
}

// GetAllDNSRecords returns all DNS records needed for a domain
func GetAllDNSRecords(ctx context.Context, store DKIMKeyStore, domain, hostname string) (*DNSRecords, error) {
	records := &DNSRecords{
		SPF:   fmt.Sprintf("v=spf1 mx a:%s -all", hostname),
		DMARC: fmt.Sprintf("v=DMARC1; p=quarantine; rua=mailto:postmaster@%s", domain),
		MX:    fmt.Sprintf("10 %s.", hostname),
	}

	// Get DKIM record if key exists
	meta, err := store.GetKeyMetadata(ctx, domain)
	if err == nil && meta.HasKey {
		dnsValue, err := store.GetPublicKeyDNS(ctx, domain)
		if err == nil {
			records.DKIM = dnsValue
		}
	}

	return records, nil
}
