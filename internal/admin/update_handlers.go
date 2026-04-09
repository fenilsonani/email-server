package admin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fenilsonani/email-server/internal/audit"
	"github.com/fenilsonani/email-server/internal/doctor"
	"github.com/fenilsonani/email-server/internal/updater"
)

// handleUpdate renders the update management page
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title": "System Updates",
	}
	s.renderTemplate(w, "update.html", data)
}

// getClientIP extracts the client IP address from the request
func getClientIP(r *http.Request) string {
	directIP := normalizeRemoteIP(r.RemoteAddr)
	if directIP == "" {
		directIP = r.RemoteAddr
	}

	if directIP == "127.0.0.1" || directIP == "::1" {
		if ip := forwardedIPFromHeader(r.Header.Get("X-Forwarded-For")); ip != "" {
			return ip
		}
		if ip := r.Header.Get("X-Real-IP"); ip != "" {
			if parsed := normalizeRemoteIP(strings.TrimSpace(ip)); parsed != "" {
				return parsed
			}
		}
	}

	return directIP
}

// isAdmin checks if the user making the request is an admin
func (s *Server) isAdmin(r *http.Request) bool {
	cookie, err := r.Cookie("admin_session")
	if err != nil {
		return false
	}
	userID, valid := s.validateSession(cookie.Value)
	if !valid {
		return false
	}
	var isAdmin bool
	err = s.db.QueryRowContext(r.Context(), "SELECT is_admin FROM users WHERE id = ?", userID).Scan(&isAdmin)
	return err == nil && isAdmin
}

// getUsername retrieves the username for the currently authenticated user
func (s *Server) getUsername(r *http.Request) string {
	cookie, err := r.Cookie("admin_session")
	if err != nil {
		return "unknown"
	}
	userID, valid := s.validateSession(cookie.Value)
	if !valid {
		return "unknown"
	}
	var username string
	err = s.db.QueryRowContext(r.Context(), "SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	if err != nil {
		return "unknown"
	}
	return username
}

// UpdateStatusResponse represents the current update status
type UpdateStatusResponse struct {
	Status          string               `json:"status"`
	CurrentVersion  string               `json:"current_version"`
	CurrentCommit   string               `json:"current_commit"`
	AvailableUpdate *updater.ReleaseInfo `json:"available_update,omitempty"`
	Mode            string               `json:"mode"`
	LastCheckTime   *time.Time           `json:"last_check_time,omitempty"`
}

// UpdateHistoryEntry represents a single update in history
type UpdateHistoryEntry struct {
	ID                int64      `json:"id"`
	UpdateType        string     `json:"update_type"`
	FromVersion       string     `json:"from_version"`
	ToVersion         string     `json:"to_version"`
	Status            string     `json:"status"`
	StartedBy         string     `json:"started_by"`
	StartedAt         time.Time  `json:"started_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	DurationSeconds   int        `json:"duration_seconds,omitempty"`
	BackupPath        string     `json:"backup_path,omitempty"`
	RollbackAvailable bool       `json:"rollback_available"`
	ErrorMessage      string     `json:"error_message,omitempty"`
}

// UpdateProgressResponse represents the progress of an ongoing update
type UpdateProgressResponse struct {
	UpdateID  int64                `json:"update_id"`
	Status    string               `json:"status"`
	Progress  int                  `json:"progress"`
	Steps     []UpdateProgressStep `json:"steps"`
	StartedAt time.Time            `json:"started_at"`
}

// UpdateProgressStep represents a single step in the update process
type UpdateProgressStep struct {
	Number      int        `json:"number"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Message     string     `json:"message"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// HandleGetUpdateStatus returns the current update status
func (s *Server) HandleGetUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	// Get current version
	versionMgr := updater.NewVersionManager(s.db, &s.config.Updater, s.logger)
	currentVersion, err := versionMgr.GetCurrentVersion(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get current version: %v", err), http.StatusInternalServerError)
		return
	}

	// Check for available updates
	availableUpdate, err := versionMgr.CheckForUpdates(ctx)
	if err != nil {
		s.logger.Warn("Failed to check for updates", "error", err)
	}

	response := UpdateStatusResponse{
		Status:          "idle",
		CurrentVersion:  currentVersion.Version,
		CurrentCommit:   currentVersion.Commit,
		AvailableUpdate: availableUpdate,
		Mode:            s.config.Updater.Mode,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleGetAvailableUpdates returns a list of available updates
func (s *Server) HandleGetAvailableUpdates(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	versionMgr := updater.NewVersionManager(s.db, &s.config.Updater, s.logger)
	releases, err := versionMgr.GetAvailableReleases(ctx, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get releases: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(releases)
}

// HandleStartUpdate starts an update process
func (s *Server) HandleStartUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Invalid form data", formErrorStatus(err))
		return
	}

	ctx := r.Context()
	username := s.getUsername(r)

	// Parse update options
	targetType := r.PostForm.Get("target_type")
	target := r.PostForm.Get("target")
	dryRun := r.PostForm.Get("dry_run") == "true"
	mode := r.PostForm.Get("mode")

	if mode == "" {
		mode = s.config.Updater.Mode
	}

	opts := updater.UpdateOptions{
		Mode:       updater.UpdateMode(mode),
		TargetType: updater.TargetType(targetType),
		Target:     target,
		DryRun:     dryRun,
		Username:   username,
	}

	// Create UpdateManager
	doc := doctor.New(s.config, s.queue)
	updateMgr := updater.NewUpdateManager(s.db, &s.config.Updater, s.logger, doc)

	// Start the update
	result, err := updateMgr.StartUpdate(ctx, opts)
	if err != nil {
		s.auditLogger.Log(ctx, username, audit.EventConfigChange, "system_update", map[string]interface{}{
			"status": "failed",
			"error":  err.Error(),
		}, getClientIP(r))
		http.Error(w, fmt.Sprintf("Failed to start update: %v", err), http.StatusInternalServerError)
		return
	}

	// Log the update attempt
	status := "completed"
	if !result.Success {
		status = "failed"
	}
	s.auditLogger.Log(ctx, username, audit.EventConfigChange, "system_update", map[string]interface{}{
		"status":    status,
		"from":      result.FromVersion,
		"to":        result.ToVersion,
		"duration":  result.Duration.String(),
		"update_id": result.UpdateID,
	}, getClientIP(r))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":            result.Success,
		"update_id":          result.UpdateID,
		"from_version":       result.FromVersion,
		"to_version":         result.ToVersion,
		"duration":           result.Duration.String(),
		"steps_completed":    result.StepsCompleted,
		"backup_path":        result.BackupPath,
		"rollback_available": result.RollbackAvailable,
	})
}

// HandleGetUpdateProgress returns the progress of an ongoing update
func (s *Server) HandleGetUpdateProgress(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	updateIDStr := r.URL.Query().Get("id")
	updateID, err := strconv.ParseInt(updateIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid update ID", http.StatusBadRequest)
		return
	}

	progressTracker := updater.NewProgressTracker(s.db, s.logger)

	// Get overall progress
	overallProgress, err := progressTracker.GetOverallProgress(ctx, updateID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get progress: %v", err), http.StatusInternalServerError)
		return
	}

	// Get step-by-step progress
	steps, err := progressTracker.GetProgress(ctx, updateID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get steps: %v", err), http.StatusInternalServerError)
		return
	}

	// Get update status
	status, err := progressTracker.GetUpdateStatus(ctx, updateID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get status: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert steps to response format
	responseSteps := make([]UpdateProgressStep, len(steps))
	for i, step := range steps {
		responseSteps[i] = UpdateProgressStep{
			Number:      step.StepNumber,
			Name:        step.StepName,
			Status:      step.Status,
			Message:     step.Message,
			StartedAt:   step.StartedAt,
			CompletedAt: step.CompletedAt,
		}
	}

	response := UpdateProgressResponse{
		UpdateID: updateID,
		Status:   status,
		Progress: overallProgress,
		Steps:    responseSteps,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// HandleGetUpdateHistory returns the update history
func (s *Server) HandleGetUpdateHistory(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	limit := 50

	query := `
	SELECT id, update_type, from_version, to_version, status, started_by, started_at,
	       completed_at, duration_seconds, backup_path, rollback_available, error_message
	FROM update_history
	ORDER BY started_at DESC
	LIMIT ?
	`
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to query history: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var history []UpdateHistoryEntry
	for rows.Next() {
		var entry UpdateHistoryEntry
		var completedAt sql.NullTime
		var durationSeconds sql.NullInt64
		var backupPath sql.NullString
		var errorMessage sql.NullString

		if err := rows.Scan(
			&entry.ID, &entry.UpdateType, &entry.FromVersion, &entry.ToVersion,
			&entry.Status, &entry.StartedBy, &entry.StartedAt,
			&completedAt, &durationSeconds, &backupPath, &entry.RollbackAvailable, &errorMessage,
		); err != nil {
			http.Error(w, fmt.Sprintf("Failed to scan row: %v", err), http.StatusInternalServerError)
			return
		}

		if completedAt.Valid {
			entry.CompletedAt = &completedAt.Time
		}
		if durationSeconds.Valid {
			entry.DurationSeconds = int(durationSeconds.Int64)
		}
		if backupPath.Valid {
			entry.BackupPath = backupPath.String
		}
		if errorMessage.Valid {
			entry.ErrorMessage = errorMessage.String
		}

		history = append(history, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

// HandleRollbackUpdate rolls back to a previous version
func (s *Server) HandleRollbackUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	username := s.getUsername(r)

	updateIDStr := strings.TrimPrefix(r.URL.Path, "/admin/system/update/rollback/")
	updateID, err := strconv.ParseInt(updateIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid update ID", http.StatusBadRequest)
		return
	}

	updateMgr := updater.NewUpdateManager(s.db, &s.config.Updater, s.logger, nil)
	if err := updateMgr.RollbackUpdate(ctx, updateID); err != nil {
		s.auditLogger.Log(ctx, username, audit.EventConfigChange, "system_rollback", map[string]interface{}{
			"update_id": updateID,
			"status":    "failed",
			"error":     err.Error(),
		}, getClientIP(r))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Rollback failed: %v", err),
		})
		return
	}

	s.auditLogger.Log(ctx, username, audit.EventConfigChange, "system_rollback", map[string]interface{}{
		"update_id": updateID,
		"status":    "completed",
	}, getClientIP(r))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"message":   "Rollback completed",
		"update_id": updateID,
	})
}

// HandleGetUpdateSettings returns current update settings
func (s *Server) HandleGetUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"mode":                     s.config.Updater.Mode,
		"auto_check_enabled":       s.config.Updater.AutoCheckEnabled,
		"auto_check_interval":      s.config.Updater.AutoCheckInterval,
		"backup_before_update":     s.config.Updater.BackupBeforeUpdate,
		"max_backups":              s.config.Updater.MaxBackups,
		"require_health_check":     s.config.Updater.RequireHealthCheck,
		"auto_rollback_on_failure": s.config.Updater.AutoRollbackOnFailure,
	})
}

// HandleUpdateSettings updates the update settings
func (s *Server) HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := parseFormWithLimit(w, r, maxAdminFormBody); err != nil {
		http.Error(w, "Invalid form data", formErrorStatus(err))
		return
	}

	username := s.getUsername(r)

	// Update settings (in production, would save to database)
	mode := r.PostForm.Get("mode")
	if mode != "" && (mode == "normal" || mode == "power") {
		s.config.Updater.Mode = mode
		s.auditLogger.Log(r.Context(), username, audit.EventConfigChange, "system_update_settings", map[string]interface{}{
			"mode": mode,
		}, getClientIP(r))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Settings updated",
	})
}
