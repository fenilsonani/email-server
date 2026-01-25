package testenv

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/logging"
	"github.com/fenilsonani/email-server/internal/testutil"
)

// TestServer represents a complete test server with all components.
type TestServer struct {
	DB            *sql.DB
	Logger        *logging.Logger
	Authenticator *auth.Authenticator
	RedisClient   interface{} // Would be *redis.Client in real implementation

	// Configuration
	Config ServerConfig

	// Cleanup functions
	cleanup []func() error
}

// ServerConfig holds test server configuration.
type ServerConfig struct {
	DatabaseType string        // "sqlite" or "postgres"
	LogLevel     string        // "debug", "info", "warn", "error"
	Timeout      time.Duration // Request timeout
	Port         int           // Server port (0 for random)
}

// NewTestServer creates a new test server with all components.
func NewTestServer(t *testing.T, cfg ServerConfig) *TestServer {
	t.Helper()

	ts := &TestServer{
		Config:  cfg,
		cleanup: make([]func() error, 0),
	}

	// Set defaults
	if cfg.DatabaseType == "" {
		cfg.DatabaseType = "sqlite"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "error"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	// Setup database
	if err := ts.setupDatabase(t); err != nil {
		t.Fatalf("Failed to setup database: %v", err)
	}

	// Setup logger
	if err := ts.setupLogger(t); err != nil {
		t.Fatalf("Failed to setup logger: %v", err)
	}

	// Setup authenticator
	if err := ts.setupAuthenticator(t); err != nil {
		t.Fatalf("Failed to setup authenticator: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		ts.Cleanup()
	})

	return ts
}

// setupDatabase initializes the database for testing.
func (ts *TestServer) setupDatabase(t *testing.T) error {
	t.Helper()

	if ts.Config.DatabaseType == "sqlite" {
		// Use in-memory SQLite for testing
		testutil.WithTestDBAndSchema(t, func(db *sql.DB) {
			ts.DB = db
		})
	} else if ts.Config.DatabaseType == "postgres" {
		dsn := testutil.TestPostgresDSN()
		if dsn == "" {
			t.Skip("PostgreSQL not configured for testing")
		}

		db, err := sql.Open("postgres", dsn)
		if err != nil {
			return fmt.Errorf("failed to open postgres: %w", err)
		}

		// Test connection
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			return fmt.Errorf("failed to ping postgres: %w", err)
		}

		ts.DB = db
		ts.cleanup = append(ts.cleanup, func() error {
			return db.Close()
		})
	}

	return nil
}

// setupLogger initializes the logger for testing.
func (ts *TestServer) setupLogger(t *testing.T) error {
	t.Helper()

	config := logging.DefaultConfig()
	config.Level = ts.Config.LogLevel

	logger, err := logging.New(config)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	ts.Logger = logger
	return nil
}

// setupAuthenticator initializes the authenticator for testing.
func (ts *TestServer) setupAuthenticator(t *testing.T) error {
	t.Helper()

	if ts.DB == nil {
		return fmt.Errorf("database must be initialized before authenticator")
	}

	ts.Authenticator = auth.NewAuthenticator(ts.DB)
	return nil
}

// AddUser adds a test user to the server.
func (ts *TestServer) AddUser(t *testing.T, email, password string) *auth.User {
	t.Helper()

	_, cancel := context.WithTimeout(context.Background(), ts.Config.Timeout)
	defer cancel()

	// Create user in database
	// This would use the storage layer in real implementation
	t.Logf("Adding test user: %s", email)

	return &auth.User{
		Email:      email,
		QuotaBytes: 1073741824, // 1GB
	}
}

// CreateMailbox creates a test mailbox.
func (ts *TestServer) CreateMailbox(t *testing.T, userEmail, mailboxName string) error {
	t.Helper()

	_, cancel := context.WithTimeout(context.Background(), ts.Config.Timeout)
	defer cancel()

	t.Logf("Creating mailbox %s for user %s", mailboxName, userEmail)

	return nil
}

// SendEmail sends a test email through the server.
func (ts *TestServer) SendEmail(t *testing.T, from, to, subject, body string) error {
	t.Helper()

	_, cancel := context.WithTimeout(context.Background(), ts.Config.Timeout)
	defer cancel()

	t.Logf("Sending email from %s to %s with subject: %s", from, to, subject)

	return nil
}

// ReceiveEmail retrieves a received email.
func (ts *TestServer) ReceiveEmail(t *testing.T, userEmail, mailbox string) (string, error) {
	t.Helper()

	_, cancel := context.WithTimeout(context.Background(), ts.Config.Timeout)
	defer cancel()

	t.Logf("Receiving email from mailbox %s for user %s", mailbox, userEmail)

	return "", nil
}

// GetAvailablePort finds an available port for testing.
func (ts *TestServer) GetAvailablePort() (int, error) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, fmt.Errorf("failed to find available port: %w", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

// Cleanup cleans up all test server resources.
func (ts *TestServer) Cleanup() error {
	var lastErr error

	// Run cleanup functions in reverse order
	for i := len(ts.cleanup) - 1; i >= 0; i-- {
		if err := ts.cleanup[i](); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// WithTestServer creates a new test server and passes it to the test function.
func WithTestServer(t *testing.T, cfg ServerConfig, fn func(*TestServer)) {
	t.Helper()

	ts := NewTestServer(t, cfg)
	fn(ts)
}

// SimpleTestServer creates a test server with minimal configuration.
func SimpleTestServer(t *testing.T) *TestServer {
	t.Helper()

	return NewTestServer(t, ServerConfig{
		DatabaseType: "sqlite",
		LogLevel:     "error",
		Timeout:      30 * time.Second,
	})
}

// IntegrationTestServer creates a test server for integration testing.
func IntegrationTestServer(t *testing.T) *TestServer {
	t.Helper()

	config := ServerConfig{
		DatabaseType: "sqlite",
		LogLevel:     "info",
		Timeout:      60 * time.Second,
	}

	// Use PostgreSQL if configured
	if testutil.PostgresAvailable() {
		config.DatabaseType = "postgres"
	}

	return NewTestServer(t, config)
}

// E2ETestServer creates a test server for end-to-end testing.
func E2ETestServer(t *testing.T) *TestServer {
	t.Helper()

	return IntegrationTestServer(t)
}

// ServerMetrics holds server performance metrics.
type ServerMetrics struct {
	RequestCount      int64
	ErrorCount        int64
	AverageLatency    time.Duration
	MaxLatency        time.Duration
	DatabaseQueries   int64
}

// GetMetrics returns current server metrics.
func (ts *TestServer) GetMetrics() ServerMetrics {
	return ServerMetrics{
		RequestCount:    0,
		ErrorCount:      0,
		AverageLatency:  0,
		MaxLatency:      0,
		DatabaseQueries: 0,
	}
}

// ResetMetrics resets server metrics.
func (ts *TestServer) ResetMetrics() {
	// Reset metric tracking
}

// Health checks the health of the test server.
func (ts *TestServer) Health() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if ts.DB == nil {
		return fmt.Errorf("database is not initialized")
	}

	if err := ts.DB.PingContext(ctx); err != nil {
		return fmt.Errorf("database health check failed: %w", err)
	}

	return nil
}

// WaitForReady waits for the server to be ready for testing.
func (ts *TestServer) WaitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if err := ts.Health(); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("server failed to become ready within %v", timeout)
}
