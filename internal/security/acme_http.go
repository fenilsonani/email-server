package security

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

	"golang.org/x/crypto/acme/autocert"
)

// HTTPChallengeServer handles HTTP-01 ACME challenges for Let's Encrypt
// It listens on port 80 and serves ACME challenge tokens
type HTTPChallengeServer struct {
	certManager *autocert.Manager
	port        int
	server      *http.Server
	mu          sync.Mutex
	running     bool
}

// NewHTTPChallengeServer creates a new HTTP challenge server
func NewHTTPChallengeServer(certManager *autocert.Manager, port int) *HTTPChallengeServer {
	return &HTTPChallengeServer{
		certManager: certManager,
		port:        port,
	}
}

// Start starts the HTTP challenge server
// Returns immediately; the server runs in the background
func (h *HTTPChallengeServer) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.running {
		return fmt.Errorf("HTTP challenge server already running")
	}

	if h.certManager == nil {
		return fmt.Errorf("no autocert manager provided")
	}

	// Create HTTP handler that serves ACME challenges and redirects other traffic to HTTPS
	mux := http.NewServeMux()

	// Serve ACME challenge tokens
	mux.HandleFunc("/.well-known/acme-challenge/", h.certManager.HTTPHandler(nil).ServeHTTP)

	// Redirect all other HTTP requests to HTTPS
	mux.HandleFunc("/", h.handleRedirect)

	// Create HTTP server
	addr := fmt.Sprintf(":%d", h.port)
	h.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Listen and serve in goroutine
	go func() {
		if err := h.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP challenge server error: %v\n", err)
		}
	}()

	h.running = true
	return nil
}

// Stop gracefully shuts down the HTTP challenge server
func (h *HTTPChallengeServer) Stop(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running || h.server == nil {
		return nil
	}

	h.running = false
	return h.server.Shutdown(ctx)
}

// IsRunning returns whether the server is currently running
func (h *HTTPChallengeServer) IsRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

// handleRedirect redirects HTTP requests to HTTPS
// This ensures browsers and clients connect to the secure version
func (h *HTTPChallengeServer) handleRedirect(w http.ResponseWriter, r *http.Request) {
	// Don't redirect ACME challenge requests
	if r.URL.Path == "/.well-known/acme-challenge/" {
		http.NotFound(w, r)
		return
	}

	// Get the host from the request
	host := r.Host
	// Remove port if present and rebuild URL with HTTPS
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	// Redirect to HTTPS
	target := fmt.Sprintf("https://%s%s", host, r.RequestURI)
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}
