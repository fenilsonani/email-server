package smtp

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/emersion/go-smtp"
	"github.com/fenilsonani/email-server/internal/config"
)

// Server wraps the go-smtp server
type Server struct {
	mxServer         *smtp.Server
	submissionServer *smtp.Server
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
	submissionServer.AllowInsecureAuth = !cfg.Security.RequireTLS

	if tlsConfig != nil {
		submissionServer.TLSConfig = tlsConfig
		mxServer.TLSConfig = tlsConfig
	}

	return &Server{
		mxServer:         mxServer,
		submissionServer: submissionServer,
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
	addr := fmt.Sprintf(":%d", s.config.Server.SMTPPort)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.mxListener = listener

	log.Printf("SMTP MX server listening on %s", addr)

	go func() {
		if err := s.mxServer.Serve(listener); err != nil {
			log.Printf("SMTP MX server error: %v", err)
		}
	}()

	return nil
}

// ListenAndServeSubmission starts the submission server
func (s *Server) ListenAndServeSubmission() error {
	addr := fmt.Sprintf(":%d", s.config.Server.SubmissionPort)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.subListener = listener

	log.Printf("SMTP Submission server listening on %s", addr)

	go func() {
		if err := s.submissionServer.Serve(listener); err != nil {
			log.Printf("SMTP Submission server error: %v", err)
		}
	}()

	return nil
}

// ListenAndServeTLS starts the SMTPS server (implicit TLS)
func (s *Server) ListenAndServeTLS() error {
	if s.submissionServer.TLSConfig == nil {
		return nil // No TLS configured
	}

	addr := fmt.Sprintf(":%d", s.config.Server.SMTPSPort)

	listener, err := tls.Listen("tcp", addr, s.submissionServer.TLSConfig)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.tlsListener = listener

	log.Printf("SMTPS server listening on %s", addr)

	go func() {
		if err := s.submissionServer.Serve(listener); err != nil {
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
	return nil
}
