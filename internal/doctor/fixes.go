package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/queue"
	"github.com/fenilsonani/email-server/internal/storage/metadata"
)

// ErrFixNotFound is returned when a fix is not found.
var ErrFixNotFound = errors.New("fix not found")

// Fix represents an auto-fix action.
type Fix interface {
	// ID returns the unique identifier for this fix.
	ID() string
	// Description returns a human-readable description.
	Description() string
	// CanAutoFix returns true if this fix can be applied automatically.
	CanAutoFix() bool
	// DryRun returns a description of what would be done without making changes.
	DryRun(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) string
	// Apply applies the fix.
	Apply(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) error
}

// FixMaildirCreate creates the maildir directory.
type FixMaildirCreate struct{}

func (f *FixMaildirCreate) ID() string {
	return "maildir-missing"
}

func (f *FixMaildirCreate) Description() string {
	return "Create missing maildir directory"
}

func (f *FixMaildirCreate) CanAutoFix() bool {
	return true
}

func (f *FixMaildirCreate) DryRun(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) string {
	return fmt.Sprintf("Would create directory: %s with mode 0750", cfg.Storage.MaildirPath)
}

func (f *FixMaildirCreate) Apply(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) error {
	path := cfg.Storage.MaildirPath
	if path == "" {
		return errors.New("maildir path not configured")
	}

	// Create directory with proper permissions
	if err := os.MkdirAll(path, 0750); err != nil {
		return fmt.Errorf("failed to create maildir: %w", err)
	}

	return nil
}

// FixMaildirPermissions fixes maildir directory permissions.
type FixMaildirPermissions struct{}

func (f *FixMaildirPermissions) ID() string {
	return "maildir-permissions"
}

func (f *FixMaildirPermissions) Description() string {
	return "Fix maildir directory permissions"
}

func (f *FixMaildirPermissions) CanAutoFix() bool {
	return true
}

func (f *FixMaildirPermissions) DryRun(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) string {
	return fmt.Sprintf("Would chmod 0750 on: %s", cfg.Storage.MaildirPath)
}

func (f *FixMaildirPermissions) Apply(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) error {
	path := cfg.Storage.MaildirPath
	if path == "" {
		return errors.New("maildir path not configured")
	}

	// Set proper permissions
	if err := os.Chmod(path, 0750); err != nil {
		return fmt.Errorf("failed to chmod maildir: %w", err)
	}

	return nil
}

// FixRunMigrations runs database migrations.
type FixRunMigrations struct{}

func (f *FixRunMigrations) ID() string {
	return "migrations-pending"
}

func (f *FixRunMigrations) Description() string {
	return "Run pending database migrations"
}

func (f *FixRunMigrations) CanAutoFix() bool {
	return true
}

func (f *FixRunMigrations) DryRun(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) string {
	return "Would run database migrations"
}

func (f *FixRunMigrations) Apply(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) error {
	// Open database
	db, err := metadata.OpenFromConfig(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Run migrations
	migrateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := db.Migrate(migrateCtx); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

// FixRecoverStaleQueue recovers stale messages in the queue.
type FixRecoverStaleQueue struct{}

func (f *FixRecoverStaleQueue) ID() string {
	return "queue-stale"
}

func (f *FixRecoverStaleQueue) Description() string {
	return "Recover stale messages stuck in processing"
}

func (f *FixRecoverStaleQueue) CanAutoFix() bool {
	return true
}

func (f *FixRecoverStaleQueue) DryRun(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) string {
	if q == nil {
		return "Queue not available"
	}

	processing, err := q.ProcessingCount(ctx)
	if err != nil {
		return fmt.Sprintf("Cannot check processing count: %v", err)
	}

	return fmt.Sprintf("Would check %d messages in processing and recover those older than 1 hour", processing)
}

func (f *FixRecoverStaleQueue) Apply(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) error {
	if q == nil {
		return errors.New("queue not available")
	}

	staleThreshold := time.Hour
	recovered, err := q.RecoverStale(ctx, staleThreshold)
	if err != nil {
		return fmt.Errorf("failed to recover stale messages: %w", err)
	}

	if recovered > 0 {
		fmt.Printf("Recovered %d stale messages\n", recovered)
	}

	return nil
}

// FixGenerateDKIM generates a DKIM key for a domain.
type FixGenerateDKIM struct {
	Domain string
}

func (f *FixGenerateDKIM) ID() string {
	return fmt.Sprintf("dkim-missing-%s", f.Domain)
}

func (f *FixGenerateDKIM) Description() string {
	return fmt.Sprintf("Generate DKIM key for %s", f.Domain)
}

func (f *FixGenerateDKIM) CanAutoFix() bool {
	// Requires confirmation due to DNS changes needed
	return false
}

func (f *FixGenerateDKIM) DryRun(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) string {
	return fmt.Sprintf("Would generate 2048-bit DKIM key for domain: %s\nNote: You will need to add a DNS TXT record after generation.", f.Domain)
}

func (f *FixGenerateDKIM) Apply(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) error {
	// This fix requires manual intervention
	return fmt.Errorf("run: mailserver dkim generate --domain %s", f.Domain)
}

// FixCertRenewal provides guidance for certificate renewal.
type FixCertRenewal struct{}

func (f *FixCertRenewal) ID() string {
	return "cert-expired"
}

func (f *FixCertRenewal) Description() string {
	return "Renew expired TLS certificate"
}

func (f *FixCertRenewal) CanAutoFix() bool {
	// Cannot auto-fix certificate renewal
	return false
}

func (f *FixCertRenewal) DryRun(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) string {
	if cfg.TLS.AutoTLS {
		return "Would trigger Let's Encrypt certificate renewal"
	}
	return "Manual certificate renewal required. Run: certbot renew"
}

func (f *FixCertRenewal) Apply(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) error {
	if cfg.TLS.AutoTLS {
		return errors.New("restart the server to trigger automatic certificate renewal")
	}
	return errors.New("run: certbot renew (or your certificate provider's renewal command)")
}

// FixCreateDataDir creates the data directory.
type FixCreateDataDir struct{}

func (f *FixCreateDataDir) ID() string {
	return "datadir-missing"
}

func (f *FixCreateDataDir) Description() string {
	return "Create missing data directory"
}

func (f *FixCreateDataDir) CanAutoFix() bool {
	return true
}

func (f *FixCreateDataDir) DryRun(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) string {
	return fmt.Sprintf("Would create directory: %s with mode 0750", cfg.Storage.DataDir)
}

func (f *FixCreateDataDir) Apply(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) error {
	path := cfg.Storage.DataDir
	if path == "" {
		return errors.New("data directory path not configured")
	}

	// Create directory with proper permissions
	if err := os.MkdirAll(path, 0750); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	return nil
}

// FixCleanupQueue cleans up old messages from the queue.
type FixCleanupQueue struct {
	OlderThan time.Duration
}

func (f *FixCleanupQueue) ID() string {
	return "queue-cleanup"
}

func (f *FixCleanupQueue) Description() string {
	return "Clean up old sent/failed messages from queue"
}

func (f *FixCleanupQueue) CanAutoFix() bool {
	return true
}

func (f *FixCleanupQueue) DryRun(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) string {
	if q == nil {
		return "Queue not available"
	}

	olderThan := f.OlderThan
	if olderThan == 0 {
		olderThan = 7 * 24 * time.Hour // Default: 7 days
	}

	return fmt.Sprintf("Would remove sent/failed messages older than %s", olderThan)
}

func (f *FixCleanupQueue) Apply(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) error {
	if q == nil {
		return errors.New("queue not available")
	}

	olderThan := f.OlderThan
	if olderThan == 0 {
		olderThan = 7 * 24 * time.Hour
	}

	return q.Cleanup(ctx, olderThan)
}

// FixRestartRedis restarts the Redis service.
type FixRestartRedis struct{}

func (f *FixRestartRedis) ID() string {
	return "redis-restart"
}

func (f *FixRestartRedis) Description() string {
	return "Restart Redis service"
}

func (f *FixRestartRedis) CanAutoFix() bool {
	return true
}

func (f *FixRestartRedis) DryRun(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) string {
	return "Would run: systemctl restart redis"
}

func (f *FixRestartRedis) Apply(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) error {
	cmd := exec.CommandContext(ctx, "systemctl", "restart", "redis")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restart redis: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// FixStartRedis starts the Redis service if stopped.
type FixStartRedis struct{}

func (f *FixStartRedis) ID() string {
	return "redis-down"
}

func (f *FixStartRedis) Description() string {
	return "Start Redis service"
}

func (f *FixStartRedis) CanAutoFix() bool {
	return true
}

func (f *FixStartRedis) DryRun(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) string {
	return "Would run: systemctl start redis"
}

func (f *FixStartRedis) Apply(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) error {
	cmd := exec.CommandContext(ctx, "systemctl", "start", "redis")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start redis: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// FixDatabasePermissions fixes database file permissions.
type FixDatabasePermissions struct{}

func (f *FixDatabasePermissions) ID() string {
	return "db-permissions"
}

func (f *FixDatabasePermissions) Description() string {
	return "Fix database file permissions to 0640"
}

func (f *FixDatabasePermissions) CanAutoFix() bool {
	return true
}

func (f *FixDatabasePermissions) DryRun(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) string {
	return fmt.Sprintf("Would chmod 0640 on: %s, %s-wal, %s-shm", cfg.Storage.DatabasePath, cfg.Storage.DatabasePath, cfg.Storage.DatabasePath)
}

func (f *FixDatabasePermissions) Apply(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) error {
	dbPath := cfg.Storage.DatabasePath
	if dbPath == "" {
		return errors.New("database path not configured")
	}

	files := []string{dbPath, dbPath + "-wal", dbPath + "-shm"}
	for _, path := range files {
		if _, err := os.Stat(path); err == nil {
			if err := os.Chmod(path, 0640); err != nil {
				return fmt.Errorf("failed to chmod %s: %w", path, err)
			}
		}
	}

	return nil
}

// FixRestartMailserver restarts the mail server service.
type FixRestartMailserver struct{}

func (f *FixRestartMailserver) ID() string {
	return "mailserver-restart"
}

func (f *FixRestartMailserver) Description() string {
	return "Restart mail server service"
}

func (f *FixRestartMailserver) CanAutoFix() bool {
	return true
}

func (f *FixRestartMailserver) DryRun(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) string {
	return "Would run: systemctl restart mailserver"
}

func (f *FixRestartMailserver) Apply(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) error {
	cmd := exec.CommandContext(ctx, "systemctl", "restart", "mailserver")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restart mailserver: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// FixStartMailserver starts the mail server service if stopped.
type FixStartMailserver struct{}

func (f *FixStartMailserver) ID() string {
	return "mailserver-down"
}

func (f *FixStartMailserver) Description() string {
	return "Start mail server service"
}

func (f *FixStartMailserver) CanAutoFix() bool {
	return true
}

func (f *FixStartMailserver) DryRun(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) string {
	return "Would run: systemctl start mailserver"
}

func (f *FixStartMailserver) Apply(ctx context.Context, cfg *config.Config, _ *queue.RedisQueue) error {
	cmd := exec.CommandContext(ctx, "systemctl", "start", "mailserver")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start mailserver: %w\nOutput: %s", err, string(output))
	}
	return nil
}
