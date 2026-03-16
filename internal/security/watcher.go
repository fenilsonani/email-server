package security

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// CertificateWatcher watches certificate files for changes and triggers reloads
type CertificateWatcher struct {
	certPath       string
	keyPath        string
	debounceDelay  time.Duration
	tlsManager     *TLSManager
	watcher        *fsnotify.Watcher
	mu             sync.RWMutex
	running        bool
	lastReloadTime time.Time
	lastCertHash   string
	lastKeyHash    string
}

// NewCertificateWatcher creates a new certificate file watcher
// debounceDelay prevents multiple reloads for rapid file changes (e.g., atomic writes)
func NewCertificateWatcher(certPath, keyPath string, debounceDelay time.Duration, tlsManager *TLSManager) (*CertificateWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &CertificateWatcher{
		certPath:      certPath,
		keyPath:       keyPath,
		debounceDelay: debounceDelay,
		tlsManager:    tlsManager,
		watcher:       watcher,
	}, nil
}

// Start begins watching certificate files for changes
// Runs in a goroutine and returns immediately
func (c *CertificateWatcher) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("watcher already running")
	}

	// Calculate initial hashes to detect changes
	certHash, _ := c.hashFile(c.certPath)
	keyHash, _ := c.hashFile(c.keyPath)
	c.lastCertHash = certHash
	c.lastKeyHash = keyHash

	c.mu.Unlock()

	// Watch certificate paths
	if err := c.watcher.Add(c.certPath); err != nil {
		return fmt.Errorf("failed to watch cert file: %w", err)
	}

	if err := c.watcher.Add(c.keyPath); err != nil {
		c.watcher.Close()
		return fmt.Errorf("failed to watch key file: %w", err)
	}

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	// Start watching in goroutine
	go c.watchLoop(ctx)

	return nil
}

// Stop stops watching certificate files
func (c *CertificateWatcher) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	c.running = false
	return c.watcher.Close()
}

// watchLoop handles file system events
func (c *CertificateWatcher) watchLoop(ctx context.Context) {
	debounceTimer := time.NewTimer(0)
	debounceTimer.Stop() // Don't trigger immediately

	for {
		select {
		case <-ctx.Done():
			c.Stop()
			return

		case err, ok := <-c.watcher.Errors:
			if !ok {
				return
			}
			// Log error but continue watching
			fmt.Printf("certificate watcher error: %v\n", err)

		case event, ok := <-c.watcher.Events:
			if !ok {
				return
			}

			// Only process write and create events
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			// Check if file was actually modified
			if !c.isModified(event.Name) {
				continue
			}

			// Reset debounce timer
			debounceTimer.Reset(c.debounceDelay)

		case <-debounceTimer.C:
			// After debounce delay, reload certificates
			c.reloadCertificates()
		}
	}
}

// isModified checks if a file has actually been modified by comparing hash
func (c *CertificateWatcher) isModified(path string) bool {
	newHash, err := c.hashFile(path)
	if err != nil {
		return false // Can't read file, probably transient
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if path == c.certPath {
		if newHash == c.lastCertHash {
			return false
		}
		c.lastCertHash = newHash
		return true
	}

	if path == c.keyPath {
		if newHash == c.lastKeyHash {
			return false
		}
		c.lastKeyHash = newHash
		return true
	}

	return false
}

// reloadCertificates triggers a reload of the TLS certificates
func (c *CertificateWatcher) reloadCertificates() {
	c.mu.Lock()
	c.lastReloadTime = time.Now()
	c.mu.Unlock()

	if err := c.tlsManager.ReloadCertificates(); err != nil {
		fmt.Printf("failed to reload certificates after file change: %v\n", err)
	} else {
		fmt.Printf("certificates reloaded successfully from file changes\n")
	}
}

// hashFile calculates a SHA-256 hash of a file for change detection
func (c *CertificateWatcher) hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// GetLastReloadTime returns when the last reload occurred
func (c *CertificateWatcher) GetLastReloadTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastReloadTime
}

// IsRunning returns whether the watcher is currently active
func (c *CertificateWatcher) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}
