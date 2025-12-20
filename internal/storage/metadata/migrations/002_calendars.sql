-- Calendar and Contacts tables for CalDAV/CardDAV support

-- Calendars table
CREATE TABLE IF NOT EXISTS calendars (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    uid TEXT NOT NULL UNIQUE,                  -- CalDAV resource ID
    name TEXT NOT NULL,
    description TEXT,
    color TEXT DEFAULT '#0066CC',              -- Hex color for UI
    timezone TEXT DEFAULT 'UTC',
    ctag TEXT NOT NULL,                        -- Change tag for sync
    is_default BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_calendars_user ON calendars(user_id);
CREATE INDEX IF NOT EXISTS idx_calendars_uid ON calendars(uid);

-- Calendar events
CREATE TABLE IF NOT EXISTS calendar_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    calendar_id INTEGER NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
    uid TEXT NOT NULL,                         -- iCalendar UID
    etag TEXT NOT NULL,                        -- For sync
    icalendar_data TEXT NOT NULL,              -- Raw iCalendar data
    summary TEXT,                              -- For quick display
    description TEXT,
    location TEXT,
    start_time DATETIME,
    end_time DATETIME,
    all_day BOOLEAN DEFAULT FALSE,
    recurrence_rule TEXT,                      -- RRULE if recurring
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(calendar_id, uid)
);

CREATE INDEX IF NOT EXISTS idx_events_calendar ON calendar_events(calendar_id);
CREATE INDEX IF NOT EXISTS idx_events_uid ON calendar_events(uid);
CREATE INDEX IF NOT EXISTS idx_events_time ON calendar_events(start_time, end_time);

-- Address books
CREATE TABLE IF NOT EXISTS addressbooks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    uid TEXT NOT NULL UNIQUE,                  -- CardDAV resource ID
    name TEXT NOT NULL,
    description TEXT,
    ctag TEXT NOT NULL,                        -- Change tag for sync
    is_default BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_addressbooks_user ON addressbooks(user_id);
CREATE INDEX IF NOT EXISTS idx_addressbooks_uid ON addressbooks(uid);

-- Contacts
CREATE TABLE IF NOT EXISTS contacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    addressbook_id INTEGER NOT NULL REFERENCES addressbooks(id) ON DELETE CASCADE,
    uid TEXT NOT NULL,                         -- vCard UID
    etag TEXT NOT NULL,                        -- For sync
    vcard_data TEXT NOT NULL,                  -- Raw vCard data
    full_name TEXT,                            -- For display/search
    given_name TEXT,
    family_name TEXT,
    nickname TEXT,
    emails TEXT,                               -- JSON array
    phones TEXT,                               -- JSON array
    organization TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(addressbook_id, uid)
);

CREATE INDEX IF NOT EXISTS idx_contacts_addressbook ON contacts(addressbook_id);
CREATE INDEX IF NOT EXISTS idx_contacts_uid ON contacts(uid);
CREATE INDEX IF NOT EXISTS idx_contacts_name ON contacts(full_name);
CREATE INDEX IF NOT EXISTS idx_contacts_email ON contacts(emails);

-- Update schema version
INSERT INTO schema_migrations (version) VALUES (2);
