package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SuppressionService manages the email suppression list.
type SuppressionService struct {
	db *sql.DB
}

// NewSuppressionService creates a new suppression service.
func NewSuppressionService(db *sql.DB) *SuppressionService {
	return &SuppressionService{db: db}
}

// IsValidSuppressionReason checks if a suppression reason is valid.
func IsValidSuppressionReason(reason string) bool {
	switch reason {
	case SuppressionHardBounce, SuppressionUnsubscribe, SuppressionComplaint, SuppressionManual:
		return true
	default:
		return false
	}
}

// IsSuppressed checks if an email address is in the suppression list for a domain.
func (s *SuppressionService) IsSuppressed(ctx context.Context, domainID int64, email string) (bool, *Suppression, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	var sup Suppression
	err := s.db.QueryRowContext(ctx, `
		SELECT id, domain_id, email, reason, created_at
		FROM suppression_list
		WHERE domain_id = ? AND email = ?
	`, domainID, email).Scan(&sup.ID, &sup.DomainID, &sup.Email, &sup.Reason, &sup.CreatedAt)

	if err == sql.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("failed to check suppression: %w", err)
	}

	return true, &sup, nil
}

// Add adds an email to the suppression list.
func (s *SuppressionService) Add(ctx context.Context, domainID int64, email, reason string) (*Suppression, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	if !IsValidSuppressionReason(reason) {
		return nil, fmt.Errorf("invalid suppression reason: %s", reason)
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO suppression_list (domain_id, email, reason)
		VALUES (?, ?, ?)
		ON CONFLICT(domain_id, email) DO UPDATE SET reason = excluded.reason
	`, domainID, email, reason)
	if err != nil {
		return nil, fmt.Errorf("failed to add suppression: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		// If we couldn't get the ID, fetch it
		err = s.db.QueryRowContext(ctx, `
			SELECT id FROM suppression_list WHERE domain_id = ? AND email = ?
		`, domainID, email).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("failed to get suppression ID: %w", err)
		}
	}

	return &Suppression{
		ID:        id,
		DomainID:  domainID,
		Email:     email,
		Reason:    reason,
		CreatedAt: time.Now(),
	}, nil
}

// Remove removes an email from the suppression list.
func (s *SuppressionService) Remove(ctx context.Context, domainID int64, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))

	result, err := s.db.ExecContext(ctx, `
		DELETE FROM suppression_list WHERE domain_id = ? AND email = ?
	`, domainID, email)
	if err != nil {
		return fmt.Errorf("failed to remove suppression: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// List returns the suppression list for a domain with pagination.
func (s *SuppressionService) List(ctx context.Context, domainID int64, page, perPage int) ([]Suppression, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	// Count total
	var total int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM suppression_list WHERE domain_id = ?
	`, domainID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count suppressions: %w", err)
	}

	// Fetch list
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, domain_id, email, reason, created_at
		FROM suppression_list
		WHERE domain_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, domainID, perPage, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list suppressions: %w", err)
	}
	defer rows.Close()

	var suppressions []Suppression
	for rows.Next() {
		var sup Suppression
		err := rows.Scan(&sup.ID, &sup.DomainID, &sup.Email, &sup.Reason, &sup.CreatedAt)
		if err != nil {
			continue
		}
		suppressions = append(suppressions, sup)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating suppressions: %w", err)
	}

	return suppressions, total, nil
}

// AddFromBounce automatically adds an email to suppression list after a hard bounce.
func (s *SuppressionService) AddFromBounce(ctx context.Context, domainID int64, email string) error {
	_, err := s.Add(ctx, domainID, email, SuppressionHardBounce)
	return err
}

// Handlers for suppression API endpoints

// handleSuppressions handles GET/POST /api/v1/suppressions
func (s *Server) handleSuppressions(w http.ResponseWriter, r *http.Request) {
	requestID := getRequestIDFromContext(r.Context())
	domainID, _ := s.getDomainID(r.Context())

	switch r.Method {
	case http.MethodGet:
		s.listSuppressions(w, r, requestID, domainID)
	case http.MethodPost:
		s.createSuppression(w, r, requestID, domainID)
	default:
		errorResponse(w, requestID, "Method not allowed", CodeMethodNotAllowed, http.StatusMethodNotAllowed)
	}
}

// handleSuppressionByEmail handles DELETE /api/v1/suppressions/{email}
func (s *Server) handleSuppressionByEmail(w http.ResponseWriter, r *http.Request) {
	requestID := getRequestIDFromContext(r.Context())
	domainID, _ := s.getDomainID(r.Context())

	// Extract email from path
	email := strings.TrimPrefix(r.URL.Path, "/api/v1/suppressions/")
	if email == "" {
		errorResponse(w, requestID, "Email is required", CodeValidationError, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getSuppression(w, r, requestID, domainID, email)
	case http.MethodDelete:
		s.deleteSuppression(w, r, requestID, domainID, email)
	default:
		errorResponse(w, requestID, "Method not allowed", CodeMethodNotAllowed, http.StatusMethodNotAllowed)
	}
}

func (s *Server) listSuppressions(w http.ResponseWriter, r *http.Request, requestID string, domainID int64) {
	// Parse pagination
	page := 1
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	perPage := 20
	if perPageStr := r.URL.Query().Get("per_page"); perPageStr != "" {
		if pp, err := strconv.Atoi(perPageStr); err == nil && pp > 0 && pp <= 100 {
			perPage = pp
		}
	}

	suppressions, total, err := s.suppression.List(r.Context(), domainID, page, perPage)
	if err != nil {
		s.logger.Error("Failed to list suppressions", "error", err.Error())
		errorResponse(w, requestID, "Internal server error", CodeInternalError, http.StatusInternalServerError)
		return
	}

	totalPages := int((total + int64(perPage) - 1) / int64(perPage))

	successResponse(w, requestID, ListResponse{
		Data:       suppressions,
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
	}, http.StatusOK)
}

func (s *Server) createSuppression(w http.ResponseWriter, r *http.Request, requestID string, domainID int64) {
	var req CreateSuppressionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, requestID, "Invalid request body", CodeInvalidRequest, http.StatusBadRequest)
		return
	}

	if req.Email == "" {
		errorResponseWithField(w, requestID, "Email is required", CodeValidationError, "email", http.StatusBadRequest)
		return
	}

	if !IsValidEmailFormat(req.Email) {
		errorResponseWithField(w, requestID, "Invalid email format", CodeValidationError, "email", http.StatusBadRequest)
		return
	}

	if req.Reason == "" {
		req.Reason = SuppressionManual
	}

	if !IsValidSuppressionReason(req.Reason) {
		errorResponseWithField(w, requestID, "Invalid reason. Must be: hard_bounce, unsubscribe, complaint, or manual", CodeValidationError, "reason", http.StatusBadRequest)
		return
	}

	suppression, err := s.suppression.Add(r.Context(), domainID, req.Email, req.Reason)
	if err != nil {
		s.logger.Error("Failed to add suppression", "error", err.Error())
		errorResponse(w, requestID, "Failed to add suppression", CodeInternalError, http.StatusInternalServerError)
		return
	}

	successResponse(w, requestID, suppression, http.StatusCreated)
}

func (s *Server) getSuppression(w http.ResponseWriter, r *http.Request, requestID string, domainID int64, email string) {
	suppressed, suppression, err := s.suppression.IsSuppressed(r.Context(), domainID, email)
	if err != nil {
		s.logger.Error("Failed to check suppression", "error", err.Error())
		errorResponse(w, requestID, "Internal server error", CodeInternalError, http.StatusInternalServerError)
		return
	}

	if !suppressed {
		errorResponse(w, requestID, "Email not found in suppression list", CodeNotFound, http.StatusNotFound)
		return
	}

	successResponse(w, requestID, suppression, http.StatusOK)
}

func (s *Server) deleteSuppression(w http.ResponseWriter, r *http.Request, requestID string, domainID int64, email string) {
	err := s.suppression.Remove(r.Context(), domainID, email)
	if err == sql.ErrNoRows {
		errorResponse(w, requestID, "Email not found in suppression list", CodeNotFound, http.StatusNotFound)
		return
	}
	if err != nil {
		s.logger.Error("Failed to remove suppression", "error", err.Error())
		errorResponse(w, requestID, "Failed to remove suppression", CodeInternalError, http.StatusInternalServerError)
		return
	}

	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(http.StatusNoContent)
}
