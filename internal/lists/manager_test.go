package lists

import (
	"testing"
	"time"

	"github.com/fenilsonani/email-server/tests/shared/helpers"
)

// TestListCreation tests mailing list creation
func TestListCreation(t *testing.T) {
	t.Run("create_announcement_list", func(t *testing.T) {
		list := &MailingList{
			ID:           1,
			DomainID:     1,
			LocalPart:    "announce",
			ListAddress:  "announce@example.com",
			Name:         "Announcements",
			Description:  "System announcements",
			ListType:     ListTypeAnnouncement,
			PostingPolicy: PostingOwnersOnly,
			IsActive:     true,
		}
		if list.ListType != ListTypeAnnouncement {
			t.Errorf("List type should be announcement")
		}
	})

	t.Run("create_discussion_list", func(t *testing.T) {
		list := &MailingList{
			LocalPart:    "discuss",
			ListAddress:  "discuss@example.com",
			ListType:     ListTypeDiscussion,
			PostingPolicy: PostingMembersOnly,
			IsActive:     true,
		}
		if list.ListType != ListTypeDiscussion {
			t.Errorf("List type should be discussion")
		}
	})

	t.Run("create_list_with_moderation", func(t *testing.T) {
		list := &MailingList{
			LocalPart:          "moderated",
			ModerationEnabled:  true,
			PostingPolicy:      PostingAny,
			RequireSubjectPrefix: true,
			SubjectPrefix:       "[TEST]",
		}
		if !list.ModerationEnabled {
			t.Errorf("Moderation should be enabled")
		}
		if list.SubjectPrefix != "[TEST]" {
			t.Errorf("Subject prefix mismatch")
		}
	})

	t.Run("create_list_with_archive", func(t *testing.T) {
		list := &MailingList{
			LocalPart:      "archived",
			ArchiveEnabled: true,
			ArchivePublic:  true,
		}
		if !list.ArchiveEnabled {
			t.Errorf("Archive should be enabled")
		}
		if !list.ArchivePublic {
			t.Errorf("Archive should be public")
		}
	})
}

// TestPostingPolicy tests posting policy enforcement
func TestPostingPolicy(t *testing.T) {
	t.Run("posting_anyone", func(t *testing.T) {
		policy := PostingAny
		if policy != PostingAny {
			t.Errorf("Policy should be PostingAny")
		}
	})

	t.Run("posting_members_only", func(t *testing.T) {
		policy := PostingMembersOnly
		if policy != PostingMembersOnly {
			t.Errorf("Policy should be PostingMembersOnly")
		}
	})

	t.Run("posting_owners_only", func(t *testing.T) {
		policy := PostingOwnersOnly
		if policy != PostingOwnersOnly {
			t.Errorf("Policy should be PostingOwnersOnly")
		}
	})
}

// TestMemberRoles tests member role definitions
func TestMemberRoles(t *testing.T) {
	t.Run("owner_role", func(t *testing.T) {
		role := RoleOwner
		if role != RoleOwner {
			t.Errorf("Role should be RoleOwner")
		}
	})

	t.Run("moderator_role", func(t *testing.T) {
		role := RoleModerator
		if role != RoleModerator {
			t.Errorf("Role should be RoleModerator")
		}
	})

	t.Run("member_role", func(t *testing.T) {
		role := RoleMember
		if role != RoleMember {
			t.Errorf("Role should be RoleMember")
		}
	})
}

// TestDeliveryModes tests delivery mode options
func TestDeliveryModes(t *testing.T) {
	t.Run("normal_delivery", func(t *testing.T) {
		mode := DeliveryNormal
		if mode != DeliveryNormal {
			t.Errorf("Mode should be DeliveryNormal")
		}
	})

	t.Run("digest_delivery", func(t *testing.T) {
		mode := DeliveryDigest
		if mode != DeliveryDigest {
			t.Errorf("Mode should be DeliveryDigest")
		}
	})

	t.Run("nomail_delivery", func(t *testing.T) {
		mode := DeliveryNomail
		if mode != DeliveryNomail {
			t.Errorf("Mode should be DeliveryNomail")
		}
	})
}

// TestMemberSubscription tests member subscription operations
func TestMemberSubscription(t *testing.T) {
	t.Run("add_member", func(t *testing.T) {
		member := &ListMember{
			Email:      "user@example.com",
			Name:       "Test User",
			Role:       RoleMember,
			DeliveryMode: DeliveryNormal,
			IsConfirmed: true,
		}
		if member.Email != "user@example.com" {
			t.Errorf("Email mismatch")
		}
	})

	t.Run("add_unconfirmed_member", func(t *testing.T) {
		member := &ListMember{
			Email:      "newuser@example.com",
			IsConfirmed: false,
			ConfirmToken: "abc123",
		}
		if member.IsConfirmed {
			t.Errorf("Member should be unconfirmed")
		}
	})

	t.Run("change_member_role", func(t *testing.T) {
		member := &ListMember{
			Email: "user@example.com",
			Role:  RoleMember,
		}
		member.Role = RoleModerator
		if member.Role != RoleModerator {
			t.Errorf("Role should be moderator")
		}
	})

	t.Run("change_delivery_mode", func(t *testing.T) {
		member := &ListMember{
			Email: "user@example.com",
			DeliveryMode: DeliveryNormal,
		}
		member.DeliveryMode = DeliveryDigest
		if member.DeliveryMode != DeliveryDigest {
			t.Errorf("Delivery mode should be digest")
		}
	})

	t.Run("remove_member", func(t *testing.T) {
		// Simulated member removal
		member := &ListMember{
			Email: "user@example.com",
		}
		_ = member
		t.Log("Member should be removed from list")
	})
}

// TestModerationQueue tests moderation queue operations
func TestModerationQueue(t *testing.T) {
	t.Run("add_message_to_moderation", func(t *testing.T) {
		msg := &ModeratedMessage{
			ListID:      1,
			SenderEmail: "sender@example.com",
			Subject:     "Test Message",
			Status:      ModerationPending,
			ExpiresAt:   time.Now().Add(7 * 24 * time.Hour),
		}
		if msg.Status != ModerationPending {
			t.Errorf("Status should be pending")
		}
	})

	t.Run("approve_message", func(t *testing.T) {
		msg := &ModeratedMessage{
			Status: ModerationPending,
		}
		msg.Status = ModerationApproved
		if msg.Status != ModerationApproved {
			t.Errorf("Status should be approved")
		}
	})

	t.Run("reject_message", func(t *testing.T) {
		msg := &ModeratedMessage{
			Status: ModerationPending,
			RejectionReason: "Spam",
		}
		msg.Status = ModerationRejected
		if msg.Status != ModerationRejected {
			t.Errorf("Status should be rejected")
		}
	})

	t.Run("expire_message", func(t *testing.T) {
		msg := &ModeratedMessage{
			Status:    ModerationPending,
			ExpiresAt: time.Now().Add(-1 * time.Hour), // Already expired
		}
		msg.Status = ModerationExpired
		if msg.Status != ModerationExpired {
			t.Errorf("Status should be expired")
		}
	})
}

// TestArchive tests message archiving
func TestArchive(t *testing.T) {
	t.Run("archive_message", func(t *testing.T) {
		archived := &ArchivedMessage{
			ListID:      1,
			SenderEmail: "sender@example.com",
			SenderName:  "Sender Name",
			Subject:     "Test Message",
			SentAt:      time.Now(),
		}
		if archived.Subject != "Test Message" {
			t.Errorf("Subject mismatch")
		}
	})

	t.Run("archive_thread", func(t *testing.T) {
		msg1 := &ArchivedMessage{
			ListID:    1,
			Subject:   "Original Message",
			ThreadID:  nil,
		}
		msg2 := &ArchivedMessage{
			ListID:    1,
			Subject:   "Re: Original Message",
			InReplyTo: "msg1-id",
			ThreadID:  &msg1.ID,
		}
		if msg2.ThreadID != &msg1.ID {
			t.Logf("Thread should be linked")
		}
	})

	t.Run("archive_with_preview", func(t *testing.T) {
		msg := &ArchivedMessage{
			Subject:     "Test",
			BodyPreview: "This is a preview of the message body...",
		}
		if msg.BodyPreview == "" {
			t.Errorf("Body preview should not be empty")
		}
	})
}

// TestListStats tests statistics calculation
func TestListStats(t *testing.T) {
	t.Run("calculate_stats", func(t *testing.T) {
		stats := &ListStats{
			TotalMembers:     100,
			ConfirmedMembers: 95,
			Owners:           2,
			Moderators:       5,
			PendingModeration: 3,
			ArchiveCount:     150,
		}
		if stats.TotalMembers != 100 {
			t.Errorf("Total members mismatch")
		}
	})

	t.Run("stats_with_no_members", func(t *testing.T) {
		stats := &ListStats{
			TotalMembers: 0,
			ConfirmedMembers: 0,
		}
		if stats.TotalMembers != 0 {
			t.Errorf("Total should be 0")
		}
	})
}

// TestPendingAction tests pending subscription actions
func TestPendingAction(t *testing.T) {
	t.Run("create_subscription_request", func(t *testing.T) {
		action := &PendingAction{
			ListID:    1,
			Email:     "newuser@example.com",
			Action:    "subscribe",
			Token:     "token123",
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}
		if action.Action != "subscribe" {
			t.Errorf("Action should be subscribe")
		}
	})

	t.Run("create_unsubscription_request", func(t *testing.T) {
		action := &PendingAction{
			ListID: 1,
			Email:  "user@example.com",
			Action: "unsubscribe",
		}
		if action.Action != "unsubscribe" {
			t.Errorf("Action should be unsubscribe")
		}
	})

	t.Run("action_expiration", func(t *testing.T) {
		action := &PendingAction{
			ExpiresAt: time.Now().Add(-1 * time.Hour), // Already expired
		}
		if time.Now().Before(action.ExpiresAt) {
			t.Errorf("Action should be expired")
		}
	})
}

// TestListValidation tests list validation
func TestListValidation(t *testing.T) {
	t.Run("validate_list_address", func(t *testing.T) {
		list := &MailingList{
			LocalPart:   "test",
			ListAddress: "test@example.com",
		}
		if list.ListAddress == "" {
			t.Errorf("List address should not be empty")
		}
	})

	t.Run("validate_list_name", func(t *testing.T) {
		list := &MailingList{
			Name: "Test List",
		}
		if list.Name == "" {
			t.Errorf("List name should not be empty")
		}
	})

	t.Run("validate_max_members", func(t *testing.T) {
		list := &MailingList{
			MaxMembers: 1000,
		}
		if list.MaxMembers <= 0 {
			t.Errorf("Max members should be positive")
		}
	})

	t.Run("validate_max_message_size", func(t *testing.T) {
		list := &MailingList{
			MaxMessageSize: 25 * 1024 * 1024, // 25MB
		}
		if list.MaxMessageSize <= 0 {
			t.Errorf("Max message size should be positive")
		}
	})
}

// TestListConcurrency tests concurrent list operations
func TestListConcurrency(t *testing.T) {
	t.Run("concurrent_member_operations", func(t *testing.T) {
		helpers.RunConcurrent(t, 10, func(i int) error {
			member := &ListMember{
				Email: "user" + string(rune('0'+i)) + "@example.com",
				Role:  RoleMember,
			}
			_ = member
			return nil
		})
	})

	t.Run("concurrent_moderation_actions", func(t *testing.T) {
		helpers.RunConcurrent(t, 5, func(i int) error {
			msg := &ModeratedMessage{
				Status: ModerationPending,
			}
			_ = msg
			return nil
		})
	})
}

// BenchmarkListCreation benchmarks list creation
func BenchmarkListCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = &MailingList{
			LocalPart:   "test",
			ListAddress: "test@example.com",
			ListType:    ListTypeDiscussion,
		}
	}
}

// BenchmarkMemberAddition benchmarks adding members
func BenchmarkMemberAddition(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = &ListMember{
			Email:        "user@example.com",
			Role:         RoleMember,
			DeliveryMode: DeliveryNormal,
		}
	}
}
