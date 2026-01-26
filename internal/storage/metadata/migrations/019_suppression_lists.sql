-- Migration 019: Suppression Lists
-- Adds table for managing suppressed email addresses (bounced, unsubscribed, etc.)

-- Suppression list for blocked email addresses
CREATE TABLE IF NOT EXISTS suppression_list (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    reason TEXT NOT NULL CHECK(reason IN ('hard_bounce', 'unsubscribe', 'complaint', 'manual')),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(domain_id, email)
);

-- Index for fast lookups by email
CREATE INDEX IF NOT EXISTS idx_suppression_list_domain_email ON suppression_list(domain_id, email);

-- Index for listing by reason
CREATE INDEX IF NOT EXISTS idx_suppression_list_reason ON suppression_list(domain_id, reason);

-- Index for cleanup/listing by date
CREATE INDEX IF NOT EXISTS idx_suppression_list_created ON suppression_list(created_at);
