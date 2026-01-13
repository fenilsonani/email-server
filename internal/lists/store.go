package lists

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Common errors
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrNotAuthorized = errors.New("not authorized")
	ErrListFull      = errors.New("list has reached maximum members")
)

// Store handles all mailing list database operations
type Store struct {
	db *sql.DB
}

// NewStore creates a new lists store
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// =============================================================================
// Mailing List CRUD
// =============================================================================

// CreateList creates a new mailing list
func (s *Store) CreateList(ctx context.Context, list *MailingList) error {
	list.ListAddress = strings.ToLower(strings.TrimSpace(list.ListAddress))
	list.LocalPart = strings.ToLower(strings.TrimSpace(list.LocalPart))

	if list.ListAddress == "" || list.LocalPart == "" || list.Name == "" {
		return ErrInvalidInput
	}

	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO mailing_lists (
			domain_id, local_part, list_address, name, description,
			list_type, posting_policy, moderation_enabled, require_subject_prefix, subject_prefix,
			reply_to_list, reply_to_sender, archive_enabled, archive_public,
			allow_subscribe, require_confirm, max_message_size, max_members,
			is_active, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		list.DomainID, list.LocalPart, list.ListAddress, list.Name, list.Description,
		list.ListType, list.PostingPolicy, list.ModerationEnabled, list.RequireSubjectPrefix, list.SubjectPrefix,
		list.ReplyToList, list.ReplyToSender, list.ArchiveEnabled, list.ArchivePublic,
		list.AllowSubscribe, list.RequireConfirm, list.MaxMessageSize, list.MaxMembers,
		list.IsActive, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "duplicate key") {
			return ErrAlreadyExists
		}
		return fmt.Errorf("failed to create list: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get list id: %w", err)
	}
	list.ID = id
	list.CreatedAt = now
	list.UpdatedAt = now

	return nil
}

// GetList retrieves a mailing list by ID
func (s *Store) GetList(ctx context.Context, id int64) (*MailingList, error) {
	list := &MailingList{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, domain_id, local_part, list_address, name, description,
			list_type, posting_policy, moderation_enabled, require_subject_prefix, subject_prefix,
			reply_to_list, reply_to_sender, archive_enabled, archive_public,
			allow_subscribe, require_confirm, max_message_size, max_members,
			is_active, created_at, updated_at
		FROM mailing_lists WHERE id = ?
	`, id).Scan(
		&list.ID, &list.DomainID, &list.LocalPart, &list.ListAddress, &list.Name, &list.Description,
		&list.ListType, &list.PostingPolicy, &list.ModerationEnabled, &list.RequireSubjectPrefix, &list.SubjectPrefix,
		&list.ReplyToList, &list.ReplyToSender, &list.ArchiveEnabled, &list.ArchivePublic,
		&list.AllowSubscribe, &list.RequireConfirm, &list.MaxMessageSize, &list.MaxMembers,
		&list.IsActive, &list.CreatedAt, &list.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get list: %w", err)
	}
	return list, nil
}

// GetListByAddress retrieves a mailing list by its address
func (s *Store) GetListByAddress(ctx context.Context, address string) (*MailingList, error) {
	address = strings.ToLower(strings.TrimSpace(address))
	list := &MailingList{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, domain_id, local_part, list_address, name, description,
			list_type, posting_policy, moderation_enabled, require_subject_prefix, subject_prefix,
			reply_to_list, reply_to_sender, archive_enabled, archive_public,
			allow_subscribe, require_confirm, max_message_size, max_members,
			is_active, created_at, updated_at
		FROM mailing_lists WHERE list_address = ?
	`, address).Scan(
		&list.ID, &list.DomainID, &list.LocalPart, &list.ListAddress, &list.Name, &list.Description,
		&list.ListType, &list.PostingPolicy, &list.ModerationEnabled, &list.RequireSubjectPrefix, &list.SubjectPrefix,
		&list.ReplyToList, &list.ReplyToSender, &list.ArchiveEnabled, &list.ArchivePublic,
		&list.AllowSubscribe, &list.RequireConfirm, &list.MaxMessageSize, &list.MaxMembers,
		&list.IsActive, &list.CreatedAt, &list.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get list by address: %w", err)
	}
	return list, nil
}

// UpdateList updates a mailing list
func (s *Store) UpdateList(ctx context.Context, list *MailingList) error {
	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		UPDATE mailing_lists SET
			name = ?, description = ?, list_type = ?, posting_policy = ?,
			moderation_enabled = ?, require_subject_prefix = ?, subject_prefix = ?,
			reply_to_list = ?, reply_to_sender = ?, archive_enabled = ?, archive_public = ?,
			allow_subscribe = ?, require_confirm = ?, max_message_size = ?, max_members = ?,
			is_active = ?, updated_at = ?
		WHERE id = ?
	`,
		list.Name, list.Description, list.ListType, list.PostingPolicy,
		list.ModerationEnabled, list.RequireSubjectPrefix, list.SubjectPrefix,
		list.ReplyToList, list.ReplyToSender, list.ArchiveEnabled, list.ArchivePublic,
		list.AllowSubscribe, list.RequireConfirm, list.MaxMessageSize, list.MaxMembers,
		list.IsActive, now, list.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update list: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check update result: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}

	list.UpdatedAt = now
	return nil
}

// DeleteList deletes a mailing list
func (s *Store) DeleteList(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM mailing_lists WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to delete list: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check delete result: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// ListLists retrieves all mailing lists for a domain
func (s *Store) ListLists(ctx context.Context, domainID int64) ([]*MailingList, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, domain_id, local_part, list_address, name, description,
			list_type, posting_policy, moderation_enabled, require_subject_prefix, subject_prefix,
			reply_to_list, reply_to_sender, archive_enabled, archive_public,
			allow_subscribe, require_confirm, max_message_size, max_members,
			is_active, created_at, updated_at
		FROM mailing_lists WHERE domain_id = ? ORDER BY name
	`, domainID)
	if err != nil {
		return nil, fmt.Errorf("failed to list lists: %w", err)
	}
	defer rows.Close()

	var lists []*MailingList
	for rows.Next() {
		list := &MailingList{}
		if err := rows.Scan(
			&list.ID, &list.DomainID, &list.LocalPart, &list.ListAddress, &list.Name, &list.Description,
			&list.ListType, &list.PostingPolicy, &list.ModerationEnabled, &list.RequireSubjectPrefix, &list.SubjectPrefix,
			&list.ReplyToList, &list.ReplyToSender, &list.ArchiveEnabled, &list.ArchivePublic,
			&list.AllowSubscribe, &list.RequireConfirm, &list.MaxMessageSize, &list.MaxMembers,
			&list.IsActive, &list.CreatedAt, &list.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan list: %w", err)
		}
		lists = append(lists, list)
	}
	return lists, rows.Err()
}

// ListAllLists retrieves all mailing lists across all domains
func (s *Store) ListAllLists(ctx context.Context) ([]*MailingList, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, domain_id, local_part, list_address, name, description,
			list_type, posting_policy, moderation_enabled, require_subject_prefix, subject_prefix,
			reply_to_list, reply_to_sender, archive_enabled, archive_public,
			allow_subscribe, require_confirm, max_message_size, max_members,
			is_active, created_at, updated_at
		FROM mailing_lists ORDER BY list_address
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list all lists: %w", err)
	}
	defer rows.Close()

	var lists []*MailingList
	for rows.Next() {
		list := &MailingList{}
		if err := rows.Scan(
			&list.ID, &list.DomainID, &list.LocalPart, &list.ListAddress, &list.Name, &list.Description,
			&list.ListType, &list.PostingPolicy, &list.ModerationEnabled, &list.RequireSubjectPrefix, &list.SubjectPrefix,
			&list.ReplyToList, &list.ReplyToSender, &list.ArchiveEnabled, &list.ArchivePublic,
			&list.AllowSubscribe, &list.RequireConfirm, &list.MaxMessageSize, &list.MaxMembers,
			&list.IsActive, &list.CreatedAt, &list.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan list: %w", err)
		}
		lists = append(lists, list)
	}
	return lists, rows.Err()
}

// IsListAddress checks if an address is a mailing list
func (s *Store) IsListAddress(ctx context.Context, address string) (bool, error) {
	address = strings.ToLower(strings.TrimSpace(address))
	var exists int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM mailing_lists WHERE list_address = ? AND is_active = TRUE`,
		address,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check list address: %w", err)
	}
	return true, nil
}

// =============================================================================
// Member Management
// =============================================================================

// AddMember adds a member to a mailing list
func (s *Store) AddMember(ctx context.Context, member *ListMember) error {
	member.Email = strings.ToLower(strings.TrimSpace(member.Email))
	if member.Email == "" {
		return ErrInvalidInput
	}

	// Check member limit
	count, err := s.CountMembers(ctx, member.ListID)
	if err != nil {
		return err
	}

	list, err := s.GetList(ctx, member.ListID)
	if err != nil {
		return err
	}
	if count >= list.MaxMembers {
		return ErrListFull
	}

	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO list_members (list_id, email, name, role, delivery_mode, is_confirmed, confirm_token, confirm_expires, subscribed_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		member.ListID, member.Email, member.Name, member.Role, member.DeliveryMode,
		member.IsConfirmed, member.ConfirmToken, member.ConfirmExpires, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "duplicate key") {
			return ErrAlreadyExists
		}
		return fmt.Errorf("failed to add member: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get member id: %w", err)
	}
	member.ID = id
	member.SubscribedAt = now
	member.UpdatedAt = now

	return nil
}

// GetMember retrieves a member by list ID and email
func (s *Store) GetMember(ctx context.Context, listID int64, email string) (*ListMember, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	member := &ListMember{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, list_id, email, name, role, delivery_mode, is_confirmed, confirm_token, confirm_expires, subscribed_at, updated_at
		FROM list_members WHERE list_id = ? AND email = ?
	`, listID, email).Scan(
		&member.ID, &member.ListID, &member.Email, &member.Name, &member.Role,
		&member.DeliveryMode, &member.IsConfirmed, &member.ConfirmToken, &member.ConfirmExpires,
		&member.SubscribedAt, &member.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get member: %w", err)
	}
	return member, nil
}

// UpdateMember updates a list member
func (s *Store) UpdateMember(ctx context.Context, member *ListMember) error {
	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		UPDATE list_members SET name = ?, role = ?, delivery_mode = ?, is_confirmed = ?, updated_at = ?
		WHERE id = ?
	`, member.Name, member.Role, member.DeliveryMode, member.IsConfirmed, now, member.ID)
	if err != nil {
		return fmt.Errorf("failed to update member: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check update result: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}

	member.UpdatedAt = now
	return nil
}

// RemoveMember removes a member from a mailing list
func (s *Store) RemoveMember(ctx context.Context, listID int64, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM list_members WHERE list_id = ? AND email = ?`,
		listID, email,
	)
	if err != nil {
		return fmt.Errorf("failed to remove member: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check delete result: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// ListMembers retrieves all members of a mailing list
func (s *Store) ListMembers(ctx context.Context, listID int64) ([]*ListMember, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, list_id, email, name, role, delivery_mode, is_confirmed, confirm_token, confirm_expires, subscribed_at, updated_at
		FROM list_members WHERE list_id = ? ORDER BY role, email
	`, listID)
	if err != nil {
		return nil, fmt.Errorf("failed to list members: %w", err)
	}
	defer rows.Close()

	var members []*ListMember
	for rows.Next() {
		member := &ListMember{}
		if err := rows.Scan(
			&member.ID, &member.ListID, &member.Email, &member.Name, &member.Role,
			&member.DeliveryMode, &member.IsConfirmed, &member.ConfirmToken, &member.ConfirmExpires,
			&member.SubscribedAt, &member.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan member: %w", err)
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

// GetConfirmedMembers retrieves confirmed members for delivery
func (s *Store) GetConfirmedMembers(ctx context.Context, listID int64) ([]*ListMember, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, list_id, email, name, role, delivery_mode, is_confirmed, confirm_token, confirm_expires, subscribed_at, updated_at
		FROM list_members WHERE list_id = ? AND is_confirmed = TRUE AND delivery_mode != 'nomail'
	`, listID)
	if err != nil {
		return nil, fmt.Errorf("failed to get confirmed members: %w", err)
	}
	defer rows.Close()

	var members []*ListMember
	for rows.Next() {
		member := &ListMember{}
		if err := rows.Scan(
			&member.ID, &member.ListID, &member.Email, &member.Name, &member.Role,
			&member.DeliveryMode, &member.IsConfirmed, &member.ConfirmToken, &member.ConfirmExpires,
			&member.SubscribedAt, &member.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan member: %w", err)
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

// GetMemberByToken retrieves a member by confirmation token
func (s *Store) GetMemberByToken(ctx context.Context, token string) (*ListMember, error) {
	member := &ListMember{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, list_id, email, name, role, delivery_mode, is_confirmed, confirm_token, confirm_expires, subscribed_at, updated_at
		FROM list_members WHERE confirm_token = ?
	`, token).Scan(
		&member.ID, &member.ListID, &member.Email, &member.Name, &member.Role,
		&member.DeliveryMode, &member.IsConfirmed, &member.ConfirmToken, &member.ConfirmExpires,
		&member.SubscribedAt, &member.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get member by token: %w", err)
	}
	return member, nil
}

// ConfirmMember confirms a member's subscription
func (s *Store) ConfirmMember(ctx context.Context, listID int64, email string) error {
	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		UPDATE list_members SET is_confirmed = TRUE, confirm_token = NULL, confirm_expires = NULL, updated_at = ?
		WHERE list_id = ? AND email = ?
	`, now, listID, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return fmt.Errorf("failed to confirm member: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check confirm result: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// IsMember checks if an email is a member of a list
func (s *Store) IsMember(ctx context.Context, listID int64, email string) (bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var exists int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM list_members WHERE list_id = ? AND email = ? AND is_confirmed = TRUE`,
		listID, email,
	).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check membership: %w", err)
	}
	return true, nil
}

// GetMemberRole returns the role of a member
func (s *Store) GetMemberRole(ctx context.Context, listID int64, email string) (MemberRole, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var role MemberRole
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM list_members WHERE list_id = ? AND email = ? AND is_confirmed = TRUE`,
		listID, email,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to get member role: %w", err)
	}
	return role, nil
}

// CountMembers returns the number of members in a list
func (s *Store) CountMembers(ctx context.Context, listID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM list_members WHERE list_id = ?`,
		listID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count members: %w", err)
	}
	return count, nil
}

// =============================================================================
// Moderation Queue
// =============================================================================

// AddToModerationQueue adds a message to the moderation queue
func (s *Store) AddToModerationQueue(ctx context.Context, msg *ModeratedMessage) error {
	now := time.Now()
	if msg.ExpiresAt.IsZero() {
		msg.ExpiresAt = now.Add(7 * 24 * time.Hour) // 7 days default
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO list_moderation_queue (list_id, sender_email, subject, message_path, message_size, status, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, msg.ListID, msg.SenderEmail, msg.Subject, msg.MessagePath, msg.MessageSize, ModerationPending, msg.ExpiresAt, now)
	if err != nil {
		return fmt.Errorf("failed to add to moderation queue: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get moderation message id: %w", err)
	}
	msg.ID = id
	msg.Status = ModerationPending
	msg.CreatedAt = now

	return nil
}

// GetModeratedMessage retrieves a message from the moderation queue
func (s *Store) GetModeratedMessage(ctx context.Context, id int64) (*ModeratedMessage, error) {
	msg := &ModeratedMessage{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, list_id, sender_email, subject, message_path, message_size, status, moderated_by, moderated_at, rejection_reason, expires_at, created_at
		FROM list_moderation_queue WHERE id = ?
	`, id).Scan(
		&msg.ID, &msg.ListID, &msg.SenderEmail, &msg.Subject, &msg.MessagePath, &msg.MessageSize,
		&msg.Status, &msg.ModeratedBy, &msg.ModeratedAt, &msg.RejectionReason, &msg.ExpiresAt, &msg.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get moderated message: %w", err)
	}
	return msg, nil
}

// ListPendingModeration retrieves all pending messages for a list
func (s *Store) ListPendingModeration(ctx context.Context, listID int64) ([]*ModeratedMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, list_id, sender_email, subject, message_path, message_size, status, moderated_by, moderated_at, rejection_reason, expires_at, created_at
		FROM list_moderation_queue WHERE list_id = ? AND status = ? ORDER BY created_at
	`, listID, ModerationPending)
	if err != nil {
		return nil, fmt.Errorf("failed to list pending moderation: %w", err)
	}
	defer rows.Close()

	var messages []*ModeratedMessage
	for rows.Next() {
		msg := &ModeratedMessage{}
		if err := rows.Scan(
			&msg.ID, &msg.ListID, &msg.SenderEmail, &msg.Subject, &msg.MessagePath, &msg.MessageSize,
			&msg.Status, &msg.ModeratedBy, &msg.ModeratedAt, &msg.RejectionReason, &msg.ExpiresAt, &msg.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan moderated message: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// ListAllPendingModeration retrieves all pending messages across all lists
func (s *Store) ListAllPendingModeration(ctx context.Context) ([]*ModeratedMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, list_id, sender_email, subject, message_path, message_size, status, moderated_by, moderated_at, rejection_reason, expires_at, created_at
		FROM list_moderation_queue WHERE status = ? ORDER BY created_at
	`, ModerationPending)
	if err != nil {
		return nil, fmt.Errorf("failed to list all pending moderation: %w", err)
	}
	defer rows.Close()

	var messages []*ModeratedMessage
	for rows.Next() {
		msg := &ModeratedMessage{}
		if err := rows.Scan(
			&msg.ID, &msg.ListID, &msg.SenderEmail, &msg.Subject, &msg.MessagePath, &msg.MessageSize,
			&msg.Status, &msg.ModeratedBy, &msg.ModeratedAt, &msg.RejectionReason, &msg.ExpiresAt, &msg.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan moderated message: %w", err)
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

// ApproveMessage approves a message in the moderation queue
func (s *Store) ApproveMessage(ctx context.Context, msgID int64, moderatorID int64) error {
	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		UPDATE list_moderation_queue SET status = ?, moderated_by = ?, moderated_at = ?
		WHERE id = ? AND status = ?
	`, ModerationApproved, moderatorID, now, msgID, ModerationPending)
	if err != nil {
		return fmt.Errorf("failed to approve message: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check approve result: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// RejectMessage rejects a message in the moderation queue
func (s *Store) RejectMessage(ctx context.Context, msgID int64, moderatorID int64, reason string) error {
	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		UPDATE list_moderation_queue SET status = ?, moderated_by = ?, moderated_at = ?, rejection_reason = ?
		WHERE id = ? AND status = ?
	`, ModerationRejected, moderatorID, now, reason, msgID, ModerationPending)
	if err != nil {
		return fmt.Errorf("failed to reject message: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check reject result: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// CleanupExpiredModeration removes expired moderation queue items
func (s *Store) CleanupExpiredModeration(ctx context.Context) (int64, error) {
	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM list_moderation_queue WHERE status = ? AND expires_at < ?
	`, ModerationPending, now)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired moderation: %w", err)
	}
	return result.RowsAffected()
}

// =============================================================================
// Archives
// =============================================================================

// ArchiveMessage stores a message in the archive
func (s *Store) ArchiveMessage(ctx context.Context, archive *ArchivedMessage) error {
	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO list_archives (list_id, message_id, sender_email, sender_name, subject, sent_at, message_path, message_size, in_reply_to, thread_id, body_preview, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		archive.ListID, archive.MessageID, archive.SenderEmail, archive.SenderName, archive.Subject,
		archive.SentAt, archive.MessagePath, archive.MessageSize, archive.InReplyTo, archive.ThreadID,
		archive.BodyPreview, now,
	)
	if err != nil {
		return fmt.Errorf("failed to archive message: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get archive id: %w", err)
	}
	archive.ID = id
	archive.CreatedAt = now

	return nil
}

// GetArchivedMessage retrieves an archived message by ID
func (s *Store) GetArchivedMessage(ctx context.Context, id int64) (*ArchivedMessage, error) {
	archive := &ArchivedMessage{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, list_id, message_id, sender_email, sender_name, subject, sent_at, message_path, message_size, in_reply_to, thread_id, body_preview, created_at
		FROM list_archives WHERE id = ?
	`, id).Scan(
		&archive.ID, &archive.ListID, &archive.MessageID, &archive.SenderEmail, &archive.SenderName,
		&archive.Subject, &archive.SentAt, &archive.MessagePath, &archive.MessageSize,
		&archive.InReplyTo, &archive.ThreadID, &archive.BodyPreview, &archive.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get archived message: %w", err)
	}
	return archive, nil
}

// ListArchives retrieves archived messages for a list with pagination
func (s *Store) ListArchives(ctx context.Context, listID int64, limit, offset int) ([]*ArchivedMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, list_id, message_id, sender_email, sender_name, subject, sent_at, message_path, message_size, in_reply_to, thread_id, body_preview, created_at
		FROM list_archives WHERE list_id = ? ORDER BY sent_at DESC LIMIT ? OFFSET ?
	`, listID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list archives: %w", err)
	}
	defer rows.Close()

	var archives []*ArchivedMessage
	for rows.Next() {
		archive := &ArchivedMessage{}
		if err := rows.Scan(
			&archive.ID, &archive.ListID, &archive.MessageID, &archive.SenderEmail, &archive.SenderName,
			&archive.Subject, &archive.SentAt, &archive.MessagePath, &archive.MessageSize,
			&archive.InReplyTo, &archive.ThreadID, &archive.BodyPreview, &archive.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan archive: %w", err)
		}
		archives = append(archives, archive)
	}
	return archives, rows.Err()
}

// SearchArchives searches archived messages
func (s *Store) SearchArchives(ctx context.Context, listID int64, query string, limit int) ([]*ArchivedMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	query = "%" + strings.ToLower(query) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, list_id, message_id, sender_email, sender_name, subject, sent_at, message_path, message_size, in_reply_to, thread_id, body_preview, created_at
		FROM list_archives WHERE list_id = ? AND (LOWER(subject) LIKE ? OR LOWER(sender_email) LIKE ? OR LOWER(body_preview) LIKE ?)
		ORDER BY sent_at DESC LIMIT ?
	`, listID, query, query, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search archives: %w", err)
	}
	defer rows.Close()

	var archives []*ArchivedMessage
	for rows.Next() {
		archive := &ArchivedMessage{}
		if err := rows.Scan(
			&archive.ID, &archive.ListID, &archive.MessageID, &archive.SenderEmail, &archive.SenderName,
			&archive.Subject, &archive.SentAt, &archive.MessagePath, &archive.MessageSize,
			&archive.InReplyTo, &archive.ThreadID, &archive.BodyPreview, &archive.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan archive: %w", err)
		}
		archives = append(archives, archive)
	}
	return archives, rows.Err()
}

// CountArchives returns the number of archived messages for a list
func (s *Store) CountArchives(ctx context.Context, listID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM list_archives WHERE list_id = ?`,
		listID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count archives: %w", err)
	}
	return count, nil
}

// =============================================================================
// Pending Actions
// =============================================================================

// CreatePendingAction creates a pending subscription action
func (s *Store) CreatePendingAction(ctx context.Context, action *PendingAction) error {
	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO list_pending_actions (list_id, email, action, token, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, action.ListID, strings.ToLower(strings.TrimSpace(action.Email)), action.Action, action.Token, action.ExpiresAt, now)
	if err != nil {
		return fmt.Errorf("failed to create pending action: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get pending action id: %w", err)
	}
	action.ID = id
	action.CreatedAt = now

	return nil
}

// GetPendingAction retrieves a pending action by token
func (s *Store) GetPendingAction(ctx context.Context, token string) (*PendingAction, error) {
	action := &PendingAction{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, list_id, email, action, token, expires_at, created_at
		FROM list_pending_actions WHERE token = ?
	`, token).Scan(&action.ID, &action.ListID, &action.Email, &action.Action, &action.Token, &action.ExpiresAt, &action.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pending action: %w", err)
	}
	return action, nil
}

// DeletePendingAction deletes a pending action
func (s *Store) DeletePendingAction(ctx context.Context, token string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM list_pending_actions WHERE token = ?`, token)
	if err != nil {
		return fmt.Errorf("failed to delete pending action: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check delete result: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// CleanupExpiredActions removes expired pending actions
func (s *Store) CleanupExpiredActions(ctx context.Context) (int64, error) {
	now := time.Now()
	result, err := s.db.ExecContext(ctx, `DELETE FROM list_pending_actions WHERE expires_at < ?`, now)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired actions: %w", err)
	}
	return result.RowsAffected()
}

// =============================================================================
// Statistics
// =============================================================================

// GetListStats returns statistics for a mailing list
func (s *Store) GetListStats(ctx context.Context, listID int64) (*ListStats, error) {
	stats := &ListStats{}

	// Total members
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM list_members WHERE list_id = ?`,
		listID,
	).Scan(&stats.TotalMembers)
	if err != nil {
		return nil, fmt.Errorf("failed to count total members: %w", err)
	}

	// Confirmed members
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM list_members WHERE list_id = ? AND is_confirmed = TRUE`,
		listID,
	).Scan(&stats.ConfirmedMembers)
	if err != nil {
		return nil, fmt.Errorf("failed to count confirmed members: %w", err)
	}

	// Owners
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM list_members WHERE list_id = ? AND role = ?`,
		listID, RoleOwner,
	).Scan(&stats.Owners)
	if err != nil {
		return nil, fmt.Errorf("failed to count owners: %w", err)
	}

	// Moderators
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM list_members WHERE list_id = ? AND role = ?`,
		listID, RoleModerator,
	).Scan(&stats.Moderators)
	if err != nil {
		return nil, fmt.Errorf("failed to count moderators: %w", err)
	}

	// Pending moderation
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM list_moderation_queue WHERE list_id = ? AND status = ?`,
		listID, ModerationPending,
	).Scan(&stats.PendingModeration)
	if err != nil {
		return nil, fmt.Errorf("failed to count pending moderation: %w", err)
	}

	// Archive count
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM list_archives WHERE list_id = ?`,
		listID,
	).Scan(&stats.ArchiveCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count archives: %w", err)
	}

	return stats, nil
}
