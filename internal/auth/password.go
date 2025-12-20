package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
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

func parseArgon2Hash(encoded string) (*argon2Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return nil, nil, nil, fmt.Errorf("invalid hash format")
	}

	if parts[1] != "argon2id" {
		return nil, nil, nil, fmt.Errorf("unsupported algorithm: %s", parts[1])
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid version: %w", err)
	}
	if version != argon2.Version {
		return nil, nil, nil, fmt.Errorf("incompatible version: %d", version)
	}

	var params argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d",
		&params.memory, &params.time, &params.threads); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid parameters: %w", err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid salt: %w", err)
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid hash: %w", err)
	}

	params.keyLen = uint32(len(hash))

	return &params, salt, hash, nil
}
