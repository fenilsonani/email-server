package smtp

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/fenilsonani/email-server/internal/config"
)

// Default connection limits
const (
	defaultMaxConnections      = 1000
	defaultMaxConnectionsPerIP = 50
)

// limitedListener wraps a net.Listener with connection limits
type limitedListener struct {
	net.Listener
	maxConns      int
	maxConnsPerIP int
	currentConns  int64
	perIPConns    map[string]int
	perIPMu       sync.Mutex
	sem           chan struct{}
}

// newLimitedListener creates a connection-limiting listener
func newLimitedListener(l net.Listener, maxConns, maxConnsPerIP int) *limitedListener {
	if maxConns <= 0 {
		maxConns = defaultMaxConnections
	}
	if maxConnsPerIP <= 0 {
		maxConnsPerIP = defaultMaxConnectionsPerIP
	}
	return &limitedListener{
		Listener:      l,
		maxConns:      maxConns,
		maxConnsPerIP: maxConnsPerIP,
		perIPConns:    make(map[string]int),
		sem:           make(chan struct{}, maxConns),
	}
}

func (l *limitedListener) Accept() (net.Conn, error) {
	// Acquire global semaphore
	l.sem <- struct{}{}

	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.sem // Release on error
		return nil, err
	}

	// Check per-IP limit
	ip := extractIP(conn.RemoteAddr())
	l.perIPMu.Lock()
	if l.perIPConns[ip] >= l.maxConnsPerIP {
		l.perIPMu.Unlock()
		<-l.sem // Release global semaphore
		conn.Close()
		log.Printf("SMTP: Rejected connection from %s: per-IP limit exceeded", ip)
		return l.Accept() // Try accepting another connection
	}
	l.perIPConns[ip]++
	l.perIPMu.Unlock()

	atomic.AddInt64(&l.currentConns, 1)

	return &limitedConn{
		Conn:     conn,
		listener: l,
		ip:       ip,
	}, nil
}

// limitedConn tracks connection lifecycle for proper cleanup
type limitedConn struct {
	net.Conn
	listener *limitedListener
	ip       string
	closed   int32
}

func (c *limitedConn) Close() error {
	if atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		c.listener.perIPMu.Lock()
		c.listener.perIPConns[c.ip]--
		if c.listener.perIPConns[c.ip] <= 0 {
			delete(c.listener.perIPConns, c.ip)
		}
		c.listener.perIPMu.Unlock()

		atomic.AddInt64(&c.listener.currentConns, -1)
		<-c.listener.sem // Release global semaphore
	}
	return c.Conn.Close()
}

// extractIP extracts the IP address from a net.Addr
func extractIP(addr net.Addr) string {
	if addr == nil {
		return "unknown"
	}
	switch a := addr.(type) {
	case *net.TCPAddr:
		return a.IP.String()
	case *net.UDPAddr:
		return a.IP.String()
	default:
		// Try to parse as host:port
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			return addr.String()
		}
		return host
	}
}

// Server wraps the go-smtp server
type Server struct {
	mxServer         *smtp.Server
	submissionServer *smtp.Server
	smtpsServer      *smtp.Server // Separate instance for implicit TLS (port 465)
	config           *config.Config
	mxListener       net.Listener
	subListener      net.Listener
	tlsListener      net.Listener
}

// NewServer creates SMTP servers for MX and submission
func NewServer(backend *Backend, cfg *config.Config, tlsConfig *tls.Config) *Server {
	// MX server (port 25) - for receiving mail from other servers
	mxServer := smtp.NewServer(backend)
	mxServer.Domain = cfg.Server.Hostname
	mxServer.ReadTimeout = 60 * time.Second
	mxServer.WriteTimeout = 60 * time.Second
	mxServer.MaxMessageBytes = int64(cfg.Security.MaxMessageSize)
	mxServer.MaxRecipients = 100
	mxServer.AllowInsecureAuth = false // No auth on port 25

	// Submission server (port 587/465) - for sending mail from clients
	submissionServer := smtp.NewServer(&submissionBackend{Backend: backend})
	submissionServer.Domain = cfg.Server.Hostname
	submissionServer.ReadTimeout = 60 * time.Second
	submissionServer.WriteTimeout = 60 * time.Second
	submissionServer.MaxMessageBytes = int64(cfg.Security.MaxMessageSize)
	submissionServer.MaxRecipients = 100
	submissionServer.AllowInsecureAuth = false // Always require TLS/STARTTLS before auth - never send credentials in plaintext

	if tlsConfig != nil {
		submissionServer.TLSConfig = tlsConfig
		mxServer.TLSConfig = tlsConfig
	}

	// SMTPS server (port 465) - implicit TLS, connection is already encrypted
	// TLSConfig=nil prevents advertising STARTTLS on an already-TLS connection
	// AllowInsecureAuth=true allows AUTH because the limitedConn wrapper hides
	// *tls.Conn from the library's type assertion, but the connection IS
	// encrypted via the TLS listener
	smtpsServer := smtp.NewServer(&submissionBackend{Backend: backend})
	smtpsServer.Domain = cfg.Server.Hostname
	smtpsServer.ReadTimeout = 60 * time.Second
	smtpsServer.WriteTimeout = 60 * time.Second
	smtpsServer.MaxMessageBytes = int64(cfg.Security.MaxMessageSize)
	smtpsServer.MaxRecipients = 100
	smtpsServer.AllowInsecureAuth = true
	smtpsServer.TLSConfig = nil // No STARTTLS needed, already encrypted

	return &Server{
		mxServer:         mxServer,
		submissionServer: submissionServer,
		smtpsServer:      smtpsServer,
		config:           cfg,
	}
}

// submissionBackend wraps Backend to mark sessions as submission
type submissionBackend struct {
	*Backend
}

func (b *submissionBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	session, err := b.Backend.NewSession(c)
	if err != nil {
		return nil, err
	}
	session.(*Session).isSubmission = true
	return session, nil
}

// ListenAndServe starts the MX server
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.config.Server.BindAddress, s.config.Server.SMTPPort)

	rawListener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// Wrap with connection limits
	listener := newLimitedListener(rawListener, defaultMaxConnections, defaultMaxConnectionsPerIP)
	s.mxListener = listener

	log.Printf("SMTP MX server listening on %s (max %d conns, %d per IP)", addr, defaultMaxConnections, defaultMaxConnectionsPerIP)

	go func() {
		if err := s.mxServer.Serve(listener); err != nil {
			log.Printf("SMTP MX server error: %v", err)
		}
	}()

	return nil
}

// ListenAndServeSubmission starts the submission server
func (s *Server) ListenAndServeSubmission() error {
	addr := fmt.Sprintf("%s:%d", s.config.Server.BindAddress, s.config.Server.SubmissionPort)

	rawListener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// Wrap with connection limits
	listener := newLimitedListener(rawListener, defaultMaxConnections, defaultMaxConnectionsPerIP)
	s.subListener = listener

	log.Printf("SMTP Submission server listening on %s (max %d conns, %d per IP)", addr, defaultMaxConnections, defaultMaxConnectionsPerIP)

	go func() {
		if err := s.submissionServer.Serve(listener); err != nil {
			log.Printf("SMTP Submission server error: %v", err)
		}
	}()

	return nil
}

// ListenAndServeTLS starts the SMTPS server (implicit TLS)
func (s *Server) ListenAndServeTLS() error {
	if s.mxServer.TLSConfig == nil {
		return nil // No TLS configured
	}

	addr := fmt.Sprintf("%s:%d", s.config.Server.BindAddress, s.config.Server.SMTPSPort)

	rawListener, err := tls.Listen("tcp", addr, s.mxServer.TLSConfig)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// Wrap with connection limits
	listener := newLimitedListener(rawListener, defaultMaxConnections, defaultMaxConnectionsPerIP)
	s.tlsListener = listener

	log.Printf("SMTPS server listening on %s (max %d conns, %d per IP)", addr, defaultMaxConnections, defaultMaxConnectionsPerIP)

	go func() {
		// Use the dedicated SMTPS server instance (TLSConfig=nil,
		// AllowInsecureAuth=true) so the library doesn't advertise STARTTLS
		// or block AUTH on already-encrypted connections
		if err := s.smtpsServer.Serve(listener); err != nil {
			log.Printf("SMTPS server error: %v", err)
		}
	}()

	return nil
}

// Close stops all servers
func (s *Server) Close() error {
	if s.mxListener != nil {
		s.mxListener.Close()
	}
	if s.subListener != nil {
		s.subListener.Close()
	}
	if s.tlsListener != nil {
		s.tlsListener.Close()
	}
	if s.mxServer != nil {
		s.mxServer.Close()
	}
	if s.submissionServer != nil {
		s.submissionServer.Close()
	}
	if s.smtpsServer != nil {
		s.smtpsServer.Close()
	}
	return nil
}
