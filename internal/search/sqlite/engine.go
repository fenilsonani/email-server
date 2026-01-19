package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/search"
)

// Engine implements SearchEngine using SQLite FTS5.
type Engine struct {
	db     *sql.DB
	config *search.Config
}

// NewEngine creates a new SQLite FTS5 search engine.
// It requires the FTS5 table to be created by the migration.
func NewEngine(db *sql.DB, cfg *search.Config) (*Engine, error) {
	e := &Engine{
		db:     db,
		config: cfg,
	}

	// Verify FTS5 table exists
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='messages_fts'").Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("failed to check FTS5 table: %w", err)
	}
	if count == 0 {
		return nil, fmt.Errorf("FTS5 table 'messages_fts' not found - run migrations first")
	}

	return e, nil
}

// Name returns the engine name.
func (e *Engine) Name() string {
	return "sqlite"
}

// Index adds or updates a single document.
func (e *Engine) Index(ctx context.Context, doc *search.EmailDocument) error {
	// Build the combined body text
	bodyText := doc.BodyText
	if doc.BodyHTML != "" {
		bodyText = bodyText + " " + doc.BodyHTML
	}

	// Convert To slice to string
	toAddrs := strings.Join(doc.To, " ")

	// Insert or replace in FTS5 table
	_, err := e.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO messages_fts (
			doc_id, user_id, mailbox_id, uid, subject, from_addr, to_addrs, body_text, message_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		doc.ID, doc.UserID, doc.MailboxID, doc.UID, doc.Subject,
		doc.From, toAddrs, bodyText, doc.MessageID,
	)
	return err
}

// IndexBatch adds or updates multiple documents.
func (e *Engine) IndexBatch(ctx context.Context, docs []*search.EmailDocument) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO messages_fts (
			doc_id, user_id, mailbox_id, uid, subject, from_addr, to_addrs, body_text, message_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, doc := range docs {
		bodyText := doc.BodyText
		if doc.BodyHTML != "" {
			bodyText = bodyText + " " + doc.BodyHTML
		}
		toAddrs := strings.Join(doc.To, " ")

		_, err := stmt.ExecContext(ctx,
			doc.ID, doc.UserID, doc.MailboxID, doc.UID, doc.Subject,
			doc.From, toAddrs, bodyText, doc.MessageID,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Delete removes a document from the index.
func (e *Engine) Delete(ctx context.Context, docID string) error {
	_, err := e.db.ExecContext(ctx, "DELETE FROM messages_fts WHERE doc_id = ?", docID)
	return err
}

// DeleteBatch removes multiple documents from the index.
func (e *Engine) DeleteBatch(ctx context.Context, docIDs []string) error {
	if len(docIDs) == 0 {
		return nil
	}

	// Build placeholder list
	placeholders := make([]string, len(docIDs))
	args := make([]interface{}, len(docIDs))
	for i, id := range docIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM messages_fts WHERE doc_id IN (%s)", strings.Join(placeholders, ","))
	_, err := e.db.ExecContext(ctx, query, args...)
	return err
}

// DeleteByMailbox removes all documents for a mailbox.
func (e *Engine) DeleteByMailbox(ctx context.Context, mailboxID int64) error {
	_, err := e.db.ExecContext(ctx, "DELETE FROM messages_fts WHERE mailbox_id = ?", mailboxID)
	return err
}

// DeleteByUser removes all documents for a user.
func (e *Engine) DeleteByUser(ctx context.Context, userID int64) error {
	_, err := e.db.ExecContext(ctx, "DELETE FROM messages_fts WHERE user_id = ?", userID)
	return err
}

// Search performs a search query.
func (e *Engine) Search(ctx context.Context, sq *search.SearchQuery) (*search.SearchResult, error) {
	start := time.Now()

	// Build the FTS5 match expression
	var matchParts []string
	var args []interface{}

	// Text search across all fields
	if sq.Text != "" {
		matchParts = append(matchParts, escapeFTS5(sq.Text))
	}

	// Subject search
	if sq.Subject != "" {
		matchParts = append(matchParts, fmt.Sprintf("subject:%s", escapeFTS5(sq.Subject)))
	}

	// Body search
	if sq.Body != "" {
		matchParts = append(matchParts, fmt.Sprintf("body_text:%s", escapeFTS5(sq.Body)))
	}

	// From search
	if sq.From != "" {
		matchParts = append(matchParts, fmt.Sprintf("from_addr:%s", escapeFTS5(sq.From)))
	}

	// To search
	if sq.To != "" {
		matchParts = append(matchParts, fmt.Sprintf("to_addrs:%s", escapeFTS5(sq.To)))
	}

	// Phrase search
	if sq.Phrase != "" {
		matchParts = append(matchParts, fmt.Sprintf("\"%s\"", escapeFTS5(sq.Phrase)))
	}

	// Build the query
	var query string
	if len(matchParts) > 0 {
		matchExpr := strings.Join(matchParts, " AND ")
		query = `
			SELECT doc_id, mailbox_id, uid, bm25(messages_fts) as score
			FROM messages_fts
			WHERE messages_fts MATCH ?`
		args = append(args, matchExpr)
	} else {
		query = `
			SELECT doc_id, mailbox_id, uid, 0 as score
			FROM messages_fts
			WHERE 1=1`
	}

	// Add user filter
	if sq.UserID > 0 {
		query += " AND user_id = ?"
		args = append(args, sq.UserID)
	}

	// Add mailbox filter
	if sq.MailboxID > 0 {
		query += " AND mailbox_id = ?"
		args = append(args, sq.MailboxID)
	}

	// Add ordering
	if len(matchParts) > 0 {
		query += " ORDER BY score"
	}

	// Add limit
	limit := sq.Limit
	if limit <= 0 || limit > e.config.MaxResults {
		limit = e.config.MaxResults
	}
	query += fmt.Sprintf(" LIMIT %d", limit)

	if sq.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", sq.Offset)
	}

	// Execute query
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search query failed: %w", err)
	}
	defer rows.Close()

	var hits []search.SearchHit
	var maxScore float64

	for rows.Next() {
		var docID string
		var mailboxID int64
		var uid uint32
		var score float64

		if err := rows.Scan(&docID, &mailboxID, &uid, &score); err != nil {
			return nil, err
		}

		// BM25 returns negative scores (more negative = better match)
		// Convert to positive for consistency
		positiveScore := -score
		if positiveScore > maxScore {
			maxScore = positiveScore
		}

		hits = append(hits, search.SearchHit{
			ID:        docID,
			Score:     positiveScore,
			MailboxID: mailboxID,
			UID:       uid,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Get total count (without limit)
	var total uint64
	if len(matchParts) > 0 {
		countQuery := `SELECT COUNT(*) FROM messages_fts WHERE messages_fts MATCH ?`
		countArgs := []interface{}{strings.Join(matchParts, " AND ")}
		if sq.UserID > 0 {
			countQuery += " AND user_id = ?"
			countArgs = append(countArgs, sq.UserID)
		}
		if sq.MailboxID > 0 {
			countQuery += " AND mailbox_id = ?"
			countArgs = append(countArgs, sq.MailboxID)
		}
		e.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total)
	} else {
		total = uint64(len(hits))
	}

	return &search.SearchResult{
		Hits:     hits,
		Total:    total,
		Took:     time.Since(start).Milliseconds(),
		MaxScore: maxScore,
	}, nil
}

// Stats returns statistics about the search index.
func (e *Engine) Stats(ctx context.Context) (*search.IndexStats, error) {
	var count uint64
	err := e.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages_fts").Scan(&count)
	if err != nil {
		return nil, err
	}

	return &search.IndexStats{
		DocumentCount: count,
		IndexSize:     0, // FTS5 doesn't easily expose index size
		LastUpdated:   time.Now(),
		Engine:        "sqlite",
	}, nil
}

// Close closes the search engine.
func (e *Engine) Close() error {
	// The database connection is managed externally
	return nil
}

// escapeFTS5 escapes special characters in FTS5 queries.
func escapeFTS5(s string) string {
	// FTS5 special characters that need escaping
	replacer := strings.NewReplacer(
		`"`, `""`,
		`*`, ``,
		`(`, ``,
		`)`, ``,
		`:`, ` `,
	)
	return replacer.Replace(s)
}

// Ensure Engine implements SearchEngine
var _ search.SearchEngine = (*Engine)(nil)
