package imap

import (
	"context"

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
	user, err := b.authenticator.Authenticate(ctx, username, password)
	if err != nil {
		return nil, backend.ErrInvalidCredentials
	}

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
