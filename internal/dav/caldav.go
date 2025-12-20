package dav

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// CalDAVBackend implements caldav.Backend
type CalDAVBackend struct {
	db *sql.DB
}

// NewCalDAVBackend creates a new CalDAV backend
func NewCalDAVBackend(db *sql.DB) *CalDAVBackend {
	return &CalDAVBackend{db: db}
}

// Calendar represents a CalDAV calendar
type Calendar struct {
	ID          int64
	UserID      int64
	UID         string
	Name        string
	Description string
	Color       string
	Timezone    string
	CTag        string
	IsDefault   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CalendarEvent represents an event in a calendar
type CalendarEvent struct {
	ID            int64
	CalendarID    int64
	UID           string
	ETag          string
	ICalendarData string
	Summary       string
	Description   string
	Location      string
	StartTime     time.Time
	EndTime       time.Time
	AllDay        bool
	Recurrence    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateCalendar creates a new calendar for a user
func (b *CalDAVBackend) CreateCalendar(ctx context.Context, userID int64, name, description string) (*Calendar, error) {
	uid := generateUID()
	ctag := generateCTag()

	result, err := b.db.ExecContext(ctx,
		`INSERT INTO calendars (user_id, uid, name, description, ctag)
		 VALUES (?, ?, ?, ?, ?)`,
		userID, uid, name, description, ctag,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create calendar: %w", err)
	}

	id, _ := result.LastInsertId()

	return &Calendar{
		ID:          id,
		UserID:      userID,
		UID:         uid,
		Name:        name,
		Description: description,
		CTag:        ctag,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, nil
}

// GetCalendar retrieves a calendar by UID
func (b *CalDAVBackend) GetCalendar(ctx context.Context, uid string) (*Calendar, error) {
	var cal Calendar
	var description sql.NullString

	err := b.db.QueryRowContext(ctx,
		`SELECT id, user_id, uid, name, description, color, timezone, ctag, is_default, created_at, updated_at
		 FROM calendars WHERE uid = ?`,
		uid,
	).Scan(&cal.ID, &cal.UserID, &cal.UID, &cal.Name, &description, &cal.Color,
		&cal.Timezone, &cal.CTag, &cal.IsDefault, &cal.CreatedAt, &cal.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("calendar not found: %s", uid)
		}
		return nil, err
	}

	cal.Description = description.String
	return &cal, nil
}

// ListCalendars returns all calendars for a user
func (b *CalDAVBackend) ListCalendars(ctx context.Context, userID int64) ([]*Calendar, error) {
	rows, err := b.db.QueryContext(ctx,
		`SELECT id, user_id, uid, name, description, color, timezone, ctag, is_default, created_at, updated_at
		 FROM calendars WHERE user_id = ? ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var calendars []*Calendar
	for rows.Next() {
		var cal Calendar
		var description sql.NullString

		if err := rows.Scan(&cal.ID, &cal.UserID, &cal.UID, &cal.Name, &description, &cal.Color,
			&cal.Timezone, &cal.CTag, &cal.IsDefault, &cal.CreatedAt, &cal.UpdatedAt); err != nil {
			return nil, err
		}

		cal.Description = description.String
		calendars = append(calendars, &cal)
	}

	return calendars, rows.Err()
}

// UpdateCalendar updates calendar properties
func (b *CalDAVBackend) UpdateCalendar(ctx context.Context, uid string, name, description, color string) error {
	ctag := generateCTag()

	result, err := b.db.ExecContext(ctx,
		`UPDATE calendars SET name = ?, description = ?, color = ?, ctag = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE uid = ?`,
		name, description, color, ctag, uid,
	)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("calendar not found: %s", uid)
	}

	return nil
}

// DeleteCalendar removes a calendar and all its events
func (b *CalDAVBackend) DeleteCalendar(ctx context.Context, uid string) error {
	result, err := b.db.ExecContext(ctx, "DELETE FROM calendars WHERE uid = ?", uid)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("calendar not found: %s", uid)
	}

	return nil
}

// CreateEvent adds a new event to a calendar
func (b *CalDAVBackend) CreateEvent(ctx context.Context, calendarUID string, event *CalendarEvent) error {
	// Get calendar ID
	var calID int64
	err := b.db.QueryRowContext(ctx, "SELECT id FROM calendars WHERE uid = ?", calendarUID).Scan(&calID)
	if err != nil {
		return fmt.Errorf("calendar not found: %s", calendarUID)
	}

	event.CalendarID = calID
	event.ETag = generateETag()

	_, err = b.db.ExecContext(ctx,
		`INSERT INTO calendar_events (calendar_id, uid, etag, icalendar_data, summary, description, location, start_time, end_time, all_day, recurrence_rule)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		calID, event.UID, event.ETag, event.ICalendarData, event.Summary, event.Description,
		event.Location, event.StartTime, event.EndTime, event.AllDay, event.Recurrence,
	)
	if err != nil {
		return fmt.Errorf("failed to create event: %w", err)
	}

	// Update calendar ctag
	b.db.ExecContext(ctx, "UPDATE calendars SET ctag = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		generateCTag(), calID)

	return nil
}

// GetEvent retrieves an event by UID
func (b *CalDAVBackend) GetEvent(ctx context.Context, calendarUID, eventUID string) (*CalendarEvent, error) {
	var event CalendarEvent
	var description, location, recurrence sql.NullString

	err := b.db.QueryRowContext(ctx,
		`SELECT e.id, e.calendar_id, e.uid, e.etag, e.icalendar_data, e.summary, e.description,
		        e.location, e.start_time, e.end_time, e.all_day, e.recurrence_rule, e.created_at, e.updated_at
		 FROM calendar_events e
		 JOIN calendars c ON e.calendar_id = c.id
		 WHERE c.uid = ? AND e.uid = ?`,
		calendarUID, eventUID,
	).Scan(&event.ID, &event.CalendarID, &event.UID, &event.ETag, &event.ICalendarData,
		&event.Summary, &description, &location, &event.StartTime, &event.EndTime,
		&event.AllDay, &recurrence, &event.CreatedAt, &event.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("event not found: %s", eventUID)
		}
		return nil, err
	}

	event.Description = description.String
	event.Location = location.String
	event.Recurrence = recurrence.String
	return &event, nil
}

// ListEvents returns all events in a calendar
func (b *CalDAVBackend) ListEvents(ctx context.Context, calendarUID string) ([]*CalendarEvent, error) {
	rows, err := b.db.QueryContext(ctx,
		`SELECT e.id, e.calendar_id, e.uid, e.etag, e.icalendar_data, e.summary, e.description,
		        e.location, e.start_time, e.end_time, e.all_day, e.recurrence_rule, e.created_at, e.updated_at
		 FROM calendar_events e
		 JOIN calendars c ON e.calendar_id = c.id
		 WHERE c.uid = ?
		 ORDER BY e.start_time`,
		calendarUID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*CalendarEvent
	for rows.Next() {
		var event CalendarEvent
		var description, location, recurrence sql.NullString

		if err := rows.Scan(&event.ID, &event.CalendarID, &event.UID, &event.ETag, &event.ICalendarData,
			&event.Summary, &description, &location, &event.StartTime, &event.EndTime,
			&event.AllDay, &recurrence, &event.CreatedAt, &event.UpdatedAt); err != nil {
			return nil, err
		}

		event.Description = description.String
		event.Location = location.String
		event.Recurrence = recurrence.String
		events = append(events, &event)
	}

	return events, rows.Err()
}

// ListEventsInRange returns events within a time range
func (b *CalDAVBackend) ListEventsInRange(ctx context.Context, calendarUID string, start, end time.Time) ([]*CalendarEvent, error) {
	rows, err := b.db.QueryContext(ctx,
		`SELECT e.id, e.calendar_id, e.uid, e.etag, e.icalendar_data, e.summary, e.description,
		        e.location, e.start_time, e.end_time, e.all_day, e.recurrence_rule, e.created_at, e.updated_at
		 FROM calendar_events e
		 JOIN calendars c ON e.calendar_id = c.id
		 WHERE c.uid = ? AND e.start_time >= ? AND e.end_time <= ?
		 ORDER BY e.start_time`,
		calendarUID, start, end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*CalendarEvent
	for rows.Next() {
		var event CalendarEvent
		var description, location, recurrence sql.NullString

		if err := rows.Scan(&event.ID, &event.CalendarID, &event.UID, &event.ETag, &event.ICalendarData,
			&event.Summary, &description, &location, &event.StartTime, &event.EndTime,
			&event.AllDay, &recurrence, &event.CreatedAt, &event.UpdatedAt); err != nil {
			return nil, err
		}

		event.Description = description.String
		event.Location = location.String
		event.Recurrence = recurrence.String
		events = append(events, &event)
	}

	return events, rows.Err()
}

// UpdateEvent updates an existing event
func (b *CalDAVBackend) UpdateEvent(ctx context.Context, calendarUID string, event *CalendarEvent) error {
	event.ETag = generateETag()

	result, err := b.db.ExecContext(ctx,
		`UPDATE calendar_events SET etag = ?, icalendar_data = ?, summary = ?, description = ?,
		        location = ?, start_time = ?, end_time = ?, all_day = ?, recurrence_rule = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE uid = ? AND calendar_id = (SELECT id FROM calendars WHERE uid = ?)`,
		event.ETag, event.ICalendarData, event.Summary, event.Description, event.Location,
		event.StartTime, event.EndTime, event.AllDay, event.Recurrence, event.UID, calendarUID,
	)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("event not found: %s", event.UID)
	}

	// Update calendar ctag
	b.db.ExecContext(ctx,
		"UPDATE calendars SET ctag = ?, updated_at = CURRENT_TIMESTAMP WHERE uid = ?",
		generateCTag(), calendarUID)

	return nil
}

// DeleteEvent removes an event from a calendar
func (b *CalDAVBackend) DeleteEvent(ctx context.Context, calendarUID, eventUID string) error {
	result, err := b.db.ExecContext(ctx,
		`DELETE FROM calendar_events
		 WHERE uid = ? AND calendar_id = (SELECT id FROM calendars WHERE uid = ?)`,
		eventUID, calendarUID,
	)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("event not found: %s", eventUID)
	}

	// Update calendar ctag
	b.db.ExecContext(ctx,
		"UPDATE calendars SET ctag = ?, updated_at = CURRENT_TIMESTAMP WHERE uid = ?",
		generateCTag(), calendarUID)

	return nil
}

// Helper functions

func generateUID() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

func generateCTag() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func generateETag() string {
	buf := make([]byte, 8)
	rand.Read(buf)
	return fmt.Sprintf("\"%s\"", hex.EncodeToString(buf))
}
