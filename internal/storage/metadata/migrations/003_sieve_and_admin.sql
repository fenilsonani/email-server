-- Migration 003: Sieve filtering and admin panel support
-- Adds tables for Sieve scripts, vacation responses, auth/delivery logs

-- Sieve scripts table
CREATE TABLE IF NOT EXISTS sieve_scripts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    content TEXT NOT NULL,
    is_active BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, name)
);

CREATE INDEX IF NOT EXISTS idx_sieve_scripts_user ON sieve_scripts(user_id);
CREATE INDEX IF NOT EXISTS idx_sieve_scripts_active ON sieve_scripts(user_id, is_active);

-- Vacation response tracking (rate limiting)
CREATE TABLE IF NOT EXISTS vacation_responses (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sender_address TEXT NOT NULL,
    responded_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, sender_address)
);

CREATE INDEX IF NOT EXISTS idx_vacation_responses_user ON vacation_responses(user_id);
CREATE INDEX IF NOT EXISTS idx_vacation_responses_time ON vacation_responses(responded_at);

-- Auth log for admin panel
CREATE TABLE IF NOT EXISTS auth_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    username TEXT NOT NULL,
    remote_addr TEXT,
    protocol TEXT,  -- smtp, imap, web
    success BOOLEAN,
    failure_reason TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_auth_log_time ON auth_log(created_at);
CREATE INDEX IF NOT EXISTS idx_auth_log_user ON auth_log(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_log_success ON auth_log(success, created_at);

-- Delivery log for admin panel
CREATE TABLE IF NOT EXISTS delivery_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id TEXT,
    sender TEXT NOT NULL,
    recipient TEXT NOT NULL,
    status TEXT NOT NULL,  -- delivered, bounced, deferred, rejected
    direction TEXT DEFAULT 'inbound',  -- inbound, outbound
    smtp_code INTEGER,
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_delivery_log_time ON delivery_log(created_at);
CREATE INDEX IF NOT EXISTS idx_delivery_log_status ON delivery_log(status);
CREATE INDEX IF NOT EXISTS idx_delivery_log_sender ON delivery_log(sender);
CREATE INDEX IF NOT EXISTS idx_delivery_log_recipient ON delivery_log(recipient);

-- Add is_admin column to users table
-- SQLite doesn't support ALTER TABLE ADD COLUMN with constraints well,
-- so we check if the column exists first
CREATE TABLE IF NOT EXISTS _migration_check (done INTEGER);
INSERT OR IGNORE INTO _migration_check VALUES (1);

-- Add is_admin column if it doesn't exist (SQLite 3.35+ supports this)
-- For older SQLite, we handle this in Go code
ALTER TABLE users ADD COLUMN is_admin BOOLEAN DEFAULT FALSE;

-- Clean up migration check table
DROP TABLE IF EXISTS _migration_check;

INSERT INTO schema_migrations (version) VALUES (3);
