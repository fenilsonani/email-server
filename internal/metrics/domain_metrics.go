package metrics

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Per-domain delivery metrics
var (
	// DeliveryByDomain tracks delivery attempts per domain
	DeliveryByDomain = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_delivery_by_domain_total",
		Help: "Delivery attempts per destination domain",
	}, []string{"domain", "result"})

	// DomainDeliveryDuration tracks delivery duration per domain
	DomainDeliveryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mailserver_domain_delivery_duration_seconds",
		Help:    "Delivery duration per destination domain",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 10), // 0.1s to ~100s
	}, []string{"domain"})

	// CircuitBreakerStateGauge tracks circuit breaker state per domain
	CircuitBreakerStateGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mailserver_circuit_breaker_state",
		Help: "Circuit breaker state (0=closed, 1=half-open, 2=open)",
	}, []string{"name", "domain"})

	// CircuitBreakerTransitions tracks circuit breaker state transitions
	CircuitBreakerTransitions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_circuit_breaker_transitions_total",
		Help: "Circuit breaker state transitions",
	}, []string{"name", "from", "to"})
)

// DomainStats tracks per-domain delivery statistics with rolling windows.
type DomainStats struct {
	mu      sync.RWMutex
	windows map[string]*RollingWindow
	window  time.Duration
}

// RollingWindow tracks success/failure events over a time window.
type RollingWindow struct {
	mu        sync.Mutex
	successes []time.Time
	failures  []time.Time
	window    time.Duration
}

// NewDomainStats creates a new domain statistics tracker.
func NewDomainStats(windowDuration time.Duration) *DomainStats {
	if windowDuration == 0 {
		windowDuration = time.Hour
	}
	return &DomainStats{
		windows: make(map[string]*RollingWindow),
		window:  windowDuration,
	}
}

// RecordDelivery records a delivery attempt for a domain.
func (ds *DomainStats) RecordDelivery(domain string, success bool, duration time.Duration) {
	// Update Prometheus metrics
	result := "success"
	if !success {
		result = "failure"
	}
	DeliveryByDomain.WithLabelValues(domain, result).Inc()
	DomainDeliveryDuration.WithLabelValues(domain).Observe(duration.Seconds())

	// Update rolling window
	ds.mu.Lock()
	rw, ok := ds.windows[domain]
	if !ok {
		rw = &RollingWindow{window: ds.window}
		ds.windows[domain] = rw
	}
	ds.mu.Unlock()

	rw.Record(success)
}

// GetSuccessRate returns the success rate for a domain over the window.
func (ds *DomainStats) GetSuccessRate(domain string) float64 {
	ds.mu.RLock()
	rw, ok := ds.windows[domain]
	ds.mu.RUnlock()

	if !ok {
		return 1.0 // No data = assume healthy
	}

	return rw.SuccessRate()
}

// GetDomainStats returns stats for all tracked domains.
func (ds *DomainStats) GetDomainStats() map[string]DomainDeliveryStat {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	stats := make(map[string]DomainDeliveryStat, len(ds.windows))
	for domain, rw := range ds.windows {
		successes, failures := rw.Counts()
		stats[domain] = DomainDeliveryStat{
			Domain:      domain,
			Successes:   successes,
			Failures:    failures,
			SuccessRate: rw.SuccessRate(),
		}
	}
	return stats
}

// DomainDeliveryStat holds delivery statistics for a domain.
type DomainDeliveryStat struct {
	Domain      string  `json:"domain"`
	Successes   int     `json:"successes"`
	Failures    int     `json:"failures"`
	SuccessRate float64 `json:"success_rate"`
}

// Record adds an event to the rolling window.
func (rw *RollingWindow) Record(success bool) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	now := time.Now()
	if success {
		rw.successes = append(rw.successes, now)
	} else {
		rw.failures = append(rw.failures, now)
	}

	// Cleanup old entries
	rw.cleanup(now)
}

// SuccessRate calculates the success rate over the window.
func (rw *RollingWindow) SuccessRate() float64 {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	now := time.Now()
	rw.cleanup(now)

	total := len(rw.successes) + len(rw.failures)
	if total == 0 {
		return 1.0 // No data = assume healthy
	}

	return float64(len(rw.successes)) / float64(total)
}

// Counts returns the number of successes and failures in the window.
func (rw *RollingWindow) Counts() (successes, failures int) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	now := time.Now()
	rw.cleanup(now)

	return len(rw.successes), len(rw.failures)
}

// cleanup removes entries older than the window.
func (rw *RollingWindow) cleanup(now time.Time) {
	cutoff := now.Add(-rw.window)

	// Cleanup successes
	newSuccesses := rw.successes[:0]
	for _, t := range rw.successes {
		if t.After(cutoff) {
			newSuccesses = append(newSuccesses, t)
		}
	}
	rw.successes = newSuccesses

	// Cleanup failures
	newFailures := rw.failures[:0]
	for _, t := range rw.failures {
		if t.After(cutoff) {
			newFailures = append(newFailures, t)
		}
	}
	rw.failures = newFailures
}

// RecordCircuitBreakerState updates the circuit breaker state metric.
func RecordCircuitBreakerState(name, domain string, state int) {
	CircuitBreakerStateGauge.WithLabelValues(name, domain).Set(float64(state))
}

// RecordCircuitBreakerTransition records a circuit breaker state change.
func RecordCircuitBreakerTransition(name, from, to string) {
	CircuitBreakerTransitions.WithLabelValues(name, from, to).Inc()
}
