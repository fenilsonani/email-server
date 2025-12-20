-- Initial database schema for the email server
-- This migration creates all core tables for email, users, and domains

-- Enable foreign keys
PRAGMA foreign_keys = ON;

-- Domains table
CREATE TABLE IF NOT EXISTS domains (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    dkim_selector TEXT NOT NULL DEFAULT 'mail',
    dkim_private_key BLOB,
    is_active BOOLEAN DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_domains_name ON domains(name);

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    username TEXT NOT NULL,                    -- local part of email
    password_hash TEXT NOT NULL,               -- argon2id hash
    display_name TEXT,
    quota_bytes INTEGER DEFAULT 1073741824,    -- 1GB default
    used_bytes INTEGER DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(domain_id, username)
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(domain_id, username);
CREATE INDEX IF NOT EXISTS idx_users_active ON users(is_active);

-- Aliases table (for forwarding/virtual addresses)
CREATE TABLE IF NOT EXISTS aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    source_address TEXT NOT NULL,              -- alias@domain.com (local part only)
    destination_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    destination_external TEXT,                 -- For external forwarding
    is_active BOOLEAN DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(domain_id, source_address),
    CHECK (destination_user_id IS NOT NULL OR destination_external IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_aliases_source ON aliases(domain_id, source_address);

-- Mailboxes table (IMAP folders)
CREATE TABLE IF NOT EXISTS mailboxes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,                        -- e.g., "INBOX", "Sent", "Archive"
    uidvalidity INTEGER NOT NULL,
    uidnext INTEGER NOT NULL DEFAULT 1,
    subscribed BOOLEAN DEFAULT TRUE,
    special_use TEXT,                          -- \Sent, \Drafts, \Trash, \Junk, \Archive
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, name)
);

CREATE INDEX IF NOT EXISTS idx_mailboxes_user ON mailboxes(user_id);
CREATE INDEX IF NOT EXISTS idx_mailboxes_special ON mailboxes(user_id, special_use);

-- Messages metadata (actual content stored in Maildir)
CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
    uid INTEGER NOT NULL,                      -- IMAP UID
    maildir_key TEXT NOT NULL,                 -- Filename in Maildir
    size INTEGER NOT NULL,
    internal_date DATETIME NOT NULL,
    flags TEXT DEFAULT '',                     -- Comma-separated: \Seen,\Flagged,etc
    message_id TEXT,                           -- Message-ID header
    subject TEXT,                              -- For search
    from_address TEXT,                         -- For search
    to_addresses TEXT,                         -- JSON array for search
    in_reply_to TEXT,                          -- For threading
    references_header TEXT,                    -- For threading
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(mailbox_id, uid)
);

CREATE INDEX IF NOT EXISTS idx_messages_mailbox ON messages(mailbox_id);
CREATE INDEX IF NOT EXISTS idx_messages_uid ON messages(mailbox_id, uid);
CREATE INDEX IF NOT EXISTS idx_messages_flags ON messages(mailbox_id, flags);
CREATE INDEX IF NOT EXISTS idx_messages_date ON messages(mailbox_id, internal_date);
CREATE INDEX IF NOT EXISTS idx_messages_msgid ON messages(message_id);
CREATE INDEX IF NOT EXISTS idx_messages_maildir ON messages(maildir_key);

-- IMAP subscriptions (separate from mailbox.subscribed for LSUB)
CREATE TABLE IF NOT EXISTS subscriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    mailbox_name TEXT NOT NULL,
    UNIQUE(user_id, mailbox_name)
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_user ON subscriptions(user_id);

-- Outbound mail queue
CREATE TABLE IF NOT EXISTS outbound_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sender TEXT NOT NULL,
    recipient TEXT NOT NULL,
    message_path TEXT NOT NULL,                -- Path to message file
    attempts INTEGER DEFAULT 0,
    last_attempt DATETIME,
    next_attempt DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT,
    status TEXT DEFAULT 'pending',             -- pending, sending, sent, failed
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_queue_status ON outbound_queue(status, next_attempt);
CREATE INDEX IF NOT EXISTS idx_queue_sender ON outbound_queue(sender);

-- Session tokens (for IMAP/SMTP auth if needed)
CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token TEXT NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO schema_migrations (version) VALUES (1);
