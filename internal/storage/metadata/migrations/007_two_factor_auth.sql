-- Migration 007: Two-Factor Authentication
-- Adds TOTP-based 2FA support for admin panel with device persistence

-- Add TOTP secret and enabled flag to users
ALTER TABLE users ADD COLUMN totp_secret TEXT;
ALTER TABLE users ADD COLUMN totp_enabled INTEGER DEFAULT 0;

-- Table to track verified 2FA devices (persisted for 30 days)
CREATE TABLE IF NOT EXISTS totp_trusted_devices (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    device_token TEXT NOT NULL UNIQUE,
    device_name TEXT,
    ip_address TEXT,
    user_agent TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    last_used_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Index for quick lookups
CREATE INDEX IF NOT EXISTS idx_totp_trusted_devices_user ON totp_trusted_devices(user_id);
CREATE INDEX IF NOT EXISTS idx_totp_trusted_devices_token ON totp_trusted_devices(device_token);
CREATE INDEX IF NOT EXISTS idx_totp_trusted_devices_expires ON totp_trusted_devices(expires_at);

-- Record migration
INSERT INTO schema_migrations (version) VALUES (7);
