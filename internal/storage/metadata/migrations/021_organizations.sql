-- Migration 021: Multi-Organization Support
-- Adds organizations, org membership, and links existing tables to orgs.

-- Organizations
CREATE TABLE IF NOT EXISTS organizations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    owner_user_id INTEGER NOT NULL,
    preset TEXT NOT NULL DEFAULT 'full',
    settings TEXT DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_organizations_slug ON organizations(slug);
CREATE INDEX IF NOT EXISTS idx_organizations_owner ON organizations(owner_user_id);

-- Org membership
CREATE TABLE IF NOT EXISTS org_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(org_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_org_members_org ON org_members(org_id);
CREATE INDEX IF NOT EXISTS idx_org_members_user ON org_members(user_id);

-- Add org_id to existing tables
ALTER TABLE domains ADD COLUMN org_id INTEGER REFERENCES organizations(id);
ALTER TABLE users ADD COLUMN org_id INTEGER REFERENCES organizations(id);
ALTER TABLE api_keys ADD COLUMN org_id INTEGER REFERENCES organizations(id);
ALTER TABLE webhooks ADD COLUMN org_id INTEGER REFERENCES organizations(id);
ALTER TABLE email_templates ADD COLUMN org_id INTEGER REFERENCES organizations(id);
ALTER TABLE mailing_lists ADD COLUMN org_id INTEGER REFERENCES organizations(id);

-- Create default organization from existing data
-- This uses the first admin user as the owner
INSERT OR IGNORE INTO organizations (id, name, slug, owner_user_id, preset, settings)
SELECT 1, 'Default', 'default',
    COALESCE((SELECT id FROM users WHERE is_admin = 1 ORDER BY id LIMIT 1), 1),
    'full', '{}';

-- Assign all existing domains to the default org
UPDATE domains SET org_id = 1 WHERE org_id IS NULL;

-- Assign all existing users to the default org
UPDATE users SET org_id = 1 WHERE org_id IS NULL;

-- Assign all existing API keys to the default org
UPDATE api_keys SET org_id = 1 WHERE org_id IS NULL;

-- Assign all existing webhooks to the default org
UPDATE webhooks SET org_id = 1 WHERE org_id IS NULL;

-- Assign all existing templates to the default org
UPDATE email_templates SET org_id = 1 WHERE org_id IS NULL;

-- Assign all existing mailing lists to the default org
UPDATE mailing_lists SET org_id = 1 WHERE org_id IS NULL;

-- Add the owner as an org member
INSERT OR IGNORE INTO org_members (org_id, user_id, role)
SELECT 1, id, 'owner' FROM users WHERE is_admin = 1;

-- Add all existing users as org members
INSERT OR IGNORE INTO org_members (org_id, user_id, role)
SELECT 1, id, CASE WHEN is_admin = 1 THEN 'admin' ELSE 'member' END
FROM users WHERE id NOT IN (SELECT user_id FROM org_members WHERE org_id = 1);

-- Record migration version
INSERT OR IGNORE INTO schema_migrations (version) VALUES (21);
