package dav

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/config"
)

// Server handles CalDAV and CardDAV requests
type Server struct {
	config         *config.Config
	authenticator  *auth.Authenticator
	caldavBackend  *CalDAVBackend
	carddavBackend *CardDAVBackend
	httpServer     *http.Server
	logger         *slog.Logger
}

const (
	// Maximum request body size (10MB)
	maxRequestBodySize = 10 * 1024 * 1024
)

var (
	// ErrNilConfig is returned when config is nil
	ErrNilConfig = errors.New("config cannot be nil")
	// ErrNilAuthenticator is returned when authenticator is nil
	ErrNilAuthenticator = errors.New("authenticator cannot be nil")
	// ErrNilDB is returned when database is nil
	ErrNilDB = errors.New("database cannot be nil")
	// ErrRequestTooLarge is returned when request body exceeds limit
	ErrRequestTooLarge = errors.New("request body too large")
)

// NewServer creates a new DAV server
func NewServer(cfg *config.Config, authenticator *auth.Authenticator, db *sql.DB) (*Server, error) {
	// Validate inputs
	if cfg == nil {
		return nil, ErrNilConfig
	}
	if authenticator == nil {
		return nil, ErrNilAuthenticator
	}
	if db == nil {
		return nil, ErrNilDB
	}

	caldavBackend, err := NewCalDAVBackend(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create CalDAV backend: %w", err)
	}

	carddavBackend, err := NewCardDAVBackend(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create CardDAV backend: %w", err)
	}

	return &Server{
		config:         cfg,
		authenticator:  authenticator,
		caldavBackend:  caldavBackend,
		carddavBackend: carddavBackend,
		logger:         slog.Default().With("component", "dav"),
	}, nil
}

// Start starts the DAV server
func (s *Server) Start(addr string, tlsConfig *tls.Config) error {
	mux := http.NewServeMux()

	// Root handler for discovery
	mux.HandleFunc("/", s.handleRoot)

	// Well-known redirects for auto-discovery
	mux.HandleFunc("/.well-known/caldav", s.wellKnownCalDAV)
	mux.HandleFunc("/.well-known/carddav", s.wellKnownCardDAV)

	// CalDAV endpoints
	mux.HandleFunc("/caldav/", s.handleCalDAV)
	mux.HandleFunc("/calendars/", s.handleCalDAV)

	// CardDAV endpoints
	mux.HandleFunc("/carddav/", s.handleCardDAV)
	mux.HandleFunc("/addressbooks/", s.handleCardDAV)

	// Principal endpoint (for user discovery)
	mux.HandleFunc("/principals/", s.handlePrincipal)

	s.httpServer = &http.Server{
		Addr:      addr,
		Handler:   s.authMiddleware(mux),
		TLSConfig: tlsConfig,
	}

	s.logger.Info("DAV server starting", "addr", addr)

	if tlsConfig != nil {
		return s.httpServer.ListenAndServeTLS("", "")
	}
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// authMiddleware handles HTTP Basic authentication
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow unauthenticated OPTIONS requests
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		// For well-known endpoints, try to authenticate if credentials provided
		// but allow pass-through if no credentials (will be handled by the endpoint)
		isWellKnown := strings.HasPrefix(r.URL.Path, "/.well-known/")

		username, password, ok := r.BasicAuth()
		if !ok {
			if isWellKnown {
				// Allow well-known without auth - handler will request if needed
				next.ServeHTTP(w, r)
				return
			}
			s.logger.Warn("DAV authentication failed: no credentials provided",
				"remote_addr", r.RemoteAddr)
			w.Header().Set("WWW-Authenticate", `Basic realm="Mail Server"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Apple Calendar/Contacts sends just the local part (e.g. "fenil") as username.
		// Append the primary domain to make it a full email for authentication.
		if !strings.Contains(username, "@") && s.config.Server.Domain != "" {
			username = username + "@" + s.config.Server.Domain
		}

		user, err := s.authenticator.Authenticate(r.Context(), username, password)
		if err != nil {
			// Don't log the actual error to prevent information disclosure
			s.logger.Warn("DAV authentication failed",
				"username", username,
				"remote_addr", r.RemoteAddr)
			w.Header().Set("WWW-Authenticate", `Basic realm="Mail Server"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		s.logger.Info("DAV authentication successful",
			"username", username,
			"remote_addr", r.RemoteAddr)

		// Store user in context
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type contextKey string

const userContextKey contextKey = "user"

func getUserFromContext(ctx context.Context) *auth.User {
	user, _ := ctx.Value(userContextKey).(*auth.User)
	return user
}

// safeReadBody reads the request body with size limit and ensures proper closure.
// Supports both Content-Length and chunked transfer encoding.
func safeReadBody(r *http.Request, maxSize int64) ([]byte, error) {
	// Use LimitReader to cap reads at maxSize+1 (to detect overflow)
	limitedReader := io.LimitReader(r.Body, maxSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	if int64(len(data)) > maxSize {
		return nil, fmt.Errorf("%w: body exceeds %d bytes", ErrRequestTooLarge, maxSize)
	}

	return data, nil
}

// escapeXML escapes user data for safe XML output
func escapeXML(s string) string {
	return html.EscapeString(s)
}

// escapeCDATA escapes data for safe inclusion inside XML CDATA sections.
// The sequence ]]> terminates CDATA, so it must be split across two CDATA sections.
func escapeCDATA(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}

// validatePath validates and sanitizes URL paths
func validatePath(path string) error {
	if path == "" {
		return errors.New("path cannot be empty")
	}
	// Check for path traversal attempts
	if strings.Contains(path, "..") {
		return errors.New("path traversal not allowed")
	}
	return nil
}

// extractCollectionUID extracts the collection UID from a DAV path.
// Returns empty string if the path points to the home collection.
// Paths: /calendars/user@domain/ → "", /calendars/user@domain/uid/ → "uid"
func extractCollectionUID(urlPath string) string {
	parts := strings.Split(strings.Trim(urlPath, "/"), "/")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

// reportRequest is used to parse REPORT XML bodies for multiget href extraction.
type reportRequest struct {
	XMLName xml.Name
	Hrefs   []string `xml:"DAV: href"`
}

// parseReportHrefs extracts href values from a REPORT request body.
// Returns nil if the body is empty or doesn't contain href elements.
func parseReportHrefs(body []byte) []string {
	if len(body) == 0 {
		return nil
	}
	var report reportRequest
	if err := xml.Unmarshal(body, &report); err != nil {
		return nil
	}
	if len(report.Hrefs) == 0 {
		return nil
	}
	return report.Hrefs
}

// isValidICalendar performs basic validation of iCalendar data
func isValidICalendar(data string) bool {
	if data == "" {
		return false
	}
	// Check for required iCalendar structure
	trimmed := strings.TrimSpace(data)
	return strings.HasPrefix(trimmed, "BEGIN:VCALENDAR") &&
		strings.HasSuffix(trimmed, "END:VCALENDAR")
}

// isValidVCard performs basic validation of vCard data
func isValidVCard(data string) bool {
	if data == "" {
		return false
	}
	// Check for required vCard structure
	trimmed := strings.TrimSpace(data)
	return strings.HasPrefix(trimmed, "BEGIN:VCARD") &&
		strings.HasSuffix(trimmed, "END:VCARD")
}

// wellKnownCalDAV handles CalDAV auto-discovery
func (s *Server) wellKnownCalDAV(w http.ResponseWriter, r *http.Request) {
	// For PROPFIND requests (used by clients for discovery), proxy to CalDAV handler
	if r.Method == "PROPFIND" {
		user := getUserFromContext(r.Context())
		if user == nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Mail Server"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		s.handleCalDAVPropfind(w, r, user)
		return
	}
	// For other methods, redirect to the CalDAV endpoint
	http.Redirect(w, r, "/caldav/", http.StatusTemporaryRedirect)
}

// wellKnownCardDAV handles CardDAV auto-discovery
func (s *Server) wellKnownCardDAV(w http.ResponseWriter, r *http.Request) {
	// For PROPFIND requests (used by clients for discovery), proxy to CardDAV handler
	if r.Method == "PROPFIND" {
		user := getUserFromContext(r.Context())
		if user == nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Mail Server"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		s.handleCardDAVPropfind(w, r, user)
		return
	}
	// For other methods, redirect to the CardDAV endpoint
	http.Redirect(w, r, "/carddav/", http.StatusTemporaryRedirect)
}

// handleRoot handles requests to the root path for DAV discovery
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	// Only handle exact root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case "OPTIONS":
		// Return DAV capabilities for discovery
		w.Header().Set("Allow", "OPTIONS, PROPFIND")
		w.Header().Set("DAV", "1, 2, 3, addressbook, calendar-access")
		w.WriteHeader(http.StatusOK)

	case "PROPFIND":
		user := getUserFromContext(r.Context())
		if user == nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Mail Server"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Return principal discovery response
		principalURL := fmt.Sprintf("/principals/%s/", escapeXML(user.Email))
		calendarHomeURL := fmt.Sprintf("/calendars/%s/", escapeXML(user.Email))
		addressbookHomeURL := fmt.Sprintf("/addressbooks/%s/", escapeXML(user.Email))

		response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:A="urn:ietf:params:xml:ns:carddav" xmlns:CS="http://calendarserver.org/ns/">
  <D:response>
    <D:href>/</D:href>
    <D:propstat>
      <D:prop>
        <D:current-user-principal>
          <D:href>%s</D:href>
        </D:current-user-principal>
        <C:calendar-home-set>
          <D:href>%s</D:href>
        </C:calendar-home-set>
        <A:addressbook-home-set>
          <D:href>%s</D:href>
        </A:addressbook-home-set>
        <D:resourcetype>
          <D:collection/>
        </D:resourcetype>
        <D:principal-URL>
          <D:href>%s</D:href>
        </D:principal-URL>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`, principalURL, calendarHomeURL, addressbookHomeURL, principalURL)

		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(response))

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePrincipal handles principal discovery requests
func (s *Server) handlePrincipal(w http.ResponseWriter, r *http.Request) {
	// Handle OPTIONS without authentication (Apple Calendar sends OPTIONS to principal URL during discovery)
	if r.Method == "OPTIONS" {
		w.Header().Set("Allow", "OPTIONS, PROPFIND")
		w.Header().Set("DAV", "1, 2, 3, addressbook, calendar-access")
		w.WriteHeader(http.StatusOK)
		return
	}

	user := getUserFromContext(r.Context())
	if user == nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="Mail Server"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "PROPFIND":
		s.handlePrincipalPropfind(w, r, user)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePrincipalPropfind responds to PROPFIND on principal
func (s *Server) handlePrincipalPropfind(w http.ResponseWriter, r *http.Request, user *auth.User) {
	principalURL := fmt.Sprintf("/principals/%s/", escapeXML(user.Email))
	calendarHomeURL := fmt.Sprintf("/calendars/%s/", escapeXML(user.Email))
	addressbookHomeURL := fmt.Sprintf("/addressbooks/%s/", escapeXML(user.Email))

	displayName := escapeXML(user.DisplayName)
	if displayName == "" {
		displayName = escapeXML(user.Email)
	}

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:A="urn:ietf:params:xml:ns:carddav" xmlns:CS="http://calendarserver.org/ns/">
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:displayname>%s</D:displayname>
        <D:resourcetype>
          <D:principal/>
          <D:collection/>
        </D:resourcetype>
        <C:calendar-home-set>
          <D:href>%s</D:href>
        </C:calendar-home-set>
        <A:addressbook-home-set>
          <D:href>%s</D:href>
        </A:addressbook-home-set>
        <D:current-user-principal>
          <D:href>%s</D:href>
        </D:current-user-principal>
        <C:calendar-user-address-set>
          <D:href>mailto:%s</D:href>
        </C:calendar-user-address-set>
        <D:principal-URL>
          <D:href>%s</D:href>
        </D:principal-URL>
        <CS:calendar-proxy-read-for/>
        <CS:calendar-proxy-write-for/>
        <D:supported-report-set>
          <D:supported-report><D:report><C:calendar-multiget/></D:report></D:supported-report>
          <D:supported-report><D:report><C:calendar-query/></D:report></D:supported-report>
          <D:supported-report><D:report><A:addressbook-multiget/></D:report></D:supported-report>
          <D:supported-report><D:report><A:addressbook-query/></D:report></D:supported-report>
        </D:supported-report-set>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`, principalURL, displayName, calendarHomeURL, addressbookHomeURL, principalURL,
		escapeXML(user.Email), principalURL)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(response))
}

// extractPathUser extracts the user email from a path like /calendars/user@domain/...
func extractPathUser(path, prefix string) string {
	// Only extract if the path actually starts with the prefix
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	path = strings.TrimPrefix(path, prefix)
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

// handleCalDAV handles CalDAV requests
func (s *Server) handleCalDAV(w http.ResponseWriter, r *http.Request) {
	// Handle OPTIONS without authentication (for CORS and discovery)
	if r.Method == "OPTIONS" {
		s.handleCalDAVOptions(w, r)
		return
	}

	user := getUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check if path user matches authenticated user (for paths like /calendars/user@domain/...)
	pathUser := extractPathUser(r.URL.Path, "/calendars")
	if pathUser == "" {
		pathUser = extractPathUser(r.URL.Path, "/caldav")
	}
	if pathUser != "" && pathUser != user.Email {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	switch r.Method {
	case "PROPFIND":
		s.handleCalDAVPropfind(w, r, user)
	case "REPORT":
		s.handleCalDAVReport(w, r, user)
	case "GET":
		s.handleCalDAVGet(w, r, user)
	case "PUT":
		s.handleCalDAVPut(w, r, user)
	case "DELETE":
		s.handleCalDAVDelete(w, r, user)
	case "MKCALENDAR":
		s.handleMkCalendar(w, r, user)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCalDAVOptions returns supported methods
func (s *Server) handleCalDAVOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "OPTIONS, GET, PUT, DELETE, PROPFIND, REPORT, MKCALENDAR")
	w.Header().Set("DAV", "1, 2, 3, calendar-access")
	w.WriteHeader(http.StatusOK)
}

// handleCalDAVPropfind handles PROPFIND for calendars.
// Supports depth-aware and path-aware responses:
//   - /calendars/user@domain/          → calendar home (Depth:0 = home only, Depth:1 = home + calendars)
//   - /calendars/user@domain/caluid/   → specific calendar (Depth:0 = calendar props, Depth:1 = props + event entries)
func (s *Server) handleCalDAVPropfind(w http.ResponseWriter, r *http.Request, user *auth.User) {
	ctx := r.Context()
	depth := r.Header.Get("Depth")
	if depth == "" {
		depth = "infinity"
	}

	collectionUID := extractCollectionUID(r.URL.Path)

	if collectionUID != "" {
		s.propfindCalendar(w, ctx, user, collectionUID, depth)
	} else {
		s.propfindCalendarHome(w, ctx, user, depth)
	}
}

// propfindCalendarHome returns PROPFIND response for the calendar home collection
func (s *Server) propfindCalendarHome(w http.ResponseWriter, ctx context.Context, user *auth.User, depth string) {
	homeURL := fmt.Sprintf("/calendars/%s/", user.Email)
	principalURL := fmt.Sprintf("/principals/%s/", user.Email)

	var responses strings.Builder
	responses.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:CS="http://calendarserver.org/ns/">`)

	// Calendar home
	responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype>
          <D:collection/>
        </D:resourcetype>
        <D:displayname>Calendars</D:displayname>
        <D:current-user-principal>
          <D:href>%s</D:href>
        </D:current-user-principal>
        <C:calendar-home-set>
          <D:href>%s</D:href>
        </C:calendar-home-set>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, homeURL, principalURL, homeURL))

	// At Depth 1 or infinity, include individual calendars
	if depth == "1" || depth == "infinity" {
		calendars, err := s.caldavBackend.ListCalendars(ctx, user.ID)
		if err != nil {
			s.logger.Error("DAV internal error", "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		for _, cal := range calendars {
			calURL := fmt.Sprintf("/calendars/%s/%s/", user.Email, cal.UID)
			responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype>
          <D:collection/>
          <C:calendar/>
        </D:resourcetype>
        <D:displayname>%s</D:displayname>
        <CS:getctag>%s</CS:getctag>
        <C:calendar-description>%s</C:calendar-description>
        <C:supported-calendar-component-set>
          <C:comp name="VEVENT"/>
          <C:comp name="VTODO"/>
        </C:supported-calendar-component-set>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, calURL, escapeXML(cal.Name), escapeXML(cal.CTag), escapeXML(cal.Description)))
		}
	}

	responses.WriteString(`
</D:multistatus>`)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write([]byte(responses.String()))
}

// propfindCalendar returns PROPFIND response for a specific calendar
func (s *Server) propfindCalendar(w http.ResponseWriter, ctx context.Context, user *auth.User, calendarUID, depth string) {
	cal, err := s.caldavBackend.GetCalendar(ctx, calendarUID)
	if err != nil {
		http.Error(w, "Calendar not found", http.StatusNotFound)
		return
	}
	if cal.UserID != user.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	calURL := fmt.Sprintf("/calendars/%s/%s/", user.Email, cal.UID)

	var responses strings.Builder
	responses.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:CS="http://calendarserver.org/ns/">`)

	// Calendar collection properties
	responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype>
          <D:collection/>
          <C:calendar/>
        </D:resourcetype>
        <D:displayname>%s</D:displayname>
        <CS:getctag>%s</CS:getctag>
        <C:calendar-description>%s</C:calendar-description>
        <C:supported-calendar-component-set>
          <C:comp name="VEVENT"/>
          <C:comp name="VTODO"/>
        </C:supported-calendar-component-set>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, calURL, escapeXML(cal.Name), escapeXML(cal.CTag), escapeXML(cal.Description)))

	// At Depth 1, include event entries with ETags
	if depth == "1" || depth == "infinity" {
		events, err := s.caldavBackend.ListEvents(ctx, calendarUID)
		if err != nil {
			s.logger.Error("DAV internal error", "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		for _, event := range events {
			eventURL := fmt.Sprintf("/calendars/%s/%s/%s.ics", user.Email, calendarUID, event.UID)
			responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:getetag>%s</D:getetag>
        <D:getcontenttype>text/calendar; charset=utf-8</D:getcontenttype>
        <D:resourcetype/>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, eventURL, escapeXML(event.ETag)))
		}
	}

	responses.WriteString(`
</D:multistatus>`)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(responses.String()))
}

// handleCalDAVReport handles REPORT requests for calendar queries.
// Supports calendar-multiget (fetch specific resources by href) and
// calendar-query (return all events).
func (s *Server) handleCalDAVReport(w http.ResponseWriter, r *http.Request, user *auth.User) {
	calendarUID := extractCollectionUID(r.URL.Path)
	if calendarUID == "" {
		http.Error(w, "Invalid path: calendar UID required", http.StatusBadRequest)
		return
	}

	// Verify the calendar belongs to the authenticated user
	cal, err := s.caldavBackend.GetCalendar(r.Context(), calendarUID)
	if err != nil {
		http.Error(w, "Calendar not found", http.StatusNotFound)
		return
	}
	if cal.UserID != user.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Read request body for multiget hrefs
	body, err := safeReadBody(r, maxRequestBodySize)
	if err != nil {
		s.logger.Warn("Failed to read REPORT body", "error", err.Error())
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	requestedHrefs := parseReportHrefs(body)

	ctx := r.Context()

	var events []*CalendarEvent
	if len(requestedHrefs) > 0 {
		// Multiget: fetch only requested resources
		for _, href := range requestedHrefs {
			hrefParts := strings.Split(strings.Trim(href, "/"), "/")
			if len(hrefParts) == 0 {
				continue
			}
			eventUID := strings.TrimSuffix(hrefParts[len(hrefParts)-1], ".ics")
			if eventUID == "" {
				continue
			}
			event, err := s.caldavBackend.GetEvent(ctx, calendarUID, eventUID)
			if err != nil {
				continue // skip missing events
			}
			events = append(events, event)
		}
	} else {
		// Calendar-query: return all events
		var err error
		events, err = s.caldavBackend.ListEvents(ctx, calendarUID)
		if err != nil {
			s.logger.Error("DAV internal error", "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	var responses strings.Builder
	responses.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">`)

	for _, event := range events {
		eventURL := fmt.Sprintf("/calendars/%s/%s/%s.ics", user.Email, calendarUID, event.UID)
		responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:getetag>%s</D:getetag>
        <C:calendar-data><![CDATA[%s]]></C:calendar-data>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, eventURL, escapeXML(event.ETag), escapeCDATA(event.ICalendarData)))
	}

	responses.WriteString(`
</D:multistatus>`)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(responses.String()))
}

// handleCalDAVGet returns an event's iCalendar data
func (s *Server) handleCalDAVGet(w http.ResponseWriter, r *http.Request, user *auth.User) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	calendarUID := parts[len(parts)-2]
	eventUID := strings.TrimSuffix(parts[len(parts)-1], ".ics")

	ctx := r.Context()
	event, err := s.caldavBackend.GetEvent(ctx, calendarUID, eventUID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("ETag", event.ETag)
	w.Write([]byte(event.ICalendarData))
}

// handleCalDAVPut creates or updates an event
func (s *Server) handleCalDAVPut(w http.ResponseWriter, r *http.Request, user *auth.User) {
	// Validate path
	if err := validatePath(r.URL.Path); err != nil {
		http.Error(w, fmt.Sprintf("Invalid path: %v", err), http.StatusBadRequest)
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	calendarUID := parts[len(parts)-2]
	eventUID := strings.TrimSuffix(parts[len(parts)-1], ".ics")

	// Read body safely with size limit
	data, err := safeReadBody(r, maxRequestBodySize)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}
	icalData := string(data)

	// Validate iCalendar data format
	if !isValidICalendar(icalData) {
		http.Error(w, "Invalid iCalendar data", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Try to get existing event
	existing, _ := s.caldavBackend.GetEvent(ctx, calendarUID, eventUID)

	// Check If-Match header for conditional updates
	ifMatch := r.Header.Get("If-Match")
	if ifMatch != "" && existing != nil {
		if ifMatch != existing.ETag && ifMatch != "*" {
			http.Error(w, "Precondition failed", http.StatusPreconditionFailed)
			return
		}
	}

	// Check If-None-Match header (prevent overwriting)
	ifNoneMatch := r.Header.Get("If-None-Match")
	if ifNoneMatch == "*" && existing != nil {
		http.Error(w, "Resource already exists", http.StatusPreconditionFailed)
		return
	}

	event := &CalendarEvent{
		UID:           eventUID,
		ICalendarData: icalData,
	}

	var updateErr error
	if existing != nil {
		updateErr = s.caldavBackend.UpdateEvent(ctx, calendarUID, event)
	} else {
		updateErr = s.caldavBackend.CreateEvent(ctx, calendarUID, event)
	}

	if updateErr != nil {
		s.logger.Error("DAV update failed", "error", updateErr.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get updated event for ETag
	updated, _ := s.caldavBackend.GetEvent(ctx, calendarUID, eventUID)
	if updated != nil {
		w.Header().Set("ETag", updated.ETag)
	}

	if existing != nil {
		w.WriteHeader(http.StatusNoContent)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
}

// handleCalDAVDelete removes an event
func (s *Server) handleCalDAVDelete(w http.ResponseWriter, r *http.Request, user *auth.User) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	calendarUID := parts[len(parts)-2]
	eventUID := strings.TrimSuffix(parts[len(parts)-1], ".ics")

	ctx := r.Context()
	err := s.caldavBackend.DeleteEvent(ctx, calendarUID, eventUID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleMkCalendar creates a new calendar
func (s *Server) handleMkCalendar(w http.ResponseWriter, r *http.Request, user *auth.User) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	calName := "New Calendar"
	if len(parts) >= 2 {
		calName = parts[len(parts)-1]
	}

	ctx := r.Context()
	_, err := s.caldavBackend.CreateCalendar(ctx, user.ID, calName, "")
	if err != nil {
		s.logger.Error("DAV internal error", "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// handleCardDAV handles CardDAV requests
func (s *Server) handleCardDAV(w http.ResponseWriter, r *http.Request) {
	// Handle OPTIONS without authentication (for CORS and discovery)
	if r.Method == "OPTIONS" {
		s.handleCardDAVOptions(w, r)
		return
	}

	user := getUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check if path user matches authenticated user (for paths like /addressbooks/user@domain/...)
	pathUser := extractPathUser(r.URL.Path, "/addressbooks")
	if pathUser == "" {
		pathUser = extractPathUser(r.URL.Path, "/carddav")
	}
	if pathUser != "" && pathUser != user.Email {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	switch r.Method {
	case "PROPFIND":
		s.handleCardDAVPropfind(w, r, user)
	case "REPORT":
		s.handleCardDAVReport(w, r, user)
	case "GET":
		s.handleCardDAVGet(w, r, user)
	case "PUT":
		s.handleCardDAVPut(w, r, user)
	case "DELETE":
		s.handleCardDAVDelete(w, r, user)
	case "MKCOL":
		s.handleMkAddressBook(w, r, user)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCardDAVOptions returns supported methods
func (s *Server) handleCardDAVOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "OPTIONS, GET, PUT, DELETE, PROPFIND, REPORT, MKCOL")
	w.Header().Set("DAV", "1, 2, 3, addressbook")
	w.WriteHeader(http.StatusOK)
}

// handleCardDAVPropfind handles PROPFIND for address books.
// Supports depth-aware and path-aware responses:
//   - /addressbooks/user@domain/        → address book home (Depth:0 = home only, Depth:1 = home + books)
//   - /addressbooks/user@domain/abuid/  → specific book (Depth:0 = book props, Depth:1 = props + contact entries)
func (s *Server) handleCardDAVPropfind(w http.ResponseWriter, r *http.Request, user *auth.User) {
	ctx := r.Context()
	depth := r.Header.Get("Depth")
	if depth == "" {
		depth = "infinity"
	}

	collectionUID := extractCollectionUID(r.URL.Path)

	if collectionUID != "" {
		s.propfindAddressBook(w, ctx, user, collectionUID, depth)
	} else {
		s.propfindAddressBookHome(w, ctx, user, depth)
	}
}

// propfindAddressBookHome returns PROPFIND response for the address book home collection
func (s *Server) propfindAddressBookHome(w http.ResponseWriter, ctx context.Context, user *auth.User, depth string) {
	homeURL := fmt.Sprintf("/addressbooks/%s/", user.Email)
	principalURL := fmt.Sprintf("/principals/%s/", user.Email)

	var responses strings.Builder
	responses.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:A="urn:ietf:params:xml:ns:carddav" xmlns:CS="http://calendarserver.org/ns/">`)

	// Address book home
	responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype>
          <D:collection/>
        </D:resourcetype>
        <D:displayname>Address Books</D:displayname>
        <D:current-user-principal>
          <D:href>%s</D:href>
        </D:current-user-principal>
        <A:addressbook-home-set>
          <D:href>%s</D:href>
        </A:addressbook-home-set>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, homeURL, principalURL, homeURL))

	// At Depth 1 or infinity, include individual address books
	if depth == "1" || depth == "infinity" {
		addressBooks, err := s.carddavBackend.ListAddressBooks(ctx, user.ID)
		if err != nil {
			s.logger.Error("DAV internal error", "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		for _, ab := range addressBooks {
			abURL := fmt.Sprintf("/addressbooks/%s/%s/", user.Email, ab.UID)
			responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype>
          <D:collection/>
          <A:addressbook/>
        </D:resourcetype>
        <D:displayname>%s</D:displayname>
        <CS:getctag>%s</CS:getctag>
        <A:addressbook-description>%s</A:addressbook-description>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, abURL, escapeXML(ab.Name), escapeXML(ab.CTag), escapeXML(ab.Description)))
		}
	}

	responses.WriteString(`
</D:multistatus>`)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	_, _ = w.Write([]byte(responses.String()))
}

// propfindAddressBook returns PROPFIND response for a specific address book
func (s *Server) propfindAddressBook(w http.ResponseWriter, ctx context.Context, user *auth.User, addressBookUID, depth string) {
	ab, err := s.carddavBackend.GetAddressBook(ctx, addressBookUID)
	if err != nil {
		http.Error(w, "Address book not found", http.StatusNotFound)
		return
	}
	if ab.UserID != user.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	abURL := fmt.Sprintf("/addressbooks/%s/%s/", user.Email, ab.UID)

	var responses strings.Builder
	responses.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:A="urn:ietf:params:xml:ns:carddav" xmlns:CS="http://calendarserver.org/ns/">`)

	// Address book collection properties
	responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype>
          <D:collection/>
          <A:addressbook/>
        </D:resourcetype>
        <D:displayname>%s</D:displayname>
        <CS:getctag>%s</CS:getctag>
        <A:addressbook-description>%s</A:addressbook-description>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, abURL, escapeXML(ab.Name), escapeXML(ab.CTag), escapeXML(ab.Description)))

	// At Depth 1, include contact entries with ETags
	if depth == "1" || depth == "infinity" {
		contacts, err := s.carddavBackend.ListContacts(ctx, addressBookUID)
		if err != nil {
			s.logger.Error("DAV internal error", "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		for _, contact := range contacts {
			contactURL := fmt.Sprintf("/addressbooks/%s/%s/%s.vcf", user.Email, addressBookUID, contact.UID)
			responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:getetag>%s</D:getetag>
        <D:getcontenttype>text/vcard; charset=utf-8</D:getcontenttype>
        <D:resourcetype/>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, contactURL, escapeXML(contact.ETag)))
		}
	}

	responses.WriteString(`
</D:multistatus>`)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(responses.String()))
}

// handleCardDAVReport handles REPORT requests for address book queries.
// Supports addressbook-multiget (fetch specific resources by href) and
// addressbook-query (return all contacts).
func (s *Server) handleCardDAVReport(w http.ResponseWriter, r *http.Request, user *auth.User) {
	addressBookUID := extractCollectionUID(r.URL.Path)
	if addressBookUID == "" {
		http.Error(w, "Invalid path: address book UID required", http.StatusBadRequest)
		return
	}

	// Verify the address book belongs to the authenticated user
	ab, err := s.carddavBackend.GetAddressBook(r.Context(), addressBookUID)
	if err != nil {
		http.Error(w, "Address book not found", http.StatusNotFound)
		return
	}
	if ab.UserID != user.ID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Read request body for multiget hrefs
	body, err := safeReadBody(r, maxRequestBodySize)
	if err != nil {
		s.logger.Warn("Failed to read REPORT body", "error", err.Error())
		http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	requestedHrefs := parseReportHrefs(body)

	ctx := r.Context()

	var contacts []*Contact
	if len(requestedHrefs) > 0 {
		// Multiget: fetch only requested resources
		for _, href := range requestedHrefs {
			hrefParts := strings.Split(strings.Trim(href, "/"), "/")
			if len(hrefParts) == 0 {
				continue
			}
			contactUID := strings.TrimSuffix(hrefParts[len(hrefParts)-1], ".vcf")
			if contactUID == "" {
				continue
			}
			contact, err := s.carddavBackend.GetContact(ctx, addressBookUID, contactUID)
			if err != nil {
				continue // skip missing contacts
			}
			contacts = append(contacts, contact)
		}
	} else {
		// Addressbook-query: return all contacts
		var err error
		contacts, err = s.carddavBackend.ListContacts(ctx, addressBookUID)
		if err != nil {
			s.logger.Error("DAV internal error", "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	var responses strings.Builder
	responses.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:A="urn:ietf:params:xml:ns:carddav">`)

	for _, contact := range contacts {
		contactURL := fmt.Sprintf("/addressbooks/%s/%s/%s.vcf", user.Email, addressBookUID, contact.UID)
		responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:getetag>%s</D:getetag>
        <A:address-data><![CDATA[%s]]></A:address-data>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, contactURL, escapeXML(contact.ETag), escapeCDATA(contact.VCardData)))
	}

	responses.WriteString(`
</D:multistatus>`)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(responses.String()))
}

// handleCardDAVGet returns a contact's vCard data
func (s *Server) handleCardDAVGet(w http.ResponseWriter, r *http.Request, user *auth.User) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	addressBookUID := parts[len(parts)-2]
	contactUID := strings.TrimSuffix(parts[len(parts)-1], ".vcf")

	ctx := r.Context()
	contact, err := s.carddavBackend.GetContact(ctx, addressBookUID, contactUID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
	w.Header().Set("ETag", contact.ETag)
	w.Write([]byte(contact.VCardData))
}

// handleCardDAVPut creates or updates a contact
func (s *Server) handleCardDAVPut(w http.ResponseWriter, r *http.Request, user *auth.User) {
	// Validate path
	if err := validatePath(r.URL.Path); err != nil {
		http.Error(w, fmt.Sprintf("Invalid path: %v", err), http.StatusBadRequest)
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	addressBookUID := parts[len(parts)-2]
	contactUID := strings.TrimSuffix(parts[len(parts)-1], ".vcf")

	// Read body safely with size limit
	data, err := safeReadBody(r, maxRequestBodySize)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}
	vcardData := string(data)

	// Validate vCard data format
	if !isValidVCard(vcardData) {
		http.Error(w, "Invalid vCard data", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Try to get existing contact
	existing, _ := s.carddavBackend.GetContact(ctx, addressBookUID, contactUID)

	// Check If-Match header for conditional updates
	ifMatch := r.Header.Get("If-Match")
	if ifMatch != "" && existing != nil {
		if ifMatch != existing.ETag && ifMatch != "*" {
			http.Error(w, "Precondition failed", http.StatusPreconditionFailed)
			return
		}
	}

	// Check If-None-Match header (prevent overwriting)
	ifNoneMatch := r.Header.Get("If-None-Match")
	if ifNoneMatch == "*" && existing != nil {
		http.Error(w, "Resource already exists", http.StatusPreconditionFailed)
		return
	}

	contact := &Contact{
		UID:       contactUID,
		VCardData: vcardData,
	}

	var updateErr error
	if existing != nil {
		updateErr = s.carddavBackend.UpdateContact(ctx, addressBookUID, contact)
	} else {
		updateErr = s.carddavBackend.CreateContact(ctx, addressBookUID, contact)
	}

	if updateErr != nil {
		s.logger.Error("DAV update failed", "error", updateErr.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get updated contact for ETag
	updated, _ := s.carddavBackend.GetContact(ctx, addressBookUID, contactUID)
	if updated != nil {
		w.Header().Set("ETag", updated.ETag)
	}

	if existing != nil {
		w.WriteHeader(http.StatusNoContent)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
}

// handleCardDAVDelete removes a contact
func (s *Server) handleCardDAVDelete(w http.ResponseWriter, r *http.Request, user *auth.User) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	addressBookUID := parts[len(parts)-2]
	contactUID := strings.TrimSuffix(parts[len(parts)-1], ".vcf")

	ctx := r.Context()
	err := s.carddavBackend.DeleteContact(ctx, addressBookUID, contactUID)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleMkAddressBook creates a new address book
func (s *Server) handleMkAddressBook(w http.ResponseWriter, r *http.Request, user *auth.User) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	abName := "Contacts"
	if len(parts) >= 2 {
		abName = parts[len(parts)-1]
	}

	ctx := r.Context()
	_, err := s.carddavBackend.CreateAddressBook(ctx, user.ID, abName, "")
	if err != nil {
		s.logger.Error("DAV internal error", "error", err.Error())
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
