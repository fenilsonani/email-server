package imap

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"sync"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
)

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

	// Mailbox trackers for IDLE notifications
	trackersMu sync.RWMutex
	trackers   map[int64]*imapserver.MailboxTracker
}

// NewServer creates a new IMAP v2 server
func NewServer(authenticator *auth.Authenticator, store *maildir.Store, addr, tlsAddr string, tlsConfig *tls.Config) *Server {
	s := &Server{
		authenticator: authenticator,
		store:         store,
		tlsConfig:     tlsConfig,
		addr:          addr,
		tlsAddr:       tlsAddr,
		trackers:      make(map[int64]*imapserver.MailboxTracker),
	}

	// Create IMAP server with v2 API
	s.imapServer = imapserver.New(&imapserver.Options{
		NewSession: func(conn *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			session := NewSession(s, conn)
			return session, &imapserver.GreetingData{}, nil
		},
		Caps: imap.CapSet{
			imap.CapIMAP4rev1: {},
			imap.CapIdle:      {},
		},
		TLSConfig:    tlsConfig,
		InsecureAuth: true, // We handle auth security ourselves
	})

	log.Printf("IMAP v2 server created with IDLE support")
	return s
}

// GetMailboxTracker returns or creates a tracker for a mailbox
func (s *Server) GetMailboxTracker(mailboxID int64) *imapserver.MailboxTracker {
	s.trackersMu.RLock()
	tracker, ok := s.trackers[mailboxID]
	s.trackersMu.RUnlock()

	if ok {
		return tracker
	}

	s.trackersMu.Lock()
	defer s.trackersMu.Unlock()

	// Double-check after acquiring write lock
	if tracker, ok = s.trackers[mailboxID]; ok {
		return tracker
	}

	// Create new tracker with initial message count
	tracker = imapserver.NewMailboxTracker(0)
	s.trackers[mailboxID] = tracker
	return tracker
}

// NotifyMailboxUpdate notifies all sessions watching a mailbox about updates
func (s *Server) NotifyMailboxUpdate(mailboxID int64) {
	s.trackersMu.RLock()
	tracker, ok := s.trackers[mailboxID]
	s.trackersMu.RUnlock()

	if !ok {
		return
	}

	// Get current message count
	// The tracker will notify all IDLE sessions
	ctx := context.Background()
	stats, err := s.store.GetMailboxStats(ctx, mailboxID)
	if err != nil {
		return
	}

	log.Printf("IMAP v2: Notifying IDLE clients of mailbox update (messages: %d)", stats.Messages)
	tracker.QueueNumMessages(uint32(stats.Messages))
}

// NotifyMailboxUpdateByName notifies by username and mailbox name
func (s *Server) NotifyMailboxUpdateByName(username, mailboxName string) {
	ctx := context.Background()

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
		listener, err := net.Listen("tcp", s.addr)
		if err != nil {
			return err
		}
		s.listener = listener

		log.Printf("IMAP server listening on %s", s.addr)

		go func() {
			if err := s.imapServer.Serve(listener); err != nil {
				log.Printf("IMAP server error: %v", err)
			}
		}()
	}

	return nil
}

// ListenAndServeTLS starts the IMAPS server
func (s *Server) ListenAndServeTLS(tlsConfig *tls.Config) error {
	if s.tlsAddr != "" && tlsConfig != nil {
		listener, err := tls.Listen("tcp", s.tlsAddr, tlsConfig)
		if err != nil {
			return err
		}
		s.tlsListener = listener

		log.Printf("IMAPS server listening on %s", s.tlsAddr)

		go func() {
			if err := s.imapServer.Serve(listener); err != nil {
				log.Printf("IMAPS server error: %v", err)
			}
		}()
	}

	return nil
}

// Close stops the server
func (s *Server) Close() error {
	if s.listener != nil {
		s.listener.Close()
	}
	if s.tlsListener != nil {
		s.tlsListener.Close()
	}
	return s.imapServer.Close()
}
