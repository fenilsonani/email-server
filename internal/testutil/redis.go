package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// WithTestRedis creates an in-memory Redis instance for testing.
// It manages the lifecycle and provides cleanup via t.Cleanup().
func WithTestRedis(t *testing.T, fn func(client *redis.Client)) {
	t.Helper()

	// Create an in-memory Redis server
	mr := miniredis.NewMiniRedis()
	if err := mr.Start(); err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}
	t.Cleanup(func() {
		mr.Close()
	})

	// Create a Redis client connected to the test server
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Failed to connect to test Redis: %v", err)
	}

	t.Cleanup(func() {
		client.Close()
	})

	fn(client)
}

// TestRedisClient creates a Redis client connected to miniredis for testing.
// Returns the client and a cleanup function.
func TestRedisClient(t *testing.T) (*redis.Client, func()) {
	t.Helper()

	mr := miniredis.NewMiniRedis()
	if err := mr.Start(); err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	cleanup := func() {
		client.Close()
		mr.Close()
	}

	return client, cleanup
}

// RedisKeyExists checks if a key exists in Redis.
func RedisKeyExists(t *testing.T, client *redis.Client, key string) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exists, err := client.Exists(ctx, key).Result()
	if err != nil {
		t.Fatalf("Failed to check Redis key: %v", err)
	}
	return exists > 0
}

// RedisGetString retrieves a string value from Redis.
func RedisGetString(t *testing.T, client *redis.Client, key string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	val, err := client.Get(ctx, key).Result()
	if err != nil {
		t.Fatalf("Failed to get Redis key %q: %v", key, err)
	}
	return val
}

// RedisSetString sets a string value in Redis.
func RedisSetString(t *testing.T, client *redis.Client, key, value string, ttl time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Set(ctx, key, value, ttl).Err(); err != nil {
		t.Fatalf("Failed to set Redis key %q: %v", key, err)
	}
}

// RedisDeleteKey deletes a key from Redis.
func RedisDeleteKey(t *testing.T, client *redis.Client, key string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Del(ctx, key).Err(); err != nil {
		t.Fatalf("Failed to delete Redis key %q: %v", key, err)
	}
}

// RedisFlushAll flushes all data from Redis.
func RedisFlushAll(t *testing.T, client *redis.Client) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.FlushAll(ctx).Err(); err != nil {
		t.Fatalf("Failed to flush Redis: %v", err)
	}
}

// RedisPushList pushes values to a Redis list.
func RedisPushList(t *testing.T, client *redis.Client, key string, values ...interface{}) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.RPush(ctx, key, values...).Err(); err != nil {
		t.Fatalf("Failed to push to Redis list %q: %v", key, err)
	}
}

// RedisGetList retrieves all values from a Redis list.
func RedisGetList(t *testing.T, client *redis.Client, key string) []string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	vals, err := client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		t.Fatalf("Failed to get Redis list %q: %v", key, err)
	}
	return vals
}

// RedisSetHash sets hash field values in Redis.
func RedisSetHash(t *testing.T, client *redis.Client, key string, fields map[string]interface{}) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.HSet(ctx, key, fields).Err(); err != nil {
		t.Fatalf("Failed to set Redis hash %q: %v", key, err)
	}
}

// RedisGetHash retrieves all fields from a Redis hash.
func RedisGetHash(t *testing.T, client *redis.Client, key string) map[string]string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	vals, err := client.HGetAll(ctx, key).Result()
	if err != nil {
		t.Fatalf("Failed to get Redis hash %q: %v", key, err)
	}
	return vals
}

// RedisAssertKeyValue asserts a key has the expected value in Redis.
func RedisAssertKeyValue(t *testing.T, client *redis.Client, key, expectedValue string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	actual, err := client.Get(ctx, key).Result()
	if err != nil {
		t.Errorf("Failed to get Redis key %q: %v", key, err)
		return
	}

	if actual != expectedValue {
		t.Errorf("Redis key %q = %q, want %q", key, actual, expectedValue)
	}
}

// RedisAssertKeyNotExists asserts a key does not exist in Redis.
func RedisAssertKeyNotExists(t *testing.T, client *redis.Client, key string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	exists, err := client.Exists(ctx, key).Result()
	if err != nil {
		t.Errorf("Failed to check Redis key %q: %v", key, err)
		return
	}

	if exists > 0 {
		t.Errorf("Redis key %q should not exist", key)
	}
}

// RedisWaitForKeyTTL waits for a key's TTL to be set, useful for verifying expiration.
func RedisWaitForKeyTTL(t *testing.T, client *redis.Client, key string, expectedTTL time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout.C:
			t.Errorf("Timeout waiting for key %q TTL", key)
			return
		case <-ticker.C:
			ttl, err := client.TTL(ctx, key).Result()
			if err != nil {
				continue
			}
			if ttl > 0 && ttl <= expectedTTL {
				return
			}
		}
	}
}

// RedisMeasureOperationLatency measures the latency of a Redis operation.
func RedisMeasureOperationLatency(t *testing.T, client *redis.Client, operation func(context.Context) error) time.Duration {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	if err := operation(ctx); err != nil {
		t.Fatalf("Redis operation failed: %v", err)
	}
	return time.Since(start)
}

// RedisAssertStringLength asserts a Redis string has the expected length.
func RedisAssertStringLength(t *testing.T, client *redis.Client, key string, expectedLength int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	val, err := client.Get(ctx, key).Result()
	if err != nil {
		t.Errorf("Failed to get Redis key %q: %v", key, err)
		return
	}

	if len(val) != expectedLength {
		t.Errorf("Redis key %q length = %d, want %d", key, len(val), expectedLength)
	}
}

// RedisAssertListLength asserts a Redis list has the expected length.
func RedisAssertListLength(t *testing.T, client *redis.Client, key string, expectedLength int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	length, err := client.LLen(ctx, key).Result()
	if err != nil {
		t.Errorf("Failed to get Redis list length %q: %v", key, err)
		return
	}

	if int(length) != expectedLength {
		t.Errorf("Redis list %q length = %d, want %d", key, length, expectedLength)
	}
}

// RedisTransactionTest performs a Redis transaction test.
func RedisTransactionTest(t *testing.T, client *redis.Client, testFunc func(*redis.Tx) error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Watch(ctx, func(tx *redis.Tx) error {
		return testFunc(tx)
	})

	if err != nil && err != redis.TxFailedErr {
		t.Fatalf("Redis transaction failed: %v", err)
	}
}

// RedisStreamWrite writes data to a Redis stream.
func RedisStreamWrite(t *testing.T, client *redis.Client, streamKey string, data map[string]interface{}) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	id, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: data,
	}).Result()

	if err != nil {
		t.Fatalf("Failed to write to Redis stream %q: %v", streamKey, err)
	}

	return id
}

// RedisStreamRead reads data from a Redis stream.
func RedisStreamRead(t *testing.T, client *redis.Client, streamKey string) []redis.XMessage {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages, err := client.XRange(ctx, streamKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("Failed to read from Redis stream %q: %v", streamKey, err)
	}

	return messages
}

// RedisExpireKey sets an expiration time for a key.
func RedisExpireKey(t *testing.T, client *redis.Client, key string, ttl time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Expire(ctx, key, ttl).Err(); err != nil {
		t.Fatalf("Failed to set Redis key expiration %q: %v", key, err)
	}
}

// RedisGetTTL gets the remaining TTL for a key.
func RedisGetTTL(t *testing.T, client *redis.Client, key string) time.Duration {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("Failed to get Redis key TTL %q: %v", key, err)
	}

	return ttl
}

// RedisIncrementKey increments an integer value in Redis.
func RedisIncrementKey(t *testing.T, client *redis.Client, key string) int64 {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	val, err := client.Incr(ctx, key).Result()
	if err != nil {
		t.Fatalf("Failed to increment Redis key %q: %v", key, err)
	}

	return val
}

// RedisDecrementKey decrements an integer value in Redis.
func RedisDecrementKey(t *testing.T, client *redis.Client, key string) int64 {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	val, err := client.Decr(ctx, key).Result()
	if err != nil {
		t.Fatalf("Failed to decrement Redis key %q: %v", key, err)
	}

	return val
}

// RedisAssertCounterValue asserts a Redis counter has the expected value.
func RedisAssertCounterValue(t *testing.T, client *redis.Client, key string, expectedValue int64) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	val, err := client.Get(ctx, key).Int64()
	if err != nil {
		t.Errorf("Failed to get Redis counter %q: %v", key, err)
		return
	}

	if val != expectedValue {
		t.Errorf("Redis counter %q = %d, want %d", key, val, expectedValue)
	}
}

// RedisBatchOperation performs multiple Redis operations as a batch.
func RedisBatchOperation(t *testing.T, client *redis.Client, operations func() error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use pipeline for batch operations
	pipe := client.Pipeline()

	if err := operations(); err != nil {
		t.Fatalf("Failed to prepare Redis pipeline: %v", err)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		t.Fatalf("Failed to execute Redis pipeline: %v", err)
	}
}

// RedisDebugInfo returns debug information about a Redis key.
func RedisDebugInfo(t *testing.T, client *redis.Client, key string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	keyType, err := client.Type(ctx, key).Result()
	if err != nil {
		return fmt.Sprintf("Error getting key type: %v", err)
	}

	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		return fmt.Sprintf("Error getting TTL: %v", err)
	}

	return fmt.Sprintf("Key: %s, Type: %s, TTL: %v", key, keyType, ttl)
}
