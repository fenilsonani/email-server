-- Migration 008: Search and Filtering Performance Indexes
-- Adds indexes for admin panel search and filtering functionality

-- Auth log indexes for username, protocol, and timestamp searches
CREATE INDEX IF NOT EXISTS idx_auth_log_username ON auth_log(username);
CREATE INDEX IF NOT EXISTS idx_auth_log_protocol ON auth_log(protocol);
CREATE INDEX IF NOT EXISTS idx_auth_log_created_at ON auth_log(created_at DESC);

-- Delivery log indexes for sender, recipient, status, and timestamp searches
CREATE INDEX IF NOT EXISTS idx_delivery_log_sender ON delivery_log(sender);
CREATE INDEX IF NOT EXISTS idx_delivery_log_recipient ON delivery_log(recipient);
CREATE INDEX IF NOT EXISTS idx_delivery_log_status ON delivery_log(status);
CREATE INDEX IF NOT EXISTS idx_delivery_log_created_at ON delivery_log(created_at DESC);

-- Users table index for username searches
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

-- Domains table index for name searches
CREATE INDEX IF NOT EXISTS idx_domains_name ON domains(name);

-- Record migration
INSERT INTO schema_migrations (version) VALUES (8);
