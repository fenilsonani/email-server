package tuning

import (
	"os"
	"runtime"
	"testing"
	"time"
)

// Critical edge cases for auto-tuning system that adapts to hardware.

// TestAutoTune_ExtremeLowResources tests tuning with minimal hardware.
func TestAutoTune_ExtremeLowResources(t *testing.T) {
	// Simulate a very constrained environment
	cfg := &Config{
		NumCPU:        1,
		TotalMemoryMB: 256, // Very low memory
		Environment:   "production",
	}

	cfg.calculateConnectionLimits()
	cfg.calculateWorkerCounts()
	cfg.calculatePoolSizes()
	cfg.calculateMemoryLimits()

	// Should hit minimum values
	if cfg.SMTPMaxConnections < 100 {
		t.Errorf("SMTP connections too low: %d", cfg.SMTPMaxConnections)
	}
	if cfg.DeliveryWorkers < 4 {
		t.Errorf("Delivery workers too low: %d", cfg.DeliveryWorkers)
	}
	if cfg.DBMaxOpenConns < 10 {
		t.Errorf("DB connections too low: %d", cfg.DBMaxOpenConns)
	}
	// Low memory should reduce connections
	if cfg.SMTPMaxConnections > 200 {
		t.Errorf("SMTP connections should be reduced for low memory: %d", cfg.SMTPMaxConnections)
	}
}

// TestAutoTune_ExtremeHighResources tests tuning with massive hardware.
func TestAutoTune_ExtremeHighResources(t *testing.T) {
	cfg := &Config{
		NumCPU:        128, // Large server
		TotalMemoryMB: 262144, // 256GB RAM
		Environment:   "production",
	}

	cfg.calculateConnectionLimits()
	cfg.calculateWorkerCounts()
	cfg.calculatePoolSizes()
	cfg.calculateMemoryLimits()

	// Should hit maximum caps
	if cfg.SMTPMaxConnections > 10000 {
		t.Errorf("SMTP connections exceed max: %d", cfg.SMTPMaxConnections)
	}
	if cfg.IMAPMaxConnections > 50000 {
		t.Errorf("IMAP connections exceed max: %d", cfg.IMAPMaxConnections)
	}
	if cfg.DeliveryWorkers > 500 {
		t.Errorf("Delivery workers exceed max: %d", cfg.DeliveryWorkers)
	}
	if cfg.MaxMessageSize > 100*1024*1024 {
		t.Errorf("Max message size exceeds cap: %d", cfg.MaxMessageSize)
	}
}

// TestAutoTune_ContainerEnvironment tests container-specific tuning.
func TestAutoTune_ContainerEnvironment(t *testing.T) {
	cfg := &Config{
		NumCPU:        4,
		TotalMemoryMB: 2048,
		Environment:   "container",
	}

	cfg.calculateConnectionLimits()
	cfg.calculateWorkerCounts()

	// Container should be more conservative than production
	prodCfg := &Config{
		NumCPU:        4,
		TotalMemoryMB: 2048,
		Environment:   "production",
	}
	prodCfg.calculateConnectionLimits()

	if cfg.SMTPMaxConnections >= prodCfg.SMTPMaxConnections {
		t.Error("Container should have fewer connections than production")
	}
}

// TestAutoTune_DevelopmentEnvironment tests development-specific tuning.
func TestAutoTune_DevelopmentEnvironment(t *testing.T) {
	cfg := &Config{
		NumCPU:        8,
		TotalMemoryMB: 8192,
		Environment:   "development",
	}

	cfg.calculateConnectionLimits()
	cfg.calculateWorkerCounts()
	cfg.calculatePoolSizes()
	cfg.calculateTimeouts()
	cfg.calculateMemoryLimits()

	// Development should have reduced resources
	if cfg.DeliveryWorkers > 20 {
		t.Errorf("Development should limit delivery workers: %d", cfg.DeliveryWorkers)
	}
	if cfg.ReadTimeout > 2*time.Minute {
		t.Errorf("Development should have shorter timeouts: %v", cfg.ReadTimeout)
	}
	if cfg.MaxMessageSize > 25*1024*1024 {
		t.Errorf("Development should limit message size: %d", cfg.MaxMessageSize)
	}
}

// TestAutoTune_EnvironmentOverrides tests environment variable overrides.
func TestAutoTune_EnvironmentOverrides(t *testing.T) {
	// Set environment variables
	os.Setenv("MAIL_SMTP_MAX_CONN", "999")
	os.Setenv("MAIL_IMAP_MAX_CONN", "888")
	os.Setenv("MAIL_DELIVERY_WORKERS", "77")
	os.Setenv("MAIL_DB_POOL_SIZE", "55")
	os.Setenv("MAIL_REDIS_POOL_SIZE", "44")
	os.Setenv("MAIL_MAX_MESSAGE_SIZE", "12345678")
	defer func() {
		os.Unsetenv("MAIL_SMTP_MAX_CONN")
		os.Unsetenv("MAIL_IMAP_MAX_CONN")
		os.Unsetenv("MAIL_DELIVERY_WORKERS")
		os.Unsetenv("MAIL_DB_POOL_SIZE")
		os.Unsetenv("MAIL_REDIS_POOL_SIZE")
		os.Unsetenv("MAIL_MAX_MESSAGE_SIZE")
	}()

	cfg := AutoTune()
	cfg.ApplyEnvOverrides()

	if cfg.SMTPMaxConnections != 999 {
		t.Errorf("SMTP override failed: %d", cfg.SMTPMaxConnections)
	}
	if cfg.IMAPMaxConnections != 888 {
		t.Errorf("IMAP override failed: %d", cfg.IMAPMaxConnections)
	}
	if cfg.DeliveryWorkers != 77 {
		t.Errorf("Delivery workers override failed: %d", cfg.DeliveryWorkers)
	}
	if cfg.DBMaxOpenConns != 55 {
		t.Errorf("DB pool override failed: %d", cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != 27 { // 55/2
		t.Errorf("DB idle should be half of max: %d", cfg.DBMaxIdleConns)
	}
	if cfg.RedisPoolSize != 44 {
		t.Errorf("Redis pool override failed: %d", cfg.RedisPoolSize)
	}
	if cfg.MaxMessageSize != 12345678 {
		t.Errorf("Max message size override failed: %d", cfg.MaxMessageSize)
	}
}

// TestAutoTune_InvalidEnvironmentOverrides tests invalid override values.
func TestAutoTune_InvalidEnvironmentOverrides(t *testing.T) {
	cfg := AutoTune()
	originalSMTP := cfg.SMTPMaxConnections

	// Set invalid values
	os.Setenv("MAIL_SMTP_MAX_CONN", "not-a-number")
	os.Setenv("MAIL_MAX_MESSAGE_SIZE", "invalid")
	defer func() {
		os.Unsetenv("MAIL_SMTP_MAX_CONN")
		os.Unsetenv("MAIL_MAX_MESSAGE_SIZE")
	}()

	cfg.ApplyEnvOverrides()

	// Should keep original values when override is invalid
	if cfg.SMTPMaxConnections != originalSMTP {
		t.Errorf("Invalid override should not change value: %d", cfg.SMTPMaxConnections)
	}
}

// TestAutoTune_ClampFunction tests the clamp helper function.
func TestAutoTune_ClampFunction(t *testing.T) {
	testCases := []struct {
		value, min, max, expected int
	}{
		{50, 0, 100, 50},    // Within range
		{-10, 0, 100, 0},    // Below min
		{150, 0, 100, 100},  // Above max
		{0, 0, 100, 0},      // At min
		{100, 0, 100, 100},  // At max
		{50, 50, 50, 50},    // Min equals max
		{10, 20, 30, 20},    // Value below range
		{40, 20, 30, 30},    // Value above range
	}

	for _, tc := range testCases {
		result := clamp(tc.value, tc.min, tc.max)
		if result != tc.expected {
			t.Errorf("clamp(%d, %d, %d) = %d, want %d",
				tc.value, tc.min, tc.max, result, tc.expected)
		}
	}
}

// TestAutoTune_PresetConfigurations tests preset server configurations.
func TestAutoTune_PresetConfigurations(t *testing.T) {
	t.Run("SmallServer", func(t *testing.T) {
		cfg := SmallServer()
		if cfg.NumCPU != 2 {
			t.Errorf("SmallServer NumCPU = %d", cfg.NumCPU)
		}
		if cfg.TotalMemoryMB != 512 {
			t.Errorf("SmallServer Memory = %d", cfg.TotalMemoryMB)
		}
		if cfg.MaxMessageSize != 25*1024*1024 {
			t.Errorf("SmallServer MaxMessageSize = %d", cfg.MaxMessageSize)
		}
	})

	t.Run("MediumServer", func(t *testing.T) {
		cfg := MediumServer()
		if cfg.NumCPU != 4 {
			t.Errorf("MediumServer NumCPU = %d", cfg.NumCPU)
		}
		if cfg.TotalMemoryMB != 4096 {
			t.Errorf("MediumServer Memory = %d", cfg.TotalMemoryMB)
		}
		if cfg.MaxMessageSize != 50*1024*1024 {
			t.Errorf("MediumServer MaxMessageSize = %d", cfg.MaxMessageSize)
		}
	})

	t.Run("LargeServer", func(t *testing.T) {
		cfg := LargeServer()
		if cfg.NumCPU != 8 {
			t.Errorf("LargeServer NumCPU = %d", cfg.NumCPU)
		}
		if cfg.TotalMemoryMB != 8192 {
			t.Errorf("LargeServer Memory = %d", cfg.TotalMemoryMB)
		}
		if cfg.MaxMessageSize != 100*1024*1024 {
			t.Errorf("LargeServer MaxMessageSize = %d", cfg.MaxMessageSize)
		}
	})
}

// TestAutoTune_MemoryBasedMessageSize tests message size calculation.
func TestAutoTune_MemoryBasedMessageSize(t *testing.T) {
	testCases := []struct {
		memoryMB    int64
		environment string
		minSize     int64
		maxSize     int64
	}{
		{256, "production", 10 * 1024 * 1024, 15 * 1024 * 1024},    // Low memory
		{1024, "production", 20 * 1024 * 1024, 30 * 1024 * 1024},   // 1GB
		{4096, "production", 50 * 1024 * 1024, 105 * 1024 * 1024},  // 4GB
		{16384, "production", 100 * 1024 * 1024, 105 * 1024 * 1024}, // 16GB (capped)
		{4096, "development", 20 * 1024 * 1024, 30 * 1024 * 1024},  // Dev always 25MB
	}

	for _, tc := range testCases {
		cfg := &Config{
			NumCPU:        4,
			TotalMemoryMB: tc.memoryMB,
			Environment:   tc.environment,
		}
		cfg.calculateMemoryLimits()

		if cfg.MaxMessageSize < tc.minSize || cfg.MaxMessageSize > tc.maxSize {
			t.Errorf("Memory %dMB (%s): MaxMessageSize %d not in [%d, %d]",
				tc.memoryMB, tc.environment, cfg.MaxMessageSize, tc.minSize, tc.maxSize)
		}
	}
}

// TestAutoTune_DetectEnvironment tests environment detection.
func TestAutoTune_DetectEnvironment(t *testing.T) {
	// Test MAIL_ENV override
	os.Setenv("MAIL_ENV", "staging")
	defer os.Unsetenv("MAIL_ENV")

	env := detectEnvironment()
	if env != "staging" {
		t.Errorf("MAIL_ENV override failed: %s", env)
	}

	os.Unsetenv("MAIL_ENV")

	// Test GO_ENV fallback
	os.Setenv("GO_ENV", "testing")
	defer os.Unsetenv("GO_ENV")

	env = detectEnvironment()
	if env != "testing" {
		t.Errorf("GO_ENV fallback failed: %s", env)
	}

	os.Unsetenv("GO_ENV")

	// Test DEBUG flag
	os.Setenv("DEBUG", "1")
	defer os.Unsetenv("DEBUG")

	env = detectEnvironment()
	if env != "development" {
		t.Errorf("DEBUG should set development: %s", env)
	}
}

// TestAutoTune_ActualSystem tests auto-tuning on actual system.
func TestAutoTune_ActualSystem(t *testing.T) {
	cfg := AutoTune()

	// Should detect actual CPU count
	if cfg.NumCPU != runtime.NumCPU() {
		t.Errorf("NumCPU mismatch: got %d, want %d", cfg.NumCPU, runtime.NumCPU())
	}

	// Memory should be reasonable (at least 100MB)
	if cfg.TotalMemoryMB < 100 {
		t.Errorf("Detected memory too low: %d MB", cfg.TotalMemoryMB)
	}

	// All values should be within expected ranges
	if cfg.SMTPMaxConnections < 100 || cfg.SMTPMaxConnections > 10000 {
		t.Errorf("SMTP connections out of range: %d", cfg.SMTPMaxConnections)
	}
	if cfg.IMAPMaxConnections < 500 || cfg.IMAPMaxConnections > 50000 {
		t.Errorf("IMAP connections out of range: %d", cfg.IMAPMaxConnections)
	}
	if cfg.DeliveryWorkers < 4 || cfg.DeliveryWorkers > 500 {
		t.Errorf("Delivery workers out of range: %d", cfg.DeliveryWorkers)
	}
	if cfg.ConnectTimeout != 30*time.Second {
		t.Errorf("Connect timeout unexpected: %v", cfg.ConnectTimeout)
	}
}

// TestAutoTune_PoolSizeRelationships tests pool size invariants.
func TestAutoTune_PoolSizeRelationships(t *testing.T) {
	environments := []string{"production", "development", "container"}

	for _, env := range environments {
		cfg := &Config{
			NumCPU:        8,
			TotalMemoryMB: 4096,
			Environment:   env,
		}
		cfg.calculatePoolSizes()

		// Idle should be <= Max
		if cfg.DBMaxIdleConns > cfg.DBMaxOpenConns {
			t.Errorf("%s: DB idle (%d) > max (%d)", env, cfg.DBMaxIdleConns, cfg.DBMaxOpenConns)
		}
		if cfg.RedisMinIdle > cfg.RedisPoolSize {
			t.Errorf("%s: Redis idle (%d) > pool (%d)", env, cfg.RedisMinIdle, cfg.RedisPoolSize)
		}
	}
}

// TestAutoTune_TimeoutConsistency tests timeout relationships.
func TestAutoTune_TimeoutConsistency(t *testing.T) {
	cfg := AutoTune()

	// Read/Write should be >= Connect
	if cfg.ReadTimeout < cfg.ConnectTimeout {
		t.Errorf("Read timeout (%v) < Connect timeout (%v)", cfg.ReadTimeout, cfg.ConnectTimeout)
	}
	if cfg.WriteTimeout < cfg.ConnectTimeout {
		t.Errorf("Write timeout (%v) < Connect timeout (%v)", cfg.WriteTimeout, cfg.ConnectTimeout)
	}

	// Idle should be > Read/Write
	if cfg.IdleTimeout < cfg.ReadTimeout {
		t.Errorf("Idle timeout (%v) < Read timeout (%v)", cfg.IdleTimeout, cfg.ReadTimeout)
	}
}

// TestAutoTune_ZeroCPU tests handling of zero CPU count.
func TestAutoTune_ZeroCPU(t *testing.T) {
	cfg := &Config{
		NumCPU:        0, // Edge case
		TotalMemoryMB: 1024,
		Environment:   "production",
	}

	// Should not panic
	cfg.calculateConnectionLimits()
	cfg.calculateWorkerCounts()
	cfg.calculatePoolSizes()

	// Should have minimum values
	if cfg.SMTPMaxConnections < 100 {
		t.Errorf("Zero CPU should still have min SMTP connections: %d", cfg.SMTPMaxConnections)
	}
}

// TestAutoTune_NegativeMemory tests handling of negative/zero memory.
func TestAutoTune_NegativeMemory(t *testing.T) {
	cfg := &Config{
		NumCPU:        4,
		TotalMemoryMB: 0, // Edge case
		Environment:   "production",
	}

	// Should not panic
	cfg.calculateConnectionLimits()
	cfg.calculateMemoryLimits()

	// Should have minimum message size
	if cfg.MaxMessageSize < 10*1024*1024 {
		t.Errorf("Zero memory should still have min message size: %d", cfg.MaxMessageSize)
	}
}

// TestAutoTune_ConcurrentAccess tests thread safety of AutoTune.
func TestAutoTune_ConcurrentAccess(t *testing.T) {
	// AutoTune creates new Config each time, should be safe
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			cfg := AutoTune()
			cfg.ApplyEnvOverrides()
			_ = cfg.SMTPMaxConnections // Read values
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestAutoTune_EnvironmentNameVariations tests various environment names.
func TestAutoTune_EnvironmentNameVariations(t *testing.T) {
	variations := []struct {
		env        string
		shouldProd bool // Should behave like production?
	}{
		{"production", true},
		{"prod", false},  // Not recognized, defaults to production
		{"PRODUCTION", false}, // Case sensitive
		{"development", false},
		{"dev", false},
		{"container", false},
		{"staging", false},
		{"", false}, // Will detect as production if no other flags
	}

	for _, v := range variations {
		cfg := &Config{
			NumCPU:        4,
			TotalMemoryMB: 4096,
			Environment:   v.env,
		}
		cfg.calculateConnectionLimits()

		prodCfg := &Config{
			NumCPU:        4,
			TotalMemoryMB: 4096,
			Environment:   "production",
		}
		prodCfg.calculateConnectionLimits()

		isProdLike := cfg.SMTPMaxConnections == prodCfg.SMTPMaxConnections
		if isProdLike != v.shouldProd {
			t.Errorf("Environment %q: isProdLike=%v, want %v (SMTP=%d)",
				v.env, isProdLike, v.shouldProd, cfg.SMTPMaxConnections)
		}
	}
}

// TestGetAvailableMemoryMB tests memory detection fallback.
func TestGetAvailableMemoryMB(t *testing.T) {
	memory := getAvailableMemoryMB()

	// Should return a reasonable value
	if memory < 100 {
		t.Errorf("Detected memory too low: %d MB", memory)
	}

	// Should not exceed reasonable limits (1TB)
	if memory > 1000000 {
		t.Errorf("Detected memory unreasonably high: %d MB", memory)
	}
}

// TestAutoTune_WorkerScaling tests worker count scaling.
func TestAutoTune_WorkerScaling(t *testing.T) {
	cpuCounts := []int{1, 2, 4, 8, 16, 32, 64}

	var lastDelivery, lastVacation int
	for _, cpus := range cpuCounts {
		cfg := &Config{
			NumCPU:        cpus,
			TotalMemoryMB: 8192,
			Environment:   "production",
		}
		cfg.calculateWorkerCounts()

		// Workers should scale with CPU
		if cpus > 1 && cfg.DeliveryWorkers <= lastDelivery {
			t.Errorf("CPU %d: Delivery workers (%d) should increase from %d",
				cpus, cfg.DeliveryWorkers, lastDelivery)
		}

		lastDelivery = cfg.DeliveryWorkers
		lastVacation = cfg.VacationWorkers
		_ = lastVacation // Use to avoid unused variable
	}
}
