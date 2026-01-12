-- Migration: Unique Features (Screener, Aliases, Scheduled Send, Snooze, VIP, Preferences)
-- This adds the tables needed for Hey.com-style inbox control, privacy, and productivity features

-- Screener: First-contact filtering (approve/block new senders)
CREATE TABLE IF NOT EXISTS screener_contacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email TEXT,                                    -- specific email address
    domain TEXT,                                   -- or entire domain
    status TEXT NOT NULL DEFAULT 'pending',        -- pending, approved, blocked
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    CHECK (email IS NOT NULL OR domain IS NOT NULL),
    UNIQUE(user_id, email),
    UNIQUE(user_id, domain)
);

CREATE INDEX IF NOT EXISTS idx_screener_user ON screener_contacts(user_id);
CREATE INDEX IF NOT EXISTS idx_screener_status ON screener_contacts(user_id, status);
CREATE INDEX IF NOT EXISTS idx_screener_email ON screener_contacts(email);
CREATE INDEX IF NOT EXISTS idx_screener_domain ON screener_contacts(domain);

-- Email Aliases: Disposable/masked email addresses
CREATE TABLE IF NOT EXISTS email_aliases (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    alias_address TEXT NOT NULL UNIQUE,            -- full alias like shop_x7k2@domain.com
    alias_local TEXT NOT NULL,                     -- just the local part
    description TEXT,                              -- "Amazon shopping", "Newsletter signups"
    is_active BOOLEAN DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME,
    email_count INTEGER DEFAULT 0,
    UNIQUE(domain_id, alias_local)
);

CREATE INDEX IF NOT EXISTS idx_aliases_user ON email_aliases(user_id);
CREATE INDEX IF NOT EXISTS idx_aliases_address ON email_aliases(alias_address);
CREATE INDEX IF NOT EXISTS idx_aliases_active ON email_aliases(user_id, is_active);

-- Scheduled Emails: Send Later functionality
CREATE TABLE IF NOT EXISTS scheduled_emails (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    send_at DATETIME NOT NULL,
    from_address TEXT NOT NULL,
    recipients TEXT NOT NULL,                      -- JSON array of recipients
    subject TEXT,
    body TEXT,
    html_body TEXT,
    headers TEXT,                                  -- JSON object of additional headers
    status TEXT DEFAULT 'pending',                 -- pending, sending, sent, cancelled, failed
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    sent_at DATETIME,
    error TEXT
);

CREATE INDEX IF NOT EXISTS idx_scheduled_user ON scheduled_emails(user_id);
CREATE INDEX IF NOT EXISTS idx_scheduled_pending ON scheduled_emails(status, send_at);
CREATE INDEX IF NOT EXISTS idx_scheduled_status ON scheduled_emails(user_id, status);

-- Snoozed Emails: Remind me later
CREATE TABLE IF NOT EXISTS snoozed_emails (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    original_mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
    wake_at DATETIME NOT NULL,
    mark_unread BOOLEAN DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(message_id)                             -- A message can only be snoozed once
);

CREATE INDEX IF NOT EXISTS idx_snoozed_user ON snoozed_emails(user_id);
CREATE INDEX IF NOT EXISTS idx_snoozed_wake ON snoozed_emails(wake_at);

-- VIP Contacts: Priority senders
CREATE TABLE IF NOT EXISTS vip_contacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    name TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, email)
);

CREATE INDEX IF NOT EXISTS idx_vip_user ON vip_contacts(user_id);
CREATE INDEX IF NOT EXISTS idx_vip_email ON vip_contacts(email);

-- User Preferences: Settings for unique features
CREATE TABLE IF NOT EXISTS user_preferences (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- Undo Send
    undo_send_delay INTEGER DEFAULT 10,            -- seconds (0, 5, 10, 20, 30)
    -- Screener
    screener_enabled BOOLEAN DEFAULT TRUE,
    -- Tracker Blocking
    tracker_blocking TEXT DEFAULT 'block',         -- block, proxy, off
    -- Smart Zones
    zones_enabled BOOLEAN DEFAULT TRUE,
    -- Snooze defaults
    snooze_mark_unread BOOLEAN DEFAULT TRUE,
    -- Created/Updated
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Pending Sends: For Undo Send feature (short-lived)
CREATE TABLE IF NOT EXISTS pending_sends (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cancel_token TEXT NOT NULL UNIQUE,             -- Token to cancel the send
    from_address TEXT NOT NULL,
    recipients TEXT NOT NULL,                      -- JSON array
    subject TEXT,
    body TEXT,
    html_body TEXT,
    headers TEXT,                                  -- JSON object
    send_after DATETIME NOT NULL,                  -- When to actually send
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pending_token ON pending_sends(cancel_token);
CREATE INDEX IF NOT EXISTS idx_pending_send_after ON pending_sends(send_after);

-- Add zone column to messages for Smart Inbox Zones
-- Values: priority, feed, paper_trail, screener, inbox
ALTER TABLE messages ADD COLUMN zone TEXT DEFAULT 'inbox';

-- Add trackers info to messages
ALTER TABLE messages ADD COLUMN trackers_blocked INTEGER DEFAULT 0;
ALTER TABLE messages ADD COLUMN tracker_domains TEXT;  -- JSON array of blocked domains

-- Add expiry for self-destructing messages
ALTER TABLE messages ADD COLUMN expires_at DATETIME;

-- Index for zone-based queries
CREATE INDEX IF NOT EXISTS idx_messages_zone ON messages(mailbox_id, zone);
CREATE INDEX IF NOT EXISTS idx_messages_expires ON messages(expires_at);

-- Update schema version
INSERT INTO schema_migrations (version) VALUES (9);
