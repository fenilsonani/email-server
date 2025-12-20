package dav

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/config"
)

// Server handles CalDAV and CardDAV requests
type Server struct {
	config        *config.Config
	authenticator *auth.Authenticator
	caldavBackend *CalDAVBackend
	carddavBackend *CardDAVBackend
	httpServer    *http.Server
}

// NewServer creates a new DAV server
func NewServer(cfg *config.Config, authenticator *auth.Authenticator, db *sql.DB) *Server {
	return &Server{
		config:         cfg,
		authenticator:  authenticator,
		caldavBackend:  NewCalDAVBackend(db),
		carddavBackend: NewCardDAVBackend(db),
	}
}

// Start starts the DAV server
func (s *Server) Start(addr string, tlsConfig *tls.Config) error {
	mux := http.NewServeMux()

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

	log.Printf("DAV server starting on %s", addr)

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
		// Allow unauthenticated access to well-known endpoints
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			next.ServeHTTP(w, r)
			return
		}

		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Mail Server"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		user, err := s.authenticator.Authenticate(r.Context(), username, password)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Mail Server"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

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

// wellKnownCalDAV handles CalDAV auto-discovery
func (s *Server) wellKnownCalDAV(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/caldav/", http.StatusMovedPermanently)
}

// wellKnownCardDAV handles CardDAV auto-discovery
func (s *Server) wellKnownCardDAV(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/carddav/", http.StatusMovedPermanently)
}

// handlePrincipal handles principal discovery requests
func (s *Server) handlePrincipal(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r.Context())
	if user == nil {
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
	principalURL := fmt.Sprintf("/principals/%s/", user.Email)
	calendarHomeURL := fmt.Sprintf("/calendars/%s/", user.Email)
	addressbookHomeURL := fmt.Sprintf("/addressbooks/%s/", user.Email)

	response := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:A="urn:ietf:params:xml:ns:carddav">
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
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`, principalURL, user.DisplayName, calendarHomeURL, addressbookHomeURL, principalURL)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(response))
}

// handleCalDAV handles CalDAV requests
func (s *Server) handleCalDAV(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "OPTIONS":
		s.handleCalDAVOptions(w, r)
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

// handleCalDAVPropfind handles PROPFIND for calendars
func (s *Server) handleCalDAVPropfind(w http.ResponseWriter, r *http.Request, user *auth.User) {
	ctx := r.Context()
	calendars, err := s.caldavBackend.ListCalendars(ctx, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Build response
	var responses strings.Builder
	responses.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:CS="http://calendarserver.org/ns/">`)

	// Calendar home
	homeURL := fmt.Sprintf("/calendars/%s/", user.Email)
	responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype>
          <D:collection/>
        </D:resourcetype>
        <D:displayname>Calendars</D:displayname>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, homeURL))

	// Each calendar
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
  </D:response>`, calURL, cal.Name, cal.CTag, cal.Description))
	}

	responses.WriteString(`
</D:multistatus>`)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(responses.String()))
}

// handleCalDAVReport handles REPORT requests for calendar queries
func (s *Server) handleCalDAVReport(w http.ResponseWriter, r *http.Request, user *auth.User) {
	// Parse path to get calendar UID
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	calendarUID := parts[len(parts)-1]
	if calendarUID == "" && len(parts) >= 2 {
		calendarUID = parts[len(parts)-2]
	}

	ctx := r.Context()
	events, err := s.caldavBackend.ListEvents(ctx, calendarUID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
        <C:calendar-data>%s</C:calendar-data>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, eventURL, event.ETag, event.ICalendarData))
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("ETag", event.ETag)
	w.Write([]byte(event.ICalendarData))
}

// handleCalDAVPut creates or updates an event
func (s *Server) handleCalDAVPut(w http.ResponseWriter, r *http.Request, user *auth.User) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	calendarUID := parts[len(parts)-2]
	eventUID := strings.TrimSuffix(parts[len(parts)-1], ".ics")

	// Read body
	buf := make([]byte, r.ContentLength)
	r.Body.Read(buf)
	icalData := string(buf)

	ctx := r.Context()

	// Try to get existing event
	existing, _ := s.caldavBackend.GetEvent(ctx, calendarUID, eventUID)

	event := &CalendarEvent{
		UID:           eventUID,
		ICalendarData: icalData,
	}

	var err error
	if existing != nil {
		err = s.caldavBackend.UpdateEvent(ctx, calendarUID, event)
	} else {
		err = s.caldavBackend.CreateEvent(ctx, calendarUID, event)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusNotFound)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// handleCardDAV handles CardDAV requests
func (s *Server) handleCardDAV(w http.ResponseWriter, r *http.Request) {
	user := getUserFromContext(r.Context())
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case "OPTIONS":
		s.handleCardDAVOptions(w, r)
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

// handleCardDAVPropfind handles PROPFIND for address books
func (s *Server) handleCardDAVPropfind(w http.ResponseWriter, r *http.Request, user *auth.User) {
	ctx := r.Context()
	addressBooks, err := s.carddavBackend.ListAddressBooks(ctx, user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var responses strings.Builder
	responses.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<D:multistatus xmlns:D="DAV:" xmlns:A="urn:ietf:params:xml:ns:carddav" xmlns:CS="http://calendarserver.org/ns/">`)

	// Address book home
	homeURL := fmt.Sprintf("/addressbooks/%s/", user.Email)
	responses.WriteString(fmt.Sprintf(`
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop>
        <D:resourcetype>
          <D:collection/>
        </D:resourcetype>
        <D:displayname>Address Books</D:displayname>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, homeURL))

	// Each address book
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
  </D:response>`, abURL, ab.Name, ab.CTag, ab.Description))
	}

	responses.WriteString(`
</D:multistatus>`)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(responses.String()))
}

// handleCardDAVReport handles REPORT requests for address book queries
func (s *Server) handleCardDAVReport(w http.ResponseWriter, r *http.Request, user *auth.User) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	addressBookUID := parts[len(parts)-1]
	if addressBookUID == "" && len(parts) >= 2 {
		addressBookUID = parts[len(parts)-2]
	}

	ctx := r.Context()
	contacts, err := s.carddavBackend.ListContacts(ctx, addressBookUID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
        <A:address-data>%s</A:address-data>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>`, contactURL, contact.ETag, contact.VCardData))
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/vcard; charset=utf-8")
	w.Header().Set("ETag", contact.ETag)
	w.Write([]byte(contact.VCardData))
}

// handleCardDAVPut creates or updates a contact
func (s *Server) handleCardDAVPut(w http.ResponseWriter, r *http.Request, user *auth.User) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	addressBookUID := parts[len(parts)-2]
	contactUID := strings.TrimSuffix(parts[len(parts)-1], ".vcf")

	// Read body
	buf := make([]byte, r.ContentLength)
	r.Body.Read(buf)
	vcardData := string(buf)

	ctx := r.Context()

	// Try to get existing contact
	existing, _ := s.carddavBackend.GetContact(ctx, addressBookUID, contactUID)

	contact := &Contact{
		UID:       contactUID,
		VCardData: vcardData,
	}

	var err error
	if existing != nil {
		err = s.carddavBackend.UpdateContact(ctx, addressBookUID, contact)
	} else {
		err = s.carddavBackend.CreateContact(ctx, addressBookUID, contact)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusNotFound)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
