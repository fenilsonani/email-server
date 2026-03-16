package admin

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/fenilsonani/email-server/internal/lists"
)

// Domain represents a mail domain (used for list creation dropdown)
type listDomain struct {
	ID   int64
	Name string
}

// =============================================================================
// Mailing Lists Management
// =============================================================================

// handleLists shows all mailing lists
func (s *Server) handleLists(w http.ResponseWriter, r *http.Request) {
	if s.listsStore == nil {
		http.Error(w, "Mailing lists not enabled", http.StatusServiceUnavailable)
		return
	}

	allLists, err := s.listsStore.ListAllLists(r.Context())
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list mailing lists", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get stats for each list
	type listWithStats struct {
		*lists.MailingList
		Stats *lists.ListStats
	}
	var listsWithStats []listWithStats
	for _, l := range allLists {
		stats, _ := s.listsStore.GetListStats(r.Context(), l.ID)
		listsWithStats = append(listsWithStats, listWithStats{l, stats})
	}

	// Get pending moderation count across all lists
	pending, _ := s.listsStore.ListAllPendingModeration(r.Context())

	data := map[string]interface{}{
		"Title":             "Mailing Lists",
		"Lists":             listsWithStats,
		"PendingModeration": len(pending),
	}

	s.renderTemplate(w, "lists.html", data)
}

// handleListAdd handles creating a new mailing list
func (s *Server) handleListAdd(w http.ResponseWriter, r *http.Request) {
	if s.listsStore == nil {
		http.Error(w, "Mailing lists not enabled", http.StatusServiceUnavailable)
		return
	}

	if r.Method == http.MethodGet {
		// Get domains for dropdown
		rows, err := s.db.QueryContext(r.Context(), "SELECT id, name FROM domains WHERE is_active = TRUE ORDER BY name")
		if err != nil {
			s.logger.ErrorContext(r.Context(), "Failed to list domains", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var domains []listDomain
		for rows.Next() {
			var d listDomain
			if err := rows.Scan(&d.ID, &d.Name); err != nil {
				continue
			}
			domains = append(domains, d)
		}

		data := map[string]interface{}{
			"Title":   "Create Mailing List",
			"Domains": domains,
			"IsNew":   true,
		}
		s.renderTemplate(w, "list_form.html", data)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Invalid form data", formErrorStatus(err))
		return
	}

	domainID, _ := strconv.ParseInt(r.PostForm.Get("domain_id"), 10, 64)
	localPart := strings.ToLower(strings.TrimSpace(r.PostForm.Get("local_part")))
	name := strings.TrimSpace(r.PostForm.Get("name"))
	description := strings.TrimSpace(r.PostForm.Get("description"))
	listType := lists.ListType(r.PostForm.Get("list_type"))
	postingPolicy := lists.PostingPolicy(r.PostForm.Get("posting_policy"))

	// Get domain name
	var domainName string
	err := s.db.QueryRowContext(r.Context(), "SELECT name FROM domains WHERE id = ?", domainID).Scan(&domainName)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Invalid domain", http.StatusBadRequest)
		} else {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	list := &lists.MailingList{
		DomainID:          domainID,
		LocalPart:         localPart,
		ListAddress:       localPart + "@" + domainName,
		Name:              name,
		Description:       description,
		ListType:          listType,
		PostingPolicy:     postingPolicy,
		ModerationEnabled: r.PostForm.Get("moderation_enabled") == "on",
		SubjectPrefix:     r.PostForm.Get("subject_prefix"),
		ReplyToList:       r.PostForm.Get("reply_to_list") == "on",
		ArchiveEnabled:    r.PostForm.Get("archive_enabled") != "off",
		AllowSubscribe:    r.PostForm.Get("allow_subscribe") != "off",
		RequireConfirm:    r.PostForm.Get("require_confirm") != "off",
		MaxMessageSize:    10 * 1024 * 1024, // 10MB default
		MaxMembers:        10000,
		IsActive:          true,
	}

	if err := s.listsStore.CreateList(r.Context(), list); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to create mailing list", err)
		if err == lists.ErrAlreadyExists {
			http.Error(w, "A list with this address already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create list", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/lists", http.StatusSeeOther)
}

// handleListEdit handles editing a mailing list
func (s *Server) handleListEdit(w http.ResponseWriter, r *http.Request) {
	if s.listsStore == nil {
		http.Error(w, "Mailing lists not enabled", http.StatusServiceUnavailable)
		return
	}

	// Extract list ID from path
	idStr := strings.TrimPrefix(r.URL.Path, "/admin/lists/edit/")
	listID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid list ID", http.StatusBadRequest)
		return
	}

	list, err := s.listsStore.GetList(r.Context(), listID)
	if err != nil {
		if err == lists.ErrNotFound {
			http.Error(w, "List not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodGet {
		data := map[string]interface{}{
			"Title": "Edit Mailing List",
			"List":  list,
			"IsNew": false,
		}
		s.renderTemplate(w, "list_form.html", data)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form
	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Invalid form data", formErrorStatus(err))
		return
	}

	// Update list fields
	list.Name = strings.TrimSpace(r.PostForm.Get("name"))
	list.Description = strings.TrimSpace(r.PostForm.Get("description"))
	list.ListType = lists.ListType(r.PostForm.Get("list_type"))
	list.PostingPolicy = lists.PostingPolicy(r.PostForm.Get("posting_policy"))
	list.ModerationEnabled = r.PostForm.Get("moderation_enabled") == "on"
	list.SubjectPrefix = r.PostForm.Get("subject_prefix")
	list.ReplyToList = r.PostForm.Get("reply_to_list") == "on"
	list.ArchiveEnabled = r.PostForm.Get("archive_enabled") == "on"
	list.AllowSubscribe = r.PostForm.Get("allow_subscribe") == "on"
	list.RequireConfirm = r.PostForm.Get("require_confirm") == "on"
	list.IsActive = r.PostForm.Get("is_active") == "on"

	if err := s.listsStore.UpdateList(r.Context(), list); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to update mailing list", err)
		http.Error(w, "Failed to update list", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/lists", http.StatusSeeOther)
}

// handleListDelete handles deleting a mailing list
func (s *Server) handleListDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.listsStore == nil {
		http.Error(w, "Mailing lists not enabled", http.StatusServiceUnavailable)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/admin/lists/delete/")
	listID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid list ID", http.StatusBadRequest)
		return
	}

	if err := s.listsStore.DeleteList(r.Context(), listID); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to delete mailing list", err)
		http.Error(w, "Failed to delete list", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/lists", http.StatusSeeOther)
}

// =============================================================================
// List Members Management
// =============================================================================

// handleListMembers shows members of a mailing list
func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	if s.listsStore == nil {
		http.Error(w, "Mailing lists not enabled", http.StatusServiceUnavailable)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/admin/lists/members/")
	listID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid list ID", http.StatusBadRequest)
		return
	}

	list, err := s.listsStore.GetList(r.Context(), listID)
	if err != nil {
		if err == lists.ErrNotFound {
			http.Error(w, "List not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	members, err := s.listsStore.ListMembers(r.Context(), listID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list members", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	stats, _ := s.listsStore.GetListStats(r.Context(), listID)

	data := map[string]interface{}{
		"Title":   "List Members - " + list.Name,
		"List":    list,
		"Members": members,
		"Stats":   stats,
	}

	s.renderTemplate(w, "list_members.html", data)
}

// handleListMemberAdd handles adding a member to a list
func (s *Server) handleListMemberAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.listsStore == nil {
		http.Error(w, "Mailing lists not enabled", http.StatusServiceUnavailable)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/admin/lists/members/add/")
	listID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid list ID", http.StatusBadRequest)
		return
	}

	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Invalid form data", formErrorStatus(err))
		return
	}

	email := strings.ToLower(strings.TrimSpace(r.PostForm.Get("email")))
	name := strings.TrimSpace(r.PostForm.Get("name"))
	role := lists.MemberRole(r.PostForm.Get("role"))
	if role == "" {
		role = lists.RoleMember
	}

	member := &lists.ListMember{
		ListID:       listID,
		Email:        email,
		Name:         name,
		Role:         role,
		DeliveryMode: lists.DeliveryNormal,
		IsConfirmed:  true, // Admin-added members are auto-confirmed
	}

	if err := s.listsStore.AddMember(r.Context(), member); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to add member", err)
		if err == lists.ErrAlreadyExists {
			http.Error(w, "Member already exists", http.StatusConflict)
			return
		}
		if err == lists.ErrListFull {
			http.Error(w, "List has reached maximum members", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to add member", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/lists/members/"+idStr, http.StatusSeeOther)
}

// handleListMemberRemove handles removing a member from a list
func (s *Server) handleListMemberRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.listsStore == nil {
		http.Error(w, "Mailing lists not enabled", http.StatusServiceUnavailable)
		return
	}

	// Path format: /admin/lists/members/remove/{listID}/{email}
	path := strings.TrimPrefix(r.URL.Path, "/admin/lists/members/remove/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	listID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "Invalid list ID", http.StatusBadRequest)
		return
	}

	email := parts[1]

	if err := s.listsStore.RemoveMember(r.Context(), listID, email); err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to remove member", err)
		http.Error(w, "Failed to remove member", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/lists/members/"+parts[0], http.StatusSeeOther)
}

// =============================================================================
// Moderation Queue
// =============================================================================

// handleListModeration shows the moderation queue for a list
func (s *Server) handleListModeration(w http.ResponseWriter, r *http.Request) {
	if s.listsStore == nil {
		http.Error(w, "Mailing lists not enabled", http.StatusServiceUnavailable)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/admin/lists/moderation/")

	var pending []*lists.ModeratedMessage
	var list *lists.MailingList
	var err error

	if idStr == "" || idStr == "all" {
		// Show all pending messages across all lists
		pending, err = s.listsStore.ListAllPendingModeration(r.Context())
	} else {
		listID, parseErr := strconv.ParseInt(idStr, 10, 64)
		if parseErr != nil {
			http.Error(w, "Invalid list ID", http.StatusBadRequest)
			return
		}
		list, err = s.listsStore.GetList(r.Context(), listID)
		if err == nil {
			pending, err = s.listsStore.ListPendingModeration(r.Context(), listID)
		}
	}

	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list pending moderation", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get list info for each message
	type messageWithList struct {
		*lists.ModeratedMessage
		ListName    string
		ListAddress string
	}
	var messagesWithList []messageWithList
	for _, msg := range pending {
		msgList, _ := s.listsStore.GetList(r.Context(), msg.ListID)
		listName := ""
		listAddr := ""
		if msgList != nil {
			listName = msgList.Name
			listAddr = msgList.ListAddress
		}
		messagesWithList = append(messagesWithList, messageWithList{msg, listName, listAddr})
	}

	data := map[string]interface{}{
		"Title":    "Moderation Queue",
		"List":     list,
		"Messages": messagesWithList,
	}

	s.renderTemplate(w, "list_moderation.html", data)
}

// handleListModerationAction handles approve/reject actions
func (s *Server) handleListModerationAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.listsStore == nil {
		http.Error(w, "Mailing lists not enabled", http.StatusServiceUnavailable)
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	path := r.URL.Path
	var action string
	var idStr string

	if strings.Contains(path, "/approve/") {
		action = "approve"
		idStr = strings.TrimPrefix(path, "/admin/lists/moderation/approve/")
	} else if strings.Contains(path, "/reject/") {
		action = "reject"
		idStr = strings.TrimPrefix(path, "/admin/lists/moderation/reject/")
	}

	msgID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid message ID", http.StatusBadRequest)
		return
	}

	switch action {
	case "approve":
		err = s.listsStore.ApproveMessage(r.Context(), msgID, userID)
	case "reject":
		reason := r.PostForm.Get("reason")
		err = s.listsStore.RejectMessage(r.Context(), msgID, userID, reason)
	}

	if err != nil {
		s.logger.ErrorContext(r.Context(), "Moderation action failed", err, "action", action)
		http.Error(w, "Action failed", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/lists/moderation/", http.StatusSeeOther)
}

// =============================================================================
// Archives
// =============================================================================

// handleListArchives shows the archive for a list
func (s *Server) handleListArchives(w http.ResponseWriter, r *http.Request) {
	if s.listsStore == nil {
		http.Error(w, "Mailing lists not enabled", http.StatusServiceUnavailable)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/admin/lists/archives/")
	listID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid list ID", http.StatusBadRequest)
		return
	}

	list, err := s.listsStore.GetList(r.Context(), listID)
	if err != nil {
		if err == lists.ErrNotFound {
			http.Error(w, "List not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Pagination
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit := 50
	offset := (page - 1) * limit

	// Search
	query := r.URL.Query().Get("q")

	var archives []*lists.ArchivedMessage
	if query != "" {
		archives, err = s.listsStore.SearchArchives(r.Context(), listID, query, limit)
	} else {
		archives, err = s.listsStore.ListArchives(r.Context(), listID, limit, offset)
	}

	if err != nil {
		s.logger.ErrorContext(r.Context(), "Failed to list archives", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	totalCount, _ := s.listsStore.CountArchives(r.Context(), listID)
	totalPages := (totalCount + limit - 1) / limit

	data := map[string]interface{}{
		"Title":      "Archives - " + list.Name,
		"List":       list,
		"Archives":   archives,
		"Query":      query,
		"Page":       page,
		"TotalPages": totalPages,
		"TotalCount": totalCount,
	}

	s.renderTemplate(w, "list_archives.html", data)
}
