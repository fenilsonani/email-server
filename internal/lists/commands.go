package lists

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
)

// CommandType represents the type of list command
type CommandType string

const (
	CmdSubscribe   CommandType = "subscribe"
	CmdUnsubscribe CommandType = "unsubscribe"
	CmdHelp        CommandType = "help"
	CmdConfirm     CommandType = "confirm"
)

// CommandResult contains the result of processing a command
type CommandResult struct {
	Success      bool
	Message      string
	ResponseData []byte // Email response to send back
}

// CommandHandler processes email commands to list addresses
type CommandHandler struct {
	store      *Store
	manager    *Manager
	logger     *logging.Logger
	confirmTTL time.Duration
	hostname   string
}

// NewCommandHandler creates a new command handler
func NewCommandHandler(store *Store, manager *Manager, hostname string, logger *logging.Logger) *CommandHandler {
	return &CommandHandler{
		store:      store,
		manager:    manager,
		logger:     logger,
		confirmTTL: 48 * time.Hour,
		hostname:   hostname,
	}
}

// confirmTokenRegex matches confirm token in addresses like list-confirm-abc123@domain.com
var confirmTokenRegex = regexp.MustCompile(`^(.+)-confirm-([a-f0-9]+)@(.+)$`)

// IsCommandAddress checks if an address is a list command address
func (h *CommandHandler) IsCommandAddress(address string) bool {
	address = strings.ToLower(strings.TrimSpace(address))

	// Check for confirm token format
	if confirmTokenRegex.MatchString(address) {
		return true
	}

	parts := strings.SplitN(address, "@", 2)
	if len(parts) != 2 {
		return false
	}

	localPart := parts[0]

	// Check for command suffixes
	if strings.HasSuffix(localPart, "-subscribe") ||
		strings.HasSuffix(localPart, "-unsubscribe") ||
		strings.HasSuffix(localPart, "-help") ||
		strings.HasSuffix(localPart, "-owner") {
		return true
	}

	return false
}

// ParseCommand extracts the command from a list address
func (h *CommandHandler) ParseCommand(address string) (listLocalPart string, domain string, cmd CommandType, token string, err error) {
	address = strings.ToLower(strings.TrimSpace(address))

	// Check for confirm token format first
	if matches := confirmTokenRegex.FindStringSubmatch(address); len(matches) == 4 {
		return matches[1], matches[3], CmdConfirm, matches[2], nil
	}

	parts := strings.SplitN(address, "@", 2)
	if len(parts) != 2 {
		return "", "", "", "", fmt.Errorf("invalid address format")
	}

	localPart := parts[0]
	domain = parts[1]

	switch {
	case strings.HasSuffix(localPart, "-subscribe"):
		listLocalPart = strings.TrimSuffix(localPart, "-subscribe")
		cmd = CmdSubscribe
	case strings.HasSuffix(localPart, "-unsubscribe"):
		listLocalPart = strings.TrimSuffix(localPart, "-unsubscribe")
		cmd = CmdUnsubscribe
	case strings.HasSuffix(localPart, "-help"):
		listLocalPart = strings.TrimSuffix(localPart, "-help")
		cmd = CmdHelp
	case strings.HasSuffix(localPart, "-owner"):
		// Owner requests are handled separately, not as commands
		return "", "", "", "", fmt.Errorf("owner requests not handled as commands")
	default:
		return "", "", "", "", fmt.Errorf("unknown command in address: %s", address)
	}

	return listLocalPart, domain, cmd, "", nil
}

// HandleCommand processes a command email
func (h *CommandHandler) HandleCommand(ctx context.Context, rcptAddress, senderEmail string) (*CommandResult, error) {
	listLocalPart, domain, cmd, token, err := h.ParseCommand(rcptAddress)
	if err != nil {
		return nil, err
	}

	listAddress := listLocalPart + "@" + domain
	senderEmail = strings.ToLower(strings.TrimSpace(senderEmail))

	h.logger.InfoContext(ctx, "Processing list command",
		"command", cmd,
		"list", listAddress,
		"sender", senderEmail,
	)

	switch cmd {
	case CmdSubscribe:
		return h.handleSubscribe(ctx, listAddress, senderEmail)
	case CmdUnsubscribe:
		return h.handleUnsubscribe(ctx, listAddress, senderEmail)
	case CmdHelp:
		return h.handleHelp(ctx, listAddress, senderEmail)
	case CmdConfirm:
		return h.handleConfirm(ctx, token, senderEmail)
	default:
		return nil, fmt.Errorf("unknown command: %s", cmd)
	}
}

// handleSubscribe initiates subscription (sends confirmation if required)
func (h *CommandHandler) handleSubscribe(ctx context.Context, listAddress, senderEmail string) (*CommandResult, error) {
	list, err := h.store.GetListByAddress(ctx, listAddress)
	if err != nil {
		return nil, fmt.Errorf("list not found: %w", err)
	}

	if !list.IsActive {
		return &CommandResult{
			Success: false,
			Message: "This mailing list is not active",
		}, nil
	}

	if !list.AllowSubscribe {
		return &CommandResult{
			Success:      false,
			Message:      "This mailing list does not accept subscription requests",
			ResponseData: h.generateErrorEmail(list, senderEmail, "Subscription Not Allowed", "This mailing list does not accept subscription requests. Please contact the list owner."),
		}, nil
	}

	// Check if already a member
	existing, err := h.store.GetMember(ctx, list.ID, senderEmail)
	if err == nil && existing != nil {
		if existing.IsConfirmed {
			return &CommandResult{
				Success:      false,
				Message:      "Already subscribed",
				ResponseData: h.generateErrorEmail(list, senderEmail, "Already Subscribed", fmt.Sprintf("You are already subscribed to %s.", list.ListAddress)),
			}, nil
		}
		// If not confirmed, resend confirmation
	}

	// Check member limit
	count, err := h.store.CountMembers(ctx, list.ID)
	if err != nil {
		return nil, err
	}
	if count >= list.MaxMembers {
		return &CommandResult{
			Success:      false,
			Message:      "List is full",
			ResponseData: h.generateErrorEmail(list, senderEmail, "List Full", "This mailing list has reached its maximum membership. Please try again later."),
		}, nil
	}

	if list.RequireConfirm {
		// Generate confirmation token
		token := h.generateToken()
		expiresAt := time.Now().Add(h.confirmTTL)

		// Create or update pending action
		action := &PendingAction{
			ListID:    list.ID,
			Email:     senderEmail,
			Action:    "subscribe",
			Token:     token,
			ExpiresAt: expiresAt,
		}

		if err := h.store.CreatePendingAction(ctx, action); err != nil {
			return nil, fmt.Errorf("failed to create pending action: %w", err)
		}

		// Generate confirmation email
		responseData := h.generateConfirmationEmail(list, senderEmail, "subscribe", token)

		h.logger.InfoContext(ctx, "Subscription confirmation sent",
			"list", listAddress,
			"email", senderEmail,
		)

		return &CommandResult{
			Success:      true,
			Message:      "Confirmation email sent",
			ResponseData: responseData,
		}, nil
	}

	// Direct subscription (no confirmation required)
	member := &ListMember{
		ListID:       list.ID,
		Email:        senderEmail,
		Role:         RoleMember,
		DeliveryMode: DeliveryNormal,
		IsConfirmed:  true,
	}

	if err := h.store.AddMember(ctx, member); err != nil {
		if err == ErrAlreadyExists {
			// Update existing to confirmed
			h.store.ConfirmMember(ctx, list.ID, senderEmail)
		} else {
			return nil, fmt.Errorf("failed to add member: %w", err)
		}
	}

	h.logger.InfoContext(ctx, "User subscribed to list",
		"list", listAddress,
		"email", senderEmail,
	)

	return &CommandResult{
		Success:      true,
		Message:      "Successfully subscribed",
		ResponseData: h.generateWelcomeEmail(list, senderEmail),
	}, nil
}

// handleUnsubscribe initiates unsubscription
func (h *CommandHandler) handleUnsubscribe(ctx context.Context, listAddress, senderEmail string) (*CommandResult, error) {
	list, err := h.store.GetListByAddress(ctx, listAddress)
	if err != nil {
		return nil, fmt.Errorf("list not found: %w", err)
	}

	// Check if member
	member, err := h.store.GetMember(ctx, list.ID, senderEmail)
	if err != nil {
		return &CommandResult{
			Success:      false,
			Message:      "Not a member",
			ResponseData: h.generateErrorEmail(list, senderEmail, "Not Subscribed", fmt.Sprintf("You are not subscribed to %s.", list.ListAddress)),
		}, nil
	}

	// Owners cannot unsubscribe directly (they must transfer ownership first)
	if member.Role == RoleOwner {
		ownerCount := 0
		members, _ := h.store.ListMembers(ctx, list.ID)
		for _, m := range members {
			if m.Role == RoleOwner {
				ownerCount++
			}
		}
		if ownerCount <= 1 {
			return &CommandResult{
				Success:      false,
				Message:      "Cannot unsubscribe: you are the only owner",
				ResponseData: h.generateErrorEmail(list, senderEmail, "Cannot Unsubscribe", "You are the only owner of this list. Please assign another owner before unsubscribing."),
			}, nil
		}
	}

	if list.RequireConfirm {
		// Generate confirmation token for unsubscribe
		token := h.generateToken()
		expiresAt := time.Now().Add(h.confirmTTL)

		action := &PendingAction{
			ListID:    list.ID,
			Email:     senderEmail,
			Action:    "unsubscribe",
			Token:     token,
			ExpiresAt: expiresAt,
		}

		if err := h.store.CreatePendingAction(ctx, action); err != nil {
			return nil, fmt.Errorf("failed to create pending action: %w", err)
		}

		responseData := h.generateConfirmationEmail(list, senderEmail, "unsubscribe", token)

		return &CommandResult{
			Success:      true,
			Message:      "Unsubscribe confirmation sent",
			ResponseData: responseData,
		}, nil
	}

	// Direct unsubscription
	if err := h.store.RemoveMember(ctx, list.ID, senderEmail); err != nil {
		return nil, fmt.Errorf("failed to remove member: %w", err)
	}

	h.logger.InfoContext(ctx, "User unsubscribed from list",
		"list", listAddress,
		"email", senderEmail,
	)

	return &CommandResult{
		Success:      true,
		Message:      "Successfully unsubscribed",
		ResponseData: h.generateGoodbyeEmail(list, senderEmail),
	}, nil
}

// handleHelp sends list information
func (h *CommandHandler) handleHelp(ctx context.Context, listAddress, senderEmail string) (*CommandResult, error) {
	list, err := h.store.GetListByAddress(ctx, listAddress)
	if err != nil {
		return nil, fmt.Errorf("list not found: %w", err)
	}

	return &CommandResult{
		Success:      true,
		Message:      "Help information sent",
		ResponseData: h.generateHelpEmail(list, senderEmail),
	}, nil
}

// handleConfirm processes a confirmation token
func (h *CommandHandler) handleConfirm(ctx context.Context, token, senderEmail string) (*CommandResult, error) {
	action, err := h.store.GetPendingAction(ctx, token)
	if err != nil {
		return &CommandResult{
			Success: false,
			Message: "Invalid or expired confirmation token",
		}, nil
	}

	// Check expiration
	if time.Now().After(action.ExpiresAt) {
		h.store.DeletePendingAction(ctx, token)
		return &CommandResult{
			Success: false,
			Message: "Confirmation token has expired",
		}, nil
	}

	// Verify sender matches
	if strings.ToLower(senderEmail) != strings.ToLower(action.Email) {
		return &CommandResult{
			Success: false,
			Message: "Confirmation must be sent from the same address",
		}, nil
	}

	list, err := h.store.GetList(ctx, action.ListID)
	if err != nil {
		return nil, err
	}

	// Process the action
	switch action.Action {
	case "subscribe":
		member := &ListMember{
			ListID:       list.ID,
			Email:        action.Email,
			Role:         RoleMember,
			DeliveryMode: DeliveryNormal,
			IsConfirmed:  true,
		}

		if err := h.store.AddMember(ctx, member); err != nil {
			if err == ErrAlreadyExists {
				h.store.ConfirmMember(ctx, list.ID, action.Email)
			} else {
				return nil, fmt.Errorf("failed to add member: %w", err)
			}
		}

		h.store.DeletePendingAction(ctx, token)

		h.logger.InfoContext(ctx, "Subscription confirmed",
			"list", list.ListAddress,
			"email", action.Email,
		)

		return &CommandResult{
			Success:      true,
			Message:      "Subscription confirmed",
			ResponseData: h.generateWelcomeEmail(list, action.Email),
		}, nil

	case "unsubscribe":
		if err := h.store.RemoveMember(ctx, list.ID, action.Email); err != nil && err != ErrNotFound {
			return nil, fmt.Errorf("failed to remove member: %w", err)
		}

		h.store.DeletePendingAction(ctx, token)

		h.logger.InfoContext(ctx, "Unsubscription confirmed",
			"list", list.ListAddress,
			"email", action.Email,
		)

		return &CommandResult{
			Success:      true,
			Message:      "Unsubscription confirmed",
			ResponseData: h.generateGoodbyeEmail(list, action.Email),
		}, nil

	default:
		return &CommandResult{
			Success: false,
			Message: "Unknown action type",
		}, nil
	}
}

// Email generation helpers

func (h *CommandHandler) generateConfirmationEmail(list *MailingList, toEmail, action, token string) []byte {
	domain := strings.SplitN(list.ListAddress, "@", 2)[1]
	confirmAddr := fmt.Sprintf("%s-confirm-%s@%s", list.LocalPart, token, domain)

	var subject, body string
	if action == "subscribe" {
		subject = fmt.Sprintf("Confirm your subscription to %s", list.Name)
		body = fmt.Sprintf(`Hello,

You have requested to subscribe to the mailing list:
  %s (%s)

To confirm your subscription, please reply to this email or send an
email to:
  %s

This confirmation request will expire in 48 hours.

If you did not request this subscription, you can safely ignore this email.

--
%s Mailing List Manager
`, list.Name, list.ListAddress, confirmAddr, h.hostname)
	} else {
		subject = fmt.Sprintf("Confirm your unsubscription from %s", list.Name)
		body = fmt.Sprintf(`Hello,

You have requested to unsubscribe from the mailing list:
  %s (%s)

To confirm your unsubscription, please reply to this email or send an
email to:
  %s

This confirmation request will expire in 48 hours.

If you did not request this unsubscription, you can safely ignore this email.

--
%s Mailing List Manager
`, list.Name, list.ListAddress, confirmAddr, h.hostname)
	}

	return h.buildEmail(list.ListAddress, toEmail, subject, body, confirmAddr)
}

func (h *CommandHandler) generateWelcomeEmail(list *MailingList, toEmail string) []byte {
	domain := strings.SplitN(list.ListAddress, "@", 2)[1]

	postingInfo := ""
	switch list.PostingPolicy {
	case PostingAny:
		postingInfo = "Anyone can post messages to this list."
	case PostingMembersOnly:
		postingInfo = "Only members can post messages to this list."
	case PostingOwnersOnly:
		postingInfo = "Only list owners can post messages to this list (announcement list)."
	}

	body := fmt.Sprintf(`Welcome to %s!

You have been successfully subscribed to:
  %s

List Description:
  %s

%s

To post a message to the list, send email to:
  %s

To unsubscribe, send email to:
  %s-unsubscribe@%s

For help, send email to:
  %s-help@%s

--
%s Mailing List Manager
`, list.Name, list.ListAddress, list.Description, postingInfo, list.ListAddress,
		list.LocalPart, domain, list.LocalPart, domain, h.hostname)

	return h.buildEmail(list.ListAddress, toEmail, fmt.Sprintf("Welcome to %s", list.Name), body, "")
}

func (h *CommandHandler) generateGoodbyeEmail(list *MailingList, toEmail string) []byte {
	domain := strings.SplitN(list.ListAddress, "@", 2)[1]

	body := fmt.Sprintf(`You have been unsubscribed from:
  %s (%s)

You will no longer receive messages from this list.

If you wish to resubscribe in the future, send email to:
  %s-subscribe@%s

--
%s Mailing List Manager
`, list.Name, list.ListAddress, list.LocalPart, domain, h.hostname)

	return h.buildEmail(list.ListAddress, toEmail, fmt.Sprintf("Unsubscribed from %s", list.Name), body, "")
}

func (h *CommandHandler) generateHelpEmail(list *MailingList, toEmail string) []byte {
	domain := strings.SplitN(list.ListAddress, "@", 2)[1]

	listType := "Discussion List"
	if list.ListType == ListTypeAnnouncement {
		listType = "Announcement List"
	}

	postingInfo := ""
	switch list.PostingPolicy {
	case PostingAny:
		postingInfo = "Anyone can post messages to this list."
	case PostingMembersOnly:
		postingInfo = "Only members can post messages to this list."
	case PostingOwnersOnly:
		postingInfo = "Only list owners can post messages (announcement list)."
	}

	moderationInfo := "Messages are delivered immediately."
	if list.ModerationEnabled {
		moderationInfo = "Messages are moderated before delivery."
	}

	body := fmt.Sprintf(`%s - Help Information

List Address: %s
List Type: %s
Description: %s

POSTING POLICY
%s
%s

COMMANDS
To subscribe:    %s-subscribe@%s
To unsubscribe:  %s-unsubscribe@%s
To post:         %s
For help:        %s-help@%s
Contact owner:   %s-owner@%s

--
%s Mailing List Manager
`, list.Name, list.ListAddress, listType, list.Description,
		postingInfo, moderationInfo,
		list.LocalPart, domain,
		list.LocalPart, domain,
		list.ListAddress,
		list.LocalPart, domain,
		list.LocalPart, domain,
		h.hostname)

	return h.buildEmail(list.ListAddress, toEmail, fmt.Sprintf("Help: %s", list.Name), body, "")
}

func (h *CommandHandler) generateErrorEmail(list *MailingList, toEmail, subject, message string) []byte {
	body := fmt.Sprintf(`%s

If you believe this is an error, please contact the list owner at:
  %s-owner@%s

--
%s Mailing List Manager
`, message, list.LocalPart, strings.SplitN(list.ListAddress, "@", 2)[1], h.hostname)

	return h.buildEmail(list.ListAddress, toEmail, subject, body, "")
}

func (h *CommandHandler) buildEmail(from, to, subject, body, replyTo string) []byte {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", to)
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&buf, "Message-ID: <%s@%s>\r\n", h.generateToken()[:16], h.hostname)
	fmt.Fprintf(&buf, "Content-Type: text/plain; charset=UTF-8\r\n")
	if replyTo != "" {
		fmt.Fprintf(&buf, "Reply-To: %s\r\n", replyTo)
	}
	fmt.Fprintf(&buf, "Auto-Submitted: auto-replied\r\n")
	fmt.Fprintf(&buf, "Precedence: bulk\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(body)

	return buf.Bytes()
}

func (h *CommandHandler) generateToken() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
