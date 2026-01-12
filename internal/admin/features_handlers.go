package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/features"
)

// JSON response helpers for features API
func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonErrorResponse(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// =============================================================================
// Screener API Handlers
// =============================================================================

// handleScreenerList handles GET /admin/api/screener
func (s *Server) handleScreenerList(w http.ResponseWriter, r *http.Request) {
	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	status := r.URL.Query().Get("status")
	contacts, err := s.featuresStore.ListScreenerContacts(r.Context(), userID, features.ScreenerStatus(status))
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list screener contacts", err)
		jsonErrorResponse(w, "Failed to list contacts", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"contacts": contacts,
		"count":    len(contacts),
	})
}

// handleScreenerApprove handles POST /admin/api/screener/approve
func (s *Server) handleScreenerApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Email  string `json:"email"`
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErrorResponse(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Email == "" && req.Domain == "" {
		jsonErrorResponse(w, "Email or domain required", http.StatusBadRequest)
		return
	}

	if err := s.featuresStore.ApproveContact(r.Context(), userID, req.Email, req.Domain); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to approve contact", err)
		jsonErrorResponse(w, "Failed to approve contact", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "approved"})
}

// handleScreenerBlock handles POST /admin/api/screener/block
func (s *Server) handleScreenerBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Email  string `json:"email"`
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErrorResponse(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Email == "" && req.Domain == "" {
		jsonErrorResponse(w, "Email or domain required", http.StatusBadRequest)
		return
	}

	if err := s.featuresStore.BlockContact(r.Context(), userID, req.Email, req.Domain); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to block contact", err)
		jsonErrorResponse(w, "Failed to block contact", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "blocked"})
}

// handleScreenerDelete handles DELETE /admin/api/screener/{id}
func (s *Server) handleScreenerDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract contact ID from URL
	contactID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/api/screener/"), 10, 64)
	if err != nil {
		jsonErrorResponse(w, "Invalid contact ID", http.StatusBadRequest)
		return
	}

	if err := s.featuresStore.DeleteScreenerContact(r.Context(), userID, contactID); err != nil {
		if err == features.ErrNotFound {
			jsonErrorResponse(w, "Contact not found", http.StatusNotFound)
			return
		}
		s.logger.ErrorContext(r.Context(), "Failed to delete contact", err)
		jsonErrorResponse(w, "Failed to delete contact", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "deleted"})
}

// =============================================================================
// Aliases API Handlers
// =============================================================================

// handleAliasesList handles GET /admin/api/aliases
func (s *Server) handleAliasesList(w http.ResponseWriter, r *http.Request) {
	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	aliases, err := s.featuresStore.ListAliases(r.Context(), userID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list aliases", err)
		jsonErrorResponse(w, "Failed to list aliases", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"aliases": aliases,
		"count":   len(aliases),
	})
}

// handleAliasCreate handles POST /admin/api/aliases
func (s *Server) handleAliasCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		DomainID    int64  `json:"domain_id"`
		LocalPart   string `json:"local_part"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErrorResponse(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.DomainID == 0 {
		jsonErrorResponse(w, "Domain ID required", http.StatusBadRequest)
		return
	}

	// Get domain name for full address
	domain, err := s.getDomainByID(r.Context(), req.DomainID)
	if err != nil {
		jsonErrorResponse(w, "Domain not found", http.StatusBadRequest)
		return
	}

	// Generate local part if not provided
	localPart := req.LocalPart
	if localPart == "" {
		localPart = features.GenerateAliasLocal(req.Description)
	}

	alias := &features.EmailAlias{
		UserID:       userID,
		DomainID:     req.DomainID,
		AliasLocal:   localPart,
		AliasAddress: localPart + "@" + domain,
		Description:  req.Description,
	}

	if err := s.featuresStore.CreateAlias(r.Context(), alias); err != nil {
		if err == features.ErrAlreadyExists {
			jsonErrorResponse(w, "Alias already exists", http.StatusConflict)
			return
		}
		s.logger.ErrorContext(r.Context(), "Failed to create alias", err)
		jsonErrorResponse(w, "Failed to create alias", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, alias)
}

// handleAliasUpdate handles PATCH /admin/api/aliases/{id}
func (s *Server) handleAliasUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	aliasID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/api/aliases/"), 10, 64)
	if err != nil {
		jsonErrorResponse(w, "Invalid alias ID", http.StatusBadRequest)
		return
	}

	var req struct {
		IsActive    *bool   `json:"is_active"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErrorResponse(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if err := s.featuresStore.UpdateAlias(r.Context(), userID, aliasID, req.IsActive, req.Description); err != nil {
		if err == features.ErrNotFound {
			jsonErrorResponse(w, "Alias not found", http.StatusNotFound)
			return
		}
		s.logger.ErrorContext(r.Context(), "Failed to update alias", err)
		jsonErrorResponse(w, "Failed to update alias", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "updated"})
}

// handleAliasDelete handles DELETE /admin/api/aliases/{id}
func (s *Server) handleAliasDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	aliasID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/api/aliases/"), 10, 64)
	if err != nil {
		jsonErrorResponse(w, "Invalid alias ID", http.StatusBadRequest)
		return
	}

	if err := s.featuresStore.DeleteAlias(r.Context(), userID, aliasID); err != nil {
		if err == features.ErrNotFound {
			jsonErrorResponse(w, "Alias not found", http.StatusNotFound)
			return
		}
		s.logger.ErrorContext(r.Context(), "Failed to delete alias", err)
		jsonErrorResponse(w, "Failed to delete alias", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "deleted"})
}

// =============================================================================
// Scheduled Emails API Handlers
// =============================================================================

// handleScheduledList handles GET /admin/api/scheduled
func (s *Server) handleScheduledList(w http.ResponseWriter, r *http.Request) {
	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	status := r.URL.Query().Get("status")
	emails, err := s.featuresStore.ListScheduledEmails(r.Context(), userID, status)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list scheduled emails", err)
		jsonErrorResponse(w, "Failed to list scheduled emails", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"emails": emails,
		"count":  len(emails),
	})
}

// handleScheduledCreate handles POST /admin/api/scheduled
func (s *Server) handleScheduledCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		SendAt      time.Time         `json:"send_at"`
		FromAddress string            `json:"from_address"`
		Recipients  []string          `json:"recipients"`
		Subject     string            `json:"subject"`
		Body        string            `json:"body"`
		HTMLBody    string            `json:"html_body"`
		Headers     map[string]string `json:"headers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErrorResponse(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.SendAt.IsZero() || req.SendAt.Before(time.Now()) {
		jsonErrorResponse(w, "Send time must be in the future", http.StatusBadRequest)
		return
	}

	if req.FromAddress == "" || len(req.Recipients) == 0 {
		jsonErrorResponse(w, "From address and recipients required", http.StatusBadRequest)
		return
	}

	email := &features.ScheduledEmail{
		UserID:      userID,
		SendAt:      req.SendAt,
		FromAddress: req.FromAddress,
		Recipients:  req.Recipients,
		Subject:     req.Subject,
		Body:        req.Body,
		HTMLBody:    req.HTMLBody,
		Headers:     req.Headers,
	}

	if err := s.featuresStore.CreateScheduledEmail(r.Context(), email); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to create scheduled email", err)
		jsonErrorResponse(w, "Failed to schedule email", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, email)
}

// handleScheduledCancel handles DELETE /admin/api/scheduled/{id}
func (s *Server) handleScheduledCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	emailID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/api/scheduled/"), 10, 64)
	if err != nil {
		jsonErrorResponse(w, "Invalid email ID", http.StatusBadRequest)
		return
	}

	if err := s.featuresStore.CancelScheduledEmail(r.Context(), userID, emailID); err != nil {
		if err == features.ErrNotFound {
			jsonErrorResponse(w, "Scheduled email not found or already sent", http.StatusNotFound)
			return
		}
		s.logger.ErrorContext(r.Context(), "Failed to cancel scheduled email", err)
		jsonErrorResponse(w, "Failed to cancel scheduled email", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "cancelled"})
}

// =============================================================================
// Snooze API Handlers
// =============================================================================

// handleSnoozedList handles GET /admin/api/snoozed
func (s *Server) handleSnoozedList(w http.ResponseWriter, r *http.Request) {
	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	snoozed, err := s.featuresStore.ListSnoozedEmails(r.Context(), userID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list snoozed emails", err)
		jsonErrorResponse(w, "Failed to list snoozed emails", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"snoozed": snoozed,
		"count":   len(snoozed),
	})
}

// handleSnoozeCreate handles POST /admin/api/messages/{id}/snooze
func (s *Server) handleSnoozeCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract message ID from URL: /admin/api/messages/{id}/snooze
	path := strings.TrimPrefix(r.URL.Path, "/admin/api/messages/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "snooze" {
		jsonErrorResponse(w, "Invalid URL", http.StatusBadRequest)
		return
	}
	messageID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		jsonErrorResponse(w, "Invalid message ID", http.StatusBadRequest)
		return
	}

	var req struct {
		WakeAt            time.Time `json:"wake_at"`
		OriginalMailboxID int64     `json:"original_mailbox_id"`
		MarkUnread        *bool     `json:"mark_unread"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErrorResponse(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.WakeAt.IsZero() || req.WakeAt.Before(time.Now()) {
		jsonErrorResponse(w, "Wake time must be in the future", http.StatusBadRequest)
		return
	}

	markUnread := true
	if req.MarkUnread != nil {
		markUnread = *req.MarkUnread
	}

	snooze := &features.SnoozedEmail{
		UserID:            userID,
		MessageID:         messageID,
		OriginalMailboxID: req.OriginalMailboxID,
		WakeAt:            req.WakeAt,
		MarkUnread:        markUnread,
	}

	if err := s.featuresStore.SnoozeEmail(r.Context(), snooze); err != nil {
		if err == features.ErrAlreadyExists {
			jsonErrorResponse(w, "Message already snoozed", http.StatusConflict)
			return
		}
		s.logger.ErrorContext(r.Context(), "Failed to snooze message", err)
		jsonErrorResponse(w, "Failed to snooze message", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, snooze)
}

// handleSnoozeCancel handles DELETE /admin/api/messages/{id}/snooze
func (s *Server) handleSnoozeCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract message ID from URL
	path := strings.TrimPrefix(r.URL.Path, "/admin/api/messages/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "snooze" {
		jsonErrorResponse(w, "Invalid URL", http.StatusBadRequest)
		return
	}
	messageID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		jsonErrorResponse(w, "Invalid message ID", http.StatusBadRequest)
		return
	}

	if err := s.featuresStore.CancelSnooze(r.Context(), userID, messageID); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to cancel snooze", err)
		jsonErrorResponse(w, "Failed to cancel snooze", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "cancelled"})
}

// =============================================================================
// VIP API Handlers
// =============================================================================

// handleVIPList handles GET /admin/api/vip
func (s *Server) handleVIPList(w http.ResponseWriter, r *http.Request) {
	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	vips, err := s.featuresStore.ListVIPs(r.Context(), userID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list VIPs", err)
		jsonErrorResponse(w, "Failed to list VIPs", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]interface{}{
		"vips":  vips,
		"count": len(vips),
	})
}

// handleVIPAdd handles POST /admin/api/vip
func (s *Server) handleVIPAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErrorResponse(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Email == "" {
		jsonErrorResponse(w, "Email required", http.StatusBadRequest)
		return
	}

	vip := &features.VIPContact{
		UserID: userID,
		Email:  req.Email,
		Name:   req.Name,
	}

	if err := s.featuresStore.AddVIP(r.Context(), vip); err != nil {
		if err == features.ErrAlreadyExists {
			jsonErrorResponse(w, "VIP already exists", http.StatusConflict)
			return
		}
		s.logger.ErrorContext(r.Context(), "Failed to add VIP", err)
		jsonErrorResponse(w, "Failed to add VIP", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, vip)
}

// handleVIPDelete handles DELETE /admin/api/vip/{id}
func (s *Server) handleVIPDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	vipID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/api/vip/"), 10, 64)
	if err != nil {
		jsonErrorResponse(w, "Invalid VIP ID", http.StatusBadRequest)
		return
	}

	if err := s.featuresStore.DeleteVIP(r.Context(), userID, vipID); err != nil {
		if err == features.ErrNotFound {
			jsonErrorResponse(w, "VIP not found", http.StatusNotFound)
			return
		}
		s.logger.ErrorContext(r.Context(), "Failed to delete VIP", err)
		jsonErrorResponse(w, "Failed to delete VIP", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"status": "deleted"})
}

// =============================================================================
// Preferences API Handlers
// =============================================================================

// handlePreferencesGet handles GET /admin/api/preferences
func (s *Server) handlePreferencesGet(w http.ResponseWriter, r *http.Request) {
	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	prefs, err := s.featuresStore.GetPreferences(r.Context(), userID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get preferences", err)
		jsonErrorResponse(w, "Failed to get preferences", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, prefs)
}

// handlePreferencesUpdate handles PATCH /admin/api/preferences
func (s *Server) handlePreferencesUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodPost {
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.featuresStore == nil {
		jsonErrorResponse(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, err := s.getContextUserID(r)
	if err != nil {
		jsonErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get current preferences
	prefs, err := s.featuresStore.GetPreferences(r.Context(), userID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get preferences", err)
		jsonErrorResponse(w, "Failed to get preferences", http.StatusInternalServerError)
		return
	}

	// Decode updates
	var req struct {
		UndoSendDelay    *int    `json:"undo_send_delay"`
		ScreenerEnabled  *bool   `json:"screener_enabled"`
		TrackerBlocking  *string `json:"tracker_blocking"`
		ZonesEnabled     *bool   `json:"zones_enabled"`
		SnoozeMarkUnread *bool   `json:"snooze_mark_unread"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErrorResponse(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Apply updates
	if req.UndoSendDelay != nil {
		// Validate delay values
		valid := []int{0, 5, 10, 20, 30}
		isValid := false
		for _, v := range valid {
			if *req.UndoSendDelay == v {
				isValid = true
				break
			}
		}
		if !isValid {
			jsonErrorResponse(w, "Invalid undo send delay (must be 0, 5, 10, 20, or 30)", http.StatusBadRequest)
			return
		}
		prefs.UndoSendDelay = *req.UndoSendDelay
	}
	if req.ScreenerEnabled != nil {
		prefs.ScreenerEnabled = *req.ScreenerEnabled
	}
	if req.TrackerBlocking != nil {
		// Validate tracker blocking values
		if *req.TrackerBlocking != "block" && *req.TrackerBlocking != "proxy" && *req.TrackerBlocking != "off" {
			jsonErrorResponse(w, "Invalid tracker blocking value (must be 'block', 'proxy', or 'off')", http.StatusBadRequest)
			return
		}
		prefs.TrackerBlocking = *req.TrackerBlocking
	}
	if req.ZonesEnabled != nil {
		prefs.ZonesEnabled = *req.ZonesEnabled
	}
	if req.SnoozeMarkUnread != nil {
		prefs.SnoozeMarkUnread = *req.SnoozeMarkUnread
	}

	if err := s.featuresStore.SavePreferences(r.Context(), prefs); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to save preferences", err)
		jsonErrorResponse(w, "Failed to save preferences", http.StatusInternalServerError)
		return
	}

	jsonResponse(w, prefs)
}

// =============================================================================
// Router functions (handle method dispatch)
// =============================================================================

// handleAliasesRouter routes /admin/api/aliases based on method
func (s *Server) handleAliasesRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAliasesList(w, r)
	case http.MethodPost:
		s.handleAliasCreate(w, r)
	default:
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAliasRouter routes /admin/api/aliases/{id} based on method
func (s *Server) handleAliasRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPatch, http.MethodPut:
		s.handleAliasUpdate(w, r)
	case http.MethodDelete:
		s.handleAliasDelete(w, r)
	case http.MethodPost:
		// Check for _method override
		if r.URL.Query().Get("_method") == "DELETE" {
			s.handleAliasDelete(w, r)
		} else if r.URL.Query().Get("_method") == "PATCH" {
			s.handleAliasUpdate(w, r)
		} else {
			s.handleAliasUpdate(w, r)
		}
	default:
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleScheduledRouter routes /admin/api/scheduled based on method
func (s *Server) handleScheduledRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleScheduledList(w, r)
	case http.MethodPost:
		s.handleScheduledCreate(w, r)
	default:
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSnoozeRouter routes /admin/api/messages/{id}/snooze based on method
func (s *Server) handleSnoozeRouter(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.URL.Path, "/snooze") {
		jsonErrorResponse(w, "Invalid URL", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPost:
		s.handleSnoozeCreate(w, r)
	case http.MethodDelete:
		s.handleSnoozeCancel(w, r)
	default:
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleVIPRouter routes /admin/api/vip based on method
func (s *Server) handleVIPRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleVIPList(w, r)
	case http.MethodPost:
		s.handleVIPAdd(w, r)
	default:
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handlePreferencesRouter routes /admin/api/preferences based on method
func (s *Server) handlePreferencesRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handlePreferencesGet(w, r)
	case http.MethodPatch, http.MethodPost, http.MethodPut:
		s.handlePreferencesUpdate(w, r)
	default:
		jsonErrorResponse(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// =============================================================================
// Helper functions
// =============================================================================

// getContextUserID extracts user ID from request context (set by auth middleware)
func (s *Server) getContextUserID(r *http.Request) (int64, error) {
	userID, ok := s.getSessionUserID(r)
	if !ok || userID == 0 {
		return 0, features.ErrNotFound
	}
	return userID, nil
}

// getDomainByID gets a domain name by its ID
func (s *Server) getDomainByID(ctx context.Context, domainID int64) (string, error) {
	var domain string
	err := s.db.QueryRowContext(ctx, "SELECT domain FROM domains WHERE id = ?", domainID).Scan(&domain)
	return domain, err
}
