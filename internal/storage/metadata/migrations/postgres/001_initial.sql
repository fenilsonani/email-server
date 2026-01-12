-- Initial PostgreSQL schema for the email server
-- This migration creates all tables with PostgreSQL-compatible syntax

-- Domains table
CREATE TABLE IF NOT EXISTS domains (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    dkim_selector TEXT NOT NULL DEFAULT 'mail',
    dkim_private_key BYTEA,
    dkim_public_key TEXT,
    dkim_key_created_at TIMESTAMP WITH TIME ZONE,
    dkim_key_algorithm TEXT DEFAULT 'RSA-2048',
    dkim_storage_type TEXT DEFAULT 'file',
    dkim_key_file TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    dns_mx_verified INTEGER DEFAULT 0,
    dns_spf_verified INTEGER DEFAULT 0,
    dns_dkim_verified INTEGER DEFAULT 0,
    dns_dmarc_verified INTEGER DEFAULT 0,
    dns_last_checked TIMESTAMP WITH TIME ZONE,
    dns_status TEXT DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_domains_name ON domains(name);
CREATE INDEX IF NOT EXISTS idx_domains_dkim_storage ON domains(dkim_storage_type);
CREATE INDEX IF NOT EXISTS idx_domains_dns_status ON domains(dns_status);

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    username TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    display_name TEXT,
    quota_bytes BIGINT DEFAULT 1073741824,
    used_bytes BIGINT DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE,
    is_admin BOOLEAN DEFAULT FALSE,
    totp_secret TEXT,
    totp_enabled INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(domain_id, username)
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users(domain_id, username);
CREATE INDEX IF NOT EXISTS idx_users_active ON users(is_active);
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

-- Aliases table
CREATE TABLE IF NOT EXISTS aliases (
    id BIGSERIAL PRIMARY KEY,
    domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    source_address TEXT NOT NULL,
    destination_user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    destination_external TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(domain_id, source_address),
    CONSTRAINT aliases_destination_check CHECK (destination_user_id IS NOT NULL OR destination_external IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_aliases_source ON aliases(domain_id, source_address);

-- Mailboxes table (IMAP folders)
CREATE TABLE IF NOT EXISTS mailboxes (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    uidvalidity INTEGER NOT NULL,
    uidnext INTEGER NOT NULL DEFAULT 1,
    subscribed BOOLEAN DEFAULT TRUE,
    special_use TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(user_id, name)
);

CREATE INDEX IF NOT EXISTS idx_mailboxes_user ON mailboxes(user_id);
CREATE INDEX IF NOT EXISTS idx_mailboxes_special ON mailboxes(user_id, special_use);

-- Messages metadata
CREATE TABLE IF NOT EXISTS messages (
    id BIGSERIAL PRIMARY KEY,
    mailbox_id BIGINT NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
    uid INTEGER NOT NULL,
    maildir_key TEXT NOT NULL,
    size INTEGER NOT NULL,
    internal_date TIMESTAMP WITH TIME ZONE NOT NULL,
    flags TEXT DEFAULT '',
    message_id TEXT,
    subject TEXT,
    from_address TEXT,
    to_addresses TEXT,
    in_reply_to TEXT,
    references_header TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(mailbox_id, uid)
);

CREATE INDEX IF NOT EXISTS idx_messages_mailbox ON messages(mailbox_id);
CREATE INDEX IF NOT EXISTS idx_messages_uid ON messages(mailbox_id, uid);
CREATE INDEX IF NOT EXISTS idx_messages_flags ON messages(mailbox_id, flags);
CREATE INDEX IF NOT EXISTS idx_messages_date ON messages(mailbox_id, internal_date);
CREATE INDEX IF NOT EXISTS idx_messages_msgid ON messages(message_id);
CREATE INDEX IF NOT EXISTS idx_messages_maildir ON messages(maildir_key);

-- IMAP subscriptions
CREATE TABLE IF NOT EXISTS subscriptions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    mailbox_name TEXT NOT NULL,
    UNIQUE(user_id, mailbox_name)
);

CREATE INDEX IF NOT EXISTS idx_subscriptions_user ON subscriptions(user_id);

-- Outbound mail queue
CREATE TABLE IF NOT EXISTS outbound_queue (
    id BIGSERIAL PRIMARY KEY,
    sender TEXT NOT NULL,
    recipient TEXT NOT NULL,
    message_path TEXT NOT NULL,
    attempts INTEGER DEFAULT 0,
    last_attempt TIMESTAMP WITH TIME ZONE,
    next_attempt TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_error TEXT,
    status TEXT DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_queue_status ON outbound_queue(status, next_attempt);
CREATE INDEX IF NOT EXISTS idx_queue_sender ON outbound_queue(sender);

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);

-- Calendars table (CalDAV)
CREATE TABLE IF NOT EXISTS calendars (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    uid TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    color TEXT DEFAULT '#0066CC',
    timezone TEXT DEFAULT 'UTC',
    ctag TEXT NOT NULL,
    is_default BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_calendars_user ON calendars(user_id);
CREATE INDEX IF NOT EXISTS idx_calendars_uid ON calendars(uid);

-- Calendar events
CREATE TABLE IF NOT EXISTS calendar_events (
    id BIGSERIAL PRIMARY KEY,
    calendar_id BIGINT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    uid TEXT NOT NULL,
    etag TEXT NOT NULL,
    icalendar_data TEXT NOT NULL,
    summary TEXT,
    description TEXT,
    location TEXT,
    start_time TIMESTAMP WITH TIME ZONE,
    end_time TIMESTAMP WITH TIME ZONE,
    all_day BOOLEAN DEFAULT FALSE,
    recurrence_rule TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(calendar_id, uid)
);

CREATE INDEX IF NOT EXISTS idx_events_calendar ON calendar_events(calendar_id);
CREATE INDEX IF NOT EXISTS idx_events_uid ON calendar_events(uid);
CREATE INDEX IF NOT EXISTS idx_events_time ON calendar_events(start_time, end_time);

-- Address books (CardDAV)
CREATE TABLE IF NOT EXISTS addressbooks (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    uid TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    ctag TEXT NOT NULL,
    is_default BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_addressbooks_user ON addressbooks(user_id);
CREATE INDEX IF NOT EXISTS idx_addressbooks_uid ON addressbooks(uid);

-- Contacts
CREATE TABLE IF NOT EXISTS contacts (
    id BIGSERIAL PRIMARY KEY,
    addressbook_id BIGINT NOT NULL REFERENCES addressbooks(id) ON DELETE CASCADE,
    uid TEXT NOT NULL,
    etag TEXT NOT NULL,
    vcard_data TEXT NOT NULL,
    full_name TEXT,
    given_name TEXT,
    family_name TEXT,
    nickname TEXT,
    emails TEXT,
    phones TEXT,
    organization TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(addressbook_id, uid)
);

CREATE INDEX IF NOT EXISTS idx_contacts_addressbook ON contacts(addressbook_id);
CREATE INDEX IF NOT EXISTS idx_contacts_uid ON contacts(uid);
CREATE INDEX IF NOT EXISTS idx_contacts_name ON contacts(full_name);
CREATE INDEX IF NOT EXISTS idx_contacts_email ON contacts(emails);

-- Sieve scripts
CREATE TABLE IF NOT EXISTS sieve_scripts (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    content TEXT NOT NULL,
    is_active BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(user_id, name)
);

CREATE INDEX IF NOT EXISTS idx_sieve_scripts_user ON sieve_scripts(user_id);
CREATE INDEX IF NOT EXISTS idx_sieve_scripts_active ON sieve_scripts(user_id, is_active);

-- Vacation responses
CREATE TABLE IF NOT EXISTS vacation_responses (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sender_address TEXT NOT NULL,
    responded_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(user_id, sender_address)
);

CREATE INDEX IF NOT EXISTS idx_vacation_responses_user ON vacation_responses(user_id);
CREATE INDEX IF NOT EXISTS idx_vacation_responses_time ON vacation_responses(responded_at);

-- Auth log
CREATE TABLE IF NOT EXISTS auth_log (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    username TEXT NOT NULL,
    remote_addr TEXT,
    protocol TEXT,
    success BOOLEAN,
    failure_reason TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_auth_log_time ON auth_log(created_at);
CREATE INDEX IF NOT EXISTS idx_auth_log_user ON auth_log(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_log_success ON auth_log(success, created_at);
CREATE INDEX IF NOT EXISTS idx_auth_log_username ON auth_log(username);
CREATE INDEX IF NOT EXISTS idx_auth_log_protocol ON auth_log(protocol);
CREATE INDEX IF NOT EXISTS idx_auth_log_created_at ON auth_log(created_at DESC);

-- Delivery log
CREATE TABLE IF NOT EXISTS delivery_log (
    id BIGSERIAL PRIMARY KEY,
    message_id TEXT,
    sender TEXT NOT NULL,
    recipient TEXT NOT NULL,
    status TEXT NOT NULL,
    direction TEXT DEFAULT 'inbound',
    smtp_code INTEGER,
    error_message TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_delivery_log_time ON delivery_log(created_at);
CREATE INDEX IF NOT EXISTS idx_delivery_log_status ON delivery_log(status);
CREATE INDEX IF NOT EXISTS idx_delivery_log_sender ON delivery_log(sender);
CREATE INDEX IF NOT EXISTS idx_delivery_log_recipient ON delivery_log(recipient);
CREATE INDEX IF NOT EXISTS idx_delivery_log_created_at ON delivery_log(created_at DESC);

-- API Keys
CREATE TABLE IF NOT EXISTS api_keys (
    id BIGSERIAL PRIMARY KEY,
    domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    key_hash TEXT NOT NULL,
    key_prefix TEXT NOT NULL,
    name TEXT NOT NULL,
    scopes TEXT DEFAULT '["send"]',
    is_active BOOLEAN DEFAULT TRUE,
    rate_limit_per_hour INTEGER DEFAULT 1000,
    last_used_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX IF NOT EXISTS idx_api_keys_domain ON api_keys(domain_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(key_prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys(is_active);

-- Email Templates
CREATE TABLE IF NOT EXISTS email_templates (
    id BIGSERIAL PRIMARY KEY,
    domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    subject TEXT NOT NULL,
    html_body TEXT,
    text_body TEXT,
    variables TEXT,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(domain_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_email_templates_domain ON email_templates(domain_id);
CREATE INDEX IF NOT EXISTS idx_email_templates_slug ON email_templates(domain_id, slug);

-- Sent Emails Tracking
CREATE TABLE IF NOT EXISTS sent_emails (
    id BIGSERIAL PRIMARY KEY,
    domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    api_key_id BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    message_id TEXT UNIQUE,
    tracking_id TEXT UNIQUE,
    from_email TEXT NOT NULL,
    to_email TEXT NOT NULL,
    subject TEXT,
    template_slug TEXT,
    tags TEXT,
    status TEXT DEFAULT 'queued',
    smtp_response TEXT,
    opened_at TIMESTAMP WITH TIME ZONE,
    opened_count INTEGER DEFAULT 0,
    clicked_at TIMESTAMP WITH TIME ZONE,
    clicked_count INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    delivered_at TIMESTAMP WITH TIME ZONE,
    bounced_at TIMESTAMP WITH TIME ZONE,
    bounce_reason TEXT
);

CREATE INDEX IF NOT EXISTS idx_sent_emails_domain ON sent_emails(domain_id);
CREATE INDEX IF NOT EXISTS idx_sent_emails_api_key ON sent_emails(api_key_id);
CREATE INDEX IF NOT EXISTS idx_sent_emails_message_id ON sent_emails(message_id);
CREATE INDEX IF NOT EXISTS idx_sent_emails_tracking_id ON sent_emails(tracking_id);
CREATE INDEX IF NOT EXISTS idx_sent_emails_status ON sent_emails(status);
CREATE INDEX IF NOT EXISTS idx_sent_emails_created ON sent_emails(created_at);
CREATE INDEX IF NOT EXISTS idx_sent_emails_to ON sent_emails(to_email);

-- Webhooks
CREATE TABLE IF NOT EXISTS webhooks (
    id BIGSERIAL PRIMARY KEY,
    domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    events TEXT NOT NULL,
    secret TEXT NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    failure_count INTEGER DEFAULT 0,
    last_triggered_at TIMESTAMP WITH TIME ZONE,
    last_success_at TIMESTAMP WITH TIME ZONE,
    last_failure_at TIMESTAMP WITH TIME ZONE,
    last_failure_reason TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhooks_domain ON webhooks(domain_id);
CREATE INDEX IF NOT EXISTS idx_webhooks_active ON webhooks(is_active);

-- Webhook Deliveries
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id BIGSERIAL PRIMARY KEY,
    webhook_id BIGINT NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    response_code INTEGER,
    response_body TEXT,
    success BOOLEAN DEFAULT FALSE,
    attempt_count INTEGER DEFAULT 1,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook ON webhook_deliveries(webhook_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_created ON webhook_deliveries(created_at);

-- TOTP Trusted Devices
CREATE TABLE IF NOT EXISTS totp_trusted_devices (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_token TEXT NOT NULL UNIQUE,
    device_name TEXT,
    ip_address TEXT,
    user_agent TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    last_used_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_totp_trusted_devices_user ON totp_trusted_devices(user_id);
CREATE INDEX IF NOT EXISTS idx_totp_trusted_devices_token ON totp_trusted_devices(device_token);
CREATE INDEX IF NOT EXISTS idx_totp_trusted_devices_expires ON totp_trusted_devices(expires_at);
