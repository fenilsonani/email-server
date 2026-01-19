package query

import (
	"strings"

	imap "github.com/emersion/go-imap/v2"
	"github.com/fenilsonani/email-server/internal/search"
)

// FromIMAPCriteria converts IMAP SearchCriteria to a SearchQuery.
func FromIMAPCriteria(criteria *imap.SearchCriteria, mailboxID int64, userID int64) *search.SearchQuery {
	if criteria == nil {
		return &search.SearchQuery{
			MailboxID: mailboxID,
			UserID:    userID,
		}
	}

	sq := &search.SearchQuery{
		MailboxID: mailboxID,
		UserID:    userID,
	}

	// Date filters
	if !criteria.Since.IsZero() {
		t := criteria.Since
		sq.Since = &t
	}
	if !criteria.Before.IsZero() {
		t := criteria.Before
		sq.Before = &t
	}

	// Internal date filters
	if !criteria.SentSince.IsZero() && sq.Since == nil {
		t := criteria.SentSince
		sq.Since = &t
	}
	if !criteria.SentBefore.IsZero() && sq.Before == nil {
		t := criteria.SentBefore
		sq.Before = &t
	}

	// Flag filters
	for _, flag := range criteria.Flag {
		sq.HasFlags = append(sq.HasFlags, string(flag))
	}
	for _, flag := range criteria.NotFlag {
		sq.NotFlags = append(sq.NotFlags, string(flag))
	}

	// Header searches
	for _, h := range criteria.Header {
		key := strings.ToLower(h.Key)
		switch key {
		case "from":
			sq.From = h.Value
		case "to":
			sq.To = h.Value
		case "subject":
			sq.Subject = h.Value
		case "cc", "bcc":
			// Combine with To search
			if sq.To != "" {
				sq.To = sq.To + " " + h.Value
			} else {
				sq.To = h.Value
			}
		}
	}

	// Body search
	for _, bodyPart := range criteria.Body {
		if sq.Body != "" {
			sq.Body = sq.Body + " " + bodyPart
		} else {
			sq.Body = bodyPart
		}
	}

	// Text search (searches all fields)
	for _, text := range criteria.Text {
		if sq.Text != "" {
			sq.Text = sq.Text + " " + text
		} else {
			sq.Text = text
		}
	}

	// Handle OR criteria - we combine them with the main query
	// Note: Bleve supports more complex boolean logic, but for IMAP
	// we take a simpler approach (combine all terms)
	for _, orPair := range criteria.Or {
		nestedQuery0 := FromIMAPCriteria(&orPair[0], mailboxID, userID)
		nestedQuery1 := FromIMAPCriteria(&orPair[1], mailboxID, userID)
		mergeSearchQuery(sq, nestedQuery0)
		mergeSearchQuery(sq, nestedQuery1)
	}

	// Handle NOT criteria
	for i := range criteria.Not {
		nestedQuery := FromIMAPCriteria(&criteria.Not[i], mailboxID, userID)
		// Add negated flags
		sq.NotFlags = append(sq.NotFlags, nestedQuery.HasFlags...)
	}

	return sq
}

// mergeSearchQuery merges source query into destination.
func mergeSearchQuery(dst, src *search.SearchQuery) {
	if src.Text != "" {
		if dst.Text != "" {
			dst.Text = dst.Text + " " + src.Text
		} else {
			dst.Text = src.Text
		}
	}

	if src.Subject != "" {
		if dst.Subject != "" {
			dst.Subject = dst.Subject + " " + src.Subject
		} else {
			dst.Subject = src.Subject
		}
	}

	if src.Body != "" {
		if dst.Body != "" {
			dst.Body = dst.Body + " " + src.Body
		} else {
			dst.Body = src.Body
		}
	}

	if src.From != "" {
		if dst.From != "" {
			dst.From = dst.From + " " + src.From
		} else {
			dst.From = src.From
		}
	}

	if src.To != "" {
		if dst.To != "" {
			dst.To = dst.To + " " + src.To
		} else {
			dst.To = src.To
		}
	}

	if src.Since != nil && (dst.Since == nil || src.Since.After(*dst.Since)) {
		dst.Since = src.Since
	}

	if src.Before != nil && (dst.Before == nil || src.Before.Before(*dst.Before)) {
		dst.Before = src.Before
	}

	dst.HasFlags = append(dst.HasFlags, src.HasFlags...)
	dst.NotFlags = append(dst.NotFlags, src.NotFlags...)
}

// IsFullTextSearch checks if the criteria requires full-text search.
func IsFullTextSearch(criteria *imap.SearchCriteria) bool {
	if criteria == nil {
		return false
	}

	// Body and Text searches require full-text
	if len(criteria.Body) > 0 || len(criteria.Text) > 0 {
		return true
	}

	// Check Not criteria
	for i := range criteria.Not {
		if IsFullTextSearch(&criteria.Not[i]) {
			return true
		}
	}

	// Check Or criteria
	for _, orPair := range criteria.Or {
		if IsFullTextSearch(&orPair[0]) || IsFullTextSearch(&orPair[1]) {
			return true
		}
	}

	return false
}
