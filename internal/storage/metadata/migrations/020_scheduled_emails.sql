-- Migration 020: Scheduled Emails (Placeholder)
-- Adds table for managing emails scheduled for future delivery
-- This migration is reserved for scheduled email functionality

-- Note: Scheduled emails table would normally be created here
-- CREATE TABLE IF NOT EXISTS scheduled_emails (...)

-- For now, we skip this migration as the feature requires additional setup
-- The core email functionality works without scheduled emails

-- Add delivery_attempts table for tracking individual delivery attempts
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
