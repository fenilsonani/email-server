package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

type contextKey string

const (
	contextKeyAPIKey contextKey = "api_key"
)

// Rate limiter for API keys
type rateLimiter struct {
	mu       sync.RWMutex
	counters map[int64]*rateCounter
}

type rateCounter struct {
	count     int
	resetTime time.Time
}

var apiRateLimiter = &rateLimiter{
	counters: make(map[int64]*rateCounter),
}

// authMiddleware validates API key and adds it to context
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearerToken(r)
		if token == "" {
			jsonError(w, "Missing API key", "UNAUTHORIZED", http.StatusUnauthorized)
			return
		}

		apiKey, err := s.validateAPIKey(r.Context(), token)
		if err != nil {
			s.logger.Warn("Invalid API key attempt", "error", err.Error(), "remote_addr", r.RemoteAddr)
			jsonError(w, "Invalid API key", "UNAUTHORIZED", http.StatusUnauthorized)
			return
		}

		// Check if key is active
		if !apiKey.IsActive {
			jsonError(w, "API key is disabled", "FORBIDDEN", http.StatusForbidden)
			return
		}

		// Check expiration
		if apiKey.ExpiresAt != nil && time.Now().After(*apiKey.ExpiresAt) {
			jsonError(w, "API key has expired", "FORBIDDEN", http.StatusForbidden)
			return
		}

		// Rate limiting
		if !s.checkRateLimit(apiKey) {
			jsonError(w, "Rate limit exceeded", "RATE_LIMITED", http.StatusTooManyRequests)
			return
		}

		// Update last used timestamp (async to not slow down request)
		go s.updateAPIKeyLastUsed(apiKey.ID)

		// Add API key to context
		ctx := context.WithValue(r.Context(), contextKeyAPIKey, apiKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractBearerToken extracts the token from Authorization header
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}

	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	return strings.TrimSpace(parts[1])
}

// validateAPIKey validates an API key and returns the APIKey if valid
func (s *Server) validateAPIKey(ctx context.Context, token string) (*APIKey, error) {
	// Token format: sk_live_<32-char-hex> or sk_test_<32-char-hex>
	if len(token) < 40 { // sk_live_ (8) + 32 chars minimum
		return nil, ErrInvalidAPIKey
	}

	prefix := token[:16] // sk_live_XXXXXXXX

	// Find API key by prefix
	var apiKey APIKey
	var keyHash, scopesJSON string
	var keySalt sql.NullString
	var expiresAt, lastUsedAt sql.NullTime

	err := s.db.QueryRowContext(ctx, `
		SELECT id, domain_id, key_hash, key_prefix, name, scopes, is_active,
		       rate_limit_per_hour, last_used_at, created_at, expires_at, key_salt
		FROM api_keys
		WHERE key_prefix = ? AND is_active = TRUE
	`, prefix).Scan(
		&apiKey.ID, &apiKey.DomainID, &keyHash, &apiKey.KeyPrefix, &apiKey.Name,
		&scopesJSON, &apiKey.IsActive, &apiKey.RateLimitPerHour, &lastUsedAt,
		&apiKey.CreatedAt, &expiresAt, &keySalt,
	)

	if err == sql.ErrNoRows {
		return nil, ErrInvalidAPIKey
	}
	if err != nil {
		return nil, err
	}

	// Verify the full key against stored hash
	// SECURITY: Use per-key salt if available, otherwise fall back to legacy fixed salt
	var salt string
	if keySalt.Valid && keySalt.String != "" {
		salt = keySalt.String
	}
	if !verifyAPIKeyHashWithSalt(token, keyHash, salt) {
		return nil, ErrInvalidAPIKey
	}

	// Parse scopes
	if scopesJSON != "" {
		json.Unmarshal([]byte(scopesJSON), &apiKey.Scopes)
	}

	if lastUsedAt.Valid {
		apiKey.LastUsedAt = &lastUsedAt.Time
	}
	if expiresAt.Valid {
		apiKey.ExpiresAt = &expiresAt.Time
	}

	return &apiKey, nil
}

// checkRateLimit checks if the API key has exceeded its rate limit
func (s *Server) checkRateLimit(apiKey *APIKey) bool {
	apiRateLimiter.mu.Lock()
	defer apiRateLimiter.mu.Unlock()

	now := time.Now()
	counter, exists := apiRateLimiter.counters[apiKey.ID]

	if !exists || now.After(counter.resetTime) {
		// Create new counter or reset
		apiRateLimiter.counters[apiKey.ID] = &rateCounter{
			count:     1,
			resetTime: now.Add(time.Hour),
		}
		return true
	}

	if counter.count >= apiKey.RateLimitPerHour {
		return false
	}

	counter.count++
	return true
}

// updateAPIKeyLastUsed updates the last_used_at timestamp
func (s *Server) updateAPIKeyLastUsed(keyID int64) {
	_, _ = s.db.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, time.Now(), keyID)
}

// requireScope middleware checks if the API key has the required scope
func (s *Server) requireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := getAPIKeyFromContext(r.Context())
			if apiKey == nil {
				jsonError(w, "Unauthorized", "UNAUTHORIZED", http.StatusUnauthorized)
				return
			}

			if !hasScope(apiKey.Scopes, scope) && !hasScope(apiKey.Scopes, ScopeAdmin) {
				jsonError(w, "Insufficient permissions", "FORBIDDEN", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getAPIKeyFromContext retrieves the API key from request context
func getAPIKeyFromContext(ctx context.Context) *APIKey {
	key, _ := ctx.Value(contextKeyAPIKey).(*APIKey)
	return key
}

// hasScope checks if a scope list contains a specific scope
func hasScope(scopes []string, target string) bool {
	for _, s := range scopes {
		if s == target {
			return true
		}
	}
	return false
}

// GenerateAPIKey generates a new API key with a random per-key salt
// SECURITY: Each key has its own salt for better protection against rainbow tables
func GenerateAPIKey(isTest bool) (fullKey, prefix, hash, salt string, err error) {
	// Generate 32 random bytes for the key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return "", "", "", "", err
	}

	keyHex := hex.EncodeToString(keyBytes)

	// Format: sk_live_<hex> or sk_test_<hex>
	keyType := "live"
	if isTest {
		keyType = "test"
	}

	fullKey = "sk_" + keyType + "_" + keyHex
	prefix = fullKey[:16] // First 16 chars for lookup

	// Generate random salt for this key
	salt, err = generateAPIKeySalt()
	if err != nil {
		return "", "", "", "", err
	}

	// Hash the full key with the salt
	hash = hashAPIKeyWithSalt(fullKey, salt)

	return fullKey, prefix, hash, salt, nil
}

// legacyAPIKeySalt is used for backward compatibility with existing API keys
// SECURITY: New keys use per-key random salt instead
const legacyAPIKeySalt = "mailserver_api_key_salt_v1"

// generateAPIKeySalt generates a random salt for a new API key
func generateAPIKeySalt() (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return hex.EncodeToString(salt), nil
}

// hashAPIKeyWithSalt hashes an API key using argon2id with the given salt
// SECURITY: Uses 3 iterations (same as password hashing) for better security
func hashAPIKeyWithSalt(key, salt string) string {
	var saltBytes []byte
	if salt != "" {
		saltBytes, _ = hex.DecodeString(salt)
	} else {
		// Legacy: use fixed salt for backward compatibility
		saltBytes = []byte(legacyAPIKeySalt)
	}
	// Use 3 iterations instead of 1 for better security
	hash := argon2.IDKey([]byte(key), saltBytes, 3, 64*1024, 4, 32)
	return hex.EncodeToString(hash)
}

// hashAPIKey hashes an API key using the legacy fixed salt (for backward compatibility)
// DEPRECATED: Use hashAPIKeyWithSalt for new keys
func hashAPIKey(key string) string {
	return hashAPIKeyWithSalt(key, "")
}

// verifyAPIKeyHashWithSalt verifies a key against its hash using the given salt
func verifyAPIKeyHashWithSalt(key, storedHash, salt string) bool {
	computedHash := hashAPIKeyWithSalt(key, salt)
	return subtle.ConstantTimeCompare([]byte(computedHash), []byte(storedHash)) == 1
}

// verifyAPIKeyHash verifies a key against its hash (legacy, uses fixed salt)
// DEPRECATED: Use verifyAPIKeyHashWithSalt for new keys
func verifyAPIKeyHash(key, storedHash string) bool {
	return verifyAPIKeyHashWithSalt(key, storedHash, "")
}

// jsonError sends a JSON error response
func jsonError(w http.ResponseWriter, message, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(APIError{
		Error: message,
		Code:  code,
	})
}

// jsonResponse sends a JSON response
func jsonResponse(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// corsMiddleware adds CORS headers for API access
// SECURITY: Restricts CORS to same-origin requests only
// API is designed for server-to-server communication, not browser clients
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Only allow same-origin requests or requests without Origin header (non-browser)
		// For cross-origin browser requests, the origin must match the server hostname
		if origin != "" {
			// Check if origin matches server hostname
			serverHost := s.config.Server.Hostname
			allowedOrigins := []string{
				"https://" + serverHost,
				"https://mail." + serverHost,
			}

			allowed := false
			for _, allowedOrigin := range allowedOrigins {
				if origin == allowedOrigin {
					allowed = true
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					break
				}
			}

			if !allowed {
				// Don't set CORS headers for unauthorized origins
				// Browser will block the request
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusForbidden)
					return
				}
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// requestLoggingMiddleware logs API requests
func (s *Server) requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status
		wrapper := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapper, r)

		// Get API key ID if available
		var keyID int64
		if apiKey := getAPIKeyFromContext(r.Context()); apiKey != nil {
			keyID = apiKey.ID
		}

		s.logger.Info("API request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapper.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"api_key_id", keyID,
			"remote_addr", r.RemoteAddr,
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Errors
var (
	ErrInvalidAPIKey = &apiErr{message: "invalid API key", code: "INVALID_API_KEY"}
)

type apiErr struct {
	message string
	code    string
}

func (e *apiErr) Error() string {
	return e.message
}
