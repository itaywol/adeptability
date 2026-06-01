package lockfile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func TestStore_Empty(t *testing.T) {
	s := NewStore(nil)
	lf := s.Empty()
	require.Equal(t, adept.LockSchemaVersion, lf.Schema)
	require.NotNil(t, lf.Skills)
	require.Empty(t, lf.Skills)
}

func TestStore_RoundTripEmpty(t *testing.T) {
	s := NewStore(nil)
	path := filepath.Join(t.TempDir(), "adeptability.lock.json")
	lf := s.Empty()
	require.NoError(t, s.Write(path, lf))
	got, err := s.Read(path)
	require.NoError(t, err)
	require.Equal(t, lf.Schema, got.Schema)
	require.Equal(t, len(lf.Skills), len(got.Skills))
}

func TestStore_RoundTripPopulated(t *testing.T) {
	s := NewStore(nil)
	path := filepath.Join(t.TempDir(), "adeptability.lock.json")
	lf := s.Empty()
	lf = s.SetEntry(lf, "alpha", adept.LockEntry{Version: 1, Hash: "sha256:aaa"})
	lf = s.SetEntry(lf, "beta", adept.LockEntry{Version: 2, Hash: "sha256:bbb", Targets: []string{"claude-code"}})
	lf = s.SetHarnessMode(lf, "claude-code", adept.ModeSymlink)
	lf = s.SetHarnessMode(lf, "codex", adept.ModeCopy)
	require.NoError(t, s.Write(path, lf))

	got, err := s.Read(path)
	require.NoError(t, err)
	require.Equal(t, adept.LockSchemaVersion, got.Schema)
	require.Equal(t, 2, got.Skills["beta"].Version)
	require.Equal(t, []string{"claude-code"}, got.Skills["beta"].Targets)
	require.ElementsMatch(t, []string{"claude-code", "codex"}, got.Harnesses)
	require.Equal(t, adept.ModeSymlink, got.HarnessModes["claude-code"])
	require.Equal(t, adept.ModeCopy, got.HarnessModes["codex"])
}

func TestStore_RejectsSchemaMismatch(t *testing.T) {
	s := NewStore(nil)
	path := filepath.Join(t.TempDir(), "lock.json")
	raw := []byte(`{"schema":1,"skills":{}}`)
	require.NoError(t, os.WriteFile(path, raw, 0o644))
	_, err := s.Read(path)
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrLockSchemaMismatch)
}

func TestStore_WriteRejectsBadSchema(t *testing.T) {
	s := NewStore(nil)
	path := filepath.Join(t.TempDir(), "lock.json")
	lf := &adept.LockFile{Schema: 99, Skills: map[string]adept.LockEntry{}}
	err := s.Write(path, lf)
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrLockSchemaMismatch)
}

func TestStore_WriteFillsSchemaIfZero(t *testing.T) {
	s := NewStore(nil)
	path := filepath.Join(t.TempDir(), "lock.json")
	lf := &adept.LockFile{Skills: map[string]adept.LockEntry{}}
	require.NoError(t, s.Write(path, lf))
	require.Equal(t, adept.LockSchemaVersion, lf.Schema)
}

func TestStore_StableKeyOrder(t *testing.T) {
	s := NewStore(nil)
	path := filepath.Join(t.TempDir(), "lock.json")
	lf := s.Empty()
	for _, id := range []string{"zeta", "alpha", "mu"} {
		lf = s.SetEntry(lf, id, adept.LockEntry{Version: 1, Hash: "h"})
	}
	require.NoError(t, s.Write(path, lf))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	idxAlpha := bytesIndex(raw, "alpha")
	idxMu := bytesIndex(raw, "mu")
	idxZeta := bytesIndex(raw, "zeta")
	require.True(t, idxAlpha >= 0 && idxAlpha < idxMu && idxMu < idxZeta,
		"map keys must be written in alphabetical order: got alpha=%d mu=%d zeta=%d",
		idxAlpha, idxMu, idxZeta)
}

func TestStore_AtomicWriteFailure_NoTempLeft(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "lock.json")
	var writeCalled bool
	failWrite := WriteFunc(func(path string, data []byte, mode os.FileMode) error {
		writeCalled = true
		// Simulate failure: don't write anything.
		return errors.New("simulated write failure")
	})
	s := NewStore(failWrite)
	lf := s.Empty()
	err := s.Write(target, lf)
	require.Error(t, err)
	require.True(t, writeCalled)
	// Target must not exist and no .tmp left in dir.
	_, err = os.Stat(target)
	require.True(t, os.IsNotExist(err))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestStore_GetHarnessMode_Default(t *testing.T) {
	s := NewStore(nil)
	require.Equal(t, adept.ModeSymlink, s.GetHarnessMode(nil, "claude-code"))
	lf := s.Empty()
	require.Equal(t, adept.ModeSymlink, s.GetHarnessMode(lf, "claude-code"))
	lf = s.SetHarnessMode(lf, "claude-code", adept.ModeCopy)
	require.Equal(t, adept.ModeCopy, s.GetHarnessMode(lf, "claude-code"))
	require.Equal(t, adept.ModeSymlink, s.GetHarnessMode(lf, "missing-harness"))
}

func TestStore_ReadInvalidJSON(t *testing.T) {
	s := NewStore(nil)
	path := filepath.Join(t.TempDir(), "lock.json")
	require.NoError(t, os.WriteFile(path, []byte("not-json"), 0o644))
	_, err := s.Read(path)
	require.Error(t, err)
}

func TestStore_ReadMissingFile(t *testing.T) {
	s := NewStore(nil)
	_, err := s.Read(filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, err)
}

func TestStore_DoesNotMutateInputWhenNil(t *testing.T) {
	s := NewStore(nil)
	lf := s.SetEntry(nil, "x", adept.LockEntry{Version: 1, Hash: "h"})
	require.NotNil(t, lf)
	require.Equal(t, adept.LockSchemaVersion, lf.Schema)
	require.Equal(t, 1, lf.Skills["x"].Version)
}

func TestStore_SetHarnessMode_NoDuplicates(t *testing.T) {
	s := NewStore(nil)
	lf := s.Empty()
	lf = s.SetHarnessMode(lf, "claude-code", adept.ModeSymlink)
	lf = s.SetHarnessMode(lf, "claude-code", adept.ModeCopy)
	count := 0
	for _, h := range lf.Harnesses {
		if h == "claude-code" {
			count++
		}
	}
	require.Equal(t, 1, count)
	require.Equal(t, adept.ModeCopy, s.GetHarnessMode(lf, "claude-code"))
}

func TestStore_RoundTripPreservesUnknownEntryFields(t *testing.T) {
	s := NewStore(nil)
	path := filepath.Join(t.TempDir(), "lock.json")
	lf := s.Empty()
	lf = s.SetEntry(lf, "sig-skill", adept.LockEntry{
		Version: 4, Hash: "sha256:zzz",
		Signature: "ed25519:abc", UpdatedAt: "2024-01-01T00:00:00Z",
	})
	require.NoError(t, s.Write(path, lf))
	got, err := s.Read(path)
	require.NoError(t, err)
	require.Equal(t, "ed25519:abc", got.Skills["sig-skill"].Signature)
	require.Equal(t, "2024-01-01T00:00:00Z", got.Skills["sig-skill"].UpdatedAt)
}

// bytesIndex returns the index of needle in haystack. Helper for ordering
// assertions without importing strings.
func bytesIndex(haystack []byte, needle string) int {
	target := []byte(needle)
	for i := 0; i+len(target) <= len(haystack); i++ {
		match := true
		for j := range target {
			if haystack[i+j] != target[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// Compile-time check: WriteFunc satisfies its expected shape.
var _ WriteFunc = func(string, []byte, os.FileMode) error { return nil }

// Compile-time check: encoded LockFile decodes back equal-ish.
func TestStore_EncodingShape(t *testing.T) {
	s := NewStore(nil)
	path := filepath.Join(t.TempDir(), "lock.json")
	lf := s.Empty()
	lf.Org = &adept.OrgRef{Remote: "git@example.com:org/skills.git", Ref: "main"}
	lf.Adapters = []string{"custom-harness"}
	require.NoError(t, s.Write(path, lf))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var generic map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &generic))
	_, ok := generic["org"]
	require.True(t, ok)
	_, ok = generic["adapters"]
	require.True(t, ok)
}
