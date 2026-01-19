package bleve

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	srch "github.com/fenilsonani/email-server/internal/search"
)

// Helper variables for boolean pointers
var (
	boolTrue  = true
	boolFalse = false
)

// Engine implements SearchEngine using Bleve.
type Engine struct {
	index     bleve.Index
	indexPath string
	config    *srch.Config
	mu        sync.RWMutex
}

// NewEngine creates a new Bleve search engine.
func NewEngine(cfg *srch.Config) (*Engine, error) {
	e := &Engine{
		indexPath: cfg.IndexPath,
		config:    cfg,
	}

	// Try to open existing index
	index, err := bleve.Open(cfg.IndexPath)
	if err == bleve.ErrorIndexPathDoesNotExist {
		// Create new index
		mapping := CreateIndexMapping()
		index, err = bleve.New(cfg.IndexPath, mapping)
		if err != nil {
			return nil, fmt.Errorf("failed to create index: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("failed to open index: %w", err)
	}

	e.index = index
	return e, nil
}

// Name returns the engine name.
func (e *Engine) Name() string {
	return "bleve"
}

// Index adds or updates a single document.
func (e *Engine) Index(ctx context.Context, doc *srch.EmailDocument) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.index == nil {
		return srch.ErrIndexNotFound
	}

	// Check context
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return e.index.Index(doc.ID, doc)
}

// IndexBatch adds or updates multiple documents.
func (e *Engine) IndexBatch(ctx context.Context, docs []*srch.EmailDocument) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.index == nil {
		return srch.ErrIndexNotFound
	}

	batch := e.index.NewBatch()
	for _, doc := range docs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := batch.Index(doc.ID, doc); err != nil {
			return fmt.Errorf("failed to add document to batch: %w", err)
		}
	}

	return e.index.Batch(batch)
}

// Delete removes a document from the index.
func (e *Engine) Delete(ctx context.Context, docID string) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.index == nil {
		return srch.ErrIndexNotFound
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	return e.index.Delete(docID)
}

// DeleteBatch removes multiple documents from the index.
func (e *Engine) DeleteBatch(ctx context.Context, docIDs []string) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.index == nil {
		return srch.ErrIndexNotFound
	}

	batch := e.index.NewBatch()
	for _, id := range docIDs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		batch.Delete(id)
	}

	return e.index.Batch(batch)
}

// DeleteByMailbox removes all documents for a mailbox.
func (e *Engine) DeleteByMailbox(ctx context.Context, mailboxID int64) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.index == nil {
		return srch.ErrIndexNotFound
	}

	// Search for all documents in the mailbox
	minVal := float64(mailboxID)
	maxVal := float64(mailboxID)
	q := query.NewNumericRangeInclusiveQuery(&minVal, &maxVal, &boolTrue, &boolTrue)
	q.SetField("mailbox_id")

	searchReq := bleve.NewSearchRequest(q)
	searchReq.Size = 10000 // Process in batches
	searchReq.Fields = []string{}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		results, err := e.index.Search(searchReq)
		if err != nil {
			return fmt.Errorf("failed to search for mailbox documents: %w", err)
		}

		if len(results.Hits) == 0 {
			break
		}

		batch := e.index.NewBatch()
		for _, hit := range results.Hits {
			batch.Delete(hit.ID)
		}

		if err := e.index.Batch(batch); err != nil {
			return fmt.Errorf("failed to delete batch: %w", err)
		}

		// If we got fewer results than requested, we're done
		if len(results.Hits) < searchReq.Size {
			break
		}
	}

	return nil
}

// DeleteByUser removes all documents for a user.
func (e *Engine) DeleteByUser(ctx context.Context, userID int64) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.index == nil {
		return srch.ErrIndexNotFound
	}

	// Search for all documents for the user
	minVal := float64(userID)
	maxVal := float64(userID)
	q := query.NewNumericRangeInclusiveQuery(&minVal, &maxVal, &boolTrue, &boolTrue)
	q.SetField("user_id")

	searchReq := bleve.NewSearchRequest(q)
	searchReq.Size = 10000

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		results, err := e.index.Search(searchReq)
		if err != nil {
			return fmt.Errorf("failed to search for user documents: %w", err)
		}

		if len(results.Hits) == 0 {
			break
		}

		batch := e.index.NewBatch()
		for _, hit := range results.Hits {
			batch.Delete(hit.ID)
		}

		if err := e.index.Batch(batch); err != nil {
			return fmt.Errorf("failed to delete batch: %w", err)
		}

		if len(results.Hits) < searchReq.Size {
			break
		}
	}

	return nil
}

// Search performs a search query.
func (e *Engine) Search(ctx context.Context, sq *srch.SearchQuery) (*srch.SearchResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.index == nil {
		return nil, srch.ErrIndexNotFound
	}

	start := time.Now()

	// Build the Bleve query
	q, err := e.buildQuery(sq)
	if err != nil {
		return nil, err
	}

	// Create search request
	searchReq := bleve.NewSearchRequest(q)

	// Set pagination
	limit := sq.Limit
	if limit <= 0 || limit > e.config.MaxResults {
		limit = 100
	}
	searchReq.Size = limit
	searchReq.From = sq.Offset

	// Set sorting
	switch sq.SortBy {
	case "date":
		if sq.SortDesc {
			searchReq.SortBy([]string{"-date"})
		} else {
			searchReq.SortBy([]string{"date"})
		}
	case "relevance", "":
		if sq.SortDesc {
			searchReq.SortBy([]string{"-_score"})
		} else {
			searchReq.SortBy([]string{"_score"})
		}
	}

	// Enable highlighting if configured
	if e.config.HighlightEnabled {
		searchReq.Highlight = bleve.NewHighlightWithStyle("html")
		searchReq.Highlight.AddField("subject")
		searchReq.Highlight.AddField("body_text")
	}

	// Store fields to return
	searchReq.Fields = []string{"mailbox_id", "uid"}

	// Execute search with timeout
	type searchResult struct {
		result *bleve.SearchResult
		err    error
	}

	resultCh := make(chan searchResult, 1)
	go func() {
		result, err := e.index.Search(searchReq)
		resultCh <- searchResult{result, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resultCh:
		if res.err != nil {
			return nil, fmt.Errorf("search failed: %w", res.err)
		}

		// Convert results
		hits := make([]srch.SearchHit, 0, len(res.result.Hits))
		for _, h := range res.result.Hits {
			hit := srch.SearchHit{
				ID:        h.ID,
				Score:     h.Score,
				Fragments: h.Fragments,
			}

			// Extract mailbox_id and uid from stored fields
			if mbID, ok := h.Fields["mailbox_id"].(float64); ok {
				hit.MailboxID = int64(mbID)
			}
			if uid, ok := h.Fields["uid"].(float64); ok {
				hit.UID = uint32(uid)
			}

			// Also try to parse from document ID as fallback
			if hit.MailboxID == 0 || hit.UID == 0 {
				if mbID, uid, ok := srch.ParseDocumentID(h.ID); ok {
					if hit.MailboxID == 0 {
						hit.MailboxID = mbID
					}
					if hit.UID == 0 {
						hit.UID = uid
					}
				}
			}

			hits = append(hits, hit)
		}

		return &srch.SearchResult{
			Hits:     hits,
			Total:    res.result.Total,
			Took:     time.Since(start).Milliseconds(),
			MaxScore: res.result.MaxScore,
		}, nil
	}
}

// buildQuery constructs a Bleve query from SearchQuery.
func (e *Engine) buildQuery(sq *srch.SearchQuery) (query.Query, error) {
	var queries []query.Query

	// Text search across all fields
	if sq.Text != "" {
		q := query.NewQueryStringQuery(sq.Text)
		queries = append(queries, q)
	}

	// Subject search
	if sq.Subject != "" {
		q := query.NewMatchQuery(sq.Subject)
		q.SetField("subject")
		queries = append(queries, q)
	}

	// Body search
	if sq.Body != "" {
		q := query.NewMatchQuery(sq.Body)
		q.SetField("body_text")
		queries = append(queries, q)
	}

	// From search - use term query for exact email matching
	if sq.From != "" {
		q := query.NewTermQuery(sq.From)
		q.SetField("from")
		queries = append(queries, q)
	}

	// To search - use term query for exact email matching
	if sq.To != "" {
		q := query.NewTermQuery(sq.To)
		q.SetField("to")
		queries = append(queries, q)
	}

	// Phrase search
	if sq.Phrase != "" {
		q := query.NewMatchPhraseQuery(sq.Phrase)
		queries = append(queries, q)
	}

	// Fuzzy search
	if sq.Fuzzy != "" && e.config.FuzzyEnabled {
		fuzziness := sq.FuzzyDistance
		if fuzziness <= 0 {
			fuzziness = e.config.FuzzyDistance
		}
		q := query.NewFuzzyQuery(sq.Fuzzy)
		q.SetFuzziness(fuzziness)
		queries = append(queries, q)
	}

	// User ID filter
	if sq.UserID > 0 {
		minVal := float64(sq.UserID)
		maxVal := float64(sq.UserID)
		q := query.NewNumericRangeInclusiveQuery(&minVal, &maxVal, &boolTrue, &boolTrue)
		q.SetField("user_id")
		queries = append(queries, q)
	}

	// Mailbox ID filter
	if sq.MailboxID > 0 {
		minVal := float64(sq.MailboxID)
		maxVal := float64(sq.MailboxID)
		q := query.NewNumericRangeInclusiveQuery(&minVal, &maxVal, &boolTrue, &boolTrue)
		q.SetField("mailbox_id")
		queries = append(queries, q)
	}

	// Date range filters
	if sq.Since != nil {
		q := query.NewDateRangeInclusiveQuery(*sq.Since, time.Time{}, &boolTrue, &boolFalse)
		q.SetField("date")
		queries = append(queries, q)
	}

	if sq.Before != nil {
		q := query.NewDateRangeInclusiveQuery(time.Time{}, *sq.Before, &boolFalse, &boolFalse)
		q.SetField("date")
		queries = append(queries, q)
	}

	// Flag filters
	for _, flag := range sq.HasFlags {
		q := query.NewTermQuery(flag)
		q.SetField("flags")
		queries = append(queries, q)
	}

	// Combine all queries
	if len(queries) == 0 {
		return query.NewMatchAllQuery(), nil
	}

	if len(queries) == 1 {
		return queries[0], nil
	}

	boolQuery := query.NewConjunctionQuery(queries)
	return boolQuery, nil
}

// Stats returns statistics about the search index.
func (e *Engine) Stats(ctx context.Context) (*srch.IndexStats, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.index == nil {
		return nil, srch.ErrIndexNotFound
	}

	docCount, err := e.index.DocCount()
	if err != nil {
		return nil, fmt.Errorf("failed to get doc count: %w", err)
	}

	// Get index size from filesystem
	var indexSize int64
	err = getDirSize(e.indexPath, &indexSize)
	if err != nil {
		// Non-fatal, just log or ignore
		indexSize = 0
	}

	return &srch.IndexStats{
		DocumentCount: docCount,
		IndexSize:     indexSize,
		LastUpdated:   time.Now(),
		Engine:        "bleve",
	}, nil
}

// Close closes the search engine.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.index != nil {
		err := e.index.Close()
		e.index = nil
		return err
	}
	return nil
}

// getDirSize calculates the total size of a directory.
func getDirSize(path string, size *int64) error {
	return walkDir(path, func(p string, info os.FileInfo) error {
		if !info.IsDir() {
			*size += info.Size()
		}
		return nil
	})
}

// walkDir walks a directory tree.
func walkDir(path string, fn func(string, os.FileInfo) error) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		fullPath := path + "/" + entry.Name()
		if err := fn(fullPath, info); err != nil {
			return err
		}

		if entry.IsDir() {
			if err := walkDir(fullPath, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// Ensure Engine implements SearchEngine
var _ srch.SearchEngine = (*Engine)(nil)
