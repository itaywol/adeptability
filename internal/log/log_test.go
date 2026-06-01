package log

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogger_TextOutput(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(LevelInfo, false, &buf)
	l.Info("hello", "k", "v")
	out := buf.String()
	require.Contains(t, out, "hello")
	require.Contains(t, out, "k=v")
}

func TestLogger_JSONOutput(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(LevelInfo, true, &buf)
	l.Info("hello", "k", "v")
	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec))
	require.Equal(t, "hello", rec["msg"])
	require.Equal(t, "v", rec["k"])
}

func TestLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(LevelWarn, false, &buf)
	l.Debug("debug-msg")
	l.Info("info-msg")
	l.Warn("warn-msg")
	l.Error("error-msg")
	out := buf.String()
	require.NotContains(t, out, "debug-msg")
	require.NotContains(t, out, "info-msg")
	require.Contains(t, out, "warn-msg")
	require.Contains(t, out, "error-msg")
}

func TestLogger_With(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(LevelInfo, true, &buf)
	child := l.With("scope", "test")
	child.Info("event", "k", "v")
	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &rec))
	require.Equal(t, "test", rec["scope"])
	require.Equal(t, "v", rec["k"])
}

func TestLogger_NilWriterDoesNotPanic(t *testing.T) {
	l := NewLogger(LevelInfo, false, nil)
	require.NotPanics(t, func() {
		l.Info("ok")
	})
}

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{
		"debug":   LevelDebug,
		"INFO":    LevelInfo,
		"":        LevelInfo,
		" warn ":  LevelWarn,
		"ERROR":   LevelError,
	}
	for in, want := range cases {
		got, err := ParseLevel(in)
		require.NoError(t, err, "in=%q", in)
		require.Equal(t, want, got, "in=%q", in)
	}
	_, err := ParseLevel("trace")
	require.Error(t, err)
}

func TestLogger_ConcurrentSafe(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(LevelDebug, true, &buf)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l.Info("event", "i", i)
		}(i)
	}
	wg.Wait()
	require.NotEmpty(t, buf.String())
}
