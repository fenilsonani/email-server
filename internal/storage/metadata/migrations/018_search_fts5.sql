-- Migration 018: Full-Text Search Placeholder
-- Creates FTS5 virtual table for email body search if available
-- Gracefully skips if FTS5 is not available on the system

-- Note: FTS5 virtual table for full-text search
-- This uses SQLite's FTS5 extension which provides better ranking and more features than FTS4
-- If FTS5 is not compiled into SQLite on this system, the table won't be created,
-- but the application can still function with the search feature disabled.

-- The table would normally be:
-- CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
--     doc_id, user_id UNINDEXED, mailbox_id UNINDEXED, uid UNINDEXED,
--     subject, from_addr, to_addrs, body_text, message_id,
--     content='', contentless_delete=1,
--     tokenize='porter unicode61 remove_diacritics 1'
-- );
--
-- However, we skip FTS5 creation for systems where it's not available.
-- The application will use database-level search or Bleve index instead.

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
