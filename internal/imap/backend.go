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
// NOTE: IDLE updates are disabled due to a crash in go-imap v1.2.1
// The crash occurs in responses/select.go when sending unilateral responses
// Apple Mail will still receive emails via its periodic polling (every 1-5 minutes)
// TODO: Upgrade to go-imap/v2 for proper IDLE support
func (b *Backend) NotifyMailboxUpdate(username, mailbox string) {
	log.Printf("IDLE: New message for %s/%s (IDLE disabled, will sync on next poll)", username, mailbox)
}
