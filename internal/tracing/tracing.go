// Package tracing provides distributed tracing capabilities for the email server.
package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/fenilsonani/email-server/internal/logging"
)

// Context keys for trace propagation
type contextKey string

const (
	traceIDKey  contextKey = "trace_id"
	spanIDKey   contextKey = "span_id"
	parentIDKey contextKey = "parent_id"
)

// Tracer provides distributed tracing capabilities.
type Tracer struct {
	enabled bool
	logger  *logging.Logger
}

// NewTracer creates a new tracer instance.
func NewTracer(enabled bool, logger *logging.Logger) *Tracer {
	return &Tracer{
		enabled: enabled,
		logger:  logger,
	}
}

// Span represents a trace span for an operation.
type Span struct {
	tracer    *Tracer
	traceID   string
	spanID    string
	parentID  string
	operation string
	startTime time.Time
	tags      map[string]string
	mu        sync.Mutex
	finished  bool
}

// StartSpan starts a new span for the given operation.
// If there's an existing trace in context, it becomes the parent.
func (t *Tracer) StartSpan(ctx context.Context, operation string) (context.Context, *Span) {
	if !t.enabled {
		return ctx, &Span{tracer: t, operation: operation}
	}

	// Get or generate trace ID
	traceID := GetTraceID(ctx)
	if traceID == "" {
		traceID = GenerateID()
	}

	// Get parent span ID if exists
	parentID := GetSpanID(ctx)

	// Generate new span ID
	spanID := GenerateID()

	span := &Span{
		tracer:    t,
		traceID:   traceID,
		spanID:    spanID,
		parentID:  parentID,
		operation: operation,
		startTime: time.Now(),
		tags:      make(map[string]string),
	}

	// Create new context with span info
	ctx = context.WithValue(ctx, traceIDKey, traceID)
	ctx = context.WithValue(ctx, spanIDKey, spanID)
	if parentID != "" {
		ctx = context.WithValue(ctx, parentIDKey, parentID)
	}

	return ctx, span
}

// SetTag adds a tag to the span.
func (s *Span) SetTag(key, value string) {
	if s == nil || !s.tracer.enabled {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tags[key] = value
}

// SetError marks the span as failed with an error.
func (s *Span) SetError(err error) {
	if s == nil || err == nil || !s.tracer.enabled {
		return
	}
	s.SetTag("error", "true")
	s.SetTag("error.message", err.Error())
}

// Finish completes the span and logs the timing.
func (s *Span) Finish() {
	if s == nil || !s.tracer.enabled {
		return
	}

	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished = true
	duration := time.Since(s.startTime)
	tags := make(map[string]string, len(s.tags))
	for k, v := range s.tags {
		tags[k] = v
	}
	s.mu.Unlock()

	// Log the span
	if s.tracer.logger != nil {
		args := []interface{}{
			"trace_id", s.traceID,
			"span_id", s.spanID,
			"operation", s.operation,
			"duration_ms", duration.Milliseconds(),
		}
		if s.parentID != "" {
			args = append(args, "parent_id", s.parentID)
		}
		for k, v := range tags {
			args = append(args, k, v)
		}

		if tags["error"] == "true" {
			s.tracer.logger.Debug("Span completed with error", args...)
		} else {
			s.tracer.logger.Debug("Span completed", args...)
		}
	}
}

// FinishWithError finishes the span and marks it as failed.
func (s *Span) FinishWithError(err error) {
	if s == nil {
		return
	}
	s.SetError(err)
	s.Finish()
}

// TraceID returns the trace ID of this span.
func (s *Span) TraceID() string {
	if s == nil {
		return ""
	}
	return s.traceID
}

// SpanID returns the span ID of this span.
func (s *Span) SpanID() string {
	if s == nil {
		return ""
	}
	return s.spanID
}

// Duration returns the duration of the span if finished.
func (s *Span) Duration() time.Duration {
	if s == nil {
		return 0
	}
	return time.Since(s.startTime)
}

// GenerateID generates a random 16-byte hex string for trace/span IDs.
func GenerateID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID
		return hex.EncodeToString([]byte(time.Now().String())[:8])
	}
	return hex.EncodeToString(b)
}

// GenerateRequestID generates a unique request ID (alias for GenerateID).
func GenerateRequestID() string {
	return GenerateID()
}

// GetTraceID extracts the trace ID from context.
func GetTraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}
	return ""
}

// GetSpanID extracts the span ID from context.
func GetSpanID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(spanIDKey).(string); ok {
		return id
	}
	return ""
}

// GetParentID extracts the parent span ID from context.
func GetParentID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(parentIDKey).(string); ok {
		return id
	}
	return ""
}

// WithTraceID adds a trace ID to context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// WithSpanID adds a span ID to context.
func WithSpanID(ctx context.Context, spanID string) context.Context {
	return context.WithValue(ctx, spanIDKey, spanID)
}

// InjectHeaders adds trace headers to a map for propagation.
func InjectHeaders(ctx context.Context, headers map[string]string) {
	if traceID := GetTraceID(ctx); traceID != "" {
		headers["X-Trace-ID"] = traceID
	}
	if spanID := GetSpanID(ctx); spanID != "" {
		headers["X-Span-ID"] = spanID
	}
}

// ExtractHeaders extracts trace info from headers and returns a new context.
func ExtractHeaders(ctx context.Context, headers map[string]string) context.Context {
	if traceID, ok := headers["X-Trace-ID"]; ok && traceID != "" {
		ctx = WithTraceID(ctx, traceID)
	}
	if spanID, ok := headers["X-Span-ID"]; ok && spanID != "" {
		// Incoming span becomes parent
		ctx = context.WithValue(ctx, parentIDKey, spanID)
	}
	return ctx
}

// EnsureTraceID ensures a trace ID exists in context, generating one if needed.
func EnsureTraceID(ctx context.Context) context.Context {
	if GetTraceID(ctx) == "" {
		ctx = WithTraceID(ctx, GenerateID())
	}
	return ctx
}
