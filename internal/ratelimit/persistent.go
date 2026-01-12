// Package ratelimit provides persistent rate limiting with automatic Redis sync.
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// PersistentLimiter provides rate limiting with automatic Redis persistence.
// It automatically falls back to in-memory when Redis is unavailable and
// syncs back when Redis recovers. No manual configuration needed.
type PersistentLimiter struct {
	redis        redis.UniversalClient
	prefix       string
	maxAttempts  int
	windowSize   time.Duration
	blockTime    time.Duration

	// In-memory fallback
	mu       sync.RWMutex
	attempts map[string]*attemptRecord
	blocks   map[string]time.Time

	// Health tracking
	redisHealthy int32 // atomic: 1 = healthy, 0 = unhealthy
	lastSync     time.Time

	// Background workers
	stopCh chan struct{}
	wg     sync.WaitGroup
}

type attemptRecord struct {
	count     int
	windowEnd time.Time
	lastSeen  time.Time
}

// Config holds rate limiter configuration with sensible defaults.
type Config struct {
	MaxAttempts int           // Max attempts before blocking (default: 5)
	WindowSize  time.Duration // Time window for counting attempts (default: 15m)
	BlockTime   time.Duration // How long to block after max attempts (default: 30m)
	KeyPrefix   string        // Redis key prefix (default: "ratelimit")
}

// DefaultConfig returns configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxAttempts: 5,
		WindowSize:  15 * time.Minute,
		BlockTime:   30 * time.Minute,
		KeyPrefix:   "ratelimit",
	}
}

// NewPersistentLimiter creates a new rate limiter with automatic Redis sync.
// Pass nil for redis client to use memory-only mode.
func NewPersistentLimiter(redisClient redis.UniversalClient, cfg Config) *PersistentLimiter {
	// Apply defaults
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.WindowSize == 0 {
		cfg.WindowSize = 15 * time.Minute
	}
	if cfg.BlockTime == 0 {
		cfg.BlockTime = 30 * time.Minute
	}
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "ratelimit"
	}

	l := &PersistentLimiter{
		redis:       redisClient,
		prefix:      cfg.KeyPrefix,
		maxAttempts: cfg.MaxAttempts,
		windowSize:  cfg.WindowSize,
		blockTime:   cfg.BlockTime,
		attempts:    make(map[string]*attemptRecord),
		blocks:      make(map[string]time.Time),
		stopCh:      make(chan struct{}),
	}

	// Check initial Redis health
	if redisClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := redisClient.Ping(ctx).Err(); err == nil {
			atomic.StoreInt32(&l.redisHealthy, 1)
		}
		cancel()
	}

	// Start background workers
	l.wg.Add(2)
	go l.healthChecker()
	go l.cleanup()

	return l
}

// Allow checks if a key is allowed to proceed.
// Returns true if allowed, false if blocked.
func (l *PersistentLimiter) Allow(key string) bool {
	// Check if blocked
	if l.IsBlocked(key) {
		return false
	}

	// Check attempt count
	count := l.getAttemptCount(key)
	return count < l.maxAttempts
}

// RecordAttempt records an attempt for a key.
// If success is false, increments the failure counter.
// If success is true, resets the counter.
func (l *PersistentLimiter) RecordAttempt(key string, success bool) {
	if success {
		l.resetAttempts(key)
		return
	}

	count := l.incrementAttempts(key)

	// Auto-block if max attempts exceeded
	if count >= l.maxAttempts {
		l.Block(key, l.blockTime)
	}
}

// Block blocks a key for the specified duration.
func (l *PersistentLimiter) Block(key string, duration time.Duration) {
	blockUntil := time.Now().Add(duration)

	// Try Redis first
	if atomic.LoadInt32(&l.redisHealthy) == 1 && l.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		blockKey := l.blockKey(key)
		err := l.redis.Set(ctx, blockKey, blockUntil.Unix(), duration).Err()
		cancel()
		if err == nil {
			return
		}
		// Redis failed, fall through to memory
		atomic.StoreInt32(&l.redisHealthy, 0)
	}

	// Memory fallback
	l.mu.Lock()
	l.blocks[key] = blockUntil
	l.mu.Unlock()
}

// IsBlocked checks if a key is currently blocked.
func (l *PersistentLimiter) IsBlocked(key string) bool {
	// Try Redis first
	if atomic.LoadInt32(&l.redisHealthy) == 1 && l.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		blockKey := l.blockKey(key)
		result, err := l.redis.Get(ctx, blockKey).Int64()
		cancel()
		if err == nil {
			return time.Now().Unix() < result
		}
		if err != redis.Nil {
			// Redis error, mark unhealthy
			atomic.StoreInt32(&l.redisHealthy, 0)
		}
	}

	// Memory fallback
	l.mu.RLock()
	blockUntil, exists := l.blocks[key]
	l.mu.RUnlock()

	if !exists {
		return false
	}

	if time.Now().After(blockUntil) {
		// Expired, clean up
		l.mu.Lock()
		delete(l.blocks, key)
		l.mu.Unlock()
		return false
	}

	return true
}

// Unblock removes a block for a key.
func (l *PersistentLimiter) Unblock(key string) {
	// Try Redis
	if atomic.LoadInt32(&l.redisHealthy) == 1 && l.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		l.redis.Del(ctx, l.blockKey(key))
		cancel()
	}

	// Also remove from memory
	l.mu.Lock()
	delete(l.blocks, key)
	l.mu.Unlock()
}

// RemainingAttempts returns how many attempts are left before blocking.
func (l *PersistentLimiter) RemainingAttempts(key string) int {
	if l.IsBlocked(key) {
		return 0
	}
	count := l.getAttemptCount(key)
	remaining := l.maxAttempts - count
	if remaining < 0 {
		return 0
	}
	return remaining
}

// BlockedUntil returns when a key will be unblocked.
// Returns zero time if not blocked.
func (l *PersistentLimiter) BlockedUntil(key string) time.Time {
	// Try Redis first
	if atomic.LoadInt32(&l.redisHealthy) == 1 && l.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		blockKey := l.blockKey(key)
		result, err := l.redis.Get(ctx, blockKey).Int64()
		cancel()
		if err == nil {
			return time.Unix(result, 0)
		}
	}

	// Memory fallback
	l.mu.RLock()
	blockUntil := l.blocks[key]
	l.mu.RUnlock()
	return blockUntil
}

// Stats returns current statistics.
func (l *PersistentLimiter) Stats() LimiterStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	stats := LimiterStats{
		TrackedKeys:   len(l.attempts),
		BlockedKeys:   0,
		RedisHealthy:  atomic.LoadInt32(&l.redisHealthy) == 1,
		LastSync:      l.lastSync,
	}

	now := time.Now()
	for _, blockUntil := range l.blocks {
		if now.Before(blockUntil) {
			stats.BlockedKeys++
		}
	}

	return stats
}

// LimiterStats holds rate limiter statistics.
type LimiterStats struct {
	TrackedKeys  int       `json:"tracked_keys"`
	BlockedKeys  int       `json:"blocked_keys"`
	RedisHealthy bool      `json:"redis_healthy"`
	LastSync     time.Time `json:"last_sync"`
}

// Close stops background workers and releases resources.
func (l *PersistentLimiter) Close() error {
	l.mu.Lock()
	select {
	case <-l.stopCh:
		// Already closed
		l.mu.Unlock()
		return nil
	default:
		close(l.stopCh)
	}
	l.mu.Unlock()
	l.wg.Wait()
	return nil
}

// Internal methods

func (l *PersistentLimiter) attemptKey(key string) string {
	return fmt.Sprintf("%s:attempts:%s", l.prefix, key)
}

func (l *PersistentLimiter) blockKey(key string) string {
	return fmt.Sprintf("%s:blocks:%s", l.prefix, key)
}

func (l *PersistentLimiter) getAttemptCount(key string) int {
	// Try Redis first
	if atomic.LoadInt32(&l.redisHealthy) == 1 && l.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		attemptKey := l.attemptKey(key)
		count, err := l.redis.Get(ctx, attemptKey).Int()
		cancel()
		if err == nil {
			return count
		}
		if err != redis.Nil {
			atomic.StoreInt32(&l.redisHealthy, 0)
		}
	}

	// Memory fallback
	l.mu.RLock()
	record, exists := l.attempts[key]
	l.mu.RUnlock()

	if !exists {
		return 0
	}

	// Check if window expired
	if time.Now().After(record.windowEnd) {
		l.mu.Lock()
		delete(l.attempts, key)
		l.mu.Unlock()
		return 0
	}

	return record.count
}

func (l *PersistentLimiter) incrementAttempts(key string) int {
	// Try Redis first
	if atomic.LoadInt32(&l.redisHealthy) == 1 && l.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		attemptKey := l.attemptKey(key)
		pipe := l.redis.TxPipeline()
		incrCmd := pipe.Incr(ctx, attemptKey)
		pipe.Expire(ctx, attemptKey, l.windowSize)
		_, err := pipe.Exec(ctx)
		cancel()
		if err == nil {
			return int(incrCmd.Val())
		}
		atomic.StoreInt32(&l.redisHealthy, 0)
	}

	// Memory fallback
	l.mu.Lock()
	defer l.mu.Unlock()

	record, exists := l.attempts[key]
	now := time.Now()

	if !exists || now.After(record.windowEnd) {
		// New window
		l.attempts[key] = &attemptRecord{
			count:     1,
			windowEnd: now.Add(l.windowSize),
			lastSeen:  now,
		}
		return 1
	}

	record.count++
	record.lastSeen = now
	return record.count
}

func (l *PersistentLimiter) resetAttempts(key string) {
	// Try Redis
	if atomic.LoadInt32(&l.redisHealthy) == 1 && l.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		l.redis.Del(ctx, l.attemptKey(key))
		cancel()
	}

	// Also reset in memory
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

// healthChecker periodically checks Redis health and syncs data.
func (l *PersistentLimiter) healthChecker() {
	defer l.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			if l.redis == nil {
				continue
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err := l.redis.Ping(ctx).Err()
			cancel()

			wasHealthy := atomic.LoadInt32(&l.redisHealthy) == 1
			isHealthy := err == nil

			if isHealthy {
				atomic.StoreInt32(&l.redisHealthy, 1)
				// If Redis just recovered, sync memory data to Redis
				if !wasHealthy {
					l.syncToRedis()
				}
			} else {
				atomic.StoreInt32(&l.redisHealthy, 0)
			}
		}
	}
}

// syncToRedis syncs in-memory data to Redis when it recovers.
func (l *PersistentLimiter) syncToRedis() {
	if l.redis == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	l.mu.RLock()
	defer l.mu.RUnlock()

	now := time.Now()
	pipe := l.redis.TxPipeline()

	// Sync blocks
	for key, blockUntil := range l.blocks {
		if now.Before(blockUntil) {
			remaining := blockUntil.Sub(now)
			pipe.Set(ctx, l.blockKey(key), blockUntil.Unix(), remaining)
		}
	}

	// Sync attempts
	for key, record := range l.attempts {
		if now.Before(record.windowEnd) {
			remaining := record.windowEnd.Sub(now)
			pipe.Set(ctx, l.attemptKey(key), record.count, remaining)
		}
	}

	pipe.Exec(ctx)
	l.lastSync = now
}

// cleanup removes expired entries from memory.
func (l *PersistentLimiter) cleanup() {
	defer l.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.cleanupExpired()
		}
	}
}

func (l *PersistentLimiter) cleanupExpired() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// Clean expired attempts
	for key, record := range l.attempts {
		if now.After(record.windowEnd) {
			delete(l.attempts, key)
		}
	}

	// Clean expired blocks
	for key, blockUntil := range l.blocks {
		if now.After(blockUntil) {
			delete(l.blocks, key)
		}
	}
}
