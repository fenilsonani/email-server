package doctor

import (
	"context"
	"testing"
	"time"

	"github.com/fenilsonani/email-server/internal/config"
	"github.com/fenilsonani/email-server/internal/queue"
)

func TestNewDoctor(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	if d == nil {
		t.Fatal("New() returned nil")
	}

	if d.cfg != cfg {
		t.Error("Doctor config not set correctly")
	}

	// Check that default checks are registered
	if len(d.checks) == 0 {
		t.Error("No default checks registered")
	}

	// Check that default fixes are registered
	if len(d.fixes) == 0 {
		t.Error("No default fixes registered")
	}
}

func TestDoctorRun(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := d.Run(ctx)

	if results == nil {
		t.Fatal("Run() returned nil")
	}

	if len(results.Checks) == 0 {
		t.Error("No checks were run")
	}

	// Verify counts add up
	total := results.Passed + results.Failed + results.Warned
	if total != len(results.Checks) {
		t.Errorf("Count mismatch: passed(%d) + failed(%d) + warned(%d) = %d, but got %d checks",
			results.Passed, results.Failed, results.Warned, total, len(results.Checks))
	}

	// Verify healthy flag consistency
	if results.Failed == 0 && !results.Healthy {
		t.Error("Results should be healthy when no failures")
	}
	if results.Failed > 0 && results.Healthy {
		t.Error("Results should not be healthy when failures exist")
	}

	// Verify duration is set
	if results.Duration == 0 {
		t.Error("Duration should be set")
	}
}

func TestDoctorRunCategory(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test each category
	categories := []Category{
		CategoryInfra,
		CategoryNetwork,
		CategorySecurity,
		CategoryDNS,
		CategoryConfig,
		CategoryQueue,
	}

	for _, cat := range categories {
		t.Run(string(cat), func(t *testing.T) {
			results := d.RunCategory(ctx, cat)

			if results == nil {
				t.Fatal("RunCategory() returned nil")
			}

			// All checks should be of the requested category
			for _, check := range results.Checks {
				if check.Category != cat {
					t.Errorf("Check %s has category %s, expected %s",
						check.ID, check.Category, cat)
				}
			}
		})
	}
}

func TestDoctorRunCategoryEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	ctx := context.Background()

	// Run with a category that doesn't exist
	results := d.RunCategory(ctx, Category("nonexistent"))

	if results == nil {
		t.Fatal("RunCategory() returned nil for nonexistent category")
	}

	if len(results.Checks) != 0 {
		t.Error("Should have no checks for nonexistent category")
	}

	if !results.Healthy {
		t.Error("Empty results should be healthy")
	}
}

func TestDoctorRegisterCheck(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	customCheck := func(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) CheckResult {
		return CheckResult{
			ID:       "custom-check",
			Name:     "Custom Check",
			Category: CategoryInfra,
			Status:   StatusPass,
			Message:  "Custom check passed",
		}
	}

	initialCount := len(d.checks[CategoryInfra])
	d.RegisterCheck(CategoryInfra, customCheck)

	if len(d.checks[CategoryInfra]) != initialCount+1 {
		t.Error("Check was not registered")
	}
}

func TestDoctorRegisterFix(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	customFix := &FixMaildirCreate{}
	initialCount := len(d.fixes)

	// Re-registering should overwrite
	d.RegisterFix(customFix)

	if len(d.fixes) < initialCount {
		t.Error("Fix was removed instead of registered")
	}
}

func TestDoctorGetFix(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	// Test getting existing fix
	fix, ok := d.GetFix("maildir-missing")
	if !ok {
		t.Error("Should find maildir-missing fix")
	}
	if fix == nil {
		t.Error("Fix should not be nil")
	}

	// Test getting non-existent fix
	_, ok = d.GetFix("nonexistent-fix")
	if ok {
		t.Error("Should not find nonexistent fix")
	}
}

func TestDoctorListFixes(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	fixes := d.ListFixes()

	if len(fixes) == 0 {
		t.Error("Should have default fixes")
	}

	// Verify each fix has required methods
	for _, fix := range fixes {
		if fix.ID() == "" {
			t.Error("Fix ID should not be empty")
		}
		if fix.Description() == "" {
			t.Error("Fix description should not be empty")
		}
	}
}

func TestDoctorApplyFix(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	ctx := context.Background()

	// Test dry run
	msg, err := d.ApplyFix(ctx, "maildir-missing", true)
	if err != nil {
		t.Errorf("Dry run should not fail: %v", err)
	}
	if msg == "" {
		t.Error("Dry run should return a message")
	}

	// Test non-existent fix
	_, err = d.ApplyFix(ctx, "nonexistent-fix", true)
	if err != ErrFixNotFound {
		t.Errorf("Expected ErrFixNotFound, got: %v", err)
	}
}

func TestCheckResultCategories(t *testing.T) {
	// Verify all categories are valid
	categories := []Category{
		CategoryInfra,
		CategoryNetwork,
		CategorySecurity,
		CategoryDNS,
		CategoryConfig,
		CategoryQueue,
	}

	for _, cat := range categories {
		if cat == "" {
			t.Error("Category should not be empty")
		}
	}
}

func TestCheckResultStatuses(t *testing.T) {
	// Verify all statuses are valid
	statuses := []Status{
		StatusPass,
		StatusFail,
		StatusWarn,
	}

	for _, status := range statuses {
		if status == "" {
			t.Error("Status should not be empty")
		}
	}
}

func TestResultsFixableIDs(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	// Add a custom check that returns a fixable issue
	d.RegisterCheck(CategoryInfra, func(ctx context.Context, cfg *config.Config, q *queue.RedisQueue) CheckResult {
		return CheckResult{
			ID:       "test-fixable",
			Name:     "Test Fixable",
			Category: CategoryInfra,
			Status:   StatusFail,
			Message:  "Test failure",
			FixID:    "maildir-missing",
		}
	})

	ctx := context.Background()
	results := d.RunCategory(ctx, CategoryInfra)

	// Should have at least one fixable ID
	hasTestFix := false
	for _, id := range results.FixableIDs {
		if id == "maildir-missing" {
			hasTestFix = true
			break
		}
	}

	if !hasTestFix {
		t.Error("Should include fixable ID from failing check")
	}
}

func TestDoctorContextCancellation(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	// Create already cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should still return results (checks handle context internally)
	results := d.Run(ctx)

	if results == nil {
		t.Fatal("Run() should return results even with cancelled context")
	}
}

func TestApplyAllFixable(t *testing.T) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)

	ctx := context.Background()

	// Create results with fixable IDs
	results := &Results{
		FixableIDs: []string{"maildir-missing", "nonexistent-fix"},
	}

	// Dry run
	fixResults, err := d.ApplyAllFixable(ctx, results, true)
	if err != nil {
		t.Errorf("ApplyAllFixable dry run failed: %v", err)
	}

	if len(fixResults) == 0 {
		t.Error("Should have fix results")
	}

	// Check that valid fix was processed
	foundValid := false
	for _, fr := range fixResults {
		if fr.FixID == "maildir-missing" {
			foundValid = true
			if !fr.DryRun {
				t.Error("Should be marked as dry run")
			}
		}
	}

	if !foundValid {
		t.Error("Should include result for valid fix")
	}
}

// Benchmark tests

func BenchmarkDoctorRun(b *testing.B) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Run(ctx)
	}
}

func BenchmarkDoctorRunCategory(b *testing.B) {
	cfg := config.DefaultConfig()
	d := New(cfg, nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.RunCategory(ctx, CategoryInfra)
	}
}
