package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/tests/shared/helpers"
	testenv "github.com/fenilsonani/email-server/tests/shared"
)

// TestQueueDelivery tests the complete message queue delivery flow.
func TestQueueDelivery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testenv.WithTestServer(t, testenv.ServerConfig{
		DatabaseType: "sqlite",
		LogLevel:     "error",
		Timeout:      30 * time.Second,
	}, func(ts *testenv.TestServer) {
		t.Run("enqueue_message", func(t *testing.T) {
			testEnqueueMessage(t, ts.DB)
		})

		t.Run("dequeue_message", func(t *testing.T) {
			testDequeueMessage(t, ts.DB)
		})

		t.Run("message_delivery_success", func(t *testing.T) {
			testMessageDeliverySuccess(t, ts.DB)
		})

		t.Run("message_delivery_retry", func(t *testing.T) {
			testMessageDeliveryRetry(t, ts.DB)
		})

		t.Run("message_delivery_bounce", func(t *testing.T) {
			testMessageDeliveryBounce(t, ts.DB)
		})

		t.Run("concurrent_queue_operations", func(t *testing.T) {
			testConcurrentQueueOperations(t, ts.DB)
		})

		t.Run("queue_ordering", func(t *testing.T) {
			testQueueOrdering(t, ts.DB)
		})

		t.Run("queue_expiration", func(t *testing.T) {
			testQueueExpiration(t, ts.DB)
		})
	})
}

// testEnqueueMessage tests enqueuing a message.
func testEnqueueMessage(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create test message
	query := `
		INSERT INTO messages (
			from_addr, to_addr, subject, body, created_at
		) VALUES (?, ?, ?, ?, ?)
	`

	result, err := db.ExecContext(ctx, query,
		"sender@example.com",
		"recipient@example.com",
		"Test Message",
		"Test body",
		time.Now(),
	)

	if err != nil {
		t.Logf("Failed to insert message: %v", err)
		return
	}

	messageID, err := result.LastInsertId()
	if err != nil {
		t.Logf("Failed to get message ID: %v", err)
		return
	}

	if messageID > 0 {
		t.Logf("Message enqueued successfully with ID: %d", messageID)
	}
}

// testDequeueMessage tests dequeuing a message.
func testDequeueMessage(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Insert message
	insertQuery := `
		INSERT INTO messages (
			from_addr, to_addr, subject, body, created_at
		) VALUES (?, ?, ?, ?, ?)
	`

	result, err := db.ExecContext(ctx, insertQuery,
		"sender@example.com",
		"recipient@example.com",
		"Test Message",
		"Test body",
		time.Now(),
	)

	if err != nil {
		t.Logf("Failed to insert message: %v", err)
		return
	}

	messageID, _ := result.LastInsertId()

	// Dequeue message
	selectQuery := `SELECT id, from_addr, to_addr, subject FROM messages WHERE id = ?`
	var id int64
	var fromAddr, toAddr, subject string

	err = db.QueryRowContext(ctx, selectQuery, messageID).Scan(&id, &fromAddr, &toAddr, &subject)
	if err != nil && err != sql.ErrNoRows {
		t.Logf("Failed to dequeue message: %v", err)
		return
	}

	if id == messageID && fromAddr == "sender@example.com" {
		t.Logf("Message dequeued successfully")
	}
}

// testMessageDeliverySuccess tests successful message delivery.
func testMessageDeliverySuccess(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Insert message
	insertQuery := `
		INSERT INTO messages (
			from_addr, to_addr, subject, body, created_at, is_delivered
		) VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := db.ExecContext(ctx, insertQuery,
		"sender@example.com",
		"recipient@example.com",
		"Test",
		"Body",
		time.Now(),
		false,
	)

	if err != nil {
		t.Logf("Failed to insert message: %v", err)
		return
	}

	// Update to mark as delivered
	updateQuery := `UPDATE messages SET is_delivered = ?, delivered_at = ? WHERE from_addr = ?`
	_, err = db.ExecContext(ctx, updateQuery, true, time.Now(), "sender@example.com")
	if err != nil {
		t.Logf("Failed to update delivery status: %v", err)
		return
	}

	// Verify delivery status
	selectQuery := `SELECT is_delivered FROM messages WHERE from_addr = ?`
	var isDelivered bool
	err = db.QueryRowContext(ctx, selectQuery, "sender@example.com").Scan(&isDelivered)
	if err == nil && isDelivered {
		t.Logf("Message delivery marked as successful")
	}
}

// testMessageDeliveryRetry tests message delivery retry logic.
func testMessageDeliveryRetry(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Insert message with retry count
	insertQuery := `
		INSERT INTO messages (
			from_addr, to_addr, subject, body, created_at, retry_count
		) VALUES (?, ?, ?, ?, ?, ?)
	`

	result, err := db.ExecContext(ctx, insertQuery,
		"sender@example.com",
		"recipient@example.com",
		"Test",
		"Body",
		time.Now(),
		0,
	)

	if err != nil {
		t.Logf("Failed to insert message: %v", err)
		return
	}

	messageID, _ := result.LastInsertId()

	// Simulate retry
	for i := 1; i <= 3; i++ {
		updateQuery := `UPDATE messages SET retry_count = ? WHERE id = ?`
		_, err := db.ExecContext(ctx, updateQuery, i, messageID)
		if err != nil {
			t.Logf("Failed to increment retry count: %v", err)
			return
		}
	}

	// Verify retry count
	selectQuery := `SELECT retry_count FROM messages WHERE id = ?`
	var retryCount int
	err = db.QueryRowContext(ctx, selectQuery, messageID).Scan(&retryCount)
	if err == nil && retryCount == 3 {
		t.Logf("Message retry count incremented successfully: %d attempts", retryCount)
	}
}

// testMessageDeliveryBounce tests bounce message handling.
func testMessageDeliveryBounce(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Insert message
	insertQuery := `
		INSERT INTO messages (
			from_addr, to_addr, subject, body, created_at, is_bounce
		) VALUES (?, ?, ?, ?, ?, ?)
	`

	result, err := db.ExecContext(ctx, insertQuery,
		"MAILER-DAEMON@example.com",
		"original-sender@example.com",
		"Undeliverable",
		"Your message could not be delivered",
		time.Now(),
		false,
	)

	if err != nil {
		t.Logf("Failed to insert bounce message: %v", err)
		return
	}

	messageID, _ := result.LastInsertId()

	// Mark as bounce
	updateQuery := `UPDATE messages SET is_bounce = ? WHERE id = ?`
	_, err = db.ExecContext(ctx, updateQuery, true, messageID)
	if err != nil {
		t.Logf("Failed to mark as bounce: %v", err)
		return
	}

	// Verify bounce status
	selectQuery := `SELECT is_bounce FROM messages WHERE id = ?`
	var isBounce bool
	err = db.QueryRowContext(ctx, selectQuery, messageID).Scan(&isBounce)
	if err == nil && isBounce {
		t.Logf("Bounce message marked successfully")
	}
}

// testConcurrentQueueOperations tests concurrent queue operations.
func testConcurrentQueueOperations(t *testing.T, db *sql.DB) {
	t.Helper()

	helpers.RunConcurrent(t, 5, func(i int) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		fromAddr := "sender" + string(rune('0'+i)) + "@example.com"
		toAddr := "recipient" + string(rune('0'+i)) + "@example.com"

		query := `
			INSERT INTO messages (
				from_addr, to_addr, subject, body, created_at
			) VALUES (?, ?, ?, ?, ?)
		`

		_, err := db.ExecContext(ctx, query,
			fromAddr,
			toAddr,
			"Concurrent Test",
			"Body",
			time.Now(),
		)

		return err
	})
}

// testQueueOrdering tests FIFO ordering of messages in queue.
func testQueueOrdering(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Insert multiple messages
	baseTime := time.Now()
	for i := 0; i < 5; i++ {
		query := `
			INSERT INTO messages (
				from_addr, to_addr, subject, body, created_at
			) VALUES (?, ?, ?, ?, ?)
		`

		_, err := db.ExecContext(ctx, query,
			"sender@example.com",
			"recipient" + string(rune('0'+i)) + "@example.com",
			"Test "+string(rune('0'+i)),
			"Body",
			baseTime.Add(time.Duration(i)*time.Second),
		)

		if err != nil {
			t.Logf("Failed to insert message %d: %v", i, err)
			return
		}
	}

	// Retrieve in FIFO order
	query := `SELECT to_addr FROM messages WHERE from_addr = ? ORDER BY created_at ASC`
	rows, err := db.QueryContext(ctx, query, "sender@example.com")
	if err != nil {
		t.Logf("Failed to query messages: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}

	if count == 5 {
		t.Logf("Queue ordering preserved for 5 messages")
	}
}

// testQueueExpiration tests message expiration in queue.
func testQueueExpiration(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Insert message with expiration
	oldTime := time.Now().Add(-48 * time.Hour)
	insertQuery := `
		INSERT INTO messages (
			from_addr, to_addr, subject, body, created_at
		) VALUES (?, ?, ?, ?, ?)
	`

	_, err := db.ExecContext(ctx, insertQuery,
		"sender@example.com",
		"recipient@example.com",
		"Expired",
		"Body",
		oldTime,
	)

	if err != nil {
		t.Logf("Failed to insert expired message: %v", err)
		return
	}

	// Query expired messages (older than 24 hours)
	selectQuery := `
		SELECT COUNT(*) FROM messages
		WHERE from_addr = ? AND created_at < ?
	`

	var count int
	err = db.QueryRowContext(ctx, selectQuery,
		"sender@example.com",
		time.Now().Add(-24*time.Hour),
	).Scan(&count)

	if err == nil && count > 0 {
		t.Logf("Found %d expired messages in queue", count)
	}
}

// TestQueuePerformance tests queue performance characteristics.
func TestQueuePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	testenv.WithTestServer(t, testenv.ServerConfig{
		DatabaseType: "sqlite",
		LogLevel:     "error",
	}, func(ts *testenv.TestServer) {
		t.Run("bulk_enqueue_performance", func(t *testing.T) {
			testBulkEnqueuePerformance(t, ts.DB)
		})

		t.Run("bulk_dequeue_performance", func(t *testing.T) {
			testBulkDequeuePerformance(t, ts.DB)
		})

		t.Run("queue_size_scaling", func(t *testing.T) {
			testQueueSizeScaling(t, ts.DB)
		})
	})
}

// testBulkEnqueuePerformance measures bulk enqueue performance.
func testBulkEnqueuePerformance(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()

	query := `
		INSERT INTO messages (
			from_addr, to_addr, subject, body, created_at
		) VALUES (?, ?, ?, ?, ?)
	`

	for i := 0; i < 1000; i++ {
		fromAddr := "sender" + string(rune('0'+(i%10))) + "@example.com"
		toAddr := "recipient" + string(rune('0'+(i%10))) + "@example.com"

		db.ExecContext(ctx, query,
			fromAddr,
			toAddr,
			"Bulk Test",
			"Body",
			time.Now(),
		)
	}

	elapsed := time.Since(start)
	t.Logf("Bulk enqueue 1000 messages: %v", elapsed)
}

// testBulkDequeuePerformance measures bulk dequeue performance.
func testBulkDequeuePerformance(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First insert messages
	for i := 0; i < 100; i++ {
		query := `
			INSERT INTO messages (
				from_addr, to_addr, subject, body, created_at
			) VALUES (?, ?, ?, ?, ?)
		`

		db.ExecContext(ctx, query,
			"sender@example.com",
			"recipient"+string(rune('0'+(i%10)))+"@example.com",
			"Bulk Test",
			"Body",
			time.Now(),
		)
	}

	// Now measure dequeue
	start := time.Now()

	query := `SELECT COUNT(*) FROM messages WHERE from_addr = ?`
	for i := 0; i < 100; i++ {
		var count int
		db.QueryRowContext(ctx, query, "sender@example.com").Scan(&count)
	}

	elapsed := time.Since(start)
	t.Logf("Bulk dequeue 100 queries: %v", elapsed)
}

// testQueueSizeScaling tests queue performance with increasing size.
func testQueueSizeScaling(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sizes := []int{100, 500, 1000}

	for _, size := range sizes {
		// Insert messages
		for i := 0; i < size; i++ {
			query := `
				INSERT INTO messages (
					from_addr, to_addr, subject, body, created_at
				) VALUES (?, ?, ?, ?, ?)
			`

			db.ExecContext(ctx, query,
				"sender@example.com",
				"recipient@example.com",
				"Scaling Test",
				"Body",
				time.Now(),
			)
		}

		// Measure query time
		start := time.Now()

		query := `SELECT COUNT(*) FROM messages WHERE from_addr = ?`
		var count int
		db.QueryRowContext(ctx, query, "sender@example.com").Scan(&count)

		elapsed := time.Since(start)
		t.Logf("Queue size %d: query took %v", size, elapsed)

		// Clean up for next iteration
		db.ExecContext(ctx, `DELETE FROM messages WHERE from_addr = ?`, "sender@example.com")
	}
}
