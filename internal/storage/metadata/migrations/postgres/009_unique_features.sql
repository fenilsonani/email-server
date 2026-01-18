-- Migration: Unique Features (Screener, Aliases, Scheduled Send, Snooze, VIP, Preferences)
-- PostgreSQL version

-- Screener: First-contact filtering
CREATE TABLE IF NOT EXISTS screener_contacts (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email TEXT,
    domain TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CHECK (email IS NOT NULL OR domain IS NOT NULL),
    UNIQUE(user_id, email),
    UNIQUE(user_id, domain)
);

CREATE INDEX IF NOT EXISTS idx_screener_user ON screener_contacts(user_id);
CREATE INDEX IF NOT EXISTS idx_screener_status ON screener_contacts(user_id, status);
CREATE INDEX IF NOT EXISTS idx_screener_email ON screener_contacts(email);
CREATE INDEX IF NOT EXISTS idx_screener_domain ON screener_contacts(domain);

-- Email Aliases
CREATE TABLE IF NOT EXISTS email_aliases (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    alias_address TEXT NOT NULL UNIQUE,
    alias_local TEXT NOT NULL,
    description TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    email_count INTEGER DEFAULT 0,
    UNIQUE(domain_id, alias_local)
);

CREATE INDEX IF NOT EXISTS idx_aliases_user ON email_aliases(user_id);
CREATE INDEX IF NOT EXISTS idx_aliases_address ON email_aliases(alias_address);
CREATE INDEX IF NOT EXISTS idx_aliases_active ON email_aliases(user_id, is_active);

-- Scheduled Emails
CREATE TABLE IF NOT EXISTS scheduled_emails (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    send_at TIMESTAMP NOT NULL,
    from_address TEXT NOT NULL,
    recipients TEXT NOT NULL,
    subject TEXT,
    body TEXT,
    html_body TEXT,
    headers TEXT,
    status TEXT DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    sent_at TIMESTAMP,
    error TEXT
);

CREATE INDEX IF NOT EXISTS idx_scheduled_user ON scheduled_emails(user_id);
CREATE INDEX IF NOT EXISTS idx_scheduled_pending ON scheduled_emails(status, send_at);
CREATE INDEX IF NOT EXISTS idx_scheduled_status ON scheduled_emails(user_id, status);

-- Snoozed Emails
CREATE TABLE IF NOT EXISTS snoozed_emails (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    original_mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
    wake_at TIMESTAMP NOT NULL,
    mark_unread BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(message_id)
);

CREATE INDEX IF NOT EXISTS idx_snoozed_user ON snoozed_emails(user_id);
CREATE INDEX IF NOT EXISTS idx_snoozed_wake ON snoozed_emails(wake_at);

-- VIP Contacts
CREATE TABLE IF NOT EXISTS vip_contacts (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    name TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, email)
);

CREATE INDEX IF NOT EXISTS idx_vip_user ON vip_contacts(user_id);
CREATE INDEX IF NOT EXISTS idx_vip_email ON vip_contacts(email);

-- User Preferences
CREATE TABLE IF NOT EXISTS user_preferences (
    user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    undo_send_delay INTEGER DEFAULT 10,
    screener_enabled BOOLEAN DEFAULT TRUE,
    tracker_blocking TEXT DEFAULT 'block',
    zones_enabled BOOLEAN DEFAULT TRUE,
    snooze_mark_unread BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Pending Sends
CREATE TABLE IF NOT EXISTS pending_sends (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    cancel_token TEXT NOT NULL UNIQUE,
    from_address TEXT NOT NULL,
    recipients TEXT NOT NULL,
    subject TEXT,
    body TEXT,
    html_body TEXT,
    headers TEXT,
    send_after TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pending_token ON pending_sends(cancel_token);
CREATE INDEX IF NOT EXISTS idx_pending_send_after ON pending_sends(send_after);

-- Add columns to messages
ALTER TABLE messages ADD COLUMN IF NOT EXISTS zone TEXT DEFAULT 'inbox';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS trackers_blocked INTEGER DEFAULT 0;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS tracker_domains TEXT;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP;

CREATE INDEX IF NOT EXISTS idx_messages_zone ON messages(mailbox_id, zone);
CREATE INDEX IF NOT EXISTS idx_messages_expires ON messages(expires_at);

INSERT INTO schema_migrations (version) VALUES (9) ON CONFLICT DO NOTHING;
