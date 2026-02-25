package logger

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
)

const (
	// LevelTrace is a verbosity level below DEBUG.
	LevelTrace = slog.Level(-8)
)

// Config controls structured logger creation.
type Config struct {
	Debug     bool
	Trace     bool
	Format    string
	Writer    io.Writer
	AddSource bool
}

// Logger wraps slog with explicit TRACE and conditional stack traces on errors.
type Logger struct {
	base          *slog.Logger
	stackOnErrors bool
}

// New constructs a configured logger.
func New(cfg Config) (*Logger, error) {
	format := strings.TrimSpace(strings.ToLower(cfg.Format))
	if format == "" {
		format = "human"
	}
	if format != "human" && format != "json" {
		return nil, errors.New("log format must be human or json")
	}

	writer := cfg.Writer
	if writer == nil {
		writer = os.Stdout
	}

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	if cfg.Trace {
		level = LevelTrace
	}

	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
	}

	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(writer, opts)
	} else {
		h = slog.NewTextHandler(writer, opts)
	}

	return &Logger{
		base:          slog.New(h),
		stackOnErrors: cfg.Debug || cfg.Trace,
	}, nil
}

// Info logs an informational event.
func (l *Logger) Info(msg string, kv ...any) {
	l.base.Info(msg, kv...)
}

// Debug logs debug-level details.
func (l *Logger) Debug(msg string, kv ...any) {
	l.base.Debug(msg, kv...)
}

// Trace logs trace-level details.
func (l *Logger) Trace(msg string, kv ...any) {
	l.base.Log(context.Background(), LevelTrace, msg, kv...)
}

// Error logs an error event. Stack traces are included only in debug/trace mode.
func (l *Logger) Error(err error, msg string, kv ...any) {
	attrs := make([]any, 0, len(kv)+4)
	attrs = append(attrs, kv...)
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	if l.stackOnErrors {
		attrs = append(attrs, "stack", string(debug.Stack()))
	}
	l.base.Error(msg, attrs...)
}
