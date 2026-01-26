package storage

import (
	"context"
	"io"
	"strings"
	"time"
)

// Flag represents an IMAP message flag
type Flag string

const (
	FlagSeen     Flag = `\Seen`
	FlagAnswered Flag = `\Answered`
	FlagFlagged  Flag = `\Flagged`
	FlagDeleted  Flag = `\Deleted`
	FlagDraft    Flag = `\Draft`
	FlagRecent   Flag = `\Recent`
)

// Common flag slices pre-allocated to avoid repeated allocations
var (
	// CommonFlags contains the standard IMAP flags
	CommonFlags = []Flag{FlagSeen, FlagAnswered, FlagFlagged, FlagDeleted, FlagDraft}

	// EmptyFlags is an empty flag slice that can be reused
	EmptyFlags = []Flag{}
)

// FlagsContain checks if a flag slice contains a specific flag.
// More efficient than creating a map for small flag sets.
func FlagsContain(flags []Flag, target Flag) bool {
	for _, f := range flags {
		if f == target {
			return true
		}
	}
	return false
}

// CopyFlags creates a copy of a flag slice with pre-allocated capacity.
func CopyFlags(flags []Flag) []Flag {
	if len(flags) == 0 {
		return EmptyFlags
	}
	result := make([]Flag, len(flags))
	copy(result, flags)
	return result
}

// AddFlag adds a flag to a slice if not already present.
// Returns the modified slice (may be a new allocation).
func AddFlag(flags []Flag, flag Flag) []Flag {
	if FlagsContain(flags, flag) {
		return flags
	}
	return append(flags, flag)
}

// RemoveFlag removes a flag from a slice.
// Returns the modified slice.
func RemoveFlag(flags []Flag, flag Flag) []Flag {
	for i, f := range flags {
		if f == flag {
			// Remove by swapping with last and truncating
			flags[i] = flags[len(flags)-1]
			return flags[:len(flags)-1]
		}
	}
	return flags
}

// FlagsToString converts flags to a space-separated string.
// Uses strings.Builder for efficient concatenation.
func FlagsToString(flags []Flag) string {
	if len(flags) == 0 {
		return ""
	}
	if len(flags) == 1 {
		return string(flags[0])
	}

	var b strings.Builder
	b.Grow(len(flags) * 10) // Estimate ~10 chars per flag
	for i, f := range flags {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(string(f))
	}
	return b.String()
}

// SpecialUse represents IMAP special-use mailbox attributes
type SpecialUse string

const (
	SpecialUseDrafts  SpecialUse = `\Drafts`
	SpecialUseSent    SpecialUse = `\Sent`
	SpecialUseTrash   SpecialUse = `\Trash`
	SpecialUseJunk    SpecialUse = `\Junk`
	SpecialUseArchive SpecialUse = `\Archive`
	SpecialUseAll     SpecialUse = `\All`
)

// Mailbox represents an IMAP mailbox/folder
type Mailbox struct {
	ID          int64
	UserID      int64
	Name        string
	UIDValidity uint32
	UIDNext     uint32
	SpecialUse  SpecialUse
	Subscribed  bool
	CreatedAt   time.Time
}

// Message represents email message metadata
type Message struct {
	ID           int64
	MailboxID    int64
	UID          uint32
	MaildirKey   string
	Size         int64
	InternalDate time.Time
	Flags        []Flag
	MessageID    string
	Subject      string
	From         string
	To           []string
	InReplyTo    string
	References   string
	CreatedAt    time.Time
}

// MessageStore handles email message storage operations
type MessageStore interface {
	// Mailbox operations
	CreateMailbox(ctx context.Context, userID int64, name string, specialUse SpecialUse) (*Mailbox, error)
	GetMailbox(ctx context.Context, userID int64, name string) (*Mailbox, error)
	GetMailboxByID(ctx context.Context, id int64) (*Mailbox, error)
	ListMailboxes(ctx context.Context, userID int64) ([]*Mailbox, error)
	RenameMailbox(ctx context.Context, userID int64, oldName, newName string) error
	DeleteMailbox(ctx context.Context, userID int64, name string) error
	SubscribeMailbox(ctx context.Context, userID int64, name string, subscribed bool) error
	InitializeUserMailboxes(ctx context.Context, userID int64) error
	EnsureDefaultMailboxes(ctx context.Context, userID int64) error

	// Message operations
	AppendMessage(ctx context.Context, mailboxID int64, flags []Flag, date time.Time, body io.Reader) (*Message, error)
	GetMessage(ctx context.Context, mailboxID int64, uid uint32) (*Message, error)
	GetMessageBody(ctx context.Context, msg *Message) (io.ReadCloser, error)
	ListMessages(ctx context.Context, mailboxID int64, start, end uint32) ([]*Message, error)
	UpdateFlags(ctx context.Context, mailboxID int64, uid uint32, flags []Flag, add bool) error
	SetFlags(ctx context.Context, mailboxID int64, uid uint32, flags []Flag) error
	DeleteMessage(ctx context.Context, mailboxID int64, uid uint32) error
	CopyMessage(ctx context.Context, srcMailboxID int64, uid uint32, destMailboxID int64) (*Message, error)
	MoveMessage(ctx context.Context, srcMailboxID int64, uid uint32, destMailboxID int64) (*Message, error)
	ExpungeMailbox(ctx context.Context, mailboxID int64) ([]uint32, error)

	// Zone operations
	UpdateMessageZone(ctx context.Context, messageID int64, zone string) error

	// Search operations
	SearchMessages(ctx context.Context, mailboxID int64, criteria *SearchCriteria) ([]uint32, error)

	// Stats
	GetMailboxStats(ctx context.Context, mailboxID int64) (*MailboxStats, error)
	UpdateUserQuota(ctx context.Context, userID int64, deltaBytes int64) error
}

// SearchCriteria defines email search parameters
type SearchCriteria struct {
	Since    *time.Time
	Before   *time.Time
	From     string
	To       string
	Subject  string
	Body     string
	Flags    []Flag
	NotFlags []Flag
	Larger   int64
	Smaller  int64
	Header   map[string]string
}

// MailboxStats contains mailbox statistics
type MailboxStats struct {
	Messages    int
	Recent      int
	Unseen      int
	UIDNext     uint32
	UIDValidity uint32
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

// CalendarEvent represents a calendar event
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

// CalendarStore handles calendar storage operations
type CalendarStore interface {
	// Calendar operations
	CreateCalendar(ctx context.Context, userID int64, cal *Calendar) error
	GetCalendar(ctx context.Context, uid string) (*Calendar, error)
	GetCalendarByID(ctx context.Context, id int64) (*Calendar, error)
	ListCalendars(ctx context.Context, userID int64) ([]*Calendar, error)
	UpdateCalendar(ctx context.Context, cal *Calendar) error
	DeleteCalendar(ctx context.Context, uid string) error

	// Event operations
	CreateEvent(ctx context.Context, calendarUID string, event *CalendarEvent) error
	GetEvent(ctx context.Context, calendarUID, eventUID string) (*CalendarEvent, error)
	ListEvents(ctx context.Context, calendarUID string, start, end *time.Time) ([]*CalendarEvent, error)
	UpdateEvent(ctx context.Context, event *CalendarEvent) error
	DeleteEvent(ctx context.Context, calendarUID, eventUID string) error
}

// AddressBook represents a CardDAV address book
type AddressBook struct {
	ID          int64
	UserID      int64
	UID         string
	Name        string
	Description string
	CTag        string
	IsDefault   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Contact represents a contact/vCard
type Contact struct {
	ID            int64
	AddressBookID int64
	UID           string
	ETag          string
	VCardData     string
	FullName      string
	GivenName     string
	FamilyName    string
	Nickname      string
	Emails        []string
	Phones        []string
	Organization  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ContactStore handles contact storage operations
type ContactStore interface {
	// Address book operations
	CreateAddressBook(ctx context.Context, userID int64, ab *AddressBook) error
	GetAddressBook(ctx context.Context, uid string) (*AddressBook, error)
	GetAddressBookByID(ctx context.Context, id int64) (*AddressBook, error)
	ListAddressBooks(ctx context.Context, userID int64) ([]*AddressBook, error)
	UpdateAddressBook(ctx context.Context, ab *AddressBook) error
	DeleteAddressBook(ctx context.Context, uid string) error

	// Contact operations
	CreateContact(ctx context.Context, addressBookUID string, contact *Contact) error
	GetContact(ctx context.Context, addressBookUID, contactUID string) (*Contact, error)
	ListContacts(ctx context.Context, addressBookUID string) ([]*Contact, error)
	UpdateContact(ctx context.Context, contact *Contact) error
	DeleteContact(ctx context.Context, addressBookUID, contactUID string) error
}
