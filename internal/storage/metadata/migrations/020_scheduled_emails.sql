-- Migration 020: Add API scheduled email columns and delivery attempts
-- The scheduled_emails table was created in migration 009 for user "Send Later".
-- This adds columns needed by the transactional API scheduler (domain_id, etc.).
-- ALTER TABLE ADD COLUMN is idempotent via the Go migration runner.

-- Add API scheduler columns to existing scheduled_emails table
ALTER TABLE scheduled_emails ADD COLUMN domain_id INTEGER REFERENCES domains(id) ON DELETE CASCADE;
ALTER TABLE scheduled_emails ADD COLUMN api_key_id INTEGER;
ALTER TABLE scheduled_emails ADD COLUMN message_id TEXT;
ALTER TABLE scheduled_emails ADD COLUMN request_payload TEXT;
ALTER TABLE scheduled_emails ADD COLUMN scheduled_at DATETIME;
ALTER TABLE scheduled_emails ADD COLUMN processed_at DATETIME;

-- Index for API scheduler queries (fetch pending scheduled emails by time)
CREATE INDEX IF NOT EXISTS idx_scheduled_emails_api ON scheduled_emails(status, scheduled_at);

-- Delivery attempts table for tracking individual delivery attempts
CREATE TABLE IF NOT EXISTS delivery_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sent_email_id INTEGER NOT NULL REFERENCES sent_emails(id) ON DELETE CASCADE,
    attempt_number INTEGER NOT NULL,
    attempted_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    status TEXT NOT NULL CHECK(status IN ('pending', 'sent', 'deferred', 'failed', 'bounced')),
    smtp_response TEXT,
    error_message TEXT
);

-- Index for looking up attempts by sent_email_id
CREATE INDEX IF NOT EXISTS idx_delivery_attempts_sent_email ON delivery_attempts(sent_email_id);

-- Record migration version
INSERT OR IGNORE INTO schema_migrations (version) VALUES (20);
