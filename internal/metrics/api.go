package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// API-specific metrics for the transactional email API

var (
	// Request metrics
	APIRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_api_requests_total",
		Help: "Total number of API requests",
	}, []string{"endpoint", "method", "status"})

	APIRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mailserver_api_request_duration_seconds",
		Help:    "API request duration in seconds",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~16s
	}, []string{"endpoint"})

	// Email queueing metrics
	APIEmailsQueuedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_api_emails_queued_total",
		Help: "Total number of emails queued via API",
	}, []string{"priority"})

	APIEmailsScheduledTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_api_emails_scheduled_total",
		Help: "Total number of emails scheduled for future delivery",
	})

	// Suppression metrics
	APISuppressionsHitTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_api_suppressions_hit_total",
		Help: "Total number of emails blocked due to suppression list",
	})

	APISuppressionsAddedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_api_suppressions_added_total",
		Help: "Total number of emails added to suppression list",
	}, []string{"reason"})

	// Idempotency metrics
	APIIdempotencyHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_api_idempotency_hits_total",
		Help: "Total number of idempotent request replays",
	})

	APIIdempotencyConflictsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_api_idempotency_conflicts_total",
		Help: "Total number of idempotency conflicts (concurrent requests with same key)",
	})

	// Validation metrics
	APIValidationErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_api_validation_errors_total",
		Help: "Total number of validation errors",
	}, []string{"field"})

	// Rate limiting metrics
	APIRateLimitedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_api_rate_limited_total",
		Help: "Total number of requests rejected due to rate limiting",
	})

	// Batch metrics
	APIBatchRequestsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_api_batch_requests_total",
		Help: "Total number of batch send requests",
	})

	APIBatchMessagesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_api_batch_messages_total",
		Help: "Total number of messages in batch requests",
	})

	// Template metrics
	APITemplateRendersTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailserver_api_template_renders_total",
		Help: "Total number of template renders",
	})

	// Webhook metrics
	APIWebhooksTriggeredTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_api_webhooks_triggered_total",
		Help: "Total number of webhooks triggered",
	}, []string{"event"})

	APIWebhooksDeliveredTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_api_webhooks_delivered_total",
		Help: "Total number of webhooks successfully delivered",
	}, []string{"event"})

	APIWebhooksFailedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailserver_api_webhooks_failed_total",
		Help: "Total number of webhook delivery failures",
	}, []string{"event"})

	// Active scheduled emails gauge
	APIScheduledEmailsGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mailserver_api_scheduled_emails",
		Help: "Current number of emails pending scheduled delivery",
	})

	// Queue depth by priority
	APIQueueDepthByPriority = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "mailserver_api_queue_depth_by_priority",
		Help: "Current queue depth by priority level",
	}, []string{"priority"})
)

// RecordAPIRequest records an API request metric
func RecordAPIRequest(endpoint, method string, statusCode int, durationSeconds float64) {
	status := statusCodeToCategory(statusCode)
	APIRequestsTotal.WithLabelValues(endpoint, method, status).Inc()
	APIRequestDuration.WithLabelValues(endpoint).Observe(durationSeconds)
}

// RecordEmailQueued records when an email is queued
func RecordEmailQueued(priority string) {
	if priority == "" {
		priority = "normal"
	}
	APIEmailsQueuedTotal.WithLabelValues(priority).Inc()
}

// RecordEmailScheduled records when an email is scheduled
func RecordEmailScheduled() {
	APIEmailsScheduledTotal.Inc()
}

// RecordSuppressionHit records when a send is blocked by suppression
func RecordSuppressionHit() {
	APISuppressionsHitTotal.Inc()
}

// RecordSuppressionAdded records when an email is added to suppression
func RecordSuppressionAdded(reason string) {
	APISuppressionsAddedTotal.WithLabelValues(reason).Inc()
}

// RecordIdempotencyHit records an idempotent request replay
func RecordIdempotencyHit() {
	APIIdempotencyHitsTotal.Inc()
}

// RecordIdempotencyConflict records an idempotency conflict
func RecordIdempotencyConflict() {
	APIIdempotencyConflictsTotal.Inc()
}

// RecordValidationError records a validation error
func RecordValidationError(field string) {
	APIValidationErrorsTotal.WithLabelValues(field).Inc()
}

// RecordRateLimited records a rate-limited request
func RecordRateLimited() {
	APIRateLimitedTotal.Inc()
}

// RecordBatchRequest records a batch request
func RecordBatchRequest(messageCount int) {
	APIBatchRequestsTotal.Inc()
	APIBatchMessagesTotal.Add(float64(messageCount))
}

// RecordTemplateRender records a template render
func RecordTemplateRender() {
	APITemplateRendersTotal.Inc()
}

// RecordWebhookTriggered records when a webhook is triggered
func RecordWebhookTriggered(event string) {
	APIWebhooksTriggeredTotal.WithLabelValues(event).Inc()
}

// RecordWebhookDelivered records when a webhook is successfully delivered
func RecordWebhookDelivered(event string) {
	APIWebhooksDeliveredTotal.WithLabelValues(event).Inc()
}

// RecordWebhookFailed records when a webhook delivery fails
func RecordWebhookFailed(event string) {
	APIWebhooksFailedTotal.WithLabelValues(event).Inc()
}

// statusCodeToCategory converts a status code to a category for metrics
func statusCodeToCategory(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500:
		return "5xx"
	default:
		return "unknown"
	}
}
