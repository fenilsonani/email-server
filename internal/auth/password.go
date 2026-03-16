package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/fenilsonani/email-server/internal/safecast"
	"golang.org/x/crypto/argon2"
)

// Pre-allocated errors for hash parsing (avoid allocations in hot path)
var (
	errInvalidHashFormat   = errors.New("invalid hash format")
	errUnsupportedAlgo     = errors.New("unsupported algorithm")
	errInvalidVersion      = errors.New("invalid version format")
	errIncompatibleVersion = errors.New("incompatible argon2 version")
	errInvalidParams       = errors.New("invalid parameters format")
	errInvalidSalt         = errors.New("invalid salt encoding")
	errInvalidHash         = errors.New("invalid hash encoding")
)

// Argon2id parameters (OWASP recommended)
const (
	argon2Time    = 3         // Number of iterations
	argon2Memory  = 64 * 1024 // 64 MB
	argon2Threads = 4         // Parallelism
	argon2KeyLen  = 32        // Output key length
	argon2SaltLen = 16        // Salt length
)

// HashPassword creates an argon2id hash of the password
func HashPassword(password string) (string, error) {
	// Generate random salt
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	// Hash password with argon2id
	hash := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	// Encode as "$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>"
	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Threads,
		b64Salt, b64Hash,
	)

	return encoded, nil
}

// VerifyPassword checks if a password matches the hash
func VerifyPassword(password, encoded string) bool {
	// Parse the encoded hash
	params, salt, hash, err := parseArgon2Hash(encoded)
	if err != nil {
		return false
	}

	// Compute hash with same parameters
	computedHash := argon2.IDKey(
		[]byte(password), salt,
		params.time, params.memory, params.threads, params.keyLen,
	)

	// Constant-time comparison
	return subtle.ConstantTimeCompare(hash, computedHash) == 1
}

type argon2Params struct {
	memory  uint32
	time    uint32
	threads uint8
	keyLen  uint32
}

// parseArgon2Hash parses an argon2id encoded hash string.
// Optimized to avoid fmt.Sscanf which is slow due to reflection.
func parseArgon2Hash(encoded string) (*argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return nil, nil, nil, errInvalidHashFormat
	}

	if parts[1] != "argon2id" {
		return nil, nil, nil, errUnsupportedAlgo
	}

	// Parse version: "v=19"
	if !strings.HasPrefix(parts[2], "v=") {
		return nil, nil, nil, errInvalidVersion
	}
	version, err := strconv.Atoi(parts[2][2:])
	if err != nil {
		return nil, nil, nil, errInvalidVersion
	}
	if version != argon2.Version {
		return nil, nil, nil, errIncompatibleVersion
	}

	// Parse parameters: "m=65536,t=3,p=4"
	params, err := parseArgon2Params(parts[3])
	if err != nil {
		return nil, nil, nil, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, nil, errInvalidSalt
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, nil, errInvalidHash
	}

	keyLen, err := safecast.IntToUint32(len(hash))
	if err != nil {
		return nil, nil, nil, errInvalidHash
	}
	params.keyLen = keyLen

	return params, salt, hash, nil
}

// parseArgon2Params parses the parameter string "m=65536,t=3,p=4"
// Uses direct string parsing instead of fmt.Sscanf for better performance.
func parseArgon2Params(s string) (*argon2Params, error) {
	var params argon2Params

	// Split by comma
	paramParts := strings.Split(s, ",")
	if len(paramParts) != 3 {
		return nil, errInvalidParams
	}

	for _, part := range paramParts {
		idx := strings.IndexByte(part, '=')
		if idx < 1 {
			return nil, errInvalidParams
		}
		key := part[:idx]
		val, err := strconv.ParseUint(part[idx+1:], 10, 32)
		if err != nil {
			return nil, errInvalidParams
		}

		switch key {
		case "m":
			params.memory = uint32(val)
		case "t":
			params.time = uint32(val)
		case "p":
			threads, err := safecast.Uint64ToUint8(val)
			if err != nil {
				return nil, errInvalidParams
			}
			params.threads = threads
		default:
			return nil, errInvalidParams
		}
	}

	return &params, nil
}
