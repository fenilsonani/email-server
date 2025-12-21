package imap

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"sync"
	"time"

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

	// Shutdown coordination
	ctx        context.Context
	cancel     context.CancelFunc
	shutdownWg sync.WaitGroup
}

// NewServer creates a new IMAP v2 server
func NewServer(authenticator *auth.Authenticator, store *maildir.Store, addr, tlsAddr string, tlsConfig *tls.Config) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		authenticator: authenticator,
		store:         store,
		tlsConfig:     tlsConfig,
		addr:          addr,
		tlsAddr:       tlsAddr,
		trackers:      make(map[int64]*imapserver.MailboxTracker),
		ctx:           ctx,
		cancel:        cancel,
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

	// Get current message count with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats, err := s.store.GetMailboxStats(ctx, mailboxID)
	if err != nil {
		log.Printf("IMAP v2: Failed to get mailbox stats for notification: %v", err)
		return
	}

	log.Printf("IMAP v2: Notifying IDLE clients of mailbox update (messages: %d)", stats.Messages)
	tracker.QueueNumMessages(uint32(stats.Messages))
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
		listener, err := net.Listen("tcp", s.addr)
		if err != nil {
			return err
		}
		s.listener = listener

		log.Printf("IMAP server listening on %s", s.addr)

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
		listener, err := tls.Listen("tcp", s.tlsAddr, tlsConfig)
		if err != nil {
			return err
		}
		s.tlsListener = listener

		log.Printf("IMAPS server listening on %s", s.tlsAddr)

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
	for id, tracker := range s.trackers {
		if tracker != nil {
			log.Printf("IMAP: Closing tracker for mailbox %d", id)
		}
	}
	s.trackers = make(map[int64]*imapserver.MailboxTracker)
	s.trackersMu.Unlock()

	return closeErr
}
