-- Migration 015: Domain Ownership Verification
-- SECURITY: Requires DNS TXT record verification before domain is active

-- Add verification columns to domains table
ALTER TABLE domains ADD COLUMN verification_token TEXT;
ALTER TABLE domains ADD COLUMN is_verified BOOLEAN DEFAULT FALSE;
ALTER TABLE domains ADD COLUMN verified_at DATETIME;

-- Existing domains are considered verified (grandfathered)
UPDATE domains SET is_verified = TRUE, verified_at = created_at WHERE is_verified IS NULL;
