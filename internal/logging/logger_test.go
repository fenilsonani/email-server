package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "default config",
			cfg:  DefaultConfig(),
		},
		{
			name: "debug level",
			cfg:  Config{Level: "debug", Format: "json", Output: "stdout"},
		},
		{
			name: "warn level",
			cfg:  Config{Level: "warn", Format: "json", Output: "stdout"},
		},
		{
			name: "warning level (alias)",
			cfg:  Config{Level: "warning", Format: "json", Output: "stdout"},
		},
		{
			name: "error level",
			cfg:  Config{Level: "error", Format: "json", Output: "stdout"},
		},
		{
			name: "info level",
			cfg:  Config{Level: "info", Format: "json", Output: "stdout"},
		},
		{
			name: "text format",
			cfg:  Config{Level: "info", Format: "text", Output: "stdout"},
		},
		{
			name: "stderr output",
			cfg:  Config{Level: "info", Format: "json", Output: "stderr"},
		},
		{
			name: "empty output defaults to stdout",
			cfg:  Config{Level: "info", Format: "json", Output: ""},
		},
		{
			name: "empty format defaults to json",
			cfg:  Config{Level: "info", Format: "", Output: "stdout"},
		},
		{
			name: "invalid level defaults to info",
			cfg:  Config{Level: "invalid", Format: "json", Output: "stdout"},
		},
		{
			name: "invalid format defaults to json",
			cfg:  Config{Level: "info", Format: "invalid", Output: "stdout"},
		},
		{
			name: "with add source",
			cfg:  Config{Level: "info", Format: "json", Output: "stdout", AddSource: true},
		},
		{
			name:    "invalid file path",
			cfg:     Config{Level: "info", Format: "json", Output: "/nonexistent/path/log.txt"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && logger == nil {
				t.Error("New() returned nil logger without error")
			}
			if !tt.wantErr && logger.Logger == nil {
				t.Error("New() returned logger with nil internal logger")
			}
		})
	}
}

func TestNewWithFile(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	cfg := Config{
		Level:  "info",
		Format: "json",
		Output: logFile,
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("New() with file output failed: %v", err)
	}
	if logger == nil {
		t.Fatal("New() returned nil logger")
	}

	// Verify file was created
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Errorf("Log file was not created at %s", logFile)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Level != "info" {
		t.Errorf("Level = %s, want info", cfg.Level)
	}
	if cfg.Format != "json" {
		t.Errorf("Format = %s, want json", cfg.Format)
	}
	if cfg.Output != "stdout" {
		t.Errorf("Output = %s, want stdout", cfg.Output)
	}
	if cfg.AddSource != false {
		t.Errorf("AddSource = %v, want false", cfg.AddSource)
	}
}

func TestDefault(t *testing.T) {
	logger := Default()
	if logger == nil {
		t.Error("Default() returned nil")
	}
	if logger.Logger == nil {
		t.Error("Default() returned logger with nil internal logger")
	}
}

func TestLogger_ComponentLoggers(t *testing.T) {
	logger := Default()

	t.Run("SMTP", func(t *testing.T) {
		smtp := logger.SMTP()
		if smtp == nil {
			t.Error("SMTP() returned nil")
		}
		if smtp.Logger == nil {
			t.Error("SMTP() returned logger with nil internal logger")
		}
	})

	t.Run("IMAP", func(t *testing.T) {
		imap := logger.IMAP()
		if imap == nil {
			t.Error("IMAP() returned nil")
		}
		if imap.Logger == nil {
			t.Error("IMAP() returned logger with nil internal logger")
		}
	})

	t.Run("Delivery", func(t *testing.T) {
		delivery := logger.Delivery()
		if delivery == nil {
			t.Error("Delivery() returned nil")
		}
		if delivery.Logger == nil {
			t.Error("Delivery() returned logger with nil internal logger")
		}
	})

	t.Run("Storage", func(t *testing.T) {
		storage := logger.Storage()
		if storage == nil {
			t.Error("Storage() returned nil")
		}
		if storage.Logger == nil {
			t.Error("Storage() returned logger with nil internal logger")
		}
	})
}

func TestLogger_WithFields(t *testing.T) {
	logger := Default()

	t.Run("with single field", func(t *testing.T) {
		withFields := logger.WithFields("key", "value")
		if withFields == nil {
			t.Error("WithFields() returned nil")
		}
		if withFields.Logger == nil {
			t.Error("WithFields() returned logger with nil internal logger")
		}
	})

	t.Run("with multiple fields", func(t *testing.T) {
		withFields := logger.WithFields("key1", "value1", "key2", 42, "key3", true)
		if withFields == nil {
			t.Error("WithFields() returned nil")
		}
	})

	t.Run("with no fields", func(t *testing.T) {
		withFields := logger.WithFields()
		if withFields == nil {
			t.Error("WithFields() returned nil")
		}
	})
}

func TestLogger_WithError(t *testing.T) {
	logger := Default()

	t.Run("with error", func(t *testing.T) {
		testErr := errors.New("test error")
		withErr := logger.WithError(testErr)
		if withErr == nil {
			t.Error("WithError() returned nil")
		}
		if withErr.Logger == nil {
			t.Error("WithError() returned logger with nil internal logger")
		}
		// Verify it returns a different logger instance
		if withErr == logger {
			t.Error("WithError() should return a new logger instance")
		}
	})

	t.Run("with nil error", func(t *testing.T) {
		withErr := logger.WithError(nil)
		if withErr != logger {
			t.Error("WithError(nil) should return same logger")
		}
	})
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()

	t.Run("WithTraceID", func(t *testing.T) {
		newCtx := WithTraceID(ctx, "trace-123")
		if v := newCtx.Value(traceIDKey); v != "trace-123" {
			t.Errorf("TraceID = %v, want trace-123", v)
		}
	})

	t.Run("WithUserID", func(t *testing.T) {
		newCtx := WithUserID(ctx, 42)
		if v := newCtx.Value(userIDKey); v != int64(42) {
			t.Errorf("UserID = %v, want 42", v)
		}
	})

	t.Run("WithRemoteAddr", func(t *testing.T) {
		newCtx := WithRemoteAddr(ctx, "192.168.1.1:1234")
		if v := newCtx.Value(remoteAddrKey); v != "192.168.1.1:1234" {
			t.Errorf("RemoteAddr = %v, want 192.168.1.1:1234", v)
		}
	})

	t.Run("WithProtocol", func(t *testing.T) {
		newCtx := WithProtocol(ctx, "IMAP")
		if v := newCtx.Value(protocolKey); v != "IMAP" {
			t.Errorf("Protocol = %v, want IMAP", v)
		}
	})

	t.Run("WithMessageID", func(t *testing.T) {
		newCtx := WithMessageID(ctx, "msg-456")
		if v := newCtx.Value(messageIDKey); v != "msg-456" {
			t.Errorf("MessageID = %v, want msg-456", v)
		}
	})

	t.Run("WithMailbox", func(t *testing.T) {
		newCtx := WithMailbox(ctx, "INBOX")
		if v := newCtx.Value(mailboxKey); v != "INBOX" {
			t.Errorf("Mailbox = %v, want INBOX", v)
		}
	})

	t.Run("multiple context values", func(t *testing.T) {
		newCtx := WithTraceID(ctx, "trace-123")
		newCtx = WithUserID(newCtx, 42)
		newCtx = WithRemoteAddr(newCtx, "192.168.1.1")
		newCtx = WithProtocol(newCtx, "SMTP")
		newCtx = WithMessageID(newCtx, "msg-789")
		newCtx = WithMailbox(newCtx, "Sent")

		if v := newCtx.Value(traceIDKey); v != "trace-123" {
			t.Errorf("TraceID = %v, want trace-123", v)
		}
		if v := newCtx.Value(userIDKey); v != int64(42) {
			t.Errorf("UserID = %v, want 42", v)
		}
		if v := newCtx.Value(remoteAddrKey); v != "192.168.1.1" {
			t.Errorf("RemoteAddr = %v, want 192.168.1.1", v)
		}
		if v := newCtx.Value(protocolKey); v != "SMTP" {
			t.Errorf("Protocol = %v, want SMTP", v)
		}
		if v := newCtx.Value(messageIDKey); v != "msg-789" {
			t.Errorf("MessageID = %v, want msg-789", v)
		}
		if v := newCtx.Value(mailboxKey); v != "Sent" {
			t.Errorf("Mailbox = %v, want Sent", v)
		}
	})
}

func TestExtractContextAttrs(t *testing.T) {
	t.Run("all attributes", func(t *testing.T) {
		ctx := context.Background()
		ctx = WithTraceID(ctx, "trace-123")
		ctx = WithUserID(ctx, 42)
		ctx = WithRemoteAddr(ctx, "192.168.1.1")
		ctx = WithProtocol(ctx, "SMTP")
		ctx = WithMessageID(ctx, "msg-456")
		ctx = WithMailbox(ctx, "INBOX")

		attrs := extractContextAttrs(ctx)

		if len(attrs) != 6 {
			t.Errorf("Expected 6 attrs, got %d", len(attrs))
		}

		// Check that attributes are present
		found := map[string]bool{}
		for _, attr := range attrs {
			found[attr.Key] = true
		}

		expected := []string{"trace_id", "user_id", "remote_addr", "protocol", "message_id", "mailbox"}
		for _, key := range expected {
			if !found[key] {
				t.Errorf("Missing attribute: %s", key)
			}
		}
	})

	t.Run("partial attributes", func(t *testing.T) {
		ctx := context.Background()
		ctx = WithTraceID(ctx, "trace-123")
		ctx = WithRemoteAddr(ctx, "192.168.1.1")

		attrs := extractContextAttrs(ctx)

		if len(attrs) != 2 {
			t.Errorf("Expected 2 attrs, got %d", len(attrs))
		}

		found := map[string]bool{}
		for _, attr := range attrs {
			found[attr.Key] = true
		}

		if !found["trace_id"] {
			t.Error("Missing trace_id attribute")
		}
		if !found["remote_addr"] {
			t.Error("Missing remote_addr attribute")
		}
	})

	t.Run("empty context", func(t *testing.T) {
		ctx := context.Background()
		attrs := extractContextAttrs(ctx)

		if len(attrs) != 0 {
			t.Errorf("Expected 0 attrs for empty context, got %d", len(attrs))
		}
	})
}

func TestLogger_Caller(t *testing.T) {
	logger := Default()
	withCaller := logger.Caller()
	if withCaller == nil {
		t.Error("Caller() returned nil")
	}
	if withCaller.Logger == nil {
		t.Error("Caller() returned logger with nil internal logger")
	}
	// Verify it returns a different logger instance
	if withCaller == logger {
		t.Error("Caller() should return a new logger instance")
	}
}

func TestLogger_InfoContext(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}

	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-123")
	ctx = WithUserID(ctx, 42)

	logger.InfoContext(ctx, "test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("Log output should contain message, got: %s", output)
	}
	if !strings.Contains(output, "trace-123") {
		t.Errorf("Log output should contain trace_id, got: %s", output)
	}
	if !strings.Contains(output, "42") {
		t.Errorf("Log output should contain user_id, got: %s", output)
	}
	if !strings.Contains(output, "value") {
		t.Errorf("Log output should contain custom field, got: %s", output)
	}
}

func TestLogger_ErrorContext(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}

	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-456")

	testErr := errors.New("test error")
	logger.ErrorContext(ctx, "error occurred", testErr, "key", "value")

	output := buf.String()
	if !strings.Contains(output, "error occurred") {
		t.Errorf("Log output should contain message, got: %s", output)
	}
	if !strings.Contains(output, "test error") {
		t.Errorf("Log output should contain error, got: %s", output)
	}
	if !strings.Contains(output, "trace-456") {
		t.Errorf("Log output should contain trace_id, got: %s", output)
	}
	if !strings.Contains(output, "ERROR") {
		t.Errorf("Log output should be at ERROR level, got: %s", output)
	}
}

func TestLogger_ErrorContext_NilError(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}

	ctx := context.Background()
	logger.ErrorContext(ctx, "error occurred", nil)

	output := buf.String()
	if !strings.Contains(output, "error occurred") {
		t.Errorf("Log output should contain message, got: %s", output)
	}
}

func TestLogger_WarnContext(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}

	ctx := context.Background()
	ctx = WithRemoteAddr(ctx, "192.168.1.1")

	logger.WarnContext(ctx, "warning message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "warning message") {
		t.Errorf("Log output should contain message, got: %s", output)
	}
	if !strings.Contains(output, "192.168.1.1") {
		t.Errorf("Log output should contain remote_addr, got: %s", output)
	}
	if !strings.Contains(output, "WARN") {
		t.Errorf("Log output should be at WARN level, got: %s", output)
	}
}

func TestLogger_DebugContext(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})),
	}

	ctx := context.Background()
	ctx = WithProtocol(ctx, "IMAP")

	logger.DebugContext(ctx, "debug message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "debug message") {
		t.Errorf("Log output should contain message, got: %s", output)
	}
	if !strings.Contains(output, "IMAP") {
		t.Errorf("Log output should contain protocol, got: %s", output)
	}
	if !strings.Contains(output, "DEBUG") {
		t.Errorf("Log output should be at DEBUG level, got: %s", output)
	}
}

func TestLogger_LogLevels(t *testing.T) {
	tests := []struct {
		name       string
		level      string
		shouldLog  map[string]bool
	}{
		{
			name:  "debug level",
			level: "debug",
			shouldLog: map[string]bool{
				"debug": true,
				"info":  true,
				"warn":  true,
				"error": true,
			},
		},
		{
			name:  "info level",
			level: "info",
			shouldLog: map[string]bool{
				"debug": false,
				"info":  true,
				"warn":  true,
				"error": true,
			},
		},
		{
			name:  "warn level",
			level: "warn",
			shouldLog: map[string]bool{
				"debug": false,
				"info":  false,
				"warn":  true,
				"error": true,
			},
		},
		{
			name:  "error level",
			level: "error",
			shouldLog: map[string]bool{
				"debug": false,
				"info":  false,
				"warn":  false,
				"error": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger, err := New(Config{
				Level:  tt.level,
				Format: "json",
				Output: "stdout",
			})
			if err != nil {
				t.Fatalf("Failed to create logger: %v", err)
			}

			// Replace handler to capture output
			logger.Logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
				Level: parseLevel(tt.level),
			}))

			ctx := context.Background()

			// Test debug
			buf.Reset()
			logger.DebugContext(ctx, "debug")
			hasOutput := buf.Len() > 0
			if hasOutput != tt.shouldLog["debug"] {
				t.Errorf("Debug: got output=%v, want %v", hasOutput, tt.shouldLog["debug"])
			}

			// Test info
			buf.Reset()
			logger.InfoContext(ctx, "info")
			hasOutput = buf.Len() > 0
			if hasOutput != tt.shouldLog["info"] {
				t.Errorf("Info: got output=%v, want %v", hasOutput, tt.shouldLog["info"])
			}

			// Test warn
			buf.Reset()
			logger.WarnContext(ctx, "warn")
			hasOutput = buf.Len() > 0
			if hasOutput != tt.shouldLog["warn"] {
				t.Errorf("Warn: got output=%v, want %v", hasOutput, tt.shouldLog["warn"])
			}

			// Test error
			buf.Reset()
			logger.ErrorContext(ctx, "error", errors.New("test"))
			hasOutput = buf.Len() > 0
			if hasOutput != tt.shouldLog["error"] {
				t.Errorf("Error: got output=%v, want %v", hasOutput, tt.shouldLog["error"])
			}
		})
	}
}

func TestLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}

	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-123")

	logger.InfoContext(ctx, "test message", "key", "value")

	// Verify it's valid JSON
	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Errorf("Failed to parse JSON output: %v", err)
	}

	// Verify expected fields
	if logEntry["msg"] != "test message" {
		t.Errorf("Expected msg='test message', got %v", logEntry["msg"])
	}
	if logEntry["trace_id"] != "trace-123" {
		t.Errorf("Expected trace_id='trace-123', got %v", logEntry["trace_id"])
	}
	if logEntry["key"] != "value" {
		t.Errorf("Expected key='value', got %v", logEntry["key"])
	}
	if logEntry["level"] != "INFO" {
		t.Errorf("Expected level='INFO', got %v", logEntry["level"])
	}
	if _, ok := logEntry["time"]; !ok {
		t.Error("Expected time field in JSON output")
	}
}

func TestLogger_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}

	ctx := context.Background()
	logger.InfoContext(ctx, "test message")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("Text output should contain message, got: %s", output)
	}
	if !strings.Contains(output, "level=INFO") {
		t.Errorf("Text output should contain level, got: %s", output)
	}
}

func TestLogger_ComponentLoggersWithFields(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}

	t.Run("SMTP component", func(t *testing.T) {
		buf.Reset()
		smtpLogger := logger.SMTP()
		smtpLogger.Info("smtp message")

		output := buf.String()
		if !strings.Contains(output, "smtp") {
			t.Errorf("SMTP logger should include component field, got: %s", output)
		}
	})

	t.Run("IMAP component", func(t *testing.T) {
		buf.Reset()
		imapLogger := logger.IMAP()
		imapLogger.Info("imap message")

		output := buf.String()
		if !strings.Contains(output, "imap") {
			t.Errorf("IMAP logger should include component field, got: %s", output)
		}
	})

	t.Run("Delivery component", func(t *testing.T) {
		buf.Reset()
		deliveryLogger := logger.Delivery()
		deliveryLogger.Info("delivery message")

		output := buf.String()
		if !strings.Contains(output, "delivery") {
			t.Errorf("Delivery logger should include component field, got: %s", output)
		}
	})

	t.Run("Storage component", func(t *testing.T) {
		buf.Reset()
		storageLogger := logger.Storage()
		storageLogger.Info("storage message")

		output := buf.String()
		if !strings.Contains(output, "storage") {
			t.Errorf("Storage logger should include component field, got: %s", output)
		}
	})
}

func TestLogger_WithFieldsOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}

	withFields := logger.WithFields("user", "john", "age", 30)
	withFields.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "john") {
		t.Errorf("Output should contain field value 'john', got: %s", output)
	}
	if !strings.Contains(output, "30") {
		t.Errorf("Output should contain field value 30, got: %s", output)
	}
}

func TestLogger_WithErrorOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}

	testErr := errors.New("test error message")
	withErr := logger.WithError(testErr)
	withErr.Info("operation failed")

	output := buf.String()
	if !strings.Contains(output, "test error message") {
		t.Errorf("Output should contain error message, got: %s", output)
	}
	if !strings.Contains(output, "operation failed") {
		t.Errorf("Output should contain log message, got: %s", output)
	}
}

func TestLogger_ChainedMethods(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}

	// Chain multiple methods
	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-999")

	logger.
		SMTP().
		WithFields("session", "abc123").
		WithError(errors.New("connection failed")).
		InfoContext(ctx, "SMTP connection error")

	output := buf.String()
	if !strings.Contains(output, "smtp") {
		t.Errorf("Output should contain component, got: %s", output)
	}
	if !strings.Contains(output, "abc123") {
		t.Errorf("Output should contain session field, got: %s", output)
	}
	if !strings.Contains(output, "connection failed") {
		t.Errorf("Output should contain error, got: %s", output)
	}
	if !strings.Contains(output, "trace-999") {
		t.Errorf("Output should contain trace_id, got: %s", output)
	}
}

func TestLogger_TimeFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Key == slog.TimeKey {
					if t, ok := a.Value.Any().(time.Time); ok {
						a.Value = slog.StringValue(t.Format(time.RFC3339Nano))
					}
				}
				return a
			},
		})),
	}

	logger.Info("test message")

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	timeStr, ok := logEntry["time"].(string)
	if !ok {
		t.Fatal("Time field is not a string")
	}

	// Verify time format is RFC3339Nano
	_, err := time.Parse(time.RFC3339Nano, timeStr)
	if err != nil {
		t.Errorf("Time format is not RFC3339Nano: %v", err)
	}
}

func TestLogger_AllContextFields(t *testing.T) {
	var buf bytes.Buffer
	logger := &Logger{
		Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	}

	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-123")
	ctx = WithUserID(ctx, 42)
	ctx = WithRemoteAddr(ctx, "192.168.1.1")
	ctx = WithProtocol(ctx, "SMTP")
	ctx = WithMessageID(ctx, "msg-456")
	ctx = WithMailbox(ctx, "INBOX")

	logger.InfoContext(ctx, "test message with all context fields")

	var logEntry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Verify all context fields are present
	expectedFields := map[string]interface{}{
		"trace_id":   "trace-123",
		"user_id":    float64(42), // JSON numbers are float64
		"remote_addr": "192.168.1.1",
		"protocol":   "SMTP",
		"message_id": "msg-456",
		"mailbox":    "INBOX",
	}

	for key, expectedValue := range expectedFields {
		if logEntry[key] != expectedValue {
			t.Errorf("Expected %s=%v, got %v", key, expectedValue, logEntry[key])
		}
	}
}

// Helper function to parse log level
func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Benchmarks
func BenchmarkNew(b *testing.B) {
	cfg := DefaultConfig()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		New(cfg)
	}
}

func BenchmarkExtractContextAttrs(b *testing.B) {
	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-123")
	ctx = WithUserID(ctx, 42)
	ctx = WithRemoteAddr(ctx, "192.168.1.1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractContextAttrs(ctx)
	}
}

func BenchmarkExtractContextAttrs_AllFields(b *testing.B) {
	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-123")
	ctx = WithUserID(ctx, 42)
	ctx = WithRemoteAddr(ctx, "192.168.1.1")
	ctx = WithProtocol(ctx, "SMTP")
	ctx = WithMessageID(ctx, "msg-456")
	ctx = WithMailbox(ctx, "INBOX")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractContextAttrs(ctx)
	}
}

func BenchmarkLogger_InfoContext(b *testing.B) {
	logger := Default()
	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-123")
	ctx = WithUserID(ctx, 42)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.InfoContext(ctx, "benchmark message", "key", "value")
	}
}

func BenchmarkLogger_InfoContext_NoContext(b *testing.B) {
	logger := Default()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.InfoContext(ctx, "benchmark message", "key", "value")
	}
}

func BenchmarkLogger_WithFields(b *testing.B) {
	logger := Default()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.WithFields("key1", "value1", "key2", 42)
	}
}

func BenchmarkLogger_ComponentLogger(b *testing.B) {
	logger := Default()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.SMTP()
	}
}

func BenchmarkLogger_ChainedMethods(b *testing.B) {
	logger := Default()
	testErr := errors.New("test error")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.SMTP().WithFields("key", "value").WithError(testErr)
	}
}
