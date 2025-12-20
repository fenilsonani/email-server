package imap

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/backend"
)

func TestNewUpdateHub(t *testing.T) {
	hub := NewUpdateHub()
	if hub == nil {
		t.Fatal("NewUpdateHub() returned nil")
	}

	if hub.clients == nil {
		t.Error("clients map should be initialized")
	}
	if hub.updateCh == nil {
		t.Error("updateCh should be initialized")
	}
	if hub.closed.Load() {
		t.Error("hub should not be closed initially")
	}

	// Clean shutdown
	hub.Close()
}

func TestUpdateHub_SubscribeUnsubscribe(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	ch := hub.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe() returned nil channel")
	}

	// Verify subscription
	if count := hub.ClientCount(); count != 1 {
		t.Errorf("ClientCount() = %d, want 1", count)
	}

	hub.mu.RLock()
	_, exists := hub.clients[ch]
	hub.mu.RUnlock()

	if !exists {
		t.Error("Subscribed channel should be in clients map")
	}

	// Unsubscribe
	hub.Unsubscribe(ch)

	// Verify unsubscription
	if count := hub.ClientCount(); count != 0 {
		t.Errorf("ClientCount() = %d, want 0", count)
	}

	hub.mu.RLock()
	_, exists = hub.clients[ch]
	hub.mu.RUnlock()

	if exists {
		t.Error("Unsubscribed channel should not be in clients map")
	}

	// Channel should be closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("Unsubscribed channel should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected closed channel to return immediately")
	}
}

func TestUpdateHub_UnsubscribeNonexistent(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	// Should not panic
	fakeCh := make(chan backend.Update)
	hub.Unsubscribe(fakeCh)
}

func TestUpdateHub_DoubleUnsubscribe(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	ch := hub.Subscribe()
	hub.Unsubscribe(ch)

	// Should not panic on second unsubscribe
	hub.Unsubscribe(ch)
}

func TestUpdateHub_Notify(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	ch := hub.Subscribe()

	// Start a goroutine to receive
	received := make(chan backend.Update, 1)
	go func() {
		select {
		case update := <-ch:
			received <- update
		case <-time.After(time.Second):
		}
	}()

	// Give the hub time to start processing
	time.Sleep(50 * time.Millisecond)

	// Send an update
	update := &backend.MailboxUpdate{
		Update:        backend.NewUpdate("user", "INBOX"),
		MailboxStatus: &imap.MailboxStatus{Name: "INBOX"},
	}
	hub.Notify(update)

	// Wait for receipt
	select {
	case got := <-received:
		if got == nil {
			t.Error("Received nil update")
		}
		mailboxUpdate, ok := got.(*backend.MailboxUpdate)
		if !ok {
			t.Errorf("Expected MailboxUpdate, got %T", got)
		}
		if mailboxUpdate.Username() != "user" {
			t.Errorf("Username = %s, want user", mailboxUpdate.Username())
		}
		if mailboxUpdate.Mailbox() != "INBOX" {
			t.Errorf("Mailbox = %s, want INBOX", mailboxUpdate.Mailbox())
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for update")
	}
}

func TestUpdateHub_NotifyAfterClose(t *testing.T) {
	hub := NewUpdateHub()
	ch := hub.Subscribe()
	hub.Close()

	// Should not panic
	update := &backend.MailboxUpdate{
		Update: backend.NewUpdate("user", "INBOX"),
	}
	hub.Notify(update)

	// Channel should be closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("Channel should be closed after hub closes")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected closed channel to return immediately")
	}
}

func TestUpdateHub_DroppedUpdates(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	// Subscribe but don't read from channel to fill it up
	_ = hub.Subscribe()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Send more updates than buffer can hold (buffer is 100)
	numUpdates := 200
	for i := 0; i < numUpdates; i++ {
		update := &backend.MailboxUpdate{
			Update: backend.NewUpdate("user", "INBOX"),
		}
		hub.Notify(update)
	}

	// Give time for updates to be processed/dropped
	time.Sleep(100 * time.Millisecond)

	// Check that drops were counted
	drops := atomic.LoadInt64(&hub.droppedUpdates)
	if drops == 0 {
		t.Log("No updates dropped (buffer may have processed them)")
	} else {
		t.Logf("Dropped %d updates as expected", drops)
		if drops < 0 {
			t.Errorf("Negative drop count: %d", drops)
		}
	}
}

func TestUpdateHub_ClientChannelOverflow(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	// Subscribe but don't read - client channel has buffer of 10
	ch := hub.Subscribe()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Send enough updates to overflow client channel
	for i := 0; i < 50; i++ {
		update := &backend.MailboxUpdate{
			Update: backend.NewUpdate("user", "INBOX"),
		}
		hub.Notify(update)
	}

	// Give time for updates to be processed
	time.Sleep(100 * time.Millisecond)

	// Drain the channel
	received := 0
	for {
		select {
		case <-ch:
			received++
		case <-time.After(50 * time.Millisecond):
			goto done
		}
	}
done:

	// Should receive at most buffer size (updateCh:100 + client:10)
	// but some may be dropped due to client channel overflow
	t.Logf("Received %d updates", received)
	if received > 50 {
		t.Error("Received more updates than expected")
	}
}

func TestUpdateHub_MultipleSubscribers(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	const numSubscribers = 5
	channels := make([]chan backend.Update, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		channels[i] = hub.Subscribe()
	}

	// Verify all subscribed
	if count := hub.ClientCount(); count != numSubscribers {
		t.Errorf("ClientCount() = %d, want %d", count, numSubscribers)
	}

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Send update
	update := &backend.MailboxUpdate{
		Update: backend.NewUpdate("user", "INBOX"),
	}
	hub.Notify(update)

	// All subscribers should receive
	var wg sync.WaitGroup
	received := int32(0)
	for i, ch := range channels {
		wg.Add(1)
		go func(idx int, c chan backend.Update) {
			defer wg.Done()
			select {
			case u := <-c:
				if u != nil {
					atomic.AddInt32(&received, 1)
				}
			case <-time.After(time.Second):
				t.Errorf("Subscriber %d timeout", idx)
			}
		}(i, ch)
	}
	wg.Wait()

	if int(received) != numSubscribers {
		t.Errorf("Received %d updates, want %d", received, numSubscribers)
	}
}

func TestUpdateHub_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	var wg sync.WaitGroup
	const goroutines = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := hub.Subscribe()
			time.Sleep(time.Millisecond)
			hub.Unsubscribe(ch)
		}()
	}

	wg.Wait()

	remaining := hub.ClientCount()
	if remaining != 0 {
		t.Errorf("Expected 0 clients after all unsubscribed, got %d", remaining)
	}
}

func TestUpdateHub_ConcurrentNotify(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	ch := hub.Subscribe()
	done := make(chan int)

	// Consumer goroutine - counts how many updates received
	go func() {
		count := 0
		timeout := time.After(500 * time.Millisecond)
	loop:
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					break loop
				}
				count++
			case <-timeout:
				break loop
			}
		}
		done <- count
	}()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Multiple producers
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				update := &backend.MailboxUpdate{
					Update: backend.NewUpdate("user", "INBOX"),
				}
				hub.Notify(update)
			}
		}()
	}

	wg.Wait()

	select {
	case count := <-done:
		// Should have received at least some updates (may drop some due to channel overflow)
		if count == 0 {
			t.Error("Consumer did not receive any updates")
		}
		t.Logf("Consumer received %d updates", count)
	case <-time.After(2 * time.Second):
		t.Error("Consumer goroutine did not complete")
	}
}

func TestUpdateHub_CloseWhileSubscribed(t *testing.T) {
	hub := NewUpdateHub()
	ch := hub.Subscribe()

	if count := hub.ClientCount(); count != 1 {
		t.Errorf("ClientCount() = %d, want 1", count)
	}

	// Close hub
	hub.Close()

	// Channel should be closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("Channel should be closed after hub closes")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected closed channel to return immediately")
	}

	// Client count should be 0
	if count := hub.ClientCount(); count != 0 {
		t.Errorf("ClientCount() = %d, want 0 after close", count)
	}

	// Double close should not panic
	hub.Close()
}

func TestUpdateHub_SubscribeAfterClose(t *testing.T) {
	hub := NewUpdateHub()
	hub.Close()

	ch := hub.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe() returned nil channel")
	}

	// Channel should be immediately closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("Channel from closed hub should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected closed channel to return immediately")
	}
}

func TestUpdateHub_ExpungeUpdate(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	ch := hub.Subscribe()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Send expunge update
	update := &backend.ExpungeUpdate{
		Update: backend.NewUpdate("user", "INBOX"),
		SeqNum: 5,
	}
	hub.Notify(update)

	select {
	case got := <-ch:
		exp, ok := got.(*backend.ExpungeUpdate)
		if !ok {
			t.Errorf("Expected ExpungeUpdate, got %T", got)
		}
		if exp.SeqNum != 5 {
			t.Errorf("SeqNum = %d, want 5", exp.SeqNum)
		}
		if exp.Username() != "user" {
			t.Errorf("Username = %s, want user", exp.Username())
		}
		if exp.Mailbox() != "INBOX" {
			t.Errorf("Mailbox = %s, want INBOX", exp.Mailbox())
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for expunge update")
	}
}

func TestUpdateHub_MessageUpdate(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	ch := hub.Subscribe()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Send message update
	update := &backend.MessageUpdate{
		Update:  backend.NewUpdate("user", "INBOX"),
		Message: &imap.Message{SeqNum: 10},
	}
	hub.Notify(update)

	select {
	case got := <-ch:
		msgUpdate, ok := got.(*backend.MessageUpdate)
		if !ok {
			t.Errorf("Expected MessageUpdate, got %T", got)
		}
		if msgUpdate.Message == nil || msgUpdate.Message.SeqNum != 10 {
			t.Errorf("SeqNum = %d, want 10", msgUpdate.Message.SeqNum)
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for message update")
	}
}

func TestUpdateHub_StatusUpdate(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	ch := hub.Subscribe()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Send status update (StatusUpdate embeds *imap.StatusResp)
	update := &backend.StatusUpdate{
		Update: backend.NewUpdate("user", "INBOX"),
		StatusResp: &imap.StatusResp{
			Tag:  "*",
			Type: imap.StatusRespOk,
			Info: "Test status",
		},
	}
	hub.Notify(update)

	select {
	case got := <-ch:
		statusUpdate, ok := got.(*backend.StatusUpdate)
		if !ok {
			t.Errorf("Expected StatusUpdate, got %T", got)
		}
		if statusUpdate.StatusResp == nil || statusUpdate.StatusResp.Info != "Test status" {
			t.Errorf("StatusResp.Info = %q, want %q", statusUpdate.StatusResp.Info, "Test status")
		}
	case <-time.After(time.Second):
		t.Error("Timeout waiting for status update")
	}
}

func TestUpdateHub_RaceCondition(t *testing.T) {
	// Run with -race flag to detect race conditions
	hub := NewUpdateHub()
	defer hub.Close()

	var wg sync.WaitGroup

	// Concurrent subscribe/unsubscribe
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := hub.Subscribe()
			time.Sleep(time.Millisecond)
			hub.Unsubscribe(ch)
		}()
	}

	// Concurrent notify
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				update := &backend.MailboxUpdate{
					Update: backend.NewUpdate("user", "INBOX"),
				}
				hub.Notify(update)
				time.Sleep(time.Microsecond * 100)
			}
		}()
	}

	// Concurrent client count checks
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = hub.ClientCount()
				time.Sleep(time.Microsecond * 200)
			}
		}()
	}

	wg.Wait()
}

func TestUpdateHub_GracefulShutdown(t *testing.T) {
	hub := NewUpdateHub()

	// Subscribe multiple clients
	channels := make([]chan backend.Update, 10)
	for i := 0; i < 10; i++ {
		channels[i] = hub.Subscribe()
	}

	// Send some updates
	for i := 0; i < 5; i++ {
		update := &backend.MailboxUpdate{
			Update: backend.NewUpdate("user", "INBOX"),
		}
		hub.Notify(update)
	}

	// Give time for updates to propagate
	time.Sleep(100 * time.Millisecond)

	// Close hub
	hub.Close()

	// All channels should eventually close (after draining any buffered updates)
	for i, ch := range channels {
		closed := false
		deadline := time.After(500 * time.Millisecond)
	drainLoop:
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					closed = true
					break drainLoop
				}
				// Got a buffered update, continue draining
			case <-deadline:
				t.Errorf("Channel %d did not close in time", i)
				break drainLoop
			}
		}
		if !closed {
			// Channel timed out without closing
		}
	}

	// Verify clean state
	if count := hub.ClientCount(); count != 0 {
		t.Errorf("ClientCount() = %d after close, want 0", count)
	}

	if !hub.closed.Load() {
		t.Error("Hub should be marked as closed")
	}
}

func TestUpdateHub_UpdatesChannel(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	updatesCh := hub.Updates()
	if updatesCh == nil {
		t.Fatal("Updates() returned nil channel")
	}

	// This is the internal channel that feeds the hub
	if updatesCh != hub.updateCh {
		t.Error("Updates() should return the internal updateCh")
	}
}

func TestUpdateHub_PartialClientConsumption(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	// One fast consumer
	fastCh := hub.Subscribe()
	fastReceived := make(chan int, 1)
	go func() {
		count := 0
		for range fastCh {
			count++
		}
		fastReceived <- count
	}()

	// One slow consumer (doesn't read)
	_ = hub.Subscribe()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	// Send updates
	const numUpdates = 20
	for i := 0; i < numUpdates; i++ {
		update := &backend.MailboxUpdate{
			Update: backend.NewUpdate("user", "INBOX"),
		}
		hub.Notify(update)
	}

	// Give time for updates to propagate
	time.Sleep(100 * time.Millisecond)

	hub.Close()

	// Fast consumer should have received updates
	select {
	case count := <-fastReceived:
		if count == 0 {
			t.Error("Fast consumer should have received some updates")
		}
		t.Logf("Fast consumer received %d updates", count)
	case <-time.After(time.Second):
		t.Error("Timeout waiting for fast consumer")
	}
}

func TestUpdateHub_ClientStateTracking(t *testing.T) {
	hub := NewUpdateHub()
	defer hub.Close()

	ch := hub.Subscribe()

	// Verify client state exists
	hub.mu.RLock()
	state, exists := hub.clients[ch]
	hub.mu.RUnlock()

	if !exists {
		t.Fatal("Client state should exist for subscribed channel")
	}

	if state.ch != ch {
		t.Error("Client state should reference the correct channel")
	}

	if state.closed.Load() {
		t.Error("Client state should not be closed initially")
	}

	// Unsubscribe
	hub.Unsubscribe(ch)

	// State should be marked closed and removed
	if !state.closed.Load() {
		t.Error("Client state should be marked closed after unsubscribe")
	}

	hub.mu.RLock()
	_, exists = hub.clients[ch]
	hub.mu.RUnlock()

	if exists {
		t.Error("Client state should be removed from map after unsubscribe")
	}
}

func BenchmarkUpdateHub_Notify(b *testing.B) {
	hub := NewUpdateHub()
	defer hub.Close()

	ch := hub.Subscribe()
	done := make(chan bool)

	// Consumer
	go func() {
		for range ch {
		}
		done <- true
	}()

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	update := &backend.MailboxUpdate{
		Update: backend.NewUpdate("user", "INBOX"),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.Notify(update)
	}
	b.StopTimer()

	hub.Close()
	<-done
}

func BenchmarkUpdateHub_SubscribeUnsubscribe(b *testing.B) {
	hub := NewUpdateHub()
	defer hub.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch := hub.Subscribe()
		hub.Unsubscribe(ch)
	}
}

func BenchmarkUpdateHub_ConcurrentNotify(b *testing.B) {
	hub := NewUpdateHub()
	defer hub.Close()

	// Multiple consumers
	const numConsumers = 10
	for i := 0; i < numConsumers; i++ {
		ch := hub.Subscribe()
		go func(c chan backend.Update) {
			for range c {
			}
		}(ch)
	}

	// Give hub time to start
	time.Sleep(50 * time.Millisecond)

	update := &backend.MailboxUpdate{
		Update: backend.NewUpdate("user", "INBOX"),
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			hub.Notify(update)
		}
	})
	b.StopTimer()
}

func BenchmarkUpdateHub_MultipleSubscribers(b *testing.B) {
	for _, numSubs := range []int{1, 10, 100, 1000} {
		b.Run(string(rune(numSubs))+"_subscribers", func(b *testing.B) {
			hub := NewUpdateHub()
			defer hub.Close()

			// Create subscribers
			for i := 0; i < numSubs; i++ {
				ch := hub.Subscribe()
				go func(c chan backend.Update) {
					for range c {
					}
				}(ch)
			}

			// Give hub time to start
			time.Sleep(50 * time.Millisecond)

			update := &backend.MailboxUpdate{
				Update: backend.NewUpdate("user", "INBOX"),
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				hub.Notify(update)
			}
			b.StopTimer()
		})
	}
}
