package imap

import (
	"context"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
	goauth "github.com/fenilsonani/email-server/internal/auth"
	"github.com/fenilsonani/email-server/internal/storage"
)

// User implements the backend.User interface
type User struct {
	backend *Backend
	user    *goauth.User
}

// Username returns the user's email address
func (u *User) Username() string {
	return u.user.Email
}

// ListMailboxes returns all mailboxes for the user
func (u *User) ListMailboxes(subscribed bool) ([]backend.Mailbox, error) {
	ctx := context.Background()

	mailboxes, err := u.backend.store.ListMailboxes(ctx, u.user.ID)
	if err != nil {
		return nil, err
	}

	result := make([]backend.Mailbox, 0, len(mailboxes))
	for _, mb := range mailboxes {
		if subscribed && !mb.Subscribed {
			continue
		}
		result = append(result, &Mailbox{
			user:    u,
			mailbox: mb,
		})
	}

	return result, nil
}

// GetMailbox returns a specific mailbox by name
func (u *User) GetMailbox(name string) (backend.Mailbox, error) {
	ctx := context.Background()

	mb, err := u.backend.store.GetMailbox(ctx, u.user.ID, name)
	if err != nil {
		return nil, backend.ErrNoSuchMailbox
	}

	return &Mailbox{
		user:    u,
		mailbox: mb,
	}, nil
}

// CreateMailbox creates a new mailbox
func (u *User) CreateMailbox(name string) error {
	ctx := context.Background()

	_, err := u.backend.store.CreateMailbox(ctx, u.user.ID, name, "")
	return err
}

// DeleteMailbox removes a mailbox
func (u *User) DeleteMailbox(name string) error {
	ctx := context.Background()

	// Don't allow deleting INBOX
	if name == "INBOX" {
		return backend.ErrNoSuchMailbox
	}

	return u.backend.store.DeleteMailbox(ctx, u.user.ID, name)
}

// RenameMailbox renames a mailbox
func (u *User) RenameMailbox(existingName, newName string) error {
	ctx := context.Background()

	// Don't allow renaming INBOX
	if existingName == "INBOX" {
		return backend.ErrNoSuchMailbox
	}

	return u.backend.store.RenameMailbox(ctx, u.user.ID, existingName, newName)
}

// Logout is called when the user logs out
func (u *User) Logout() error {
	// Cleanup if needed
	return nil
}

// convertFlags converts our storage flags to go-imap flags
func convertFlags(flags []storage.Flag) []string {
	result := make([]string, len(flags))
	for i, f := range flags {
		result[i] = string(f)
	}
	return result
}

// parseFlags converts go-imap flags to our storage flags
func parseFlags(flags []string) []storage.Flag {
	result := make([]storage.Flag, len(flags))
	for i, f := range flags {
		result[i] = storage.Flag(f)
	}
	return result
}

// flagsContain checks if a flag exists in a slice
func flagsContain(flags []string, flag string) bool {
	for _, f := range flags {
		if f == flag {
			return true
		}
	}
	return false
}

// convertMailboxInfo converts our mailbox to go-imap MailboxInfo
func convertMailboxInfo(mb *storage.Mailbox) *imap.MailboxInfo {
	info := &imap.MailboxInfo{
		Name:       mb.Name,
		Attributes: []string{}, // Initialize to empty slice to avoid nil
	}

	if mb.SpecialUse != "" {
		info.Attributes = append(info.Attributes, string(mb.SpecialUse))
	}

	return info
}
