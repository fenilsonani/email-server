package imap

import (
	"sync"

	"github.com/emersion/go-imap/backend"
)

// UpdateHub manages IMAP IDLE updates for multiple clients
type UpdateHub struct {
	mu       sync.RWMutex
	clients  map[chan backend.Update]struct{}
	updateCh chan backend.Update
}

// NewUpdateHub creates a new update hub
func NewUpdateHub() *UpdateHub {
	hub := &UpdateHub{
		clients:  make(map[chan backend.Update]struct{}),
		updateCh: make(chan backend.Update, 100),
	}

	go hub.run()
	return hub
}

// Updates returns the update channel for the backend
func (h *UpdateHub) Updates() <-chan backend.Update {
	return h.updateCh
}

// Notify sends an update to all listening clients
func (h *UpdateHub) Notify(update backend.Update) {
	select {
	case h.updateCh <- update:
	default:
		// Channel full, drop update
	}
}

// Subscribe registers a new client for updates
func (h *UpdateHub) Subscribe() chan backend.Update {
	ch := make(chan backend.Update, 10)

	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	return ch
}

// Unsubscribe removes a client from updates
func (h *UpdateHub) Unsubscribe(ch chan backend.Update) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()

	close(ch)
}

// run distributes updates to all subscribed clients
func (h *UpdateHub) run() {
	for update := range h.updateCh {
		h.mu.RLock()
		for client := range h.clients {
			select {
			case client <- update:
			default:
				// Client channel full, skip
			}
		}
		h.mu.RUnlock()
	}
}
