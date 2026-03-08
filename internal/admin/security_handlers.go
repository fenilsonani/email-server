package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// --- Security: Suppression List ---

func (s *Server) handleAPISecuritySuppression(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAPIGetSuppression(w, r)
	case http.MethodPost:
		s.handleAPIAddSuppression(w, r)
	default:
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (s *Server) handleAPIGetSuppression(w http.ResponseWriter, r *http.Request) {
	pagination := getPaginationParams(r)

	var totalCount int
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM suppression_list").Scan(&totalCount)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT sl.id, sl.email, sl.reason, d.name, sl.created_at
		FROM suppression_list sl
		JOIN domains d ON sl.domain_id = d.id
		ORDER BY sl.created_at DESC
		LIMIT ? OFFSET ?
	`, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query suppression list")
		return
	}
	defer rows.Close()

	type SuppressionEntry struct {
		ID        int64     `json:"id"`
		Email     string    `json:"email"`
		Reason    string    `json:"reason"`
		Domain    string    `json:"domain"`
		CreatedAt time.Time `json:"created_at"`
	}

	entries := []SuppressionEntry{}
	for rows.Next() {
		var e SuppressionEntry
		if err := rows.Scan(&e.ID, &e.Email, &e.Reason, &e.Domain, &e.CreatedAt); err == nil {
			entries = append(entries, e)
		}
	}

	totalPages := (totalCount + pagination.PageSize - 1) / pagination.PageSize
	if totalPages == 0 {
		totalPages = 1
	}

	s.jsonResponseWithMeta(w, http.StatusOK, entries, &APIMeta{
		Page: pagination.Page, PageSize: pagination.PageSize,
		TotalCount: totalCount, TotalPages: totalPages,
	})
}

func (s *Server) handleAPIAddSuppression(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email  string `json:"email"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Email == "" || req.Reason == "" {
		s.jsonError(w, http.StatusBadRequest, "Email and reason are required")
		return
	}

	// Validate reason
	validReasons := map[string]bool{"hard_bounce": true, "unsubscribe": true, "complaint": true, "manual": true}
	if !validReasons[req.Reason] {
		s.jsonError(w, http.StatusBadRequest, "Invalid reason. Must be: hard_bounce, unsubscribe, complaint, or manual")
		return
	}

	// Extract domain from email
	parts := strings.SplitN(req.Email, "@", 2)
	if len(parts) != 2 {
		s.jsonError(w, http.StatusBadRequest, "Invalid email address")
		return
	}

	var domainID int64
	err := s.db.QueryRowContext(r.Context(), "SELECT id FROM domains WHERE name = ?", parts[1]).Scan(&domainID)
	if err != nil {
		// Use first domain as fallback
		err = s.db.QueryRowContext(r.Context(), "SELECT id FROM domains ORDER BY id LIMIT 1").Scan(&domainID)
		if err != nil {
			s.jsonError(w, http.StatusBadRequest, "No domains configured")
			return
		}
	}

	result, err := s.db.ExecContext(r.Context(),
		`INSERT INTO suppression_list (domain_id, email, reason) VALUES (?, ?, ?)`,
		domainID, req.Email, req.Reason,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			s.jsonError(w, http.StatusConflict, "Email already in suppression list")
			return
		}
		s.jsonError(w, http.StatusInternalServerError, "Failed to add to suppression list")
		return
	}

	id, _ := result.LastInsertId()
	s.jsonResponse(w, http.StatusCreated, map[string]interface{}{
		"id":    id,
		"email": req.Email,
	})
}

func (s *Server) handleAPISecuritySuppressionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract ID from path: /admin/api/v1/security/suppression/{id}
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	idStr := parts[len(parts)-1]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	result, err := s.db.ExecContext(r.Context(), "DELETE FROM suppression_list WHERE id = ?", id)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to delete entry")
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		s.jsonError(w, http.StatusNotFound, "Entry not found")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"deleted": true})
}

// --- Security: Greylist ---

func (s *Server) handleAPISecurityGreylist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	pagination := getPaginationParams(r)

	var totalCount int
	s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM greylist").Scan(&totalCount)

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, sender_ip, sender, recipient, first_seen, passed, pass_count, last_seen
		FROM greylist
		ORDER BY last_seen DESC
		LIMIT ? OFFSET ?
	`, pagination.PageSize, pagination.Offset)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query greylist")
		return
	}
	defer rows.Close()

	type GreylistEntry struct {
		ID        int64     `json:"id"`
		SenderIP  string    `json:"sender_ip"`
		Sender    string    `json:"sender"`
		Recipient string    `json:"recipient"`
		FirstSeen time.Time `json:"first_seen"`
		Passed    bool      `json:"passed"`
		PassCount int       `json:"pass_count"`
		LastSeen  time.Time `json:"last_seen"`
	}

	entries := []GreylistEntry{}
	for rows.Next() {
		var e GreylistEntry
		if err := rows.Scan(&e.ID, &e.SenderIP, &e.Sender, &e.Recipient, &e.FirstSeen, &e.Passed, &e.PassCount, &e.LastSeen); err == nil {
			entries = append(entries, e)
		}
	}

	totalPages := (totalCount + pagination.PageSize - 1) / pagination.PageSize
	if totalPages == 0 {
		totalPages = 1
	}

	s.jsonResponseWithMeta(w, http.StatusOK, entries, &APIMeta{
		Page: pagination.Page, PageSize: pagination.PageSize,
		TotalCount: totalCount, TotalPages: totalPages,
	})
}

func (s *Server) handleAPISecurityGreylistWhitelist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract ID from path: /admin/api/v1/security/greylist/{id}/whitelist
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/"), "/")
	// Find "greylist" in parts, ID is next
	greylistIdx := -1
	for i, p := range parts {
		if p == "greylist" {
			greylistIdx = i
			break
		}
	}
	if greylistIdx < 0 || greylistIdx+1 >= len(parts) {
		s.jsonError(w, http.StatusBadRequest, "ID required")
		return
	}

	id, err := strconv.ParseInt(parts[greylistIdx+1], 10, 64)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "Invalid ID")
		return
	}

	result, err := s.db.ExecContext(r.Context(),
		"UPDATE greylist SET passed = 1 WHERE id = ?", id)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to whitelist entry")
		return
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		s.jsonError(w, http.StatusNotFound, "Entry not found")
		return
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{"whitelisted": true})
}

// --- Security: Failed Logins ---

func (s *Server) handleAPISecurityFailedLogins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT remote_addr, COUNT(*) as attempt_count, MAX(created_at) as last_attempt
		FROM auth_log
		WHERE success = 0
		GROUP BY remote_addr
		ORDER BY attempt_count DESC
		LIMIT 100
	`)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "Failed to query failed logins")
		return
	}
	defer rows.Close()

	type FailedLogin struct {
		IP           string    `json:"ip"`
		AttemptCount int       `json:"attempt_count"`
		LastAttempt  time.Time `json:"last_attempt"`
	}

	entries := []FailedLogin{}
	for rows.Next() {
		var e FailedLogin
		if err := rows.Scan(&e.IP, &e.AttemptCount, &e.LastAttempt); err == nil {
			entries = append(entries, e)
		}
	}

	s.jsonResponse(w, http.StatusOK, entries)
}

// --- Security: Overview counts ---

func (s *Server) handleAPISecurityOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	ctx := r.Context()
	var suppressionCount, greylistCount, failedLoginCount int

	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM suppression_list").Scan(&suppressionCount)
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM greylist").Scan(&greylistCount)
	s.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT remote_addr) FROM auth_log WHERE success = 0").Scan(&failedLoginCount)

	// Daily failed logins trend (last 30 days)
	type DayCount struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
	}
	dailyTrend := []DayCount{}
	trendRows, trendErr := s.db.QueryContext(ctx,
		`SELECT date(created_at) as d, COUNT(*) as c FROM auth_log
		 WHERE success = 0 AND created_at >= datetime('now', '-30 days')
		 GROUP BY d ORDER BY d`)
	if trendErr == nil {
		defer trendRows.Close()
		for trendRows.Next() {
			var dc DayCount
			if err := trendRows.Scan(&dc.Date, &dc.Count); err == nil {
				dailyTrend = append(dailyTrend, dc)
			}
		}
	}

	// Top offending IPs (last 7 days)
	type IPCount struct {
		IP    string `json:"ip"`
		Count int    `json:"count"`
	}
	topIPs := []IPCount{}
	ipRows, ipErr := s.db.QueryContext(ctx,
		`SELECT remote_addr, COUNT(*) as c FROM auth_log
		 WHERE success = 0 AND created_at >= datetime('now', '-7 days')
		 GROUP BY remote_addr ORDER BY c DESC LIMIT 10`)
	if ipErr == nil {
		defer ipRows.Close()
		for ipRows.Next() {
			var ic IPCount
			if err := ipRows.Scan(&ic.IP, &ic.Count); err == nil {
				topIPs = append(topIPs, ic)
			}
		}
	}

	// Protocol breakdown
	type ProtoCount struct {
		Protocol string `json:"protocol"`
		Count    int    `json:"count"`
	}
	protocols := []ProtoCount{}
	protoRows, protoErr := s.db.QueryContext(ctx,
		`SELECT COALESCE(protocol, 'unknown'), COUNT(*) as c FROM auth_log
		 WHERE success = 0 AND created_at >= datetime('now', '-30 days')
		 GROUP BY protocol ORDER BY c DESC`)
	if protoErr == nil {
		defer protoRows.Close()
		for protoRows.Next() {
			var pc ProtoCount
			if err := protoRows.Scan(&pc.Protocol, &pc.Count); err == nil {
				protocols = append(protocols, pc)
			}
		}
	}

	s.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"suppression_count":  suppressionCount,
		"greylist_count":     greylistCount,
		"failed_login_count": failedLoginCount,
		"daily_trend":        dailyTrend,
		"top_ips":            topIPs,
		"protocol_breakdown": protocols,
	})
}
