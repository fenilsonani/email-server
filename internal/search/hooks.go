package search

import (
	"context"
	"sync"
)

// MessageHook is called when messages are added or removed.
type MessageHook func(ctx context.Context, mailboxID int64, uid uint32) error

// Hooks manages search-related event hooks.
type Hooks struct {
	onAppend  []MessageHook
	onExpunge []MessageHook
	mu        sync.RWMutex
}

// GlobalHooks is the global hooks instance.
var GlobalHooks = &Hooks{}

// OnAppend registers a hook to be called when a message is appended.
func (h *Hooks) OnAppend(hook MessageHook) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onAppend = append(h.onAppend, hook)
}

// OnExpunge registers a hook to be called when a message is expunged.
func (h *Hooks) OnExpunge(hook MessageHook) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onExpunge = append(h.onExpunge, hook)
}

// TriggerAppend calls all registered append hooks.
func (h *Hooks) TriggerAppend(ctx context.Context, mailboxID int64, uid uint32) {
	h.mu.RLock()
	hooks := make([]MessageHook, len(h.onAppend))
	copy(hooks, h.onAppend)
	h.mu.RUnlock()

	for _, hook := range hooks {
		// Run hooks asynchronously to not block storage operations
		go func(fn MessageHook) {
			fn(ctx, mailboxID, uid)
		}(hook)
	}
}

// TriggerExpunge calls all registered expunge hooks.
func (h *Hooks) TriggerExpunge(ctx context.Context, mailboxID int64, uid uint32) {
	h.mu.RLock()
	hooks := make([]MessageHook, len(h.onExpunge))
	copy(hooks, h.onExpunge)
	h.mu.RUnlock()

	for _, hook := range hooks {
		// Run hooks asynchronously
		go func(fn MessageHook) {
			fn(ctx, mailboxID, uid)
		}(hook)
	}
}

// Clear removes all registered hooks.
func (h *Hooks) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onAppend = nil
	h.onExpunge = nil
}
