-- Migration 018: Update System
-- Adds tables for version management, update history, progress tracking, and rollback capabilities

-- Update Settings - System configuration for update management
CREATE TABLE IF NOT EXISTS update_settings (
    id INTEGER PRIMARY KEY,
    mode TEXT NOT NULL DEFAULT 'normal',
    auto_check_enabled BOOLEAN DEFAULT 1,
    auto_check_interval INTEGER DEFAULT 3600,
    git_repo_url TEXT DEFAULT 'https://github.com/fenilsonani/email-server',
    current_branch TEXT DEFAULT 'main',
    current_commit TEXT,
    current_version TEXT,
    build_path TEXT DEFAULT '/tmp/mailserver-build',
    backup_before_update BOOLEAN DEFAULT 1,
    max_backups INTEGER DEFAULT 5,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Update History - Audit trail of all updates
CREATE TABLE IF NOT EXISTS update_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    update_type TEXT NOT NULL,
    from_version TEXT,
    to_version TEXT,
    from_commit TEXT,
    to_commit TEXT,
    pr_number INTEGER,
    branch_name TEXT,
    status TEXT NOT NULL,
    started_by TEXT NOT NULL,
    started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    duration_seconds INTEGER,
    backup_path TEXT,
    rollback_available BOOLEAN DEFAULT 1,
    error_message TEXT,
    metadata TEXT,
    FOREIGN KEY (started_by) REFERENCES users(username)
);

CREATE INDEX IF NOT EXISTS idx_update_history_status ON update_history(status);
CREATE INDEX IF NOT EXISTS idx_update_history_started_by ON update_history(started_by);
CREATE INDEX IF NOT EXISTS idx_update_history_created ON update_history(started_at DESC);

-- Update Progress - Real-time tracking of update steps
CREATE TABLE IF NOT EXISTS update_progress (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    update_id INTEGER NOT NULL,
    step_number INTEGER NOT NULL,
    step_name TEXT NOT NULL,
    status TEXT NOT NULL,
    message TEXT,
    progress_percent INTEGER DEFAULT 0,
    started_at DATETIME,
    completed_at DATETIME,
    FOREIGN KEY (update_id) REFERENCES update_history(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_update_progress_update ON update_progress(update_id);
CREATE INDEX IF NOT EXISTS idx_update_progress_step ON update_progress(update_id, step_number);

-- Version Cache - Cached GitHub releases, PRs, branches
CREATE TABLE IF NOT EXISTS version_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version_type TEXT NOT NULL,
    version_name TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    published_at DATETIME,
    is_prerelease BOOLEAN DEFAULT 0,
    changelog TEXT,
    metadata TEXT,
    cached_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(version_type, version_name)
);

CREATE INDEX IF NOT EXISTS idx_version_cache_type ON version_cache(version_type);
CREATE INDEX IF NOT EXISTS idx_version_cache_cached ON version_cache(cached_at DESC);

-- Rollback Snapshots - Pre-update backup metadata and binary snapshots
CREATE TABLE IF NOT EXISTS rollback_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    update_id INTEGER NOT NULL,
    snapshot_type TEXT DEFAULT 'pre_update',
    version TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    binary_path TEXT NOT NULL,
    backup_path TEXT NOT NULL,
    config_snapshot TEXT,
    health_status TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME,
    FOREIGN KEY (update_id) REFERENCES update_history(id)
);

CREATE INDEX IF NOT EXISTS idx_rollback_snapshots_update ON rollback_snapshots(update_id);
CREATE INDEX IF NOT EXISTS idx_rollback_snapshots_expires ON rollback_snapshots(expires_at);
CREATE INDEX IF NOT EXISTS idx_rollback_snapshots_type ON rollback_snapshots(snapshot_type);

INSERT OR IGNORE INTO schema_migrations (version) VALUES (18);
