package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// IdempotencyStore manages idempotency keys to prevent duplicate email sends.
// Keys are stored in Redis with a 24-hour TTL.
type IdempotencyStore struct {
	client redis.UniversalClient
	prefix string
	ttl    time.Duration
}

// IdempotencyResult stores the cached response for an idempotent request.
type IdempotencyResult struct {
	MessageID  string    `json:"message_id"`
	Status     string    `json:"status"`
	StatusCode int       `json:"status_code"`
	CreatedAt  time.Time `json:"created_at"`
}

// Common errors for idempotency operations.
var (
	ErrIdempotencyKeyExists     = errors.New("idempotency key already exists")
	ErrIdempotencyKeyInProgress = errors.New("request with this idempotency key is in progress")
)

const (
	// Default TTL for idempotency keys (24 hours).
	DefaultIdempotencyTTL = 24 * time.Hour
	// Lock TTL for in-progress requests (5 minutes).
	IdempotencyLockTTL = 5 * time.Minute
	// Prefix for idempotency keys.
	idempotencyKeyPrefix = "idempotency:"
	// Suffix for lock keys.
	idempotencyLockSuffix = ":lock"
)

// NewIdempotencyStore creates a new idempotency store using the provided Redis client.
func NewIdempotencyStore(client redis.UniversalClient, prefix string) *IdempotencyStore {
	if prefix == "" {
		prefix = "mail"
	}
	return &IdempotencyStore{
		client: client,
		prefix: prefix + ":" + idempotencyKeyPrefix,
		ttl:    DefaultIdempotencyTTL,
	}
}

// keyName returns the full Redis key for an idempotency key.
func (s *IdempotencyStore) keyName(domainID int64, key string) string {
	return fmt.Sprintf("%s%d:%s", s.prefix, domainID, key)
}

// lockKeyName returns the lock key for an idempotency key.
func (s *IdempotencyStore) lockKeyName(domainID int64, key string) string {
	return s.keyName(domainID, key) + idempotencyLockSuffix
}

// Check checks if an idempotency key exists and returns the cached result if found.
// Returns nil, nil if the key doesn't exist (new request).
// Returns result, nil if the key exists (replay response).
// Returns nil, ErrIdempotencyKeyInProgress if a request with this key is in progress.
func (s *IdempotencyStore) Check(ctx context.Context, domainID int64, key string) (*IdempotencyResult, error) {
	if key == "" {
		return nil, nil // No idempotency key provided, allow the request
	}

	fullKey := s.keyName(domainID, key)
	lockKey := s.lockKeyName(domainID, key)

	// Check if there's a lock (request in progress)
	exists, err := s.client.Exists(ctx, lockKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to check idempotency lock: %w", err)
	}
	if exists > 0 {
		return nil, ErrIdempotencyKeyInProgress
	}

	// Check if we have a cached result
	data, err := s.client.Get(ctx, fullKey).Bytes()
	if err == redis.Nil {
		return nil, nil // Key doesn't exist, new request
	}
	if err != nil {
		return nil, fmt.Errorf("failed to check idempotency key: %w", err)
	}

	var result IdempotencyResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal idempotency result: %w", err)
	}

	return &result, nil
}

// Lock acquires a lock for the idempotency key to indicate a request is in progress.
// Returns true if the lock was acquired, false if it already exists.
func (s *IdempotencyStore) Lock(ctx context.Context, domainID int64, key string) (bool, error) {
	if key == "" {
		return true, nil // No key, no lock needed
	}

	lockKey := s.lockKeyName(domainID, key)

	// Try to set the lock with NX (only if not exists)
	ok, err := s.client.SetNX(ctx, lockKey, time.Now().Unix(), IdempotencyLockTTL).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire idempotency lock: %w", err)
	}

	return ok, nil
}

// Unlock releases the lock for an idempotency key.
func (s *IdempotencyStore) Unlock(ctx context.Context, domainID int64, key string) error {
	if key == "" {
		return nil
	}

	lockKey := s.lockKeyName(domainID, key)
	_, err := s.client.Del(ctx, lockKey).Result()
	if err != nil {
		return fmt.Errorf("failed to release idempotency lock: %w", err)
	}
	return nil
}

// Store stores the result for an idempotency key.
// This also releases any existing lock.
func (s *IdempotencyStore) Store(ctx context.Context, domainID int64, key string, result *IdempotencyResult) error {
	if key == "" {
		return nil // No key, nothing to store
	}

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal idempotency result: %w", err)
	}

	fullKey := s.keyName(domainID, key)
	lockKey := s.lockKeyName(domainID, key)

	// Use transaction to store result and delete lock atomically
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, fullKey, data, s.ttl)
	pipe.Del(ctx, lockKey)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to store idempotency result: %w", err)
	}

	return nil
}

// Delete removes an idempotency key and its lock (for testing/cleanup).
func (s *IdempotencyStore) Delete(ctx context.Context, domainID int64, key string) error {
	if key == "" {
		return nil
	}

	fullKey := s.keyName(domainID, key)
	lockKey := s.lockKeyName(domainID, key)

	pipe := s.client.TxPipeline()
	pipe.Del(ctx, fullKey)
	pipe.Del(ctx, lockKey)

	_, err := pipe.Exec(ctx)
	return err
}
