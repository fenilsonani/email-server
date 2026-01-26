-- Migration 020: Scheduled Emails
-- Adds table for managing emails scheduled for future delivery

-- Scheduled emails table
CREATE TABLE IF NOT EXISTS scheduled_emails (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    api_key_id INTEGER NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    message_id TEXT NOT NULL,
    request_payload TEXT NOT NULL,  -- JSON serialized SendEmailRequest
    scheduled_at DATETIME NOT NULL,
    status TEXT DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'sent', 'failed', 'cancelled')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    processed_at DATETIME
);

-- Index for finding pending scheduled emails
CREATE INDEX IF NOT EXISTS idx_scheduled_emails_pending ON scheduled_emails(status, scheduled_at)
    WHERE status = 'pending';

-- Index for domain lookups
CREATE INDEX IF NOT EXISTS idx_scheduled_emails_domain ON scheduled_emails(domain_id);

-- Index for message_id lookups (for cancellation)
CREATE INDEX IF NOT EXISTS idx_scheduled_emails_message_id ON scheduled_emails(message_id);

-- Index for API key
CREATE INDEX IF NOT EXISTS idx_scheduled_emails_api_key ON scheduled_emails(api_key_id);

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
