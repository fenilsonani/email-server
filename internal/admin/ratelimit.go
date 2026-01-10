package admin

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter tracks failed login attempts per IP
type RateLimiter struct {
	mu       sync.RWMutex
	attempts map[string]*attemptInfo
	// Configuration
	maxAttempts    int
	windowSize     time.Duration
	blockDuration  time.Duration
	trustedProxies map[string]bool // Only trust proxy headers from these IPs
}

type attemptInfo struct {
	count     int
	firstTime time.Time
	blockedAt time.Time
}

// NewRateLimiter creates a new rate limiter
// maxAttempts: max failed attempts before blocking
// windowSize: time window for counting attempts
// blockDuration: how long to block after exceeding limit
// trustedProxies: list of proxy IPs that are trusted for X-Forwarded-For headers
func NewRateLimiter(maxAttempts int, windowSize, blockDuration time.Duration, trustedProxies []string) *RateLimiter {
	proxyMap := make(map[string]bool)
	for _, ip := range trustedProxies {
		proxyMap[ip] = true
	}
	rl := &RateLimiter{
		attempts:       make(map[string]*attemptInfo),
		maxAttempts:    maxAttempts,
		windowSize:     windowSize,
		blockDuration:  blockDuration,
		trustedProxies: proxyMap,
	}
	// Start cleanup goroutine
	go rl.cleanup()
	return rl
}

// DefaultRateLimiter returns a rate limiter with sensible defaults
// 5 attempts per 15 minutes, 30 minute block
// Trusts localhost as proxy (for nginx reverse proxy)
func DefaultRateLimiter() *RateLimiter {
	// Trust nginx on localhost - this allows rate limiting by actual client IP
	return NewRateLimiter(5, 15*time.Minute, 30*time.Minute, []string{"127.0.0.1", "::1"})
}

// GetClientIP extracts the client IP from the request
// Only trusts X-Forwarded-For/X-Real-IP if request comes from a trusted proxy
func (rl *RateLimiter) GetClientIP(r *http.Request) string {
	// First get the direct connection IP
	directIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		directIP = r.RemoteAddr
	}

	// Only trust proxy headers if the direct connection is from a trusted proxy
	if len(rl.trustedProxies) > 0 && rl.trustedProxies[directIP] {
		// Check X-Forwarded-For header (for reverse proxy)
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Take the first IP in the chain (original client)
			ips := strings.Split(xff, ",")
			if len(ips) > 0 {
				clientIP := strings.TrimSpace(ips[0])
				if net.ParseIP(clientIP) != nil {
					return clientIP
				}
			}
		}

		// Check X-Real-IP header
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			if net.ParseIP(xri) != nil {
				return xri
			}
		}
	}

	// Use direct connection IP (don't trust headers from untrusted sources)
	return directIP
}

// getIP is deprecated - use GetClientIP instead
// Kept for backward compatibility
func getIP(r *http.Request) string {
	// Fall back to RemoteAddr only - don't trust headers without validation
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// IsBlocked checks if an IP is currently blocked
func (rl *RateLimiter) IsBlocked(ip string) bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	info, exists := rl.attempts[ip]
	if !exists {
		return false
	}

	// Check if blocked
	if !info.blockedAt.IsZero() {
		if time.Since(info.blockedAt) < rl.blockDuration {
			return true
		}
	}

	return false
}

// RecordFailure records a failed login attempt
// Returns true if the IP is now blocked
func (rl *RateLimiter) RecordFailure(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	info, exists := rl.attempts[ip]

	if !exists {
		rl.attempts[ip] = &attemptInfo{
			count:     1,
			firstTime: now,
		}
		return false
	}

	// Check if the window has expired
	if now.Sub(info.firstTime) > rl.windowSize {
		// Reset the window
		info.count = 1
		info.firstTime = now
		info.blockedAt = time.Time{}
		return false
	}

	// Increment count
	info.count++

	// Check if we should block
	if info.count >= rl.maxAttempts {
		info.blockedAt = now
		return true
	}

	return false
}

// RecordSuccess clears failed attempts for an IP on successful login
func (rl *RateLimiter) RecordSuccess(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, ip)
}

// RemainingAttempts returns how many attempts remain before blocking
func (rl *RateLimiter) RemainingAttempts(ip string) int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	info, exists := rl.attempts[ip]
	if !exists {
		return rl.maxAttempts
	}

	// Check if window expired
	if time.Since(info.firstTime) > rl.windowSize {
		return rl.maxAttempts
	}

	remaining := rl.maxAttempts - info.count
	if remaining < 0 {
		return 0
	}
	return remaining
}

// BlockedUntil returns when the block expires for an IP
func (rl *RateLimiter) BlockedUntil(ip string) time.Time {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	info, exists := rl.attempts[ip]
	if !exists || info.blockedAt.IsZero() {
		return time.Time{}
	}

	return info.blockedAt.Add(rl.blockDuration)
}

// Stats returns current rate limiter statistics
func (rl *RateLimiter) Stats() (total, blocked int) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	now := time.Now()
	for _, info := range rl.attempts {
		total++
		if !info.blockedAt.IsZero() && now.Sub(info.blockedAt) < rl.blockDuration {
			blocked++
		}
	}
	return
}

// cleanup periodically removes stale entries
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, info := range rl.attempts {
			// Remove entries older than window + block duration
			maxAge := rl.windowSize + rl.blockDuration
			if now.Sub(info.firstTime) > maxAge {
				delete(rl.attempts, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// PreviewRateLimiter limits email preview requests per session
type PreviewRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*previewBucket
}

type previewBucket struct {
	count     int
	resetTime time.Time
}

// NewPreviewRateLimiter creates a new preview rate limiter
func NewPreviewRateLimiter() *PreviewRateLimiter {
	prl := &PreviewRateLimiter{
		buckets: make(map[string]*previewBucket),
	}
	// Start cleanup goroutine
	go prl.cleanup()
	return prl
}

// Allow checks if a session can preview an email (max 10 per minute)
func (prl *PreviewRateLimiter) Allow(sessionID string) bool {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	now := time.Now()
	bucket, exists := prl.buckets[sessionID]

	if !exists || now.After(bucket.resetTime) {
		prl.buckets[sessionID] = &previewBucket{
			count:     1,
			resetTime: now.Add(time.Minute),
		}
		return true
	}

	if bucket.count >= 10 {
		return false
	}

	bucket.count++
	return true
}

// cleanup removes stale buckets every 5 minutes
func (prl *PreviewRateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		prl.mu.Lock()
		now := time.Now()
		for sessionID, bucket := range prl.buckets {
			if now.After(bucket.resetTime.Add(5 * time.Minute)) {
				delete(prl.buckets, sessionID)
			}
		}
		prl.mu.Unlock()
	}
}
