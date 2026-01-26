package indexer

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/fenilsonani/email-server/internal/search"
	"github.com/fenilsonani/email-server/internal/storage"
)

// Prometheus metrics for search indexing
var (
	indexOperationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "search_index_operations_total",
		Help: "Total number of search index operations",
	}, []string{"operation", "status"})

	indexOperationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "search_index_operation_duration_seconds",
		Help:    "Duration of search index operations",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to 4s
	}, []string{"operation"})

	indexQueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "search_index_queue_size",
		Help: "Current size of the indexing queue",
	})

	indexBatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "search_index_batch_size",
		Help:    "Size of index batches",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10), // 1 to 512
	})

	indexRetries = promauto.NewCounter(prometheus.CounterOpts{
		Name: "search_index_retries_total",
		Help: "Total number of index operation retries",
	})

	indexDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "search_index_dropped_total",
		Help: "Total number of dropped index operations after max retries",
	})
)

// MessageStore interface for accessing message data.
type MessageStore interface {
	GetMessage(ctx context.Context, mailboxID int64, uid uint32) (*storage.Message, error)
	GetMessageBody(ctx context.Context, msg *storage.Message) (io.ReadCloser, error)
	GetMailboxByID(ctx context.Context, id int64) (*storage.Mailbox, error)
	ListMessages(ctx context.Context, mailboxID int64, start, end uint32) ([]*storage.Message, error)
	ListMailboxes(ctx context.Context, userID int64) ([]*storage.Mailbox, error)
}

// UserStore interface for accessing user data.
type UserStore interface {
	ListAllUsers(ctx context.Context) ([]int64, error)
}

// IndexOperation represents a pending indexing operation.
type IndexOperation struct {
	Type      OpType
	MailboxID int64
	UID       uint32
	UserID    int64
	Retries   int
	LastError error
}

// OpType is the type of index operation.
type OpType int

const (
	OpIndex OpType = iota
	OpDelete
)

const (
	maxRetries        = 3
	initialRetryDelay = 100 * time.Millisecond
	maxRetryDelay     = 5 * time.Second
)

// Indexer manages background indexing of email messages.
type Indexer struct {
	engine      search.SearchEngine
	store       MessageStore
	userStore   UserStore
	parser      *EmailParser
	config      *search.Config
	operations  chan IndexOperation
	retryQueue  chan IndexOperation
	batch       []*search.EmailDocument
	deleteBatch []string
	mu          sync.Mutex
	wg          sync.WaitGroup
	stopCh      chan struct{}
	stopped     atomic.Bool

	// Stats
	indexed   atomic.Int64
	deleted   atomic.Int64
	errors    atomic.Int64
}

// NewIndexer creates a new background indexer.
func NewIndexer(engine search.SearchEngine, store MessageStore, userStore UserStore, cfg *search.Config) *Indexer {
	return &Indexer{
		engine:      engine,
		store:       store,
		userStore:   userStore,
		parser:      NewEmailParser(),
		config:      cfg,
		operations:  make(chan IndexOperation, 10000),
		retryQueue:  make(chan IndexOperation, 1000),
		batch:       make([]*search.EmailDocument, 0, cfg.BatchSize),
		deleteBatch: make([]string, 0, cfg.BatchSize),
		stopCh:      make(chan struct{}),
	}
}

// Start starts the background indexing workers.
func (i *Indexer) Start(ctx context.Context) error {
	if i.stopped.Load() {
		return fmt.Errorf("indexer already stopped")
	}

	log.Printf("[search] Starting indexer with %d workers", i.config.Workers)

	// Start worker goroutines
	for w := 0; w < i.config.Workers; w++ {
		i.wg.Add(1)
		go i.worker(w)
	}

	// Start flush goroutine
	i.wg.Add(1)
	go i.flusher()

	// Start retry processor
	i.wg.Add(1)
	go i.retryProcessor()

	// Start metrics reporter
	i.wg.Add(1)
	go i.metricsReporter()

	return nil
}

// Stop gracefully stops the indexer.
func (i *Indexer) Stop() error {
	if i.stopped.Swap(true) {
		return nil // Already stopped
	}

	log.Printf("[search] Stopping indexer...")
	close(i.stopCh)

	// Wait for all workers to finish with timeout
	done := make(chan struct{})
	go func() {
		i.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[search] Indexer stopped gracefully")
	case <-time.After(10 * time.Second):
		log.Printf("[search] Indexer stop timed out")
	}

	// Flush any remaining operations
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := i.flushBatches(ctx); err != nil {
		log.Printf("[search] Error flushing final batches: %v", err)
		return err
	}

	log.Printf("[search] Indexer stats: indexed=%d deleted=%d errors=%d",
		i.indexed.Load(), i.deleted.Load(), i.errors.Load())

	return nil
}

// IndexMessage queues a message for indexing.
// IMPORTANT: This creates a new background context - don't use the request context
// for background operations as it may be cancelled.
func (i *Indexer) IndexMessage(ctx context.Context, mailboxID int64, uid uint32) error {
	if i.stopped.Load() {
		return fmt.Errorf("indexer stopped")
	}

	// Get mailbox to find user ID - use a short timeout
	lookupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mb, err := i.store.GetMailboxByID(lookupCtx, mailboxID)
	if err != nil {
		log.Printf("[search] Failed to get mailbox %d for indexing: %v", mailboxID, err)
		indexOperationsTotal.WithLabelValues("index", "error").Inc()
		return fmt.Errorf("failed to get mailbox: %w", err)
	}

	op := IndexOperation{
		Type:      OpIndex,
		MailboxID: mailboxID,
		UID:       uid,
		UserID:    mb.UserID,
	}

	select {
	case i.operations <- op:
		indexQueueSize.Set(float64(len(i.operations)))
		return nil
	default:
		// Queue full - log warning but don't block
		log.Printf("[search] Warning: index queue full, dropping message %d:%d", mailboxID, uid)
		indexOperationsTotal.WithLabelValues("index", "dropped").Inc()
		indexDropped.Inc()
		return fmt.Errorf("index queue full")
	}
}

// DeleteMessage queues a message for deletion from the index.
func (i *Indexer) DeleteMessage(ctx context.Context, mailboxID int64, uid uint32) error {
	if i.stopped.Load() {
		return fmt.Errorf("indexer stopped")
	}

	op := IndexOperation{
		Type:      OpDelete,
		MailboxID: mailboxID,
		UID:       uid,
	}

	select {
	case i.operations <- op:
		indexQueueSize.Set(float64(len(i.operations)))
		return nil
	default:
		log.Printf("[search] Warning: index queue full, dropping delete %d:%d", mailboxID, uid)
		indexOperationsTotal.WithLabelValues("delete", "dropped").Inc()
		return fmt.Errorf("index queue full")
	}
}

// ReindexMailbox re-indexes all messages in a mailbox.
func (i *Indexer) ReindexMailbox(ctx context.Context, mailboxID int64) error {
	log.Printf("[search] Reindexing mailbox %d", mailboxID)

	// Delete existing documents for this mailbox
	if err := i.engine.DeleteByMailbox(ctx, mailboxID); err != nil {
		log.Printf("[search] Failed to clear mailbox %d index: %v", mailboxID, err)
		return fmt.Errorf("failed to clear mailbox index: %w", err)
	}

	// List all messages in the mailbox
	messages, err := i.store.ListMessages(ctx, mailboxID, 0, 0)
	if err != nil {
		log.Printf("[search] Failed to list messages for mailbox %d: %v", mailboxID, err)
		return fmt.Errorf("failed to list messages: %w", err)
	}

	log.Printf("[search] Queuing %d messages for reindex in mailbox %d", len(messages), mailboxID)

	// Queue all messages for indexing
	queued := 0
	for _, msg := range messages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := i.IndexMessage(ctx, mailboxID, msg.UID); err != nil {
			log.Printf("[search] Failed to queue message %d:%d for reindex: %v", mailboxID, msg.UID, err)
			continue
		}
		queued++
	}

	log.Printf("[search] Queued %d/%d messages for reindex in mailbox %d", queued, len(messages), mailboxID)
	return nil
}

// ReindexAll re-indexes all messages for all users.
func (i *Indexer) ReindexAll(ctx context.Context) error {
	if i.userStore == nil {
		return fmt.Errorf("user store not configured")
	}

	log.Printf("[search] Starting full reindex of all users")

	userIDs, err := i.userStore.ListAllUsers(ctx)
	if err != nil {
		log.Printf("[search] Failed to list users for reindex: %v", err)
		return fmt.Errorf("failed to list users: %w", err)
	}

	log.Printf("[search] Reindexing %d users", len(userIDs))

	for idx, userID := range userIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		mailboxes, err := i.store.ListMailboxes(ctx, userID)
		if err != nil {
			log.Printf("[search] Failed to list mailboxes for user %d: %v", userID, err)
			continue
		}

		for _, mb := range mailboxes {
			if err := i.ReindexMailbox(ctx, mb.ID); err != nil {
				log.Printf("[search] Failed to reindex mailbox %d: %v", mb.ID, err)
				continue
			}
		}

		if (idx+1)%10 == 0 {
			log.Printf("[search] Reindex progress: %d/%d users", idx+1, len(userIDs))
		}
	}

	log.Printf("[search] Full reindex complete")
	return nil
}

// Flush forces any pending operations to complete.
func (i *Indexer) Flush(ctx context.Context) error {
	log.Printf("[search] Flushing indexer...")

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return i.flushBatches(ctx)
		default:
			if len(i.operations) == 0 {
				return i.flushBatches(ctx)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// worker processes indexing operations.
func (i *Indexer) worker(id int) {
	defer i.wg.Done()

	log.Printf("[search] Worker %d started", id)

	for {
		select {
		case <-i.stopCh:
			log.Printf("[search] Worker %d stopping", id)
			return
		case op := <-i.operations:
			i.processOperation(op)
		}
	}
}

// flusher periodically flushes batches.
func (i *Indexer) flusher() {
	defer i.wg.Done()

	ticker := time.NewTicker(i.config.GetFlushInterval())
	defer ticker.Stop()

	for {
		select {
		case <-i.stopCh:
			return
		case <-ticker.C:
			// Use background context - not tied to any request
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := i.flushBatches(ctx); err != nil {
				log.Printf("[search] Error flushing batches: %v", err)
			}
			cancel()
		}
	}
}

// retryProcessor handles failed operations with exponential backoff.
func (i *Indexer) retryProcessor() {
	defer i.wg.Done()

	for {
		select {
		case <-i.stopCh:
			return
		case op := <-i.retryQueue:
			// Calculate backoff delay
			delay := initialRetryDelay * time.Duration(1<<uint(op.Retries))
			if delay > maxRetryDelay {
				delay = maxRetryDelay
			}

			select {
			case <-i.stopCh:
				return
			case <-time.After(delay):
				// Re-queue the operation
				select {
				case i.operations <- op:
					indexRetries.Inc()
				default:
					log.Printf("[search] Retry queue full, dropping %d:%d after %d retries",
						op.MailboxID, op.UID, op.Retries)
					indexDropped.Inc()
				}
			}
		}
	}
}

// metricsReporter periodically logs indexer stats.
func (i *Indexer) metricsReporter() {
	defer i.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-i.stopCh:
			return
		case <-ticker.C:
			indexQueueSize.Set(float64(len(i.operations)))
			log.Printf("[search] Indexer stats: queue=%d indexed=%d deleted=%d errors=%d",
				len(i.operations), i.indexed.Load(), i.deleted.Load(), i.errors.Load())
		}
	}
}

// processOperation handles a single indexing operation.
func (i *Indexer) processOperation(op IndexOperation) {
	start := time.Now()

	var err error
	switch op.Type {
	case OpIndex:
		err = i.indexMessage(op)
		indexOperationDuration.WithLabelValues("index").Observe(time.Since(start).Seconds())
	case OpDelete:
		err = i.deleteMessage(op)
		indexOperationDuration.WithLabelValues("delete").Observe(time.Since(start).Seconds())
	}

	if err != nil {
		i.handleError(op, err)
	}
}

// indexMessage indexes a single message.
func (i *Indexer) indexMessage(op IndexOperation) error {
	// Use background context with timeout - not the original request context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get message metadata
	msg, err := i.store.GetMessage(ctx, op.MailboxID, op.UID)
	if err != nil {
		return fmt.Errorf("failed to get message %d:%d: %w", op.MailboxID, op.UID, err)
	}

	// Get message body
	body, err := i.store.GetMessageBody(ctx, msg)
	if err != nil {
		return fmt.Errorf("failed to get message body %d:%d: %w", op.MailboxID, op.UID, err)
	}
	defer body.Close()

	// Parse the message
	doc, err := i.parser.ParseMessage(body, op.MailboxID, op.UID, op.UserID)
	if err != nil {
		return fmt.Errorf("failed to parse message %d:%d: %w", op.MailboxID, op.UID, err)
	}

	// Add metadata from storage
	doc.InternalDate = msg.InternalDate
	doc.Size = msg.Size
	doc.Flags = make([]string, len(msg.Flags))
	for j, f := range msg.Flags {
		doc.Flags[j] = string(f)
	}

	// Add to batch
	i.mu.Lock()
	i.batch = append(i.batch, doc)
	shouldFlush := len(i.batch) >= i.config.BatchSize
	i.mu.Unlock()

	if shouldFlush {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := i.flushBatches(flushCtx)
		flushCancel()
		if err != nil {
			return fmt.Errorf("failed to flush batch: %w", err)
		}
	}

	return nil
}

// deleteMessage removes a message from the index.
func (i *Indexer) deleteMessage(op IndexOperation) error {
	docID := search.FormatDocumentID(op.MailboxID, op.UID)

	i.mu.Lock()
	i.deleteBatch = append(i.deleteBatch, docID)
	shouldFlush := len(i.deleteBatch) >= i.config.BatchSize
	i.mu.Unlock()

	if shouldFlush {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := i.flushBatches(ctx)
		cancel()
		if err != nil {
			return fmt.Errorf("failed to flush delete batch: %w", err)
		}
	}

	return nil
}

// handleError handles operation errors with retry logic.
func (i *Indexer) handleError(op IndexOperation, err error) {
	i.errors.Add(1)

	opType := "index"
	if op.Type == OpDelete {
		opType = "delete"
	}

	if op.Retries >= maxRetries {
		log.Printf("[search] Error %s %d:%d after %d retries, dropping: %v",
			opType, op.MailboxID, op.UID, op.Retries, err)
		indexOperationsTotal.WithLabelValues(opType, "error").Inc()
		indexDropped.Inc()
		return
	}

	// Queue for retry
	op.Retries++
	op.LastError = err

	log.Printf("[search] Error %s %d:%d (retry %d/%d): %v",
		opType, op.MailboxID, op.UID, op.Retries, maxRetries, err)

	select {
	case i.retryQueue <- op:
		// Successfully queued for retry
	default:
		log.Printf("[search] Retry queue full, dropping %d:%d", op.MailboxID, op.UID)
		indexOperationsTotal.WithLabelValues(opType, "error").Inc()
		indexDropped.Inc()
	}
}

// flushBatches writes pending batches to the index.
func (i *Indexer) flushBatches(ctx context.Context) error {
	i.mu.Lock()
	batch := i.batch
	deleteBatch := i.deleteBatch
	i.batch = make([]*search.EmailDocument, 0, i.config.BatchSize)
	i.deleteBatch = make([]string, 0, i.config.BatchSize)
	i.mu.Unlock()

	var lastErr error

	// Index documents
	if len(batch) > 0 {
		indexBatchSize.Observe(float64(len(batch)))
		start := time.Now()

		if err := i.engine.IndexBatch(ctx, batch); err != nil {
			log.Printf("[search] Failed to index batch of %d documents: %v", len(batch), err)
			indexOperationsTotal.WithLabelValues("batch_index", "error").Inc()
			lastErr = err
		} else {
			i.indexed.Add(int64(len(batch)))
			indexOperationsTotal.WithLabelValues("batch_index", "success").Inc()
			indexOperationDuration.WithLabelValues("batch_index").Observe(time.Since(start).Seconds())
		}
	}

	// Delete documents
	if len(deleteBatch) > 0 {
		start := time.Now()

		if err := i.engine.DeleteBatch(ctx, deleteBatch); err != nil {
			log.Printf("[search] Failed to delete batch of %d documents: %v", len(deleteBatch), err)
			indexOperationsTotal.WithLabelValues("batch_delete", "error").Inc()
			lastErr = err
		} else {
			i.deleted.Add(int64(len(deleteBatch)))
			indexOperationsTotal.WithLabelValues("batch_delete", "success").Inc()
			indexOperationDuration.WithLabelValues("batch_delete").Observe(time.Since(start).Seconds())
		}
	}

	return lastErr
}

// Stats returns current indexer statistics.
func (i *Indexer) Stats() (indexed, deleted, errors int64, queueSize int) {
	return i.indexed.Load(), i.deleted.Load(), i.errors.Load(), len(i.operations)
}

// Ensure Indexer implements search.Indexer
var _ search.Indexer = (*Indexer)(nil)
