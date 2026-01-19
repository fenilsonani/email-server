-- Migration 018: Full-Text Search with FTS5
-- Creates FTS5 virtual table for email body search

-- FTS5 virtual table for full-text search
-- This uses SQLite's FTS5 extension which provides better ranking and more features than FTS4
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    doc_id,           -- Document ID (format: "{mailbox_id}:{uid}")
    user_id UNINDEXED, -- User ID (not searchable, just for filtering)
    mailbox_id UNINDEXED, -- Mailbox ID (not searchable, just for filtering)
    uid UNINDEXED,    -- Message UID (not searchable, just for filtering)
    subject,          -- Email subject (searchable)
    from_addr,        -- From address (searchable)
    to_addrs,         -- To addresses (searchable)
    body_text,        -- Plain text + stripped HTML body (searchable)
    message_id,       -- Message-ID header (searchable)
    content='',       -- External content (we manage content manually)
    contentless_delete=1, -- Allow DELETE on contentless tables
    tokenize='porter unicode61 remove_diacritics 1' -- Porter stemming with unicode support
);

-- Index for fast lookups by doc_id (for updates/deletes)
-- Note: FTS5 doesn't support regular indexes, but we store doc_id as the rowid mapping

-- Trigger to auto-update FTS index when messages are inserted
-- Note: This is optional - the application can manage indexing directly
-- CREATE TRIGGER IF NOT EXISTS messages_ai AFTER INSERT ON messages
-- BEGIN
--     INSERT INTO messages_fts(doc_id, user_id, mailbox_id, uid, subject, from_addr, to_addrs, body_text, message_id)
--     SELECT
--         CAST(NEW.mailbox_id AS TEXT) || ':' || CAST(NEW.uid AS TEXT),
--         (SELECT user_id FROM mailboxes WHERE id = NEW.mailbox_id),
--         NEW.mailbox_id,
--         NEW.uid,
--         COALESCE(NEW.subject, ''),
--         COALESCE(NEW.from_address, ''),
--         COALESCE(NEW.to_addresses, ''),
--         '',  -- Body text needs to be extracted from file
--         COALESCE(NEW.message_id, '');
-- END;

-- Trigger to remove from FTS index when messages are deleted
-- CREATE TRIGGER IF NOT EXISTS messages_ad AFTER DELETE ON messages
-- BEGIN
--     DELETE FROM messages_fts WHERE doc_id = CAST(OLD.mailbox_id AS TEXT) || ':' || CAST(OLD.uid AS TEXT);
-- END;

INSERT INTO schema_migrations (version) VALUES (18);
