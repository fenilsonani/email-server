-- Migration 010: Mailing Lists Feature
-- Adds tables for mailing list management, membership, moderation, and archives

-- Mailing Lists table
CREATE TABLE IF NOT EXISTS mailing_lists (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    local_part TEXT NOT NULL,
    list_address TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,

    -- List type and posting policy
    list_type TEXT NOT NULL DEFAULT 'discussion',
    posting_policy TEXT NOT NULL DEFAULT 'members_only',

    -- Moderation settings
    moderation_enabled BOOLEAN DEFAULT FALSE,
    require_subject_prefix BOOLEAN DEFAULT FALSE,
    subject_prefix TEXT,

    -- Reply behavior
    reply_to_list BOOLEAN DEFAULT TRUE,
    reply_to_sender BOOLEAN DEFAULT FALSE,

    -- Archive settings
    archive_enabled BOOLEAN DEFAULT TRUE,
    archive_public BOOLEAN DEFAULT FALSE,

    -- Subscription settings
    allow_subscribe BOOLEAN DEFAULT TRUE,
    require_confirm BOOLEAN DEFAULT TRUE,

    -- Size limits
    max_message_size INTEGER DEFAULT 10485760,
    max_members INTEGER DEFAULT 10000,

    -- Status
    is_active BOOLEAN DEFAULT TRUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_lists_domain_local ON mailing_lists(domain_id, local_part);
CREATE INDEX IF NOT EXISTS idx_lists_active ON mailing_lists(is_active);
CREATE INDEX IF NOT EXISTS idx_lists_domain ON mailing_lists(domain_id);

-- List Members table
CREATE TABLE IF NOT EXISTS list_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    list_id INTEGER NOT NULL REFERENCES mailing_lists(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    name TEXT,

    -- Membership role
    role TEXT NOT NULL DEFAULT 'member',

    -- Delivery preferences
    delivery_mode TEXT DEFAULT 'normal',

    -- Status
    is_confirmed BOOLEAN DEFAULT FALSE,
    confirm_token TEXT,
    confirm_expires DATETIME,

    subscribed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    UNIQUE(list_id, email)
);

CREATE INDEX IF NOT EXISTS idx_members_list ON list_members(list_id);
CREATE INDEX IF NOT EXISTS idx_members_email ON list_members(email);
CREATE INDEX IF NOT EXISTS idx_members_role ON list_members(list_id, role);
CREATE INDEX IF NOT EXISTS idx_members_confirmed ON list_members(list_id, is_confirmed);
CREATE INDEX IF NOT EXISTS idx_members_token ON list_members(confirm_token);

-- Moderation Queue table
CREATE TABLE IF NOT EXISTS list_moderation_queue (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    list_id INTEGER NOT NULL REFERENCES mailing_lists(id) ON DELETE CASCADE,

    -- Message envelope
    sender_email TEXT NOT NULL,
    subject TEXT,
    message_path TEXT NOT NULL,
    message_size INTEGER,

    -- Moderation status
    status TEXT DEFAULT 'pending',
    moderated_by INTEGER REFERENCES users(id),
    moderated_at DATETIME,
    rejection_reason TEXT,

    -- Auto-expire old items
    expires_at DATETIME,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_modqueue_list ON list_moderation_queue(list_id);
CREATE INDEX IF NOT EXISTS idx_modqueue_status ON list_moderation_queue(list_id, status);
CREATE INDEX IF NOT EXISTS idx_modqueue_expires ON list_moderation_queue(expires_at);

-- List Archives table
CREATE TABLE IF NOT EXISTS list_archives (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    list_id INTEGER NOT NULL REFERENCES mailing_lists(id) ON DELETE CASCADE,

    -- Message metadata
    message_id TEXT,
    sender_email TEXT NOT NULL,
    sender_name TEXT,
    subject TEXT,
    sent_at DATETIME NOT NULL,

    -- Content storage
    message_path TEXT NOT NULL,
    message_size INTEGER,

    -- Threading
    in_reply_to TEXT,
    thread_id INTEGER REFERENCES list_archives(id),

    -- Search optimization
    body_preview TEXT,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_archives_list ON list_archives(list_id);
CREATE INDEX IF NOT EXISTS idx_archives_date ON list_archives(list_id, sent_at DESC);
CREATE INDEX IF NOT EXISTS idx_archives_thread ON list_archives(thread_id);
CREATE INDEX IF NOT EXISTS idx_archives_msgid ON list_archives(message_id);
CREATE INDEX IF NOT EXISTS idx_archives_sender ON list_archives(sender_email);

-- Pending subscription confirmations
CREATE TABLE IF NOT EXISTS list_pending_actions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    list_id INTEGER NOT NULL REFERENCES mailing_lists(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    action TEXT NOT NULL,
    token TEXT NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_pending_token ON list_pending_actions(token);
CREATE INDEX IF NOT EXISTS idx_pending_expires ON list_pending_actions(expires_at);
