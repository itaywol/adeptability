// Package log wraps log/slog with a tiny domain-friendly interface.
//
// The Logger interface is the only thing callers should depend on. The
// concrete implementation is backed by log/slog, configured for either
// human-readable text output or structured JSON (e.g. with --json flag).
package log

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Level represents a verbosity setting.
type Level string

// Supported levels. Anything outside this set falls back to LevelInfo.
const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Logger is the structured logger used across adeptability. Implementations
// MUST be safe for concurrent use.
type Logger interface {
	Debug(msg string, kv ...any)
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
	// With returns a child logger that includes the additional key/value
	// pairs in every record it emits.
	With(kv ...any) Logger
}

// slogLogger is the default Logger implementation.
type slogLogger struct {
	inner *slog.Logger
}

// NewLogger builds a Logger writing to w at the given level. When jsonMode is
// true the output uses slog's JSON handler; otherwise the text handler.
// Passing nil for w falls back to io.Discard so tests don't pollute stderr.
func NewLogger(level Level, jsonMode bool, w io.Writer) Logger {
	if w == nil {
		w = io.Discard
	}
	opts := &slog.HandlerOptions{Level: toSlogLevel(level)}
	var handler slog.Handler
	if jsonMode {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}
	return &slogLogger{inner: slog.New(handler)}
}

func (l *slogLogger) Debug(msg string, kv ...any) { l.inner.Debug(msg, kv...) }
func (l *slogLogger) Info(msg string, kv ...any)  { l.inner.Info(msg, kv...) }
func (l *slogLogger) Warn(msg string, kv ...any)  { l.inner.Warn(msg, kv...) }
func (l *slogLogger) Error(msg string, kv ...any) { l.inner.Error(msg, kv...) }

func (l *slogLogger) With(kv ...any) Logger {
	return &slogLogger{inner: l.inner.With(kv...)}
}

// toSlogLevel maps domain levels to slog. Unknown values map to Info to be
// safe rather than silently dropping records.
func toSlogLevel(l Level) slog.Level {
	switch strings.ToLower(string(l)) {
	case string(LevelDebug):
		return slog.LevelDebug
	case string(LevelWarn):
		return slog.LevelWarn
	case string(LevelError):
		return slog.LevelError
	case string(LevelInfo), "":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}

// ParseLevel converts a string into a Level. Unknown inputs return LevelInfo
// and an error so the caller can surface the typo.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(LevelDebug):
		return LevelDebug, nil
	case string(LevelInfo), "":
		return LevelInfo, nil
	case string(LevelWarn):
		return LevelWarn, nil
	case string(LevelError):
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("log: unknown level %q", s)
	}
}
