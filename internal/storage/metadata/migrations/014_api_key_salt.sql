-- Migration 014: Add per-key salt for API keys
-- SECURITY: Using a fixed salt was a vulnerability - each key needs its own salt

-- Add salt column for per-key salt storage
ALTER TABLE api_keys ADD COLUMN key_salt TEXT;

-- Existing keys will have NULL salt and continue using the legacy fixed salt
-- New keys will have a random salt stored here
