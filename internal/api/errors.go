package api

import (
	"encoding/json"
	"net/http"
)

// Error codes for API responses
const (
	CodeValidationError     = "VALIDATION_ERROR"
	CodeInvalidRequest      = "INVALID_REQUEST"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeForbidden           = "FORBIDDEN"
	CodeNotFound            = "NOT_FOUND"
	CodeMethodNotAllowed    = "METHOD_NOT_ALLOWED"
	CodeRateLimited         = "RATE_LIMITED"
	CodeConflict            = "CONFLICT"
	CodeInternalError       = "INTERNAL_ERROR"
	CodeQueueError          = "QUEUE_ERROR"
	CodeIdempotencyConflict = "IDEMPOTENCY_CONFLICT"
	CodeSuppressed          = "SUPPRESSED"
	CodeSchedulingError     = "SCHEDULING_ERROR"
)

// errorResponse sends a JSON error response with request ID
func errorResponse(w http.ResponseWriter, requestID, message, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIError{
		Error:     message,
		Code:      code,
		RequestID: requestID,
	})
}

// errorResponseWithField sends a JSON error response with field information
func errorResponseWithField(w http.ResponseWriter, requestID, message, code, field string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIError{
		Error:     message,
		Code:      code,
		Field:     field,
		RequestID: requestID,
	})
}

// errorResponseWithDetails sends a JSON error response with additional details
func errorResponseWithDetails(w http.ResponseWriter, requestID, message, code, details string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIError{
		Error:     message,
		Code:      code,
		Details:   details,
		RequestID: requestID,
	})
}

// successResponse sends a JSON success response with request ID
func successResponse(w http.ResponseWriter, requestID string, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-ID", requestID)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// ValidationError represents a field validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// NewValidationError creates a new validation error
func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{Field: field, Message: message}
}
