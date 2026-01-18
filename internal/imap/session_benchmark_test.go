package imap

import (
	"sync"
	"testing"

	"github.com/fenilsonani/email-server/internal/storage"
)

// BenchmarkSession_Close benchmarks session close operation.
func BenchmarkSession_Close(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		session := &Session{
			updates: make(chan any, 100),
			closed:  false,
		}
		session.Close()
	}
}

// BenchmarkSession_Unselect benchmarks unselect operation.
func BenchmarkSession_Unselect(b *testing.B) {
	session := &Session{
		updates: make(chan any, 100),
		closed:  false,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.Unselect()
	}
}

// BenchmarkSession_MutexContention benchmarks mutex contention patterns.
func BenchmarkSession_MutexContention(b *testing.B) {
	session := &Session{
		updates: make(chan any, 100),
		closed:  false,
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			session.mu.RLock()
			_ = session.user
			_ = session.selected
			session.mu.RUnlock()
		}
	})
}

// BenchmarkMatchMailboxPattern benchmarks mailbox pattern matching.
func BenchmarkMatchMailboxPattern(b *testing.B) {
	patterns := []struct {
		name    string
		pattern string
	}{
		{"INBOX", "*"},
		{"INBOX", "%"},
		{"Sent/Subfolder", "Sent*"},
		{"Archive/2024/January", "*"},
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, p := range patterns {
			matchMailboxPattern(p.name, p.pattern)
		}
	}
}

// BenchmarkMatchMailboxPattern_Wildcard benchmarks wildcard pattern matching.
func BenchmarkMatchMailboxPattern_Wildcard(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matchMailboxPattern("INBOX", "*")
	}
}

// BenchmarkMatchMailboxPattern_Percent benchmarks percent pattern matching.
func BenchmarkMatchMailboxPattern_Percent(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matchMailboxPattern("INBOX", "%")
	}
}

// BenchmarkMatchMailboxPattern_Prefix benchmarks prefix pattern matching.
func BenchmarkMatchMailboxPattern_Prefix(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matchMailboxPattern("Sent/Subfolder/Deep", "Sent*")
	}
}

// BenchmarkExtractEnvelope benchmarks envelope extraction.
func BenchmarkExtractEnvelope(b *testing.B) {
	data := []byte("Subject: Test Subject\r\n" +
		"From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Message-ID: <test123@example.com>\r\n" +
		"Date: Mon, 01 Jan 2024 12:00:00 +0000\r\n" +
		"\r\n" +
		"Body content here.")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractEnvelope(data)
	}
}

// BenchmarkExtractEnvelope_Large benchmarks envelope extraction with large headers.
func BenchmarkExtractEnvelope_Large(b *testing.B) {
	// Build a large header set
	data := []byte("Subject: This is a very long subject line that spans multiple words and contains a lot of text\r\n" +
		"From: \"Very Long Display Name With Many Words\" <sender@example.com>\r\n" +
		"To: recipient1@example.com, recipient2@example.com, recipient3@example.com, recipient4@example.com\r\n" +
		"Cc: cc1@example.com, cc2@example.com, cc3@example.com\r\n" +
		"Message-ID: <very-long-message-id-12345678901234567890@example.com>\r\n" +
		"Date: Mon, 01 Jan 2024 12:00:00 +0000\r\n" +
		"X-Custom-Header-1: value1\r\n" +
		"X-Custom-Header-2: value2\r\n" +
		"X-Custom-Header-3: value3\r\n" +
		"\r\n" +
		"Body content here.")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractEnvelope(data)
	}
}

// BenchmarkParseAddresses benchmarks address parsing.
func BenchmarkParseAddresses(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		parseAddresses("user@example.com")
	}
}

// BenchmarkParseAddresses_Multiple benchmarks parsing multiple addresses.
func BenchmarkParseAddresses_Multiple(b *testing.B) {
	input := "user1@example.com, user2@example.com, user3@example.com, user4@example.com, user5@example.com"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseAddresses(input)
	}
}

// BenchmarkParseAddresses_WithNames benchmarks parsing addresses with display names.
func BenchmarkParseAddresses_WithNames(b *testing.B) {
	input := "John Doe <john@example.com>, Jane Smith <jane@example.com>"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseAddresses(input)
	}
}

// BenchmarkBuildMessageMappings benchmarks message mapping construction.
func BenchmarkBuildMessageMappings_10(b *testing.B) {
	messages := make([]*storage.Message, 10)
	for i := 0; i < 10; i++ {
		messages[i] = &storage.Message{UID: uint32(i + 1)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildMessageMappings(messages)
	}
}

// BenchmarkBuildMessageMappings_100 benchmarks mapping with 100 messages.
func BenchmarkBuildMessageMappings_100(b *testing.B) {
	messages := make([]*storage.Message, 100)
	for i := 0; i < 100; i++ {
		messages[i] = &storage.Message{UID: uint32(i + 1)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildMessageMappings(messages)
	}
}

// BenchmarkBuildMessageMappings_1000 benchmarks mapping with 1000 messages.
func BenchmarkBuildMessageMappings_1000(b *testing.B) {
	messages := make([]*storage.Message, 1000)
	for i := 0; i < 1000; i++ {
		messages[i] = &storage.Message{UID: uint32(i + 1)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildMessageMappings(messages)
	}
}

// BenchmarkBuildMessageMappings_10000 benchmarks mapping with 10000 messages.
func BenchmarkBuildMessageMappings_10000(b *testing.B) {
	messages := make([]*storage.Message, 10000)
	for i := 0; i < 10000; i++ {
		messages[i] = &storage.Message{UID: uint32(i + 1)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildMessageMappings(messages)
	}
}

// BenchmarkSession_ChannelOperations benchmarks channel send/receive.
func BenchmarkSession_ChannelOperations(b *testing.B) {
	session := &Session{
		updates: make(chan any, 100),
		closed:  false,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		select {
		case session.updates <- i:
		default:
			// Drain if full
			select {
			case <-session.updates:
			default:
			}
		}
	}
}

// BenchmarkSession_ConcurrentUnselect benchmarks concurrent unselect calls.
func BenchmarkSession_ConcurrentUnselect(b *testing.B) {
	session := &Session{
		updates: make(chan any, 100),
		closed:  false,
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			session.Unselect()
		}
	})
}

// BenchmarkMessageMappings_Lookup benchmarks UID to sequence number lookup.
func BenchmarkMessageMappings_Lookup(b *testing.B) {
	messages := make([]*storage.Message, 1000)
	for i := 0; i < 1000; i++ {
		messages[i] = &storage.Message{UID: uint32(i + 1)}
	}
	m := buildMessageMappings(messages)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		uid := uint32((i % 1000) + 1)
		_ = m.uidToSeq[uid]
	}
}

// BenchmarkMessageMappings_SeqLookup benchmarks sequence to message lookup.
func BenchmarkMessageMappings_SeqLookup(b *testing.B) {
	messages := make([]*storage.Message, 1000)
	for i := 0; i < 1000; i++ {
		messages[i] = &storage.Message{UID: uint32(i + 1)}
	}
	m := buildMessageMappings(messages)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seq := uint32((i % 1000) + 1)
		_ = m.seqToMsg[seq]
	}
}

// BenchmarkSession_FieldAccess benchmarks session field access.
func BenchmarkSession_FieldAccess(b *testing.B) {
	session := &Session{
		updates: make(chan any, 100),
		closed:  false,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = session.user
		_ = session.selected
		_ = session.closed
	}
}

// BenchmarkSession_Poll_NilTracker benchmarks poll with nil tracker.
func BenchmarkSession_Poll_NilTracker(b *testing.B) {
	session := &Session{
		updates: make(chan any, 100),
		closed:  false,
		tracker: nil,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		session.Poll(nil, true)
	}
}

// BenchmarkSession_MutexLockUnlock benchmarks raw mutex operations.
func BenchmarkSession_MutexLockUnlock(b *testing.B) {
	var mu sync.RWMutex
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mu.RLock()
		mu.RUnlock()
	}
}

// BenchmarkSession_MutexLockUnlock_Parallel benchmarks parallel mutex operations.
func BenchmarkSession_MutexLockUnlock_Parallel(b *testing.B) {
	var mu sync.RWMutex
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mu.RLock()
			mu.RUnlock()
		}
	})
}
