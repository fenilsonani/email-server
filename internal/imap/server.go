package imap

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"

	"github.com/emersion/go-imap/server"
)

// Server wraps the go-imap server with our configuration
type Server struct {
	*server.Server
	addr        string
	tlsAddr     string
	listener    net.Listener
	tlsListener net.Listener
}

// NewServer creates a new IMAP server
func NewServer(backend *Backend, addr, tlsAddr string, tlsConfig *tls.Config) *Server {
	imapServer := server.New(backend)
	imapServer.AllowInsecureAuth = true // We handle auth security ourselves

	// Enable IDLE extension - go-imap automatically enables it when backend has Updates()
	// Log enabled extensions for debugging
	log.Printf("IMAP server created, IDLE enabled: %v", imapServer.EnableAuth)

	return &Server{
		Server:  imapServer,
		addr:    addr,
		tlsAddr: tlsAddr,
	}
}

// ListenAndServe starts the IMAP server
func (s *Server) ListenAndServe() error {
	if s.addr != "" {
		listener, err := net.Listen("tcp", s.addr)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
		}
		s.listener = listener

		log.Printf("IMAP server listening on %s", s.addr)

		go func() {
			if err := s.Serve(listener); err != nil {
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
			return fmt.Errorf("failed to listen on %s: %w", s.tlsAddr, err)
		}
		s.tlsListener = listener

		log.Printf("IMAPS server listening on %s", s.tlsAddr)

		go func() {
			if err := s.Serve(listener); err != nil {
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
	return nil
}
