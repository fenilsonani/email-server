package security

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"strings"
	"testing"
)

func generateTestKey(t *testing.T) (string, *rsa.PrivateKey) {
	// Generate a test RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "dkim_test_*.pem")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Encode to PEM
	keyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: keyBytes,
	}

	if err := pem.Encode(tmpFile, block); err != nil {
		t.Fatalf("Failed to encode key: %v", err)
	}

	tmpFile.Close()
	return tmpFile.Name(), privateKey
}

func TestNewDKIMSigner(t *testing.T) {
	keyPath, _ := generateTestKey(t)
	defer os.Remove(keyPath)

	signer, err := NewDKIMSigner("example.com", "mail", keyPath)
	if err != nil {
		t.Fatalf("NewDKIMSigner failed: %v", err)
	}

	if signer.domain != "example.com" {
		t.Errorf("Expected domain 'example.com', got '%s'", signer.domain)
	}

	if signer.selector != "mail" {
		t.Errorf("Expected selector 'mail', got '%s'", signer.selector)
	}

	if signer.privateKey == nil {
		t.Error("Expected non-nil private key")
	}
}

func TestNewDKIMSigner_InvalidPath(t *testing.T) {
	_, err := NewDKIMSigner("example.com", "mail", "/nonexistent/path.pem")
	if err == nil {
		t.Error("Expected error for invalid path")
	}
}

func TestNewDKIMSigner_InvalidKey(t *testing.T) {
	// Create temp file with invalid content
	tmpFile, _ := os.CreateTemp("", "invalid_key_*.pem")
	tmpFile.WriteString("not a valid PEM key")
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	_, err := NewDKIMSigner("example.com", "mail", tmpFile.Name())
	if err == nil {
		t.Error("Expected error for invalid key")
	}
}

func TestDKIMSigner_Sign(t *testing.T) {
	keyPath, _ := generateTestKey(t)
	defer os.Remove(keyPath)

	signer, err := NewDKIMSigner("example.com", "mail", keyPath)
	if err != nil {
		t.Fatalf("NewDKIMSigner failed: %v", err)
	}

	// Create a test email
	email := `From: sender@example.com
To: recipient@example.com
Subject: Test Message
Date: Thu, 19 Dec 2024 12:00:00 +0000
Message-ID: <test@example.com>
Content-Type: text/plain

This is a test message.
`

	var signedBuf bytes.Buffer
	err = signer.Sign(&signedBuf, strings.NewReader(email))
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	signed := signedBuf.String()

	// Check that DKIM-Signature header was added
	if !strings.Contains(signed, "DKIM-Signature:") {
		t.Error("Expected DKIM-Signature header in signed message")
	}

	// Check that the signature contains expected fields
	if !strings.Contains(signed, "d=example.com") {
		t.Error("Expected domain in DKIM signature")
	}

	if !strings.Contains(signed, "s=mail") {
		t.Error("Expected selector in DKIM signature")
	}

	// Original content should be preserved
	if !strings.Contains(signed, "This is a test message.") {
		t.Error("Original message content should be preserved")
	}
}

func TestDKIMSignerPool(t *testing.T) {
	keyPath1, _ := generateTestKey(t)
	defer os.Remove(keyPath1)

	keyPath2, _ := generateTestKey(t)
	defer os.Remove(keyPath2)

	pool := NewDKIMSignerPool()

	// Add signers
	err := pool.AddSigner("example.com", "mail", keyPath1)
	if err != nil {
		t.Fatalf("AddSigner failed: %v", err)
	}

	err = pool.AddSigner("example.org", "default", keyPath2)
	if err != nil {
		t.Fatalf("AddSigner failed: %v", err)
	}

	// Get signers
	signer1 := pool.GetSigner("example.com")
	if signer1 == nil {
		t.Error("Expected signer for example.com")
	}

	signer2 := pool.GetSigner("EXAMPLE.ORG") // Test case insensitivity
	if signer2 == nil {
		t.Error("Expected signer for example.org (case insensitive)")
	}

	// Non-existent domain
	signer3 := pool.GetSigner("nonexistent.com")
	if signer3 != nil {
		t.Error("Expected nil for non-existent domain")
	}
}

func TestDKIMSignerPool_Sign(t *testing.T) {
	keyPath, _ := generateTestKey(t)
	defer os.Remove(keyPath)

	pool := NewDKIMSignerPool()
	pool.AddSigner("example.com", "mail", keyPath)

	email := `From: sender@example.com
To: recipient@example.com
Subject: Test

Body
`

	var buf bytes.Buffer
	err := pool.Sign("example.com", &buf, strings.NewReader(email))
	if err != nil {
		t.Fatalf("Pool Sign failed: %v", err)
	}

	if !strings.Contains(buf.String(), "DKIM-Signature:") {
		t.Error("Expected DKIM-Signature in signed message")
	}

	// Sign with non-existent domain
	err = pool.Sign("nonexistent.com", &buf, strings.NewReader(email))
	if err == nil {
		t.Error("Expected error for non-existent domain")
	}
}

func TestGenerateDKIMKey(t *testing.T) {
	// Test with default bits
	key, err := GenerateDKIMKey(0)
	if err != nil {
		t.Fatalf("GenerateDKIMKey failed: %v", err)
	}

	if key.N.BitLen() != 2048 {
		t.Errorf("Expected 2048 bit key, got %d", key.N.BitLen())
	}

	// Test with explicit bits
	key, err = GenerateDKIMKey(4096)
	if err != nil {
		t.Fatalf("GenerateDKIMKey failed: %v", err)
	}

	if key.N.BitLen() != 4096 {
		t.Errorf("Expected 4096 bit key, got %d", key.N.BitLen())
	}
}

func TestFormatDKIMPublicKey(t *testing.T) {
	key, _ := GenerateDKIMKey(2048)

	dnsRecord, err := FormatDKIMPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("FormatDKIMPublicKey failed: %v", err)
	}

	// Check format
	if !strings.HasPrefix(dnsRecord, "v=DKIM1; k=rsa; p=") {
		t.Errorf("Invalid DNS record format: %s", dnsRecord)
	}

	// Should contain base64 encoded key
	if len(dnsRecord) < 100 {
		t.Error("DNS record seems too short")
	}
}

func TestGenerateDNSRecords(t *testing.T) {
	key, _ := GenerateDKIMKey(2048)

	records, err := GenerateDNSRecords("example.com", "mail.example.com", "mail", &key.PublicKey)
	if err != nil {
		t.Fatalf("GenerateDNSRecords failed: %v", err)
	}

	// Check DKIM record
	if !strings.Contains(records.DKIM, "mail._domainkey.example.com") {
		t.Errorf("DKIM record should contain selector and domain: %s", records.DKIM)
	}

	// Check SPF record
	if !strings.Contains(records.SPF, "v=spf1") {
		t.Errorf("Invalid SPF record: %s", records.SPF)
	}

	if !strings.Contains(records.SPF, "mail.example.com") {
		t.Errorf("SPF record should contain hostname: %s", records.SPF)
	}

	// Check DMARC record
	if !strings.Contains(records.DMARC, "v=DMARC1") {
		t.Errorf("Invalid DMARC record: %s", records.DMARC)
	}

	if !strings.Contains(records.DMARC, "_dmarc.example.com") {
		t.Errorf("DMARC record should contain domain: %s", records.DMARC)
	}

	// Check MX record
	if !strings.Contains(records.MX, "mail.example.com") {
		t.Errorf("MX record should contain hostname: %s", records.MX)
	}
}

func TestGenerateDNSRecords_NilKey(t *testing.T) {
	records, err := GenerateDNSRecords("example.com", "mail.example.com", "mail", nil)
	if err != nil {
		t.Fatalf("GenerateDNSRecords should not fail with nil key: %v", err)
	}

	// DKIM record should be empty without key
	if records.DKIM != "" {
		t.Error("DKIM record should be empty without public key")
	}

	// Other records should still be generated
	if records.SPF == "" {
		t.Error("SPF record should be generated")
	}

	if records.DMARC == "" {
		t.Error("DMARC record should be generated")
	}

	if records.MX == "" {
		t.Error("MX record should be generated")
	}
}

func TestDKIMSigner_PKCS8Key(t *testing.T) {
	// Generate key and save as PKCS#8
	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)

	tmpFile, _ := os.CreateTemp("", "dkim_pkcs8_*.pem")
	keyBytes, _ := x509.MarshalPKCS8PrivateKey(privateKey)
	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	}
	pem.Encode(tmpFile, block)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	// Should be able to load PKCS#8 key
	signer, err := NewDKIMSigner("example.com", "mail", tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load PKCS#8 key: %v", err)
	}

	if signer.privateKey == nil {
		t.Error("Expected non-nil private key")
	}
}
