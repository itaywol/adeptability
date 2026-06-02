package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func newTestStore() Store {
	return NewStore(nil)
}

func writePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), adept.ConfigFileName)
}

func TestStore_Empty(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	cfg := s.Empty()
	require.NotNil(t, cfg)
	require.Equal(t, adept.ConfigSchemaVersion, cfg.Schema)
	require.Empty(t, cfg.Harnesses)
	require.Empty(t, cfg.Mode)
	require.Nil(t, cfg.Library)
}

func TestStore_ReadMissingReturnsEmpty(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	cfg, err := s.Read(filepath.Join(t.TempDir(), "does-not-exist.json"))
	require.NoError(t, err)
	require.Equal(t, adept.ConfigSchemaVersion, cfg.Schema)
}

func TestStore_WriteEmptyRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	path := writePath(t)
	require.NoError(t, s.Write(path, s.Empty()))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(data)
	require.True(t, strings.HasSuffix(got, "\n"), "missing trailing newline")
	require.Contains(t, got, `"schema": 1`)
	require.NotContains(t, got, "harnesses")
	require.NotContains(t, got, `"mode"`)
	require.NotContains(t, got, `"library"`)

	back, err := s.Read(path)
	require.NoError(t, err)
	require.Equal(t, adept.ConfigSchemaVersion, back.Schema)
}

func TestStore_WritePopulatedRoundTrip(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	path := writePath(t)
	in := &adept.Config{
		Schema:    adept.ConfigSchemaVersion,
		Harnesses: []string{"claude-code", "cursor"},
		Mode:      adept.ModeCopy,
		Library:   &adept.LibraryRef{Remote: "https://example.com/org.git", Ref: "v1"},
	}
	require.NoError(t, s.Write(path, in))

	got, err := s.Read(path)
	require.NoError(t, err)
	require.Equal(t, in.Schema, got.Schema)
	require.Equal(t, in.Harnesses, got.Harnesses)
	require.Equal(t, in.Mode, got.Mode)
	require.Equal(t, in.Library, got.Library)
}

func TestStore_WriteCanonicalFieldOrder(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	path := writePath(t)
	in := &adept.Config{
		Schema:    adept.ConfigSchemaVersion,
		Harnesses: []string{"claude-code"},
		Mode:      adept.ModeSymlink,
		Library:   &adept.LibraryRef{Remote: "https://x.example/org"},
	}
	require.NoError(t, s.Write(path, in))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	body := string(data)
	order := []string{`"schema"`, `"harnesses"`, `"mode"`, `"library"`}
	prev := -1
	for _, key := range order {
		idx := strings.Index(body, key)
		require.NotEqual(t, -1, idx, "missing key %s", key)
		require.Greater(t, idx, prev, "key %s out of order", key)
		prev = idx
	}
}

func TestStore_RejectsSchemaMismatch(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	path := writePath(t)
	require.NoError(t, os.WriteFile(path, []byte(`{"schema":2}`), 0o644))
	_, err := s.Read(path)
	require.Error(t, err)
	require.True(t, errors.Is(err, adept.ErrLockSchemaMismatch), "expected ErrLockSchemaMismatch, got %v", err)
}

func TestStore_RejectsSchemaZero(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	path := writePath(t)
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o644))
	_, err := s.Read(path)
	require.Error(t, err)
	require.True(t, errors.Is(err, adept.ErrLockSchemaMismatch))
}

func TestStore_RejectsInvalidMode(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	path := writePath(t)
	require.NoError(t, os.WriteFile(path, []byte(`{"schema":1,"mode":"bogus"}`), 0o644))
	_, err := s.Read(path)
	require.Error(t, err)
	require.False(t, errors.Is(err, adept.ErrLockSchemaMismatch), "wrong sentinel for invalid mode")
}

func TestStore_RejectsUnknownTopLevelField(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	path := writePath(t)
	// "version" is not a valid top-level key; schema must reject.
	require.NoError(t, os.WriteFile(path, []byte(`{"schema":1,"version":"oops"}`), 0o644))
	_, err := s.Read(path)
	require.Error(t, err)
}

func TestStore_GetModeDefault(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	cfg := s.Empty()
	require.Equal(t, adept.ModeSymlink, s.GetMode(cfg))
	require.Equal(t, adept.ModeSymlink, s.GetMode(nil))
}

func TestStore_SetModeReplaces(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	cfg := s.Empty()
	s.SetMode(cfg, adept.ModeCopy)
	require.Equal(t, adept.ModeCopy, s.GetMode(cfg))
	s.SetMode(cfg, adept.ModeSymlink)
	require.Equal(t, adept.ModeSymlink, s.GetMode(cfg))
}

func TestStore_SetModeNilSafe(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	require.Nil(t, s.SetMode(nil, adept.ModeSymlink))
}

func TestStore_WriteUsesInjectedWriter(t *testing.T) {
	t.Parallel()
	called := false
	w := WriteFunc(func(path string, data []byte, mode os.FileMode) error {
		called = true
		return os.WriteFile(path, data, mode)
	})
	s := NewStore(w)
	path := writePath(t)
	require.NoError(t, s.Write(path, s.Empty()))
	require.True(t, called, "injected writer should be invoked")
}

func TestStore_WriteRejectsNil(t *testing.T) {
	t.Parallel()
	s := newTestStore()
	err := s.Write(writePath(t), nil)
	require.Error(t, err)
}
