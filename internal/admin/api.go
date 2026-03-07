package admin

import (
	"encoding/json"
	"net/http"
)

// APIResponse is the standard JSON response envelope
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Meta    *APIMeta    `json:"meta,omitempty"`
}

// APIMeta holds pagination metadata
type APIMeta struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalCount int `json:"total_count"`
	TotalPages int `json:"total_pages"`
}

// jsonResponse sends a success JSON response
func (s *Server) jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{Success: true, Data: data})
}

// jsonResponseWithMeta sends a success JSON response with pagination meta
func (s *Server) jsonResponseWithMeta(w http.ResponseWriter, status int, data interface{}, meta *APIMeta) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{Success: true, Data: data, Meta: meta})
}

// jsonError sends an error JSON response
func (s *Server) jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIResponse{Success: false, Error: msg})
}

// decodeJSON decodes a JSON request body into the given struct
func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
