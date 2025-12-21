package dns

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Record represents a DNS record to be created
type Record struct {
	Type     string // MX, TXT, CNAME, A
	Host     string // hostname (@ for root)
	Value    string // record value
	Priority int    // for MX records
	TTL      int    // time to live in seconds
	Comment  string // helpful description
}

// Generator creates DNS record recommendations
type Generator struct {
	domain     string
	mailServer string
	serverIP   string
	dkimKey    *rsa.PublicKey
	dkimKeyPEM string
}

var (
	// ErrInvalidIP is returned when IP validation fails
	ErrInvalidIP = errors.New("invalid IP address")
	// ipv4Regex validates IPv4 addresses
	ipv4Regex = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
)

// NewGenerator creates a new DNS record generator
func NewGenerator(domain, mailServer, serverIP string) (*Generator, error) {
	// Validate inputs
	if domain == "" {
		return nil, fmt.Errorf("%w: domain cannot be empty", ErrInvalidDomain)
	}
	if mailServer == "" {
		return nil, fmt.Errorf("%w: mail server cannot be empty", ErrInvalidMailServer)
	}
	if serverIP == "" {
		return nil, fmt.Errorf("%w: server IP cannot be empty", ErrInvalidIP)
	}

	// Validate domain format
	if !domainRegex.MatchString(domain) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDomain, domain)
	}

	// Validate mail server format
	if !domainRegex.MatchString(mailServer) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMailServer, mailServer)
	}

	// Validate IP address
	ip := net.ParseIP(serverIP)
	if ip == nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidIP, serverIP)
	}

	return &Generator{
		domain:     domain,
		mailServer: mailServer,
		serverIP:   serverIP,
	}, nil
}

// SetDKIMKey sets the DKIM public key for record generation
func (g *Generator) SetDKIMKey(key *rsa.PublicKey) error {
	if key == nil {
		return errors.New("DKIM key cannot be nil")
	}
	// Validate key size (minimum 1024 bits for security)
	if key.N.BitLen() < 1024 {
		return fmt.Errorf("DKIM key too short: %d bits (minimum 1024)", key.N.BitLen())
	}
	g.dkimKey = key
	return nil
}

// SetDKIMKeyPEM sets the DKIM public key from PEM format
func (g *Generator) SetDKIMKeyPEM(pem string) error {
	if pem == "" {
		return errors.New("PEM string cannot be empty")
	}
	// Basic validation of PEM format
	if !strings.Contains(pem, "-----BEGIN") || !strings.Contains(pem, "-----END") {
		return errors.New("invalid PEM format: missing header or footer")
	}
	g.dkimKeyPEM = pem
	return nil
}

// GenerateAll generates all required DNS records
func (g *Generator) GenerateAll() []Record {
	var records []Record

	records = append(records, g.GenerateMX())
	records = append(records, g.GenerateA())
	records = append(records, g.GenerateSPF())
	records = append(records, g.GenerateDKIM())
	records = append(records, g.GenerateDMARC())

	return records
}

// GenerateMX generates MX record
func (g *Generator) GenerateMX() Record {
	return Record{
		Type:     "MX",
		Host:     "@",
		Value:    g.mailServer + ".",
		Priority: 10,
		TTL:      3600,
		Comment:  "Mail server for " + g.domain,
	}
}

// GenerateA generates A record for mail server
func (g *Generator) GenerateA() Record {
	host := g.mailServer
	if strings.HasSuffix(host, "."+g.domain) {
		host = strings.TrimSuffix(host, "."+g.domain)
	}

	return Record{
		Type:    "A",
		Host:    host,
		Value:   g.serverIP,
		TTL:     3600,
		Comment: "Mail server IP address",
	}
}

// GenerateSPF generates SPF record
func (g *Generator) GenerateSPF() Record {
	spf := fmt.Sprintf("v=spf1 mx a:%s -all", g.mailServer)
	return Record{
		Type:    "TXT",
		Host:    "@",
		Value:   spf,
		TTL:     3600,
		Comment: "SPF record - authorizes mail server to send email",
	}
}

// GenerateDKIM generates DKIM record
func (g *Generator) GenerateDKIM() Record {
	var value string

	if g.dkimKey != nil {
		// Generate from RSA public key
		pubBytes, err := x509.MarshalPKIXPublicKey(g.dkimKey)
		if err != nil {
			// Log error and use placeholder - don't silently fail
			value = fmt.Sprintf("v=DKIM1; k=rsa; p=<ERROR_MARSHALING_KEY:%s>", err.Error())
		} else {
			pubB64 := base64.StdEncoding.EncodeToString(pubBytes)
			value = fmt.Sprintf("v=DKIM1; k=rsa; p=%s", pubB64)
		}
	} else if g.dkimKeyPEM != "" {
		// Extract key from PEM
		lines := strings.Split(g.dkimKeyPEM, "\n")
		var keyData strings.Builder
		for _, line := range lines {
			if !strings.HasPrefix(line, "-----") && line != "" {
				keyData.WriteString(strings.TrimSpace(line))
			}
		}
		if keyData.Len() == 0 {
			value = "v=DKIM1; k=rsa; p=<INVALID_PEM_NO_KEY_DATA>"
		} else {
			value = fmt.Sprintf("v=DKIM1; k=rsa; p=%s", keyData.String())
		}
	} else {
		value = "v=DKIM1; k=rsa; p=<YOUR_DKIM_PUBLIC_KEY_HERE>"
	}

	return Record{
		Type:    "TXT",
		Host:    "mail._domainkey",
		Value:   value,
		TTL:     3600,
		Comment: "DKIM signing key - verifies email authenticity",
	}
}

// GenerateDMARC generates DMARC record
func (g *Generator) GenerateDMARC() Record {
	dmarc := fmt.Sprintf("v=DMARC1; p=quarantine; rua=mailto:postmaster@%s; ruf=mailto:postmaster@%s; fo=1", g.domain, g.domain)
	return Record{
		Type:    "TXT",
		Host:    "_dmarc",
		Value:   dmarc,
		TTL:     3600,
		Comment: "DMARC policy - handles failed authentication",
	}
}

// FormatAsZone formats records as BIND zone file format
func FormatAsZone(records []Record, domain string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("; DNS records for %s\n", domain))
	sb.WriteString("; Generated by mailserver dns generate\n\n")

	for _, r := range records {
		host := r.Host
		if host == "@" {
			host = domain + "."
		} else if !strings.HasSuffix(host, ".") {
			host = host + "." + domain + "."
		}

		switch r.Type {
		case "MX":
			sb.WriteString(fmt.Sprintf("; %s\n", r.Comment))
			sb.WriteString(fmt.Sprintf("%s\t%d\tIN\t%s\t%d\t%s\n\n", host, r.TTL, r.Type, r.Priority, r.Value))
		case "TXT":
			sb.WriteString(fmt.Sprintf("; %s\n", r.Comment))
			// Split long TXT records
			value := r.Value
			if len(value) > 255 {
				value = splitTXTRecord(value)
			} else {
				value = fmt.Sprintf("\"%s\"", value)
			}
			sb.WriteString(fmt.Sprintf("%s\t%d\tIN\t%s\t%s\n\n", host, r.TTL, r.Type, value))
		default:
			sb.WriteString(fmt.Sprintf("; %s\n", r.Comment))
			sb.WriteString(fmt.Sprintf("%s\t%d\tIN\t%s\t%s\n\n", host, r.TTL, r.Type, r.Value))
		}
	}

	return sb.String()
}

// FormatForProvider formats records for easy copy-paste to DNS providers
func FormatForProvider(records []Record, domain string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("DNS Records for %s\n", domain))
	sb.WriteString("=" + strings.Repeat("=", 50) + "\n\n")

	for _, r := range records {
		sb.WriteString(fmt.Sprintf("Type: %s\n", r.Type))
		sb.WriteString(fmt.Sprintf("Host/Name: %s\n", r.Host))
		if r.Type == "MX" {
			sb.WriteString(fmt.Sprintf("Priority: %d\n", r.Priority))
		}
		sb.WriteString(fmt.Sprintf("Value: %s\n", r.Value))
		sb.WriteString(fmt.Sprintf("TTL: %d\n", r.TTL))
		sb.WriteString(fmt.Sprintf("Note: %s\n", r.Comment))
		sb.WriteString(strings.Repeat("-", 50) + "\n\n")
	}

	return sb.String()
}

// splitTXTRecord splits a long TXT record into 255-byte chunks
func splitTXTRecord(value string) string {
	var parts []string
	for len(value) > 0 {
		end := 255
		if end > len(value) {
			end = len(value)
		}
		parts = append(parts, fmt.Sprintf("\"%s\"", value[:end]))
		value = value[end:]
	}
	return strings.Join(parts, " ")
}
