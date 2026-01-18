// Package features implements unique email features like Screener, Aliases, Send Later, etc.
package features

import (
	"time"
)

// ScreenerStatus represents the status of a contact in the screener
type ScreenerStatus string

const (
	ScreenerPending  ScreenerStatus = "pending"
	ScreenerApproved ScreenerStatus = "approved"
	ScreenerBlocked  ScreenerStatus = "blocked"
)

// ScreenerContact represents a sender in the screener system
type ScreenerContact struct {
	ID        int64          `json:"id"`
	UserID    int64          `json:"user_id"`
	Email     string         `json:"email,omitempty"`     // Specific email address
	Domain    string         `json:"domain,omitempty"`    // Or entire domain
	Status    ScreenerStatus `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// EmailAlias represents a disposable/masked email address
type EmailAlias struct {
	ID           int64     `json:"id"`
	UserID       int64     `json:"user_id"`
	DomainID     int64     `json:"domain_id"`
	AliasAddress string    `json:"alias_address"` // Full address: alias@domain.com
	AliasLocal   string    `json:"alias_local"`   // Just the local part
	Description  string    `json:"description,omitempty"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	LastUsedAt   time.Time `json:"last_used_at,omitempty"`
	EmailCount   int64     `json:"email_count"`
}

// ScheduledEmail represents an email scheduled to be sent later
type ScheduledEmail struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	SendAt      time.Time `json:"send_at"`
	FromAddress string    `json:"from_address"`
	Recipients  []string  `json:"recipients"`
	Subject     string    `json:"subject"`
	Body        string    `json:"body,omitempty"`
	HTMLBody    string    `json:"html_body,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Status      string    `json:"status"` // pending, sending, sent, cancelled, failed
	CreatedAt   time.Time `json:"created_at"`
	SentAt      time.Time `json:"sent_at,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// ScheduledEmailStatus constants
const (
	ScheduledStatusPending   = "pending"
	ScheduledStatusSending   = "sending"
	ScheduledStatusSent      = "sent"
	ScheduledStatusCancelled = "cancelled"
	ScheduledStatusFailed    = "failed"
)

// SnoozedEmail represents an email that's been snoozed to reappear later
type SnoozedEmail struct {
	ID                int64     `json:"id"`
	UserID            int64     `json:"user_id"`
	MessageID         int64     `json:"message_id"`
	OriginalMailboxID int64     `json:"original_mailbox_id"`
	WakeAt            time.Time `json:"wake_at"`
	MarkUnread        bool      `json:"mark_unread"`
	CreatedAt         time.Time `json:"created_at"`
}

// VIPContact represents a priority sender
type VIPContact struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// UserPreferences stores feature settings for a user
type UserPreferences struct {
	UserID          int64  `json:"user_id"`
	UndoSendDelay   int    `json:"undo_send_delay"`   // seconds: 0, 5, 10, 20, 30
	ScreenerEnabled bool   `json:"screener_enabled"`
	TrackerBlocking string `json:"tracker_blocking"`  // block, proxy, off
	ZonesEnabled    bool   `json:"zones_enabled"`
	SnoozeMarkUnread bool  `json:"snooze_mark_unread"`
}

// DefaultUserPreferences returns default settings
func DefaultUserPreferences(userID int64) *UserPreferences {
	return &UserPreferences{
		UserID:          userID,
		UndoSendDelay:   10,
		ScreenerEnabled: true,
		TrackerBlocking: "block",
		ZonesEnabled:    true,
		SnoozeMarkUnread: true,
	}
}

// PendingSend represents an email in the undo-send delay period
type PendingSend struct {
	ID          int64             `json:"id"`
	UserID      int64             `json:"user_id"`
	CancelToken string            `json:"cancel_token"`
	FromAddress string            `json:"from_address"`
	Recipients  []string          `json:"recipients"`
	Subject     string            `json:"subject"`
	Body        string            `json:"body,omitempty"`
	HTMLBody    string            `json:"html_body,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	SendAfter   time.Time         `json:"send_after"`
	CreatedAt   time.Time         `json:"created_at"`
}

// MessageZone represents the smart inbox zone for a message
type MessageZone string

const (
	ZoneInbox      MessageZone = "inbox"      // Default/uncategorized
	ZonePriority   MessageZone = "priority"   // From contacts/VIPs
	ZoneFeed       MessageZone = "feed"       // Newsletters, marketing
	ZonePaperTrail MessageZone = "paper_trail" // Receipts, confirmations
	ZoneScreener   MessageZone = "screener"   // Unknown senders (pending approval)
)

// TrackerInfo contains information about blocked trackers in a message
type TrackerInfo struct {
	BlockedCount int      `json:"blocked_count"`
	Domains      []string `json:"domains,omitempty"`
}
