package features

import (
	"context"
	"sync"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
)

// EmailSender is the interface for sending emails
type EmailSender interface {
	SendEmail(ctx context.Context, from string, to []string, subject, body, htmlBody string, headers map[string]string) error
}

// MessageMover is the interface for moving messages between mailboxes
type MessageMover interface {
	MoveMessageToMailbox(ctx context.Context, userID, messageID, targetMailboxID int64, markUnread bool) error
}

// Scheduler handles time-based feature operations
type Scheduler struct {
	store        *Store
	emailSender  EmailSender
	messageMover MessageMover
	logger       *logging.Logger

	stopCh    chan struct{}
	wg        sync.WaitGroup
	isRunning bool
	mu        sync.Mutex
}

// NewScheduler creates a new feature scheduler
func NewScheduler(store *Store, logger *logging.Logger) *Scheduler {
	return &Scheduler{
		store:  store,
		logger: logger,
		stopCh: make(chan struct{}),
	}
}

// SetEmailSender sets the email sender for scheduled sends
func (s *Scheduler) SetEmailSender(sender EmailSender) {
	s.emailSender = sender
}

// SetMessageMover sets the message mover for snooze wake-ups
func (s *Scheduler) SetMessageMover(mover MessageMover) {
	s.messageMover = mover
}

// Start begins the scheduler loops
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isRunning {
		return
	}
	s.isRunning = true
	s.stopCh = make(chan struct{})

	// Start scheduled email processor
	s.wg.Add(1)
	go s.processScheduledEmails()

	// Start snooze processor
	s.wg.Add(1)
	go s.processSnoozedEmails()

	// Start pending send processor (undo send)
	s.wg.Add(1)
	go s.processPendingSends()

	s.logger.Info("Feature scheduler started")
}

// Stop gracefully stops the scheduler
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return
	}

	close(s.stopCh)
	s.wg.Wait()
	s.isRunning = false
	s.logger.Info("Feature scheduler stopped")
}

// processScheduledEmails checks for and sends scheduled emails
func (s *Scheduler) processScheduledEmails() {
	defer s.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.sendDueScheduledEmails()
		}
	}
}

// sendDueScheduledEmails sends all emails that are due
func (s *Scheduler) sendDueScheduledEmails() {
	if s.emailSender == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	emails, err := s.store.GetPendingScheduledEmails(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to get due scheduled emails", err)
		return
	}

	for _, email := range emails {
		// Mark as sending
		if err := s.store.UpdateScheduledEmailStatus(ctx, email.ID, ScheduledStatusSending, ""); err != nil {
			s.logger.ErrorContext(ctx, "Failed to mark scheduled email as sending", err,
				"email_id", email.ID,
			)
			continue
		}

		// Send the email
		err := s.emailSender.SendEmail(ctx, email.FromAddress, email.Recipients,
			email.Subject, email.Body, email.HTMLBody, email.Headers)

		if err != nil {
			// Mark as failed
			s.store.UpdateScheduledEmailStatus(ctx, email.ID, ScheduledStatusFailed, err.Error())
			s.logger.ErrorContext(ctx, "Failed to send scheduled email", err,
				"email_id", email.ID,
				"from", email.FromAddress,
			)
		} else {
			// Mark as sent
			s.store.UpdateScheduledEmailStatus(ctx, email.ID, ScheduledStatusSent, "")
			s.logger.InfoContext(ctx, "Scheduled email sent successfully",
				"email_id", email.ID,
				"from", email.FromAddress,
				"to", email.Recipients,
			)
		}
	}
}

// processSnoozedEmails checks for and wakes snoozed emails
func (s *Scheduler) processSnoozedEmails() {
	defer s.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.wakeDueSnoozedEmails()
		}
	}
}

// wakeDueSnoozedEmails wakes all snoozed emails that are due
func (s *Scheduler) wakeDueSnoozedEmails() {
	if s.messageMover == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	snoozed, err := s.store.GetReadySnoozedEmails(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to get due snoozed emails", err)
		return
	}

	for _, snz := range snoozed {
		// Move message back to original mailbox
		err := s.messageMover.MoveMessageToMailbox(ctx, snz.UserID, snz.MessageID,
			snz.OriginalMailboxID, snz.MarkUnread)

		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to wake snoozed email", err,
				"snooze_id", snz.ID,
				"message_id", snz.MessageID,
			)
			continue
		}

		// Delete the snooze record
		if err := s.store.DeleteSnoozedEmail(ctx, snz.UserID, snz.ID); err != nil {
			s.logger.WarnContext(ctx, "Failed to delete snooze record after wake",
				"snooze_id", snz.ID,
				"error", err.Error(),
			)
		}

		s.logger.InfoContext(ctx, "Snoozed email woken up",
			"snooze_id", snz.ID,
			"message_id", snz.MessageID,
			"user_id", snz.UserID,
		)
	}
}

// processPendingSends checks for and sends pending emails (undo send feature)
func (s *Scheduler) processPendingSends() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Second) // Check every second for undo send
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.sendDuePendingSends()
		}
	}
}

// sendDuePendingSends sends all pending emails that have passed their delay
func (s *Scheduler) sendDuePendingSends() {
	if s.emailSender == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pending, err := s.store.GetReadyPendingSends(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to get due pending sends", err)
		return
	}

	for _, p := range pending {
		// Send the email
		err := s.emailSender.SendEmail(ctx, p.FromAddress, p.Recipients,
			p.Subject, p.Body, p.HTMLBody, p.Headers)

		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to send pending email", err,
				"pending_id", p.ID,
				"from", p.FromAddress,
			)
			// Keep it in pending state for retry? Or mark as failed?
			// For now, delete it anyway to prevent infinite retries
		} else {
			s.logger.InfoContext(ctx, "Pending email sent successfully",
				"pending_id", p.ID,
				"from", p.FromAddress,
				"to", p.Recipients,
			)
		}

		// Delete the pending record (sent or failed)
		if err := s.store.DeletePendingSend(ctx, p.ID); err != nil {
			s.logger.WarnContext(ctx, "Failed to delete pending send after processing",
				"pending_id", p.ID,
				"error", err.Error(),
			)
		}
	}
}
