package admin

import (
	"net/http"
	"strconv"
	"strings"

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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	domainID, _ := strconv.ParseInt(r.FormValue("domain_id"), 10, 64)
	description := r.FormValue("description")
	customLocal := r.FormValue("local_part")

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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	vip := &features.VIPContact{
		UserID: userID,
		Email:  r.FormValue("email"),
		Name:   r.FormValue("name"),
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
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	// Parse form values
	undoDelay, _ := strconv.Atoi(r.FormValue("undo_send_delay"))
	prefs.UndoSendDelay = undoDelay
	prefs.ScreenerEnabled = r.FormValue("screener_enabled") == "on"
	prefs.TrackerBlocking = r.FormValue("tracker_blocking")
	prefs.ZonesEnabled = r.FormValue("zones_enabled") == "on"
	prefs.SnoozeMarkUnread = r.FormValue("snooze_mark_unread") == "on"

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
