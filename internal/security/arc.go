// Package security provides email security features.
package security

import (
	"bufio"
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ARCChainValidation represents ARC chain validation state.
type ARCChainValidation string

const (
	ARCChainNone ARCChainValidation = "none" // No ARC headers present
	ARCChainPass ARCChainValidation = "pass" // All ARC sets validated
	ARCChainFail ARCChainValidation = "fail" // ARC validation failed
)

// ARCSet represents a complete ARC header set (i=N).
type ARCSet struct {
	Instance              int
	AuthenticationResults string // ARC-Authentication-Results header value
	MessageSignature      string // ARC-Message-Signature header value
	Seal                  string // ARC-Seal header value
}

// ARCSigner handles ARC signing for messages.
type ARCSigner struct {
	domain     string
	selector   string
	privateKey *rsa.PrivateKey
	authServID string
}

// ARCSignerPool manages ARC signers for multiple domains.
type ARCSignerPool struct {
	signers map[string]*ARCSigner
	mu      sync.RWMutex
}

// ARCSignOptions configures ARC signing.
type ARCSignOptions struct {
	// Instance is the ARC set instance number (1 for first hop)
	Instance int
	// AuthResults is the Authentication-Results for this hop
	AuthResults *AuthenticationResults
	// ChainValidation is the result of validating prior ARC sets
	ChainValidation ARCChainValidation
	// Timestamp for signature (defaults to now)
	Timestamp time.Time
}

// NewARCSigner creates a new ARC signer.
func NewARCSigner(domain, selector string, privateKey *rsa.PrivateKey, authServID string) *ARCSigner {
	if authServID == "" {
		authServID = domain
	}
	return &ARCSigner{
		domain:     domain,
		selector:   selector,
		privateKey: privateKey,
		authServID: authServID,
	}
}

// NewARCSignerPool creates a new pool of ARC signers.
func NewARCSignerPool() *ARCSignerPool {
	return &ARCSignerPool{
		signers: make(map[string]*ARCSigner),
	}
}

// AddSigner adds an ARC signer for a domain.
func (p *ARCSignerPool) AddSigner(domain, selector string, privateKey *rsa.PrivateKey, authServID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.signers[strings.ToLower(domain)] = NewARCSigner(domain, selector, privateKey, authServID)
}

// GetSigner returns the ARC signer for a domain.
func (p *ARCSignerPool) GetSigner(domain string) *ARCSigner {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.signers[strings.ToLower(domain)]
}

// Sign adds ARC headers to a message.
// Returns the complete message with ARC headers prepended.
func (s *ARCSigner) Sign(message []byte, opts ARCSignOptions) ([]byte, error) {
	if opts.Instance < 1 {
		opts.Instance = 1
	}
	if opts.ChainValidation == "" {
		opts.ChainValidation = ARCChainNone
	}
	if opts.Timestamp.IsZero() {
		opts.Timestamp = time.Now()
	}

	// For instance > 1 with chain validation "none", that's invalid
	if opts.Instance > 1 && opts.ChainValidation == ARCChainNone {
		return nil, fmt.Errorf("invalid chain validation for instance %d", opts.Instance)
	}

	// Generate ARC-Authentication-Results
	aar := s.generateAAR(opts)

	// Generate ARC-Message-Signature
	ams, err := s.generateAMS(message, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ARC-Message-Signature: %w", err)
	}

	// Generate ARC-Seal
	seal, err := s.generateSeal(aar, ams, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ARC-Seal: %w", err)
	}

	// Prepend ARC headers to message
	var result bytes.Buffer
	result.WriteString(fmt.Sprintf("ARC-Seal: %s\r\n", seal))
	result.WriteString(fmt.Sprintf("ARC-Message-Signature: %s\r\n", ams))
	result.WriteString(fmt.Sprintf("ARC-Authentication-Results: %s\r\n", aar))
	result.Write(message)

	return result.Bytes(), nil
}

// generateAAR generates the ARC-Authentication-Results header.
func (s *ARCSigner) generateAAR(opts ARCSignOptions) string {
	if opts.AuthResults != nil {
		return opts.AuthResults.FormatARC(opts.Instance)
	}
	return fmt.Sprintf("i=%d; %s; none", opts.Instance, s.authServID)
}

// generateAMS generates the ARC-Message-Signature header.
// This is similar to DKIM signature but with i= instance tag.
func (s *ARCSigner) generateAMS(message []byte, opts ARCSignOptions) (string, error) {
	// Headers to sign (similar to DKIM)
	headersToSign := []string{
		"From",
		"To",
		"Subject",
		"Date",
		"Message-ID",
		"Content-Type",
		"MIME-Version",
		"ARC-Authentication-Results",
	}

	// Parse message headers
	headers, body := parseMessage(message)

	// Build header canonicalization
	signedHeaders := make([]string, 0, len(headersToSign))
	canonHeaders := bytes.Buffer{}

	for _, headerName := range headersToSign {
		headerValue := getHeader(headers, headerName)
		if headerValue != "" {
			signedHeaders = append(signedHeaders, strings.ToLower(headerName))
			canonHeaders.WriteString(relaxedHeaderCanonicalization(headerName, headerValue))
		}
	}

	// Calculate body hash (relaxed canonicalization)
	bodyHash := calculateBodyHash(body)

	// Build the ARC-Message-Signature header (without signature)
	timestamp := opts.Timestamp.Unix()
	amsHeader := fmt.Sprintf("i=%d; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s; t=%d; h=%s; bh=%s; b=",
		opts.Instance,
		s.domain,
		s.selector,
		timestamp,
		strings.Join(signedHeaders, ":"),
		bodyHash,
	)

	// Add AMS to canonicalized headers for signing
	canonHeaders.WriteString(relaxedHeaderCanonicalization("arc-message-signature", amsHeader))

	// Remove trailing CRLF from last header
	canonData := bytes.TrimSuffix(canonHeaders.Bytes(), []byte("\r\n"))

	// Sign
	signature, err := s.signData(canonData)
	if err != nil {
		return "", err
	}

	return amsHeader + signature, nil
}

// generateSeal generates the ARC-Seal header.
func (s *ARCSigner) generateSeal(aar, ams string, opts ARCSignOptions) (string, error) {
	// Headers to sign for seal (all previous ARC headers + current AAR and AMS)
	var canonHeaders bytes.Buffer

	// For instance > 1, we need to include previous ARC headers
	// For simplicity, we sign just the current set's AAR and AMS
	// A full implementation would extract and include all previous ARC-Seal headers

	// Add current ARC headers
	canonHeaders.WriteString(relaxedHeaderCanonicalization("arc-authentication-results", aar))
	canonHeaders.WriteString(relaxedHeaderCanonicalization("arc-message-signature", ams))

	// Build the ARC-Seal header (without signature)
	timestamp := opts.Timestamp.Unix()
	sealHeader := fmt.Sprintf("i=%d; a=rsa-sha256; t=%d; cv=%s; d=%s; s=%s; b=",
		opts.Instance,
		timestamp,
		opts.ChainValidation,
		s.domain,
		s.selector,
	)

	// Add seal header to canonicalized data
	canonHeaders.WriteString(relaxedHeaderCanonicalization("arc-seal", sealHeader))

	// Remove trailing CRLF
	canonData := bytes.TrimSuffix(canonHeaders.Bytes(), []byte("\r\n"))

	// Sign
	signature, err := s.signData(canonData)
	if err != nil {
		return "", err
	}

	return sealHeader + signature, nil
}

// signData signs data using RSA-SHA256.
func (s *ARCSigner) signData(data []byte) (string, error) {
	hash := sha256.Sum256(data)

	signature, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(signature), nil
}

// parseMessage parses a message into headers and body.
func parseMessage(message []byte) ([]string, []byte) {
	// Find the header/body separator (blank line)
	idx := bytes.Index(message, []byte("\r\n\r\n"))
	if idx == -1 {
		idx = bytes.Index(message, []byte("\n\n"))
		if idx == -1 {
			return nil, message
		}
		return strings.Split(string(message[:idx]), "\n"), message[idx+2:]
	}
	return strings.Split(string(message[:idx]), "\r\n"), message[idx+4:]
}

// getHeader gets a header value from parsed headers.
func getHeader(headers []string, name string) string {
	name = strings.ToLower(name)
	var value strings.Builder
	inHeader := false

	for _, line := range headers {
		if inHeader {
			// Check if this is a continuation line
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				value.WriteString(line)
				continue
			}
			inHeader = false
		}

		// Check for header start
		colonIdx := strings.Index(line, ":")
		if colonIdx > 0 {
			headerName := strings.ToLower(strings.TrimSpace(line[:colonIdx]))
			if headerName == name {
				value.WriteString(strings.TrimSpace(line[colonIdx+1:]))
				inHeader = true
			}
		}
	}

	return value.String()
}

// relaxedHeaderCanonicalization applies relaxed canonicalization to a header.
func relaxedHeaderCanonicalization(name, value string) string {
	// Convert name to lowercase
	name = strings.ToLower(name)

	// Unfold header value and reduce whitespace
	value = strings.Join(strings.Fields(value), " ")

	return fmt.Sprintf("%s:%s\r\n", name, value)
}

// calculateBodyHash calculates the body hash for signing.
func calculateBodyHash(body []byte) string {
	// Apply relaxed body canonicalization
	// - Reduce all sequences of WSP to a single SP
	// - Ignore all empty lines at end of body
	// - Ensure body ends with CRLF

	canon := relaxedBodyCanonicalization(body)
	hash := sha256.Sum256(canon)
	return base64.StdEncoding.EncodeToString(hash[:])
}

// relaxedBodyCanonicalization applies relaxed canonicalization to the body.
func relaxedBodyCanonicalization(body []byte) []byte {
	var result bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(body))

	for scanner.Scan() {
		line := scanner.Text()

		// Reduce sequences of whitespace to single space
		line = strings.Join(strings.Fields(line), " ")

		// Remove trailing whitespace
		line = strings.TrimRight(line, " \t")

		result.WriteString(line)
		result.WriteString("\r\n")
	}

	// Remove empty lines at the end
	canonical := result.Bytes()
	for len(canonical) > 2 && bytes.HasSuffix(canonical, []byte("\r\n\r\n")) {
		canonical = canonical[:len(canonical)-2]
	}

	// Ensure exactly one trailing CRLF
	if !bytes.HasSuffix(canonical, []byte("\r\n")) {
		canonical = append(canonical, '\r', '\n')
	}

	return canonical
}

// ARCVerifier validates ARC chains on incoming messages.
type ARCVerifier struct {
	// DNSLookup is a function to lookup DKIM public keys
	DNSLookup func(domain, selector string) (*rsa.PublicKey, error)
}

// ARCVerifyResult contains ARC validation results.
type ARCVerifyResult struct {
	ChainValid    bool
	InstanceCount int
	LatestResult  ARCChainValidation
	Sets          []ARCSetVerification
	Error         error
}

// ARCSetVerification contains verification for a single ARC set.
type ARCSetVerification struct {
	Instance    int
	SealValid   bool
	AMSValid    bool
	AARPresent  bool
	Error       error
}

// Verify validates all ARC sets in a message.
func (v *ARCVerifier) Verify(message []byte) *ARCVerifyResult {
	result := &ARCVerifyResult{
		LatestResult: ARCChainNone,
	}

	// Extract all ARC sets
	sets, err := ExtractARCSets(message)
	if err != nil {
		result.Error = err
		return result
	}

	if len(sets) == 0 {
		result.ChainValid = true // No ARC = valid (nothing to verify)
		return result
	}

	result.InstanceCount = len(sets)

	// Sort by instance
	sort.Slice(sets, func(i, j int) bool {
		return sets[i].Instance < sets[j].Instance
	})

	// Verify each set in order
	for i, set := range sets {
		verification := ARCSetVerification{
			Instance:   set.Instance,
			AARPresent: set.AuthenticationResults != "",
		}

		// Verify instance numbers are sequential
		expectedInstance := i + 1
		if set.Instance != expectedInstance {
			verification.Error = fmt.Errorf("instance %d expected, got %d", expectedInstance, set.Instance)
			result.Sets = append(result.Sets, verification)
			result.LatestResult = ARCChainFail
			return result
		}

		// For full verification, we would verify signatures here
		// This requires DNS lookups for public keys
		// For now, we mark as valid if all components are present
		verification.SealValid = set.Seal != ""
		verification.AMSValid = set.MessageSignature != ""

		if !verification.SealValid || !verification.AMSValid || !verification.AARPresent {
			verification.Error = fmt.Errorf("incomplete ARC set")
			result.LatestResult = ARCChainFail
		}

		result.Sets = append(result.Sets, verification)
	}

	// Check chain validation from last seal
	if len(sets) > 0 {
		lastSeal := sets[len(sets)-1].Seal
		if strings.Contains(lastSeal, "cv=pass") {
			result.LatestResult = ARCChainPass
		} else if strings.Contains(lastSeal, "cv=fail") {
			result.LatestResult = ARCChainFail
		} else if strings.Contains(lastSeal, "cv=none") && len(sets) == 1 {
			result.LatestResult = ARCChainPass // First set with cv=none is valid
		}
	}

	result.ChainValid = result.LatestResult != ARCChainFail
	return result
}

// ExtractARCSets extracts all ARC header sets from a message.
func ExtractARCSets(message []byte) ([]ARCSet, error) {
	headers, _ := parseMessage(message)

	// Group ARC headers by instance
	sealsByInstance := make(map[int]string)
	amsByInstance := make(map[int]string)
	aarByInstance := make(map[int]string)

	instanceRe := regexp.MustCompile(`i=(\d+)`)

	for _, header := range headers {
		colonIdx := strings.Index(header, ":")
		if colonIdx < 0 {
			continue
		}

		name := strings.ToLower(strings.TrimSpace(header[:colonIdx]))
		value := strings.TrimSpace(header[colonIdx+1:])

		// Extract instance number
		match := instanceRe.FindStringSubmatch(value)
		if match == nil {
			continue
		}
		instance, _ := strconv.Atoi(match[1])

		switch name {
		case "arc-seal":
			sealsByInstance[instance] = value
		case "arc-message-signature":
			amsByInstance[instance] = value
		case "arc-authentication-results":
			aarByInstance[instance] = value
		}
	}

	// Build ARC sets
	var sets []ARCSet
	for instance := range sealsByInstance {
		sets = append(sets, ARCSet{
			Instance:              instance,
			Seal:                  sealsByInstance[instance],
			MessageSignature:      amsByInstance[instance],
			AuthenticationResults: aarByInstance[instance],
		})
	}

	return sets, nil
}

// GetNextInstance returns the next ARC instance number for a message.
func GetNextInstance(message []byte) int {
	sets, _ := ExtractARCSets(message)
	if len(sets) == 0 {
		return 1
	}

	maxInstance := 0
	for _, set := range sets {
		if set.Instance > maxInstance {
			maxInstance = set.Instance
		}
	}
	return maxInstance + 1
}

// ValidateARCChain validates the existing ARC chain in a message.
func ValidateARCChain(message []byte) ARCChainValidation {
	verifier := &ARCVerifier{}
	result := verifier.Verify(message)

	if result.InstanceCount == 0 {
		return ARCChainNone
	}

	if result.ChainValid {
		return ARCChainPass
	}
	return ARCChainFail
}
