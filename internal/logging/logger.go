// Package logging provides structured logging for the email server.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"time"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// Context keys for common fields
	traceIDKey   contextKey = "trace_id"
	userIDKey    contextKey = "user_id"
	remoteAddrKey contextKey = "remote_addr"
	protocolKey  contextKey = "protocol"
	messageIDKey contextKey = "message_id"
	mailboxKey   contextKey = "mailbox"
)

// Logger wraps slog with email-server-specific functionality.
type Logger struct {
	*slog.Logger
}

// Config configures the logger.
type Config struct {
	// Level is the minimum log level (debug, info, warn, error).
	Level string
	// Format is the output format (json, text).
	Format string
	// Output is the output destination (stdout, stderr, or file path).
	Output string
	// AddSource adds source code location to log entries.
	AddSource bool
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	return Config{
		Level:     "info",
		Format:    "json",
		Output:    "stdout",
		AddSource: false,
	}
}

// New creates a new Logger with the given configuration.
func New(cfg Config) (*Logger, error) {
	// Parse log level
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Determine output
	var output io.Writer
	switch cfg.Output {
	case "stdout", "":
		output = os.Stdout
	case "stderr":
		output = os.Stderr
	default:
		f, err := os.OpenFile(cfg.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		output = f
	}

	// Create handler options
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Customize time format
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					a.Value = slog.StringValue(t.Format(time.RFC3339Nano))
				}
			}
			return a
		},
	}

	// Create handler based on format
	var handler slog.Handler
	switch cfg.Format {
	case "text":
		handler = slog.NewTextHandler(output, opts)
	case "json", "":
		handler = slog.NewJSONHandler(output, opts)
	default:
		handler = slog.NewJSONHandler(output, opts)
	}

	return &Logger{
		Logger: slog.New(handler),
	}, nil
}

// Default returns a default logger.
func Default() *Logger {
	logger, _ := New(DefaultConfig())
	return logger
}

// WithTraceID returns a new context with the trace ID.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// WithUserID returns a new context with the user ID.
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// WithRemoteAddr returns a new context with the remote address.
func WithRemoteAddr(ctx context.Context, addr string) context.Context {
	return context.WithValue(ctx, remoteAddrKey, addr)
}

// WithProtocol returns a new context with the protocol.
func WithProtocol(ctx context.Context, protocol string) context.Context {
	return context.WithValue(ctx, protocolKey, protocol)
}

// WithMessageID returns a new context with the message ID.
func WithMessageID(ctx context.Context, msgID string) context.Context {
	return context.WithValue(ctx, messageIDKey, msgID)
}

// WithMailbox returns a new context with the mailbox name.
func WithMailbox(ctx context.Context, mailbox string) context.Context {
	return context.WithValue(ctx, mailboxKey, mailbox)
}

// extractContextAttrs extracts logging attributes from context.
func extractContextAttrs(ctx context.Context) []slog.Attr {
	var attrs []slog.Attr

	if v := ctx.Value(traceIDKey); v != nil {
		attrs = append(attrs, slog.String("trace_id", v.(string)))
	}
	if v := ctx.Value(userIDKey); v != nil {
		attrs = append(attrs, slog.Int64("user_id", v.(int64)))
	}
	if v := ctx.Value(remoteAddrKey); v != nil {
		attrs = append(attrs, slog.String("remote_addr", v.(string)))
	}
	if v := ctx.Value(protocolKey); v != nil {
		attrs = append(attrs, slog.String("protocol", v.(string)))
	}
	if v := ctx.Value(messageIDKey); v != nil {
		attrs = append(attrs, slog.String("message_id", v.(string)))
	}
	if v := ctx.Value(mailboxKey); v != nil {
		attrs = append(attrs, slog.String("mailbox", v.(string)))
	}

	return attrs
}

// InfoContext logs an info message with context.
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	attrs := extractContextAttrs(ctx)
	allArgs := make([]any, 0, len(attrs)*2+len(args))
	for _, attr := range attrs {
		allArgs = append(allArgs, attr.Key, attr.Value.Any())
	}
	allArgs = append(allArgs, args...)
	l.Logger.InfoContext(ctx, msg, allArgs...)
}

// ErrorContext logs an error message with context.
func (l *Logger) ErrorContext(ctx context.Context, msg string, err error, args ...any) {
	attrs := extractContextAttrs(ctx)
	allArgs := make([]any, 0, len(attrs)*2+len(args)+2)
	if err != nil {
		allArgs = append(allArgs, "error", err.Error())
	}
	for _, attr := range attrs {
		allArgs = append(allArgs, attr.Key, attr.Value.Any())
	}
	allArgs = append(allArgs, args...)
	l.Logger.ErrorContext(ctx, msg, allArgs...)
}

// WarnContext logs a warning message with context.
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	attrs := extractContextAttrs(ctx)
	allArgs := make([]any, 0, len(attrs)*2+len(args))
	for _, attr := range attrs {
		allArgs = append(allArgs, attr.Key, attr.Value.Any())
	}
	allArgs = append(allArgs, args...)
	l.Logger.WarnContext(ctx, msg, allArgs...)
}

// DebugContext logs a debug message with context.
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	attrs := extractContextAttrs(ctx)
	allArgs := make([]any, 0, len(attrs)*2+len(args))
	for _, attr := range attrs {
		allArgs = append(allArgs, attr.Key, attr.Value.Any())
	}
	allArgs = append(allArgs, args...)
	l.Logger.DebugContext(ctx, msg, allArgs...)
}

// WithError returns a logger with the error attached.
func (l *Logger) WithError(err error) *Logger {
	if err == nil {
		return l
	}
	return &Logger{
		Logger: l.Logger.With("error", err.Error()),
	}
}

// WithFields returns a logger with additional fields.
func (l *Logger) WithFields(args ...any) *Logger {
	return &Logger{
		Logger: l.Logger.With(args...),
	}
}

// SMTP returns a logger configured for SMTP operations.
func (l *Logger) SMTP() *Logger {
	return &Logger{
		Logger: l.Logger.With("component", "smtp"),
	}
}

// IMAP returns a logger configured for IMAP operations.
func (l *Logger) IMAP() *Logger {
	return &Logger{
		Logger: l.Logger.With("component", "imap"),
	}
}

// Delivery returns a logger configured for delivery operations.
func (l *Logger) Delivery() *Logger {
	return &Logger{
		Logger: l.Logger.With("component", "delivery"),
	}
}

// Storage returns a logger configured for storage operations.
func (l *Logger) Storage() *Logger {
	return &Logger{
		Logger: l.Logger.With("component", "storage"),
	}
}

// Caller adds caller information to the log entry.
func (l *Logger) Caller() *Logger {
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		return l
	}
	return &Logger{
		Logger: l.Logger.With("caller", slog.GroupValue(
			slog.String("file", file),
			slog.Int("line", line),
		)),
	}
}
