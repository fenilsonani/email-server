package sieve

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Action is an interface for Sieve actions
type Action interface {
	Apply(ctx context.Context, result *Result, msg *Message, vacationStore *VacationStore, userID int64) error
}

// KeepAction delivers message to INBOX (default action)
type KeepAction struct{}

func (a *KeepAction) Apply(ctx context.Context, result *Result, msg *Message, vs *VacationStore, userID int64) error {
	result.Keep = true
	return nil
}

// FileIntoAction delivers message to a specific folder
type FileIntoAction struct {
	Folder string
}

func (a *FileIntoAction) Apply(ctx context.Context, result *Result, msg *Message, vs *VacationStore, userID int64) error {
	if a == nil || result == nil {
		return fmt.Errorf("action or result is nil")
	}
	result.Filed = true
	result.FileInto = a.Folder
	result.Keep = false
	return nil
}

// RedirectAction forwards message to another address
type RedirectAction struct {
	Address string
}

func (a *RedirectAction) Apply(ctx context.Context, result *Result, msg *Message, vs *VacationStore, userID int64) error {
	if a == nil || result == nil {
		return fmt.Errorf("action or result is nil")
	}
	result.Redirected = true
	result.RedirectTo = append(result.RedirectTo, a.Address)
	result.Keep = false
	return nil
}

// DiscardAction silently discards the message
type DiscardAction struct{}

func (a *DiscardAction) Apply(ctx context.Context, result *Result, msg *Message, vs *VacationStore, userID int64) error {
	result.Discarded = true
	result.Keep = false
	return nil
}

// RejectAction rejects the message with a reason
type RejectAction struct {
	Message string
}

func (a *RejectAction) Apply(ctx context.Context, result *Result, msg *Message, vs *VacationStore, userID int64) error {
	result.Rejected = true
	result.RejectMsg = a.Message
	result.Keep = false
	return nil
}

// StopAction stops processing more rules
type StopAction struct{}

func (a *StopAction) Apply(ctx context.Context, result *Result, msg *Message, vs *VacationStore, userID int64) error {
	// Stop is handled in the executor by setting rule.Stop
	return nil
}

// VacationAction sends an automatic vacation response
type VacationAction struct {
	Days      int      // Minimum days between responses to same sender
	Subject   string   // Response subject
	From      string   // Response from address (optional)
	Body      string   // Response body
	Addresses []string // Additional addresses that belong to user
	Handle    string   // Unique identifier for this vacation
}

func (a *VacationAction) Apply(ctx context.Context, result *Result, msg *Message, vs *VacationStore, userID int64) error {
	if a == nil || result == nil || msg == nil {
		return fmt.Errorf("action, result, or message is nil")
	}

	// Don't reply to messages that shouldn't get vacation responses
	if shouldSkipVacation(msg) {
		return nil
	}

	// Check rate limiting
	if vs != nil {
		days := a.Days
		if days <= 0 {
			days = 7 // default
		}

		shouldRespond, err := vs.ShouldRespond(ctx, userID, msg.From, days)
		if err != nil {
			return err
		}

		if !shouldRespond {
			return nil // Already responded recently
		}

		// Record this response
		if err := vs.RecordResponse(ctx, userID, msg.From); err != nil {
			return err
		}
	}

	result.Vacation = true
	result.VacationTo = msg.From
	result.VacationSubject = a.Subject
	if result.VacationSubject == "" {
		result.VacationSubject = "Re: " + msg.Subject
	}
	result.VacationBody = a.Body

	return nil
}

// shouldSkipVacation checks if we should skip sending vacation response
func shouldSkipVacation(msg *Message) bool {
	from := strings.ToLower(msg.From)

	// Skip noreply/mailer-daemon addresses
	skipPrefixes := []string{
		"noreply@", "no-reply@", "donotreply@", "do-not-reply@",
		"mailer-daemon@", "postmaster@", "bounces@", "bounce@",
	}
	for _, prefix := range skipPrefixes {
		if strings.Contains(from, prefix) {
			return true
		}
	}

	// Check for mailing list indicators
	if msg.Headers != nil {
		// Precedence header
		if precedence, ok := msg.Headers["Precedence"]; ok {
			for _, p := range precedence {
				p = strings.ToLower(p)
				if p == "bulk" || p == "list" || p == "junk" {
					return true
				}
			}
		}

		// List-Id header (indicates mailing list)
		if _, ok := msg.Headers["List-Id"]; ok {
			return true
		}

		// List-Unsubscribe header
		if _, ok := msg.Headers["List-Unsubscribe"]; ok {
			return true
		}

		// Auto-Submitted header (indicates automated message)
		if autoSubmitted, ok := msg.Headers["Auto-Submitted"]; ok {
			for _, as := range autoSubmitted {
				if strings.ToLower(as) != "no" {
					return true
				}
			}
		}

		// X-Auto-Response-Suppress header
		if _, ok := msg.Headers["X-Auto-Response-Suppress"]; ok {
			return true
		}
	}

	return false
}

// VacationStore handles vacation response rate limiting
type VacationStore struct {
	db *sql.DB
}

// NewVacationStore creates a new vacation store
func NewVacationStore(db *sql.DB) *VacationStore {
	return &VacationStore{db: db}
}

// ShouldRespond checks if we should send a vacation response to this sender
func (s *VacationStore) ShouldRespond(ctx context.Context, userID int64, senderAddr string, days int) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("vacation store or database is nil")
	}

	senderAddr = extractAddressPart(senderAddr, "all")
	senderAddr = strings.ToLower(senderAddr)

	// Check for recent response
	var respondedAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT responded_at FROM vacation_responses
		WHERE user_id = ? AND sender_address = ?
	`, userID, senderAddr).Scan(&respondedAt)

	if err == sql.ErrNoRows {
		// No previous response found
		return true, nil
	}
	if err != nil {
		// Database error
		return false, err
	}

	// Check if enough time has passed
	minInterval := time.Duration(days) * 24 * time.Hour
	return time.Since(respondedAt) > minInterval, nil
}

// RecordResponse records that we sent a vacation response to this sender
func (s *VacationStore) RecordResponse(ctx context.Context, userID int64, senderAddr string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("vacation store or database is nil")
	}

	senderAddr = extractAddressPart(senderAddr, "all")
	senderAddr = strings.ToLower(senderAddr)

	// Upsert the response record
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO vacation_responses (user_id, sender_address, responded_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT (user_id, sender_address) DO UPDATE SET responded_at = CURRENT_TIMESTAMP
	`, userID, senderAddr)

	return err
}

// CleanupOldResponses removes old vacation response records
func (s *VacationStore) CleanupOldResponses(ctx context.Context, maxAge time.Duration) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("vacation store or database is nil")
	}

	cutoff := time.Now().Add(-maxAge)
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM vacation_responses WHERE responded_at < ?
	`, cutoff)
	return err
}
