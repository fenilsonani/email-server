package imap

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
)

// Default connection limits and keepalive settings for IMAP
const (
	defaultIMAPMaxConnections      = 2000
	defaultIMAPMaxConnectionsPerIP = 100
	defaultTCPKeepalivePeriod      = 60 * time.Second
	defaultReadTimeout             = 30 * time.Minute
	defaultWriteTimeout            = 5 * time.Minute
)

// limitedListener wraps a net.Listener with connection limits and keepalive settings
type limitedListener struct {
	net.Listener
	maxConns           int
	maxConnsPerIP      int
	currentConns       int64
	perIPConns         map[string]int
	perIPMu            sync.Mutex
	sem                chan struct{}
	tcpKeepalivePeriod time.Duration
	readTimeout        time.Duration
	writeTimeout       time.Duration
}

// newLimitedListener creates a connection-limiting listener with keepalive settings
func newLimitedListener(l net.Listener, config *IMAPConfig) *limitedListener {
	if config == nil {
		config = DefaultIMAPConfig()
	}
	return &limitedListener{
		Listener:           l,
		maxConns:           config.MaxConnections,
		maxConnsPerIP:      config.MaxConnectionsPerIP,
		perIPConns:         make(map[string]int),
		sem:                make(chan struct{}, config.MaxConnections),
		tcpKeepalivePeriod: config.TCPKeepalivePeriod,
		readTimeout:        config.ReadTimeout,
		writeTimeout:       config.WriteTimeout,
	}
}

func (l *limitedListener) Accept() (net.Conn, error) {
	// Acquire global semaphore
	l.sem <- struct{}{}

	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.sem
		return nil, err
	}

	// Enable TCP-level keepalive to detect dead connections at OS level
	enableTCPKeepalive(conn, l.tcpKeepalivePeriod)

	// Check per-IP limit
	ip := extractIP(conn.RemoteAddr())
	l.perIPMu.Lock()
	if l.perIPConns[ip] >= l.maxConnsPerIP {
		l.perIPMu.Unlock()
		<-l.sem
		conn.Close()
		log.Printf("IMAP: Rejected connection from %s: per-IP limit exceeded", ip)
		return l.Accept()
	}
	l.perIPConns[ip]++
	l.perIPMu.Unlock()

	atomic.AddInt64(&l.currentConns, 1)

	// Wrap with deadline management for stale connection detection
	wrappedConn := &deadlineConn{
		Conn:         conn,
		readTimeout:  l.readTimeout,
		writeTimeout: l.writeTimeout,
	}

	return &limitedConn{
		Conn:     wrappedConn,
		listener: l,
		ip:       ip,
	}, nil
}

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
		<-c.listener.sem
	}
	return c.Conn.Close()
}

func extractIP(addr net.Addr) string {
	if addr == nil {
		return "unknown"
	}
	switch a := addr.(type) {
	case *net.TCPAddr:
		return a.IP.String()
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err != nil {
			return addr.String()
		}
		return host
	}
}

// enableTCPKeepalive enables TCP-level keepalive on a connection.
// This works for both raw TCP and TLS-wrapped connections.
func enableTCPKeepalive(conn net.Conn, period time.Duration) {
	// Try direct TCP connection first
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(period)
		return
	}

	// Handle TLS-wrapped connections
	if tlsConn, ok := conn.(*tls.Conn); ok {
		if netConn := tlsConn.NetConn(); netConn != nil {
			if tcpConn, ok := netConn.(*net.TCPConn); ok {
				tcpConn.SetKeepAlive(true)
				tcpConn.SetKeepAlivePeriod(period)
			}
		}
	}
}

// deadlineConn wraps a net.Conn with automatic read/write deadline management.
// This helps detect stale connections that stop responding.
type deadlineConn struct {
	net.Conn
	readTimeout  time.Duration
	writeTimeout time.Duration
}

func (c *deadlineConn) Read(b []byte) (int, error) {
	if c.readTimeout > 0 {
		c.Conn.SetReadDeadline(time.Now().Add(c.readTimeout))
	}
	return c.Conn.Read(b)
}

func (c *deadlineConn) Write(b []byte) (int, error) {
	if c.writeTimeout > 0 {
		c.Conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
	}
	return c.Conn.Write(b)
}

// Maximum number of mailbox trackers to cache (prevents unbounded memory growth)
const maxTrackerCacheSize = 5000

// trackerEntry holds a tracker with access time for LRU eviction
type trackerEntry struct {
	tracker    *imapserver.MailboxTracker
	lastAccess time.Time
}

// IMAPConfig holds IMAP-specific configuration
type IMAPConfig struct {
	IdleKeepaliveInterval time.Duration
	TCPKeepalivePeriod    time.Duration
	ReadTimeout           time.Duration
	WriteTimeout          time.Duration
	MaxConnections        int
	MaxConnectionsPerIP   int
}

// DefaultIMAPConfig returns sensible defaults for IMAP configuration
func DefaultIMAPConfig() *IMAPConfig {
	return &IMAPConfig{
		IdleKeepaliveInterval: 3 * time.Minute,
		TCPKeepalivePeriod:    60 * time.Second,
		ReadTimeout:           30 * time.Minute,
		WriteTimeout:          5 * time.Minute,
		MaxConnections:        2000,
		MaxConnectionsPerIP:   100,
	}
}

// Server wraps the go-imap v2 server
type Server struct {
	authenticator *auth.Authenticator
	store         *maildir.Store
	imapServer    *imapserver.Server
	tlsConfig     *tls.Config
	addr          string
	tlsAddr       string
	listener      net.Listener
	tlsListener   net.Listener
	config        *IMAPConfig

	// Mailbox trackers for IDLE notifications with LRU eviction
	trackersMu sync.RWMutex
	trackers   map[int64]*trackerEntry

	// Shutdown coordination
	ctx        context.Context
	cancel     context.CancelFunc
	shutdownWg sync.WaitGroup
}

// NewServer creates a new IMAP v2 server
func NewServer(authenticator *auth.Authenticator, store *maildir.Store, addr, tlsAddr string, tlsConfig *tls.Config, config *IMAPConfig) *Server {
	if config == nil {
		config = DefaultIMAPConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		authenticator: authenticator,
		store:         store,
		tlsConfig:     tlsConfig,
		addr:          addr,
		tlsAddr:       tlsAddr,
		trackers:      make(map[int64]*trackerEntry),
		ctx:           ctx,
		cancel:        cancel,
		config:        config,
	}

	// Create IMAP server with v2 API
	s.imapServer = imapserver.New(&imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			session := NewSession(s, conn)
			return session, &imapserver.GreetingData{}, nil
		},
		Caps: imap.CapSet{
			imap.CapIMAP4rev1:  {},
			imap.CapIdle:       {},
			imap.CapUIDPlus:    {}, // RFC 4315 - Better UID handling (UIDPLUS responses)
			imap.CapNamespace:  {}, // RFC 2342 - Namespace support (Thunderbird needs this)
			imap.CapMove:       {}, // RFC 6851 - Efficient MOVE command
			imap.CapSpecialUse: {}, // RFC 6154 - Special-use mailboxes (Sent, Drafts, etc.)
			imap.CapUnselect:   {}, // RFC 3691 - Clean mailbox unselect
		},
		TLSConfig:    tlsConfig,
		InsecureAuth: false, // Require TLS/STARTTLS before authentication
	})

	log.Printf("IMAP v2 server created with IDLE support")
	return s
}

// GetMailboxTracker returns or creates a tracker for a mailbox
func (s *Server) GetMailboxTracker(mailboxID int64) *imapserver.MailboxTracker {
	now := time.Now()

	s.trackersMu.RLock()
	entry, ok := s.trackers[mailboxID]
	s.trackersMu.RUnlock()

	if ok {
		// Update last access time (under write lock)
		s.trackersMu.Lock()
		if entry, ok = s.trackers[mailboxID]; ok {
			entry.lastAccess = now
		}
		s.trackersMu.Unlock()
		if entry != nil {
			return entry.tracker
		}
	}

	s.trackersMu.Lock()
	defer s.trackersMu.Unlock()

	// Double-check after acquiring write lock
	if entry, ok = s.trackers[mailboxID]; ok {
		entry.lastAccess = now
		return entry.tracker
	}

	// Evict old entries if cache is full (LRU eviction)
	if len(s.trackers) >= maxTrackerCacheSize {
		s.evictOldTrackers()
	}

	// Create new tracker with initial message count
	tracker := imapserver.NewMailboxTracker(0)
	s.trackers[mailboxID] = &trackerEntry{
		tracker:    tracker,
		lastAccess: now,
	}
	return tracker
}

// evictOldTrackers removes the least recently used trackers
// Must be called with trackersMu held
func (s *Server) evictOldTrackers() {
	// Remove 20% of entries to make room
	toRemove := maxTrackerCacheSize / 5
	if toRemove < 10 {
		toRemove = 10
	}

	// Find oldest entries
	type ageEntry struct {
		id   int64
		time time.Time
	}
	entries := make([]ageEntry, 0, len(s.trackers))
	for id, entry := range s.trackers {
		entries = append(entries, ageEntry{id: id, time: entry.lastAccess})
	}

	// Sort by time (oldest first) using simple selection for small batches
	for i := 0; i < toRemove && i < len(entries); i++ {
		minIdx := i
		for j := i + 1; j < len(entries); j++ {
			if entries[j].time.Before(entries[minIdx].time) {
				minIdx = j
			}
		}
		if minIdx != i {
			entries[i], entries[minIdx] = entries[minIdx], entries[i]
		}
		// Delete the oldest entry
		delete(s.trackers, entries[i].id)
	}

	log.Printf("IMAP: Evicted %d old mailbox trackers (cache was at %d)", toRemove, maxTrackerCacheSize)
}

// NotifyMailboxUpdate notifies all sessions watching a mailbox about updates
func (s *Server) NotifyMailboxUpdate(mailboxID int64) {
	s.trackersMu.RLock()
	entry, ok := s.trackers[mailboxID]
	s.trackersMu.RUnlock()

	if !ok || entry == nil {
		return
	}

	// Get current message count with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats, err := s.store.GetMailboxStats(ctx, mailboxID)
	if err != nil {
		log.Printf("IMAP v2: Failed to get mailbox stats for notification: %v", err)
		return
	}

	log.Printf("IMAP v2: Notifying IDLE clients of mailbox update (messages: %d)", stats.Messages)
	entry.tracker.QueueNumMessages(uint32(stats.Messages))
}

// NotifyMailboxUpdateByName notifies by username and mailbox name
func (s *Server) NotifyMailboxUpdateByName(username, mailboxName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Look up user
	user, err := s.authenticator.LookupUser(ctx, username)
	if err != nil {
		log.Printf("IMAP v2: NotifyMailboxUpdateByName - user not found: %s", username)
		return
	}

	// Look up mailbox
	mb, err := s.store.GetMailbox(ctx, user.ID, mailboxName)
	if err != nil {
		log.Printf("IMAP v2: NotifyMailboxUpdateByName - mailbox not found: %s/%s", username, mailboxName)
		return
	}

	s.NotifyMailboxUpdate(mb.ID)
}

// ListenAndServe starts the IMAP server
func (s *Server) ListenAndServe() error {
	if s.addr != "" {
		rawListener, err := net.Listen("tcp", s.addr)
		if err != nil {
			return err
		}

		// Wrap with connection limits and keepalive settings
		listener := newLimitedListener(rawListener, s.config)
		s.listener = listener

		log.Printf("IMAP server listening on %s (max %d conns, %d per IP, TCP keepalive %v)",
			s.addr, s.config.MaxConnections, s.config.MaxConnectionsPerIP, s.config.TCPKeepalivePeriod)

		s.shutdownWg.Add(1)
		go func() {
			defer s.shutdownWg.Done()
			if err := s.imapServer.Serve(listener); err != nil {
				select {
				case <-s.ctx.Done():
					// Server is shutting down, expected error
					log.Printf("IMAP server stopped")
				default:
					log.Printf("IMAP server error: %v", err)
				}
			}
		}()
	}

	return nil
}

// ListenAndServeTLS starts the IMAPS server
func (s *Server) ListenAndServeTLS(tlsConfig *tls.Config) error {
	if s.tlsAddr != "" && tlsConfig != nil {
		rawListener, err := tls.Listen("tcp", s.tlsAddr, tlsConfig)
		if err != nil {
			return err
		}

		// Wrap with connection limits and keepalive settings
		listener := newLimitedListener(rawListener, s.config)
		s.tlsListener = listener

		log.Printf("IMAPS server listening on %s (max %d conns, %d per IP, TCP keepalive %v)",
			s.tlsAddr, s.config.MaxConnections, s.config.MaxConnectionsPerIP, s.config.TCPKeepalivePeriod)

		s.shutdownWg.Add(1)
		go func() {
			defer s.shutdownWg.Done()
			if err := s.imapServer.Serve(listener); err != nil {
				select {
				case <-s.ctx.Done():
					// Server is shutting down, expected error
					log.Printf("IMAPS server stopped")
				default:
					log.Printf("IMAPS server error: %v", err)
				}
			}
		}()
	}

	return nil
}

// Close stops the server gracefully
func (s *Server) Close() error {
	// Signal shutdown to all goroutines
	if s.cancel != nil {
		s.cancel()
	}

	var closeErr error

	// Close listeners first to stop accepting new connections
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			log.Printf("IMAP: Error closing listener: %v", err)
			closeErr = err
		}
	}
	if s.tlsListener != nil {
		if err := s.tlsListener.Close(); err != nil {
			log.Printf("IMAPS: Error closing TLS listener: %v", err)
			if closeErr == nil {
				closeErr = err
			}
		}
	}

	// Close the IMAP server
	if s.imapServer != nil {
		if err := s.imapServer.Close(); err != nil {
			log.Printf("IMAP: Error closing server: %v", err)
			if closeErr == nil {
				closeErr = err
			}
		}
	}

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		s.shutdownWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("IMAP: All goroutines finished")
	case <-time.After(10 * time.Second):
		log.Printf("IMAP: Timeout waiting for goroutines to finish")
	}

	// Close all trackers
	s.trackersMu.Lock()
	for id, entry := range s.trackers {
		if entry != nil && entry.tracker != nil {
			log.Printf("IMAP: Closing tracker for mailbox %d", id)
		}
	}
	s.trackers = make(map[int64]*trackerEntry)
	s.trackersMu.Unlock()

	return closeErr
}
