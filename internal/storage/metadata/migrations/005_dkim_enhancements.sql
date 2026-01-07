-- Migration 005: Enhanced DKIM key management
-- Adds columns for complete DKIM key lifecycle management
-- Enables database storage of keys, key metadata tracking, and storage preferences

-- Add public key cache column (for quick DNS record generation without parsing private key)
ALTER TABLE domains ADD COLUMN dkim_public_key TEXT;

-- Add key metadata columns
ALTER TABLE domains ADD COLUMN dkim_key_created_at DATETIME;

-- Add key algorithm tracking (RSA-2048, RSA-4096, future: Ed25519)
ALTER TABLE domains ADD COLUMN dkim_key_algorithm TEXT DEFAULT 'RSA-2048';

-- Add key storage preference: 'file', 'database', or 'hybrid' (both)
ALTER TABLE domains ADD COLUMN dkim_storage_type TEXT DEFAULT 'file';

-- Add key file path for file-based storage (allows per-domain paths)
ALTER TABLE domains ADD COLUMN dkim_key_file TEXT;

-- Index for key storage type lookups
CREATE INDEX IF NOT EXISTS idx_domains_dkim_storage ON domains(dkim_storage_type);

-- Record migration
INSERT INTO schema_migrations (version) VALUES (5);
