-- Migration 017: Observability Improvements
-- Adds tracing and metrics columns to delivery_log, circuit breaker history table

-- Add observability columns to delivery_log
ALTER TABLE delivery_log ADD COLUMN trace_id TEXT;
ALTER TABLE delivery_log ADD COLUMN domain TEXT;
ALTER TABLE delivery_log ADD COLUMN attempt_number INTEGER DEFAULT 1;
ALTER TABLE delivery_log ADD COLUMN delivery_duration_ms INTEGER;
ALTER TABLE delivery_log ADD COLUMN circuit_breaker_state TEXT;

-- Index for trace ID lookups
CREATE INDEX IF NOT EXISTS idx_delivery_log_trace_id ON delivery_log(trace_id);

-- Index for per-domain analysis
CREATE INDEX IF NOT EXISTS idx_delivery_log_domain ON delivery_log(domain);

-- Circuit breaker state history for analysis and debugging
CREATE TABLE IF NOT EXISTS circuit_breaker_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    domain TEXT,
    from_state TEXT NOT NULL,
    to_state TEXT NOT NULL,
    failure_count INTEGER,
    success_count INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cb_history_name ON circuit_breaker_history(name);
CREATE INDEX IF NOT EXISTS idx_cb_history_domain ON circuit_breaker_history(domain);
CREATE INDEX IF NOT EXISTS idx_cb_history_created ON circuit_breaker_history(created_at);

INSERT INTO schema_migrations (version) VALUES (17);
