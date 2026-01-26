package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/search"
)

// Engine implements SearchEngine using PostgreSQL tsvector/tsquery.
type Engine struct {
	db     *sql.DB
	config *search.Config
}

// NewEngine creates a new PostgreSQL tsvector search engine.
// It requires the search table and indexes to be created by the migration.
func NewEngine(db *sql.DB, cfg *search.Config) (*Engine, error) {
	e := &Engine{
		db:     db,
		config: cfg,
	}

	// Verify search table exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_name = 'messages_search'
	`).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("failed to check search table: %w", err)
	}
	if count == 0 {
		return nil, fmt.Errorf("search table 'messages_search' not found - run migrations first")
	}

	return e, nil
}

// Name returns the engine name.
func (e *Engine) Name() string {
	return "postgres"
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

	// Upsert into search table
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO messages_search (
			doc_id, user_id, mailbox_id, uid, subject, from_addr, to_addrs,
			body_text, message_id, search_vector
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			setweight(to_tsvector('english', COALESCE($5, '')), 'A') ||
			setweight(to_tsvector('english', COALESCE($6, '')), 'B') ||
			setweight(to_tsvector('english', COALESCE($7, '')), 'B') ||
			setweight(to_tsvector('english', COALESCE($8, '')), 'C')
		)
		ON CONFLICT (doc_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			mailbox_id = EXCLUDED.mailbox_id,
			uid = EXCLUDED.uid,
			subject = EXCLUDED.subject,
			from_addr = EXCLUDED.from_addr,
			to_addrs = EXCLUDED.to_addrs,
			body_text = EXCLUDED.body_text,
			message_id = EXCLUDED.message_id,
			search_vector =
				setweight(to_tsvector('english', COALESCE(EXCLUDED.subject, '')), 'A') ||
				setweight(to_tsvector('english', COALESCE(EXCLUDED.from_addr, '')), 'B') ||
				setweight(to_tsvector('english', COALESCE(EXCLUDED.to_addrs, '')), 'B') ||
				setweight(to_tsvector('english', COALESCE(EXCLUDED.body_text, '')), 'C'),
			updated_at = NOW()`,
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
		INSERT INTO messages_search (
			doc_id, user_id, mailbox_id, uid, subject, from_addr, to_addrs,
			body_text, message_id, search_vector
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			setweight(to_tsvector('english', COALESCE($5, '')), 'A') ||
			setweight(to_tsvector('english', COALESCE($6, '')), 'B') ||
			setweight(to_tsvector('english', COALESCE($7, '')), 'B') ||
			setweight(to_tsvector('english', COALESCE($8, '')), 'C')
		)
		ON CONFLICT (doc_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			mailbox_id = EXCLUDED.mailbox_id,
			uid = EXCLUDED.uid,
			subject = EXCLUDED.subject,
			from_addr = EXCLUDED.from_addr,
			to_addrs = EXCLUDED.to_addrs,
			body_text = EXCLUDED.body_text,
			message_id = EXCLUDED.message_id,
			search_vector =
				setweight(to_tsvector('english', COALESCE(EXCLUDED.subject, '')), 'A') ||
				setweight(to_tsvector('english', COALESCE(EXCLUDED.from_addr, '')), 'B') ||
				setweight(to_tsvector('english', COALESCE(EXCLUDED.to_addrs, '')), 'B') ||
				setweight(to_tsvector('english', COALESCE(EXCLUDED.body_text, '')), 'C'),
			updated_at = NOW()`)
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
	_, err := e.db.ExecContext(ctx, "DELETE FROM messages_search WHERE doc_id = $1", docID)
	return err
}

// DeleteBatch removes multiple documents from the index.
func (e *Engine) DeleteBatch(ctx context.Context, docIDs []string) error {
	if len(docIDs) == 0 {
		return nil
	}

	// Build placeholder list with PostgreSQL-style parameters
	placeholders := make([]string, len(docIDs))
	args := make([]interface{}, len(docIDs))
	for i, id := range docIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf("DELETE FROM messages_search WHERE doc_id IN (%s)", strings.Join(placeholders, ","))
	_, err := e.db.ExecContext(ctx, query, args...)
	return err
}

// DeleteByMailbox removes all documents for a mailbox.
func (e *Engine) DeleteByMailbox(ctx context.Context, mailboxID int64) error {
	_, err := e.db.ExecContext(ctx, "DELETE FROM messages_search WHERE mailbox_id = $1", mailboxID)
	return err
}

// DeleteByUser removes all documents for a user.
func (e *Engine) DeleteByUser(ctx context.Context, userID int64) error {
	_, err := e.db.ExecContext(ctx, "DELETE FROM messages_search WHERE user_id = $1", userID)
	return err
}

// Search performs a search query.
func (e *Engine) Search(ctx context.Context, sq *search.SearchQuery) (*search.SearchResult, error) {
	start := time.Now()

	// Build the tsquery from search terms
	var queryParts []string
	var args []interface{}
	argNum := 1

	// Build tsquery expression
	tsqueryExpr := ""

	// Text search across all fields
	if sq.Text != "" {
		tsqueryExpr = buildTsquery(sq.Text)
	}

	// Subject search
	if sq.Subject != "" {
		if tsqueryExpr != "" {
			tsqueryExpr += " & "
		}
		tsqueryExpr += buildTsquery(sq.Subject)
	}

	// Body search
	if sq.Body != "" {
		if tsqueryExpr != "" {
			tsqueryExpr += " & "
		}
		tsqueryExpr += buildTsquery(sq.Body)
	}

	// From search
	if sq.From != "" {
		if tsqueryExpr != "" {
			tsqueryExpr += " & "
		}
		tsqueryExpr += buildTsquery(sq.From)
	}

	// To search
	if sq.To != "" {
		if tsqueryExpr != "" {
			tsqueryExpr += " & "
		}
		tsqueryExpr += buildTsquery(sq.To)
	}

	// Phrase search
	if sq.Phrase != "" {
		if tsqueryExpr != "" {
			tsqueryExpr += " & "
		}
		tsqueryExpr += buildPhraseTsquery(sq.Phrase)
	}

	// Build the query
	var query string
	if tsqueryExpr != "" {
		query = fmt.Sprintf(`
			SELECT doc_id, mailbox_id, uid, ts_rank(search_vector, to_tsquery('english', $%d)) as score
			FROM messages_search
			WHERE search_vector @@ to_tsquery('english', $%d)`, argNum, argNum)
		args = append(args, tsqueryExpr)
		queryParts = append(queryParts, tsqueryExpr)
		argNum++
	} else {
		query = `
			SELECT doc_id, mailbox_id, uid, 0 as score
			FROM messages_search
			WHERE 1=1`
	}

	// Add user filter
	if sq.UserID > 0 {
		query += fmt.Sprintf(" AND user_id = $%d", argNum)
		args = append(args, sq.UserID)
		argNum++
	}

	// Add mailbox filter
	if sq.MailboxID > 0 {
		query += fmt.Sprintf(" AND mailbox_id = $%d", argNum)
		args = append(args, sq.MailboxID)
		argNum++
	}

	// Add ordering
	if tsqueryExpr != "" {
		query += " ORDER BY score DESC"
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

		if score > maxScore {
			maxScore = score
		}

		hits = append(hits, search.SearchHit{
			ID:        docID,
			Score:     score,
			MailboxID: mailboxID,
			UID:       uid,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Get total count (without limit)
	var total uint64
	if len(queryParts) > 0 {
		countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM messages_search WHERE search_vector @@ to_tsquery('english', $1)`)
		countArgs := []interface{}{queryParts[0]}
		argIdx := 2
		if sq.UserID > 0 {
			countQuery += fmt.Sprintf(" AND user_id = $%d", argIdx)
			countArgs = append(countArgs, sq.UserID)
			argIdx++
		}
		if sq.MailboxID > 0 {
			countQuery += fmt.Sprintf(" AND mailbox_id = $%d", argIdx)
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
	err := e.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages_search").Scan(&count)
	if err != nil {
		return nil, err
	}

	// Get index size
	var indexSize int64
	e.db.QueryRowContext(ctx, `
		SELECT pg_total_relation_size('messages_search')
	`).Scan(&indexSize)

	return &search.IndexStats{
		DocumentCount: count,
		IndexSize:     indexSize,
		LastUpdated:   time.Now(),
		Engine:        "postgres",
	}, nil
}

// Close closes the search engine.
func (e *Engine) Close() error {
	// The database connection is managed externally
	return nil
}

// buildTsquery converts a search string to a tsquery expression.
func buildTsquery(s string) string {
	// Split into words and join with AND
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}

	escaped := make([]string, len(words))
	for i, word := range words {
		// Escape special characters and add prefix matching
		word = strings.ReplaceAll(word, "'", "''")
		word = strings.ReplaceAll(word, "\\", "\\\\")
		escaped[i] = word + ":*" // Prefix matching
	}

	return strings.Join(escaped, " & ")
}

// buildPhraseTsquery converts a phrase to a tsquery expression.
func buildPhraseTsquery(phrase string) string {
	// Use phrase search operator <->
	words := strings.Fields(phrase)
	if len(words) == 0 {
		return ""
	}

	escaped := make([]string, len(words))
	for i, word := range words {
		word = strings.ReplaceAll(word, "'", "''")
		word = strings.ReplaceAll(word, "\\", "\\\\")
		escaped[i] = word
	}

	return strings.Join(escaped, " <-> ")
}

// Ensure Engine implements SearchEngine
var _ search.SearchEngine = (*Engine)(nil)
