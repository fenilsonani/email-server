package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// listTemplates returns all templates for a domain
func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request, domainID int64) {
	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, slug, name, subject, html_body, text_body, variables, is_active, created_at, updated_at
		FROM email_templates
		WHERE domain_id = ? AND is_active = TRUE
		ORDER BY name ASC
	`, domainID)
	if err != nil {
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var templates []EmailTemplate
	for rows.Next() {
		var t EmailTemplate
		var htmlBody, textBody, variablesJSON sql.NullString

		err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.Subject, &htmlBody, &textBody, &variablesJSON, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			// Scan errors signal a query/schema/type mismatch or driver-level
			// read failure — not a row-level data problem we can skip past.
			// Surface them as a 500 so a broken deployment doesn't quietly
			// return a partial template list.
			s.logger.ErrorContext(r.Context(), "template row scan failed", err, "domain_id", domainID)
			jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
			return
		}

		if htmlBody.Valid {
			t.HTMLBody = htmlBody.String
		}
		if textBody.Valid {
			t.TextBody = textBody.String
		}
		if variablesJSON.Valid && variablesJSON.String != "" {
			if err := json.Unmarshal([]byte(variablesJSON.String), &t.Variables); err != nil {
				s.logger.WarnContext(r.Context(), "skipping template with malformed variables JSON", "template_id", t.ID, "domain_id", domainID, "error", err)
				continue
			}
		}

		t.DomainID = domainID
		templates = append(templates, t)
	}

	jsonResponse(w, templates, http.StatusOK)
}

// createTemplate creates a new email template
func (s *Server) createTemplate(w http.ResponseWriter, r *http.Request, domainID int64) {
	var req CreateTemplateRequest
	if err := parseJSONRequest(r, &req); err != nil {
		jsonError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	// Validate
	if req.Slug == "" || req.Name == "" || req.Subject == "" {
		jsonError(w, "slug, name, and subject are required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	if req.HTMLBody == "" && req.TextBody == "" {
		jsonError(w, "html_body or text_body is required", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Validate slug format
	if !isValidSlug(req.Slug) {
		jsonError(w, "slug must contain only lowercase letters, numbers, and hyphens", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	// Extract variables from template if not provided
	variables := req.Variables
	if len(variables) == 0 {
		variables = extractVariables(req.HTMLBody + " " + req.TextBody + " " + req.Subject)
	}

	variablesJSON, _ := json.Marshal(variables)
	now := time.Now()

	result, err := s.db.ExecContext(r.Context(), `
		INSERT INTO email_templates (domain_id, slug, name, subject, html_body, text_body, variables, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, domainID, req.Slug, req.Name, req.Subject, nullString(req.HTMLBody), nullString(req.TextBody), string(variablesJSON), now, now)

	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			jsonError(w, "Template with this slug already exists", "CONFLICT", http.StatusConflict)
			return
		}
		s.logger.Error("Failed to create template", "error", err.Error())
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()

	template := &EmailTemplate{
		ID:        id,
		DomainID:  domainID,
		Slug:      req.Slug,
		Name:      req.Name,
		Subject:   req.Subject,
		HTMLBody:  req.HTMLBody,
		TextBody:  req.TextBody,
		Variables: variables,
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	jsonResponse(w, template, http.StatusCreated)
}

// getTemplate returns a single template
func (s *Server) getTemplate(w http.ResponseWriter, r *http.Request, domainID int64, slug string) {
	template, err := s.getTemplateBySlug(r.Context(), domainID, slug)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "Template not found", "NOT_FOUND", http.StatusNotFound)
		} else {
			jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		}
		return
	}

	jsonResponse(w, template, http.StatusOK)
}

// updateTemplate updates an existing template
func (s *Server) updateTemplate(w http.ResponseWriter, r *http.Request, domainID int64, slug string) {
	var req CreateTemplateRequest
	if err := parseJSONRequest(r, &req); err != nil {
		jsonError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	// Check template exists
	existing, err := s.getTemplateBySlug(r.Context(), domainID, slug)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "Template not found", "NOT_FOUND", http.StatusNotFound)
		} else {
			jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		}
		return
	}

	// Update fields
	name := req.Name
	if name == "" {
		name = existing.Name
	}
	subject := req.Subject
	if subject == "" {
		subject = existing.Subject
	}
	htmlBody := req.HTMLBody
	if htmlBody == "" {
		htmlBody = existing.HTMLBody
	}
	textBody := req.TextBody
	if textBody == "" {
		textBody = existing.TextBody
	}

	variables := req.Variables
	if len(variables) == 0 {
		variables = extractVariables(htmlBody + " " + textBody + " " + subject)
	}
	variablesJSON, _ := json.Marshal(variables)

	now := time.Now()
	_, err = s.db.ExecContext(r.Context(), `
		UPDATE email_templates
		SET name = ?, subject = ?, html_body = ?, text_body = ?, variables = ?, updated_at = ?
		WHERE domain_id = ? AND slug = ?
	`, name, subject, nullString(htmlBody), nullString(textBody), string(variablesJSON), now, domainID, slug)

	if err != nil {
		s.logger.Error("Failed to update template", "error", err.Error())
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	// Return updated template
	template, _ := s.getTemplateBySlug(r.Context(), domainID, slug)
	jsonResponse(w, template, http.StatusOK)
}

// deleteTemplate deletes a template
func (s *Server) deleteTemplate(w http.ResponseWriter, r *http.Request, domainID int64, slug string) {
	result, err := s.db.ExecContext(r.Context(), `
		DELETE FROM email_templates WHERE domain_id = ? AND slug = ?
	`, domainID, slug)

	if err != nil {
		jsonError(w, "Internal server error", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		jsonError(w, "Template not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// getTemplateBySlug retrieves a template by its slug
func (s *Server) getTemplateBySlug(ctx context.Context, domainID int64, slug string) (*EmailTemplate, error) {
	var t EmailTemplate
	var htmlBody, textBody, variablesJSON sql.NullString

	err := s.db.QueryRowContext(ctx, `
		SELECT id, slug, name, subject, html_body, text_body, variables, is_active, created_at, updated_at
		FROM email_templates
		WHERE domain_id = ? AND slug = ?
	`, domainID, slug).Scan(&t.ID, &t.Slug, &t.Name, &t.Subject, &htmlBody, &textBody, &variablesJSON, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)

	if err != nil {
		return nil, err
	}

	if htmlBody.Valid {
		t.HTMLBody = htmlBody.String
	}
	if textBody.Valid {
		t.TextBody = textBody.String
	}
	if variablesJSON.Valid && variablesJSON.String != "" {
		if err := json.Unmarshal([]byte(variablesJSON.String), &t.Variables); err != nil {
			return nil, fmt.Errorf("template %q has malformed variables: %w", t.Slug, err)
		}
	}

	t.DomainID = domainID
	return &t, nil
}

// renderTemplateString renders a template string with variable substitution
func renderTemplateString(tmpl string, vars map[string]string) string {
	if tmpl == "" {
		return ""
	}

	result := tmpl
	for key, val := range vars {
		// Escape HTML in values to prevent XSS
		escapedVal := html.EscapeString(val)
		result = strings.ReplaceAll(result, "{{"+key+"}}", escapedVal)
	}

	return result
}

// extractVariables extracts {{variable}} patterns from a template
func extractVariables(content string) []string {
	re := regexp.MustCompile(`\{\{(\w+)\}\}`)
	matches := re.FindAllStringSubmatch(content, -1)

	seen := make(map[string]bool)
	var variables []string

	for _, match := range matches {
		if len(match) >= 2 && !seen[match[1]] {
			seen[match[1]] = true
			variables = append(variables, match[1])
		}
	}

	return variables
}

// isValidSlug checks if a slug is valid
func isValidSlug(slug string) bool {
	if len(slug) == 0 || len(slug) > 64 {
		return false
	}
	re := regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)
	return re.MatchString(slug)
}

// nullString converts empty string to sql.NullString
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
