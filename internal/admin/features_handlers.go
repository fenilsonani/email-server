package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/features"
)

// =============================================================================
// Features Overview Page
// =============================================================================

// handleFeatures shows the features overview/landing page
func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	// Get user preferences
	var prefs *features.UserPreferences
	if s.featuresStore != nil {
		prefs, _ = s.featuresStore.GetPreferences(r.Context(), userID)
	}

	data := map[string]interface{}{
		"Title":       "Features",
		"Preferences": prefs,
	}

	s.renderTemplate(w, "features.html", data)
}

// =============================================================================
// Screener Handlers
// =============================================================================

// handleScreener shows the screener contacts list
func (s *Server) handleScreener(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	// Get filter status
	filterStatus := r.URL.Query().Get("status")

	// Get contacts
	contacts, err := s.featuresStore.ListScreenerContacts(r.Context(), userID, features.ScreenerStatus(filterStatus))
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list screener contacts", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Count by status
	pending, _ := s.featuresStore.ListScreenerContacts(r.Context(), userID, features.ScreenerPending)
	approved, _ := s.featuresStore.ListScreenerContacts(r.Context(), userID, features.ScreenerApproved)
	blocked, _ := s.featuresStore.ListScreenerContacts(r.Context(), userID, features.ScreenerBlocked)

	data := map[string]interface{}{
		"Title":         "Screener",
		"Contacts":      contacts,
		"FilterStatus":  filterStatus,
		"PendingCount":  len(pending),
		"ApprovedCount": len(approved),
		"BlockedCount":  len(blocked),
	}

	s.renderTemplate(w, "features_screener.html", data)
}

// handleScreenerAction handles approve/block/delete actions
func (s *Server) handleScreenerAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	// Determine action from URL path
	path := r.URL.Path
	var action string
	var idStr string

	if strings.Contains(path, "/approve/") {
		action = "approve"
		idStr = strings.TrimPrefix(path, "/admin/features/screener/approve/")
	} else if strings.Contains(path, "/block/") {
		action = "block"
		idStr = strings.TrimPrefix(path, "/admin/features/screener/block/")
	} else if strings.Contains(path, "/delete/") {
		action = "delete"
		idStr = strings.TrimPrefix(path, "/admin/features/screener/delete/")
	}

	contactID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid contact ID", http.StatusBadRequest)
		return
	}

	// Get the contact to get email/domain
	contacts, err := s.featuresStore.ListScreenerContacts(r.Context(), userID, "")
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	var email, domain string
	for _, c := range contacts {
		if c.ID == contactID {
			email = c.Email
			domain = c.Domain
			break
		}
	}

	switch action {
	case "approve":
		err = s.featuresStore.ApproveContact(r.Context(), userID, email, domain)
	case "block":
		err = s.featuresStore.BlockContact(r.Context(), userID, email, domain)
	case "delete":
		err = s.featuresStore.DeleteScreenerContact(r.Context(), userID, contactID)
	}

	if err != nil {
		s.logger.ErrorContext(r.Context(), "Screener action failed", err, "action", action)
	}

	http.Redirect(w, r, "/admin/features/screener", http.StatusSeeOther)
}

// =============================================================================
// Aliases Handlers
// =============================================================================

// handleAliases shows the email aliases list
func (s *Server) handleAliases(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	aliases, err := s.featuresStore.ListAliases(r.Context(), userID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list aliases", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Count active vs inactive
	activeCount := 0
	for _, a := range aliases {
		if a.IsActive {
			activeCount++
		}
	}

	data := map[string]interface{}{
		"Title":         "Email Aliases",
		"Aliases":       aliases,
		"TotalCount":    len(aliases),
		"ActiveCount":   activeCount,
		"InactiveCount": len(aliases) - activeCount,
	}

	s.renderTemplate(w, "features_aliases.html", data)
}

// handleAliasAdd shows the add alias form and handles creation
func (s *Server) handleAliasAdd(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	// Get domains for dropdown
	rows, err := s.db.QueryContext(r.Context(), "SELECT id, name FROM domains ORDER BY name")
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get domains", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type domainItem struct {
		ID     int64
		Domain string
	}
	var domains []domainItem
	for rows.Next() {
		var d domainItem
		rows.Scan(&d.ID, &d.Domain)
		domains = append(domains, d)
	}

	if r.Method == http.MethodGet {
		data := map[string]interface{}{
			"Title":   "Create Alias",
			"Domains": domains,
		}
		s.renderTemplate(w, "features_alias_form.html", data)
		return
	}

	// POST: Create alias
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Bad request", formErrorStatus(err))
		return
	}

	domainID, _ := strconv.ParseInt(r.PostForm.Get("domain_id"), 10, 64)
	description := r.PostForm.Get("description")
	customLocal := r.PostForm.Get("local_part")

	// Get domain name
	var domainName string
	s.db.QueryRowContext(r.Context(), "SELECT name FROM domains WHERE id = ?", domainID).Scan(&domainName)

	// Generate or use custom local part
	localPart := customLocal
	if localPart == "" {
		localPart = features.GenerateAliasLocal(description)
	}

	alias := &features.EmailAlias{
		UserID:       userID,
		DomainID:     domainID,
		AliasLocal:   localPart,
		AliasAddress: localPart + "@" + domainName,
		Description:  description,
	}

	if err := s.featuresStore.CreateAlias(r.Context(), alias); err != nil {
		data := map[string]interface{}{
			"Title":   "Create Alias",
			"Domains": domains,
			"Error":   "Failed to create alias: " + err.Error(),
		}
		s.renderTemplate(w, "features_alias_form.html", data)
		return
	}

	http.Redirect(w, r, "/admin/features/aliases", http.StatusSeeOther)
}

// handleAliasToggle toggles an alias active/inactive
func (s *Server) handleAliasToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	aliasID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/features/aliases/toggle/"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid alias ID", http.StatusBadRequest)
		return
	}

	// Get current state
	aliases, _ := s.featuresStore.ListAliases(r.Context(), userID)
	var currentActive bool
	for _, a := range aliases {
		if a.ID == aliasID {
			currentActive = a.IsActive
			break
		}
	}

	// Toggle
	newActive := !currentActive
	s.featuresStore.UpdateAlias(r.Context(), userID, aliasID, &newActive, nil)

	http.Redirect(w, r, "/admin/features/aliases", http.StatusSeeOther)
}

// handleAliasDelete deletes an alias
func (s *Server) handleAliasDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	aliasID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/features/aliases/delete/"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid alias ID", http.StatusBadRequest)
		return
	}

	s.featuresStore.DeleteAlias(r.Context(), userID, aliasID)
	http.Redirect(w, r, "/admin/features/aliases", http.StatusSeeOther)
}

// =============================================================================
// VIP Handlers
// =============================================================================

// handleVIP shows the VIP contacts list
func (s *Server) handleVIP(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	vips, err := s.featuresStore.ListVIPs(r.Context(), userID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list VIPs", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Title": "VIP Contacts",
		"VIPs":  vips,
		"Count": len(vips),
	}

	s.renderTemplate(w, "features_vip.html", data)
}

// handleVIPAdd shows the add VIP form and handles creation
func (s *Server) handleVIPAdd(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	if r.Method == http.MethodGet {
		data := map[string]interface{}{
			"Title": "Add VIP Contact",
		}
		s.renderTemplate(w, "features_vip_form.html", data)
		return
	}

	// POST: Add VIP
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Bad request", formErrorStatus(err))
		return
	}

	vip := &features.VIPContact{
		UserID: userID,
		Email:  r.PostForm.Get("email"),
		Name:   r.PostForm.Get("name"),
	}

	if err := s.featuresStore.AddVIP(r.Context(), vip); err != nil {
		data := map[string]interface{}{
			"Title": "Add VIP Contact",
			"Error": "Failed to add VIP: " + err.Error(),
		}
		s.renderTemplate(w, "features_vip_form.html", data)
		return
	}

	http.Redirect(w, r, "/admin/features/vip", http.StatusSeeOther)
}

// handleVIPRemove removes a VIP contact
func (s *Server) handleVIPRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	vipID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/features/vip/delete/"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid VIP ID", http.StatusBadRequest)
		return
	}

	s.featuresStore.DeleteVIP(r.Context(), userID, vipID)
	http.Redirect(w, r, "/admin/features/vip", http.StatusSeeOther)
}

// =============================================================================
// Preferences Handler
// =============================================================================

// handlePreferences shows and updates user preferences
func (s *Server) handlePreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	prefs, err := s.featuresStore.GetPreferences(r.Context(), userID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to get preferences", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodGet {
		data := map[string]interface{}{
			"Title":       "Preferences",
			"Preferences": prefs,
		}
		s.renderTemplate(w, "features_preferences.html", data)
		return
	}

	// POST: Update preferences
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Bad request", formErrorStatus(err))
		return
	}

	// Parse form values
	undoDelay, _ := strconv.Atoi(r.PostForm.Get("undo_send_delay"))
	prefs.UndoSendDelay = undoDelay
	prefs.ScreenerEnabled = r.PostForm.Get("screener_enabled") == "on"
	prefs.TrackerBlocking = r.PostForm.Get("tracker_blocking")
	prefs.ZonesEnabled = r.PostForm.Get("zones_enabled") == "on"
	prefs.SnoozeMarkUnread = r.PostForm.Get("snooze_mark_unread") == "on"

	if err := s.featuresStore.SavePreferences(r.Context(), prefs); err != nil {
		data := map[string]interface{}{
			"Title":       "Preferences",
			"Preferences": prefs,
			"Error":       "Failed to save preferences",
		}
		s.renderTemplate(w, "features_preferences.html", data)
		return
	}

	data := map[string]interface{}{
		"Title":       "Preferences",
		"Preferences": prefs,
		"Success":     "Preferences saved successfully",
	}
	s.renderTemplate(w, "features_preferences.html", data)
}

// =============================================================================
// Scheduled Email (Send Later) Handlers
// =============================================================================

// handleScheduled shows the list of scheduled emails
func (s *Server) handleScheduled(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	// Get filter status
	filterStatus := r.URL.Query().Get("status")
	if filterStatus == "" {
		filterStatus = features.ScheduledStatusPending
	}

	emails, err := s.featuresStore.ListScheduledEmails(r.Context(), userID, filterStatus)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list scheduled emails", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Count by status
	pending, _ := s.featuresStore.ListScheduledEmails(r.Context(), userID, features.ScheduledStatusPending)
	sent, _ := s.featuresStore.ListScheduledEmails(r.Context(), userID, features.ScheduledStatusSent)
	cancelled, _ := s.featuresStore.ListScheduledEmails(r.Context(), userID, features.ScheduledStatusCancelled)

	data := map[string]interface{}{
		"Title":          "Scheduled Emails",
		"Emails":         emails,
		"FilterStatus":   filterStatus,
		"PendingCount":   len(pending),
		"SentCount":      len(sent),
		"CancelledCount": len(cancelled),
	}

	s.renderTemplate(w, "features_scheduled.html", data)
}

// handleScheduledAdd shows the schedule email form and handles creation
func (s *Server) handleScheduledAdd(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	// Get user's primary email
	var userEmail string
	s.db.QueryRowContext(r.Context(), "SELECT username FROM users WHERE id = ?", userID).Scan(&userEmail)

	// Get user's aliases for the from dropdown
	type fromAddress struct {
		Email       string
		Description string
	}
	fromAddresses := []fromAddress{{Email: userEmail, Description: "Primary"}}

	// Add aliases
	aliases, _ := s.featuresStore.ListAliases(r.Context(), userID)
	for _, alias := range aliases {
		if alias.IsActive {
			desc := alias.Description
			if desc == "" {
				desc = "Alias"
			}
			fromAddresses = append(fromAddresses, fromAddress{Email: alias.AliasAddress, Description: desc})
		}
	}

	if r.Method == http.MethodGet {
		data := map[string]interface{}{
			"Title":         "Schedule Email",
			"FromAddresses": fromAddresses,
		}
		s.renderTemplate(w, "features_scheduled_form.html", data)
		return
	}

	// POST: Create scheduled email
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Bad request", formErrorStatus(err))
		return
	}

	// Parse send time
	sendAtStr := r.PostForm.Get("send_at")
	sendAt, err := time.Parse("2006-01-02T15:04", sendAtStr)
	if err != nil {
		data := map[string]interface{}{
			"Title":         "Schedule Email",
			"FromAddresses": fromAddresses,
			"Error":         "Invalid date/time format",
		}
		s.renderTemplate(w, "features_scheduled_form.html", data)
		return
	}

	// Parse recipients
	recipientsStr := r.PostForm.Get("recipients")
	recipients := strings.Split(recipientsStr, ",")
	for i, r := range recipients {
		recipients[i] = strings.TrimSpace(r)
	}

	email := &features.ScheduledEmail{
		UserID:      userID,
		SendAt:      sendAt,
		FromAddress: r.PostForm.Get("from_address"),
		Recipients:  recipients,
		Subject:     r.PostForm.Get("subject"),
		Body:        r.PostForm.Get("body"),
	}

	if err := s.featuresStore.CreateScheduledEmail(r.Context(), email); err != nil {
		data := map[string]interface{}{
			"Title":         "Schedule Email",
			"FromAddresses": fromAddresses,
			"Error":         "Failed to schedule email: " + err.Error(),
		}
		s.renderTemplate(w, "features_scheduled_form.html", data)
		return
	}

	http.Redirect(w, r, "/admin/features/scheduled", http.StatusSeeOther)
}

// handleScheduledCancel cancels a scheduled email
func (s *Server) handleScheduledCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	emailID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/features/scheduled/cancel/"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid email ID", http.StatusBadRequest)
		return
	}

	s.featuresStore.CancelScheduledEmail(r.Context(), userID, emailID)
	http.Redirect(w, r, "/admin/features/scheduled", http.StatusSeeOther)
}

// =============================================================================
// Snoozed Email Handlers
// =============================================================================

// handleSnoozed shows the list of snoozed emails
func (s *Server) handleSnoozed(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	snoozed, err := s.featuresStore.ListSnoozedEmails(r.Context(), userID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list snoozed emails", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Title":   "Snoozed Emails",
		"Snoozed": snoozed,
		"Count":   len(snoozed),
	}

	s.renderTemplate(w, "features_snoozed.html", data)
}

// handleSnoozeCancel cancels a snooze (wakes the email immediately)
func (s *Server) handleSnoozeCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if s.featuresStore == nil {
		http.Error(w, "Features not enabled", http.StatusServiceUnavailable)
		return
	}

	snoozeID, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/features/snoozed/cancel/"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid snooze ID", http.StatusBadRequest)
		return
	}

	s.featuresStore.DeleteSnoozedEmail(r.Context(), userID, snoozeID)
	http.Redirect(w, r, "/admin/features/snoozed", http.StatusSeeOther)
}
