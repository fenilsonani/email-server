-- Migration 006: DNS Status Tracking
-- Adds columns to track DNS verification status for each domain

-- Add DNS verification status columns
ALTER TABLE domains ADD COLUMN dns_mx_verified INTEGER DEFAULT 0;
ALTER TABLE domains ADD COLUMN dns_spf_verified INTEGER DEFAULT 0;
ALTER TABLE domains ADD COLUMN dns_dkim_verified INTEGER DEFAULT 0;
ALTER TABLE domains ADD COLUMN dns_dmarc_verified INTEGER DEFAULT 0;
ALTER TABLE domains ADD COLUMN dns_last_checked DATETIME;
ALTER TABLE domains ADD COLUMN dns_status TEXT DEFAULT 'pending';

-- Index for quick filtering by DNS status
CREATE INDEX IF NOT EXISTS idx_domains_dns_status ON domains(dns_status);

-- Record migration
INSERT INTO schema_migrations (version) VALUES (6);
