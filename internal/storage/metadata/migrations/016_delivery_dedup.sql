-- Migration 016: Delivery Deduplication
-- Adds table for tracking message delivery state to prevent double delivery after crashes

-- Delivery deduplication table
CREATE TABLE IF NOT EXISTS delivery_dedup (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    smtp_message_id TEXT NOT NULL,
    queue_id TEXT NOT NULL,
    recipients TEXT,  -- JSON array of recipients
    state TEXT NOT NULL DEFAULT 'pending',  -- pending, delivering, delivered, failed
    worker_id TEXT,
    smtp_response TEXT,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);

-- Unique constraint on SMTP Message-ID to prevent duplicates
CREATE UNIQUE INDEX IF NOT EXISTS idx_dedup_smtp_message_id ON delivery_dedup(smtp_message_id);

-- Index for finding pending/delivering entries for recovery
CREATE INDEX IF NOT EXISTS idx_dedup_state ON delivery_dedup(state);

-- Index for cleanup of old entries
CREATE INDEX IF NOT EXISTS idx_dedup_started ON delivery_dedup(started_at);

-- Index for finding entries by queue ID
CREATE INDEX IF NOT EXISTS idx_dedup_queue_id ON delivery_dedup(queue_id);

INSERT INTO schema_migrations (version) VALUES (16);
