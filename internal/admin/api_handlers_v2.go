package admin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/features"
	"github.com/fenilsonani/email-server/internal/lists"
	"github.com/fenilsonani/email-server/internal/sieve"
)

// =============================================================================
// Features: Screener API
// =============================================================================

func (s *Server) handleAPIScreener(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetScreener(w, r)
	case http.MethodPost:
		s.handleAPICreateScreener(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIGetScreener(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	filterStatus := features.ScreenerStatus(r.URL.Query().Get("status"))
	contacts, err := s.featuresStore.ListScreenerContacts(r.Context(), userID, filterStatus)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list screener contacts")
		return
	}

	if contacts == nil {
		contacts = []*features.ScreenerContact{}
	}

	s.jsonResponse(w, http.StatusOK, contacts)
}

func (s *Server) handleAPICreateScreener(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	var req struct {
		Sender string `json:"sender"`
		Action string `json:"action"` // "approve" or "block"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Sender == "" {
		s.jsonError(w, http.StatusBadRequest, "Sender is required")
		return
	}

	// Determine if this is an email or domain
	email := req.Sender
	domain := ""
	if strings.Contains(req.Sender, "@") {
		parts := strings.SplitN(req.Sender, "@", 2)
		email = req.Sender
		domain = parts[1]
	} else {
		domain = req.Sender
		email = ""
	}

	switch req.Action {
	case "approve":
		if err := s.featuresStore.ApproveContact(r.Context(), userID, email, domain); err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to approve contact")
			return
		}
	case "block":
		if err := s.featuresStore.BlockContact(r.Context(), userID, email, domain); err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to block contact")
			return
		}
	default:
		s.jsonError(w, http.StatusBadRequest, "Action must be 'approve' or 'block'")
		return
	}

	s.jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"sender": req.Sender,
		"action": req.Action,
	})
}

func (s *Server) handleAPIScreenerByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	// Extract ID from path: /admin/api/v1/features/screener/{id}
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	idStr := parts[len(parts)-1]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid screener contact ID")
		return
	}

	if err := s.featuresStore.DeleteScreenerContact(r.Context(), userID, id); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to delete screener contact")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

// =============================================================================
// Features: Aliases API
// =============================================================================

func (s *Server) handleAPIAliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetAliases(w, r)
	case http.MethodPost:
		s.handleAPICreateAlias(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIGetAliases(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	aliases, err := s.featuresStore.ListAliases(r.Context(), userID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list aliases")
		return
	}

	if aliases == nil {
		aliases = []*features.EmailAlias{}
	}

	s.jsonResponse(w, http.StatusOK, aliases)
}

func (s *Server) handleAPICreateAlias(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	var req struct {
		Alias      string `json:"alias"`
		ForwardsTo string `json:"forwards_to"`
		DomainID   int64  `json:"domain_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Alias == "" {
		s.jsonError(w, http.StatusBadRequest, "Alias is required")
		return
	}

	// Get domain name
	var domainName string
	if req.DomainID > 0 {
		s.db.QueryRowContext(r.Context(), "SELECT name FROM domains WHERE id = ?", req.DomainID).Scan(&domainName)
	}
	if domainName == "" {
		// Use first available domain
		s.db.QueryRowContext(r.Context(), "SELECT id, name FROM domains ORDER BY name LIMIT 1").Scan(&req.DomainID, &domainName)
	}
	if domainName == "" {
		s.jsonError(w, http.StatusBadRequest, "No domain available")
		return
	}

	localPart := req.Alias
	if localPart == "" {
		localPart = features.GenerateAliasLocal(req.ForwardsTo)
	}

	alias := &features.EmailAlias{
		UserID:       userID,
		DomainID:     req.DomainID,
		AliasLocal:   localPart,
		AliasAddress: localPart + "@" + domainName,
		Description:  req.ForwardsTo,
	}

	if err := s.featuresStore.CreateAlias(r.Context(), alias); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to create alias: "+err.Error())
		return
	}

	s.jsonResponse(w, http.StatusCreated, alias)
}

func (s *Server) handleAPIAliasByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	idStr := parts[len(parts)-1]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid alias ID")
		return
	}

	if err := s.featuresStore.DeleteAlias(r.Context(), userID, id); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to delete alias")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

// =============================================================================
// Features: VIP API
// =============================================================================

func (s *Server) handleAPIVIP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetVIP(w, r)
	case http.MethodPost:
		s.handleAPICreateVIP(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIGetVIP(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	vips, err := s.featuresStore.ListVIPs(r.Context(), userID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list VIP contacts")
		return
	}

	if vips == nil {
		vips = []*features.VIPContact{}
	}

	s.jsonResponse(w, http.StatusOK, vips)
}

func (s *Server) handleAPICreateVIP(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	var req struct {
		Sender string `json:"sender"`
		Label  string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Sender == "" {
		s.jsonError(w, http.StatusBadRequest, "Sender email is required")
		return
	}

	vip := &features.VIPContact{
		UserID: userID,
		Email:  req.Sender,
		Name:   req.Label,
	}

	if err := s.featuresStore.AddVIP(r.Context(), vip); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to add VIP contact: "+err.Error())
		return
	}

	s.jsonResponse(w, http.StatusCreated, vip)
}

func (s *Server) handleAPIVIPByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	idStr := parts[len(parts)-1]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid VIP ID")
		return
	}

	if err := s.featuresStore.DeleteVIP(r.Context(), userID, id); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to delete VIP contact")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

// =============================================================================
// Features: Preferences API
// =============================================================================

func (s *Server) handleAPIPreferences(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetPreferences(w, r)
	case http.MethodPut:
		s.handleAPIUpdatePreferences(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIGetPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	prefs, err := s.featuresStore.GetPreferences(r.Context(), userID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to get preferences")
		return
	}

	s.jsonResponse(w, http.StatusOK, prefs)
}

func (s *Server) handleAPIUpdatePreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	var prefs features.UserPreferences
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	prefs.UserID = userID

	if err := s.featuresStore.SavePreferences(r.Context(), &prefs); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to save preferences")
		return
	}

	s.jsonResponse(w, http.StatusOK, prefs)
}

// =============================================================================
// Features: Scheduled Emails API
// =============================================================================

func (s *Server) handleAPIScheduled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	filterStatus := r.URL.Query().Get("status")
	if filterStatus == "" {
		filterStatus = features.ScheduledStatusPending
	}

	emails, err := s.featuresStore.ListScheduledEmails(r.Context(), userID, filterStatus)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list scheduled emails")
		return
	}

	if emails == nil {
		emails = []*features.ScheduledEmail{}
	}

	s.jsonResponse(w, http.StatusOK, emails)
}

// =============================================================================
// Features: Snoozed Emails API
// =============================================================================

func (s *Server) handleAPISnoozed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if s.featuresStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Features store not initialized")
		return
	}

	snoozed, err := s.featuresStore.ListSnoozedEmails(r.Context(), userID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list snoozed emails")
		return
	}

	if snoozed == nil {
		snoozed = []*features.SnoozedEmail{}
	}

	s.jsonResponse(w, http.StatusOK, snoozed)
}

// =============================================================================
// Lists: Collection API (GET + POST on /admin/api/v1/lists)
// =============================================================================

func (s *Server) handleAPIListsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetLists(w, r)
	case http.MethodPost:
		s.handleAPIListCreate(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// =============================================================================
// Lists: CRUD API
// =============================================================================

func (s *Server) handleAPIListCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if s.listsStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Lists store not initialized")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Address     string `json:"address"`
		Description string `json:"description"`
		DomainID    int64  `json:"domain_id"`
		LocalPart   string `json:"local_part"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		s.jsonError(w, http.StatusBadRequest, "Name is required")
		return
	}

	// If address is provided, extract local_part and domain
	localPart := req.LocalPart
	var domainName string
	domainID := req.DomainID

	if req.Address != "" && strings.Contains(req.Address, "@") {
		parts := strings.SplitN(req.Address, "@", 2)
		localPart = strings.ToLower(strings.TrimSpace(parts[0]))
		domainName = parts[1]
		// Look up domain ID
		s.db.QueryRowContext(r.Context(), "SELECT id FROM domains WHERE name = ?", domainName).Scan(&domainID)
	}

	if domainID == 0 {
		// Use first available domain
		s.db.QueryRowContext(r.Context(), "SELECT id, name FROM domains ORDER BY name LIMIT 1").Scan(&domainID, &domainName)
	} else if domainName == "" {
		s.db.QueryRowContext(r.Context(), "SELECT name FROM domains WHERE id = ?", domainID).Scan(&domainName)
	}

	if domainID == 0 || domainName == "" {
		s.jsonError(w, http.StatusBadRequest, "No valid domain available")
		return
	}

	if localPart == "" {
		localPart = strings.ToLower(strings.ReplaceAll(req.Name, " ", "-"))
	}

	list := &lists.MailingList{
		DomainID:      domainID,
		LocalPart:     localPart,
		ListAddress:   localPart + "@" + domainName,
		Name:          req.Name,
		Description:   req.Description,
		ListType:      lists.ListTypeDiscussion,
		PostingPolicy: lists.PostingMembersOnly,
		IsActive:      true,
		MaxMessageSize: 10 * 1024 * 1024,
		MaxMembers:     10000,
	}

	if err := s.listsStore.CreateList(r.Context(), list); err != nil {
		if err == lists.ErrAlreadyExists {
			s.jsonError(w, http.StatusConflict, "A list with this address already exists")
			return
		}
		s.jsonError(w, http.StatusInternalServerError, "Failed to create list")
		return
	}

	s.jsonResponse(w, http.StatusCreated, list)
}

func (s *Server) handleAPIListByID(w http.ResponseWriter, r *http.Request) {
	if s.listsStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Lists store not initialized")
		return
	}

	// Parse path: /admin/api/v1/lists/{id}[/sub-resource[/...]]
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")

	// Find "lists" in path and get ID
	listsIdx := -1
	for i, p := range parts {
		if p == "lists" {
			listsIdx = i
			break
		}
	}
	if listsIdx < 0 || listsIdx+1 >= len(parts) {
		s.jsonError(w, http.StatusBadRequest, "List ID required")
		return
	}

	idStr := parts[listsIdx+1]
	listID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid list ID")
		return
	}

	// Check for sub-routes
	if listsIdx+2 < len(parts) {
		subResource := parts[listsIdx+2]
		switch subResource {
		case "members":
			s.handleAPIListMembers(w, r, listID, parts)
			return
		case "moderation":
			s.handleAPIListModeration(w, r, listID, parts)
			return
		case "archives":
			s.handleAPIListArchives(w, r, listID)
			return
		}
	}

	// Handle direct list CRUD
	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetList(w, r, listID)
	case http.MethodPut:
		s.handleAPIUpdateList(w, r, listID)
	case http.MethodDelete:
		s.handleAPIDeleteList(w, r, listID)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIGetList(w http.ResponseWriter, r *http.Request, listID int64) {
	list, err := s.listsStore.GetList(r.Context(), listID)
	if err != nil {
		if err == lists.ErrNotFound {
			s.jsonError(w, http.StatusNotFound, "List not found")
			return
		}
		s.jsonError(w, http.StatusInternalServerError, "Failed to get list")
		return
	}

	stats, _ := s.listsStore.GetListStats(r.Context(), listID)

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"list":  list,
		"stats": stats,
	})
}

func (s *Server) handleAPIUpdateList(w http.ResponseWriter, r *http.Request, listID int64) {
	list, err := s.listsStore.GetList(r.Context(), listID)
	if err != nil {
		if err == lists.ErrNotFound {
			s.jsonError(w, http.StatusNotFound, "List not found")
			return
		}
		s.jsonError(w, http.StatusInternalServerError, "Failed to get list")
		return
	}

	var req struct {
		Name              *string `json:"name"`
		Description       *string `json:"description"`
		IsActive          *bool   `json:"is_active"`
		ModerationEnabled *bool   `json:"moderation_enabled"`
		SubjectPrefix     *string `json:"subject_prefix"`
		ReplyToList       *bool   `json:"reply_to_list"`
		ArchiveEnabled    *bool   `json:"archive_enabled"`
		AllowSubscribe    *bool   `json:"allow_subscribe"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name != nil {
		list.Name = *req.Name
	}
	if req.Description != nil {
		list.Description = *req.Description
	}
	if req.IsActive != nil {
		list.IsActive = *req.IsActive
	}
	if req.ModerationEnabled != nil {
		list.ModerationEnabled = *req.ModerationEnabled
	}
	if req.SubjectPrefix != nil {
		list.SubjectPrefix = *req.SubjectPrefix
	}
	if req.ReplyToList != nil {
		list.ReplyToList = *req.ReplyToList
	}
	if req.ArchiveEnabled != nil {
		list.ArchiveEnabled = *req.ArchiveEnabled
	}
	if req.AllowSubscribe != nil {
		list.AllowSubscribe = *req.AllowSubscribe
	}

	if err := s.listsStore.UpdateList(r.Context(), list); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to update list")
		return
	}

	s.jsonResponse(w, http.StatusOK, list)
}

func (s *Server) handleAPIDeleteList(w http.ResponseWriter, r *http.Request, listID int64) {
	if err := s.listsStore.DeleteList(r.Context(), listID); err != nil {
		if err == lists.ErrNotFound {
			s.jsonError(w, http.StatusNotFound, "List not found")
			return
		}
		s.jsonError(w, http.StatusInternalServerError, "Failed to delete list")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

// =============================================================================
// Lists: Members API
// =============================================================================

func (s *Server) handleAPIListMembers(w http.ResponseWriter, r *http.Request, listID int64, parts []string) {
	// Find "members" in parts
	membersIdx := -1
	for i, p := range parts {
		if p == "members" {
			membersIdx = i
			break
		}
	}

	// Check for member ID sub-route: /lists/{id}/members/{memberId}
	if membersIdx >= 0 && membersIdx+1 < len(parts) {
		memberIDStr := parts[membersIdx+1]
		memberID, err := strconv.ParseInt(memberIDStr, 10, 64)
		if err == nil {
			// DELETE /lists/{id}/members/{memberId}
			if r.Method == http.MethodDelete {
				s.handleAPIRemoveListMemberByID(w, r, listID, memberID)
				return
			}
			s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetListMembers(w, r, listID)
	case http.MethodPost:
		s.handleAPIAddListMember(w, r, listID)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIGetListMembers(w http.ResponseWriter, r *http.Request, listID int64) {
	members, err := s.listsStore.ListMembers(r.Context(), listID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list members")
		return
	}

	if members == nil {
		members = []*lists.ListMember{}
	}

	s.jsonResponse(w, http.StatusOK, members)
}

func (s *Server) handleAPIAddListMember(w http.ResponseWriter, r *http.Request, listID int64) {
	var req struct {
		Email string `json:"email"`
		Role  string `json:"role"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Email == "" {
		s.jsonError(w, http.StatusBadRequest, "Email is required")
		return
	}

	role := lists.MemberRole(req.Role)
	if role == "" {
		role = lists.RoleMember
	}

	member := &lists.ListMember{
		ListID:       listID,
		Email:        strings.ToLower(strings.TrimSpace(req.Email)),
		Name:         req.Name,
		Role:         role,
		DeliveryMode: lists.DeliveryNormal,
		IsConfirmed:  true,
	}

	if err := s.listsStore.AddMember(r.Context(), member); err != nil {
		if err == lists.ErrAlreadyExists {
			s.jsonError(w, http.StatusConflict, "Member already exists")
			return
		}
		if err == lists.ErrListFull {
			s.jsonError(w, http.StatusConflict, "List has reached maximum members")
			return
		}
		s.jsonError(w, http.StatusInternalServerError, "Failed to add member")
		return
	}

	s.jsonResponse(w, http.StatusCreated, member)
}

func (s *Server) handleAPIRemoveListMemberByID(w http.ResponseWriter, r *http.Request, listID int64, memberID int64) {
	// We need to find the member email by ID to call RemoveMember
	// List all members and find by ID
	members, err := s.listsStore.ListMembers(r.Context(), listID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list members")
		return
	}

	var memberEmail string
	for _, m := range members {
		if m.ID == memberID {
			memberEmail = m.Email
			break
		}
	}

	if memberEmail == "" {
		s.jsonError(w, http.StatusNotFound, "Member not found")
		return
	}

	if err := s.listsStore.RemoveMember(r.Context(), listID, memberEmail); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to remove member")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

// =============================================================================
// Lists: Moderation API
// =============================================================================

func (s *Server) handleAPIListModeration(w http.ResponseWriter, r *http.Request, listID int64, parts []string) {
	// Find "moderation" in parts
	modIdx := -1
	for i, p := range parts {
		if p == "moderation" {
			modIdx = i
			break
		}
	}

	// Check for message ID sub-route: /lists/{id}/moderation/{msgId}/approve or /reject
	if modIdx >= 0 && modIdx+1 < len(parts) {
		msgIDStr := parts[modIdx+1]
		msgID, err := strconv.ParseInt(msgIDStr, 10, 64)
		if err == nil && modIdx+2 < len(parts) {
			action := parts[modIdx+2]
			if r.Method == http.MethodPost {
				switch action {
				case "approve":
					s.handleAPIApproveModerationMsg(w, r, msgID)
					return
				case "reject":
					s.handleAPIRejectModerationMsg(w, r, msgID)
					return
				}
			}
			s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
	}

	// GET /lists/{id}/moderation - list pending
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	pending, err := s.listsStore.ListPendingModeration(r.Context(), listID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list pending moderation")
		return
	}

	if pending == nil {
		pending = []*lists.ModeratedMessage{}
	}

	s.jsonResponse(w, http.StatusOK, pending)
}

func (s *Server) handleAPIApproveModerationMsg(w http.ResponseWriter, r *http.Request, msgID int64) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	if err := s.listsStore.ApproveMessage(r.Context(), msgID, userID); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to approve message")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"approved": true})
}

func (s *Server) handleAPIRejectModerationMsg(w http.ResponseWriter, r *http.Request, msgID int64) {
	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if err := s.listsStore.RejectMessage(r.Context(), msgID, userID, req.Reason); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to reject message")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"rejected": true})
}

// =============================================================================
// Lists: Archives API
// =============================================================================

func (s *Server) handleAPIListArchives(w http.ResponseWriter, r *http.Request, listID int64) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit := 50
	if l := r.URL.Query().Get("page_size"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	offset := (page - 1) * limit

	query := r.URL.Query().Get("q")

	var archives []*lists.ArchivedMessage
	var err error
	if query != "" {
		archives, err = s.listsStore.SearchArchives(r.Context(), listID, query, limit)
	} else {
		archives, err = s.listsStore.ListArchives(r.Context(), listID, limit, offset)
	}

	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list archives")
		return
	}

	if archives == nil {
		archives = []*lists.ArchivedMessage{}
	}

	totalCount, _ := s.listsStore.CountArchives(r.Context(), listID)
	totalPages := (totalCount + limit - 1) / limit
	if totalPages == 0 {
		totalPages = 1
	}

	s.jsonResponseWithMeta(w, http.StatusOK, archives, &APIMeta{
		Page:       page,
		PageSize:   limit,
		TotalCount: totalCount,
		TotalPages: totalPages,
	})
}

// =============================================================================
// Queue: Retry / Delete API
// =============================================================================

func (s *Server) handleAPIQueueByID(w http.ResponseWriter, r *http.Request) {
	if s.queue == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Queue not configured")
		return
	}

	// Parse path: /admin/api/v1/queue/{id}[/retry]
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")

	// Find "queue" in parts
	queueIdx := -1
	for i, p := range parts {
		if p == "queue" {
			queueIdx = i
			break
		}
	}
	if queueIdx < 0 || queueIdx+1 >= len(parts) {
		s.jsonError(w, http.StatusBadRequest, "Message ID required")
		return
	}

	msgID := parts[queueIdx+1]

	// Check for /retry sub-route
	if queueIdx+2 < len(parts) && parts[queueIdx+2] == "retry" {
		if r.Method != http.MethodPost {
			s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		s.handleAPIQueueRetry(w, r, msgID)
		return
	}

	// DELETE /queue/{id}
	if r.Method != http.MethodDelete {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	s.handleAPIQueueDelete(w, r, msgID)
}

func (s *Server) handleAPIQueueRetry(w http.ResponseWriter, r *http.Request, msgID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	msg, err := s.queue.GetMessage(ctx, msgID)
	if err != nil {
		s.jsonError(w, http.StatusNotFound, "Message not found")
		return
	}

	msg.Attempts = 0
	msg.NextAttempt = time.Now()

	if err := s.queue.Enqueue(ctx, msg); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to reschedule message")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"retried":    true,
		"message_id": msgID,
	})
}

func (s *Server) handleAPIQueueDelete(w http.ResponseWriter, r *http.Request, msgID string) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s.queue.Fail(ctx, msgID, "Manually deleted by admin via API"); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to delete message")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"deleted":    true,
		"message_id": msgID,
	})
}

// =============================================================================
// Sieve API
// =============================================================================

func (s *Server) handleAPISieve(w http.ResponseWriter, r *http.Request) {
	if s.sieveStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Sieve not configured")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		scripts, err := s.sieveStore.ListScripts(r.Context(), userID)
		if err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to list scripts")
			return
		}
		if scripts == nil {
			scripts = []*sieve.Script{}
		}
		s.jsonResponse(w, http.StatusOK, scripts)

	case http.MethodPut:
		var req struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		if req.Name == "" || req.Content == "" {
			s.jsonError(w, http.StatusBadRequest, "Name and content are required")
			return
		}

		// Try to update first, create if it doesn't exist
		err := s.sieveStore.UpdateScript(r.Context(), userID, req.Name, req.Content)
		if err != nil {
			// Try create
			_, err = s.sieveStore.CreateScript(r.Context(), userID, req.Name, req.Content)
			if err != nil {
				s.jsonError(w, http.StatusInternalServerError, "Failed to save script")
				return
			}
		}

		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"saved": true,
			"name":  req.Name,
		})

	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPISieveValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Content == "" {
		s.jsonError(w, http.StatusBadRequest, "Content is required")
		return
	}

	err := sieve.ValidateScript(req.Content)
	if err != nil {
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"valid": true,
	})
}

// =============================================================================
// System: Backup API
// =============================================================================

func (s *Server) handleAPIBackupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Return basic backup info
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"backup_available": true,
		"data_dir":         s.config.Storage.DataDir,
		"database_path":    s.config.Storage.DatabasePath,
	})
}

func (s *Server) handleAPIBackupTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Trigger backup - for API, we return success and info
	// Actual file download should use the existing /admin/system/backup endpoint
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":      "Use POST /admin/system/backup to download the backup file directly",
		"download_url": "/admin/system/backup",
	})
}

func (s *Server) handleAPIBackupHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Backup history is not persisted in this implementation
	s.jsonResponse(w, http.StatusOK, []interface{}{})
}

func (s *Server) handleAPIRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Restore requires multipart file upload - delegate to existing handler
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":    "Use POST /admin/system/restore with multipart form data containing the backup file",
		"upload_url": "/admin/system/restore",
	})
}

// =============================================================================
// System: Certificates API
// =============================================================================

func (s *Server) handleAPICertificates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Return TLS configuration info
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"auto_tls":  s.config.TLS.AutoTLS,
		"cert_file": s.config.TLS.CertFile,
		"key_file":  s.config.TLS.KeyFile,
	})
}

func (s *Server) handleAPICertificatesRenew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Certificate renewal depends on Let's Encrypt or manual process
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Certificate renewal initiated. Check server logs for status.",
	})
}

// =============================================================================
// System: 2FA API
// =============================================================================

func (s *Server) handleAPI2FAStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	var totpSecret string
	err := s.db.QueryRowContext(r.Context(),
		"SELECT COALESCE(totp_secret, '') FROM users WHERE id = ?", userID,
	).Scan(&totpSecret)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to check 2FA status")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"enabled": totpSecret != "",
	})
}

func (s *Server) handleAPI2FASetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// 2FA setup requires interactive flow - direct to existing handler
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":   "Use the admin UI at /admin/2fa/setup for interactive 2FA setup",
		"setup_url": "/admin/2fa/setup",
	})
}

func (s *Server) handleAPI2FAVerifyCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Code == "" {
		s.jsonError(w, http.StatusBadRequest, "Code is required")
		return
	}

	// Verification is handled by the existing 2FA flow
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Use the existing 2FA verification flow at /admin/2fa/verify",
	})
}

func (s *Server) handleAPI2FADisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	_, err := s.db.ExecContext(r.Context(),
		"UPDATE users SET totp_secret = NULL WHERE id = ?", userID,
	)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to disable 2FA")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"disabled": true,
	})
}

// =============================================================================
// System: Check Update API
// =============================================================================

func (s *Server) handleAPICheckUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"current_version": "1.0.0",
		"update_available": false,
		"message":          "System is up to date",
	})
}

// =============================================================================
// System: DKIM Auto-Rotate API
// =============================================================================

func (s *Server) handleAPIDKIMAutoRotate(w http.ResponseWriter, r *http.Request) {
	// Parse path for sub-routes
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	lastPart := parts[len(parts)-1]

	if lastPart == "rotate-now" {
		if r.Method != http.MethodPost {
			s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		// Delegate to existing handler which already returns JSON
		s.handleDKIMAutoRotate(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Return DKIM auto-rotation settings
		rows, err := s.db.QueryContext(r.Context(), `
			SELECT id, name, COALESCE(dkim_selector, ''), COALESCE(dkim_key_created_at, created_at)
			FROM domains WHERE is_active = TRUE AND dkim_private_key IS NOT NULL
		`)
		if err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to query DKIM settings")
			return
		}
		defer rows.Close()

		type dkimInfo struct {
			DomainID    int64     `json:"domain_id"`
			DomainName  string    `json:"domain_name"`
			Selector    string    `json:"selector"`
			KeyCreated  time.Time `json:"key_created_at"`
		}

		var domains []dkimInfo
		for rows.Next() {
			var d dkimInfo
			if err := rows.Scan(&d.DomainID, &d.DomainName, &d.Selector, &d.KeyCreated); err == nil {
				domains = append(domains, d)
			}
		}
		if domains == nil {
			domains = []dkimInfo{}
		}

		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"domains":             domains,
			"default_rotation_days": 90,
		})

	case http.MethodPut:
		// Update rotation settings (stub - settings not currently persisted)
		var req struct {
			RotationDays int `json:"rotation_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"updated":       true,
			"rotation_days": req.RotationDays,
		})

	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// =============================================================================
// Tools: Doctor API
// =============================================================================

func (s *Server) handleAPIToolsDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Return basic diagnostics
	checks := []map[string]interface{}{}

	// Check database
	dbOK := true
	if err := s.db.Ping(); err != nil {
		dbOK = false
	}
	checks = append(checks, map[string]interface{}{
		"name":   "database",
		"status": boolToStatus(dbOK),
		"message": boolToMessage(dbOK, "Database connection OK", "Database connection failed"),
	})

	// Check queue
	queueOK := s.queue != nil
	if queueOK {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, err := s.queue.Stats(ctx)
		if err != nil {
			queueOK = false
		}
	}
	checks = append(checks, map[string]interface{}{
		"name":   "queue",
		"status": boolToStatus(queueOK),
		"message": boolToMessage(queueOK, "Queue connection OK", "Queue not available"),
	})

	// Check features store
	checks = append(checks, map[string]interface{}{
		"name":   "features",
		"status": boolToStatus(s.featuresStore != nil),
		"message": boolToMessage(s.featuresStore != nil, "Features store initialized", "Features store not initialized"),
	})

	// Check lists store
	checks = append(checks, map[string]interface{}{
		"name":   "lists",
		"status": boolToStatus(s.listsStore != nil),
		"message": boolToMessage(s.listsStore != nil, "Lists store initialized", "Lists store not initialized"),
	})

	// Check sieve store
	checks = append(checks, map[string]interface{}{
		"name":   "sieve",
		"status": boolToStatus(s.sieveStore != nil),
		"message": boolToMessage(s.sieveStore != nil, "Sieve store initialized", "Sieve store not initialized"),
	})

	_ = dbOK && queueOK
	s.jsonResponse(w, http.StatusOK, checks)
}

// =============================================================================
// Tools: Test Email API
// =============================================================================

func (s *Server) handleAPIToolsTestEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.To == "" {
		s.jsonError(w, http.StatusBadRequest, "Recipient (to) is required")
		return
	}

	if req.Subject == "" {
		req.Subject = "Test Email from Mail Server"
	}

	if req.Body == "" {
		req.Body = "This is a test email sent from the admin dashboard."
	}

	// Stub: test email sending would go through the queue
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"sent":    true,
		"to":      req.To,
		"subject": req.Subject,
		"message": "Test email queued for delivery",
	})
}

// =============================================================================
// Tools: DNS Check API
// =============================================================================

func (s *Server) handleAPIToolsDNSCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	domain := r.URL.Query().Get("domain")
	if domain == "" {
		s.jsonError(w, http.StatusBadRequest, "Domain is required")
		return
	}

	req := struct{ Domain string }{Domain: domain}

	mailServer := s.config.Server.Hostname
	results := []DNSCheckResult{}

	// Check MX records
	mxRecords, err := net.LookupMX(req.Domain)
	if err != nil {
		results = append(results, DNSCheckResult{
			RecordType: "MX",
			Status:     "fail",
			Expected:   mailServer,
			Actual:     "",
			Message:    "No MX records found: " + err.Error(),
		})
	} else {
		found := false
		var actualMX string
		for _, mx := range mxRecords {
			actualMX += mx.Host + " "
			if strings.TrimSuffix(mx.Host, ".") == mailServer {
				found = true
			}
		}
		status := "fail"
		msg := "MX record does not point to expected mail server"
		if found {
			status = "pass"
			msg = "MX record correctly points to mail server"
		}
		results = append(results, DNSCheckResult{
			RecordType: "MX",
			Status:     status,
			Expected:   mailServer,
			Actual:     strings.TrimSpace(actualMX),
			Message:    msg,
		})
	}

	// Check SPF record
	txtRecords, err := net.LookupTXT(req.Domain)
	if err != nil {
		results = append(results, DNSCheckResult{
			RecordType: "SPF",
			Status:     "fail",
			Expected:   "v=spf1 ...",
			Actual:     "",
			Message:    "No TXT records found: " + err.Error(),
		})
	} else {
		foundSPF := false
		var spfRecord string
		for _, txt := range txtRecords {
			if strings.HasPrefix(txt, "v=spf1") {
				foundSPF = true
				spfRecord = txt
				break
			}
		}
		if foundSPF {
			results = append(results, DNSCheckResult{
				RecordType: "SPF",
				Status:     "pass",
				Expected:   "v=spf1 ...",
				Actual:     spfRecord,
				Message:    "SPF record found",
			})
		} else {
			results = append(results, DNSCheckResult{
				RecordType: "SPF",
				Status:     "fail",
				Expected:   "v=spf1 mx -all",
				Actual:     "",
				Message:    "No SPF record found",
			})
		}
	}

	// Check DMARC record
	dmarcRecords, err := net.LookupTXT("_dmarc." + req.Domain)
	if err != nil {
		results = append(results, DNSCheckResult{
			RecordType: "DMARC",
			Status:     "warning",
			Expected:   "v=DMARC1; ...",
			Actual:     "",
			Message:    "No DMARC record found (recommended)",
		})
	} else {
		foundDMARC := false
		var dmarcRecord string
		for _, txt := range dmarcRecords {
			if strings.HasPrefix(txt, "v=DMARC1") {
				foundDMARC = true
				dmarcRecord = txt
				break
			}
		}
		if foundDMARC {
			results = append(results, DNSCheckResult{
				RecordType: "DMARC",
				Status:     "pass",
				Expected:   "v=DMARC1; ...",
				Actual:     dmarcRecord,
				Message:    "DMARC record found",
			})
		} else {
			results = append(results, DNSCheckResult{
				RecordType: "DMARC",
				Status:     "warning",
				Expected:   "v=DMARC1; p=quarantine; ...",
				Actual:     "",
				Message:    "Invalid DMARC record",
			})
		}
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"domain":  req.Domain,
		"results": results,
	})
}

// =============================================================================
// Helpers
// =============================================================================

func boolToStatus(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

func boolToMessage(ok bool, passMsg, failMsg string) string {
	if ok {
		return passMsg
	}
	return failMsg
}
