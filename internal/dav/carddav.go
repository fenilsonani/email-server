package dav

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// CardDAVBackend implements carddav.Backend
type CardDAVBackend struct {
	db *sql.DB
}

// NewCardDAVBackend creates a new CardDAV backend
func NewCardDAVBackend(db *sql.DB) (*CardDAVBackend, error) {
	if db == nil {
		return nil, ErrNilDB
	}
	return &CardDAVBackend{db: db}, nil
}

// AddressBook represents a CardDAV address book
type AddressBook struct {
	ID          int64
	UserID      int64
	UID         string
	Name        string
	Description string
	CTag        string
	IsDefault   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Contact represents a contact in an address book
type Contact struct {
	ID            int64
	AddressBookID int64
	UID           string
	ETag          string
	VCardData     string
	FullName      string
	GivenName     string
	FamilyName    string
	Nickname      string
	Emails        string // JSON array
	Phones        string // JSON array
	Organization  string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateAddressBook creates a new address book for a user
func (b *CardDAVBackend) CreateAddressBook(ctx context.Context, userID int64, name, description string) (*AddressBook, error) {
	uid, err := generateUID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate UID: %w", err)
	}
	ctag := generateCTag()

	result, err := b.db.ExecContext(ctx,
		`INSERT INTO addressbooks (user_id, uid, name, description, ctag)
		 VALUES (?, ?, ?, ?, ?)`,
		userID, uid, name, description, ctag,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create address book: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert ID: %w", err)
	}

	return &AddressBook{
		ID:          id,
		UserID:      userID,
		UID:         uid,
		Name:        name,
		Description: description,
		CTag:        ctag,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}, nil
}

// GetAddressBook retrieves an address book by UID
func (b *CardDAVBackend) GetAddressBook(ctx context.Context, uid string) (*AddressBook, error) {
	var ab AddressBook
	var description sql.NullString

	err := b.db.QueryRowContext(ctx,
		`SELECT id, user_id, uid, name, description, ctag, is_default, created_at, updated_at
		 FROM addressbooks WHERE uid = ?`,
		uid,
	).Scan(&ab.ID, &ab.UserID, &ab.UID, &ab.Name, &description, &ab.CTag,
		&ab.IsDefault, &ab.CreatedAt, &ab.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("address book not found: %s", uid)
		}
		return nil, err
	}

	ab.Description = description.String
	return &ab, nil
}

// ListAddressBooks returns all address books for a user
func (b *CardDAVBackend) ListAddressBooks(ctx context.Context, userID int64) ([]*AddressBook, error) {
	rows, err := b.db.QueryContext(ctx,
		`SELECT id, user_id, uid, name, description, ctag, is_default, created_at, updated_at
		 FROM addressbooks WHERE user_id = ? ORDER BY name`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var addressBooks []*AddressBook
	for rows.Next() {
		var ab AddressBook
		var description sql.NullString

		if err := rows.Scan(&ab.ID, &ab.UserID, &ab.UID, &ab.Name, &description, &ab.CTag,
			&ab.IsDefault, &ab.CreatedAt, &ab.UpdatedAt); err != nil {
			return nil, err
		}

		ab.Description = description.String
		addressBooks = append(addressBooks, &ab)
	}

	return addressBooks, rows.Err()
}

// UpdateAddressBook updates address book properties
func (b *CardDAVBackend) UpdateAddressBook(ctx context.Context, uid string, name, description string) error {
	ctag := generateCTag()

	result, err := b.db.ExecContext(ctx,
		`UPDATE addressbooks SET name = ?, description = ?, ctag = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE uid = ?`,
		name, description, ctag, uid,
	)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("address book not found: %s", uid)
	}

	return nil
}

// DeleteAddressBook removes an address book and all its contacts
func (b *CardDAVBackend) DeleteAddressBook(ctx context.Context, uid string) error {
	result, err := b.db.ExecContext(ctx, "DELETE FROM addressbooks WHERE uid = ?", uid)
	if err != nil {
		return err
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("address book not found: %s", uid)
	}

	return nil
}

// CreateContact adds a new contact to an address book
func (b *CardDAVBackend) CreateContact(ctx context.Context, addressBookUID string, contact *Contact) error {
	// Get address book ID
	var abID int64
	err := b.db.QueryRowContext(ctx, "SELECT id FROM addressbooks WHERE uid = ?", addressBookUID).Scan(&abID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("address book not found: %s", addressBookUID)
		}
		return fmt.Errorf("failed to query address book: %w", err)
	}

	contact.AddressBookID = abID
	etag, err := generateETag()
	if err != nil {
		return fmt.Errorf("failed to generate ETag: %w", err)
	}
	contact.ETag = etag

	_, err = b.db.ExecContext(ctx,
		`INSERT INTO contacts (addressbook_id, uid, etag, vcard_data, full_name, given_name, family_name, nickname, emails, phones, organization)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		abID, contact.UID, contact.ETag, contact.VCardData, contact.FullName, contact.GivenName,
		contact.FamilyName, contact.Nickname, contact.Emails, contact.Phones, contact.Organization,
	)
	if err != nil {
		return fmt.Errorf("failed to create contact: %w", err)
	}

	// Update address book ctag
	ctag := generateCTag()
	_, err = b.db.ExecContext(ctx, "UPDATE addressbooks SET ctag = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		ctag, abID)
	if err != nil {
		return fmt.Errorf("contact created but failed to update address book ctag: %w", err)
	}

	return nil
}

// GetContact retrieves a contact by UID
func (b *CardDAVBackend) GetContact(ctx context.Context, addressBookUID, contactUID string) (*Contact, error) {
	var contact Contact
	var nickname, emails, phones, organization sql.NullString

	err := b.db.QueryRowContext(ctx,
		`SELECT c.id, c.addressbook_id, c.uid, c.etag, c.vcard_data, c.full_name, c.given_name,
		        c.family_name, c.nickname, c.emails, c.phones, c.organization, c.created_at, c.updated_at
		 FROM contacts c
		 JOIN addressbooks ab ON c.addressbook_id = ab.id
		 WHERE ab.uid = ? AND c.uid = ?`,
		addressBookUID, contactUID,
	).Scan(&contact.ID, &contact.AddressBookID, &contact.UID, &contact.ETag, &contact.VCardData,
		&contact.FullName, &contact.GivenName, &contact.FamilyName, &nickname, &emails,
		&phones, &organization, &contact.CreatedAt, &contact.UpdatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("contact not found: %s", contactUID)
		}
		return nil, err
	}

	contact.Nickname = nickname.String
	contact.Emails = emails.String
	contact.Phones = phones.String
	contact.Organization = organization.String
	return &contact, nil
}

// ListContacts returns all contacts in an address book
func (b *CardDAVBackend) ListContacts(ctx context.Context, addressBookUID string) ([]*Contact, error) {
	rows, err := b.db.QueryContext(ctx,
		`SELECT c.id, c.addressbook_id, c.uid, c.etag, c.vcard_data, c.full_name, c.given_name,
		        c.family_name, c.nickname, c.emails, c.phones, c.organization, c.created_at, c.updated_at
		 FROM contacts c
		 JOIN addressbooks ab ON c.addressbook_id = ab.id
		 WHERE ab.uid = ?
		 ORDER BY c.full_name`,
		addressBookUID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []*Contact
	for rows.Next() {
		var contact Contact
		var nickname, emails, phones, organization sql.NullString

		if err := rows.Scan(&contact.ID, &contact.AddressBookID, &contact.UID, &contact.ETag, &contact.VCardData,
			&contact.FullName, &contact.GivenName, &contact.FamilyName, &nickname, &emails,
			&phones, &organization, &contact.CreatedAt, &contact.UpdatedAt); err != nil {
			return nil, err
		}

		contact.Nickname = nickname.String
		contact.Emails = emails.String
		contact.Phones = phones.String
		contact.Organization = organization.String
		contacts = append(contacts, &contact)
	}

	return contacts, rows.Err()
}

// SearchContacts searches contacts by name or email
func (b *CardDAVBackend) SearchContacts(ctx context.Context, addressBookUID, query string) ([]*Contact, error) {
	// Sanitize query to prevent SQL injection
	// Escape SQL LIKE special characters using ! as escape char
	query = strings.ReplaceAll(query, "!", "!!")
	query = strings.ReplaceAll(query, "%", "!%")
	query = strings.ReplaceAll(query, "_", "!_")

	searchPattern := "%" + query + "%"

	rows, err := b.db.QueryContext(ctx,
		`SELECT c.id, c.addressbook_id, c.uid, c.etag, c.vcard_data, c.full_name, c.given_name,
		        c.family_name, c.nickname, c.emails, c.phones, c.organization, c.created_at, c.updated_at
		 FROM contacts c
		 JOIN addressbooks ab ON c.addressbook_id = ab.id
		 WHERE ab.uid = ? AND (c.full_name LIKE ? ESCAPE '!' OR c.emails LIKE ? ESCAPE '!')
		 ORDER BY c.full_name`,
		addressBookUID, searchPattern, searchPattern,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contacts []*Contact
	for rows.Next() {
		var contact Contact
		var nickname, emails, phones, organization sql.NullString

		if err := rows.Scan(&contact.ID, &contact.AddressBookID, &contact.UID, &contact.ETag, &contact.VCardData,
			&contact.FullName, &contact.GivenName, &contact.FamilyName, &nickname, &emails,
			&phones, &organization, &contact.CreatedAt, &contact.UpdatedAt); err != nil {
			return nil, err
		}

		contact.Nickname = nickname.String
		contact.Emails = emails.String
		contact.Phones = phones.String
		contact.Organization = organization.String
		contacts = append(contacts, &contact)
	}

	return contacts, rows.Err()
}

// UpdateContact updates an existing contact
func (b *CardDAVBackend) UpdateContact(ctx context.Context, addressBookUID string, contact *Contact) error {
	etag, err := generateETag()
	if err != nil {
		return fmt.Errorf("failed to generate ETag: %w", err)
	}
	contact.ETag = etag

	result, err := b.db.ExecContext(ctx,
		`UPDATE contacts SET etag = ?, vcard_data = ?, full_name = ?, given_name = ?, family_name = ?,
		        nickname = ?, emails = ?, phones = ?, organization = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE uid = ? AND addressbook_id = (SELECT id FROM addressbooks WHERE uid = ?)`,
		contact.ETag, contact.VCardData, contact.FullName, contact.GivenName, contact.FamilyName,
		contact.Nickname, contact.Emails, contact.Phones, contact.Organization, contact.UID, addressBookUID,
	)
	if err != nil {
		return fmt.Errorf("failed to update contact: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("contact not found: %s", contact.UID)
	}

	// Update address book ctag
	ctag := generateCTag()
	_, err = b.db.ExecContext(ctx,
		"UPDATE addressbooks SET ctag = ?, updated_at = CURRENT_TIMESTAMP WHERE uid = ?",
		ctag, addressBookUID)
	if err != nil {
		return fmt.Errorf("contact updated but failed to update address book ctag: %w", err)
	}

	return nil
}

// DeleteContact removes a contact from an address book
func (b *CardDAVBackend) DeleteContact(ctx context.Context, addressBookUID, contactUID string) error {
	result, err := b.db.ExecContext(ctx,
		`DELETE FROM contacts
		 WHERE uid = ? AND addressbook_id = (SELECT id FROM addressbooks WHERE uid = ?)`,
		contactUID, addressBookUID,
	)
	if err != nil {
		return fmt.Errorf("failed to delete contact: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("contact not found: %s", contactUID)
	}

	// Update address book ctag
	ctag := generateCTag()
	_, err = b.db.ExecContext(ctx,
		"UPDATE addressbooks SET ctag = ?, updated_at = CURRENT_TIMESTAMP WHERE uid = ?",
		ctag, addressBookUID)
	if err != nil {
		return fmt.Errorf("contact deleted but failed to update address book ctag: %w", err)
	}

	return nil
}
