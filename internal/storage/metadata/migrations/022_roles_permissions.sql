-- Roles table
CREATE TABLE IF NOT EXISTS roles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Predefined roles
INSERT OR IGNORE INTO roles (name, description) VALUES
    ('super_admin', 'Full access to all domains and settings'),
    ('domain_admin', 'Manage users, aliases, and settings for assigned domains'),
    ('support', 'View logs, reset passwords, read-only user access');

-- Permissions table
CREATE TABLE IF NOT EXISTS permissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT
);

-- Predefined permissions
INSERT OR IGNORE INTO permissions (name, description) VALUES
    ('users.create', 'Create new users'),
    ('users.read', 'View user list and details'),
    ('users.update', 'Edit user settings'),
    ('users.delete', 'Delete users'),
    ('users.password', 'Reset user passwords'),
    ('domains.create', 'Add new domains'),
    ('domains.read', 'View domain list and DNS'),
    ('domains.update', 'Edit domain settings'),
    ('domains.delete', 'Delete domains'),
    ('aliases.manage', 'Create/edit/delete aliases'),
    ('lists.manage', 'Manage mailing lists'),
    ('logs.view', 'View auth and delivery logs'),
    ('audit.view', 'View audit logs'),
    ('settings.manage', 'Manage server settings'),
    ('features.manage', 'Manage features (sieve, screener, etc)'),
    ('queue.manage', 'View and manage mail queue');

-- Role-permission mapping
CREATE TABLE IF NOT EXISTS role_permissions (
    role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id INTEGER NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

-- super_admin gets ALL permissions
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
    SELECT r.id, p.id FROM roles r, permissions p WHERE r.name = 'super_admin';

-- domain_admin gets user/alias/list management + logs
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
    SELECT r.id, p.id FROM roles r, permissions p
    WHERE r.name = 'domain_admin' AND p.name IN (
        'users.create', 'users.read', 'users.update', 'users.delete', 'users.password',
        'domains.read',
        'aliases.manage', 'lists.manage', 'logs.view', 'features.manage'
    );

-- support gets read-only + password reset
INSERT OR IGNORE INTO role_permissions (role_id, permission_id)
    SELECT r.id, p.id FROM roles r, permissions p
    WHERE r.name = 'support' AND p.name IN (
        'users.read', 'users.password', 'domains.read', 'logs.view', 'audit.view'
    );

-- User role assignment (replaces is_admin boolean)
CREATE TABLE IF NOT EXISTS user_roles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    domain_id INTEGER REFERENCES domains(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_roles_unique ON user_roles(user_id, role_id, COALESCE(domain_id, 0));
CREATE INDEX IF NOT EXISTS idx_user_roles_user_id ON user_roles(user_id);

-- Migrate existing is_admin users to super_admin role
INSERT OR IGNORE INTO user_roles (user_id, role_id)
    SELECT u.id, r.id FROM users u, roles r
    WHERE u.is_admin = 1 AND r.name = 'super_admin';
