package api

import (
	"time"
)

// APIKey represents an API key for authentication
type APIKey struct {
	ID               int64      `json:"id"`
	DomainID         int64      `json:"domain_id"`
	KeyHash          string     `json:"-"` // Never expose hash
	KeyPrefix        string     `json:"key_prefix"`
	Name             string     `json:"name"`
	Scopes           []string   `json:"scopes"`
	IsActive         bool       `json:"is_active"`
	RateLimitPerHour int        `json:"rate_limit_per_hour"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

// EmailTemplate represents an email template
type EmailTemplate struct {
	ID        int64     `json:"id"`
	DomainID  int64     `json:"domain_id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Subject   string    `json:"subject"`
	HTMLBody  string    `json:"html_body,omitempty"`
	TextBody  string    `json:"text_body,omitempty"`
	Variables []string  `json:"variables,omitempty"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SentEmail represents a sent email record
type SentEmail struct {
	ID           int64      `json:"id"`
	DomainID     int64      `json:"domain_id"`
	APIKeyID     *int64     `json:"api_key_id,omitempty"`
	MessageID    string     `json:"message_id"`
	TrackingID   string     `json:"tracking_id"`
	FromEmail    string     `json:"from_email"`
	ToEmail      string     `json:"to_email"`
	Subject      string     `json:"subject,omitempty"`
	TemplateSlug string     `json:"template_slug,omitempty"`
	Tags         []string   `json:"tags,omitempty"`
	Status       string     `json:"status"`
	SMTPResponse string     `json:"smtp_response,omitempty"`
	OpenedAt     *time.Time `json:"opened_at,omitempty"`
	OpenedCount  int        `json:"opened_count"`
	ClickedAt    *time.Time `json:"clicked_at,omitempty"`
	ClickedCount int        `json:"clicked_count"`
	CreatedAt    time.Time  `json:"created_at"`
	DeliveredAt  *time.Time `json:"delivered_at,omitempty"`
	BouncedAt    *time.Time `json:"bounced_at,omitempty"`
	BounceReason string     `json:"bounce_reason,omitempty"`
}

// Webhook represents a webhook endpoint
type Webhook struct {
	ID                int64      `json:"id"`
	DomainID          int64      `json:"domain_id"`
	URL               string     `json:"url"`
	Events            []string   `json:"events"`
	Secret            string     `json:"-"` // Never expose in responses
	IsActive          bool       `json:"is_active"`
	FailureCount      int        `json:"failure_count"`
	LastTriggeredAt   *time.Time `json:"last_triggered_at,omitempty"`
	LastSuccessAt     *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt     *time.Time `json:"last_failure_at,omitempty"`
	LastFailureReason string     `json:"last_failure_reason,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

// Attachment represents an email attachment
type Attachment struct {
	Filename    string `json:"filename"`               // Required: filename with extension
	Content     string `json:"content"`                // Required: base64-encoded content
	ContentType string `json:"content_type,omitempty"` // Optional: MIME type (auto-detected if empty)
}

// Priority levels for email sending
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// SendEmailRequest is the request body for sending an email
type SendEmailRequest struct {
	IdempotencyKey string            `json:"idempotency_key,omitempty"` // Prevents duplicate sends on retries
	From           string            `json:"from"`
	To             string            `json:"to"`
	Subject        string            `json:"subject"`
	HTML           string            `json:"html,omitempty"`
	Text           string            `json:"text,omitempty"`
	Variables      map[string]string `json:"variables,omitempty"`
	Tags           []string          `json:"tags,omitempty"`
	TrackOpens     bool              `json:"track_opens"`
	TrackClicks    bool              `json:"track_clicks"`
	ReplyTo        string            `json:"reply_to,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Attachments    []Attachment      `json:"attachments,omitempty"` // Optional: file attachments
	Priority       Priority          `json:"priority,omitempty"`    // Queue priority: high, normal, low
	ScheduledAt    *time.Time        `json:"scheduled_at,omitempty"` // Send at a future time (up to 30 days)
}

// SendTemplateRequest is the request body for sending with a template
type SendTemplateRequest struct {
	From       string            `json:"from"`
	To         string            `json:"to"`
	Template   string            `json:"template"`
	Variables  map[string]string `json:"variables,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	ReplyTo    string            `json:"reply_to,omitempty"`
	TrackOpens *bool             `json:"track_opens,omitempty"`
	TrackClick *bool             `json:"track_clicks,omitempty"`
}

// BatchSendRequest is the request body for batch sending
type BatchSendRequest struct {
	From     string              `json:"from"`
	Messages []BatchEmailMessage `json:"messages"`
}

// BatchEmailMessage represents a single message in a batch
type BatchEmailMessage struct {
	To          string            `json:"to"`
	Subject     string            `json:"subject,omitempty"`
	HTML        string            `json:"html,omitempty"`
	Text        string            `json:"text,omitempty"`
	Variables   map[string]string `json:"variables,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"` // Optional: file attachments
}

// SendResponse is the response for a successful send
type SendResponse struct {
	Success           bool       `json:"success"`
	MessageID         string     `json:"message_id"`
	Status            string     `json:"status"`
	ScheduledAt       *time.Time `json:"scheduled_at,omitempty"`       // When the email will be sent (for scheduled emails)
	IdempotentReplayed bool       `json:"idempotent_replayed,omitempty"` // True if this is a replayed response
}

// BatchSendResponse is the response for batch sending
type BatchSendResponse struct {
	Success  bool            `json:"success"`
	Messages []SendResponse  `json:"messages"`
	Errors   []BatchError    `json:"errors,omitempty"`
}

// BatchError represents an error in batch sending
type BatchError struct {
	Index   int    `json:"index"`
	To      string `json:"to"`
	Message string `json:"message"`
}

// APIError represents an API error response
type APIError struct {
	Error     string `json:"error"`
	Code      string `json:"code,omitempty"`
	Details   string `json:"details,omitempty"`
	Field     string `json:"field,omitempty"`      // Field that caused the error (for validation errors)
	RequestID string `json:"request_id,omitempty"` // Unique request ID for tracing
}

// StatsResponse holds email sending statistics
type StatsResponse struct {
	Period    string     `json:"period"`
	Sent      int64      `json:"sent"`
	Delivered int64      `json:"delivered"`
	Opened    int64      `json:"opened"`
	Clicked   int64      `json:"clicked"`
	Bounced   int64      `json:"bounced"`
	Failed    int64      `json:"failed"`
	StartDate time.Time  `json:"start_date"`
	EndDate   time.Time  `json:"end_date"`
}

// WebhookEvent represents a webhook event payload
type WebhookEvent struct {
	Event     string                 `json:"event"`
	Timestamp time.Time              `json:"timestamp"`
	MessageID string                 `json:"message_id"`
	Recipient string                 `json:"recipient"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// CreateAPIKeyRequest is the request for creating a new API key
type CreateAPIKeyRequest struct {
	Name             string   `json:"name"`
	Scopes           []string `json:"scopes,omitempty"`
	RateLimitPerHour int      `json:"rate_limit_per_hour,omitempty"`
	ExpiresInDays    int      `json:"expires_in_days,omitempty"`
}

// CreateAPIKeyResponse includes the full key (only shown once)
type CreateAPIKeyResponse struct {
	APIKey  *APIKey `json:"api_key"`
	FullKey string  `json:"key"` // Only shown on creation
}

// CreateTemplateRequest is the request for creating a template
type CreateTemplateRequest struct {
	Slug      string   `json:"slug"`
	Name      string   `json:"name"`
	Subject   string   `json:"subject"`
	HTMLBody  string   `json:"html_body,omitempty"`
	TextBody  string   `json:"text_body,omitempty"`
	Variables []string `json:"variables,omitempty"`
}

// CreateWebhookRequest is the request for creating a webhook
type CreateWebhookRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

// ListResponse is a generic list response with pagination
type ListResponse struct {
	Data       interface{} `json:"data"`
	Total      int64       `json:"total"`
	Page       int         `json:"page"`
	PerPage    int         `json:"per_page"`
	TotalPages int         `json:"total_pages"`
}

// Email status constants
const (
	StatusQueued     = "queued"
	StatusScheduled  = "scheduled"
	StatusSent       = "sent"
	StatusDelivered  = "delivered"
	StatusBounced    = "bounced"
	StatusFailed     = "failed"
	StatusOpened     = "opened"
	StatusClicked    = "clicked"
	StatusSuppressed = "suppressed"
)

// Suppression represents a suppressed email address
type Suppression struct {
	ID        int64     `json:"id"`
	DomainID  int64     `json:"domain_id"`
	Email     string    `json:"email"`
	Reason    string    `json:"reason"` // hard_bounce, unsubscribe, complaint, manual
	CreatedAt time.Time `json:"created_at"`
}

// SuppressionReason constants
const (
	SuppressionHardBounce  = "hard_bounce"
	SuppressionUnsubscribe = "unsubscribe"
	SuppressionComplaint   = "complaint"
	SuppressionManual      = "manual"
)

// CreateSuppressionRequest is the request for adding an email to suppression list
type CreateSuppressionRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// DeliveryAttempt represents a single delivery attempt for an email
type DeliveryAttempt struct {
	Attempt      int       `json:"attempt"`
	AttemptedAt  time.Time `json:"attempted_at"`
	Status       string    `json:"status"` // pending, sent, deferred, failed
	SMTPResponse string    `json:"smtp_response,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

// EnhancedSentEmail extends SentEmail with delivery attempt history
type EnhancedSentEmail struct {
	SentEmail
	DeliveryAttempts []DeliveryAttempt `json:"delivery_attempts,omitempty"`
	Priority         Priority          `json:"priority,omitempty"`
	ScheduledAt      *time.Time        `json:"scheduled_at,omitempty"`
}

// ScheduledEmail represents a scheduled email in the database
type ScheduledEmail struct {
	ID             int64      `json:"id"`
	DomainID       int64      `json:"domain_id"`
	APIKeyID       int64      `json:"api_key_id"`
	MessageID      string     `json:"message_id"`
	RequestPayload string     `json:"-"` // JSON serialized SendEmailRequest
	ScheduledAt    time.Time  `json:"scheduled_at"`
	Status         string     `json:"status"` // pending, sent, cancelled
	CreatedAt      time.Time  `json:"created_at"`
	ProcessedAt    *time.Time `json:"processed_at,omitempty"`
}

// Webhook event types
const (
	EventQueued    = "queued"
	EventSent      = "sent"
	EventDelivered = "delivered"
	EventBounced   = "bounced"
	EventOpened    = "opened"
	EventClicked   = "clicked"
	EventFailed    = "failed"
)

// ResendRequest is the request body for resending an email
type ResendRequest struct {
	HTML      string            `json:"html,omitempty"`
	Text      string            `json:"text,omitempty"`
	Variables map[string]string `json:"variables,omitempty"` // Template variables for re-rendering
	Force     bool              `json:"force,omitempty"`     // Bypass suppression check
}

// ResendResponse is the response for a successful resend
type ResendResponse struct {
	Success      bool   `json:"success"`
	MessageID    string `json:"message_id"`
	Status       string `json:"status"`
	OriginalID   string `json:"original_message_id"`
}

// API scopes
const (
	ScopeSend      = "send"
	ScopeTemplates = "templates"
	ScopeStats     = "stats"
	ScopeWebhooks  = "webhooks"
	ScopeAdmin     = "admin"
)
