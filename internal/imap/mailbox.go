package imap

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"github.com/fenilsonani/email-server/internal/storage"
)

// Mailbox implements the backend.Mailbox interface
type Mailbox struct {
	user    *User
	mailbox *storage.Mailbox
}

// Name returns the mailbox name
func (m *Mailbox) Name() string {
	return m.mailbox.Name
}

// Info returns the mailbox info
func (m *Mailbox) Info() (*imap.MailboxInfo, error) {
	return convertMailboxInfo(m.mailbox), nil
}

// Status returns the mailbox status
func (m *Mailbox) Status(items []imap.StatusItem) (*imap.MailboxStatus, error) {
	ctx := context.Background()

	stats, err := m.user.backend.store.GetMailboxStats(ctx, m.mailbox.ID)
	if err != nil {
		return nil, err
	}

	status := imap.NewMailboxStatus(m.mailbox.Name, items)
	status.UidValidity = stats.UIDValidity
	status.UidNext = stats.UIDNext

	for _, item := range items {
		switch item {
		case imap.StatusMessages:
			status.Messages = uint32(stats.Messages)
		case imap.StatusRecent:
			status.Recent = uint32(stats.Recent)
		case imap.StatusUnseen:
			status.Unseen = uint32(stats.Unseen)
		case imap.StatusUidNext:
			status.UidNext = stats.UIDNext
		case imap.StatusUidValidity:
			status.UidValidity = stats.UIDValidity
		}
	}

	return status, nil
}

// SetSubscribed updates the subscription status
func (m *Mailbox) SetSubscribed(subscribed bool) error {
	ctx := context.Background()
	return m.user.backend.store.SubscribeMailbox(ctx, m.user.user.ID, m.mailbox.Name, subscribed)
}

// Check requests a checkpoint of the mailbox
func (m *Mailbox) Check() error {
	// Not implemented - maildir doesn't need checkpointing
	return nil
}

// ListMessages returns messages matching the given sequence set
func (m *Mailbox) ListMessages(uid bool, seqSet *imap.SeqSet, items []imap.FetchItem, ch chan<- *imap.Message) error {
	defer close(ch)

	ctx := context.Background()

	// Get all messages for this mailbox
	messages, err := m.user.backend.store.ListMessages(ctx, m.mailbox.ID, 0, 0)
	if err != nil {
		return err
	}

	// Build sequence number to UID mapping
	seqToUID := make(map[uint32]uint32)
	uidToSeq := make(map[uint32]uint32)
	for i, msg := range messages {
		seqNum := uint32(i + 1)
		seqToUID[seqNum] = msg.UID
		uidToSeq[msg.UID] = seqNum
	}

	for i, msg := range messages {
		seqNum := uint32(i + 1)

		// Check if message matches sequence set
		var matches bool
		if uid {
			matches = seqSet.Contains(msg.UID)
		} else {
			matches = seqSet.Contains(seqNum)
		}

		if !matches {
			continue
		}

		// Build IMAP message
		imapMsg := imap.NewMessage(seqNum, items)
		imapMsg.Uid = msg.UID

		for _, item := range items {
			switch item {
			case imap.FetchEnvelope:
				imapMsg.Envelope = m.buildEnvelope(msg)
			case imap.FetchFlags:
				imapMsg.Flags = convertFlags(msg.Flags)
			case imap.FetchInternalDate:
				imapMsg.InternalDate = msg.InternalDate
			case imap.FetchRFC822Size:
				imapMsg.Size = uint32(msg.Size)
			case imap.FetchUid:
				imapMsg.Uid = msg.UID
			default:
				// Handle BODY and BODY.PEEK
				section, err := imap.ParseBodySectionName(item)
				if err != nil {
					continue
				}

				body, err := m.user.backend.store.GetMessageBody(ctx, msg)
				if err != nil {
					continue
				}
				defer body.Close()

				content, _ := io.ReadAll(body)
				literal := imap.Literal(newBytesLiteral(content))
				imapMsg.Body[section] = literal

				// Mark as seen if not PEEK
				if !section.Peek {
					m.user.backend.store.UpdateFlags(ctx, m.mailbox.ID, msg.UID,
						[]storage.Flag{storage.FlagSeen}, true)
				}
			}
		}

		ch <- imapMsg
	}

	return nil
}

// SearchMessages searches for messages matching the given criteria
func (m *Mailbox) SearchMessages(uid bool, criteria *imap.SearchCriteria) ([]uint32, error) {
	ctx := context.Background()

	// Convert IMAP criteria to our criteria
	storageCriteria := &storage.SearchCriteria{}

	if !criteria.Since.IsZero() {
		storageCriteria.Since = &criteria.Since
	}
	if !criteria.Before.IsZero() {
		storageCriteria.Before = &criteria.Before
	}
	if criteria.Larger > 0 {
		storageCriteria.Larger = int64(criteria.Larger)
	}
	if criteria.Smaller > 0 {
		storageCriteria.Smaller = int64(criteria.Smaller)
	}

	// Handle flags
	for _, flag := range criteria.WithFlags {
		storageCriteria.Flags = append(storageCriteria.Flags, storage.Flag(flag))
	}
	for _, flag := range criteria.WithoutFlags {
		storageCriteria.NotFlags = append(storageCriteria.NotFlags, storage.Flag(flag))
	}

	// Header searches - criteria.Header is a textproto.MIMEHeader (map[string][]string)
	if from := criteria.Header.Get("From"); from != "" {
		storageCriteria.From = from
	}
	if to := criteria.Header.Get("To"); to != "" {
		storageCriteria.To = to
	}
	if subject := criteria.Header.Get("Subject"); subject != "" {
		storageCriteria.Subject = subject
	}

	uids, err := m.user.backend.store.SearchMessages(ctx, m.mailbox.ID, storageCriteria)
	if err != nil {
		return nil, err
	}

	if uid {
		return uids, nil
	}

	// Convert UIDs to sequence numbers
	messages, err := m.user.backend.store.ListMessages(ctx, m.mailbox.ID, 0, 0)
	if err != nil {
		return nil, err
	}

	uidToSeq := make(map[uint32]uint32)
	for i, msg := range messages {
		uidToSeq[msg.UID] = uint32(i + 1)
	}

	result := make([]uint32, 0, len(uids))
	for _, u := range uids {
		if seq, ok := uidToSeq[u]; ok {
			result = append(result, seq)
		}
	}

	return result, nil
}

// CreateMessage appends a message to the mailbox
func (m *Mailbox) CreateMessage(flags []string, date time.Time, body imap.Literal) error {
	ctx := context.Background()

	storageFlags := parseFlags(flags)

	_, err := m.user.backend.store.AppendMessage(ctx, m.mailbox.ID, storageFlags, date, body)
	if err != nil {
		return err
	}

	// Notify IDLE listeners
	m.user.backend.NotifyUpdate(&backend.MailboxUpdate{
		Update:        backend.NewUpdate(m.user.Username(), m.mailbox.Name),
		MailboxStatus: nil,
	})

	return nil
}

// UpdateMessagesFlags updates flags on messages
func (m *Mailbox) UpdateMessagesFlags(uid bool, seqSet *imap.SeqSet, op imap.FlagsOp, flags []string) error {
	ctx := context.Background()

	messages, err := m.user.backend.store.ListMessages(ctx, m.mailbox.ID, 0, 0)
	if err != nil {
		return err
	}

	storageFlags := parseFlags(flags)

	for i, msg := range messages {
		seqNum := uint32(i + 1)

		var matches bool
		if uid {
			matches = seqSet.Contains(msg.UID)
		} else {
			matches = seqSet.Contains(seqNum)
		}

		if !matches {
			continue
		}

		switch op {
		case imap.SetFlags:
			err = m.user.backend.store.SetFlags(ctx, m.mailbox.ID, msg.UID, storageFlags)
		case imap.AddFlags:
			err = m.user.backend.store.UpdateFlags(ctx, m.mailbox.ID, msg.UID, storageFlags, true)
		case imap.RemoveFlags:
			err = m.user.backend.store.UpdateFlags(ctx, m.mailbox.ID, msg.UID, storageFlags, false)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// CopyMessages copies messages to another mailbox
func (m *Mailbox) CopyMessages(uid bool, seqSet *imap.SeqSet, destName string) error {
	ctx := context.Background()

	// Get destination mailbox
	destMb, err := m.user.backend.store.GetMailbox(ctx, m.user.user.ID, destName)
	if err != nil {
		return backend.ErrNoSuchMailbox
	}

	messages, err := m.user.backend.store.ListMessages(ctx, m.mailbox.ID, 0, 0)
	if err != nil {
		return err
	}

	for i, msg := range messages {
		seqNum := uint32(i + 1)

		var matches bool
		if uid {
			matches = seqSet.Contains(msg.UID)
		} else {
			matches = seqSet.Contains(seqNum)
		}

		if !matches {
			continue
		}

		_, err = m.user.backend.store.CopyMessage(ctx, m.mailbox.ID, msg.UID, destMb.ID)
		if err != nil {
			return err
		}
	}

	return nil
}

// Expunge permanently removes messages marked for deletion
func (m *Mailbox) Expunge() error {
	ctx := context.Background()

	expunged, err := m.user.backend.store.ExpungeMailbox(ctx, m.mailbox.ID)
	if err != nil {
		return err
	}

	// Notify about expunged messages
	if len(expunged) > 0 {
		m.user.backend.NotifyUpdate(&backend.ExpungeUpdate{
			Update: backend.NewUpdate(m.user.Username(), m.mailbox.Name),
			SeqNum: expunged[0], // Simplified - should send multiple updates
		})
	}

	return nil
}

// buildEnvelope creates an IMAP envelope from message metadata
func (m *Mailbox) buildEnvelope(msg *storage.Message) *imap.Envelope {
	env := &imap.Envelope{
		Date:      msg.InternalDate,
		Subject:   msg.Subject,
		MessageId: msg.MessageID,
		InReplyTo: msg.InReplyTo,
	}

	if msg.From != "" {
		env.From = []*imap.Address{parseAddress(msg.From)}
	}

	for _, to := range msg.To {
		env.To = append(env.To, parseAddress(to))
	}

	return env
}

// parseAddress parses an email address into an imap.Address
func parseAddress(addr string) *imap.Address {
	// Simple parsing - in production you'd use mail.ParseAddress
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) == 2 {
		return &imap.Address{
			MailboxName: parts[0],
			HostName:    parts[1],
		}
	}
	return &imap.Address{MailboxName: addr}
}

// bytesLiteral implements imap.Literal for a byte slice
type bytesLiteral struct {
	data []byte
	pos  int
}

func newBytesLiteral(data []byte) *bytesLiteral {
	return &bytesLiteral{data: data}
}

func (b *bytesLiteral) Len() int {
	return len(b.data)
}

func (b *bytesLiteral) Read(p []byte) (n int, err error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n = copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}
