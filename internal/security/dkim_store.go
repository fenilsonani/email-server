package security

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// KeyMetadata contains information about a DKIM key
type KeyMetadata struct {
	Domain      string
	Selector    string
	Algorithm   string
	CreatedAt   time.Time
	StorageType string
	KeyFile     string
	HasKey      bool
}

// DKIMKeyStore provides abstracted key storage
type DKIMKeyStore interface {
	// SaveKey saves a DKIM private key for a domain
	SaveKey(ctx context.Context, domain string, privateKey *rsa.PrivateKey, selector string, algorithm string) error
	// LoadKey loads a DKIM private key for a domain, returns key and selector
	LoadKey(ctx context.Context, domain string) (*rsa.PrivateKey, string, error)
	// DeleteKey removes the DKIM key for a domain
	DeleteKey(ctx context.Context, domain string) error
	// KeyExists checks if a key exists for a domain
	KeyExists(ctx context.Context, domain string) bool
	// GetPublicKeyDNS returns the public key formatted for DNS TXT record
	GetPublicKeyDNS(ctx context.Context, domain string) (string, error)
	// GetKeyMetadata returns metadata about the stored key
	GetKeyMetadata(ctx context.Context, domain string) (*KeyMetadata, error)
	// ListDomains lists all domains with keys
	ListDomains(ctx context.Context) ([]KeyMetadata, error)
}

// FileKeyStore implements file-based DKIM key storage
type FileKeyStore struct {
	basePath string
	db       *sql.DB // For reading selector from database
}

// NewFileKeyStore creates a new file-based key store
func NewFileKeyStore(basePath string, db *sql.DB) *FileKeyStore {
	return &FileKeyStore{
		basePath: basePath,
		db:       db,
	}
}

// keyPath returns the path for a domain's private key
func (s *FileKeyStore) keyPath(domain string) string {
	return filepath.Join(s.basePath, domain+".key")
}

// pubKeyPath returns the path for a domain's public key
func (s *FileKeyStore) pubKeyPath(domain string) string {
	return filepath.Join(s.basePath, domain+".pub")
}

func (s *FileKeyStore) SaveKey(ctx context.Context, domain string, privateKey *rsa.PrivateKey, selector string, algorithm string) error {
	// Ensure directory exists
	if err := os.MkdirAll(s.basePath, 0700); err != nil {
		return fmt.Errorf("failed to create DKIM key directory: %w", err)
	}

	// Encode private key to PEM
	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	})

	// Write private key with restricted permissions
	keyFile := s.keyPath(domain)
	if err := os.WriteFile(keyFile, privPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	// Encode and write public key
	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	pubFile := s.pubKeyPath(domain)
	if err := os.WriteFile(pubFile, pubPEM, 0644); err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}

	// Update database with key metadata
	if s.db != nil {
		_, err := s.db.ExecContext(ctx, `
			UPDATE domains
			SET dkim_selector = ?,
			    dkim_key_algorithm = ?,
			    dkim_key_created_at = ?,
			    dkim_storage_type = 'file',
			    dkim_key_file = ?,
			    dkim_public_key = ?
			WHERE name = ?
		`, selector, algorithm, time.Now(), keyFile, string(pubPEM), domain)
		if err != nil {
			return fmt.Errorf("failed to update database: %w", err)
		}
	}

	return nil
}

func (s *FileKeyStore) LoadKey(ctx context.Context, domain string) (*rsa.PrivateKey, string, error) {
	// Try to get key file path from database first
	var keyFile, selector string
	if s.db != nil {
		err := s.db.QueryRowContext(ctx,
			"SELECT COALESCE(dkim_key_file, ''), COALESCE(dkim_selector, 'mail') FROM domains WHERE name = ?",
			domain).Scan(&keyFile, &selector)
		if err != nil && err != sql.ErrNoRows {
			return nil, "", fmt.Errorf("failed to query database: %w", err)
		}
	}

	// Fall back to default path if not in database
	if keyFile == "" {
		keyFile = s.keyPath(domain)
		selector = "mail"
	}

	// Read key file
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read key file %s: %w", keyFile, err)
	}

	// Parse PEM
	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, "", fmt.Errorf("failed to decode PEM block")
	}

	// Try PKCS#1 format first
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS#8 format
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse private key: %w", err)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, "", fmt.Errorf("key is not an RSA private key")
		}
	}

	return privateKey, selector, nil
}

func (s *FileKeyStore) DeleteKey(ctx context.Context, domain string) error {
	// Remove key files
	os.Remove(s.keyPath(domain))
	os.Remove(s.pubKeyPath(domain))

	// Clear database metadata
	if s.db != nil {
		_, err := s.db.ExecContext(ctx, `
			UPDATE domains
			SET dkim_private_key = NULL,
			    dkim_public_key = NULL,
			    dkim_key_created_at = NULL,
			    dkim_key_file = NULL
			WHERE name = ?
		`, domain)
		if err != nil {
			return fmt.Errorf("failed to update database: %w", err)
		}
	}

	return nil
}

func (s *FileKeyStore) KeyExists(ctx context.Context, domain string) bool {
	_, err := os.Stat(s.keyPath(domain))
	return err == nil
}

func (s *FileKeyStore) GetPublicKeyDNS(ctx context.Context, domain string) (string, error) {
	// Try to get cached public key from database first
	if s.db != nil {
		var pubKey sql.NullString
		err := s.db.QueryRowContext(ctx,
			"SELECT dkim_public_key FROM domains WHERE name = ?",
			domain).Scan(&pubKey)
		if err == nil && pubKey.Valid && pubKey.String != "" {
			return formatPEMToDNS(pubKey.String), nil
		}
	}

	// Load key and format
	key, _, err := s.LoadKey(ctx, domain)
	if err != nil {
		return "", err
	}

	return FormatDKIMPublicKey(&key.PublicKey)
}

func (s *FileKeyStore) GetKeyMetadata(ctx context.Context, domain string) (*KeyMetadata, error) {
	meta := &KeyMetadata{
		Domain:      domain,
		StorageType: "file",
	}

	if s.db != nil {
		var selector, algorithm, keyFile sql.NullString
		var createdAt sql.NullTime
		err := s.db.QueryRowContext(ctx, `
			SELECT dkim_selector, dkim_key_algorithm, dkim_key_created_at, dkim_key_file
			FROM domains WHERE name = ?
		`, domain).Scan(&selector, &algorithm, &createdAt, &keyFile)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("failed to query database: %w", err)
		}

		if selector.Valid {
			meta.Selector = selector.String
		} else {
			meta.Selector = "mail"
		}
		if algorithm.Valid {
			meta.Algorithm = algorithm.String
		}
		if createdAt.Valid {
			meta.CreatedAt = createdAt.Time
		}
		if keyFile.Valid {
			meta.KeyFile = keyFile.String
		}
	}

	meta.HasKey = s.KeyExists(ctx, domain)
	return meta, nil
}

func (s *FileKeyStore) ListDomains(ctx context.Context) ([]KeyMetadata, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database required for listing domains")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT name, dkim_selector, dkim_key_algorithm, dkim_key_created_at, dkim_key_file, dkim_storage_type
		FROM domains WHERE is_active = TRUE ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query domains: %w", err)
	}
	defer rows.Close()

	var domains []KeyMetadata
	for rows.Next() {
		var meta KeyMetadata
		var selector, algorithm, keyFile, storageType sql.NullString
		var createdAt sql.NullTime

		if err := rows.Scan(&meta.Domain, &selector, &algorithm, &createdAt, &keyFile, &storageType); err != nil {
			continue
		}

		meta.Selector = "mail"
		if selector.Valid && selector.String != "" {
			meta.Selector = selector.String
		}
		if algorithm.Valid {
			meta.Algorithm = algorithm.String
		}
		if createdAt.Valid {
			meta.CreatedAt = createdAt.Time
		}
		if keyFile.Valid {
			meta.KeyFile = keyFile.String
		}
		meta.StorageType = "file"
		if storageType.Valid && storageType.String != "" {
			meta.StorageType = storageType.String
		}

		// Check if key exists based on storage type
		if meta.StorageType == "database" {
			// For database storage, check if dkim_private_key has data
			var keyLen int
			s.db.QueryRowContext(ctx,
				"SELECT LENGTH(COALESCE(dkim_private_key, '')) FROM domains WHERE name = ?",
				meta.Domain).Scan(&keyLen)
			meta.HasKey = keyLen > 0
		} else {
			// For file storage, check if file exists
			meta.HasKey = s.KeyExists(ctx, meta.Domain)
		}

		domains = append(domains, meta)
	}

	return domains, rows.Err()
}

// DatabaseKeyStore implements database-based DKIM key storage
type DatabaseKeyStore struct {
	db *sql.DB
}

// NewDatabaseKeyStore creates a new database-based key store
func NewDatabaseKeyStore(db *sql.DB) *DatabaseKeyStore {
	return &DatabaseKeyStore{db: db}
}

func (s *DatabaseKeyStore) SaveKey(ctx context.Context, domain string, privateKey *rsa.PrivateKey, selector string, algorithm string) error {
	// Encode private key to PEM
	privBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	})

	// Encode public key to PEM
	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubBytes,
	})

	// Store in database
	result, err := s.db.ExecContext(ctx, `
		UPDATE domains
		SET dkim_selector = ?,
		    dkim_private_key = ?,
		    dkim_public_key = ?,
		    dkim_key_algorithm = ?,
		    dkim_key_created_at = ?,
		    dkim_storage_type = 'database',
		    dkim_key_file = NULL
		WHERE name = ?
	`, selector, privPEM, string(pubPEM), algorithm, time.Now(), domain)
	if err != nil {
		return fmt.Errorf("failed to save key to database: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("domain not found: %s", domain)
	}

	return nil
}

func (s *DatabaseKeyStore) LoadKey(ctx context.Context, domain string) (*rsa.PrivateKey, string, error) {
	var privKeyPEM []byte
	var selector string

	err := s.db.QueryRowContext(ctx,
		"SELECT dkim_private_key, COALESCE(dkim_selector, 'mail') FROM domains WHERE name = ?",
		domain).Scan(&privKeyPEM, &selector)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("domain not found: %s", domain)
		}
		return nil, "", fmt.Errorf("failed to query database: %w", err)
	}

	if len(privKeyPEM) == 0 {
		return nil, "", fmt.Errorf("no DKIM key stored for domain: %s", domain)
	}

	// Parse PEM
	block, _ := pem.Decode(privKeyPEM)
	if block == nil {
		return nil, "", fmt.Errorf("failed to decode PEM block")
	}

	// Try PKCS#1 format first
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS#8 format
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, "", fmt.Errorf("failed to parse private key: %w", err)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, "", fmt.Errorf("key is not an RSA private key")
		}
	}

	return privateKey, selector, nil
}

func (s *DatabaseKeyStore) DeleteKey(ctx context.Context, domain string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE domains
		SET dkim_private_key = NULL,
		    dkim_public_key = NULL,
		    dkim_key_created_at = NULL
		WHERE name = ?
	`, domain)
	if err != nil {
		return fmt.Errorf("failed to delete key: %w", err)
	}
	return nil
}

func (s *DatabaseKeyStore) KeyExists(ctx context.Context, domain string) bool {
	var hasKey bool
	err := s.db.QueryRowContext(ctx,
		"SELECT dkim_private_key IS NOT NULL AND LENGTH(dkim_private_key) > 0 FROM domains WHERE name = ?",
		domain).Scan(&hasKey)
	return err == nil && hasKey
}

func (s *DatabaseKeyStore) GetPublicKeyDNS(ctx context.Context, domain string) (string, error) {
	var pubKey sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT dkim_public_key FROM domains WHERE name = ?",
		domain).Scan(&pubKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("domain not found: %s", domain)
		}
		return "", fmt.Errorf("failed to query database: %w", err)
	}

	if !pubKey.Valid || pubKey.String == "" {
		return "", fmt.Errorf("no DKIM public key stored for domain: %s", domain)
	}

	return formatPEMToDNS(pubKey.String), nil
}

func (s *DatabaseKeyStore) GetKeyMetadata(ctx context.Context, domain string) (*KeyMetadata, error) {
	meta := &KeyMetadata{
		Domain:      domain,
		StorageType: "database",
	}

	var selector, algorithm, storageType sql.NullString
	var createdAt sql.NullTime
	var hasKey bool

	err := s.db.QueryRowContext(ctx, `
		SELECT dkim_selector, dkim_key_algorithm, dkim_key_created_at, dkim_storage_type,
		       dkim_private_key IS NOT NULL AND LENGTH(dkim_private_key) > 0
		FROM domains WHERE name = ?
	`, domain).Scan(&selector, &algorithm, &createdAt, &storageType, &hasKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("domain not found: %s", domain)
		}
		return nil, fmt.Errorf("failed to query database: %w", err)
	}

	meta.Selector = "mail"
	if selector.Valid && selector.String != "" {
		meta.Selector = selector.String
	}
	if algorithm.Valid {
		meta.Algorithm = algorithm.String
	}
	if createdAt.Valid {
		meta.CreatedAt = createdAt.Time
	}
	if storageType.Valid {
		meta.StorageType = storageType.String
	}
	meta.HasKey = hasKey

	return meta, nil
}

func (s *DatabaseKeyStore) ListDomains(ctx context.Context) ([]KeyMetadata, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, dkim_selector, dkim_key_algorithm, dkim_key_created_at, dkim_storage_type,
		       dkim_private_key IS NOT NULL AND LENGTH(dkim_private_key) > 0
		FROM domains WHERE is_active = TRUE ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query domains: %w", err)
	}
	defer rows.Close()

	var domains []KeyMetadata
	for rows.Next() {
		var meta KeyMetadata
		var selector, algorithm, storageType sql.NullString
		var createdAt sql.NullTime

		if err := rows.Scan(&meta.Domain, &selector, &algorithm, &createdAt, &storageType, &meta.HasKey); err != nil {
			continue
		}

		meta.Selector = "mail"
		if selector.Valid && selector.String != "" {
			meta.Selector = selector.String
		}
		if algorithm.Valid {
			meta.Algorithm = algorithm.String
		}
		if createdAt.Valid {
			meta.CreatedAt = createdAt.Time
		}
		meta.StorageType = "database"
		if storageType.Valid && storageType.String != "" {
			meta.StorageType = storageType.String
		}

		domains = append(domains, meta)
	}

	return domains, rows.Err()
}

// HybridKeyStore uses both file and database storage for redundancy
type HybridKeyStore struct {
	file     *FileKeyStore
	database *DatabaseKeyStore
}

// NewHybridKeyStore creates a new hybrid key store
func NewHybridKeyStore(basePath string, db *sql.DB) *HybridKeyStore {
	return &HybridKeyStore{
		file:     NewFileKeyStore(basePath, db),
		database: NewDatabaseKeyStore(db),
	}
}

func (s *HybridKeyStore) SaveKey(ctx context.Context, domain string, privateKey *rsa.PrivateKey, selector string, algorithm string) error {
	// Save to both stores
	if err := s.file.SaveKey(ctx, domain, privateKey, selector, algorithm); err != nil {
		return fmt.Errorf("failed to save to file: %w", err)
	}

	if err := s.database.SaveKey(ctx, domain, privateKey, selector, algorithm); err != nil {
		return fmt.Errorf("failed to save to database: %w", err)
	}

	// Update storage type to hybrid
	_, err := s.database.db.ExecContext(ctx,
		"UPDATE domains SET dkim_storage_type = 'hybrid', dkim_key_file = ? WHERE name = ?",
		s.file.keyPath(domain), domain)
	if err != nil {
		return fmt.Errorf("failed to update storage type: %w", err)
	}

	return nil
}

func (s *HybridKeyStore) LoadKey(ctx context.Context, domain string) (*rsa.PrivateKey, string, error) {
	// Try database first (faster)
	key, selector, err := s.database.LoadKey(ctx, domain)
	if err == nil {
		return key, selector, nil
	}

	// Fall back to file
	return s.file.LoadKey(ctx, domain)
}

func (s *HybridKeyStore) DeleteKey(ctx context.Context, domain string) error {
	// Delete from both
	s.file.DeleteKey(ctx, domain)
	return s.database.DeleteKey(ctx, domain)
}

func (s *HybridKeyStore) KeyExists(ctx context.Context, domain string) bool {
	return s.database.KeyExists(ctx, domain) || s.file.KeyExists(ctx, domain)
}

func (s *HybridKeyStore) GetPublicKeyDNS(ctx context.Context, domain string) (string, error) {
	// Try database first
	dns, err := s.database.GetPublicKeyDNS(ctx, domain)
	if err == nil {
		return dns, nil
	}
	return s.file.GetPublicKeyDNS(ctx, domain)
}

func (s *HybridKeyStore) GetKeyMetadata(ctx context.Context, domain string) (*KeyMetadata, error) {
	meta, err := s.database.GetKeyMetadata(ctx, domain)
	if err != nil {
		return s.file.GetKeyMetadata(ctx, domain)
	}
	meta.StorageType = "hybrid"
	return meta, nil
}

func (s *HybridKeyStore) ListDomains(ctx context.Context) ([]KeyMetadata, error) {
	return s.database.ListDomains(ctx)
}

// Helper function to format PEM public key to DNS TXT record format
func formatPEMToDNS(pemKey string) string {
	// Remove PEM headers and newlines
	pubStr := pemKey
	pubStr = strings.ReplaceAll(pubStr, "-----BEGIN PUBLIC KEY-----", "")
	pubStr = strings.ReplaceAll(pubStr, "-----END PUBLIC KEY-----", "")
	pubStr = strings.ReplaceAll(pubStr, "\n", "")
	pubStr = strings.ReplaceAll(pubStr, "\r", "")
	pubStr = strings.TrimSpace(pubStr)

	return fmt.Sprintf("v=DKIM1; k=rsa; p=%s", pubStr)
}

// NewKeyStore creates the appropriate key store based on storage type
func NewKeyStore(storageType string, basePath string, db *sql.DB) DKIMKeyStore {
	switch storageType {
	case "database":
		return NewDatabaseKeyStore(db)
	case "hybrid":
		return NewHybridKeyStore(basePath, db)
	default:
		return NewFileKeyStore(basePath, db)
	}
}
