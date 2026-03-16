package imap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/metrics"
	"github.com/fenilsonani/email-server/internal/search"
	"github.com/fenilsonani/email-server/internal/search/query"
	"github.com/fenilsonani/email-server/internal/storage"
)

// Flag slice pools to reduce allocations in hot paths.
// Most messages have 1-5 flags, so we pool slices with capacity 8.
var (
	imapFlagPool = sync.Pool{
		New: func() any {
			s := make([]imap.Flag, 0, 8)
			return &s
		},
	}
	storageFlagPool = sync.Pool{
		New: func() any {
			s := make([]storage.Flag, 0, 8)
			return &s
		},
	}
	// Buffer pool for reading message bodies
	bodyBufferPool = sync.Pool{
		New: func() any {
			return bytes.NewBuffer(make([]byte, 0, 64*1024)) // 64KB initial
		},
	}
)

// getImapFlags gets a flag slice from pool and populates it
func getImapFlags(flags []storage.Flag) []imap.Flag {
	ptr := imapFlagPool.Get().(*[]imap.Flag)
	result := (*ptr)[:0]
	for _, f := range flags {
		result = append(result, imap.Flag(f))
	}
	return result
}

// putImapFlags returns a flag slice to the pool
func putImapFlags(flags []imap.Flag) {
	if cap(flags) <= 16 { // Only pool reasonably sized slices
		flags = flags[:0]
		imapFlagPool.Put(&flags)
	}
}

// getStorageFlags gets a storage flag slice from pool and populates it
func getStorageFlags(flags []imap.Flag) []storage.Flag {
	ptr := storageFlagPool.Get().(*[]storage.Flag)
	result := (*ptr)[:0]
	for _, f := range flags {
		result = append(result, storage.Flag(f))
	}
	return result
}

// putStorageFlags returns a storage flag slice to the pool
func putStorageFlags(flags []storage.Flag) {
	if cap(flags) <= 16 {
		flags = flags[:0]
		storageFlagPool.Put(&flags)
	}
}

// readBodyEfficiently reads message body using pooled buffer
func readBodyEfficiently(r io.ReadCloser) ([]byte, error) {
	buf := bodyBufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	_, err := buf.ReadFrom(r)
	r.Close()

	if err != nil {
		bodyBufferPool.Put(buf)
		return nil, err
	}

	// Make a copy since we're returning the buffer to pool
	data := make([]byte, buf.Len())
	copy(data, buf.Bytes())
	bodyBufferPool.Put(buf)
	return data, nil
}

// messageMappings holds pre-allocated maps for seq/uid lookups
// This reduces GC pressure by pre-allocating with known capacity
type messageMappings struct {
	seqToMsg map[uint32]*storage.Message
	uidToSeq map[uint32]uint32
	messages []*storage.Message
}

// buildMessageMappings creates mappings with pre-allocated capacity
func buildMessageMappings(messages []*storage.Message) *messageMappings {
	n := len(messages)
	m := &messageMappings{
		seqToMsg: make(map[uint32]*storage.Message, n),
		uidToSeq: make(map[uint32]uint32, n),
		messages: messages,
	}
	for i, msg := range messages {
		seqNum := uint32(i + 1)
		m.seqToMsg[seqNum] = msg
		m.uidToSeq[msg.UID] = seqNum
	}
	return m
}

// Session implements imapserver.Session for go-imap v2
type Session struct {
	server     *Server
	conn       *imapserver.Conn
	remoteAddr string
	user       *auth.User
	selected   *storage.Mailbox
	tracker    *imapserver.SessionTracker
	updates    chan any
	mu         sync.RWMutex
	closed     bool
}

// NewSession creates a new IMAP session
func NewSession(server *Server, conn *imapserver.Conn) *Session {
	remoteAddr := ""
	if netConn := conn.NetConn(); netConn != nil {
		remoteAddr = netConn.RemoteAddr().String()
	}
	return &Session{
		server:     server,
		conn:       conn,
		remoteAddr: remoteAddr,
		updates:    make(chan any, 100),
	}
}

// Close cleans up the session
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prevent double close
	if s.closed {
		return nil
	}
	s.closed = true

	if s.tracker != nil {
		s.tracker.Close()
		s.tracker = nil
	}

	// Close channel safely
	if s.updates != nil {
		close(s.updates)
		s.updates = nil
	}

	return nil
}

// Login authenticates the user
func (s *Session) Login(username, password string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("IMAP v2: Login attempt for %s from %s", username, s.remoteAddr)

	user, err := s.server.authenticator.Authenticate(ctx, username, password)
	if err != nil {
		log.Printf("IMAP v2: Login failed for %s: %v", username, err)
		// Log failed auth attempt
		s.server.authenticator.LogAuthAttempt(ctx, nil, username, s.remoteAddr, "imap", false, err.Error())
		return imapserver.ErrAuthFailed
	}

	s.mu.Lock()
	s.user = user
	s.mu.Unlock()

	log.Printf("IMAP v2: Login successful for %s", username)
	// Log successful auth attempt
	s.server.authenticator.LogAuthAttempt(ctx, &user.ID, username, s.remoteAddr, "imap", true, "")

	// Ensure all default mailboxes exist for this user (handles existing users that may not have new mailboxes like Screener)
	if err := s.server.store.EnsureDefaultMailboxes(ctx, user.ID); err != nil {
		log.Printf("IMAP v2: Warning - failed to ensure default mailboxes for user %d: %v", user.ID, err)
		// Log warning but don't fail the login - user can still access existing mailboxes
	}

	return nil
}

// Select opens a mailbox
func (s *Session) Select(name string, options *imap.SelectOptions) (*imap.SelectData, error) {
	s.mu.RLock()
	user := s.user
	s.mu.RUnlock()

	if user == nil {
		return nil, fmt.Errorf("not authenticated")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mb, err := s.server.store.GetMailbox(ctx, user.ID, name)
	if err != nil {
		return nil, &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeNonExistent,
			Text: "Mailbox not found",
		}
	}

	stats, err := s.server.store.GetMailboxStats(ctx, mb.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get mailbox stats: %w", err)
	}

	s.mu.Lock()
	s.selected = mb
	// Create tracker for this mailbox
	if s.tracker != nil {
		s.tracker.Close()
	}
	s.tracker = s.server.GetMailboxTracker(mb.ID).NewSession()
	s.mu.Unlock()

	return &imap.SelectData{
		Flags:          []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagFlagged, imap.FlagDeleted, imap.FlagDraft},
		PermanentFlags: []imap.Flag{imap.FlagSeen, imap.FlagAnswered, imap.FlagFlagged, imap.FlagDeleted, imap.FlagDraft, imap.FlagWildcard},
		NumMessages:    safeMessageCount(stats.Messages),
		UIDValidity:    stats.UIDValidity,
		UIDNext:        imap.UID(stats.UIDNext),
	}, nil
}

// Unselect closes the current mailbox
func (s *Session) Unselect() error {
	s.mu.Lock()
	s.selected = nil
	if s.tracker != nil {
		s.tracker.Close()
		s.tracker = nil
	}
	s.mu.Unlock()
	return nil
}

// Create creates a new mailbox
func (s *Session) Create(name string, options *imap.CreateOptions) error {
	s.mu.RLock()
	user := s.user
	s.mu.RUnlock()

	if user == nil {
		return fmt.Errorf("not authenticated")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := s.server.store.CreateMailbox(ctx, user.ID, name, "")
	return err
}

// Delete removes a mailbox
func (s *Session) Delete(name string) error {
	s.mu.RLock()
	user := s.user
	s.mu.RUnlock()

	if user == nil {
		return fmt.Errorf("not authenticated")
	}

	if name == "INBOX" {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "Cannot delete INBOX",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return s.server.store.DeleteMailbox(ctx, user.ID, name)
}

// Rename renames a mailbox
func (s *Session) Rename(oldName, newName string, options *imap.RenameOptions) error {
	s.mu.RLock()
	user := s.user
	s.mu.RUnlock()

	if user == nil {
		return fmt.Errorf("not authenticated")
	}

	if oldName == "INBOX" {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "Cannot rename INBOX",
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return s.server.store.RenameMailbox(ctx, user.ID, oldName, newName)
}

// Subscribe subscribes to a mailbox
func (s *Session) Subscribe(name string) error {
	s.mu.RLock()
	user := s.user
	s.mu.RUnlock()

	if user == nil {
		return fmt.Errorf("not authenticated")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return s.server.store.SubscribeMailbox(ctx, user.ID, name, true)
}

// Unsubscribe unsubscribes from a mailbox
func (s *Session) Unsubscribe(name string) error {
	s.mu.RLock()
	user := s.user
	s.mu.RUnlock()

	if user == nil {
		return fmt.Errorf("not authenticated")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return s.server.store.SubscribeMailbox(ctx, user.ID, name, false)
}

// List lists mailboxes
func (s *Session) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	s.mu.RLock()
	user := s.user
	s.mu.RUnlock()

	if user == nil {
		return fmt.Errorf("not authenticated")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mailboxes, err := s.server.store.ListMailboxes(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("failed to list mailboxes: %w", err)
	}

	for _, mb := range mailboxes {
		// Check if matches pattern
		match := false
		for _, pattern := range patterns {
			if pattern == "*" || pattern == "%" || matchMailboxPattern(mb.Name, pattern) {
				match = true
				break
			}
		}
		if !match && len(patterns) > 0 {
			continue
		}

		// Skip unsubscribed if requested
		if options != nil && options.SelectSubscribed && !mb.Subscribed {
			continue
		}

		attrs := []imap.MailboxAttr{}
		if mb.SpecialUse != "" {
			attrs = append(attrs, imap.MailboxAttr(mb.SpecialUse))
		}

		w.WriteList(&imap.ListData{
			Mailbox: mb.Name,
			Delim:   '/',
			Attrs:   attrs,
		})
	}

	return nil
}

// Status returns mailbox status
func (s *Session) Status(name string, options *imap.StatusOptions) (*imap.StatusData, error) {
	s.mu.RLock()
	user := s.user
	s.mu.RUnlock()

	if user == nil {
		return nil, fmt.Errorf("not authenticated")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mb, err := s.server.store.GetMailbox(ctx, user.ID, name)
	if err != nil {
		return nil, &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeNonExistent,
			Text: "Mailbox not found",
		}
	}

	stats, err := s.server.store.GetMailboxStats(ctx, mb.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get mailbox stats: %w", err)
	}

	numMessages := safeMessageCount(stats.Messages)
	numUnseen := safeMessageCount(stats.Unseen)

	return &imap.StatusData{
		Mailbox:     name,
		NumMessages: &numMessages,
		NumUnseen:   &numUnseen,
		UIDNext:     imap.UID(stats.UIDNext),
		UIDValidity: stats.UIDValidity,
	}, nil
}

// Append adds a message to a mailbox
func (s *Session) Append(mailbox string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	s.mu.RLock()
	user := s.user
	s.mu.RUnlock()

	if user == nil {
		return nil, fmt.Errorf("not authenticated")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mb, err := s.server.store.GetMailbox(ctx, user.ID, mailbox)
	if err != nil {
		return nil, &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeTryCreate,
			Text: "Mailbox not found",
		}
	}

	// Convert flags
	var flags []storage.Flag
	if options != nil && len(options.Flags) > 0 {
		flags = make([]storage.Flag, len(options.Flags))
		for i, f := range options.Flags {
			flags[i] = storage.Flag(f)
		}
	}

	date := time.Now()
	if options != nil && !options.Time.IsZero() {
		date = options.Time
	}

	msg, err := s.server.store.AppendMessage(ctx, mb.ID, flags, date, r)
	if err != nil {
		return nil, fmt.Errorf("failed to append message: %w", err)
	}

	// Notify other sessions about new message
	s.server.NotifyMailboxUpdate(mb.ID)

	return &imap.AppendData{
		UID:         imap.UID(msg.UID),
		UIDValidity: mb.UIDValidity,
	}, nil
}

// Poll checks for updates (called periodically)
func (s *Session) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	s.mu.RLock()
	tracker := s.tracker
	s.mu.RUnlock()

	if tracker != nil {
		return tracker.Poll(w, allowExpunge)
	}
	return nil
}

// defaultIdleKeepaliveInterval is the default keepalive interval during IDLE.
// Apple Mail and other clients expect periodic responses to detect dead connections.
// NAT/firewall timeouts are typically 5-30 minutes. Using 3 minutes provides better
// compatibility with strict NAT environments and Apple Mail.
const defaultIdleKeepaliveInterval = 3 * time.Minute

// getIdleKeepaliveInterval returns the configured IDLE keepalive interval
func (s *Session) getIdleKeepaliveInterval() time.Duration {
	if s.server != nil && s.server.config != nil && s.server.config.IdleKeepaliveInterval > 0 {
		return s.server.config.IdleKeepaliveInterval
	}
	return defaultIdleKeepaliveInterval
}

// Idle handles IDLE command - the key to instant notifications!
// Includes keepalive mechanism to prevent connection drops from NAT/firewall timeouts.
func (s *Session) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	s.mu.RLock()
	tracker := s.tracker
	user := s.user
	selected := s.selected
	s.mu.RUnlock()

	if tracker == nil {
		<-stop
		return nil
	}

	// Safely log user email with nil check
	userEmail := "unknown"
	if user != nil {
		userEmail = user.Email
	}

	log.Printf("IMAP v2: IDLE started for %s", userEmail)
	metrics.RecordIMAPIdleStart()
	defer func() {
		metrics.RecordIMAPIdleEnd()
		log.Printf("IMAP v2: IDLE ended for %s", userEmail)
	}()

	// Create a done channel for the tracker goroutine
	done := make(chan error, 1)
	trackerStop := make(chan struct{})

	// Run tracker.Idle in a goroutine
	go func() {
		done <- tracker.Idle(w, trackerStop)
	}()

	// Keepalive ticker to prevent NAT/firewall timeouts
	keepaliveInterval := s.getIdleKeepaliveInterval()
	keepaliveTicker := time.NewTicker(keepaliveInterval)
	defer keepaliveTicker.Stop()

	for {
		select {
		case <-stop:
			// Client sent DONE, stop the tracker
			close(trackerStop)
			return <-done

		case err := <-done:
			// Tracker finished (shouldn't happen normally during IDLE)
			return err

		case <-keepaliveTicker.C:
			// Send keepalive by triggering a mailbox status update
			// This causes the server to send an EXISTS response with current count
			if selected != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				stats, err := s.server.store.GetMailboxStats(ctx, selected.ID)
				cancel()
				if err == nil {
					// Queue the current message count - this triggers an untagged response
					// even if the count hasn't changed, keeping the connection alive
					s.server.GetMailboxTracker(selected.ID).QueueNumMessages(safeMessageCount(stats.Messages))
					metrics.RecordIMAPKeepalive()
					log.Printf("IMAP v2: Keepalive sent for %s (messages: %d)", userEmail, stats.Messages)
				}
			}
		}
	}
}

// Fetch retrieves messages
func (s *Session) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
	s.mu.RLock()
	selected := s.selected
	s.mu.RUnlock()

	if selected == nil {
		return fmt.Errorf("no mailbox selected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get all messages to build seq->uid mapping
	messages, err := s.server.store.ListMessages(ctx, selected.ID, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	// Build mappings with pre-allocated capacity
	m := buildMessageMappings(messages)

	// Determine which messages to fetch based on set type
	// Pre-allocate slice with estimated capacity
	toFetch := make([]uint32, 0, len(messages)/2+1)
	switch set := numSet.(type) {
	case imap.UIDSet:
		// UID set
		for uid := range m.uidToSeq {
			if set.Contains(imap.UID(uid)) {
				toFetch = append(toFetch, m.uidToSeq[uid])
			}
		}
	case imap.SeqSet:
		// Sequence set
		for seqNum := range m.seqToMsg {
			if set.Contains(seqNum) {
				toFetch = append(toFetch, seqNum)
			}
		}
	}

	// Fetch each message
	for _, seqNum := range toFetch {
		msg := m.seqToMsg[seqNum]
		if msg == nil {
			continue
		}

		respWriter := w.CreateMessage(seqNum)

		// Always include UID
		respWriter.WriteUID(imap.UID(msg.UID))

		// Write flags (using pooled slice)
		if options.Flags {
			flags := getImapFlags(msg.Flags)
			respWriter.WriteFlags(flags)
			putImapFlags(flags)
		}

		// Write internal date
		if options.InternalDate {
			respWriter.WriteInternalDate(msg.InternalDate)
		}

		// Write size
		if options.RFC822Size {
			respWriter.WriteRFC822Size(msg.Size)
		}

		// Write envelope (using pooled buffer)
		if options.Envelope {
			body, err := s.server.store.GetMessageBody(ctx, msg)
			if err == nil {
				data, readErr := readBodyEfficiently(body)
				if readErr == nil {
					envelope := extractEnvelope(data)
					respWriter.WriteEnvelope(envelope)
				} else {
					log.Printf("IMAP: Failed to read message body for envelope: %v", readErr)
				}
			} else {
				log.Printf("IMAP: Failed to get message body for envelope: %v", err)
			}
		}

		// Write body sections (using pooled buffer)
		for _, bs := range options.BodySection {
			body, err := s.server.store.GetMessageBody(ctx, msg)
			if err != nil {
				log.Printf("IMAP: Failed to get message body for section: %v", err)
				continue
			}

			data, readErr := readBodyEfficiently(body)
			if readErr != nil {
				log.Printf("IMAP: Failed to read message body for section: %v", readErr)
				continue
			}

			sectionData := extractBodySection(data, bs)
			bsw := respWriter.WriteBodySection(bs, int64(len(sectionData)))
			if _, err := bsw.Write(sectionData); err != nil {
				log.Printf("IMAP: Failed to write body section: %v", err)
			}
			bsw.Close()
		}

		respWriter.Close()
	}

	return nil
}

// Store updates message flags
func (s *Session) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	s.mu.RLock()
	selected := s.selected
	s.mu.RUnlock()

	if selected == nil {
		return fmt.Errorf("no mailbox selected")
	}

	if flags == nil {
		return fmt.Errorf("flags cannot be nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get all messages for mapping
	messages, err := s.server.store.ListMessages(ctx, selected.ID, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	// Build mappings with pre-allocated capacity
	m := buildMessageMappings(messages)

	// Determine which messages to update based on set type
	// Pre-allocate slice with estimated capacity
	toUpdate := make([]uint32, 0, len(messages)/2+1)
	switch set := numSet.(type) {
	case imap.UIDSet:
		for uid := range m.uidToSeq {
			if set.Contains(imap.UID(uid)) {
				toUpdate = append(toUpdate, m.uidToSeq[uid])
			}
		}
	case imap.SeqSet:
		for seqNum := range m.seqToMsg {
			if set.Contains(seqNum) {
				toUpdate = append(toUpdate, seqNum)
			}
		}
	}

	// Update each message (using pooled flag slices)
	storageFlags := getStorageFlags(flags.Flags)
	defer putStorageFlags(storageFlags)

	for _, seqNum := range toUpdate {
		msg := m.seqToMsg[seqNum]
		if msg == nil {
			continue
		}

		switch flags.Op {
		case imap.StoreFlagsAdd:
			err = s.server.store.UpdateFlags(ctx, selected.ID, msg.UID, storageFlags, true)
		case imap.StoreFlagsDel:
			err = s.server.store.UpdateFlags(ctx, selected.ID, msg.UID, storageFlags, false)
		case imap.StoreFlagsSet:
			err = s.server.store.SetFlags(ctx, selected.ID, msg.UID, storageFlags)
		}

		if err != nil {
			log.Printf("IMAP: Failed to update flags for message UID %d: %v", msg.UID, err)
			continue
		}

		// Send updated flags unless silent
		if !flags.Silent {
			respWriter := w.CreateMessage(seqNum)
			// Get updated message
			updatedMsg, err := s.server.store.GetMessage(ctx, selected.ID, msg.UID)
			if err != nil {
				log.Printf("IMAP: Failed to get updated message UID %d: %v", msg.UID, err)
			} else if updatedMsg != nil {
				newFlags := getImapFlags(updatedMsg.Flags)
				respWriter.WriteFlags(newFlags)
				putImapFlags(newFlags)
			}
			respWriter.Close()
		}
	}

	return nil
}

// Expunge removes deleted messages
func (s *Session) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	s.mu.RLock()
	selected := s.selected
	s.mu.RUnlock()

	if selected == nil {
		return fmt.Errorf("no mailbox selected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	expunged, err := s.server.store.ExpungeMailbox(ctx, selected.ID)
	if err != nil {
		return fmt.Errorf("failed to expunge mailbox: %w", err)
	}

	// Get current message list for seq mapping
	messages, err := s.server.store.ListMessages(ctx, selected.ID, 0, 0)
	if err != nil {
		log.Printf("IMAP: Failed to list messages after expunge: %v", err)
		// Still report expunged messages even if we can't get seq numbers
		for _, uid := range expunged {
			w.WriteExpunge(uid)
		}
		return nil
	}

	// Build mapping with pre-allocated capacity
	m := buildMessageMappings(messages)

	for _, uid := range expunged {
		if seqNum, ok := m.uidToSeq[uid]; ok {
			w.WriteExpunge(seqNum)
		}
	}

	return nil
}

// Copy copies messages to another mailbox
func (s *Session) Copy(numSet imap.NumSet, dest string) (*imap.CopyData, error) {
	s.mu.RLock()
	selected := s.selected
	user := s.user
	s.mu.RUnlock()

	if selected == nil {
		return nil, fmt.Errorf("no mailbox selected")
	}

	if user == nil {
		return nil, fmt.Errorf("not authenticated")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get destination mailbox
	destMb, err := s.server.store.GetMailbox(ctx, user.ID, dest)
	if err != nil {
		return nil, &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeTryCreate,
			Text: "Destination mailbox not found",
		}
	}

	// Get messages
	messages, err := s.server.store.ListMessages(ctx, selected.ID, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}

	var srcUIDs, destUIDs []imap.UID

	for i, msg := range messages {
		seqNum := uint32(i + 1)
		var shouldCopy bool
		switch set := numSet.(type) {
		case imap.UIDSet:
			shouldCopy = set.Contains(imap.UID(msg.UID))
		case imap.SeqSet:
			shouldCopy = set.Contains(seqNum)
		}

		if shouldCopy {
			newMsg, err := s.server.store.CopyMessage(ctx, selected.ID, msg.UID, destMb.ID)
			if err == nil {
				srcUIDs = append(srcUIDs, imap.UID(msg.UID))
				destUIDs = append(destUIDs, imap.UID(newMsg.UID))
			} else {
				log.Printf("IMAP: Failed to copy message UID %d: %v", msg.UID, err)
			}
		}
	}

	// Notify destination mailbox
	s.server.NotifyMailboxUpdate(destMb.ID)

	return &imap.CopyData{
		UIDValidity: destMb.UIDValidity,
		SourceUIDs:  imap.UIDSetNum(srcUIDs...),
		DestUIDs:    imap.UIDSetNum(destUIDs...),
	}, nil
}

// Move moves messages to another mailbox (RFC 6851)
// This is more efficient than COPY + STORE \Deleted + EXPUNGE
func (s *Session) Move(w *imapserver.MoveWriter, numSet imap.NumSet, dest string) error {
	s.mu.RLock()
	selected := s.selected
	user := s.user
	s.mu.RUnlock()

	if selected == nil {
		return fmt.Errorf("no mailbox selected")
	}

	if user == nil {
		return fmt.Errorf("not authenticated")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get destination mailbox
	destMb, err := s.server.store.GetMailbox(ctx, user.ID, dest)
	if err != nil {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeTryCreate,
			Text: "Destination mailbox not found",
		}
	}

	// Get messages
	messages, err := s.server.store.ListMessages(ctx, selected.ID, 0, 0)
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	var srcUIDs, destUIDs []imap.UID
	var expungeSeqs []uint32

	for i, msg := range messages {
		seqNum := uint32(i + 1)
		var shouldMove bool
		switch set := numSet.(type) {
		case imap.UIDSet:
			shouldMove = set.Contains(imap.UID(msg.UID))
		case imap.SeqSet:
			shouldMove = set.Contains(seqNum)
		}

		if shouldMove {
			// Copy message to destination
			newMsg, err := s.server.store.CopyMessage(ctx, selected.ID, msg.UID, destMb.ID)
			if err != nil {
				log.Printf("IMAP: Failed to move message UID %d: %v", msg.UID, err)
				continue
			}

			srcUIDs = append(srcUIDs, imap.UID(msg.UID))
			destUIDs = append(destUIDs, imap.UID(newMsg.UID))
			expungeSeqs = append(expungeSeqs, seqNum)

			// Delete from source
			if err := s.server.store.DeleteMessage(ctx, selected.ID, msg.UID); err != nil {
				log.Printf("IMAP: Failed to delete moved message UID %d: %v", msg.UID, err)
			}
		}
	}

	// Write COPYUID response
	if len(srcUIDs) > 0 {
		w.WriteCopyData(&imap.CopyData{
			UIDValidity: destMb.UIDValidity,
			SourceUIDs:  imap.UIDSetNum(srcUIDs...),
			DestUIDs:    imap.UIDSetNum(destUIDs...),
		})

		// Write EXPUNGE responses (in reverse order for correct sequence numbers)
		for i := len(expungeSeqs) - 1; i >= 0; i-- {
			w.WriteExpunge(expungeSeqs[i])
		}
	}

	// Notify both mailboxes
	s.server.NotifyMailboxUpdate(selected.ID)
	s.server.NotifyMailboxUpdate(destMb.ID)

	return nil
}

// Namespace returns the namespace hierarchy (RFC 2342)
// Thunderbird and other clients use this to understand mailbox organization
func (s *Session) Namespace() (*imap.NamespaceData, error) {
	s.mu.RLock()
	user := s.user
	s.mu.RUnlock()

	if user == nil {
		return nil, fmt.Errorf("not authenticated")
	}

	// Return standard personal namespace with "/" as delimiter
	// Most email clients expect this format
	return &imap.NamespaceData{
		Personal: []imap.NamespaceDescriptor{
			{
				Prefix: "",
				Delim:  '/',
			},
		},
		// No shared or other namespaces (single-user mailboxes)
		Other:  nil,
		Shared: nil,
	}, nil
}

// Search searches for messages
func (s *Session) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	s.mu.RLock()
	selected := s.selected
	user := s.user
	s.mu.RUnlock()

	if selected == nil {
		return nil, fmt.Errorf("no mailbox selected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var uids []uint32
	var err error

	// Check if full-text search is needed and available
	if s.server.searchEngine != nil && needsFullTextSearch(criteria) {
		// Use full-text search engine
		uids, err = s.searchWithEngine(ctx, selected.ID, user.ID, criteria)
	} else {
		// Fall back to database search
		uids, err = s.searchWithStorage(ctx, selected.ID, criteria)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to search messages: %w", err)
	}

	if kind == imapserver.NumKindUID {
		imapUIDs := make([]imap.UID, len(uids))
		for i, uid := range uids {
			imapUIDs[i] = imap.UID(uid)
		}
		return &imap.SearchData{
			All: imap.UIDSetNum(imapUIDs...),
		}, nil
	}

	// Convert UIDs to sequence numbers
	messages, err := s.server.store.ListMessages(ctx, selected.ID, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages for seq conversion: %w", err)
	}

	// Build mapping with pre-allocated capacity
	m := buildMessageMappings(messages)

	// Pre-allocate slice with expected capacity
	seqNums := make([]uint32, 0, len(uids))
	for _, uid := range uids {
		if seq, ok := m.uidToSeq[uid]; ok {
			seqNums = append(seqNums, seq)
		}
	}

	return &imap.SearchData{
		All: imap.SeqSetNum(seqNums...),
	}, nil
}

// searchWithStorage uses the storage layer for simple searches
func (s *Session) searchWithStorage(ctx context.Context, mailboxID int64, criteria *imap.SearchCriteria) ([]uint32, error) {
	storageCriteria := &storage.SearchCriteria{}
	if criteria != nil {
		if !criteria.Since.IsZero() {
			storageCriteria.Since = &criteria.Since
		}
		if !criteria.Before.IsZero() {
			storageCriteria.Before = &criteria.Before
		}
		for _, f := range criteria.Flag {
			storageCriteria.Flags = append(storageCriteria.Flags, storage.Flag(f))
		}
		for _, f := range criteria.NotFlag {
			storageCriteria.NotFlags = append(storageCriteria.NotFlags, storage.Flag(f))
		}
	}
	return s.server.store.SearchMessages(ctx, mailboxID, storageCriteria)
}

// searchWithEngine uses the full-text search engine
func (s *Session) searchWithEngine(ctx context.Context, mailboxID, userID int64, criteria *imap.SearchCriteria) ([]uint32, error) {
	// Build search query from IMAP criteria
	sq := buildSearchQuery(criteria, mailboxID, userID)

	// Execute search
	result, err := s.server.searchEngine.Search(ctx, sq)
	if err != nil {
		// Fall back to storage search on error
		log.Printf("Full-text search failed, falling back to storage: %v", err)
		return s.searchWithStorage(ctx, mailboxID, criteria)
	}

	// Extract UIDs from search results
	uids := make([]uint32, 0, len(result.Hits))
	for _, hit := range result.Hits {
		if hit.MailboxID == mailboxID {
			uids = append(uids, hit.UID)
		}
	}

	return uids, nil
}

// needsFullTextSearch checks if the criteria requires full-text search.
// Delegates to query.IsFullTextSearch which handles Body, Text, Header,
// and recursive NOT/OR checks.
func needsFullTextSearch(criteria *imap.SearchCriteria) bool {
	return query.IsFullTextSearch(criteria)
}

// buildSearchQuery converts IMAP criteria to a search query
func buildSearchQuery(criteria *imap.SearchCriteria, mailboxID, userID int64) *search.SearchQuery {
	sq := &search.SearchQuery{
		MailboxID: mailboxID,
		UserID:    userID,
		Limit:     10000, // IMAP searches return all matches
	}

	if criteria == nil {
		return sq
	}

	// Date filters
	if !criteria.Since.IsZero() {
		t := criteria.Since
		sq.Since = &t
	}
	if !criteria.Before.IsZero() {
		t := criteria.Before
		sq.Before = &t
	}

	// Flag filters
	for _, flag := range criteria.Flag {
		sq.HasFlags = append(sq.HasFlags, string(flag))
	}
	for _, flag := range criteria.NotFlag {
		sq.NotFlags = append(sq.NotFlags, string(flag))
	}

	// Header searches
	for _, h := range criteria.Header {
		key := strings.ToLower(h.Key)
		switch key {
		case "from":
			sq.From = h.Value
		case "to":
			sq.To = h.Value
		case "subject":
			sq.Subject = h.Value
		}
	}

	// Body search
	for _, bodyPart := range criteria.Body {
		if sq.Body != "" {
			sq.Body = sq.Body + " " + bodyPart
		} else {
			sq.Body = bodyPart
		}
	}

	// Text search (searches all fields)
	for _, text := range criteria.Text {
		if sq.Text != "" {
			sq.Text = sq.Text + " " + text
		} else {
			sq.Text = text
		}
	}

	return sq
}

// Helper functions

func matchMailboxPattern(name, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == "%" {
		return !strings.Contains(name, "/")
	}
	// Simple prefix match for now
	return strings.HasPrefix(name, strings.TrimSuffix(pattern, "*"))
}

func extractEnvelope(data []byte) *imap.Envelope {
	// Simple envelope extraction - in production use proper MIME parsing
	env := &imap.Envelope{}

	// Convert to string once
	dataStr := string(data)
	lines := strings.Split(dataStr, "\n")

	for _, line := range lines {
		if len(line) == 0 || line == "\r" {
			break // End of headers
		}

		// Use case-insensitive prefix matching without allocating new strings
		lineLower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lineLower, "subject:"):
			env.Subject = strings.TrimSpace(line[8:])
		case strings.HasPrefix(lineLower, "date:"):
			dateStr := strings.TrimSpace(line[5:])
			// Try multiple date formats
			if t, err := time.Parse(time.RFC1123Z, dateStr); err == nil {
				env.Date = t
			} else if t, err := time.Parse(time.RFC1123, dateStr); err == nil {
				env.Date = t
			} else if t, err := time.Parse(time.RFC822Z, dateStr); err == nil {
				env.Date = t
			}
		case strings.HasPrefix(lineLower, "from:"):
			env.From = parseAddresses(strings.TrimSpace(line[5:]))
		case strings.HasPrefix(lineLower, "to:"):
			env.To = parseAddresses(strings.TrimSpace(line[3:]))
		case strings.HasPrefix(lineLower, "message-id:"):
			env.MessageID = strings.TrimSpace(line[11:])
		}
	}

	return env
}

func parseAddresses(s string) []imap.Address {
	if s == "" {
		return nil
	}

	// Simple address parsing with preallocated slice
	parts := strings.Split(s, ",")
	// Preallocate with capacity based on number of parts
	addrs := make([]imap.Address, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		addr := imap.Address{}
		if idx := strings.Index(part, "<"); idx >= 0 {
			addr.Name = strings.TrimSpace(part[:idx])
			end := strings.Index(part, ">")
			if end > idx {
				email := part[idx+1 : end]
				if at := strings.Index(email, "@"); at >= 0 {
					addr.Mailbox = email[:at]
					addr.Host = email[at+1:]
				}
			}
		} else if at := strings.Index(part, "@"); at >= 0 {
			addr.Mailbox = part[:at]
			addr.Host = part[at+1:]
		}
		addrs = append(addrs, addr)
	}
	return addrs
}

func extractBodySection(data []byte, section *imap.FetchItemBodySection) []byte {
	// For now, return full message for BODY[] requests
	if section.Part == nil || len(section.Part) == 0 {
		if section.Specifier == imap.PartSpecifierHeader {
			// Return just headers
			if idx := bytes.Index(data, []byte("\r\n\r\n")); idx >= 0 {
				return data[:idx+2]
			}
			if idx := bytes.Index(data, []byte("\n\n")); idx >= 0 {
				return data[:idx+1]
			}
		} else if section.Specifier == imap.PartSpecifierText {
			// Return just body
			if idx := bytes.Index(data, []byte("\r\n\r\n")); idx >= 0 {
				return data[idx+4:]
			}
			if idx := bytes.Index(data, []byte("\n\n")); idx >= 0 {
				return data[idx+2:]
			}
		}
		return data
	}
	return data
}
