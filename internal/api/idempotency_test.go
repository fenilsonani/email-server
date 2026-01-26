package api

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, redis.UniversalClient) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return mr, client
}

func TestIdempotencyStore_CheckNewKey(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	store := NewIdempotencyStore(client, "test")
	ctx := context.Background()

	// Check a key that doesn't exist
	result, err := store.Check(ctx, 1, "new-key")
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result != nil {
		t.Error("Expected nil result for new key")
	}
}

func TestIdempotencyStore_StoreAndRetrieve(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	store := NewIdempotencyStore(client, "test")
	ctx := context.Background()

	// Store a result
	expected := &IdempotencyResult{
		MessageID:  "msg_123",
		Status:     "queued",
		StatusCode: 200,
		CreatedAt:  time.Now(),
	}

	err := store.Store(ctx, 1, "test-key", expected)
	if err != nil {
		t.Fatalf("Store returned error: %v", err)
	}

	// Retrieve it
	result, err := store.Check(ctx, 1, "test-key")
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected result, got nil")
	}
	if result.MessageID != expected.MessageID {
		t.Errorf("MessageID mismatch: got %s, want %s", result.MessageID, expected.MessageID)
	}
	if result.Status != expected.Status {
		t.Errorf("Status mismatch: got %s, want %s", result.Status, expected.Status)
	}
	if result.StatusCode != expected.StatusCode {
		t.Errorf("StatusCode mismatch: got %d, want %d", result.StatusCode, expected.StatusCode)
	}
}

func TestIdempotencyStore_Lock(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	store := NewIdempotencyStore(client, "test")
	ctx := context.Background()

	// First lock should succeed
	locked, err := store.Lock(ctx, 1, "lock-key")
	if err != nil {
		t.Fatalf("Lock returned error: %v", err)
	}
	if !locked {
		t.Error("Expected lock to succeed")
	}

	// Second lock should fail
	locked, err = store.Lock(ctx, 1, "lock-key")
	if err != nil {
		t.Fatalf("Second Lock returned error: %v", err)
	}
	if locked {
		t.Error("Expected second lock to fail")
	}

	// Check should return in-progress error
	_, err = store.Check(ctx, 1, "lock-key")
	if err != ErrIdempotencyKeyInProgress {
		t.Errorf("Expected ErrIdempotencyKeyInProgress, got %v", err)
	}
}

func TestIdempotencyStore_Unlock(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	store := NewIdempotencyStore(client, "test")
	ctx := context.Background()

	// Lock
	locked, _ := store.Lock(ctx, 1, "unlock-key")
	if !locked {
		t.Fatal("Expected lock to succeed")
	}

	// Unlock
	err := store.Unlock(ctx, 1, "unlock-key")
	if err != nil {
		t.Fatalf("Unlock returned error: %v", err)
	}

	// Should be able to lock again
	locked, _ = store.Lock(ctx, 1, "unlock-key")
	if !locked {
		t.Error("Expected lock to succeed after unlock")
	}
}

func TestIdempotencyStore_EmptyKey(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	store := NewIdempotencyStore(client, "test")
	ctx := context.Background()

	// Empty key should always return nil (no idempotency)
	result, err := store.Check(ctx, 1, "")
	if err != nil {
		t.Fatalf("Check with empty key returned error: %v", err)
	}
	if result != nil {
		t.Error("Expected nil result for empty key")
	}

	// Lock with empty key should succeed (no-op)
	locked, err := store.Lock(ctx, 1, "")
	if err != nil {
		t.Fatalf("Lock with empty key returned error: %v", err)
	}
	if !locked {
		t.Error("Expected lock with empty key to succeed")
	}

	// Store with empty key should succeed (no-op)
	err = store.Store(ctx, 1, "", &IdempotencyResult{})
	if err != nil {
		t.Fatalf("Store with empty key returned error: %v", err)
	}
}

func TestIdempotencyStore_DomainIsolation(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	store := NewIdempotencyStore(client, "test")
	ctx := context.Background()

	// Store for domain 1
	err := store.Store(ctx, 1, "shared-key", &IdempotencyResult{
		MessageID: "msg_domain1",
		Status:    "queued",
	})
	if err != nil {
		t.Fatalf("Store for domain 1 failed: %v", err)
	}

	// Store for domain 2 with same key
	err = store.Store(ctx, 2, "shared-key", &IdempotencyResult{
		MessageID: "msg_domain2",
		Status:    "queued",
	})
	if err != nil {
		t.Fatalf("Store for domain 2 failed: %v", err)
	}

	// Retrieve for domain 1
	result1, err := store.Check(ctx, 1, "shared-key")
	if err != nil {
		t.Fatalf("Check for domain 1 failed: %v", err)
	}
	if result1.MessageID != "msg_domain1" {
		t.Errorf("Domain 1 got wrong message: %s", result1.MessageID)
	}

	// Retrieve for domain 2
	result2, err := store.Check(ctx, 2, "shared-key")
	if err != nil {
		t.Fatalf("Check for domain 2 failed: %v", err)
	}
	if result2.MessageID != "msg_domain2" {
		t.Errorf("Domain 2 got wrong message: %s", result2.MessageID)
	}
}
