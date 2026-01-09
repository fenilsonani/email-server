package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Transparent 1x1 GIF pixel
var trackingPixel = func() []byte {
	data, _ := base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")
	return data
}()

// Pre-compiled regex patterns for performance (avoid compiling on every request)
var (
	bodyTagRegex = regexp.MustCompile(`(?i)(</body>)`)
	linkHrefRegex = regexp.MustCompile(`(<a[^>]*href=["'])([^"']+)(["'][^>]*>)`)
)

// handleTrackOpen handles GET /t/o/{tracking_id}
func (s *Server) handleTrackOpen(w http.ResponseWriter, r *http.Request) {
	trackingID := strings.TrimPrefix(r.URL.Path, "/t/o/")
	if trackingID == "" || len(trackingID) > 64 {
		// Return pixel anyway to avoid revealing tracking status
		s.serveTrackingPixel(w)
		return
	}

	// Update open tracking in database
	now := time.Now()
	result, err := s.db.ExecContext(r.Context(), `
		UPDATE sent_emails
		SET opened_count = opened_count + 1,
		    opened_at = COALESCE(opened_at, ?)
		WHERE tracking_id = ?
	`, now, trackingID)

	if err != nil {
		s.logger.Error("Failed to track open", "error", err.Error(), "tracking_id", trackingID)
	} else {
		rows, _ := result.RowsAffected()
		if rows > 0 {
			// Get domain_id for webhook
			var domainID int64
			var messageID, recipient string
			s.db.QueryRowContext(r.Context(), `
				SELECT domain_id, message_id, to_email FROM sent_emails WHERE tracking_id = ?
			`, trackingID).Scan(&domainID, &messageID, &recipient)

			// Trigger webhook
			go s.triggerWebhook(r.Context(), domainID, EventOpened, &WebhookEvent{
				Event:     EventOpened,
				Timestamp: now,
				MessageID: messageID,
				Recipient: recipient,
				Data: map[string]interface{}{
					"user_agent": r.UserAgent(),
					"ip":         getClientIP(r),
				},
			})
		}
	}

	s.serveTrackingPixel(w)
}

// handleTrackClick handles GET /t/c/{tracking_id}?url={encoded_url}
func (s *Server) handleTrackClick(w http.ResponseWriter, r *http.Request) {
	trackingID := strings.TrimPrefix(r.URL.Path, "/t/c/")
	if trackingID == "" || len(trackingID) > 64 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Get the original URL
	originalURL := r.URL.Query().Get("url")
	if originalURL == "" {
		http.Error(w, "Missing URL", http.StatusBadRequest)
		return
	}

	// Decode URL
	decodedURL, err := url.QueryUnescape(originalURL)
	if err != nil {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	// Validate URL to prevent open redirect
	if !isValidRedirectURL(decodedURL) {
		http.Error(w, "Invalid redirect URL", http.StatusBadRequest)
		return
	}

	// Update click tracking
	now := time.Now()
	result, err := s.db.ExecContext(r.Context(), `
		UPDATE sent_emails
		SET clicked_count = clicked_count + 1,
		    clicked_at = COALESCE(clicked_at, ?)
		WHERE tracking_id = ?
	`, now, trackingID)

	if err != nil {
		s.logger.Error("Failed to track click", "error", err.Error(), "tracking_id", trackingID)
	} else {
		rows, _ := result.RowsAffected()
		if rows > 0 {
			var domainID int64
			var messageID, recipient string
			s.db.QueryRowContext(r.Context(), `
				SELECT domain_id, message_id, to_email FROM sent_emails WHERE tracking_id = ?
			`, trackingID).Scan(&domainID, &messageID, &recipient)

			go s.triggerWebhook(r.Context(), domainID, EventClicked, &WebhookEvent{
				Event:     EventClicked,
				Timestamp: now,
				MessageID: messageID,
				Recipient: recipient,
				Data: map[string]interface{}{
					"url":        decodedURL,
					"user_agent": r.UserAgent(),
					"ip":         getClientIP(r),
				},
			})
		}
	}

	// Redirect to original URL
	http.Redirect(w, r, decodedURL, http.StatusFound)
}

// serveTrackingPixel serves the 1x1 transparent GIF
func (s *Server) serveTrackingPixel(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.WriteHeader(http.StatusOK)
	w.Write(trackingPixel)
}

// injectOpenTracking injects an open tracking pixel into HTML
func injectOpenTracking(html, trackingID, trackingDomain, hostname string) string {
	if html == "" {
		return html
	}

	domain := trackingDomain
	if domain == "" {
		domain = hostname
	}

	pixelURL := fmt.Sprintf("https://%s/t/o/%s", domain, trackingID)
	pixelTag := fmt.Sprintf(`<img src="%s" width="1" height="1" alt="" style="display:none;width:1px;height:1px;"/>`, pixelURL)

	// Try to inject before </body> using pre-compiled regex
	if strings.Contains(strings.ToLower(html), "</body>") {
		return bodyTagRegex.ReplaceAllString(html, pixelTag+"$1")
	}

	// Otherwise append to end
	return html + pixelTag
}

// rewriteLinksForTracking rewrites links in HTML for click tracking
func rewriteLinksForTracking(html, trackingID, trackingDomain, hostname string) string {
	if html == "" {
		return html
	}

	domain := trackingDomain
	if domain == "" {
		domain = hostname
	}

	// Use pre-compiled regex for performance
	return linkHrefRegex.ReplaceAllStringFunc(html, func(match string) string {
		submatches := linkHrefRegex.FindStringSubmatch(match)
		if len(submatches) < 4 {
			return match
		}

		originalURL := submatches[2]

		// Skip mailto:, tel:, and anchor links
		if strings.HasPrefix(originalURL, "mailto:") ||
			strings.HasPrefix(originalURL, "tel:") ||
			strings.HasPrefix(originalURL, "#") ||
			strings.HasPrefix(originalURL, "javascript:") {
			return match
		}

		// Skip tracking URLs (don't double-track)
		if strings.Contains(originalURL, "/t/c/") || strings.Contains(originalURL, "/t/o/") {
			return match
		}

		// Create tracking URL
		encodedURL := url.QueryEscape(originalURL)
		trackingURL := fmt.Sprintf("https://%s/t/c/%s?url=%s", domain, trackingID, encodedURL)

		return submatches[1] + trackingURL + submatches[3]
	})
}

// isValidRedirectURL validates that a URL is safe for redirection
func isValidRedirectURL(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}

	// Only allow http and https
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}

	// Must have a host
	if parsed.Host == "" {
		return false
	}

	return true
}

// getClientIP gets the real client IP from request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}
