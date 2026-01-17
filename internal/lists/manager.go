package lists

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
)

// Manager handles mailing list operations
type Manager struct {
	store          *Store
	logger         *logging.Logger
	archivePath    string
	moderationPath string
}

// NewManager creates a new mailing list manager
func NewManager(store *Store, archivePath, moderationPath string, logger *logging.Logger) *Manager {
	return &Manager{
		store:          store,
		logger:         logger,
		archivePath:    archivePath,
		moderationPath: moderationPath,
	}
}

// Store returns the underlying store
func (m *Manager) Store() *Store {
	return m.store
}

// IsListAddress checks if an address is a mailing list
func (m *Manager) IsListAddress(ctx context.Context, address string) (bool, error) {
	return m.store.IsListAddress(ctx, address)
}

// GetListByAddress retrieves a mailing list by address
func (m *Manager) GetListByAddress(ctx context.Context, address string) (*MailingList, error) {
	return m.store.GetListByAddress(ctx, address)
}

// CanPost checks if a sender can post to the list
func (m *Manager) CanPost(ctx context.Context, list *MailingList, senderEmail string) (bool, error) {
	senderEmail = strings.ToLower(strings.TrimSpace(senderEmail))

	switch list.PostingPolicy {
	case PostingAny:
		return true, nil

	case PostingOwnersOnly:
		role, err := m.store.GetMemberRole(ctx, list.ID, senderEmail)
		if err != nil {
			if err == ErrNotFound {
				return false, nil
			}
			return false, err
		}
		return role == RoleOwner, nil

	case PostingMembersOnly:
		isMember, err := m.store.IsMember(ctx, list.ID, senderEmail)
		if err != nil {
			return false, err
		}
		return isMember, nil

	default:
		// Default to members only
		isMember, err := m.store.IsMember(ctx, list.ID, senderEmail)
		if err != nil {
			return false, err
		}
		return isMember, nil
	}
}

// ProcessMessage handles an incoming message to a list
// Returns: (shouldDeliver bool, needsModeration bool, error)
func (m *Manager) ProcessMessage(ctx context.Context, list *MailingList, senderEmail string, data []byte) (bool, bool, error) {
	// Check if moderation is enabled
	if list.ModerationEnabled {
		// Check if sender is a moderator or owner (bypass moderation)
		role, err := m.store.GetMemberRole(ctx, list.ID, senderEmail)
		if err != nil && err != ErrNotFound {
			return false, false, err
		}

		// Owners and moderators bypass moderation
		if role == RoleOwner || role == RoleModerator {
			return true, false, nil
		}

		// Save to moderation queue
		subject := extractSubject(data)
		_, err = m.SaveToModerationQueue(ctx, list, data, senderEmail, subject)
		if err != nil {
			return false, false, fmt.Errorf("failed to save to moderation queue: %w", err)
		}

		m.logger.InfoContext(ctx, "Message held for moderation",
			"list", list.ListAddress,
			"sender", senderEmail,
			"subject", subject,
		)

		return false, true, nil
	}

	return true, false, nil
}

// ExpandRecipients returns all confirmed members for delivery
func (m *Manager) ExpandRecipients(ctx context.Context, listID int64) ([]string, error) {
	members, err := m.store.GetConfirmedMembers(ctx, listID)
	if err != nil {
		return nil, err
	}

	recipients := make([]string, 0, len(members))
	for _, member := range members {
		recipients = append(recipients, member.Email)
	}
	return recipients, nil
}

// PrepareListMessage transforms a message for list delivery
func (m *Manager) PrepareListMessage(ctx context.Context, list *MailingList, originalData []byte) ([]byte, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(originalData))
	if err != nil {
		return nil, fmt.Errorf("failed to parse message: %w", err)
	}

	// Build new headers
	var buf bytes.Buffer

	// Preserve original headers except those we'll modify
	skipHeaders := map[string]bool{
		"Reply-To":         true,
		"List-Id":          true,
		"List-Help":        true,
		"List-Subscribe":   true,
		"List-Unsubscribe": true,
		"List-Post":        true,
		"List-Owner":       true,
		"Precedence":       true,
		"X-Mailing-List":   true,
	}

	// Write original headers (except those we skip)
	for key, values := range msg.Header {
		if skipHeaders[key] {
			continue
		}

		// Handle Subject prefix
		if key == "Subject" && list.RequireSubjectPrefix && list.SubjectPrefix != "" {
			for _, v := range values {
				if !strings.Contains(v, list.SubjectPrefix) {
					v = list.SubjectPrefix + " " + v
				}
				fmt.Fprintf(&buf, "Subject: %s\r\n", v)
			}
			continue
		}

		for _, v := range values {
			fmt.Fprintf(&buf, "%s: %s\r\n", key, v)
		}
	}

	// Add list headers
	domain := strings.SplitN(list.ListAddress, "@", 2)[1]
	listID := fmt.Sprintf("<%s.lists.%s>", list.LocalPart, domain)

	fmt.Fprintf(&buf, "List-Id: %s %s\r\n", list.Name, listID)
	fmt.Fprintf(&buf, "List-Help: <mailto:%s-help@%s>\r\n", list.LocalPart, domain)
	fmt.Fprintf(&buf, "List-Subscribe: <mailto:%s-subscribe@%s>\r\n", list.LocalPart, domain)
	fmt.Fprintf(&buf, "List-Unsubscribe: <mailto:%s-unsubscribe@%s>\r\n", list.LocalPart, domain)

	if list.ListType == ListTypeAnnouncement {
		fmt.Fprintf(&buf, "List-Post: NO (This is an announcement list)\r\n")
	} else {
		fmt.Fprintf(&buf, "List-Post: <mailto:%s>\r\n", list.ListAddress)
	}

	fmt.Fprintf(&buf, "List-Owner: <mailto:%s-owner@%s>\r\n", list.LocalPart, domain)
	fmt.Fprintf(&buf, "Precedence: list\r\n")
	fmt.Fprintf(&buf, "X-Mailing-List: %s\r\n", list.ListAddress)

	// Set Reply-To
	if list.ReplyToList {
		fmt.Fprintf(&buf, "Reply-To: %s\r\n", list.ListAddress)
	}

	// Empty line to separate headers from body
	buf.WriteString("\r\n")

	// Copy body (limit to 25MB to prevent OOM)
	const maxBodySize = 25 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(msg.Body, maxBodySize))
	if err != nil {
		return nil, fmt.Errorf("failed to read message body: %w", err)
	}
	buf.Write(body)

	return buf.Bytes(), nil
}

// SaveToArchive stores a delivered message in the archive
func (m *Manager) SaveToArchive(ctx context.Context, list *MailingList, data []byte, senderEmail string) error {
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to parse message: %w", err)
	}

	now := time.Now()

	// Create archive directory
	archiveDir := filepath.Join(m.archivePath, fmt.Sprintf("list_%d", list.ID),
		now.Format("2006"), now.Format("01"))
	if err := os.MkdirAll(archiveDir, 0750); err != nil {
		return fmt.Errorf("failed to create archive directory: %w", err)
	}

	// Generate unique filename
	msgID := msg.Header.Get("Message-ID")
	if msgID == "" {
		msgID = generateMessageID()
	}
	// Sanitize message ID for filename
	filename := strings.ReplaceAll(msgID, "<", "")
	filename = strings.ReplaceAll(filename, ">", "")
	filename = strings.ReplaceAll(filename, "/", "_")
	filename = strings.ReplaceAll(filename, "\\", "_")
	if len(filename) > 100 {
		filename = filename[:100]
	}
	filename = filename + ".eml"

	messagePath := filepath.Join(archiveDir, filename)

	// Write message to file
	if err := os.WriteFile(messagePath, data, 0640); err != nil {
		return fmt.Errorf("failed to write archive file: %w", err)
	}

	// Extract metadata
	subject := msg.Header.Get("Subject")
	inReplyTo := msg.Header.Get("In-Reply-To")

	// Extract sender name from From header
	senderName := ""
	if from := msg.Header.Get("From"); from != "" {
		if addr, err := mail.ParseAddress(from); err == nil {
			senderName = addr.Name
		}
	}

	// Extract body preview
	bodyPreview := extractBodyPreview(msg, 500)

	// Parse sent time
	sentAt := now
	if dateStr := msg.Header.Get("Date"); dateStr != "" {
		if t, err := mail.ParseDate(dateStr); err == nil {
			sentAt = t
		}
	}

	// Store in database
	archive := &ArchivedMessage{
		ListID:      list.ID,
		MessageID:   msgID,
		SenderEmail: senderEmail,
		SenderName:  senderName,
		Subject:     subject,
		SentAt:      sentAt,
		MessagePath: messagePath,
		MessageSize: int64(len(data)),
		InReplyTo:   inReplyTo,
		BodyPreview: bodyPreview,
	}

	return m.store.ArchiveMessage(ctx, archive)
}

// SaveToModerationQueue stores a message pending moderation
func (m *Manager) SaveToModerationQueue(ctx context.Context, list *MailingList, data []byte, senderEmail, subject string) (*ModeratedMessage, error) {
	now := time.Now()

	// Create moderation directory
	modDir := filepath.Join(m.moderationPath, fmt.Sprintf("list_%d", list.ID))
	if err := os.MkdirAll(modDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create moderation directory: %w", err)
	}

	// Generate unique filename
	filename := fmt.Sprintf("%d_%s.eml", now.UnixNano(), generateRandomString(8))
	messagePath := filepath.Join(modDir, filename)

	// Write message to file
	if err := os.WriteFile(messagePath, data, 0640); err != nil {
		return nil, fmt.Errorf("failed to write moderation file: %w", err)
	}

	// Store in database
	msg := &ModeratedMessage{
		ListID:      list.ID,
		SenderEmail: senderEmail,
		Subject:     subject,
		MessagePath: messagePath,
		MessageSize: int64(len(data)),
		ExpiresAt:   now.Add(7 * 24 * time.Hour), // 7 days
	}

	if err := m.store.AddToModerationQueue(ctx, msg); err != nil {
		// Clean up file on failure
		os.Remove(messagePath)
		return nil, err
	}

	return msg, nil
}

// GetModerationMessage retrieves a message from the moderation queue
func (m *Manager) GetModerationMessage(ctx context.Context, msgID int64) ([]byte, error) {
	msg, err := m.store.GetModeratedMessage(ctx, msgID)
	if err != nil {
		return nil, err
	}

	return os.ReadFile(msg.MessagePath)
}

// ApproveAndDeliver approves a moderated message and delivers it
func (m *Manager) ApproveAndDeliver(ctx context.Context, msgID int64, moderatorID int64) (*ModeratedMessage, []byte, error) {
	// Get the moderated message
	msg, err := m.store.GetModeratedMessage(ctx, msgID)
	if err != nil {
		return nil, nil, err
	}

	// Get the list
	list, err := m.store.GetList(ctx, msg.ListID)
	if err != nil {
		return nil, nil, err
	}

	// Read the message data
	data, err := os.ReadFile(msg.MessagePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read moderated message: %w", err)
	}

	// Mark as approved
	if err := m.store.ApproveMessage(ctx, msgID, moderatorID); err != nil {
		return nil, nil, err
	}

	// Prepare message with list headers
	listData, err := m.PrepareListMessage(ctx, list, data)
	if err != nil {
		return nil, nil, err
	}

	// Archive if enabled
	if list.ArchiveEnabled {
		if err := m.SaveToArchive(ctx, list, listData, msg.SenderEmail); err != nil {
			m.logger.WarnContext(ctx, "Failed to archive approved message",
				"list", list.ListAddress,
				"error", err.Error(),
			)
		}
	}

	// Clean up moderation file (optional, keep for audit)
	// os.Remove(msg.MessagePath)

	return msg, listData, nil
}

// RejectModeratedMessage rejects a message in the moderation queue
func (m *Manager) RejectModeratedMessage(ctx context.Context, msgID int64, moderatorID int64, reason string) error {
	msg, err := m.store.GetModeratedMessage(ctx, msgID)
	if err != nil {
		return err
	}

	// Mark as rejected
	if err := m.store.RejectMessage(ctx, msgID, moderatorID, reason); err != nil {
		return err
	}

	// Clean up moderation file
	if err := os.Remove(msg.MessagePath); err != nil {
		m.logger.WarnContext(ctx, "Failed to remove rejected moderation file",
			"path", msg.MessagePath,
			"error", err.Error(),
		)
	}

	return nil
}

// CleanupExpired cleans up expired moderation queue items and pending actions
func (m *Manager) CleanupExpired(ctx context.Context) error {
	// Clean up expired moderation queue items
	modCount, err := m.store.CleanupExpiredModeration(ctx)
	if err != nil {
		return fmt.Errorf("failed to cleanup expired moderation: %w", err)
	}
	if modCount > 0 {
		m.logger.InfoContext(ctx, "Cleaned up expired moderation queue items", "count", modCount)
	}

	// Clean up expired pending actions
	actionCount, err := m.store.CleanupExpiredActions(ctx)
	if err != nil {
		return fmt.Errorf("failed to cleanup expired actions: %w", err)
	}
	if actionCount > 0 {
		m.logger.InfoContext(ctx, "Cleaned up expired pending actions", "count", actionCount)
	}

	return nil
}

// Helper functions

func extractSubject(data []byte) string {
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	return msg.Header.Get("Subject")
}

func extractBodyPreview(msg *mail.Message, maxLen int) string {
	contentType := msg.Header.Get("Content-Type")
	mediaType, params, _ := mime.ParseMediaType(contentType)

	var bodyText string
	// Limit preview reads to prevent OOM (read slightly more than maxLen to handle encoding)
	previewLimit := int64(maxLen * 4)
	if previewLimit < 64*1024 {
		previewLimit = 64 * 1024 // Minimum 64KB
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(msg.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			partType := part.Header.Get("Content-Type")
			if strings.HasPrefix(partType, "text/plain") || partType == "" {
				data, err := io.ReadAll(io.LimitReader(part, previewLimit))
				if err == nil {
					bodyText = string(data)
				}
				break
			}
		}
	} else {
		data, err := io.ReadAll(io.LimitReader(msg.Body, previewLimit))
		if err == nil {
			bodyText = string(data)
		}
	}

	// Clean up and truncate
	bodyText = strings.TrimSpace(bodyText)
	bodyText = strings.ReplaceAll(bodyText, "\r\n", " ")
	bodyText = strings.ReplaceAll(bodyText, "\n", " ")

	if len(bodyText) > maxLen {
		bodyText = bodyText[:maxLen]
	}

	return bodyText
}

func generateMessageID() string {
	return fmt.Sprintf("<%s@lists.local>", generateRandomString(20))
}

func generateRandomString(length int) string {
	bytes := make([]byte, length/2+1)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)[:length]
}
