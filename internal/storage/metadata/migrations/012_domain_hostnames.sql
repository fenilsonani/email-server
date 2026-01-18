-- Multi-domain mail hostnames support
-- Each domain gets its own mail.{domain} hostname

-- Add mail_hostname column (auto-generated as mail.{domain})
ALTER TABLE domains ADD COLUMN mail_hostname TEXT;

-- Mark primary domain (where admin dashboard is accessible)
ALTER TABLE domains ADD COLUMN is_primary BOOLEAN DEFAULT FALSE;

-- Update existing domains with auto-generated hostnames
UPDATE domains SET mail_hostname = 'mail.' || name WHERE mail_hostname IS NULL;

-- Create index for hostname lookups
CREATE INDEX IF NOT EXISTS idx_domains_mail_hostname ON domains(mail_hostname);
