package storage

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// BenchmarkFlag_Comparison benchmarks flag comparison.
func BenchmarkFlag_Comparison(b *testing.B) {
	flag := FlagSeen
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = flag == FlagSeen
		_ = flag == FlagAnswered
		_ = flag == FlagFlagged
		_ = flag == FlagDeleted
	}
}

// BenchmarkFlag_StringConversion benchmarks flag to string conversion.
func BenchmarkFlag_StringConversion(b *testing.B) {
	flags := []Flag{FlagSeen, FlagAnswered, FlagFlagged, FlagDeleted, FlagDraft}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, f := range flags {
			_ = string(f)
		}
	}
}

// BenchmarkFlag_MapLookup benchmarks flag map lookup.
func BenchmarkFlag_MapLookup(b *testing.B) {
	flagSet := map[Flag]bool{
		FlagSeen:     true,
		FlagAnswered: true,
		FlagFlagged:  true,
	}
	flags := []Flag{FlagSeen, FlagDeleted, FlagFlagged, FlagDraft}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, f := range flags {
			_ = flagSet[f]
		}
	}
}

// BenchmarkFlag_SliceContains benchmarks flag slice contains check.
func BenchmarkFlag_SliceContains(b *testing.B) {
	flags := []Flag{FlagSeen, FlagAnswered, FlagFlagged}
	target := FlagFlagged
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		found := false
		for _, f := range flags {
			if f == target {
				found = true
				break
			}
		}
		_ = found
	}
}

// BenchmarkMessage_FieldAccess benchmarks message field access.
func BenchmarkMessage_FieldAccess(b *testing.B) {
	msg := Message{
		ID:        1,
		MailboxID: 2,
		UID:       100,
		Size:      1024,
		Flags:     []Flag{FlagSeen, FlagFlagged},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = msg.ID
		_ = msg.MailboxID
		_ = msg.UID
		_ = msg.Size
		_ = len(msg.Flags)
	}
}

// BenchmarkMessage_Creation benchmarks message struct creation.
func BenchmarkMessage_Creation(b *testing.B) {
	now := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Message{
			ID:           int64(i),
			MailboxID:    1,
			UID:          uint32(i),
			MaildirKey:   "test-key",
			Size:         1024,
			InternalDate: now,
			Flags:        []Flag{FlagSeen},
			MessageID:    "<test@example.com>",
			Subject:      "Test Subject",
			From:         "sender@example.com",
			To:           []string{"recipient@example.com"},
			CreatedAt:    now,
		}
	}
}

// BenchmarkMailbox_FieldAccess benchmarks mailbox field access.
func BenchmarkMailbox_FieldAccess(b *testing.B) {
	mb := Mailbox{
		ID:          1,
		UserID:      100,
		Name:        "INBOX",
		UIDValidity: 123456,
		UIDNext:     500,
		Subscribed:  true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mb.ID
		_ = mb.UserID
		_ = mb.Name
		_ = mb.UIDValidity
		_ = mb.UIDNext
		_ = mb.Subscribed
	}
}

// BenchmarkMailboxStats_FieldAccess benchmarks stats field access.
func BenchmarkMailboxStats_FieldAccess(b *testing.B) {
	stats := MailboxStats{
		Messages:    100,
		Recent:      10,
		Unseen:      20,
		UIDNext:     500,
		UIDValidity: 123456,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = stats.Messages
		_ = stats.Recent
		_ = stats.Unseen
		_ = stats.UIDNext
		_ = stats.UIDValidity
	}
}

// BenchmarkSearchCriteria_Creation benchmarks search criteria creation.
func BenchmarkSearchCriteria_Creation(b *testing.B) {
	now := time.Now()
	before := now.Add(-24 * time.Hour)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SearchCriteria{
			Since:    &now,
			Before:   &before,
			From:     "sender@example.com",
			Subject:  "Test",
			Flags:    []Flag{FlagSeen},
			NotFlags: []Flag{FlagDeleted},
			Larger:   1000,
			Smaller:  100000,
		}
	}
}

// BenchmarkFlag_SliceCopy benchmarks flag slice copy.
func BenchmarkFlag_SliceCopy_5(b *testing.B) {
	original := []Flag{FlagSeen, FlagAnswered, FlagFlagged, FlagDeleted, FlagDraft}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		flags := make([]Flag, len(original))
		copy(flags, original)
		_ = flags
	}
}

// BenchmarkFlag_SliceCopy_1 benchmarks single flag copy.
func BenchmarkFlag_SliceCopy_1(b *testing.B) {
	original := []Flag{FlagSeen}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		flags := make([]Flag, len(original))
		copy(flags, original)
		_ = flags
	}
}

// BenchmarkFlag_SliceAppend benchmarks flag slice append.
func BenchmarkFlag_SliceAppend(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var flags []Flag
		flags = append(flags, FlagSeen)
		flags = append(flags, FlagFlagged)
		flags = append(flags, FlagAnswered)
		_ = flags
	}
}

// BenchmarkFlag_SliceAppend_Preallocated benchmarks preallocated append.
func BenchmarkFlag_SliceAppend_Preallocated(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		flags := make([]Flag, 0, 5)
		flags = append(flags, FlagSeen)
		flags = append(flags, FlagFlagged)
		flags = append(flags, FlagAnswered)
		_ = flags
	}
}

// BenchmarkMessage_ToSlice benchmarks To slice operations.
func BenchmarkMessage_ToSlice_1(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Message{
			To: []string{"recipient@example.com"},
		}
	}
}

// BenchmarkMessage_ToSlice_10 benchmarks larger To slice.
func BenchmarkMessage_ToSlice_10(b *testing.B) {
	recipients := make([]string, 10)
	for i := 0; i < 10; i++ {
		recipients[i] = "recipient@example.com"
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Message{
			To: recipients,
		}
	}
}

// BenchmarkSpecialUse_Comparison benchmarks special use comparison.
func BenchmarkSpecialUse_Comparison(b *testing.B) {
	su := SpecialUseDrafts
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = su == SpecialUseDrafts
		_ = su == SpecialUseSent
		_ = su == SpecialUseTrash
		_ = su == SpecialUseJunk
	}
}

// BenchmarkSpecialUse_StringConversion benchmarks special use to string.
func BenchmarkSpecialUse_StringConversion(b *testing.B) {
	specialUses := []SpecialUse{SpecialUseDrafts, SpecialUseSent, SpecialUseTrash, SpecialUseJunk}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, su := range specialUses {
			_ = string(su)
		}
	}
}

// BenchmarkMailbox_ConcurrentRead benchmarks concurrent mailbox reads.
func BenchmarkMailbox_ConcurrentRead(b *testing.B) {
	mb := &Mailbox{
		ID:          1,
		UserID:      100,
		Name:        "INBOX",
		UIDValidity: 123456,
		UIDNext:     500,
		Subscribed:  true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = mb.ID
			_ = mb.Name
			_ = mb.UIDNext
		}
	})
}

// BenchmarkMessage_ConcurrentRead benchmarks concurrent message reads.
func BenchmarkMessage_ConcurrentRead(b *testing.B) {
	msg := &Message{
		ID:        1,
		MailboxID: 1,
		UID:       100,
		Size:      1024,
		Flags:     []Flag{FlagSeen},
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = msg.ID
			_ = msg.UID
			_ = msg.Size
		}
	})
}

// BenchmarkFlag_StringBuilder benchmarks building flag string.
func BenchmarkFlag_StringBuilder(b *testing.B) {
	flags := []Flag{FlagSeen, FlagAnswered, FlagFlagged, FlagDeleted, FlagDraft}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var builder strings.Builder
		for j, f := range flags {
			if j > 0 {
				builder.WriteString(",")
			}
			builder.WriteString(string(f))
		}
		_ = builder.String()
	}
}

// BenchmarkFlag_JoinString benchmarks joining flags with strings.Join.
func BenchmarkFlag_JoinString(b *testing.B) {
	flags := []Flag{FlagSeen, FlagAnswered, FlagFlagged, FlagDeleted, FlagDraft}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		strs := make([]string, len(flags))
		for j, f := range flags {
			strs[j] = string(f)
		}
		_ = strings.Join(strs, ",")
	}
}

// BenchmarkCalendar_FieldAccess benchmarks calendar field access.
func BenchmarkCalendar_FieldAccess(b *testing.B) {
	cal := Calendar{
		ID:          1,
		UserID:      100,
		UID:         "cal-uid",
		Name:        "Personal",
		Description: "My personal calendar",
		Color:       "#FF0000",
		IsDefault:   true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cal.ID
		_ = cal.UserID
		_ = cal.Name
		_ = cal.IsDefault
	}
}

// BenchmarkContact_FieldAccess benchmarks contact field access.
func BenchmarkContact_FieldAccess(b *testing.B) {
	contact := Contact{
		ID:         1,
		FullName:   "John Doe",
		GivenName:  "John",
		FamilyName: "Doe",
		Emails:     []string{"john@example.com"},
		Phones:     []string{"+1234567890"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = contact.ID
		_ = contact.FullName
		_ = len(contact.Emails)
		_ = len(contact.Phones)
	}
}

// BenchmarkCalendarEvent_FieldAccess benchmarks event field access.
func BenchmarkCalendarEvent_FieldAccess(b *testing.B) {
	now := time.Now()
	event := CalendarEvent{
		ID:        1,
		UID:       "event-uid",
		Summary:   "Meeting",
		StartTime: now,
		EndTime:   now.Add(time.Hour),
		AllDay:    false,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = event.ID
		_ = event.Summary
		_ = event.StartTime
		_ = event.EndTime
		_ = event.AllDay
	}
}

// BenchmarkAddressBook_FieldAccess benchmarks address book field access.
func BenchmarkAddressBook_FieldAccess(b *testing.B) {
	ab := AddressBook{
		ID:          1,
		UserID:      100,
		UID:         "ab-uid",
		Name:        "Personal",
		Description: "My contacts",
		IsDefault:   true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ab.ID
		_ = ab.UserID
		_ = ab.Name
		_ = ab.IsDefault
	}
}

// BenchmarkMutex_RLock benchmarks RWMutex read lock.
func BenchmarkMutex_RLock(b *testing.B) {
	var mu sync.RWMutex
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mu.RLock()
		mu.RUnlock()
	}
}

// BenchmarkMutex_RLock_Parallel benchmarks parallel read locks.
func BenchmarkMutex_RLock_Parallel(b *testing.B) {
	var mu sync.RWMutex
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mu.RLock()
			mu.RUnlock()
		}
	})
}

// BenchmarkMutex_Lock benchmarks RWMutex write lock.
func BenchmarkMutex_Lock(b *testing.B) {
	var mu sync.RWMutex
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mu.Lock()
		mu.Unlock()
	}
}

// BenchmarkTime_Now benchmarks time.Now() calls.
func BenchmarkTime_Now(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		time.Now()
	}
}

// BenchmarkTime_Format benchmarks time formatting.
func BenchmarkTime_Format(b *testing.B) {
	t := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = t.Format(time.RFC3339)
	}
}

// BenchmarkTime_Parse benchmarks time parsing.
func BenchmarkTime_Parse(b *testing.B) {
	ts := "2024-01-01T12:00:00Z"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		time.Parse(time.RFC3339, ts)
	}
}
