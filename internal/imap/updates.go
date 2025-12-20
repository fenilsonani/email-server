package imap

import (
	"log"
	"sync/atomic"

	"github.com/emersion/go-imap/backend"
)

// UpdateHub manages IMAP IDLE updates
// go-imap server reads directly from the Updates() channel
// We just need to send updates to that channel - go-imap handles the rest
type UpdateHub struct {
	updateCh       chan backend.Update
	closed         atomic.Bool
	droppedUpdates int64 // atomic counter for dropped updates
}

// NewUpdateHub creates a new update hub
func NewUpdateHub() *UpdateHub {
	return &UpdateHub{
		// Large buffer so sends never block
		// go-imap server is the only reader
		updateCh: make(chan backend.Update, 10000),
	}
}

// Updates returns the update channel for go-imap server to read from
// go-imap handles routing updates to IDLE clients internally
func (h *UpdateHub) Updates() <-chan backend.Update {
	return h.updateCh
}

// Notify sends an update to the go-imap server
func (h *UpdateHub) Notify(update backend.Update) {
	if h.closed.Load() {
		return
	}

	select {
	case h.updateCh <- update:
		// Successfully sent - log for debugging
		log.Printf("IDLE: Update sent to channel (buffer: %d/%d)", len(h.updateCh), cap(h.updateCh))
	default:
		// Channel full, drop update and track it
		count := atomic.AddInt64(&h.droppedUpdates, 1)
		// Log every 100th drop to avoid log spam
		if count%100 == 1 {
			log.Printf("WARNING: IMAP update channel full, dropped %d updates total", count)
		}
	}
}

// Close gracefully shuts down the update hub
func (h *UpdateHub) Close() {
	if !h.closed.CompareAndSwap(false, true) {
		return // Already closed
	}
	close(h.updateCh)
}
