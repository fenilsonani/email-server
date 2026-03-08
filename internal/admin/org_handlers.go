package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// handleAPIOrgs handles listing and creating organizations.
func (s *Server) handleAPIOrgs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIListOrgs(w, r)
	case http.MethodPost:
		s.handleAPICreateOrg(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIListOrgs(w http.ResponseWriter, r *http.Request) {
	if s.orgStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Organization store not initialized")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	orgs, err := s.orgStore.ListByUser(r.Context(), userID)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to list organizations")
		return
	}

	s.jsonResponse(w, http.StatusOK, orgs)
}

func (s *Server) handleAPICreateOrg(w http.ResponseWriter, r *http.Request) {
	if s.orgStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Organization store not initialized")
		return
	}

	userID, ok := s.getSessionUserID(r)
	if !ok {
		s.jsonError(w, http.StatusUnauthorized, "Authentication required")
		return
	}

	var req struct {
		Name   string `json:"name"`
		Preset string `json:"preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		s.jsonError(w, http.StatusBadRequest, "Organization name is required")
		return
	}

	org, err := s.orgStore.Create(r.Context(), req.Name, userID, req.Preset)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.jsonResponse(w, http.StatusCreated, org)
}

// handleAPIOrgByID handles operations on a specific organization.
func (s *Server) handleAPIOrgByID(w http.ResponseWriter, r *http.Request) {
	if s.orgStore == nil {
		s.jsonError(w, http.StatusServiceUnavailable, "Organization store not initialized")
		return
	}

	// Parse org ID and sub-resource from path: /admin/api/v1/orgs/{id}[/members[/{memberID}]]
	path := strings.TrimPrefix(r.URL.Path, "/admin/api/v1/orgs/")
	parts := strings.SplitN(path, "/", 3)

	orgID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid organization ID")
		return
	}

	// Check for sub-resource
	if len(parts) > 1 && parts[1] == "members" {
		if len(parts) > 2 {
			s.handleAPIOrgMemberByID(w, r, orgID, parts[2])
		} else {
			s.handleAPIOrgMembers(w, r, orgID)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		org, err := s.orgStore.Get(r.Context(), orgID)
		if err != nil {
			s.jsonError(w, http.StatusNotFound, "Organization not found")
			return
		}
		s.jsonResponse(w, http.StatusOK, org)

	case http.MethodPut:
		var req struct {
			Name   string `json:"name"`
			Preset string `json:"preset"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		if err := s.orgStore.Update(r.Context(), orgID, req.Name, req.Preset, nil); err != nil {
			s.jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{"updated": true})

	case http.MethodDelete:
		if err := s.orgStore.Delete(r.Context(), orgID); err != nil {
			s.jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{"deleted": true})

	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIOrgMembers(w http.ResponseWriter, r *http.Request, orgID int64) {
	switch r.Method {
	case http.MethodGet:
		members, err := s.orgStore.ListMembers(r.Context(), orgID)
		if err != nil {
			s.jsonError(w, http.StatusInternalServerError, "Failed to list members")
			return
		}
		s.jsonResponse(w, http.StatusOK, members)

	case http.MethodPost:
		var req struct {
			UserID int64  `json:"user_id"`
			Role   string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		if err := s.orgStore.AddMember(r.Context(), orgID, req.UserID, req.Role); err != nil {
			s.jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.jsonResponse(w, http.StatusCreated, map[string]interface{}{"added": true})

	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIOrgMemberByID(w http.ResponseWriter, r *http.Request, orgID int64, memberIDStr string) {
	memberID, err := strconv.ParseInt(memberIDStr, 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid member ID")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.jsonError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		if err := s.orgStore.UpdateMemberRole(r.Context(), orgID, memberID, req.Role); err != nil {
			s.jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{"updated": true})

	case http.MethodDelete:
		if err := s.orgStore.RemoveMember(r.Context(), orgID, memberID); err != nil {
			s.jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.jsonResponse(w, http.StatusOK, map[string]interface{}{"removed": true})

	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
