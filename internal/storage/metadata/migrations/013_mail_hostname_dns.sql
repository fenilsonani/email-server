-- Migration 013: Mail Hostname DNS Verification
-- Tracks if the mail.{domain} A record is configured correctly

-- Add mail hostname DNS verification column
ALTER TABLE domains ADD COLUMN dns_mail_hostname_verified INTEGER DEFAULT 0;
