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
	"github.com/fenilsonani/email-server/internal/storage"
)

// Session implements imapserver.Session for go-imap v2
type Session struct {
	server   *Server
	conn     *imapserver.Conn
	user     *auth.User
	selected *storage.Mailbox
	tracker  *imapserver.SessionTracker
	updates  chan any
	mu       sync.RWMutex
	closed   bool
}

// NewSession creates a new IMAP session
func NewSession(server *Server, conn *imapserver.Conn) *Session {
	return &Session{
		server:  server,
		conn:    conn,
		updates: make(chan any, 100),
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

	log.Printf("IMAP v2: Login attempt for %s", username)

	user, err := s.server.authenticator.Authenticate(ctx, username, password)
	if err != nil {
		log.Printf("IMAP v2: Login failed for %s: %v", username, err)
		return imapserver.ErrAuthFailed
	}

	s.mu.Lock()
	s.user = user
	s.mu.Unlock()

	log.Printf("IMAP v2: Login successful for %s", username)
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
		NumMessages:    uint32(stats.Messages),
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

	numMessages := uint32(stats.Messages)
	numUnseen := uint32(stats.Unseen)

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

// Idle handles IDLE command - the key to instant notifications!
func (s *Session) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	s.mu.RLock()
	tracker := s.tracker
	user := s.user
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
	defer log.Printf("IMAP v2: IDLE ended for %s", userEmail)

	return tracker.Idle(w, stop)
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

	// Build mappings
	seqToMsg := make(map[uint32]*storage.Message)
	uidToSeq := make(map[uint32]uint32)
	for i, msg := range messages {
		seqNum := uint32(i + 1)
		seqToMsg[seqNum] = msg
		uidToSeq[msg.UID] = seqNum
	}

	// Determine which messages to fetch based on set type
	var toFetch []uint32
	switch set := numSet.(type) {
	case imap.UIDSet:
		// UID set
		for uid := range uidToSeq {
			if set.Contains(imap.UID(uid)) {
				toFetch = append(toFetch, uidToSeq[uid])
			}
		}
	case imap.SeqSet:
		// Sequence set
		for seqNum := range seqToMsg {
			if set.Contains(seqNum) {
				toFetch = append(toFetch, seqNum)
			}
		}
	}

	// Fetch each message
	for _, seqNum := range toFetch {
		msg := seqToMsg[seqNum]
		if msg == nil {
			continue
		}

		respWriter := w.CreateMessage(seqNum)

		// Always include UID
		respWriter.WriteUID(imap.UID(msg.UID))

		// Write flags
		if options.Flags {
			flags := make([]imap.Flag, len(msg.Flags))
			for i, f := range msg.Flags {
				flags[i] = imap.Flag(f)
			}
			respWriter.WriteFlags(flags)
		}

		// Write internal date
		if options.InternalDate {
			respWriter.WriteInternalDate(msg.InternalDate)
		}

		// Write size
		if options.RFC822Size {
			respWriter.WriteRFC822Size(msg.Size)
		}

		// Write envelope
		if options.Envelope {
			body, err := s.server.store.GetMessageBody(ctx, msg)
			if err == nil {
				data, readErr := io.ReadAll(body)
				body.Close() // Close immediately, not deferred in loop
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

		// Write body sections
		for _, bs := range options.BodySection {
			body, err := s.server.store.GetMessageBody(ctx, msg)
			if err != nil {
				log.Printf("IMAP: Failed to get message body for section: %v", err)
				continue
			}

			data, readErr := io.ReadAll(body)
			body.Close() // Close immediately after reading

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

	uidToSeq := make(map[uint32]uint32)
	seqToMsg := make(map[uint32]*storage.Message)
	for i, msg := range messages {
		seqNum := uint32(i + 1)
		seqToMsg[seqNum] = msg
		uidToSeq[msg.UID] = seqNum
	}

	// Determine which messages to update based on set type
	var toUpdate []uint32
	switch set := numSet.(type) {
	case imap.UIDSet:
		for uid := range uidToSeq {
			if set.Contains(imap.UID(uid)) {
				toUpdate = append(toUpdate, uidToSeq[uid])
			}
		}
	case imap.SeqSet:
		for seqNum := range seqToMsg {
			if set.Contains(seqNum) {
				toUpdate = append(toUpdate, seqNum)
			}
		}
	}

	// Update each message
	for _, seqNum := range toUpdate {
		msg := seqToMsg[seqNum]
		if msg == nil {
			continue
		}

		storageFlags := make([]storage.Flag, len(flags.Flags))
		for i, f := range flags.Flags {
			storageFlags[i] = storage.Flag(f)
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
				newFlags := make([]imap.Flag, len(updatedMsg.Flags))
				for i, f := range updatedMsg.Flags {
					newFlags[i] = imap.Flag(f)
				}
				respWriter.WriteFlags(newFlags)
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

	uidToSeq := make(map[uint32]uint32)
	for i, msg := range messages {
		uidToSeq[msg.UID] = uint32(i + 1)
	}

	for _, uid := range expunged {
		if seqNum, ok := uidToSeq[uid]; ok {
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

// Search searches for messages
func (s *Session) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	s.mu.RLock()
	selected := s.selected
	s.mu.RUnlock()

	if selected == nil {
		return nil, fmt.Errorf("no mailbox selected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Convert criteria to our format
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

	uids, err := s.server.store.SearchMessages(ctx, selected.ID, storageCriteria)
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

	uidToSeq := make(map[uint32]uint32)
	for i, msg := range messages {
		uidToSeq[msg.UID] = uint32(i + 1)
	}

	var seqNums []uint32
	for _, uid := range uids {
		if seq, ok := uidToSeq[uid]; ok {
			seqNums = append(seqNums, seq)
		}
	}

	return &imap.SearchData{
		All: imap.SeqSetNum(seqNums...),
	}, nil
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

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(strings.ToLower(line), "subject:") {
			env.Subject = strings.TrimSpace(line[8:])
		} else if strings.HasPrefix(strings.ToLower(line), "date:") {
			dateStr := strings.TrimSpace(line[5:])
			if t, err := time.Parse(time.RFC1123Z, dateStr); err == nil {
				env.Date = t
			}
		} else if strings.HasPrefix(strings.ToLower(line), "from:") {
			env.From = parseAddresses(strings.TrimSpace(line[5:]))
		} else if strings.HasPrefix(strings.ToLower(line), "to:") {
			env.To = parseAddresses(strings.TrimSpace(line[3:]))
		} else if strings.HasPrefix(strings.ToLower(line), "message-id:") {
			env.MessageID = strings.TrimSpace(line[11:])
		} else if line == "" || line == "\r" {
			break // End of headers
		}
	}

	return env
}

func parseAddresses(s string) []imap.Address {
	// Simple address parsing
	parts := strings.Split(s, ",")
	var addrs []imap.Address
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
