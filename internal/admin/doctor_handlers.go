package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/fenilsonani/email-server/internal/doctor"
)

// handleDoctor shows the doctor/diagnostics page
func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Create doctor instance
	d := doctor.New(s.config, s.queue)

	// Run all checks
	results := d.Run(ctx)

	// Run comparison
	comparison := doctor.CompareConfigToReality(ctx, s.config, s.queue)

	// Group checks by category
	checksByCategory := make(map[doctor.Category][]doctor.CheckResult)
	for _, check := range results.Checks {
		checksByCategory[check.Category] = append(checksByCategory[check.Category], check)
	}

	data := map[string]interface{}{
		"Title":            "Server Diagnostics",
		"Results":          results,
		"Comparison":       comparison,
		"ChecksByCategory": checksByCategory,
		"Categories": []struct {
			Key   doctor.Category
			Label string
		}{
			{doctor.CategoryInfra, "Infrastructure"},
			{doctor.CategoryNetwork, "Network"},
			{doctor.CategorySecurity, "Security"},
			{doctor.CategoryDNS, "DNS"},
			{doctor.CategoryConfig, "Configuration"},
			{doctor.CategoryQueue, "Queue"},
		},
	}

	s.renderTemplate(w, "doctor.html", data)
}

// handleDoctorAPI returns doctor results as JSON
func (s *Server) handleDoctorAPI(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	d := doctor.New(s.config, s.queue)
	results := d.Run(ctx)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleDoctorCompareAPI returns comparison results as JSON
func (s *Server) handleDoctorCompareAPI(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	comparison := doctor.CompareConfigToReality(ctx, s.config, s.queue)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(comparison)
}

// handleDoctorFixAPI handles fix requests
func (s *Server) handleDoctorFixAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Parse request
	var req struct {
		FixID  string `json:"fix_id"`
		DryRun bool   `json:"dry_run"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	d := doctor.New(s.config, s.queue)

	// Apply fix
	msg, err := d.ApplyFix(ctx, req.FixID, req.DryRun)

	response := map[string]interface{}{
		"fix_id":  req.FixID,
		"dry_run": req.DryRun,
	}

	if err != nil {
		response["success"] = false
		response["error"] = err.Error()
	} else {
		response["success"] = true
		response["message"] = msg
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDoctorFixAllAPI handles fix all requests
func (s *Server) handleDoctorFixAllAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	// Parse request
	var req struct {
		DryRun bool `json:"dry_run"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.DryRun = true // Default to dry run for safety
	}

	d := doctor.New(s.config, s.queue)

	// Run checks first to find fixable issues
	results := d.Run(ctx)

	// Apply all fixes
	fixResults, err := d.ApplyAllFixable(ctx, results, req.DryRun)

	response := map[string]interface{}{
		"dry_run": req.DryRun,
		"results": fixResults,
	}

	if err != nil {
		response["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDoctorCategoryAPI returns checks for a specific category
func (s *Server) handleDoctorCategoryAPI(w http.ResponseWriter, r *http.Request) {
	categoryStr := r.URL.Query().Get("category")
	if categoryStr == "" {
		http.Error(w, "Category required", http.StatusBadRequest)
		return
	}

	category, ok := doctor.ParseCategory(categoryStr)
	if !ok {
		http.Error(w, "Invalid category", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	d := doctor.New(s.config, s.queue)
	results := d.RunCategory(ctx, category)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}
