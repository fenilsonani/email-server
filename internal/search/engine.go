package search

import (
	"context"
	"errors"
)

// Common errors for search operations.
var (
	// ErrIndexNotFound indicates the search index doesn't exist
	ErrIndexNotFound = errors.New("search index not found")

	// ErrDocumentNotFound indicates the document doesn't exist in the index
	ErrDocumentNotFound = errors.New("document not found")

	// ErrIndexCorrupted indicates the index is corrupted
	ErrIndexCorrupted = errors.New("search index corrupted")

	// ErrSearchTimeout indicates the search operation timed out
	ErrSearchTimeout = errors.New("search timeout")

	// ErrInvalidQuery indicates the search query is malformed
	ErrInvalidQuery = errors.New("invalid search query")
)

// SearchEngine defines the interface for search implementations.
type SearchEngine interface {
	// Index adds or updates a single document in the search index.
	Index(ctx context.Context, doc *EmailDocument) error

	// IndexBatch adds or updates multiple documents in the search index.
	// This is more efficient than calling Index multiple times.
	IndexBatch(ctx context.Context, docs []*EmailDocument) error

	// Delete removes a document from the search index.
	Delete(ctx context.Context, docID string) error

	// DeleteBatch removes multiple documents from the search index.
	DeleteBatch(ctx context.Context, docIDs []string) error

	// DeleteByMailbox removes all documents for a mailbox.
	DeleteByMailbox(ctx context.Context, mailboxID int64) error

	// DeleteByUser removes all documents for a user.
	DeleteByUser(ctx context.Context, userID int64) error

	// Search performs a search and returns matching documents.
	Search(ctx context.Context, query *SearchQuery) (*SearchResult, error)

	// Stats returns statistics about the search index.
	Stats(ctx context.Context) (*IndexStats, error)

	// Close closes the search engine and releases resources.
	Close() error

	// Name returns the engine name (e.g., "bleve", "sqlite", "postgres")
	Name() string
}

// Indexer defines the interface for managing the search indexer.
type Indexer interface {
	// Start starts the background indexing worker.
	Start(ctx context.Context) error

	// Stop gracefully stops the indexer.
	Stop() error

	// IndexMessage queues a message for indexing.
	IndexMessage(ctx context.Context, mailboxID int64, uid uint32) error

	// DeleteMessage queues a message for deletion from index.
	DeleteMessage(ctx context.Context, mailboxID int64, uid uint32) error

	// ReindexMailbox re-indexes all messages in a mailbox.
	ReindexMailbox(ctx context.Context, mailboxID int64) error

	// ReindexAll re-indexes all messages for all users.
	ReindexAll(ctx context.Context) error

	// Flush forces any pending operations to complete.
	Flush(ctx context.Context) error
}

// SearchService combines engine and indexer functionality.
type SearchService struct {
	Engine  SearchEngine
	Indexer Indexer
}

// NewSearchService creates a new search service.
func NewSearchService(engine SearchEngine, indexer Indexer) *SearchService {
	return &SearchService{
		Engine:  engine,
		Indexer: indexer,
	}
}

// Close closes the search service.
func (s *SearchService) Close() error {
	if s.Indexer != nil {
		if err := s.Indexer.Stop(); err != nil {
			return err
		}
	}
	if s.Engine != nil {
		return s.Engine.Close()
	}
	return nil
}
