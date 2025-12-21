// Package autodiscover provides email client auto-configuration endpoints.
// Supports Mozilla Autoconfig, Microsoft Autodiscover, and Apple Mail profiles.
package autodiscover

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"text/template"
	"time"
)

// Config holds the autodiscover server configuration
type Config struct {
	Domain      string // Primary mail domain (e.g., "fenilsonani.com")
	Hostname    string // Mail server hostname (e.g., "mail.fenilsonani.com")
	IMAPPort    int    // IMAP port (default: 993)
	SMTPPort    int    // SMTP submission port (default: 587)
	DisplayName string // Display name for the mail service
}

// Server handles autodiscover requests
type Server struct {
	config Config
	logger *slog.Logger
	mux    *http.ServeMux
}

// NewServer creates a new autodiscover server
func NewServer(config Config, logger *slog.Logger) *Server {
	if config.IMAPPort == 0 {
		config.IMAPPort = 993
	}
	if config.SMTPPort == 0 {
		config.SMTPPort = 587
	}
	if config.DisplayName == "" {
		config.DisplayName = config.Domain + " Mail"
	}

	s := &Server{
		config: config,
		logger: logger,
		mux:    http.NewServeMux(),
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// Mozilla Autoconfig
	s.mux.HandleFunc("/.well-known/autoconfig/mail/config-v1.1.xml", s.handleMozillaAutoconfig)
	s.mux.HandleFunc("/mail/config-v1.1.xml", s.handleMozillaAutoconfig)

	// Microsoft Autodiscover
	s.mux.HandleFunc("/autodiscover/autodiscover.xml", s.handleMicrosoftAutodiscover)
	s.mux.HandleFunc("/Autodiscover/Autodiscover.xml", s.handleMicrosoftAutodiscover)
	s.mux.HandleFunc("/AutoDiscover/AutoDiscover.xml", s.handleMicrosoftAutodiscover)

	// Apple Mail mobileconfig profile
	s.mux.HandleFunc("/email.mobileconfig", s.handleAppleMobileconfig)
	s.mux.HandleFunc("/.well-known/email.mobileconfig", s.handleAppleMobileconfig)

	// MTA-STS policy
	s.mux.HandleFunc("/.well-known/mta-sts.txt", s.handleMTASTS)
}

// ServeHTTP implements http.Handler
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Mozilla Autoconfig XML response
type mozillaConfig struct {
	XMLName       xml.Name `xml:"clientConfig"`
	Version       string   `xml:"version,attr"`
	EmailProvider struct {
		ID          string `xml:"id,attr"`
		Domain      string `xml:"domain"`
		DisplayName string `xml:"displayName"`
		InServer    struct {
			Type       string `xml:"type,attr"`
			Hostname   string `xml:"hostname"`
			Port       int    `xml:"port"`
			SocketType string `xml:"socketType"`
			Username   string `xml:"username"`
			Auth       string `xml:"authentication"`
		} `xml:"incomingServer"`
		OutServer struct {
			Type       string `xml:"type,attr"`
			Hostname   string `xml:"hostname"`
			Port       int    `xml:"port"`
			SocketType string `xml:"socketType"`
			Username   string `xml:"username"`
			Auth       string `xml:"authentication"`
		} `xml:"outgoingServer"`
	} `xml:"emailProvider"`
}

func (s *Server) handleMozillaAutoconfig(w http.ResponseWriter, r *http.Request) {
	// Get email from query parameter
	email := r.URL.Query().Get("emailaddress")
	if email == "" {
		email = "%EMAILADDRESS%"
	}

	config := mozillaConfig{
		Version: "1.1",
	}
	config.EmailProvider.ID = s.config.Domain
	config.EmailProvider.Domain = s.config.Domain
	config.EmailProvider.DisplayName = s.config.DisplayName

	// IMAP settings
	config.EmailProvider.InServer.Type = "imap"
	config.EmailProvider.InServer.Hostname = s.config.Hostname
	config.EmailProvider.InServer.Port = s.config.IMAPPort
	config.EmailProvider.InServer.SocketType = "SSL"
	config.EmailProvider.InServer.Username = email
	config.EmailProvider.InServer.Auth = "password-cleartext"

	// SMTP settings
	config.EmailProvider.OutServer.Type = "smtp"
	config.EmailProvider.OutServer.Hostname = s.config.Hostname
	config.EmailProvider.OutServer.Port = s.config.SMTPPort
	config.EmailProvider.OutServer.SocketType = "STARTTLS"
	config.EmailProvider.OutServer.Username = email
	config.EmailProvider.OutServer.Auth = "password-cleartext"

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	xmlData, _ := xml.MarshalIndent(config, "", "  ")
	w.Write([]byte(xml.Header))
	w.Write(xmlData)

	s.logger.Info("Mozilla autoconfig served",
		slog.String("email", email),
		slog.String("remote_addr", r.RemoteAddr))
}

// Microsoft Autodiscover request/response
type autodiscoverRequest struct {
	XMLName xml.Name `xml:"Autodiscover"`
	Request struct {
		Email string `xml:"EMailAddress"`
	} `xml:"Request"`
}

func (s *Server) handleMicrosoftAutodiscover(w http.ResponseWriter, r *http.Request) {
	var email string

	if r.Method == http.MethodPost {
		var req autodiscoverRequest
		if err := xml.NewDecoder(r.Body).Decode(&req); err == nil {
			email = req.Request.Email
		}
	}

	if email == "" {
		email = r.URL.Query().Get("Email")
	}
	if email == "" {
		email = "user@" + s.config.Domain
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/responseschema/2006">
  <Response xmlns="http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a">
    <Account>
      <AccountType>email</AccountType>
      <Action>settings</Action>
      <Protocol>
        <Type>IMAP</Type>
        <Server>%s</Server>
        <Port>%d</Port>
        <LoginName>%s</LoginName>
        <DomainRequired>off</DomainRequired>
        <SPA>off</SPA>
        <SSL>on</SSL>
        <AuthRequired>on</AuthRequired>
      </Protocol>
      <Protocol>
        <Type>SMTP</Type>
        <Server>%s</Server>
        <Port>%d</Port>
        <LoginName>%s</LoginName>
        <DomainRequired>off</DomainRequired>
        <SPA>off</SPA>
        <Encryption>TLS</Encryption>
        <AuthRequired>on</AuthRequired>
        <UsePOPAuth>on</UsePOPAuth>
        <SMTPLast>off</SMTPLast>
      </Protocol>
    </Account>
  </Response>
</Autodiscover>`,
		s.config.Hostname, s.config.IMAPPort, email,
		s.config.Hostname, s.config.SMTPPort, email)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(response))

	s.logger.Info("Microsoft autodiscover served",
		slog.String("email", email),
		slog.String("remote_addr", r.RemoteAddr))
}

// Apple mobileconfig template
const appleMobileconfigTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>PayloadContent</key>
    <array>
        <dict>
            <key>EmailAccountDescription</key>
            <string>{{.DisplayName}}</string>
            <key>EmailAccountName</key>
            <string>{{.Email}}</string>
            <key>EmailAccountType</key>
            <string>EmailTypeIMAP</string>
            <key>EmailAddress</key>
            <string>{{.Email}}</string>
            <key>IncomingMailServerAuthentication</key>
            <string>EmailAuthPassword</string>
            <key>IncomingMailServerHostName</key>
            <string>{{.Hostname}}</string>
            <key>IncomingMailServerPortNumber</key>
            <integer>{{.IMAPPort}}</integer>
            <key>IncomingMailServerUseSSL</key>
            <true/>
            <key>IncomingMailServerUsername</key>
            <string>{{.Email}}</string>
            <key>OutgoingMailServerAuthentication</key>
            <string>EmailAuthPassword</string>
            <key>OutgoingMailServerHostName</key>
            <string>{{.Hostname}}</string>
            <key>OutgoingMailServerPortNumber</key>
            <integer>465</integer>
            <key>OutgoingMailServerUseSSL</key>
            <true/>
            <key>OutgoingMailServerUsername</key>
            <string>{{.Email}}</string>
            <key>OutgoingPasswordSameAsIncomingPassword</key>
            <true/>
            <key>PayloadDescription</key>
            <string>Email account configuration for {{.Domain}}</string>
            <key>PayloadDisplayName</key>
            <string>{{.DisplayName}}</string>
            <key>PayloadIdentifier</key>
            <string>com.{{.Domain}}.email</string>
            <key>PayloadType</key>
            <string>com.apple.mail.managed</string>
            <key>PayloadUUID</key>
            <string>{{.UUID}}</string>
            <key>PayloadVersion</key>
            <integer>1</integer>
            <key>PreventAppSheet</key>
            <false/>
            <key>PreventMove</key>
            <false/>
            <key>SMIMEEnabled</key>
            <false/>
        </dict>
    </array>
    <key>PayloadDescription</key>
    <string>Email configuration profile for {{.Domain}}</string>
    <key>PayloadDisplayName</key>
    <string>{{.DisplayName}}</string>
    <key>PayloadIdentifier</key>
    <string>com.{{.Domain}}.profile</string>
    <key>PayloadOrganization</key>
    <string>{{.Domain}}</string>
    <key>PayloadRemovalDisallowed</key>
    <false/>
    <key>PayloadType</key>
    <string>Configuration</string>
    <key>PayloadUUID</key>
    <string>{{.ProfileUUID}}</string>
    <key>PayloadVersion</key>
    <integer>1</integer>
</dict>
</plist>`

func (s *Server) handleAppleMobileconfig(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	if email == "" {
		// Show a simple form to enter email
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>%s - Email Setup</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; max-width: 400px; margin: 50px auto; padding: 20px; }
        h1 { font-size: 24px; }
        input[type=email] { width: 100%%; padding: 12px; margin: 10px 0; border: 1px solid #ccc; border-radius: 8px; font-size: 16px; }
        button { width: 100%%; padding: 12px; background: #007AFF; color: white; border: none; border-radius: 8px; font-size: 16px; cursor: pointer; }
        button:hover { background: #0056b3; }
        p { color: #666; font-size: 14px; }
    </style>
</head>
<body>
    <h1>%s Email Setup</h1>
    <p>Enter your email address to download the configuration profile for Apple Mail.</p>
    <form method="get">
        <input type="email" name="email" placeholder="your.email@%s" required>
        <button type="submit">Download Profile</button>
    </form>
    <p>After downloading, open the profile on your iPhone, iPad, or Mac to automatically configure your email.</p>
</body>
</html>`, s.config.DisplayName, s.config.DisplayName, s.config.Domain)
		return
	}

	// Validate email domain
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		http.Error(w, "Invalid email address", http.StatusBadRequest)
		return
	}

	// Generate deterministic UUIDs based on email
	uuid := generateUUID(email)
	profileUUID := generateUUID(email + "-profile")

	data := struct {
		DisplayName string
		Domain      string
		Hostname    string
		Email       string
		IMAPPort    int
		SMTPPort    int
		UUID        string
		ProfileUUID string
	}{
		DisplayName: s.config.DisplayName,
		Domain:      s.config.Domain,
		Hostname:    s.config.Hostname,
		Email:       email,
		IMAPPort:    s.config.IMAPPort,
		SMTPPort:    s.config.SMTPPort,
		UUID:        uuid,
		ProfileUUID: profileUUID,
	}

	tmpl, err := template.New("mobileconfig").Parse(appleMobileconfigTemplate)
	if err != nil {
		s.logger.Error("Failed to parse mobileconfig template", slog.Any("error", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Set headers for profile download
	w.Header().Set("Content-Type", "application/x-apple-aspen-config")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-email.mobileconfig\"", s.config.Domain))

	if err := tmpl.Execute(w, data); err != nil {
		s.logger.Error("Failed to execute mobileconfig template", slog.Any("error", err))
	}

	s.logger.Info("Apple mobileconfig served",
		slog.String("email", email),
		slog.String("remote_addr", r.RemoteAddr))
}

func (s *Server) handleMTASTS(w http.ResponseWriter, r *http.Request) {
	// MTA-STS policy for enforcing TLS
	policy := fmt.Sprintf(`version: STSv1
mode: enforce
mx: %s
max_age: 604800
`, s.config.Hostname)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(policy))
}

// generateUUID creates a deterministic UUID-like string from input
func generateUUID(input string) string {
	// Simple hash-based UUID generation
	hash := uint64(0)
	for _, c := range input {
		hash = hash*31 + uint64(c)
	}
	return fmt.Sprintf("%08X-%04X-%04X-%04X-%012X",
		hash&0xFFFFFFFF,
		(hash>>32)&0xFFFF,
		(hash>>48)&0xFFFF,
		(hash>>56)&0xFFFF,
		hash&0xFFFFFFFFFFFF)
}

// ListenAndServe starts the autodiscover server
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	server := &http.Server{
		Addr:         addr,
		Handler:      s,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	s.logger.Info("Autodiscover server starting", slog.String("addr", addr))
	return server.ListenAndServe()
}
