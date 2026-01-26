package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// SMTP Metrics
	MessagesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_messages_received_total",
		Help: "Total number of messages received via SMTP",
	})

	MessagesSent = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_messages_sent_total",
		Help: "Total number of messages sent successfully",
	})

	MessagesRejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_messages_rejected_total",
		Help: "Total number of messages rejected",
	}, []string{"reason"})

	MessagesBounced = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_messages_bounced_total",
		Help: "Total number of messages that bounced",
	})

	MessagesQueued = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_messages_queued_total",
		Help: "Total number of messages queued for delivery",
	})

	// Delivery Metrics
	DeliveryDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "mailserver_delivery_duration_seconds",
		Help:    "Time taken to deliver messages",
		Buckets: prometheus.ExponentialBuckets(0.1, 2, 10), // 0.1s to ~100s
	})

	DeliveryRetries = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_delivery_retries_total",
		Help: "Total number of delivery retry attempts",
	})

	// Queue Metrics
	QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mailserver_queue_depth",
		Help: "Current number of messages in the delivery queue",
	})

	// Connection Metrics
	ActiveConnections = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mailserver_active_connections",
		Help: "Number of active connections by protocol",
	}, []string{"protocol"})

	TotalConnections = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_connections_total",
		Help: "Total number of connections by protocol",
	}, []string{"protocol"})

	// Authentication Metrics
	AuthAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_auth_attempts_total",
		Help: "Total authentication attempts",
	}, []string{"result", "protocol"})

	// IMAP Metrics
	IMAPCommands = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_imap_commands_total",
		Help: "Total IMAP commands executed",
	}, []string{"command"})

	// IMAP Connection Health Metrics
	IMAPDisconnectReasons = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_imap_disconnects_total",
		Help: "Total IMAP disconnects by reason (timeout, client_close, error, idle_timeout)",
	}, []string{"reason"})

	IMAPKeepalivesSent = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_imap_keepalives_sent_total",
		Help: "Total IDLE keepalives sent to maintain connections",
	})

	IMAPConnectionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "mailserver_imap_connection_duration_seconds",
		Help:    "Duration of IMAP connections",
		Buckets: prometheus.ExponentialBuckets(60, 2, 15), // 1min to ~22 days
	})

	IMAPIdleSessions = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mailserver_imap_idle_sessions",
		Help: "Number of currently active IMAP IDLE sessions",
	})

	// DAV Metrics
	DAVRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_dav_requests_total",
		Help: "Total DAV requests by method",
	}, []string{"method", "type"})

	// Greylist Metrics (for future use)
	GreylistChecks = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_greylist_checks_total",
		Help: "Total greylist checks",
	}, []string{"result"})

	// Quota Metrics
	QuotaExceeded = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_quota_exceeded_total",
		Help: "Total number of messages rejected due to quota",
	})

	// System Metrics
	Uptime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mailserver_uptime_seconds",
		Help: "Server uptime in seconds",
	})

	// Error Metrics
	Errors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_errors_total",
		Help: "Total errors by component",
	}, []string{"component", "type"})
)

// RecordDelivery records a delivery attempt with its duration
func RecordDelivery(success bool, durationSeconds float64) {
	DeliveryDuration.Observe(durationSeconds)
	if success {
		MessagesSent.Inc()
	}
}

// RecordRejection records a message rejection with reason
func RecordRejection(reason string) {
	MessagesRejected.WithLabelValues(reason).Inc()
}

// RecordAuth records an authentication attempt
func RecordAuth(success bool, protocol string) {
	result := "success"
	if !success {
		result = "failure"
	}
	AuthAttempts.WithLabelValues(result, protocol).Inc()
}

// RecordConnection records a new connection
func RecordConnection(protocol string) {
	ActiveConnections.WithLabelValues(protocol).Inc()
	TotalConnections.WithLabelValues(protocol).Inc()
}

// ReleaseConnection records a connection closing
func ReleaseConnection(protocol string) {
	ActiveConnections.WithLabelValues(protocol).Dec()
}

// RecordError records an error
func RecordError(component, errorType string) {
	Errors.WithLabelValues(component, errorType).Inc()
}

// RecordIMAPDisconnect records an IMAP disconnection with reason
func RecordIMAPDisconnect(reason string) {
	IMAPDisconnectReasons.WithLabelValues(reason).Inc()
}

// RecordIMAPKeepalive records an IMAP keepalive sent
func RecordIMAPKeepalive() {
	IMAPKeepalivesSent.Inc()
}

// RecordIMAPConnectionDuration records the duration of an IMAP connection
func RecordIMAPConnectionDuration(durationSeconds float64) {
	IMAPConnectionDuration.Observe(durationSeconds)
}

// RecordIMAPIdleStart records the start of an IDLE session
func RecordIMAPIdleStart() {
	IMAPIdleSessions.Inc()
}

// RecordIMAPIdleEnd records the end of an IDLE session
func RecordIMAPIdleEnd() {
	IMAPIdleSessions.Dec()
}
