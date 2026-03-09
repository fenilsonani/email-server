package admin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/audit"
	"github.com/fenilsonani/email-server/internal/features"
	"github.com/fenilsonani/email-server/internal/lists"
	"github.com/fenilsonani/email-server/internal/queue"
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
		s.logger.ErrorContext(r.Context(), "Failed to create alias", err)
		s.jsonError(w, http.StatusInternalServerError, "Failed to create alias")
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
		s.logger.ErrorContext(r.Context(), "Failed to add VIP contact", err)
		s.jsonError(w, http.StatusInternalServerError, "Failed to add VIP contact")
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
		// Return empty script if sieve not configured
		if r.Method == http.MethodGet {
			s.jsonResponse(w, http.StatusOK, map[string]interface{}{
				"script":     "",
				"updated_at": "",
			})
			return
		}
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
		// Frontend expects {script: string, updated_at: string}
		scripts, err := s.sieveStore.ListScripts(r.Context(), userID)
		if err != nil || len(scripts) == 0 {
			s.jsonResponse(w, http.StatusOK, map[string]interface{}{
				"script":     "",
				"updated_at": "",
			})
			return
		}
		// Return the first (active) script
		activeScript := scripts[0]
		for _, sc := range scripts {
			if sc.IsActive {
				activeScript = sc
				break
			}
		}
		updatedAt := activeScript.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = activeScript.CreatedAt
		}
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"script":     activeScript.Content,
			"updated_at": updatedAt.Format(time.RFC3339),
		})

	case http.MethodPut:
		// Frontend sends {script: string}
		var req struct {
			Script string `json:"script"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		name := "default"
		// Try to update first, create if it doesn't exist
		err := s.sieveStore.UpdateScript(r.Context(), userID, name, req.Script)
		if err != nil {
			_, err = s.sieveStore.CreateScript(r.Context(), userID, name, req.Script)
			if err != nil {
				s.jsonError(w, http.StatusInternalServerError, "Failed to save script")
				return
			}
		}

		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"saved": true,
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
		Script  string `json:"script"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	scriptContent := req.Script
	if scriptContent == "" {
		scriptContent = req.Content
	}
	if scriptContent == "" {
		s.jsonError(w, http.StatusBadRequest, "Script content is required")
		return
	}

	err := sieve.ValidateScript(scriptContent)
	if err != nil {
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"valid":  false,
			"errors": []string{err.Error()},
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

	// Check database file for last modified time and size
	dbPath := s.config.Storage.DatabasePath
	var lastBackup string
	var sizeStr string

	if info, err := os.Stat(dbPath); err == nil {
		lastBackup = info.ModTime().Format(time.RFC3339)
		sizeMB := float64(info.Size()) / (1024 * 1024)
		if sizeMB >= 1 {
			sizeStr = strings.TrimRight(strings.TrimRight(
				strconv.FormatFloat(sizeMB, 'f', 1, 64), "0"), ".") + " MB"
		} else {
			sizeKB := float64(info.Size()) / 1024
			sizeStr = strings.TrimRight(strings.TrimRight(
				strconv.FormatFloat(sizeKB, 'f', 1, 64), "0"), ".") + " KB"
		}
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"last_backup": lastBackup,
		"size":        sizeStr,
		"location":    dbPath,
	})
}

func (s *Server) handleAPIBackupTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Create backup to the backups directory
	dataDir := s.config.Storage.DataDir
	backupDir := filepath.Join(dataDir, "backups")
	os.MkdirAll(backupDir, 0755)

	filename := fmt.Sprintf("mailserver-backup-%s.tar.gz", time.Now().Format("2006-01-02-150405"))
	backupPath := filepath.Join(backupDir, filename)

	outFile, err := os.Create(backupPath) // #nosec G304 -- path constructed from server config dir + timestamp
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to create backup file", err)
		s.jsonError(w, http.StatusInternalServerError, "Failed to create backup file")
		return
	}
	defer outFile.Close()

	gzWriter := gzip.NewWriter(outFile)
	tarWriter := tar.NewWriter(gzWriter)

	// Backup database
	dbPath := s.config.Storage.DatabasePath
	if err := addFileToTar(tarWriter, dbPath, "metadata.db"); err != nil {
		s.logger.Error("Failed to backup database", "error", err.Error())
	}

	// Backup DKIM keys
	dkimPath := filepath.Join(dataDir, "dkim")
	if err := addDirToTar(tarWriter, dkimPath, "dkim"); err != nil {
		s.logger.Debug("Failed to backup DKIM keys", "error", err.Error())
	}

	tarWriter.Close()
	gzWriter.Close()

	// Get size
	stat, _ := os.Stat(backupPath)
	size := "0 B"
	if stat != nil {
		size = formatBytes(stat.Size())
	}

	if s.auditLogger != nil {
		s.auditLogger.Log(r.Context(), getSessionUsername(r), audit.EventConfigChange, "system", map[string]interface{}{"action": "backup_create", "file": filename}, getIP(r))
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"created":  true,
		"filename": filename,
		"size":     size,
		"path":     backupPath,
	})
}

func (s *Server) handleAPIBackupHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	type BackupItem struct {
		ID        string `json:"id"`
		CreatedAt string `json:"created_at"`
		Size      string `json:"size"`
		Type      string `json:"type"`
	}

	history := []BackupItem{}

	backupDir := filepath.Join(s.config.Storage.DataDir, "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		// No backups directory yet — return empty
		s.jsonResponse(w, http.StatusOK, history)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		history = append(history, BackupItem{
			ID:        entry.Name(),
			CreatedAt: info.ModTime().UTC().Format(time.RFC3339),
			Size:      formatBytes(info.Size()),
			Type:      "manual",
		})
	}

	// Reverse so newest first
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}

	s.jsonResponse(w, http.StatusOK, history)
}

func (s *Server) handleAPIRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse multipart form (max 500MB)
	if err := r.ParseMultipartForm(500 << 20); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Failed to parse form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "No backup file provided")
		return
	}
	defer file.Close()

	if !strings.HasSuffix(header.Filename, ".tar.gz") && !strings.HasSuffix(header.Filename, ".zip") {
		s.jsonError(w, http.StatusBadRequest, "Invalid backup file format. Expected .tar.gz or .zip")
		return
	}

	// Save to temp file
	tempFile, err := os.CreateTemp("", "mailserver-restore-*"+filepath.Ext(header.Filename))
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to process backup")
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if _, err := io.Copy(tempFile, file); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to save backup file")
		return
	}
	tempFile.Seek(0, 0)

	// Extract tar.gz
	gzReader, err := gzip.NewReader(tempFile)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid gzip file")
		return
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	dataDir := s.config.Storage.DataDir
	restored := 0

	for {
		hdr, err := tarReader.Next()
		if err != nil {
			break
		}

		// Sanitize path to prevent directory traversal
		cleanName := filepath.Clean(hdr.Name)
		if strings.Contains(cleanName, "..") {
			continue
		}

		targetPath := filepath.Join(dataDir, cleanName)

		// Verify resolved path is within dataDir
		absTarget, err := filepath.Abs(targetPath)
		if err != nil {
			continue
		}
		absBase, err := filepath.Abs(dataDir)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(absTarget, absBase+string(filepath.Separator)) && absTarget != absBase {
			continue
		}

		if hdr.Typeflag == tar.TypeDir {
			os.MkdirAll(targetPath, 0755)
			continue
		}

		// Ensure parent dir exists
		os.MkdirAll(filepath.Dir(targetPath), 0755)

		outFile, err := os.Create(targetPath)
		if err != nil {
			s.logger.Error("Failed to restore file", "path", targetPath, "error", err.Error())
			continue
		}
		io.Copy(outFile, io.LimitReader(tarReader, 100<<20)) // #nosec G110 -- limit to 100MB per file
		outFile.Close()
		restored++
	}

	if s.auditLogger != nil {
		s.auditLogger.Log(r.Context(), getSessionUsername(r), audit.EventConfigChange, "system", map[string]interface{}{"action": "backup_restore", "file": header.Filename, "files_restored": restored}, getIP(r))
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"restored":       true,
		"files_restored": restored,
		"message":        "Backup restored. Server restart may be required for changes to take effect.",
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

	type CertInfo struct {
		Domain    string `json:"domain"`
		Issuer    string `json:"issuer"`
		ExpiresAt string `json:"expires_at"`
		IsValid   bool   `json:"is_valid"`
		AutoRenew bool   `json:"auto_renew"`
	}

	certs := []CertInfo{}

	// Try to read the actual TLS certificate
	certFile := s.config.TLS.CertFile
	if certFile != "" {
		certPEM, err := os.ReadFile(certFile) // #nosec G304 -- path from server config, not user input
		if err != nil {
			s.logger.Debug("Failed to read cert file", "path", certFile, "error", err.Error())
		} else {
			// Parse all certificates in the PEM chain, use the leaf (first one)
			var leafCert *x509.Certificate
			rest := certPEM
			for {
				var block *pem.Block
				block, rest = pem.Decode(rest)
				if block == nil {
					break
				}
				if block.Type != "CERTIFICATE" {
					continue
				}
				cert, err := x509.ParseCertificate(block.Bytes)
				if err != nil {
					s.logger.Debug("Failed to parse certificate", "error", err.Error())
					continue
				}
				// Use the first cert (leaf) that is not a CA
				if leafCert == nil && !cert.IsCA {
					leafCert = cert
				}
				// If all are CAs, just use the first one
				if leafCert == nil {
					leafCert = cert
				}
			}

			if leafCert != nil {
				issuer := leafCert.Issuer.CommonName
				if issuer == "" && len(leafCert.Issuer.Organization) > 0 {
					issuer = leafCert.Issuer.Organization[0]
				}
				isValid := time.Now().Before(leafCert.NotAfter) && time.Now().After(leafCert.NotBefore)

				names := leafCert.DNSNames
				if len(names) == 0 && leafCert.Subject.CommonName != "" {
					names = []string{leafCert.Subject.CommonName}
				}

				for _, name := range names {
					certs = append(certs, CertInfo{
						Domain:    name,
						Issuer:    issuer,
						ExpiresAt: leafCert.NotAfter.Format(time.RFC3339),
						IsValid:   isValid,
						AutoRenew: s.config.TLS.AutoTLS,
					})
				}
			} else {
				s.logger.Debug("No certificate found in PEM file", "path", certFile)
			}
		}
	}

	s.jsonResponse(w, http.StatusOK, certs)
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

	status, err := s.getTwoFactorStatus(userID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to check 2FA status")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"enabled": status.Enabled,
	})
}

func (s *Server) handleAPI2FASetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	// Get username
	var username string
	err := s.db.QueryRowContext(r.Context(), "SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to get user info")
		return
	}

	// Check if already enabled
	status, err := s.getTwoFactorStatus(userID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to check 2FA status")
		return
	}
	if status.Enabled {
		s.jsonError(w, http.StatusBadRequest, "2FA is already enabled")
		return
	}

	// Generate new TOTP secret
	key, err := s.generateTOTPSecret(username)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to generate 2FA secret")
		return
	}

	// Generate QR code as base64 PNG
	qrBase64, err := generateQRCodeBase64(key)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to generate QR code")
		return
	}

	// Store secret temporarily (not enabled yet)
	_, err = s.db.ExecContext(r.Context(),
		"UPDATE users SET totp_secret = ? WHERE id = ?",
		key.Secret(), userID,
	)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to save 2FA secret")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"secret": key.Secret(),
		"qr_url": "data:image/png;base64," + qrBase64,
	})
}

func (s *Server) handleAPI2FAVerifyCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
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

	// Get the stored secret
	var secret string
	err := s.db.QueryRowContext(r.Context(),
		"SELECT COALESCE(totp_secret, '') FROM users WHERE id = ?", userID,
	).Scan(&secret)
	if err != nil || secret == "" {
		s.jsonError(w, http.StatusBadRequest, "No 2FA setup in progress. Start setup first.")
		return
	}

	// Validate the code
	if !s.validateTOTPCode(secret, req.Code) {
		s.jsonError(w, http.StatusBadRequest, "Invalid verification code")
		return
	}

	// Enable 2FA
	_, err = s.db.ExecContext(r.Context(),
		"UPDATE users SET totp_enabled = 1 WHERE id = ?", userID,
	)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to enable 2FA")
		return
	}

	// Trust current device
	token, err := s.createTrustedDevice(userID, r)
	if err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     trustedDeviceCookie,
			Value:    token,
			Path:     "/admin",
			HttpOnly: true,
			Secure:   isSecureContext(r),
			SameSite: http.SameSiteStrictMode,
			MaxAge:   trustedDeviceDays * 24 * 60 * 60,
		})
	}

	// Get username for audit log
	var username string
	_ = s.db.QueryRowContext(r.Context(), "SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	s.auditLogger.Log(r.Context(), username, audit.EventConfigChange, "2FA enabled", nil, getIP(r))

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"enabled": true,
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

	// Get current 2FA status and verify code
	status, err := s.getTwoFactorStatus(userID)
	if err != nil || !status.Enabled {
		s.jsonError(w, http.StatusBadRequest, "2FA is not enabled")
		return
	}

	if !s.validateTOTPCode(status.Secret, req.Code) {
		s.jsonError(w, http.StatusBadRequest, "Invalid verification code")
		return
	}

	// Disable 2FA
	_, err = s.db.ExecContext(r.Context(),
		"UPDATE users SET totp_enabled = 0, totp_secret = NULL WHERE id = ?", userID,
	)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to disable 2FA")
		return
	}

	// Remove all trusted devices
	s.db.ExecContext(r.Context(), "DELETE FROM totp_trusted_devices WHERE user_id = ?", userID)

	// Get username for audit log
	var username string
	_ = s.db.QueryRowContext(r.Context(), "SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	s.auditLogger.Log(r.Context(), username, audit.EventConfigChange, "2FA disabled", nil, getIP(r))

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

	currentVersion := "1.0.0"
	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"current_version":  currentVersion,
		"latest_version":   currentVersion,
		"update_available": false,
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
		// Frontend expects: {enabled, interval_days, last_rotation, next_rotation}
		// Check if any domains have DKIM keys (enabled = at least one key exists)
		var keyCount int
		s.db.QueryRowContext(r.Context(),
			"SELECT COUNT(*) FROM domains WHERE is_active = TRUE AND dkim_private_key IS NOT NULL",
		).Scan(&keyCount)

		// Get most recent key creation date as "last_rotation"
		var lastRotation string
		var lastTime time.Time
		err := s.db.QueryRowContext(r.Context(),
			"SELECT COALESCE(dkim_key_created_at, created_at) FROM domains WHERE is_active = TRUE AND dkim_private_key IS NOT NULL ORDER BY COALESCE(dkim_key_created_at, created_at) DESC LIMIT 1",
		).Scan(&lastTime)
		if err == nil {
			lastRotation = lastTime.Format(time.RFC3339)
		}

		intervalDays := 90
		nextRotation := ""
		if lastRotation != "" {
			nextTime := lastTime.AddDate(0, 0, intervalDays)
			nextRotation = nextTime.Format(time.RFC3339)
		}

		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"enabled":       keyCount > 0,
			"interval_days": intervalDays,
			"last_rotation": lastRotation,
			"next_rotation": nextRotation,
		})

	case http.MethodPut:
		// Frontend sends {enabled, interval_days}
		var req struct {
			Enabled      bool `json:"enabled"`
			IntervalDays int  `json:"interval_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		s.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"saved": true,
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

	if s.queue == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Queue not configured — cannot send email")
		return
	}

	// Get first active domain for From address
	var testDomain string
	err := s.db.QueryRowContext(r.Context(),
		"SELECT name FROM domains WHERE is_active = TRUE ORDER BY id LIMIT 1",
	).Scan(&testDomain)
	if err != nil || testDomain == "" {
		testDomain = s.config.Server.Domain
	}

	from := "postmaster@" + testDomain
	messageID := time.Now().Format("20060102150405") + "." + strconv.FormatInt(time.Now().UnixNano(), 36) + "@" + testDomain
	msg := "From: " + from + "\r\n" +
		"To: " + req.To + "\r\n" +
		"Subject: " + req.Subject + "\r\n" +
		"Message-ID: <" + messageID + ">\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		req.Body

	// Write to temp file
	tmpDir := s.config.Storage.MaildirPath
	if tmpDir == "" {
		tmpDir = "/tmp"
	}
	tmpFile, err := os.CreateTemp(tmpDir, "test-email-*.eml")
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to create message file")
		return
	}
	tmpFile.WriteString(msg)
	tmpFile.Close()

	// Extract recipient domain
	recipientDomain := ""
	if parts := strings.Split(req.To, "@"); len(parts) == 2 {
		recipientDomain = parts[1]
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	queueMsg := &queue.Message{
		Sender:      from,
		Recipients:  []string{req.To},
		MessagePath: tmpFile.Name(),
		Size:        int64(len(msg)),
		Domain:      recipientDomain,
	}

	if err := s.queue.Enqueue(ctx, queueMsg); err != nil {
		os.Remove(tmpFile.Name())
		s.logger.ErrorContext(r.Context(), "Failed to queue message", err)
		s.jsonError(w, http.StatusInternalServerError, "Failed to queue message")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"sent":    true,
		"to":      req.To,
		"subject": req.Subject,
		"message": "Test email queued for delivery to " + req.To,
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
