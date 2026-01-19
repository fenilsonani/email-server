// Package doctor provides comprehensive diagnostic tools for the mail server.
package doctor

import (
	"context"
	"sync"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/queue"
)

// Status represents the result status of a health check.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusWarn Status = "warn"
)

// Category represents the type/category of a health check.
type Category string

const (
	CategoryInfra    Category = "infrastructure"
	CategoryNetwork  Category = "network"
	CategorySecurity Category = "security"
	CategoryDNS      Category = "dns"
	CategoryConfig   Category = "config"
	CategoryQueue    Category = "queue"
)

// CheckResult represents the result of a single health check.
type CheckResult struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Category Category               `json:"category"`
	Status   Status                 `json:"status"`
	Message  string                 `json:"message"`
	Details  map[string]interface{} `json:"details,omitempty"`
	Help     string                 `json:"help,omitempty"`
	FixID    string                 `json:"fix_id,omitempty"` // Links to auto-fix action
	Duration time.Duration          `json:"duration"`
}

// Results contains all doctor check results.
type Results struct {
	Checks     []CheckResult `json:"checks"`
	Passed     int           `json:"passed"`
	Failed     int           `json:"failed"`
	Warned     int           `json:"warned"`
	Healthy    bool          `json:"healthy"`
	Duration   time.Duration `json:"duration"`
	StartTime  time.Time     `json:"start_time"`
	FixableIDs []string      `json:"fixable_ids,omitempty"`
}

// Check is a function type that performs a health check.
type Check func(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) CheckResult

// Doctor orchestrates health checks for the mail server.
type Doctor struct {
	cfg    *config.Config
	queue  *queue.RedisQueue
	checks map[Category][]Check
	fixes  map[string]Fix
	mu     sync.RWMutex
}

// New creates a new Doctor instance.
func New(cfg *config.Config, q *queue.RedisQueue) *Doctor {
	d := &Doctor{
		cfg:    cfg,
		queue:  q,
		checks: make(map[Category][]Check),
		fixes:  make(map[string]Fix),
	}
	d.registerDefaultChecks()
	d.registerDefaultFixes()
	return d
}

// registerDefaultChecks adds all built-in health checks.
func (d *Doctor) registerDefaultChecks() {
	// Infrastructure checks
	d.RegisterCheck(CategoryInfra, checkDatabaseDriver)
	d.RegisterCheck(CategoryInfra, checkDiskSpace)
	d.RegisterCheck(CategoryInfra, checkMaildirPermissions)
	d.RegisterCheck(CategoryInfra, checkMemoryUsage)

	// Network checks
	d.RegisterCheck(CategoryNetwork, checkServiceRunning)
	d.RegisterCheck(CategoryNetwork, checkHealthEndpoint)
	d.RegisterCheck(CategoryNetwork, checkRedisConnection)
	d.RegisterCheck(CategoryNetwork, checkOutboundSMTP)

	// Security checks
	d.RegisterCheck(CategorySecurity, checkTLSCertificates)
	d.RegisterCheck(CategorySecurity, checkCertExpiry)
	d.RegisterCheck(CategorySecurity, checkAllDomainsDKIM)

	// DNS checks
	d.RegisterCheck(CategoryDNS, checkAllDomainsDNS)

	// Config checks
	d.RegisterCheck(CategoryConfig, checkConfigPorts)

	// Queue checks
	d.RegisterCheck(CategoryQueue, checkQueueHealth)
	d.RegisterCheck(CategoryQueue, checkQueueStale)
}

// registerDefaultFixes adds all built-in auto-fix implementations.
func (d *Doctor) registerDefaultFixes() {
	d.RegisterFix(&FixMaildirCreate{})
	d.RegisterFix(&FixMaildirPermissions{})
	d.RegisterFix(&FixRunMigrations{})
	d.RegisterFix(&FixRecoverStaleQueue{})
	d.RegisterFix(&FixRestartRedis{})
	d.RegisterFix(&FixStartRedis{})
	d.RegisterFix(&FixRestartMailserver{})
	d.RegisterFix(&FixStartMailserver{})
}

// RegisterCheck adds a health check for a specific category.
func (d *Doctor) RegisterCheck(category Category, check Check) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.checks[category] = append(d.checks[category], check)
}

// RegisterFix adds an auto-fix implementation.
func (d *Doctor) RegisterFix(fix Fix) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fixes[fix.ID()] = fix
}

// Run executes all health checks.
func (d *Doctor) Run(ctx context.Context) *Results {
	startTime := time.Now()
	results := &Results{
		StartTime: startTime,
		Checks:    make([]CheckResult, 0),
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	// Run checks in a specific category order for better output
	categoryOrder := []Category{
		CategoryInfra,
		CategoryNetwork,
		CategorySecurity,
		CategoryDNS,
		CategoryConfig,
		CategoryQueue,
	}

	for _, category := range categoryOrder {
		checks := d.checks[category]
		for _, check := range checks {
			checkStart := time.Now()
			result := check(ctx, d.cfg, d.queue)
			result.Duration = time.Since(checkStart)
			results.Checks = append(results.Checks, result)

			switch result.Status {
			case StatusPass:
				results.Passed++
			case StatusFail:
				results.Failed++
			case StatusWarn:
				results.Warned++
			}

			if result.FixID != "" {
				results.FixableIDs = append(results.FixableIDs, result.FixID)
			}
		}
	}

	results.Healthy = results.Failed == 0
	results.Duration = time.Since(startTime)
	return results
}

// RunCategory executes health checks for a specific category.
func (d *Doctor) RunCategory(ctx context.Context, category Category) *Results {
	startTime := time.Now()
	results := &Results{
		StartTime: startTime,
		Checks:    make([]CheckResult, 0),
	}

	d.mu.RLock()
	checks := d.checks[category]
	d.mu.RUnlock()

	for _, check := range checks {
		checkStart := time.Now()
		result := check(ctx, d.cfg, d.queue)
		result.Duration = time.Since(checkStart)
		results.Checks = append(results.Checks, result)

		switch result.Status {
		case StatusPass:
			results.Passed++
		case StatusFail:
			results.Failed++
		case StatusWarn:
			results.Warned++
		}

		if result.FixID != "" {
			results.FixableIDs = append(results.FixableIDs, result.FixID)
		}
	}

	results.Healthy = results.Failed == 0
	results.Duration = time.Since(startTime)
	return results
}

// GetFix returns a fix by ID.
func (d *Doctor) GetFix(id string) (Fix, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	fix, ok := d.fixes[id]
	return fix, ok
}

// ListFixes returns all available fixes.
func (d *Doctor) ListFixes() []Fix {
	d.mu.RLock()
	defer d.mu.RUnlock()
	fixes := make([]Fix, 0, len(d.fixes))
	for _, fix := range d.fixes {
		fixes = append(fixes, fix)
	}
	return fixes
}

// ApplyFix applies a fix by ID.
func (d *Doctor) ApplyFix(ctx context.Context, fixID string, dryRun bool) (string, error) {
	d.mu.RLock()
	fix, ok := d.fixes[fixID]
	d.mu.RUnlock()

	if !ok {
		return "", ErrFixNotFound
	}

	if dryRun {
		return fix.DryRun(ctx, d.cfg, d.queue), nil
	}

	return "", fix.Apply(ctx, d.cfg, d.queue)
}

// ApplyAllFixable applies all fixes for issues found in the results.
func (d *Doctor) ApplyAllFixable(ctx context.Context, results *Results, dryRun bool) ([]FixResult, error) {
	fixResults := make([]FixResult, 0, len(results.FixableIDs))

	for _, fixID := range results.FixableIDs {
		fix, ok := d.GetFix(fixID)
		if !ok {
			continue
		}

		if !fix.CanAutoFix() {
			fixResults = append(fixResults, FixResult{
				FixID:       fixID,
				Description: fix.Description(),
				Success:     false,
				Message:     "Manual fix required",
			})
			continue
		}

		var message string
		var err error

		if dryRun {
			message = fix.DryRun(ctx, d.cfg, d.queue)
			fixResults = append(fixResults, FixResult{
				FixID:       fixID,
				Description: fix.Description(),
				Success:     true,
				Message:     message,
				DryRun:      true,
			})
		} else {
			err = fix.Apply(ctx, d.cfg, d.queue)
			if err != nil {
				message = err.Error()
			} else {
				message = "Fix applied successfully"
			}
			fixResults = append(fixResults, FixResult{
				FixID:       fixID,
				Description: fix.Description(),
				Success:     err == nil,
				Message:     message,
			})
		}
	}

	return fixResults, nil
}

// FixResult represents the result of applying a fix.
type FixResult struct {
	FixID       string `json:"fix_id"`
	Description string `json:"description"`
	Success     bool   `json:"success"`
	Message     string `json:"message"`
	DryRun      bool   `json:"dry_run,omitempty"`
}
