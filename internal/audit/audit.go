package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// EventType represents the type of audit event
type EventType string

const (
	EventUserCreate       EventType = "user.create"
	EventUserDelete       EventType = "user.delete"
	EventUserUpdate       EventType = "user.update"
	EventPasswordChange   EventType = "password.change"
	EventDomainCreate     EventType = "domain.create"
	EventDomainDelete     EventType = "domain.delete"
	EventLoginSuccess     EventType = "login.success"
	EventLoginFailure     EventType = "login.failure"
	EventSieveUpdate      EventType = "sieve.update"
	EventQueueRetry       EventType = "queue.retry"
	EventQueueDelete      EventType = "queue.delete"
	EventConfigChange     EventType = "config.change"
)

// Event represents an audit log entry
type Event struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Actor     string    `json:"actor"`     // Email of the admin performing the action
	Action    EventType `json:"action"`    // Type of action
	Target    string    `json:"target"`    // Affected user/domain/resource
	Details   string    `json:"details"`   // JSON with additional context
	IPAddress string    `json:"ip_address"`
}

// Logger handles audit logging
type Logger struct {
	db *sql.DB
}

// NewLogger creates a new audit logger
func NewLogger(db *sql.DB) (*Logger, error) {
	if db == nil {
		return nil, nil // Return nil logger if no database (graceful degradation)
	}

	// Create audit_log table if it doesn't exist
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			actor TEXT NOT NULL,
			action TEXT NOT NULL,
			target TEXT,
			details TEXT,
			ip_address TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
		CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_log(actor);
		CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);
	`)
	if err != nil {
		return nil, err
	}

	return &Logger{db: db}, nil
}

// Log records an audit event
func (l *Logger) Log(ctx context.Context, actor string, action EventType, target string, details map[string]interface{}, ipAddress string) error {
	if l == nil || l.db == nil {
		return nil // Graceful degradation if logger not configured
	}

	var detailsJSON string
	if details != nil {
		data, err := json.Marshal(details)
		if err != nil {
			detailsJSON = "{}"
		} else {
			detailsJSON = string(data)
		}
	}

	_, err := l.db.ExecContext(ctx,
		`INSERT INTO audit_log (actor, action, target, details, ip_address) VALUES (?, ?, ?, ?, ?)`,
		actor, string(action), target, detailsJSON, ipAddress,
	)
	return err
}

// LogSimple logs an event without details
func (l *Logger) LogSimple(ctx context.Context, actor string, action EventType, target, ipAddress string) error {
	return l.Log(ctx, actor, action, target, nil, ipAddress)
}

// QueryFilter defines filters for querying audit logs
type QueryFilter struct {
	Actor     string
	Action    EventType
	Target    string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
	Offset    int
}

// Query retrieves audit events based on filters
func (l *Logger) Query(ctx context.Context, filter QueryFilter) ([]Event, error) {
	if l == nil || l.db == nil {
		return nil, nil
	}

	query := `SELECT id, timestamp, actor, action, target, details, ip_address FROM audit_log WHERE 1=1`
	args := []interface{}{}

	if filter.Actor != "" {
		query += " AND actor = ?"
		args = append(args, filter.Actor)
	}
	if filter.Action != "" {
		query += " AND action = ?"
		args = append(args, string(filter.Action))
	}
	if filter.Target != "" {
		query += " AND target LIKE ?"
		args = append(args, "%"+filter.Target+"%")
	}
	if !filter.StartTime.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.StartTime)
	}
	if !filter.EndTime.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.EndTime)
	}

	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	} else {
		query += " LIMIT 100" // Default limit
	}

	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var target, details, ip sql.NullString
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Actor, &e.Action, &target, &details, &ip); err != nil {
			return nil, err
		}
		e.Target = target.String
		e.Details = details.String
		e.IPAddress = ip.String
		events = append(events, e)
	}

	return events, rows.Err()
}

// GetRecent retrieves the most recent audit events
func (l *Logger) GetRecent(ctx context.Context, limit int) ([]Event, error) {
	return l.Query(ctx, QueryFilter{Limit: limit})
}

// Count returns the total number of audit events matching the filter
func (l *Logger) Count(ctx context.Context, filter QueryFilter) (int, error) {
	if l == nil || l.db == nil {
		return 0, nil
	}

	query := `SELECT COUNT(*) FROM audit_log WHERE 1=1`
	args := []interface{}{}

	if filter.Actor != "" {
		query += " AND actor = ?"
		args = append(args, filter.Actor)
	}
	if filter.Action != "" {
		query += " AND action = ?"
		args = append(args, string(filter.Action))
	}

	var count int
	err := l.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}
