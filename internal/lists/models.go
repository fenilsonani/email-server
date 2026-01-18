package lists

import "time"

// ListType defines the type of mailing list
type ListType string

const (
	ListTypeAnnouncement ListType = "announcement"
	ListTypeDiscussion   ListType = "discussion"
)

// PostingPolicy defines who can post to the list
type PostingPolicy string

const (
	PostingAny         PostingPolicy = "anyone"
	PostingMembersOnly PostingPolicy = "members_only"
	PostingOwnersOnly  PostingPolicy = "owners_only"
)

// MemberRole defines the role of a list member
type MemberRole string

const (
	RoleOwner     MemberRole = "owner"
	RoleModerator MemberRole = "moderator"
	RoleMember    MemberRole = "member"
)

// DeliveryMode defines how a member receives messages
type DeliveryMode string

const (
	DeliveryNormal DeliveryMode = "normal"
	DeliveryDigest DeliveryMode = "digest"
	DeliveryNomail DeliveryMode = "nomail"
)

// ModerationStatus defines the status of a moderated message
type ModerationStatus string

const (
	ModerationPending  ModerationStatus = "pending"
	ModerationApproved ModerationStatus = "approved"
	ModerationRejected ModerationStatus = "rejected"
	ModerationExpired  ModerationStatus = "expired"
)

// MailingList represents a mailing list
type MailingList struct {
	ID                   int64         `json:"id"`
	DomainID             int64         `json:"domain_id"`
	LocalPart            string        `json:"local_part"`
	ListAddress          string        `json:"list_address"`
	Name                 string        `json:"name"`
	Description          string        `json:"description,omitempty"`
	ListType             ListType      `json:"list_type"`
	PostingPolicy        PostingPolicy `json:"posting_policy"`
	ModerationEnabled    bool          `json:"moderation_enabled"`
	RequireSubjectPrefix bool          `json:"require_subject_prefix"`
	SubjectPrefix        string        `json:"subject_prefix,omitempty"`
	ReplyToList          bool          `json:"reply_to_list"`
	ReplyToSender        bool          `json:"reply_to_sender"`
	ArchiveEnabled       bool          `json:"archive_enabled"`
	ArchivePublic        bool          `json:"archive_public"`
	AllowSubscribe       bool          `json:"allow_subscribe"`
	RequireConfirm       bool          `json:"require_confirm"`
	MaxMessageSize       int64         `json:"max_message_size"`
	MaxMembers           int           `json:"max_members"`
	IsActive             bool          `json:"is_active"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`
}

// ListMember represents a member of a mailing list
type ListMember struct {
	ID             int64        `json:"id"`
	ListID         int64        `json:"list_id"`
	Email          string       `json:"email"`
	Name           string       `json:"name,omitempty"`
	Role           MemberRole   `json:"role"`
	DeliveryMode   DeliveryMode `json:"delivery_mode"`
	IsConfirmed    bool         `json:"is_confirmed"`
	ConfirmToken   string       `json:"-"`
	ConfirmExpires *time.Time   `json:"-"`
	SubscribedAt   time.Time    `json:"subscribed_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

// ModeratedMessage represents a message in the moderation queue
type ModeratedMessage struct {
	ID              int64            `json:"id"`
	ListID          int64            `json:"list_id"`
	SenderEmail     string           `json:"sender_email"`
	Subject         string           `json:"subject"`
	MessagePath     string           `json:"message_path"`
	MessageSize     int64            `json:"message_size"`
	Status          ModerationStatus `json:"status"`
	ModeratedBy     *int64           `json:"moderated_by,omitempty"`
	ModeratedAt     *time.Time       `json:"moderated_at,omitempty"`
	RejectionReason string           `json:"rejection_reason,omitempty"`
	ExpiresAt       time.Time        `json:"expires_at"`
	CreatedAt       time.Time        `json:"created_at"`
}

// ArchivedMessage represents an archived list message
type ArchivedMessage struct {
	ID          int64     `json:"id"`
	ListID      int64     `json:"list_id"`
	MessageID   string    `json:"message_id,omitempty"`
	SenderEmail string    `json:"sender_email"`
	SenderName  string    `json:"sender_name,omitempty"`
	Subject     string    `json:"subject"`
	SentAt      time.Time `json:"sent_at"`
	MessagePath string    `json:"message_path"`
	MessageSize int64     `json:"message_size"`
	InReplyTo   string    `json:"in_reply_to,omitempty"`
	ThreadID    *int64    `json:"thread_id,omitempty"`
	BodyPreview string    `json:"body_preview,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// PendingAction represents a pending subscription action
type PendingAction struct {
	ID        int64     `json:"id"`
	ListID    int64     `json:"list_id"`
	Email     string    `json:"email"`
	Action    string    `json:"action"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// ListStats holds statistics for a mailing list
type ListStats struct {
	TotalMembers      int `json:"total_members"`
	ConfirmedMembers  int `json:"confirmed_members"`
	Owners            int `json:"owners"`
	Moderators        int `json:"moderators"`
	PendingModeration int `json:"pending_moderation"`
	ArchiveCount      int `json:"archive_count"`
}
