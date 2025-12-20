package imap

import (
	"log"
	"sync"
	"sync/atomic"

	"github.com/emersion/go-imap/backend"
)

// UpdateHub manages IMAP IDLE updates for multiple clients
type UpdateHub struct {
	mu             sync.RWMutex
	clients        map[chan backend.Update]*clientState
	updateCh       chan backend.Update
	closed         atomic.Bool
	wg             sync.WaitGroup
	droppedUpdates int64 // atomic counter for dropped updates
}

// clientState tracks the state of a subscribed client
type clientState struct {
	ch     chan backend.Update
	closed atomic.Bool
}

// NewUpdateHub creates a new update hub
func NewUpdateHub() *UpdateHub {
	hub := &UpdateHub{
		clients:  make(map[chan backend.Update]*clientState),
		updateCh: make(chan backend.Update, 10000), // Large buffer for instant notifications
	}

	hub.wg.Add(1)
	go hub.run()
	return hub
}

// Updates returns the update channel for the backend
func (h *UpdateHub) Updates() <-chan backend.Update {
	return h.updateCh
}

// Notify sends an update to all listening clients
func (h *UpdateHub) Notify(update backend.Update) {
	if h.closed.Load() {
		return
	}

	select {
	case h.updateCh <- update:
	default:
		// Channel full, drop update and track it
		count := atomic.AddInt64(&h.droppedUpdates, 1)
		// Log every 100th drop to avoid log spam
		if count%100 == 1 {
			log.Printf("WARNING: IMAP update channel full, dropped %d updates total", count)
		}
	}
}

// Subscribe registers a new client for updates
func (h *UpdateHub) Subscribe() chan backend.Update {
	if h.closed.Load() {
		// Return a closed channel if hub is closed
		ch := make(chan backend.Update)
		close(ch)
		return ch
	}

	ch := make(chan backend.Update, 1000) // Large buffer for fast delivery
	state := &clientState{ch: ch}

	h.mu.Lock()
	h.clients[ch] = state
	h.mu.Unlock()

	return ch
}

// Unsubscribe removes a client from updates
func (h *UpdateHub) Unsubscribe(ch chan backend.Update) {
	h.mu.Lock()
	state, exists := h.clients[ch]
	if exists {
		delete(h.clients, ch)
		// Mark as closed before releasing lock
		state.closed.Store(true)
	}
	h.mu.Unlock()

	if exists {
		// Close the channel - the for range in run() will exit naturally
		// No drain goroutine needed; closing handles cleanup
		close(ch)
	}
}

// Close gracefully shuts down the update hub
func (h *UpdateHub) Close() {
	if !h.closed.CompareAndSwap(false, true) {
		return // Already closed
	}

	// Close all client channels
	h.mu.Lock()
	for ch, state := range h.clients {
		state.closed.Store(true)
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()

	// Close the main update channel to stop run()
	close(h.updateCh)

	// Wait for run() to finish
	h.wg.Wait()
}

// run distributes updates to all subscribed clients
func (h *UpdateHub) run() {
	defer h.wg.Done()

	for update := range h.updateCh {
		h.mu.RLock()
		for ch, state := range h.clients {
			// Check if client is still active
			if state.closed.Load() {
				continue
			}

			select {
			case ch <- update:
			default:
				// Client channel full, skip this update
				// The client should drain their channel
			}
		}
		h.mu.RUnlock()
	}
}

// ClientCount returns the number of subscribed clients
func (h *UpdateHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
