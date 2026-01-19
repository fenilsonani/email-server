-- Migration 018: Full-Text Search with PostgreSQL tsvector
-- Creates search table with tsvector column and GIN index

-- Search table for full-text search
CREATE TABLE IF NOT EXISTS messages_search (
    id BIGSERIAL PRIMARY KEY,
    doc_id TEXT UNIQUE NOT NULL,  -- Document ID (format: "{mailbox_id}:{uid}")
    user_id BIGINT NOT NULL,
    mailbox_id BIGINT NOT NULL,
    uid INTEGER NOT NULL,
    subject TEXT,
    from_addr TEXT,
    to_addrs TEXT,
    body_text TEXT,
    message_id TEXT,
    search_vector TSVECTOR,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- GIN index for fast full-text search
CREATE INDEX IF NOT EXISTS idx_messages_search_vector ON messages_search USING GIN (search_vector);

-- Index for user filtering
CREATE INDEX IF NOT EXISTS idx_messages_search_user ON messages_search (user_id);

-- Index for mailbox filtering
CREATE INDEX IF NOT EXISTS idx_messages_search_mailbox ON messages_search (mailbox_id);

-- Composite index for common filter combination
CREATE INDEX IF NOT EXISTS idx_messages_search_user_mailbox ON messages_search (user_id, mailbox_id);

-- Function to update search_vector automatically
CREATE OR REPLACE FUNCTION messages_search_update_vector()
RETURNS TRIGGER AS $$
BEGIN
    NEW.search_vector :=
        setweight(to_tsvector('english', COALESCE(NEW.subject, '')), 'A') ||
        setweight(to_tsvector('english', COALESCE(NEW.from_addr, '')), 'B') ||
        setweight(to_tsvector('english', COALESCE(NEW.to_addrs, '')), 'B') ||
        setweight(to_tsvector('english', COALESCE(NEW.body_text, '')), 'C');
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger to update search_vector on insert/update
DROP TRIGGER IF EXISTS messages_search_vector_trigger ON messages_search;
CREATE TRIGGER messages_search_vector_trigger
    BEFORE INSERT OR UPDATE ON messages_search
    FOR EACH ROW
    EXECUTE FUNCTION messages_search_update_vector();

INSERT INTO schema_migrations (version) VALUES (18);
