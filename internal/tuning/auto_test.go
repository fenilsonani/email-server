package tuning

import (
	"os"
	"testing"
	"time"
)

func TestAutoTune(t *testing.T) {
	cfg := AutoTune()

	// Verify all fields are populated with reasonable values
	if cfg.NumCPU <= 0 {
		t.Errorf("NumCPU should be positive, got %d", cfg.NumCPU)
	}

	if cfg.TotalMemoryMB <= 0 {
		t.Errorf("TotalMemoryMB should be positive, got %d", cfg.TotalMemoryMB)
	}

	if cfg.Environment == "" {
		t.Error("Environment should not be empty")
	}

	// Connection limits should be reasonable
	if cfg.SMTPMaxConnections < 100 {
		t.Errorf("SMTPMaxConnections too low: %d", cfg.SMTPMaxConnections)
	}

	if cfg.IMAPMaxConnections < 500 {
		t.Errorf("IMAPMaxConnections too low: %d", cfg.IMAPMaxConnections)
	}

	if cfg.MaxConnectionsPerIP < 10 {
		t.Errorf("MaxConnectionsPerIP too low: %d", cfg.MaxConnectionsPerIP)
	}

	// Worker counts should be reasonable
	if cfg.DeliveryWorkers < 2 {
		t.Errorf("DeliveryWorkers too low: %d", cfg.DeliveryWorkers)
	}

	if cfg.VacationWorkers < 1 {
		t.Errorf("VacationWorkers too low: %d", cfg.VacationWorkers)
	}

	// Pool sizes should be reasonable
	if cfg.DBMaxOpenConns < 5 {
		t.Errorf("DBMaxOpenConns too low: %d", cfg.DBMaxOpenConns)
	}

	if cfg.DBMaxIdleConns < 2 {
		t.Errorf("DBMaxIdleConns too low: %d", cfg.DBMaxIdleConns)
	}

	if cfg.RedisPoolSize < 5 {
		t.Errorf("RedisPoolSize too low: %d", cfg.RedisPoolSize)
	}

	// Timeouts should be set
	if cfg.ConnectTimeout == 0 {
		t.Error("ConnectTimeout should be set")
	}

	if cfg.ReadTimeout == 0 {
		t.Error("ReadTimeout should be set")
	}

	if cfg.WriteTimeout == 0 {
		t.Error("WriteTimeout should be set")
	}

	// Memory limits
	if cfg.MaxMessageSize < 10*1024*1024 {
		t.Errorf("MaxMessageSize too low: %d", cfg.MaxMessageSize)
	}

	t.Logf("Auto-tuned config: CPUs=%d, Memory=%dMB, Env=%s",
		cfg.NumCPU, cfg.TotalMemoryMB, cfg.Environment)
	t.Logf("  SMTP=%d, IMAP=%d, Delivery=%d workers",
		cfg.SMTPMaxConnections, cfg.IMAPMaxConnections, cfg.DeliveryWorkers)
}

func TestAutoTune_Consistency(t *testing.T) {
	// Run AutoTune multiple times and verify consistency
	cfg1 := AutoTune()
	cfg2 := AutoTune()

	if cfg1.NumCPU != cfg2.NumCPU {
		t.Error("NumCPU should be consistent across calls")
	}

	if cfg1.SMTPMaxConnections != cfg2.SMTPMaxConnections {
		t.Error("SMTPMaxConnections should be consistent")
	}

	if cfg1.DeliveryWorkers != cfg2.DeliveryWorkers {
		t.Error("DeliveryWorkers should be consistent")
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	// Set environment variables
	os.Setenv("MAIL_DELIVERY_WORKERS", "42")
	os.Setenv("MAIL_DB_POOL_SIZE", "100")
	os.Setenv("MAIL_MAX_MESSAGE_SIZE", "52428800") // 50MB
	defer func() {
		os.Unsetenv("MAIL_DELIVERY_WORKERS")
		os.Unsetenv("MAIL_DB_POOL_SIZE")
		os.Unsetenv("MAIL_MAX_MESSAGE_SIZE")
	}()

	cfg := AutoTune()
	cfg.ApplyEnvOverrides()

	if cfg.DeliveryWorkers != 42 {
		t.Errorf("Expected DeliveryWorkers=42, got %d", cfg.DeliveryWorkers)
	}

	if cfg.DBMaxOpenConns != 100 {
		t.Errorf("Expected DBMaxOpenConns=100, got %d", cfg.DBMaxOpenConns)
	}

	if cfg.MaxMessageSize != 52428800 {
		t.Errorf("Expected MaxMessageSize=52428800, got %d", cfg.MaxMessageSize)
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		value, min, max, expected int
	}{
		{50, 10, 100, 50},  // Within range
		{5, 10, 100, 10},   // Below min
		{150, 10, 100, 100}, // Above max
		{10, 10, 100, 10},  // At min
		{100, 10, 100, 100}, // At max
	}

	for _, tt := range tests {
		result := clamp(tt.value, tt.min, tt.max)
		if result != tt.expected {
			t.Errorf("clamp(%d, %d, %d) = %d, expected %d",
				tt.value, tt.min, tt.max, result, tt.expected)
		}
	}
}

func TestSmallServer(t *testing.T) {
	cfg := SmallServer()

	if cfg.SMTPMaxConnections > 500 {
		t.Errorf("SmallServer SMTP connections too high: %d", cfg.SMTPMaxConnections)
	}

	if cfg.DeliveryWorkers > 16 {
		t.Errorf("SmallServer delivery workers too high: %d", cfg.DeliveryWorkers)
	}

	if cfg.MaxMessageSize > 30*1024*1024 {
		t.Errorf("SmallServer max message size too high: %d", cfg.MaxMessageSize)
	}
}

func TestMediumServer(t *testing.T) {
	cfg := MediumServer()

	if cfg.SMTPMaxConnections < 500 || cfg.SMTPMaxConnections > 2000 {
		t.Errorf("MediumServer SMTP connections out of range: %d", cfg.SMTPMaxConnections)
	}

	if cfg.DeliveryWorkers < 8 || cfg.DeliveryWorkers > 32 {
		t.Errorf("MediumServer delivery workers out of range: %d", cfg.DeliveryWorkers)
	}
}

func TestLargeServer(t *testing.T) {
	cfg := LargeServer()

	if cfg.SMTPMaxConnections < 2000 {
		t.Errorf("LargeServer SMTP connections too low: %d", cfg.SMTPMaxConnections)
	}

	if cfg.DeliveryWorkers < 32 {
		t.Errorf("LargeServer delivery workers too low: %d", cfg.DeliveryWorkers)
	}

	if cfg.MaxMessageSize < 50*1024*1024 {
		t.Errorf("LargeServer max message size too low: %d", cfg.MaxMessageSize)
	}
}

func TestDetectEnvironment(t *testing.T) {
	// Save original env
	origDebug := os.Getenv("DEBUG")
	origMailEnv := os.Getenv("MAIL_ENV")
	defer func() {
		if origDebug != "" {
			os.Setenv("DEBUG", origDebug)
		} else {
			os.Unsetenv("DEBUG")
		}
		if origMailEnv != "" {
			os.Setenv("MAIL_ENV", origMailEnv)
		} else {
			os.Unsetenv("MAIL_ENV")
		}
	}()

	// Test with explicit env
	os.Setenv("MAIL_ENV", "staging")
	os.Unsetenv("DEBUG")
	env := detectEnvironment()
	if env != "staging" {
		t.Errorf("Expected 'staging', got '%s'", env)
	}

	// Test with DEBUG
	os.Unsetenv("MAIL_ENV")
	os.Setenv("DEBUG", "1")
	env = detectEnvironment()
	if env != "development" {
		t.Errorf("Expected 'development' with DEBUG set, got '%s'", env)
	}
}

func TestTimeoutValues(t *testing.T) {
	cfg := AutoTune()

	// Timeouts should be reasonable
	if cfg.ConnectTimeout < 10*time.Second || cfg.ConnectTimeout > 60*time.Second {
		t.Errorf("ConnectTimeout out of range: %v", cfg.ConnectTimeout)
	}

	if cfg.ReadTimeout < 1*time.Minute || cfg.ReadTimeout > 10*time.Minute {
		t.Errorf("ReadTimeout out of range: %v", cfg.ReadTimeout)
	}

	if cfg.ShutdownTimeout < 10*time.Second || cfg.ShutdownTimeout > 60*time.Second {
		t.Errorf("ShutdownTimeout out of range: %v", cfg.ShutdownTimeout)
	}
}

func TestPoolSizeRelationships(t *testing.T) {
	cfg := AutoTune()

	// Idle connections should be less than or equal to max connections
	if cfg.DBMaxIdleConns > cfg.DBMaxOpenConns {
		t.Errorf("DBMaxIdleConns (%d) > DBMaxOpenConns (%d)",
			cfg.DBMaxIdleConns, cfg.DBMaxOpenConns)
	}

	if cfg.RedisMinIdle > cfg.RedisPoolSize {
		t.Errorf("RedisMinIdle (%d) > RedisPoolSize (%d)",
			cfg.RedisMinIdle, cfg.RedisPoolSize)
	}
}
