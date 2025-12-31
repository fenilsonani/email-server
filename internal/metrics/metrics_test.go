package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMessagesReceived(t *testing.T) {
	// Get initial value
	initial := testutil.ToFloat64(MessagesReceived)

	// Increment
	MessagesReceived.Inc()

	// Verify increment
	if got := testutil.ToFloat64(MessagesReceived); got != initial+1 {
		t.Errorf("MessagesReceived = %v, want %v", got, initial+1)
	}
}

func TestMessagesSent(t *testing.T) {
	initial := testutil.ToFloat64(MessagesSent)

	MessagesSent.Inc()

	if got := testutil.ToFloat64(MessagesSent); got != initial+1 {
		t.Errorf("MessagesSent = %v, want %v", got, initial+1)
	}
}

func TestMessagesRejected(t *testing.T) {
	reasons := []string{"spam", "quota", "policy"}

	for _, reason := range reasons {
		initial := testutil.ToFloat64(MessagesRejected.WithLabelValues(reason))

		RecordRejection(reason)

		if got := testutil.ToFloat64(MessagesRejected.WithLabelValues(reason)); got != initial+1 {
			t.Errorf("MessagesRejected[%s] = %v, want %v", reason, got, initial+1)
		}
	}
}

func TestRecordAuth(t *testing.T) {
	tests := []struct {
		name     string
		success  bool
		protocol string
		want     string
	}{
		{"success smtp", true, "smtp", "success"},
		{"failure smtp", false, "smtp", "failure"},
		{"success imap", true, "imap", "success"},
		{"failure imap", false, "imap", "failure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initial := testutil.ToFloat64(AuthAttempts.WithLabelValues(tt.want, tt.protocol))

			RecordAuth(tt.success, tt.protocol)

			if got := testutil.ToFloat64(AuthAttempts.WithLabelValues(tt.want, tt.protocol)); got != initial+1 {
				t.Errorf("AuthAttempts[%s,%s] = %v, want %v", tt.want, tt.protocol, got, initial+1)
			}
		})
	}
}

func TestRecordDelivery(t *testing.T) {
	initialSent := testutil.ToFloat64(MessagesSent)

	// Record successful delivery
	RecordDelivery(true, 0.5)

	if got := testutil.ToFloat64(MessagesSent); got != initialSent+1 {
		t.Errorf("MessagesSent after successful delivery = %v, want %v", got, initialSent+1)
	}

	// Record failed delivery (should not increment MessagesSent)
	sentAfterSuccess := testutil.ToFloat64(MessagesSent)
	RecordDelivery(false, 0.5)

	if got := testutil.ToFloat64(MessagesSent); got != sentAfterSuccess {
		t.Errorf("MessagesSent after failed delivery = %v, want %v (unchanged)", got, sentAfterSuccess)
	}

	// Histogram is tested indirectly - we just verify it doesn't panic
	DeliveryDuration.Observe(1.0)
}

func TestRecordConnection(t *testing.T) {
	protocols := []string{"smtp", "imap", "pop3"}

	for _, protocol := range protocols {
		t.Run(protocol, func(t *testing.T) {
			initialActive := testutil.ToFloat64(ActiveConnections.WithLabelValues(protocol))
			initialTotal := testutil.ToFloat64(TotalConnections.WithLabelValues(protocol))

			RecordConnection(protocol)

			if got := testutil.ToFloat64(ActiveConnections.WithLabelValues(protocol)); got != initialActive+1 {
				t.Errorf("ActiveConnections[%s] = %v, want %v", protocol, got, initialActive+1)
			}

			if got := testutil.ToFloat64(TotalConnections.WithLabelValues(protocol)); got != initialTotal+1 {
				t.Errorf("TotalConnections[%s] = %v, want %v", protocol, got, initialTotal+1)
			}

			// Release connection
			ReleaseConnection(protocol)

			if got := testutil.ToFloat64(ActiveConnections.WithLabelValues(protocol)); got != initialActive {
				t.Errorf("ActiveConnections[%s] after release = %v, want %v", protocol, got, initialActive)
			}
		})
	}
}

func TestRecordError(t *testing.T) {
	tests := []struct {
		component string
		errorType string
	}{
		{"smtp", "connection"},
		{"imap", "auth"},
		{"delivery", "dns"},
	}

	for _, tt := range tests {
		t.Run(tt.component+"_"+tt.errorType, func(t *testing.T) {
			initial := testutil.ToFloat64(Errors.WithLabelValues(tt.component, tt.errorType))

			RecordError(tt.component, tt.errorType)

			if got := testutil.ToFloat64(Errors.WithLabelValues(tt.component, tt.errorType)); got != initial+1 {
				t.Errorf("Errors[%s,%s] = %v, want %v", tt.component, tt.errorType, got, initial+1)
			}
		})
	}
}

func TestQuotaExceeded(t *testing.T) {
	initial := testutil.ToFloat64(QuotaExceeded)

	QuotaExceeded.Inc()

	if got := testutil.ToFloat64(QuotaExceeded); got != initial+1 {
		t.Errorf("QuotaExceeded = %v, want %v", got, initial+1)
	}
}

func TestGreylistChecks(t *testing.T) {
	results := []string{"deferred_new", "deferred_retry", "passed"}

	for _, result := range results {
		t.Run(result, func(t *testing.T) {
			initial := testutil.ToFloat64(GreylistChecks.WithLabelValues(result))

			GreylistChecks.WithLabelValues(result).Inc()

			if got := testutil.ToFloat64(GreylistChecks.WithLabelValues(result)); got != initial+1 {
				t.Errorf("GreylistChecks[%s] = %v, want %v", result, got, initial+1)
			}
		})
	}
}

func TestMetricsRegistration(t *testing.T) {
	// Verify key metrics can be collected without panic
	// We test a subset that are gauges/counters (testable with testutil)
	counters := []prometheus.Counter{
		MessagesReceived,
		MessagesSent,
		MessagesBounced,
		MessagesQueued,
		DeliveryRetries,
		QuotaExceeded,
	}

	for _, c := range counters {
		_ = testutil.ToFloat64(c) // Should not panic
	}

	gauges := []prometheus.Gauge{
		QueueDepth,
		Uptime,
	}

	for _, g := range gauges {
		_ = testutil.ToFloat64(g) // Should not panic
	}

	// For vector types, test with specific labels
	_ = testutil.ToFloat64(MessagesRejected.WithLabelValues("test"))
	_ = testutil.ToFloat64(ActiveConnections.WithLabelValues("test"))
	_ = testutil.ToFloat64(TotalConnections.WithLabelValues("test"))
	_ = testutil.ToFloat64(AuthAttempts.WithLabelValues("success", "test"))
	_ = testutil.ToFloat64(IMAPCommands.WithLabelValues("test"))
	_ = testutil.ToFloat64(DAVRequests.WithLabelValues("GET", "caldav"))
	_ = testutil.ToFloat64(GreylistChecks.WithLabelValues("passed"))
	_ = testutil.ToFloat64(Errors.WithLabelValues("test", "test"))

	// Histogram can be tested via Observe
	DeliveryDuration.Observe(0.5)
}

func TestMetricNames(t *testing.T) {
	// Verify metric names follow convention (mailserver_ prefix)
	expected := "mailserver_"

	metricsToCheck := []struct {
		name   string
		metric prometheus.Collector
	}{
		{"MessagesReceived", MessagesReceived},
		{"MessagesSent", MessagesSent},
		{"QuotaExceeded", QuotaExceeded},
	}

	for _, m := range metricsToCheck {
		t.Run(m.name, func(t *testing.T) {
			ch := make(chan prometheus.Metric, 1)
			m.metric.Collect(ch)
			metric := <-ch
			desc := metric.Desc().String()
			if !strings.Contains(desc, expected) {
				t.Errorf("Metric %s description doesn't contain prefix %s: %s", m.name, expected, desc)
			}
		})
	}
}
