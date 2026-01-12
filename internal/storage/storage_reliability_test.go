package storage

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestFlag_Constants tests flag constant values.
func TestFlag_Constants(t *testing.T) {
	expectedFlags := map[Flag]string{
		FlagSeen:     `\Seen`,
		FlagAnswered: `\Answered`,
		FlagFlagged:  `\Flagged`,
		FlagDeleted:  `\Deleted`,
		FlagDraft:    `\Draft`,
		FlagRecent:   `\Recent`,
	}

	for flag, expected := range expectedFlags {
		if string(flag) != expected {
			t.Errorf("Flag %v = %q, want %q", flag, string(flag), expected)
		}
	}
}

// TestSpecialUse_Constants tests special use constant values.
func TestSpecialUse_Constants(t *testing.T) {
	expectedSpecialUse := map[SpecialUse]string{
		SpecialUseDrafts:  `\Drafts`,
		SpecialUseSent:    `\Sent`,
		SpecialUseTrash:   `\Trash`,
		SpecialUseJunk:    `\Junk`,
		SpecialUseArchive: `\Archive`,
		SpecialUseAll:     `\All`,
	}

	for su, expected := range expectedSpecialUse {
		if string(su) != expected {
			t.Errorf("SpecialUse %v = %q, want %q", su, string(su), expected)
		}
	}
}

// TestMailbox_ZeroValue tests Mailbox struct zero value.
func TestMailbox_ZeroValue(t *testing.T) {
	var mb Mailbox

	if mb.ID != 0 {
		t.Errorf("ID = %d, want 0", mb.ID)
	}
	if mb.UserID != 0 {
		t.Errorf("UserID = %d, want 0", mb.UserID)
	}
	if mb.Name != "" {
		t.Errorf("Name = %q, want empty", mb.Name)
	}
	if mb.UIDValidity != 0 {
		t.Errorf("UIDValidity = %d, want 0", mb.UIDValidity)
	}
	if mb.UIDNext != 0 {
		t.Errorf("UIDNext = %d, want 0", mb.UIDNext)
	}
	if mb.SpecialUse != "" {
		t.Errorf("SpecialUse = %q, want empty", mb.SpecialUse)
	}
	if mb.Subscribed {
		t.Error("Subscribed = true, want false")
	}
}

// TestMessage_ZeroValue tests Message struct zero value.
func TestMessage_ZeroValue(t *testing.T) {
	var msg Message

	if msg.ID != 0 {
		t.Errorf("ID = %d, want 0", msg.ID)
	}
	if msg.MailboxID != 0 {
		t.Errorf("MailboxID = %d, want 0", msg.MailboxID)
	}
	if msg.UID != 0 {
		t.Errorf("UID = %d, want 0", msg.UID)
	}
	if msg.Size != 0 {
		t.Errorf("Size = %d, want 0", msg.Size)
	}
	if msg.Flags != nil {
		t.Error("Flags should be nil")
	}
	if msg.To != nil {
		t.Error("To should be nil")
	}
}

// TestSearchCriteria_ZeroValue tests SearchCriteria struct zero value.
func TestSearchCriteria_ZeroValue(t *testing.T) {
	var sc SearchCriteria

	if sc.Since != nil {
		t.Error("Since should be nil")
	}
	if sc.Before != nil {
		t.Error("Before should be nil")
	}
	if sc.From != "" {
		t.Errorf("From = %q, want empty", sc.From)
	}
	if sc.To != "" {
		t.Errorf("To = %q, want empty", sc.To)
	}
	if sc.Subject != "" {
		t.Errorf("Subject = %q, want empty", sc.Subject)
	}
	if sc.Larger != 0 {
		t.Errorf("Larger = %d, want 0", sc.Larger)
	}
	if sc.Smaller != 0 {
		t.Errorf("Smaller = %d, want 0", sc.Smaller)
	}
	if sc.Flags != nil {
		t.Error("Flags should be nil")
	}
	if sc.NotFlags != nil {
		t.Error("NotFlags should be nil")
	}
	if sc.Header != nil {
		t.Error("Header should be nil")
	}
}

// TestMailboxStats_ZeroValue tests MailboxStats struct zero value.
func TestMailboxStats_ZeroValue(t *testing.T) {
	var stats MailboxStats

	if stats.Messages != 0 {
		t.Errorf("Messages = %d, want 0", stats.Messages)
	}
	if stats.Recent != 0 {
		t.Errorf("Recent = %d, want 0", stats.Recent)
	}
	if stats.Unseen != 0 {
		t.Errorf("Unseen = %d, want 0", stats.Unseen)
	}
	if stats.UIDNext != 0 {
		t.Errorf("UIDNext = %d, want 0", stats.UIDNext)
	}
	if stats.UIDValidity != 0 {
		t.Errorf("UIDValidity = %d, want 0", stats.UIDValidity)
	}
}

// TestCalendar_ZeroValue tests Calendar struct zero value.
func TestCalendar_ZeroValue(t *testing.T) {
	var cal Calendar

	if cal.ID != 0 {
		t.Errorf("ID = %d, want 0", cal.ID)
	}
	if cal.UserID != 0 {
		t.Errorf("UserID = %d, want 0", cal.UserID)
	}
	if cal.UID != "" {
		t.Errorf("UID = %q, want empty", cal.UID)
	}
	if cal.Name != "" {
		t.Errorf("Name = %q, want empty", cal.Name)
	}
	if cal.IsDefault {
		t.Error("IsDefault = true, want false")
	}
}

// TestCalendarEvent_ZeroValue tests CalendarEvent struct zero value.
func TestCalendarEvent_ZeroValue(t *testing.T) {
	var event CalendarEvent

	if event.ID != 0 {
		t.Errorf("ID = %d, want 0", event.ID)
	}
	if event.CalendarID != 0 {
		t.Errorf("CalendarID = %d, want 0", event.CalendarID)
	}
	if event.AllDay {
		t.Error("AllDay = true, want false")
	}
}

// TestAddressBook_ZeroValue tests AddressBook struct zero value.
func TestAddressBook_ZeroValue(t *testing.T) {
	var ab AddressBook

	if ab.ID != 0 {
		t.Errorf("ID = %d, want 0", ab.ID)
	}
	if ab.UserID != 0 {
		t.Errorf("UserID = %d, want 0", ab.UserID)
	}
	if ab.UID != "" {
		t.Errorf("UID = %q, want empty", ab.UID)
	}
	if ab.IsDefault {
		t.Error("IsDefault = true, want false")
	}
}

// TestContact_ZeroValue tests Contact struct zero value.
func TestContact_ZeroValue(t *testing.T) {
	var contact Contact

	if contact.ID != 0 {
		t.Errorf("ID = %d, want 0", contact.ID)
	}
	if contact.AddressBookID != 0 {
		t.Errorf("AddressBookID = %d, want 0", contact.AddressBookID)
	}
	if contact.Emails != nil {
		t.Error("Emails should be nil")
	}
	if contact.Phones != nil {
		t.Error("Phones should be nil")
	}
}

// TestFlag_TypeConversion tests Flag type conversions.
func TestFlag_TypeConversion(t *testing.T) {
	// String to Flag
	str := `\Seen`
	flag := Flag(str)
	if flag != FlagSeen {
		t.Errorf("Flag(%q) != FlagSeen", str)
	}

	// Flag to string
	if string(FlagSeen) != `\Seen` {
		t.Errorf("string(FlagSeen) = %q", string(FlagSeen))
	}
}

// TestSpecialUse_TypeConversion tests SpecialUse type conversions.
func TestSpecialUse_TypeConversion(t *testing.T) {
	// String to SpecialUse
	str := `\Drafts`
	su := SpecialUse(str)
	if su != SpecialUseDrafts {
		t.Errorf("SpecialUse(%q) != SpecialUseDrafts", str)
	}

	// SpecialUse to string
	if string(SpecialUseDrafts) != `\Drafts` {
		t.Errorf("string(SpecialUseDrafts) = %q", string(SpecialUseDrafts))
	}
}

// TestMailbox_FieldAssignment tests mailbox field assignments.
func TestMailbox_FieldAssignment(t *testing.T) {
	now := time.Now()
	mb := Mailbox{
		ID:          123,
		UserID:      456,
		Name:        "INBOX",
		UIDValidity: 789,
		UIDNext:     100,
		SpecialUse:  "",
		Subscribed:  true,
		CreatedAt:   now,
	}

	if mb.ID != 123 {
		t.Errorf("ID = %d", mb.ID)
	}
	if mb.UserID != 456 {
		t.Errorf("UserID = %d", mb.UserID)
	}
	if mb.Name != "INBOX" {
		t.Errorf("Name = %q", mb.Name)
	}
	if mb.UIDValidity != 789 {
		t.Errorf("UIDValidity = %d", mb.UIDValidity)
	}
	if mb.UIDNext != 100 {
		t.Errorf("UIDNext = %d", mb.UIDNext)
	}
	if !mb.Subscribed {
		t.Error("Subscribed should be true")
	}
}

// TestMessage_FieldAssignment tests message field assignments.
func TestMessage_FieldAssignment(t *testing.T) {
	now := time.Now()
	msg := Message{
		ID:           1,
		MailboxID:    2,
		UID:          3,
		MaildirKey:   "key123",
		Size:         1024,
		InternalDate: now,
		Flags:        []Flag{FlagSeen, FlagFlagged},
		MessageID:    "<msg@example.com>",
		Subject:      "Test Subject",
		From:         "sender@example.com",
		To:           []string{"recipient@example.com"},
		InReplyTo:    "<parent@example.com>",
		References:   "<ref1@example.com> <ref2@example.com>",
		CreatedAt:    now,
	}

	if msg.ID != 1 {
		t.Errorf("ID = %d", msg.ID)
	}
	if msg.MailboxID != 2 {
		t.Errorf("MailboxID = %d", msg.MailboxID)
	}
	if msg.UID != 3 {
		t.Errorf("UID = %d", msg.UID)
	}
	if msg.MaildirKey != "key123" {
		t.Errorf("MaildirKey = %q", msg.MaildirKey)
	}
	if msg.Size != 1024 {
		t.Errorf("Size = %d", msg.Size)
	}
	if len(msg.Flags) != 2 {
		t.Errorf("Flags count = %d", len(msg.Flags))
	}
	if len(msg.To) != 1 {
		t.Errorf("To count = %d", len(msg.To))
	}
}

// TestSearchCriteria_FieldAssignment tests search criteria field assignments.
func TestSearchCriteria_FieldAssignment(t *testing.T) {
	now := time.Now()
	before := now.Add(-24 * time.Hour)

	sc := SearchCriteria{
		Since:    &now,
		Before:   &before,
		From:     "sender@example.com",
		To:       "recipient@example.com",
		Subject:  "Test",
		Body:     "content",
		Flags:    []Flag{FlagSeen},
		NotFlags: []Flag{FlagDeleted},
		Larger:   1000,
		Smaller:  100000,
		Header:   map[string]string{"X-Custom": "value"},
	}

	if sc.Since == nil {
		t.Error("Since should not be nil")
	}
	if sc.Before == nil {
		t.Error("Before should not be nil")
	}
	if sc.From != "sender@example.com" {
		t.Errorf("From = %q", sc.From)
	}
	if len(sc.Flags) != 1 {
		t.Errorf("Flags count = %d", len(sc.Flags))
	}
	if len(sc.NotFlags) != 1 {
		t.Errorf("NotFlags count = %d", len(sc.NotFlags))
	}
	if len(sc.Header) != 1 {
		t.Errorf("Header count = %d", len(sc.Header))
	}
}

// TestFlag_Comparison tests flag comparisons.
func TestFlag_Comparison(t *testing.T) {
	flags := []Flag{FlagSeen, FlagAnswered, FlagFlagged, FlagDeleted, FlagDraft, FlagRecent}

	// All flags should be unique
	seen := make(map[Flag]bool)
	for _, f := range flags {
		if seen[f] {
			t.Errorf("Duplicate flag: %s", f)
		}
		seen[f] = true
	}

	// Test contains check pattern
	testFlags := []Flag{FlagSeen, FlagFlagged}
	for _, f := range flags {
		contains := false
		for _, tf := range testFlags {
			if f == tf {
				contains = true
				break
			}
		}
		if f == FlagSeen || f == FlagFlagged {
			if !contains {
				t.Errorf("Flag %s should be in testFlags", f)
			}
		} else {
			if contains {
				t.Errorf("Flag %s should not be in testFlags", f)
			}
		}
	}
}

// TestFlag_StringBuilding tests building flag strings.
func TestFlag_StringBuilding(t *testing.T) {
	flags := []Flag{FlagSeen, FlagFlagged, FlagDeleted}

	var builder strings.Builder
	for i, f := range flags {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(string(f))
	}

	result := builder.String()
	if !strings.Contains(result, string(FlagSeen)) {
		t.Error("Result should contain \\Seen")
	}
	if !strings.Contains(result, string(FlagFlagged)) {
		t.Error("Result should contain \\Flagged")
	}
	if !strings.Contains(result, string(FlagDeleted)) {
		t.Error("Result should contain \\Deleted")
	}
}

// TestMailboxStats_Values tests mailbox stats value handling.
func TestMailboxStats_Values(t *testing.T) {
	stats := MailboxStats{
		Messages:    100,
		Recent:      10,
		Unseen:      20,
		UIDNext:     500,
		UIDValidity: 123456,
	}

	// Unseen should be <= Messages
	if stats.Unseen > stats.Messages {
		t.Error("Unseen should be <= Messages")
	}

	// Recent should be <= Messages
	if stats.Recent > stats.Messages {
		t.Error("Recent should be <= Messages")
	}
}

// TestConcurrentMailboxAccess tests concurrent access patterns.
func TestConcurrentMailboxAccess(t *testing.T) {
	var wg sync.WaitGroup
	var panicked int32

	// Simulate concurrent read/write patterns
	mb := &Mailbox{
		ID:          1,
		UserID:      1,
		Name:        "INBOX",
		UIDValidity: 123,
		UIDNext:     1,
		Subscribed:  true,
	}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt32(&panicked, 1)
				}
			}()

			// Read operations
			_ = mb.Name
			_ = mb.UIDNext
			_ = mb.Subscribed
		}(i)
	}

	wg.Wait()

	if atomic.LoadInt32(&panicked) > 0 {
		t.Errorf("Concurrent access caused %d panic(s)", panicked)
	}
}

// TestConcurrentMessageAccess tests concurrent message access patterns.
func TestConcurrentMessageAccess(t *testing.T) {
	var wg sync.WaitGroup
	var panicked int32

	msg := &Message{
		ID:        1,
		MailboxID: 1,
		UID:       100,
		Size:      1024,
		Flags:     []Flag{FlagSeen},
		To:        []string{"recipient@example.com"},
	}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt32(&panicked, 1)
				}
			}()

			// Read operations
			_ = msg.UID
			_ = msg.Size
			_ = len(msg.Flags)
			_ = len(msg.To)
		}(i)
	}

	wg.Wait()

	if atomic.LoadInt32(&panicked) > 0 {
		t.Errorf("Concurrent access caused %d panic(s)", panicked)
	}
}

// TestSearchCriteria_Empty tests empty search criteria.
func TestSearchCriteria_Empty(t *testing.T) {
	sc := SearchCriteria{}

	// Empty criteria should match any message
	if sc.Since != nil || sc.Before != nil {
		t.Error("Empty criteria should have nil time bounds")
	}
	if sc.From != "" || sc.To != "" || sc.Subject != "" || sc.Body != "" {
		t.Error("Empty criteria should have empty string fields")
	}
	if len(sc.Flags) != 0 || len(sc.NotFlags) != 0 {
		t.Error("Empty criteria should have no flags")
	}
	if sc.Larger != 0 || sc.Smaller != 0 {
		t.Error("Empty criteria should have zero size bounds")
	}
}

// TestMessage_FlagsSliceSafety tests flag slice operations.
func TestMessage_FlagsSliceSafety(t *testing.T) {
	msg := Message{
		Flags: []Flag{FlagSeen, FlagFlagged},
	}

	// Safe copy pattern
	flags := make([]Flag, len(msg.Flags))
	copy(flags, msg.Flags)

	if len(flags) != 2 {
		t.Errorf("Copied flags count = %d", len(flags))
	}

	// Modify copy shouldn't affect original
	flags[0] = FlagDeleted
	if msg.Flags[0] != FlagSeen {
		t.Error("Original flags should not be modified")
	}
}

// TestMailbox_NameValidation tests mailbox name handling.
func TestMailbox_NameValidation(t *testing.T) {
	validNames := []string{
		"INBOX",
		"Sent",
		"Drafts",
		"Trash",
		"Archive",
		"Custom Folder",
		"Folder/Subfolder",
		"日本語",
		"émoji 📧",
	}

	for _, name := range validNames {
		mb := Mailbox{Name: name}
		if mb.Name != name {
			t.Errorf("Name %q was changed to %q", name, mb.Name)
		}
	}
}

// TestMessage_SizeValidation tests message size handling.
func TestMessage_SizeValidation(t *testing.T) {
	testCases := []struct {
		size  int64
		valid bool
	}{
		{0, true},     // Empty message
		{1, true},     // Tiny message
		{1024, true},  // 1 KB
		{1048576, true}, // 1 MB
		{104857600, true}, // 100 MB
		{-1, false},   // Negative size is invalid
	}

	for _, tc := range testCases {
		msg := Message{Size: tc.size}
		isValid := msg.Size >= 0
		if isValid != tc.valid {
			t.Errorf("Size %d: valid = %v, want %v", tc.size, isValid, tc.valid)
		}
	}
}

// TestCalendarEvent_TimeRange tests calendar event time handling.
func TestCalendarEvent_TimeRange(t *testing.T) {
	now := time.Now()
	event := CalendarEvent{
		StartTime: now,
		EndTime:   now.Add(time.Hour),
		AllDay:    false,
	}

	// Duration should be positive
	duration := event.EndTime.Sub(event.StartTime)
	if duration <= 0 {
		t.Error("Event duration should be positive")
	}

	// All-day event
	allDayEvent := CalendarEvent{
		StartTime: now.Truncate(24 * time.Hour),
		EndTime:   now.Truncate(24 * time.Hour).Add(24 * time.Hour),
		AllDay:    true,
	}

	if !allDayEvent.AllDay {
		t.Error("AllDay should be true")
	}
}

// TestContact_EmailList tests contact email list handling.
func TestContact_EmailList(t *testing.T) {
	contact := Contact{
		Emails: []string{
			"primary@example.com",
			"secondary@example.com",
			"work@company.com",
		},
	}

	if len(contact.Emails) != 3 {
		t.Errorf("Emails count = %d, want 3", len(contact.Emails))
	}

	// Check for valid email format (basic)
	for _, email := range contact.Emails {
		if !strings.Contains(email, "@") {
			t.Errorf("Invalid email format: %s", email)
		}
	}
}
