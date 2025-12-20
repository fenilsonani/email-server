package imap

import (
	"context"
	"log"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	"github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/storage/maildir"
)

// Backend implements the go-imap backend.Backend interface
type Backend struct {
	authenticator *auth.Authenticator
	store         *maildir.Store
	updates       *UpdateHub
}

// NewBackend creates a new IMAP backend
func NewBackend(authenticator *auth.Authenticator, store *maildir.Store) *Backend {
	return &Backend{
		authenticator: authenticator,
		store:         store,
		updates:       NewUpdateHub(),
	}
}

// Login authenticates a user and returns their User implementation
func (b *Backend) Login(connInfo *imap.ConnInfo, username, password string) (backend.User, error) {
	ctx := context.Background()
	log.Printf("IMAP: Login attempt for user: %s", username)

	user, err := b.authenticator.Authenticate(ctx, username, password)
	if err != nil {
		log.Printf("IMAP: Login failed for user: %s", username)
		return nil, backend.ErrInvalidCredentials
	}

	log.Printf("IMAP: Login successful for user: %s (email: %s)", username, user.Email)
	return &User{
		backend: b,
		user:    user,
	}, nil
}

// Updates returns the update channel for IDLE support
func (b *Backend) Updates() <-chan backend.Update {
	return b.updates.Updates()
}

// NotifyUpdate sends an update to listening clients
func (b *Backend) NotifyUpdate(update backend.Update) {
	b.updates.Notify(update)
}

// NotifyMailboxUpdate notifies IDLE clients about a mailbox change (new message)
func (b *Backend) NotifyMailboxUpdate(username, mailbox string) {
	ctx := context.Background()
	log.Printf("IDLE: NotifyMailboxUpdate called for %s/%s", username, mailbox)

	// Look up user to get their mailbox stats
	user, err := b.authenticator.LookupUser(ctx, username)
	if err != nil {
		// Fallback to simple update without stats
		b.updates.Notify(&backend.MailboxUpdate{
			Update: backend.NewUpdate(username, mailbox),
		})
		return
	}

	// Get mailbox to find its ID
	mb, err := b.store.GetMailbox(ctx, user.ID, mailbox)
	if err != nil {
		b.updates.Notify(&backend.MailboxUpdate{
			Update: backend.NewUpdate(username, mailbox),
		})
		return
	}

	// Get current mailbox stats for accurate message count
	stats, err := b.store.GetMailboxStats(ctx, mb.ID)
	if err != nil {
		b.updates.Notify(&backend.MailboxUpdate{
			Update: backend.NewUpdate(username, mailbox),
		})
		return
	}

	// Send update with full mailbox status - this triggers IDLE notification
	status := imap.NewMailboxStatus(mailbox, []imap.StatusItem{imap.StatusMessages, imap.StatusRecent, imap.StatusUnseen})
	status.Messages = uint32(stats.Messages)
	status.Recent = uint32(stats.Recent)
	status.Unseen = uint32(stats.Unseen)

	b.updates.Notify(&backend.MailboxUpdate{
		Update:        backend.NewUpdate(username, mailbox),
		MailboxStatus: status,
	})
}
