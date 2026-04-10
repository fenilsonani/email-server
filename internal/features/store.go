package features

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Common errors
var (
	ErrNotFound       = errors.New("not found")
	ErrAlreadyExists  = errors.New("already exists")
	ErrInvalidInput   = errors.New("invalid input")
)

// Store handles all unique feature database operations
type Store struct {
	db *sql.DB
}

// NewStore creates a new features store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// =============================================================================
// Screener Operations
// =============================================================================

// GetScreenerStatus checks if a sender is approved, blocked, or unknown
func (s *Store) GetScreenerStatus(ctx context.Context, userID int64, senderEmail string) (ScreenerStatus, error) {
	senderEmail = strings.ToLower(strings.TrimSpace(senderEmail))

	// Extract domain from email
	parts := strings.SplitN(senderEmail, "@", 2)
	if len(parts) != 2 {
		return ScreenerPending, ErrInvalidInput
	}
	domain := parts[1]

	// Check for exact email match first, then domain match
	var status string
	err := s.db.QueryRowContext(ctx, `
		SELECT status FROM screener_contacts
		WHERE user_id = ? AND (email = ? OR domain = ?)
		ORDER BY CASE WHEN email IS NOT NULL THEN 0 ELSE 1 END
		LIMIT 1
	`, userID, senderEmail, domain).Scan(&status)

	if errors.Is(err, sql.ErrNoRows) {
		return ScreenerPending, nil // Unknown sender
	}
	if err != nil {
		return ScreenerPending, fmt.Errorf("failed to check screener status: %w", err)
	}

	return ScreenerStatus(status), nil
}

// ApproveContact approves a sender (email or domain)
func (s *Store) ApproveContact(ctx context.Context, userID int64, email, domain string) error {
	return s.setScreenerStatus(ctx, userID, email, domain, ScreenerApproved)
}

// BlockContact blocks a sender (email or domain)
func (s *Store) BlockContact(ctx context.Context, userID int64, email, domain string) error {
	return s.setScreenerStatus(ctx, userID, email, domain, ScreenerBlocked)
}

func (s *Store) setScreenerStatus(ctx context.Context, userID int64, email, domain string, status ScreenerStatus) error {
	email = strings.ToLower(strings.TrimSpace(email))
	domain = strings.ToLower(strings.TrimSpace(domain))

	if email == "" && domain == "" {
		return ErrInvalidInput
	}

	now := time.Now()

	// Upsert the contact
	if email != "" {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO screener_contacts (user_id, email, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id, email) DO UPDATE SET status = ?, updated_at = ?
		`, userID, email, status, now, now, status, now)
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO screener_contacts (user_id, domain, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, domain) DO UPDATE SET status = ?, updated_at = ?
	`, userID, domain, status, now, now, status, now)
	return err
}

// ListScreenerContacts lists all contacts in the screener for a user
func (s *Store) ListScreenerContacts(ctx context.Context, userID int64, status ScreenerStatus) ([]*ScreenerContact, error) {
	query := `SELECT id, user_id, email, domain, status, created_at, updated_at
		FROM screener_contacts WHERE user_id = ?`
	args := []any{userID}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY updated_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []*ScreenerContact
	for rows.Next() {
		c := &ScreenerContact{}
		var email, domain sql.NullString
		err := rows.Scan(&c.ID, &c.UserID, &email, &domain, &c.Status, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			return nil, err
		}
		c.Email = email.String
		c.Domain = domain.String
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

// DeleteScreenerContact removes a contact from the screener
func (s *Store) DeleteScreenerContact(ctx context.Context, userID, contactID int64) error {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM screener_contacts WHERE id = ? AND user_id = ?",
		contactID, userID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// =============================================================================
// Email Aliases Operations
// =============================================================================

// CreateAlias creates a new email alias
func (s *Store) CreateAlias(ctx context.Context, alias *EmailAlias) error {
	alias.AliasAddress = strings.ToLower(strings.TrimSpace(alias.AliasAddress))
	alias.AliasLocal = strings.ToLower(strings.TrimSpace(alias.AliasLocal))
	alias.CreatedAt = time.Now()
	alias.IsActive = true

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO email_aliases (user_id, domain_id, alias_address, alias_local, description, is_active, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, alias.UserID, alias.DomainID, alias.AliasAddress, alias.AliasLocal, alias.Description, alias.IsActive, alias.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrAlreadyExists
		}
		return err
	}

	alias.ID, _ = result.LastInsertId()
	return nil
}

// GetAliasByAddress finds an alias by its full address
func (s *Store) GetAliasByAddress(ctx context.Context, address string) (*EmailAlias, error) {
	address = strings.ToLower(strings.TrimSpace(address))

	alias := &EmailAlias{}
	var lastUsed sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, domain_id, alias_address, alias_local, description,
		       is_active, created_at, last_used_at, email_count
		FROM email_aliases WHERE alias_address = ?
	`, address).Scan(
		&alias.ID, &alias.UserID, &alias.DomainID, &alias.AliasAddress, &alias.AliasLocal,
		&alias.Description, &alias.IsActive, &alias.CreatedAt, &lastUsed, &alias.EmailCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if lastUsed.Valid {
		alias.LastUsedAt = lastUsed.Time
	}
	return alias, nil
}

// ListAliases lists all aliases for a user
func (s *Store) ListAliases(ctx context.Context, userID int64) ([]*EmailAlias, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, domain_id, alias_address, alias_local, description,
		       is_active, created_at, last_used_at, email_count
		FROM email_aliases WHERE user_id = ? ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aliases []*EmailAlias
	for rows.Next() {
		a := &EmailAlias{}
		var lastUsed sql.NullTime
		err := rows.Scan(
			&a.ID, &a.UserID, &a.DomainID, &a.AliasAddress, &a.AliasLocal,
			&a.Description, &a.IsActive, &a.CreatedAt, &lastUsed, &a.EmailCount,
		)
		if err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			a.LastUsedAt = lastUsed.Time
		}
		aliases = append(aliases, a)
	}
	return aliases, rows.Err()
}

// UpdateAlias updates an alias (enable/disable, description)
func (s *Store) UpdateAlias(ctx context.Context, userID, aliasID int64, isActive *bool, description *string) error {
	updates := []string{}
	args := []any{}

	if isActive != nil {
		updates = append(updates, "is_active = ?")
		args = append(args, *isActive)
	}
	if description != nil {
		updates = append(updates, "description = ?")
		args = append(args, *description)
	}

	if len(updates) == 0 {
		return nil
	}

	args = append(args, aliasID, userID)
	query := fmt.Sprintf("UPDATE email_aliases SET %s WHERE id = ? AND user_id = ?", strings.Join(updates, ", "))

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteAlias deletes an alias
func (s *Store) DeleteAlias(ctx context.Context, userID, aliasID int64) error {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM email_aliases WHERE id = ? AND user_id = ?",
		aliasID, userID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// IncrementAliasCount increments the email count and updates last_used_at
func (s *Store) IncrementAliasCount(ctx context.Context, aliasID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE email_aliases SET email_count = email_count + 1, last_used_at = ? WHERE id = ?
	`, time.Now(), aliasID)
	return err
}

// GenerateAliasLocal generates a random alias local part
// Format: prefix_randomhex (e.g., "shop_a1b2c3d4" or just "a1b2c3d4" if no prefix)
func GenerateAliasLocal(prefix string) string {
	b := make([]byte, 4) // 8 hex chars
	rand.Read(b)
	suffix := hex.EncodeToString(b)
	if prefix == "" {
		return suffix
	}
	// Clean prefix: lowercase, replace spaces with underscores
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	prefix = strings.ReplaceAll(prefix, " ", "_")
	// Remove any characters that aren't alphanumeric or underscore
	var cleaned strings.Builder
	for _, r := range prefix {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			cleaned.WriteRune(r)
		}
	}
	if cleaned.Len() == 0 {
		return suffix
	}
	return cleaned.String() + "_" + suffix
}

// =============================================================================
// Scheduled Email Operations
// =============================================================================

// CreateScheduledEmail schedules an email to be sent later
func (s *Store) CreateScheduledEmail(ctx context.Context, email *ScheduledEmail) error {
	recipientsJSON, _ := json.Marshal(email.Recipients)
	headersJSON, _ := json.Marshal(email.Headers)
	email.Status = ScheduledStatusPending
	email.CreatedAt = time.Now()

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO scheduled_emails
		(user_id, send_at, from_address, recipients, subject, body, html_body, headers, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, email.UserID, email.SendAt, email.FromAddress, string(recipientsJSON),
		email.Subject, email.Body, email.HTMLBody, string(headersJSON), email.Status, email.CreatedAt)
	if err != nil {
		return err
	}

	email.ID, _ = result.LastInsertId()
	return nil
}

// GetScheduledEmail gets a scheduled email by ID
func (s *Store) GetScheduledEmail(ctx context.Context, userID, emailID int64) (*ScheduledEmail, error) {
	email := &ScheduledEmail{}
	var recipientsJSON, headersJSON string
	var sentAt sql.NullTime
	var errStr sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, send_at, from_address, recipients, subject, body, html_body,
		       headers, status, created_at, sent_at, error
		FROM scheduled_emails WHERE id = ? AND user_id = ?
	`, emailID, userID).Scan(
		&email.ID, &email.UserID, &email.SendAt, &email.FromAddress, &recipientsJSON,
		&email.Subject, &email.Body, &email.HTMLBody, &headersJSON, &email.Status,
		&email.CreatedAt, &sentAt, &errStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(recipientsJSON), &email.Recipients); err != nil {
		return nil, fmt.Errorf("scheduled email %d has malformed recipients: %w", email.ID, err)
	}
	if err := json.Unmarshal([]byte(headersJSON), &email.Headers); err != nil {
		return nil, fmt.Errorf("scheduled email %d has malformed headers: %w", email.ID, err)
	}
	if sentAt.Valid {
		email.SentAt = sentAt.Time
	}
	email.Error = errStr.String

	return email, nil
}

// ListScheduledEmails lists scheduled emails for a user
func (s *Store) ListScheduledEmails(ctx context.Context, userID int64, status string) ([]*ScheduledEmail, error) {
	query := `SELECT id, user_id, send_at, from_address, recipients, subject, status, created_at
		FROM scheduled_emails WHERE user_id = ?`
	args := []any{userID}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY send_at ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var emails []*ScheduledEmail
	for rows.Next() {
		e := &ScheduledEmail{}
		var recipientsJSON string
		err := rows.Scan(&e.ID, &e.UserID, &e.SendAt, &e.FromAddress, &recipientsJSON,
			&e.Subject, &e.Status, &e.CreatedAt)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(recipientsJSON), &e.Recipients); err != nil {
			return nil, fmt.Errorf("scheduled email %d has malformed recipients: %w", e.ID, err)
		}
		emails = append(emails, e)
	}
	return emails, rows.Err()
}

// GetPendingScheduledEmails gets emails ready to be sent
func (s *Store) GetPendingScheduledEmails(ctx context.Context) ([]*ScheduledEmail, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, send_at, from_address, recipients, subject, body, html_body, headers
		FROM scheduled_emails
		WHERE status = ? AND send_at <= ?
		ORDER BY send_at ASC
		LIMIT 100
	`, ScheduledStatusPending, time.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var emails []*ScheduledEmail
	for rows.Next() {
		e := &ScheduledEmail{}
		var recipientsJSON, headersJSON sql.NullString
		err := rows.Scan(&e.ID, &e.UserID, &e.SendAt, &e.FromAddress, &recipientsJSON,
			&e.Subject, &e.Body, &e.HTMLBody, &headersJSON)
		if err != nil {
			return nil, err
		}
		if recipientsJSON.Valid {
			if err := json.Unmarshal([]byte(recipientsJSON.String), &e.Recipients); err != nil {
				return nil, fmt.Errorf("scheduled email %d has malformed recipients: %w", e.ID, err)
			}
		}
		if headersJSON.Valid {
			if err := json.Unmarshal([]byte(headersJSON.String), &e.Headers); err != nil {
				return nil, fmt.Errorf("scheduled email %d has malformed headers: %w", e.ID, err)
			}
		}
		emails = append(emails, e)
	}
	return emails, rows.Err()
}

// UpdateScheduledEmailStatus updates the status of a scheduled email
func (s *Store) UpdateScheduledEmailStatus(ctx context.Context, emailID int64, status string, errMsg string) error {
	var sentAt any
	if status == ScheduledStatusSent {
		sentAt = time.Now()
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE scheduled_emails SET status = ?, sent_at = ?, error = ? WHERE id = ?
	`, status, sentAt, errMsg, emailID)
	return err
}

// CancelScheduledEmail cancels a pending scheduled email
func (s *Store) CancelScheduledEmail(ctx context.Context, userID, emailID int64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE scheduled_emails SET status = ? WHERE id = ? AND user_id = ? AND status = ?
	`, ScheduledStatusCancelled, emailID, userID, ScheduledStatusPending)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// =============================================================================
// VIP Contacts Operations
// =============================================================================

// AddVIP adds a VIP contact
func (s *Store) AddVIP(ctx context.Context, vip *VIPContact) error {
	vip.Email = strings.ToLower(strings.TrimSpace(vip.Email))
	vip.CreatedAt = time.Now()

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO vip_contacts (user_id, email, name, created_at)
		VALUES (?, ?, ?, ?)
	`, vip.UserID, vip.Email, vip.Name, vip.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrAlreadyExists
		}
		return err
	}

	vip.ID, _ = result.LastInsertId()
	return nil
}

// IsVIP checks if an email is a VIP for a user
func (s *Store) IsVIP(ctx context.Context, userID int64, email string) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM vip_contacts WHERE user_id = ? AND email = ?",
		userID, email).Scan(&count)
	return count > 0, err
}

// ListVIPs lists all VIP contacts for a user
func (s *Store) ListVIPs(ctx context.Context, userID int64) ([]*VIPContact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, email, name, created_at FROM vip_contacts
		WHERE user_id = ? ORDER BY name, email
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var vips []*VIPContact
	for rows.Next() {
		v := &VIPContact{}
		var name sql.NullString
		err := rows.Scan(&v.ID, &v.UserID, &v.Email, &name, &v.CreatedAt)
		if err != nil {
			return nil, err
		}
		v.Name = name.String
		vips = append(vips, v)
	}
	return vips, rows.Err()
}

// DeleteVIP removes a VIP contact
func (s *Store) DeleteVIP(ctx context.Context, userID, vipID int64) error {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM vip_contacts WHERE id = ? AND user_id = ?",
		vipID, userID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// =============================================================================
// User Preferences Operations
// =============================================================================

// GetPreferences gets user preferences, creating defaults if not exist
func (s *Store) GetPreferences(ctx context.Context, userID int64) (*UserPreferences, error) {
	prefs := &UserPreferences{}
	err := s.db.QueryRowContext(ctx, `
		SELECT user_id, undo_send_delay, screener_enabled, tracker_blocking,
		       zones_enabled, snooze_mark_unread
		FROM user_preferences WHERE user_id = ?
	`, userID).Scan(
		&prefs.UserID, &prefs.UndoSendDelay, &prefs.ScreenerEnabled,
		&prefs.TrackerBlocking, &prefs.ZonesEnabled, &prefs.SnoozeMarkUnread,
	)

	if errors.Is(err, sql.ErrNoRows) {
		// Create default preferences
		prefs = DefaultUserPreferences(userID)
		err = s.SavePreferences(ctx, prefs)
		if err != nil {
			return nil, err
		}
		return prefs, nil
	}
	if err != nil {
		return nil, err
	}
	return prefs, nil
}

// SavePreferences saves user preferences
func (s *Store) SavePreferences(ctx context.Context, prefs *UserPreferences) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_preferences
		(user_id, undo_send_delay, screener_enabled, tracker_blocking, zones_enabled, snooze_mark_unread, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET
			undo_send_delay = ?, screener_enabled = ?, tracker_blocking = ?,
			zones_enabled = ?, snooze_mark_unread = ?, updated_at = ?
	`, prefs.UserID, prefs.UndoSendDelay, prefs.ScreenerEnabled, prefs.TrackerBlocking,
		prefs.ZonesEnabled, prefs.SnoozeMarkUnread, time.Now(),
		prefs.UndoSendDelay, prefs.ScreenerEnabled, prefs.TrackerBlocking,
		prefs.ZonesEnabled, prefs.SnoozeMarkUnread, time.Now())
	return err
}

// =============================================================================
// Pending Sends (Undo Send) Operations
// =============================================================================

// CreatePendingSend creates a pending send that can be cancelled
func (s *Store) CreatePendingSend(ctx context.Context, pending *PendingSend) error {
	recipientsJSON, _ := json.Marshal(pending.Recipients)
	headersJSON, _ := json.Marshal(pending.Headers)
	pending.CreatedAt = time.Now()

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO pending_sends
		(user_id, cancel_token, from_address, recipients, subject, body, html_body, headers, send_after, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, pending.UserID, pending.CancelToken, pending.FromAddress, string(recipientsJSON),
		pending.Subject, pending.Body, pending.HTMLBody, string(headersJSON),
		pending.SendAfter, pending.CreatedAt)
	if err != nil {
		return err
	}

	pending.ID, _ = result.LastInsertId()
	return nil
}

// CancelPendingSend cancels a pending send by token
func (s *Store) CancelPendingSend(ctx context.Context, userID int64, cancelToken string) error {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM pending_sends WHERE user_id = ? AND cancel_token = ?",
		userID, cancelToken)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetReadyPendingSends gets pending sends that are ready to be sent
func (s *Store) GetReadyPendingSends(ctx context.Context) ([]*PendingSend, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, cancel_token, from_address, recipients, subject, body, html_body, headers
		FROM pending_sends WHERE send_after <= ?
		ORDER BY send_after ASC
		LIMIT 100
	`, time.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pending []*PendingSend
	for rows.Next() {
		p := &PendingSend{}
		var recipientsJSON, headersJSON sql.NullString
		err := rows.Scan(&p.ID, &p.UserID, &p.CancelToken, &p.FromAddress, &recipientsJSON,
			&p.Subject, &p.Body, &p.HTMLBody, &headersJSON)
		if err != nil {
			return nil, err
		}
		if recipientsJSON.Valid {
			if err := json.Unmarshal([]byte(recipientsJSON.String), &p.Recipients); err != nil {
				return nil, fmt.Errorf("pending send %d has malformed recipients: %w", p.ID, err)
			}
		}
		if headersJSON.Valid {
			if err := json.Unmarshal([]byte(headersJSON.String), &p.Headers); err != nil {
				return nil, fmt.Errorf("pending send %d has malformed headers: %w", p.ID, err)
			}
		}
		pending = append(pending, p)
	}
	return pending, rows.Err()
}

// DeletePendingSend removes a pending send after it's been sent
func (s *Store) DeletePendingSend(ctx context.Context, pendingID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM pending_sends WHERE id = ?", pendingID)
	return err
}

// CleanupExpiredPendingSends removes old pending sends (failsafe)
func (s *Store) CleanupExpiredPendingSends(ctx context.Context) error {
	// Delete pending sends older than 5 minutes (they should have been sent by now)
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM pending_sends WHERE created_at < ?",
		time.Now().Add(-5*time.Minute))
	return err
}

// =============================================================================
// Snoozed Emails Operations
// =============================================================================

// SnoozeEmail creates a snooze record for an email
func (s *Store) SnoozeEmail(ctx context.Context, snooze *SnoozedEmail) error {
	snooze.CreatedAt = time.Now()

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO snoozed_emails (user_id, message_id, original_mailbox_id, wake_at, mark_unread, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, snooze.UserID, snooze.MessageID, snooze.OriginalMailboxID, snooze.WakeAt, snooze.MarkUnread, snooze.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrAlreadyExists
		}
		return err
	}

	snooze.ID, _ = result.LastInsertId()
	return nil
}

// GetSnoozedEmail gets a snooze record by ID
func (s *Store) GetSnoozedEmail(ctx context.Context, userID, snoozeID int64) (*SnoozedEmail, error) {
	snz := &SnoozedEmail{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, message_id, original_mailbox_id, wake_at, mark_unread, created_at
		FROM snoozed_emails WHERE id = ? AND user_id = ?
	`, snoozeID, userID).Scan(
		&snz.ID, &snz.UserID, &snz.MessageID, &snz.OriginalMailboxID,
		&snz.WakeAt, &snz.MarkUnread, &snz.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return snz, err
}

// ListSnoozedEmails lists snoozed emails for a user
func (s *Store) ListSnoozedEmails(ctx context.Context, userID int64) ([]*SnoozedEmail, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, message_id, original_mailbox_id, wake_at, mark_unread, created_at
		FROM snoozed_emails WHERE user_id = ? ORDER BY wake_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snoozed []*SnoozedEmail
	for rows.Next() {
		snz := &SnoozedEmail{}
		err := rows.Scan(&snz.ID, &snz.UserID, &snz.MessageID, &snz.OriginalMailboxID,
			&snz.WakeAt, &snz.MarkUnread, &snz.CreatedAt)
		if err != nil {
			return nil, err
		}
		snoozed = append(snoozed, snz)
	}
	return snoozed, rows.Err()
}

// GetReadySnoozedEmails gets snoozed emails that are ready to wake
func (s *Store) GetReadySnoozedEmails(ctx context.Context) ([]*SnoozedEmail, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, message_id, original_mailbox_id, wake_at, mark_unread, created_at
		FROM snoozed_emails WHERE wake_at <= ?
		ORDER BY wake_at ASC
		LIMIT 100
	`, time.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snoozed []*SnoozedEmail
	for rows.Next() {
		snz := &SnoozedEmail{}
		err := rows.Scan(&snz.ID, &snz.UserID, &snz.MessageID, &snz.OriginalMailboxID,
			&snz.WakeAt, &snz.MarkUnread, &snz.CreatedAt)
		if err != nil {
			return nil, err
		}
		snoozed = append(snoozed, snz)
	}
	return snoozed, rows.Err()
}

// DeleteSnoozedEmail removes a snooze record
func (s *Store) DeleteSnoozedEmail(ctx context.Context, userID, snoozeID int64) error {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM snoozed_emails WHERE id = ? AND user_id = ?",
		snoozeID, userID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CancelSnooze cancels a snooze for a message (e.g., if user opens the email)
func (s *Store) CancelSnooze(ctx context.Context, userID, messageID int64) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM snoozed_emails WHERE user_id = ? AND message_id = ?",
		userID, messageID)
	return err
}

// MoveMessageToMailbox moves a message to a different mailbox (for snooze wake-ups)
// This implements the MessageMover interface used by the scheduler
func (s *Store) MoveMessageToMailbox(ctx context.Context, userID, messageID, targetMailboxID int64, markUnread bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Verify the target mailbox belongs to the user
	var mailboxUserID int64
	err = tx.QueryRowContext(ctx,
		"SELECT u.id FROM mailboxes m JOIN users u ON m.user_id = u.id WHERE m.id = ?",
		targetMailboxID).Scan(&mailboxUserID)
	if err != nil {
		return fmt.Errorf("target mailbox not found: %w", err)
	}
	if mailboxUserID != userID {
		return fmt.Errorf("mailbox does not belong to user")
	}

	// Get the next UID for the target mailbox
	var nextUID int64
	err = tx.QueryRowContext(ctx,
		"SELECT COALESCE(MAX(uid), 0) + 1 FROM messages WHERE mailbox_id = ?",
		targetMailboxID).Scan(&nextUID)
	if err != nil {
		return fmt.Errorf("failed to get next UID: %w", err)
	}

	// Get current message flags
	var flags string
	err = tx.QueryRowContext(ctx,
		"SELECT flags FROM messages WHERE id = ?",
		messageID).Scan(&flags)
	if err != nil {
		return fmt.Errorf("message not found: %w", err)
	}

	// Update flags if marking unread (remove \Seen flag)
	if markUnread {
		flagList := strings.Split(flags, ",")
		var newFlags []string
		for _, f := range flagList {
			f = strings.TrimSpace(f)
			if f != "" && f != "\\Seen" {
				newFlags = append(newFlags, f)
			}
		}
		flags = strings.Join(newFlags, ",")
	}

	// Move the message to the target mailbox with new UID
	_, err = tx.ExecContext(ctx,
		"UPDATE messages SET mailbox_id = ?, uid = ?, flags = ? WHERE id = ?",
		targetMailboxID, nextUID, flags, messageID)
	if err != nil {
		return fmt.Errorf("failed to move message: %w", err)
	}

	// Update mailbox UIDNEXT
	_, err = tx.ExecContext(ctx,
		"UPDATE mailboxes SET uid_next = ? WHERE id = ? AND uid_next <= ?",
		nextUID+1, targetMailboxID, nextUID)
	if err != nil {
		return fmt.Errorf("failed to update UIDNEXT: %w", err)
	}

	return tx.Commit()
}
