// Package tuning provides automatic performance tuning based on system resources.
package tuning

import (
	"os"
	"runtime"
	"strconv"
	"time"
)

// Config holds auto-tuned performance configuration.
// All values are automatically calculated if not explicitly set.
type Config struct {
	// Connection limits
	SMTPMaxConnections int `json:"smtp_max_connections"`
	IMAPMaxConnections int `json:"imap_max_connections"`
	MaxConnectionsPerIP int `json:"max_connections_per_ip"`

	// Worker counts
	DeliveryWorkers    int `json:"delivery_workers"`
	VacationWorkers    int `json:"vacation_workers"`

	// Pool sizes
	DBMaxOpenConns  int `json:"db_max_open_conns"`
	DBMaxIdleConns  int `json:"db_max_idle_conns"`
	RedisPoolSize   int `json:"redis_pool_size"`
	RedisMinIdle    int `json:"redis_min_idle"`

	// Timeouts
	ConnectTimeout  time.Duration `json:"connect_timeout"`
	ReadTimeout     time.Duration `json:"read_timeout"`
	WriteTimeout    time.Duration `json:"write_timeout"`
	IdleTimeout     time.Duration `json:"idle_timeout"`
	ShutdownTimeout time.Duration `json:"shutdown_timeout"`

	// Memory limits
	MaxMessageSize int64 `json:"max_message_size"`

	// Detection info
	NumCPU       int    `json:"num_cpu"`
	TotalMemoryMB int64 `json:"total_memory_mb"`
	Environment  string `json:"environment"` // development, production, container
}

// AutoTune detects system resources and returns optimal configuration.
// All values are calculated automatically - no user input required.
func AutoTune() *Config {
	numCPU := runtime.NumCPU()
	memoryMB := getAvailableMemoryMB()
	env := detectEnvironment()

	cfg := &Config{
		NumCPU:        numCPU,
		TotalMemoryMB: memoryMB,
		Environment:   env,
	}

	// Calculate connection limits based on resources
	cfg.calculateConnectionLimits()
	cfg.calculateWorkerCounts()
	cfg.calculatePoolSizes()
	cfg.calculateTimeouts()
	cfg.calculateMemoryLimits()

	return cfg
}

// calculateConnectionLimits sets connection limits based on CPU and memory.
func (c *Config) calculateConnectionLimits() {
	// Base calculations on CPU count
	baseFactor := c.NumCPU

	// Adjust for environment
	multiplier := 1.0
	switch c.Environment {
	case "production":
		multiplier = 2.0
	case "container":
		multiplier = 1.0 // Containers often have limited resources
	default:
		multiplier = 0.5 // Development - use less resources
	}

	// SMTP connections: start lower, email is bursty
	c.SMTPMaxConnections = clamp(int(float64(baseFactor*100)*multiplier), 100, 10000)

	// IMAP connections: users stay connected longer
	c.IMAPMaxConnections = clamp(int(float64(baseFactor*500)*multiplier), 500, 50000)

	// Per-IP limits to prevent abuse
	c.MaxConnectionsPerIP = clamp(int(float64(baseFactor*10)*multiplier), 10, 500)

	// Reduce limits if memory is low
	if c.TotalMemoryMB < 1024 {
		c.SMTPMaxConnections = c.SMTPMaxConnections / 2
		c.IMAPMaxConnections = c.IMAPMaxConnections / 2
	}
}

// calculateWorkerCounts sets worker counts based on CPU.
func (c *Config) calculateWorkerCounts() {
	// Delivery workers: I/O bound, can have more than CPU count
	c.DeliveryWorkers = clamp(c.NumCPU*4, 4, 500)

	// Vacation workers: typically low volume
	c.VacationWorkers = clamp(c.NumCPU*2, 2, 100)

	// Reduce for development
	if c.Environment == "development" {
		c.DeliveryWorkers = clamp(c.DeliveryWorkers/2, 2, 20)
		c.VacationWorkers = clamp(c.VacationWorkers/2, 1, 10)
	}
}

// calculatePoolSizes sets database and Redis pool sizes.
func (c *Config) calculatePoolSizes() {
	// Database connections: balance between parallelism and resource usage
	c.DBMaxOpenConns = clamp(c.NumCPU*5, 10, 100)
	c.DBMaxIdleConns = clamp(c.DBMaxOpenConns/2, 5, 50)

	// Redis pool: typically needs more connections for pub/sub and queues
	c.RedisPoolSize = clamp(c.NumCPU*10, 10, 200)
	c.RedisMinIdle = clamp(c.RedisPoolSize/4, 2, 50)

	// Reduce for development
	if c.Environment == "development" {
		c.DBMaxOpenConns = clamp(c.DBMaxOpenConns/2, 5, 25)
		c.DBMaxIdleConns = clamp(c.DBMaxIdleConns/2, 2, 10)
		c.RedisPoolSize = clamp(c.RedisPoolSize/2, 5, 50)
		c.RedisMinIdle = clamp(c.RedisMinIdle/2, 1, 10)
	}
}

// calculateTimeouts sets sensible timeouts.
func (c *Config) calculateTimeouts() {
	// These are generally fixed, not resource-dependent
	c.ConnectTimeout = 30 * time.Second
	c.ReadTimeout = 5 * time.Minute  // SMTP/IMAP sessions can be slow
	c.WriteTimeout = 5 * time.Minute
	c.IdleTimeout = 10 * time.Minute
	c.ShutdownTimeout = 30 * time.Second

	// Shorter timeouts for development
	if c.Environment == "development" {
		c.ReadTimeout = 2 * time.Minute
		c.WriteTimeout = 2 * time.Minute
		c.IdleTimeout = 5 * time.Minute
	}
}

// calculateMemoryLimits sets memory-based limits.
func (c *Config) calculateMemoryLimits() {
	// Max message size based on available memory
	// Use roughly 2.5% of memory as max message size, capped
	maxMsgMB := c.TotalMemoryMB / 40
	if maxMsgMB < 10 {
		maxMsgMB = 10 // Minimum 10MB
	}
	if maxMsgMB > 100 {
		maxMsgMB = 100 // Maximum 100MB
	}
	c.MaxMessageSize = maxMsgMB * 1024 * 1024

	// Development: smaller limits
	if c.Environment == "development" {
		c.MaxMessageSize = 25 * 1024 * 1024 // 25MB
	}
}

// ApplyEnvOverrides allows environment variables to override auto-tuned values.
// This provides a way to customize without changing code.
func (c *Config) ApplyEnvOverrides() {
	if v := os.Getenv("MAIL_SMTP_MAX_CONN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.SMTPMaxConnections = n
		}
	}
	if v := os.Getenv("MAIL_IMAP_MAX_CONN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.IMAPMaxConnections = n
		}
	}
	if v := os.Getenv("MAIL_DELIVERY_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.DeliveryWorkers = n
		}
	}
	if v := os.Getenv("MAIL_DB_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.DBMaxOpenConns = n
			c.DBMaxIdleConns = n / 2
		}
	}
	if v := os.Getenv("MAIL_REDIS_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RedisPoolSize = n
			c.RedisMinIdle = n / 4
		}
	}
	if v := os.Getenv("MAIL_MAX_MESSAGE_SIZE"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			c.MaxMessageSize = n
		}
	}
}

// Helper functions

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func detectEnvironment() string {
	// Check for container
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "container"
	}
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return "container"
	}

	// Check for explicit environment variable
	if env := os.Getenv("MAIL_ENV"); env != "" {
		return env
	}
	if env := os.Getenv("GO_ENV"); env != "" {
		return env
	}

	// Default based on debug flags
	if os.Getenv("DEBUG") != "" {
		return "development"
	}

	return "production"
}

func getAvailableMemoryMB() int64 {
	// Try to read from cgroup (container-aware)
	if data, err := os.ReadFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); err == nil {
		if limit, err := strconv.ParseInt(string(data[:len(data)-1]), 10, 64); err == nil {
			// Convert to MB
			mb := limit / (1024 * 1024)
			// Sanity check - if it's very large, use a reasonable default
			if mb > 0 && mb < 1000000 {
				return mb
			}
		}
	}

	// Try cgroup v2
	if data, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		str := string(data[:len(data)-1])
		if str != "max" {
			if limit, err := strconv.ParseInt(str, 10, 64); err == nil {
				mb := limit / (1024 * 1024)
				if mb > 0 && mb < 1000000 {
					return mb
				}
			}
		}
	}

	// Fallback: estimate based on Go's runtime
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Use system memory as approximation, defaulting to 2GB if unknown
	sysMB := int64(memStats.Sys) / (1024 * 1024)
	if sysMB < 100 {
		// Too small, likely just current process memory - assume 2GB
		return 2048
	}

	// Multiply by rough factor since Sys is just Go's portion
	return sysMB * 4
}

// Preset configurations for common scenarios

// SmallServer returns config optimized for <1GB RAM, 1-2 CPUs.
func SmallServer() *Config {
	cfg := &Config{
		NumCPU:              2,
		TotalMemoryMB:       512,
		Environment:         "production",
		SMTPMaxConnections:  200,
		IMAPMaxConnections:  1000,
		MaxConnectionsPerIP: 20,
		DeliveryWorkers:     8,
		VacationWorkers:     4,
		DBMaxOpenConns:      15,
		DBMaxIdleConns:      5,
		RedisPoolSize:       20,
		RedisMinIdle:        5,
		ConnectTimeout:      30 * time.Second,
		ReadTimeout:         5 * time.Minute,
		WriteTimeout:        5 * time.Minute,
		IdleTimeout:         10 * time.Minute,
		ShutdownTimeout:     30 * time.Second,
		MaxMessageSize:      25 * 1024 * 1024,
	}
	return cfg
}

// MediumServer returns config optimized for 2-4GB RAM, 2-4 CPUs.
func MediumServer() *Config {
	cfg := &Config{
		NumCPU:              4,
		TotalMemoryMB:       4096,
		Environment:         "production",
		SMTPMaxConnections:  1000,
		IMAPMaxConnections:  5000,
		MaxConnectionsPerIP: 50,
		DeliveryWorkers:     16,
		VacationWorkers:     8,
		DBMaxOpenConns:      25,
		DBMaxIdleConns:      10,
		RedisPoolSize:       50,
		RedisMinIdle:        10,
		ConnectTimeout:      30 * time.Second,
		ReadTimeout:         5 * time.Minute,
		WriteTimeout:        5 * time.Minute,
		IdleTimeout:         10 * time.Minute,
		ShutdownTimeout:     30 * time.Second,
		MaxMessageSize:      50 * 1024 * 1024,
	}
	return cfg
}

// LargeServer returns config optimized for 8GB+ RAM, 8+ CPUs.
func LargeServer() *Config {
	cfg := &Config{
		NumCPU:              8,
		TotalMemoryMB:       8192,
		Environment:         "production",
		SMTPMaxConnections:  5000,
		IMAPMaxConnections:  20000,
		MaxConnectionsPerIP: 100,
		DeliveryWorkers:     64,
		VacationWorkers:     16,
		DBMaxOpenConns:      50,
		DBMaxIdleConns:      25,
		RedisPoolSize:       100,
		RedisMinIdle:        25,
		ConnectTimeout:      30 * time.Second,
		ReadTimeout:         5 * time.Minute,
		WriteTimeout:        5 * time.Minute,
		IdleTimeout:         10 * time.Minute,
		ShutdownTimeout:     30 * time.Second,
		MaxMessageSize:      100 * 1024 * 1024,
	}
	return cfg
}
