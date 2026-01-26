// Package search provides full-text search functionality for email messages.
package search

import (
	"time"
)

// EmailDocument represents an indexed email document.
type EmailDocument struct {
	// ID is the unique document identifier in format "{mailboxID}:{uid}"
	ID string `json:"id"`

	// UserID is the owner of the email
	UserID int64 `json:"user_id"`

	// MailboxID is the mailbox containing the email
	MailboxID int64 `json:"mailbox_id"`

	// UID is the IMAP UID of the message
	UID uint32 `json:"uid"`

	// Subject is the email subject line
	Subject string `json:"subject"`

	// From is the sender email address
	From string `json:"from"`

	// To contains recipient email addresses
	To []string `json:"to"`

	// Cc contains carbon copy recipients
	Cc []string `json:"cc"`

	// BodyText is the plain text content of the email
	BodyText string `json:"body_text"`

	// BodyHTML is the HTML content stripped of tags
	BodyHTML string `json:"body_html"`

	// Date is the email date from headers
	Date time.Time `json:"date"`

	// InternalDate is the IMAP internal date
	InternalDate time.Time `json:"internal_date"`

	// Flags are the IMAP message flags
	Flags []string `json:"flags"`

	// MessageID is the Message-ID header
	MessageID string `json:"message_id"`

	// Size is the message size in bytes
	Size int64 `json:"size"`
}

// SearchQuery represents a search request.
type SearchQuery struct {
	// Text is a general text search across all fields
	Text string `json:"text,omitempty"`

	// Subject searches the subject field
	Subject string `json:"subject,omitempty"`

	// Body searches the body text
	Body string `json:"body,omitempty"`

	// From searches the sender field
	From string `json:"from,omitempty"`

	// To searches the recipient fields
	To string `json:"to,omitempty"`

	// Phrase searches for an exact phrase
	Phrase string `json:"phrase,omitempty"`

	// Fuzzy enables fuzzy matching with edit distance
	Fuzzy string `json:"fuzzy,omitempty"`

	// FuzzyDistance is the edit distance for fuzzy matching (default 2)
	FuzzyDistance int `json:"fuzzy_distance,omitempty"`

	// UserID restricts search to a specific user
	UserID int64 `json:"user_id,omitempty"`

	// MailboxID restricts search to a specific mailbox
	MailboxID int64 `json:"mailbox_id,omitempty"`

	// Since filters for messages on or after this date
	Since *time.Time `json:"since,omitempty"`

	// Before filters for messages before this date
	Before *time.Time `json:"before,omitempty"`

	// HasFlags filters for messages with these flags
	HasFlags []string `json:"has_flags,omitempty"`

	// NotFlags filters for messages without these flags
	NotFlags []string `json:"not_flags,omitempty"`

	// Limit is the maximum number of results to return
	Limit int `json:"limit,omitempty"`

	// Offset is the number of results to skip
	Offset int `json:"offset,omitempty"`

	// SortBy specifies the sort order: "relevance" or "date"
	SortBy string `json:"sort_by,omitempty"`

	// SortDesc sorts in descending order when true
	SortDesc bool `json:"sort_desc,omitempty"`
}

// SearchResult represents search results.
type SearchResult struct {
	// Hits contains the matching documents
	Hits []SearchHit `json:"hits"`

	// Total is the total number of matching documents
	Total uint64 `json:"total"`

	// Took is the search duration in milliseconds
	Took int64 `json:"took_ms"`

	// MaxScore is the highest relevance score
	MaxScore float64 `json:"max_score,omitempty"`
}

// SearchHit represents a single search result.
type SearchHit struct {
	// ID is the document ID
	ID string `json:"id"`

	// Score is the relevance score
	Score float64 `json:"score"`

	// MailboxID is the mailbox containing the message
	MailboxID int64 `json:"mailbox_id"`

	// UID is the IMAP UID
	UID uint32 `json:"uid"`

	// Fragments contains highlighted text snippets
	Fragments map[string][]string `json:"fragments,omitempty"`
}

// IndexStats contains statistics about the search index.
type IndexStats struct {
	// DocumentCount is the total number of indexed documents
	DocumentCount uint64 `json:"document_count"`

	// IndexSize is the size of the index in bytes
	IndexSize int64 `json:"index_size"`

	// LastUpdated is when the index was last modified
	LastUpdated time.Time `json:"last_updated"`

	// Engine is the search engine type
	Engine string `json:"engine"`
}

// ParseDocumentID extracts mailbox ID and UID from a document ID.
func ParseDocumentID(id string) (mailboxID int64, uid uint32, ok bool) {
	var mb int64
	var u uint32
	n, _ := scanDocID(id, &mb, &u)
	if n != 2 {
		return 0, 0, false
	}
	return mb, u, true
}

// scanDocID is a helper to parse document IDs without fmt.Sscanf overhead.
func scanDocID(id string, mailboxID *int64, uid *uint32) (int, error) {
	// Parse "{mailboxID}:{uid}" format
	colonIdx := -1
	for i := 0; i < len(id); i++ {
		if id[i] == ':' {
			colonIdx = i
			break
		}
	}
	if colonIdx <= 0 || colonIdx >= len(id)-1 {
		return 0, nil
	}

	mb := int64(0)
	for i := 0; i < colonIdx; i++ {
		c := id[i]
		if c < '0' || c > '9' {
			return 0, nil
		}
		mb = mb*10 + int64(c-'0')
	}

	u := uint32(0)
	for i := colonIdx + 1; i < len(id); i++ {
		c := id[i]
		if c < '0' || c > '9' {
			return 0, nil
		}
		u = u*10 + uint32(c-'0')
	}

	*mailboxID = mb
	*uid = u
	return 2, nil
}

// FormatDocumentID creates a document ID from mailbox ID and UID.
func FormatDocumentID(mailboxID int64, uid uint32) string {
	// Avoid fmt.Sprintf allocation
	return itoa64(mailboxID) + ":" + itoa32(uid)
}

// itoa64 converts int64 to string without fmt.Sprintf
func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// itoa32 converts uint32 to string without fmt.Sprintf
func itoa32(n uint32) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
