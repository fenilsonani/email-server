// Package queue provides message queue implementations.
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Buffer pool for message ID generation to reduce allocations
var idBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 12)
		return &b
	},
}

// Common errors
var (
	ErrMessageNotFound = errors.New("message not found")
	ErrQueueClosed     = errors.New("queue is closed")
)

// Priority represents message priority level.
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// Message represents a queued email message.
type Message struct {
	ID          string    `json:"id"`
	Sender      string    `json:"sender"`
	Recipients  []string  `json:"recipients"`
	MessagePath string    `json:"message_path"` // Path to message file on disk
	Size        int64     `json:"size"`
	Attempts    int       `json:"attempts"`
	MaxAttempts int       `json:"max_attempts"`
	LastAttempt time.Time `json:"last_attempt,omitempty"`
	NextAttempt time.Time `json:"next_attempt"`
	LastError   string    `json:"last_error,omitempty"`
	Status      Status    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	Domain      string    `json:"domain"`   // Recipient domain for circuit breaker
	Priority    Priority  `json:"priority"` // Message priority (high, normal, low)
}

// Status represents the message delivery status.
type Status string

const (
	StatusPending  Status = "pending"
	StatusSending  Status = "sending"
	StatusSent     Status = "sent"
	StatusFailed   Status = "failed"
	StatusDeferred Status = "deferred"
	StatusBounced  Status = "bounced"
)

// Config configures the Redis queue.
type Config struct {
	// RedisURL is the Redis connection URL (for standalone mode).
	RedisURL string
	// Mode is the connection mode: standalone, sentinel, cluster
	Mode string
	// SentinelMaster is the master name for Sentinel mode.
	SentinelMaster string
	// SentinelAddrs are the Sentinel addresses.
	SentinelAddrs []string
	// ClusterAddrs are the cluster node addresses.
	ClusterAddrs []string
	// Password is the Redis password (optional).
	Password string
	// DB is the database number (not used in cluster mode).
	DB int
	// Prefix is the key prefix for all queue keys.
	Prefix string
	// MaxRetries is the maximum delivery attempts.
	MaxRetries int
	// RetryMaxAge is the maximum time to retry before permanent failure.
	RetryMaxAge time.Duration
	// PoolSize is the connection pool size.
	PoolSize int
	// MinIdleConns is the minimum number of idle connections.
	MinIdleConns int
	// DialTimeout is the connection dial timeout.
	DialTimeout time.Duration
	// ReadTimeout is the read timeout.
	ReadTimeout time.Duration
	// WriteTimeout is the write timeout.
	WriteTimeout time.Duration
}

// DefaultConfig returns default queue configuration.
func DefaultConfig() Config {
	return Config{
		RedisURL:     "redis://localhost:6379/0",
		Mode:         "standalone",
		Prefix:       "mail",
		MaxRetries:   15,
		RetryMaxAge:  7 * 24 * time.Hour, // 7 days
		PoolSize:     10,
		MinIdleConns: 5,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
}

// RedisQueue implements a message queue using Redis.
type RedisQueue struct {
	client redis.UniversalClient // Supports standalone, sentinel, and cluster modes
	config Config
	closed int32 // atomic: 1 if closed, 0 if open

	// Graceful shutdown
	wg sync.WaitGroup
	mu sync.RWMutex

	// Cached keys to avoid repeated string concatenation
	cachedPendingKey       string
	cachedPendingKeyHigh   string // Priority: high
	cachedPendingKeyNormal string // Priority: normal
	cachedPendingKeyLow    string // Priority: low
	cachedProcessingKey    string
	cachedFailedKey        string
	cachedSentKey          string
	cachedStatsKey         string
	cachedMessagePrefix    string
}

// NewRedisQueue creates a new Redis-backed message queue.
// Supports standalone, sentinel, and cluster modes based on cfg.Mode.
func NewRedisQueue(cfg Config) (*RedisQueue, error) {
	var client redis.UniversalClient

	// Apply defaults
	if cfg.PoolSize == 0 {
		cfg.PoolSize = 10
	}
	if cfg.MinIdleConns == 0 {
		cfg.MinIdleConns = 5
	}
	if cfg.RetryMaxAge == 0 {
		cfg.RetryMaxAge = 7 * 24 * time.Hour
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 3 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 3 * time.Second
	}

	mode := cfg.Mode
	if mode == "" {
		mode = "standalone"
	}

	switch mode {
	case "standalone":
		client = createStandaloneClient(cfg)
	case "sentinel":
		client = createSentinelClient(cfg)
	case "cluster":
		client = createClusterClient(cfg)
	default:
		return nil, fmt.Errorf("unsupported Redis mode: %s (supported: standalone, sentinel, cluster)", mode)
	}

	// Test connection with retry
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var lastErr error
	for i := 0; i < 3; i++ {
		if err := client.Ping(ctx).Err(); err == nil {
			break
		} else {
			lastErr = err
			if i < 2 {
				time.Sleep(time.Duration(i+1) * time.Second)
			}
		}
	}
	if lastErr != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Redis (%s mode) after retries: %w", mode, lastErr)
	}

	q := &RedisQueue{
		client: client,
		config: cfg,
		closed: 0,
		// Pre-compute cached keys to avoid repeated string concatenation
		cachedPendingKey:       cfg.Prefix + ":queue:pending",
		cachedPendingKeyHigh:   cfg.Prefix + ":queue:pending:high",
		cachedPendingKeyNormal: cfg.Prefix + ":queue:pending:normal",
		cachedPendingKeyLow:    cfg.Prefix + ":queue:pending:low",
		cachedProcessingKey:    cfg.Prefix + ":queue:processing",
		cachedFailedKey:        cfg.Prefix + ":queue:failed",
		cachedSentKey:          cfg.Prefix + ":queue:sent",
		cachedStatsKey:         cfg.Prefix + ":stats",
		cachedMessagePrefix:    cfg.Prefix + ":message:",
	}

	// Start connection health monitor
	go q.healthMonitor()

	return q, nil
}

// createStandaloneClient creates a single-instance Redis client.
func createStandaloneClient(cfg Config) *redis.Client {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		// Fallback to manual configuration
		opts = &redis.Options{
			Addr: "localhost:6379",
			DB:   cfg.DB,
		}
	}

	// Override with explicit config values
	if cfg.Password != "" {
		opts.Password = cfg.Password
	}

	// Configure connection pool for reliability
	opts.MaxRetries = 3
	opts.MinRetryBackoff = 100 * time.Millisecond
	opts.MaxRetryBackoff = 1 * time.Second
	opts.DialTimeout = cfg.DialTimeout
	opts.ReadTimeout = cfg.ReadTimeout
	opts.WriteTimeout = cfg.WriteTimeout
	opts.PoolSize = cfg.PoolSize
	opts.MinIdleConns = cfg.MinIdleConns
	opts.MaxIdleConns = cfg.PoolSize
	opts.ConnMaxIdleTime = 5 * time.Minute
	opts.ConnMaxLifetime = 30 * time.Minute
	opts.PoolTimeout = cfg.DialTimeout

	return redis.NewClient(opts)
}

// createSentinelClient creates a Redis Sentinel client for high availability.
func createSentinelClient(cfg Config) *redis.Client {
	if len(cfg.SentinelAddrs) == 0 {
		// Fallback to default sentinel address
		cfg.SentinelAddrs = []string{"localhost:26379"}
	}
	if cfg.SentinelMaster == "" {
		cfg.SentinelMaster = "mymaster"
	}

	opts := &redis.FailoverOptions{
		MasterName:      cfg.SentinelMaster,
		SentinelAddrs:   cfg.SentinelAddrs,
		Password:        cfg.Password,
		DB:              cfg.DB,
		MaxRetries:      3,
		MinRetryBackoff: 100 * time.Millisecond,
		MaxRetryBackoff: 1 * time.Second,
		DialTimeout:     cfg.DialTimeout,
		ReadTimeout:     cfg.ReadTimeout,
		WriteTimeout:    cfg.WriteTimeout,
		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		MaxIdleConns:    cfg.PoolSize,
		ConnMaxIdleTime: 5 * time.Minute,
		ConnMaxLifetime: 30 * time.Minute,
		PoolTimeout:     cfg.DialTimeout,
	}

	return redis.NewFailoverClient(opts)
}

// createClusterClient creates a Redis Cluster client for horizontal scaling.
func createClusterClient(cfg Config) *redis.ClusterClient {
	if len(cfg.ClusterAddrs) == 0 {
		// Fallback to default cluster addresses
		cfg.ClusterAddrs = []string{"localhost:7000", "localhost:7001", "localhost:7002"}
	}

	opts := &redis.ClusterOptions{
		Addrs:           cfg.ClusterAddrs,
		Password:        cfg.Password,
		MaxRetries:      3,
		MinRetryBackoff: 100 * time.Millisecond,
		MaxRetryBackoff: 1 * time.Second,
		DialTimeout:     cfg.DialTimeout,
		ReadTimeout:     cfg.ReadTimeout,
		WriteTimeout:    cfg.WriteTimeout,
		PoolSize:        cfg.PoolSize,
		MinIdleConns:    cfg.MinIdleConns,
		MaxIdleConns:    cfg.PoolSize,
		ConnMaxIdleTime: 5 * time.Minute,
		ConnMaxLifetime: 30 * time.Minute,
		PoolTimeout:     cfg.DialTimeout,
		// Cluster-specific options
		RouteByLatency: true,  // Route to lowest latency node
		RouteRandomly:  false, // Use RouteByLatency instead
	}

	return redis.NewClusterClient(opts)
}

// Key helpers - use cached keys to avoid allocations, with fallback for tests
func (q *RedisQueue) pendingKey() string {
	if q.cachedPendingKey != "" {
		return q.cachedPendingKey
	}
	return q.config.Prefix + ":queue:pending"
}

// pendingKeyForPriority returns the pending queue key for a specific priority.
func (q *RedisQueue) pendingKeyForPriority(priority Priority) string {
	switch priority {
	case PriorityHigh:
		if q.cachedPendingKeyHigh != "" {
			return q.cachedPendingKeyHigh
		}
		return q.config.Prefix + ":queue:pending:high"
	case PriorityLow:
		if q.cachedPendingKeyLow != "" {
			return q.cachedPendingKeyLow
		}
		return q.config.Prefix + ":queue:pending:low"
	default: // Normal and fallback
		if q.cachedPendingKeyNormal != "" {
			return q.cachedPendingKeyNormal
		}
		return q.config.Prefix + ":queue:pending:normal"
	}
}

// allPendingKeys returns all priority queue keys in order of priority (high first).
func (q *RedisQueue) allPendingKeys() []string {
	return []string{
		q.pendingKeyForPriority(PriorityHigh),
		q.pendingKeyForPriority(PriorityNormal),
		q.pendingKeyForPriority(PriorityLow),
	}
}
func (q *RedisQueue) processingKey() string {
	if q.cachedProcessingKey != "" {
		return q.cachedProcessingKey
	}
	return q.config.Prefix + ":queue:processing"
}
func (q *RedisQueue) failedKey() string {
	if q.cachedFailedKey != "" {
		return q.cachedFailedKey
	}
	return q.config.Prefix + ":queue:failed"
}
func (q *RedisQueue) sentKey() string {
	if q.cachedSentKey != "" {
		return q.cachedSentKey
	}
	return q.config.Prefix + ":queue:sent"
}
func (q *RedisQueue) messageKey(id string) string {
	if q.cachedMessagePrefix != "" {
		return q.cachedMessagePrefix + id
	}
	return q.config.Prefix + ":message:" + id
}
func (q *RedisQueue) statsKey() string {
	if q.cachedStatsKey != "" {
		return q.cachedStatsKey
	}
	return q.config.Prefix + ":stats"
}

// healthMonitor periodically checks Redis connection health.
func (q *RedisQueue) healthMonitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		if atomic.LoadInt32(&q.closed) == 1 {
			return
		}

		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := q.client.Ping(ctx).Err()
			cancel()

			if err != nil {
				// Connection issue detected - Redis client will auto-reconnect
				// Log this in production
				_ = err
			}
		}
	}
}

// isClosed safely checks if the queue is closed.
func (q *RedisQueue) isClosed() bool {
	return atomic.LoadInt32(&q.closed) == 1
}

// validateContext ensures context is valid and queue is open.
func (q *RedisQueue) validateContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context is nil")
	}
	if q.isClosed() {
		return ErrQueueClosed
	}
	return nil
}

// Enqueue adds a message to the queue for delivery.
func (q *RedisQueue) Enqueue(ctx context.Context, msg *Message) error {
	if err := q.validateContext(ctx); err != nil {
		return err
	}

	q.wg.Add(1)
	defer q.wg.Done()

	if msg == nil {
		return errors.New("message is nil")
	}
	if msg.ID == "" {
		msg.ID = generateMessageID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}
	if msg.NextAttempt.IsZero() {
		msg.NextAttempt = time.Now()
	}
	if msg.MaxAttempts == 0 {
		msg.MaxAttempts = q.config.MaxRetries
	}
	if msg.Priority == "" {
		msg.Priority = PriorityNormal
	}
	msg.Status = StatusPending

	// Store message data
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	// Determine which queue to use based on priority
	queueKey := q.pendingKeyForPriority(msg.Priority)

	// Use transaction to ensure atomicity with retry on transient errors
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		pipe := q.client.TxPipeline()
		pipe.Set(ctx, q.messageKey(msg.ID), data, 0)
		pipe.ZAdd(ctx, queueKey, redis.Z{
			Score:  float64(msg.NextAttempt.UnixNano()),
			Member: msg.ID,
		})
		pipe.HIncrBy(ctx, q.statsKey(), "enqueued", 1)
		pipe.HIncrBy(ctx, q.statsKey(), "enqueued:"+string(msg.Priority), 1)

		_, err = pipe.Exec(ctx)
		if err == nil {
			return nil
		}

		// Check if error is transient
		if !isTransientRedisError(err) {
			return fmt.Errorf("failed to enqueue message: %w", err)
		}

		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}

	return fmt.Errorf("failed to enqueue message after %d retries: %w", maxRetries, err)
}

// Dequeue retrieves the next message ready for delivery.
// Returns nil if no messages are ready.
// Checks priority queues in order: high, normal, low.
func (q *RedisQueue) Dequeue(ctx context.Context) (*Message, error) {
	if err := q.validateContext(ctx); err != nil {
		return nil, err
	}

	q.wg.Add(1)
	defer q.wg.Done()

	now := float64(time.Now().UnixNano())

	// Check priority queues in order: high, normal, low
	var results []redis.Z
	var sourceQueue string

	for _, queueKey := range q.allPendingKeys() {
		qResults, err := q.client.ZRangeByScoreWithScores(ctx, queueKey, &redis.ZRangeBy{
			Min:   "-inf",
			Max:   fmt.Sprintf("%f", now),
			Count: 1,
		}).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to query pending queue %s: %w", queueKey, err)
		}

		if len(qResults) > 0 {
			results = qResults
			sourceQueue = queueKey
			break
		}
	}

	// Also check the legacy pending queue for backward compatibility
	if len(results) == 0 {
		legacyResults, err := q.client.ZRangeByScoreWithScores(ctx, q.pendingKey(), &redis.ZRangeBy{
			Min:   "-inf",
			Max:   fmt.Sprintf("%f", now),
			Count: 1,
		}).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to query pending queue: %w", err)
		}
		if len(legacyResults) > 0 {
			results = legacyResults
			sourceQueue = q.pendingKey()
		}
	}

	if len(results) == 0 {
		return nil, nil
	}

	msgID, ok := results[0].Member.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected member type in pending queue: %T", results[0].Member)
	}

	// Atomically move to processing queue
	pipe := q.client.TxPipeline()
	pipe.ZRem(ctx, sourceQueue, msgID)
	pipe.SAdd(ctx, q.processingKey(), msgID)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to move message to processing: %w", err)
	}

	// Get message data
	msg, err := q.GetMessage(ctx, msgID)
	if err != nil {
		// Put it back atomically if we can't get the data
		rollbackPipe := q.client.TxPipeline()
		rollbackPipe.SRem(ctx, q.processingKey(), msgID)
		rollbackPipe.ZAdd(ctx, sourceQueue, redis.Z{
			Score:  results[0].Score,
			Member: msgID,
		})
		if _, rbErr := rollbackPipe.Exec(ctx); rbErr != nil {
			// Log rollback failure in production
			return nil, fmt.Errorf("failed to get message %s and rollback failed: %w (rollback error: %v)", msgID, err, rbErr)
		}
		return nil, err
	}

	msg.Status = StatusSending
	msg.Attempts++
	msg.LastAttempt = time.Now()

	// Update message status
	if err := q.updateMessage(ctx, msg); err != nil {
		// Attempt rollback
		rollbackPipe := q.client.TxPipeline()
		rollbackPipe.SRem(ctx, q.processingKey(), msgID)
		rollbackPipe.ZAdd(ctx, sourceQueue, redis.Z{
			Score:  results[0].Score,
			Member: msgID,
		})
		rollbackPipe.Exec(ctx)
		return nil, err
	}

	return msg, nil
}

// Complete marks a message as successfully delivered.
func (q *RedisQueue) Complete(ctx context.Context, msgID string) error {
	if err := q.validateContext(ctx); err != nil {
		return err
	}

	q.wg.Add(1)
	defer q.wg.Done()

	msg, err := q.GetMessage(ctx, msgID)
	if err != nil {
		return err
	}

	msg.Status = StatusSent

	pipe := q.client.TxPipeline()
	pipe.SRem(ctx, q.processingKey(), msgID)
	pipe.ZAdd(ctx, q.sentKey(), redis.Z{
		Score:  float64(time.Now().UnixNano()),
		Member: msgID,
	})
	pipe.HIncrBy(ctx, q.statsKey(), "sent", 1)

	// Update message data
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	pipe.Set(ctx, q.messageKey(msgID), data, 7*24*time.Hour) // Keep sent messages for 7 days

	_, err = pipe.Exec(ctx)
	return err
}

// Retry schedules a message for retry with exponential backoff.
func (q *RedisQueue) Retry(ctx context.Context, msgID string, lastError error) error {
	msg, err := q.GetMessage(ctx, msgID)
	if err != nil {
		return err
	}

	msg.LastError = lastError.Error()

	// Check if we should give up
	if msg.Attempts >= msg.MaxAttempts {
		return q.Fail(ctx, msgID, "max attempts exceeded")
	}

	// Check if message is too old
	if time.Since(msg.CreatedAt) > q.config.RetryMaxAge {
		return q.Fail(ctx, msgID, "message expired")
	}

	// Calculate next retry time with exponential backoff + jitter
	msg.NextAttempt = calculateNextRetry(msg.Attempts)
	msg.Status = StatusDeferred

	pipe := q.client.TxPipeline()
	pipe.SRem(ctx, q.processingKey(), msgID)
	pipe.ZAdd(ctx, q.pendingKeyForPriority(msg.Priority), redis.Z{
		Score:  float64(msg.NextAttempt.UnixNano()),
		Member: msgID,
	})
	pipe.HIncrBy(ctx, q.statsKey(), "retried", 1)

	// Update message data
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	pipe.Set(ctx, q.messageKey(msgID), data, 0)

	_, err = pipe.Exec(ctx)
	return err
}

// Fail permanently fails a message (no more retries).
func (q *RedisQueue) Fail(ctx context.Context, msgID string, reason string) error {
	msg, err := q.GetMessage(ctx, msgID)
	if err != nil {
		return err
	}

	msg.Status = StatusFailed
	msg.LastError = reason

	pipe := q.client.TxPipeline()
	pipe.SRem(ctx, q.processingKey(), msgID)
	pipe.ZAdd(ctx, q.failedKey(), redis.Z{
		Score:  float64(time.Now().UnixNano()),
		Member: msgID,
	})
	pipe.HIncrBy(ctx, q.statsKey(), "failed", 1)

	// Update message data
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	pipe.Set(ctx, q.messageKey(msgID), data, 30*24*time.Hour) // Keep failed messages for 30 days

	_, err = pipe.Exec(ctx)
	return err
}

// GetMessage retrieves a message by ID.
func (q *RedisQueue) GetMessage(ctx context.Context, msgID string) (*Message, error) {
	data, err := q.client.Get(ctx, q.messageKey(msgID)).Bytes()
	if err == redis.Nil {
		return nil, ErrMessageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return &msg, nil
}

// updateMessage updates message data in Redis.
func (q *RedisQueue) updateMessage(ctx context.Context, msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return q.client.Set(ctx, q.messageKey(msg.ID), data, 0).Err()
}

// Stats returns queue statistics.
func (q *RedisQueue) Stats(ctx context.Context) (*QueueStats, error) {
	pipe := q.client.TxPipeline()
	pendingCmd := pipe.ZCard(ctx, q.pendingKey())
	pendingHighCmd := pipe.ZCard(ctx, q.pendingKeyForPriority(PriorityHigh))
	pendingNormalCmd := pipe.ZCard(ctx, q.pendingKeyForPriority(PriorityNormal))
	pendingLowCmd := pipe.ZCard(ctx, q.pendingKeyForPriority(PriorityLow))
	processingCmd := pipe.SCard(ctx, q.processingKey())
	sentCmd := pipe.ZCard(ctx, q.sentKey())
	failedCmd := pipe.ZCard(ctx, q.failedKey())
	statsCmd := pipe.HGetAll(ctx, q.statsKey())

	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	stats := &QueueStats{
		Pending:       pendingCmd.Val() + pendingHighCmd.Val() + pendingNormalCmd.Val() + pendingLowCmd.Val(),
		PendingHigh:   pendingHighCmd.Val(),
		PendingNormal: pendingNormalCmd.Val(),
		PendingLow:    pendingLowCmd.Val(),
		Processing:    processingCmd.Val(),
		Sent:          sentCmd.Val(),
		Failed:        failedCmd.Val(),
	}

	counters := statsCmd.Val()
	if v, ok := counters["enqueued"]; ok {
		fmt.Sscanf(v, "%d", &stats.TotalEnqueued)
	}
	if v, ok := counters["sent"]; ok {
		fmt.Sscanf(v, "%d", &stats.TotalSent)
	}
	if v, ok := counters["failed"]; ok {
		fmt.Sscanf(v, "%d", &stats.TotalFailed)
	}
	if v, ok := counters["retried"]; ok {
		fmt.Sscanf(v, "%d", &stats.TotalRetried)
	}

	return stats, nil
}

// QueueStats contains queue statistics.
type QueueStats struct {
	Pending       int64
	PendingHigh   int64 // Messages in high priority queue
	PendingNormal int64 // Messages in normal priority queue
	PendingLow    int64 // Messages in low priority queue
	Processing    int64
	Sent          int64
	Failed        int64
	TotalEnqueued int64
	TotalSent     int64
	TotalFailed   int64
	TotalRetried  int64
}

// PendingCount returns the number of messages waiting for delivery (across all priority queues).
func (q *RedisQueue) PendingCount(ctx context.Context) (int64, error) {
	pipe := q.client.TxPipeline()
	legacyCmd := pipe.ZCard(ctx, q.pendingKey())
	highCmd := pipe.ZCard(ctx, q.pendingKeyForPriority(PriorityHigh))
	normalCmd := pipe.ZCard(ctx, q.pendingKeyForPriority(PriorityNormal))
	lowCmd := pipe.ZCard(ctx, q.pendingKeyForPriority(PriorityLow))

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}

	return legacyCmd.Val() + highCmd.Val() + normalCmd.Val() + lowCmd.Val(), nil
}

// ProcessingCount returns the number of messages being processed.
func (q *RedisQueue) ProcessingCount(ctx context.Context) (int64, error) {
	return q.client.SCard(ctx, q.processingKey()).Result()
}

// ListPending returns pending messages up to limit.
func (q *RedisQueue) ListPending(ctx context.Context, limit int64) ([]*Message, error) {
	if err := q.validateContext(ctx); err != nil {
		return nil, err
	}

	results, err := q.client.ZRangeWithScores(ctx, q.pendingKey(), 0, limit-1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to query pending queue: %w", err)
	}

	messages := make([]*Message, 0, len(results))
	for _, r := range results {
		msgID, ok := r.Member.(string)
		if !ok {
			// Skip entries with unexpected type
			continue
		}
		msg, err := q.GetMessage(ctx, msgID)
		if err != nil {
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// ListFailed returns failed messages up to limit.
func (q *RedisQueue) ListFailed(ctx context.Context, limit int64) ([]*Message, error) {
	if err := q.validateContext(ctx); err != nil {
		return nil, err
	}

	results, err := q.client.ZRevRangeWithScores(ctx, q.failedKey(), 0, limit-1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to query failed queue: %w", err)
	}

	messages := make([]*Message, 0, len(results))
	for _, r := range results {
		msgID, ok := r.Member.(string)
		if !ok {
			// Skip entries with unexpected type
			continue
		}
		msg, err := q.GetMessage(ctx, msgID)
		if err != nil {
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// ListSent returns recently sent messages up to limit.
func (q *RedisQueue) ListSent(ctx context.Context, limit int64) ([]*Message, error) {
	if err := q.validateContext(ctx); err != nil {
		return nil, err
	}

	results, err := q.client.ZRevRangeWithScores(ctx, q.sentKey(), 0, limit-1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to query sent queue: %w", err)
	}

	messages := make([]*Message, 0, len(results))
	for _, r := range results {
		msgID, ok := r.Member.(string)
		if !ok {
			// Skip entries with unexpected type
			continue
		}
		msg, err := q.GetMessage(ctx, msgID)
		if err != nil {
			continue
		}
		messages = append(messages, msg)
	}

	return messages, nil
}

// RecoverStale moves messages stuck in processing back to pending.
// This handles cases where a worker crashed.
func (q *RedisQueue) RecoverStale(ctx context.Context, staleThreshold time.Duration) (int, error) {
	processing, err := q.client.SMembers(ctx, q.processingKey()).Result()
	if err != nil {
		return 0, err
	}

	recovered := 0
	for _, msgID := range processing {
		msg, err := q.GetMessage(ctx, msgID)
		if err != nil {
			continue
		}

		// Check if message has been processing too long
		if time.Since(msg.LastAttempt) > staleThreshold {
			// Move back to pending
			if err := q.Retry(ctx, msgID, errors.New("worker timeout")); err == nil {
				recovered++
			}
		}
	}

	return recovered, nil
}

// Cleanup removes old sent/failed messages.
func (q *RedisQueue) Cleanup(ctx context.Context, olderThan time.Duration) error {
	if err := q.validateContext(ctx); err != nil {
		return err
	}

	q.wg.Add(1)
	defer q.wg.Done()

	threshold := float64(time.Now().Add(-olderThan).UnixNano())

	// Remove old sent messages
	if err := q.client.ZRemRangeByScore(ctx, q.sentKey(), "-inf", fmt.Sprintf("%f", threshold)).Err(); err != nil {
		return fmt.Errorf("failed to cleanup sent messages: %w", err)
	}

	// Remove old failed messages
	if err := q.client.ZRemRangeByScore(ctx, q.failedKey(), "-inf", fmt.Sprintf("%f", threshold)).Err(); err != nil {
		return fmt.Errorf("failed to cleanup failed messages: %w", err)
	}

	return nil
}

// Client returns the underlying Redis client for health checks.
func (q *RedisQueue) Client() redis.UniversalClient {
	return q.client
}

// Close closes the Redis connection gracefully.
func (q *RedisQueue) Close() error {
	// Set closed flag atomically
	if !atomic.CompareAndSwapInt32(&q.closed, 0, 1) {
		// Already closed
		return nil
	}

	// Wait for in-flight operations to complete with timeout
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All operations completed
	case <-time.After(30 * time.Second):
		// Timeout - force close
		// Log timeout in production
	}

	return q.client.Close()
}

// isTransientRedisError checks if an error is transient and worth retrying.
func isTransientRedisError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Check for common transient errors
	return contains(errStr, "connection refused") ||
		contains(errStr, "timeout") ||
		contains(errStr, "connection reset") ||
		contains(errStr, "broken pipe") ||
		contains(errStr, "i/o timeout") ||
		contains(errStr, "network") ||
		contains(errStr, "EOF")
}

// contains checks if a string contains a substring.
// Uses optimized strings.Contains from standard library.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// Helper functions

// calculateNextRetry calculates the next retry time with exponential backoff.
func calculateNextRetry(attempts int) time.Time {
	// Retry intervals: 5m, 15m, 30m, 1h, 2h, 4h, 8h, 16h, 24h, then every 24h
	intervals := []time.Duration{
		5 * time.Minute,
		15 * time.Minute,
		30 * time.Minute,
		1 * time.Hour,
		2 * time.Hour,
		4 * time.Hour,
		8 * time.Hour,
		16 * time.Hour,
		24 * time.Hour,
	}

	idx := attempts - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(intervals) {
		idx = len(intervals) - 1
	}

	base := intervals[idx]

	// Add jitter: +/- 10%
	jitterRange := int64(base / 10)
	if jitterRange > 0 {
		jitter := time.Duration(time.Now().UnixNano()%jitterRange) - time.Duration(jitterRange/2)
		base += jitter
	}

	return time.Now().Add(base)
}

// generateMessageID generates a unique message ID.
// Uses buffer pool to reduce allocations.
func generateMessageID() string {
	bufPtr := idBufferPool.Get().(*[]byte)
	b := *bufPtr
	defer idBufferPool.Put(bufPtr)

	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if crypto fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(b))
}
