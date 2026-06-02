package locks

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNew_DefaultsSchema(t *testing.T) {
	l := New()
	require.Equal(t, SchemaVersion, l.Schema)
	require.Empty(t, l.External)
}

func TestLoad_MissingReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adept.lock.json")
	l, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, SchemaVersion, l.Schema)
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "adept.lock.json")
	l := New()
	l.Set("find-skills", Entry{
		Source:      SourceSkillsSh,
		Slug:        "vercel-labs/skills/find-skills",
		Repo:        "https://github.com/vercel-labs/skills",
		SHA:         "abc123",
		SkillPath:   "skills/find-skills",
		ContentHash: "sha256:deadbeef",
		InstalledAt: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, Save(path, l))

	back, err := Load(path)
	require.NoError(t, err)
	require.Len(t, back.External, 1)
	got, ok := back.Get("find-skills")
	require.True(t, ok)
	require.Equal(t, SourceSkillsSh, got.Source)
	require.Equal(t, "abc123", got.SHA)
	require.Equal(t, "skills/find-skills", got.SkillPath)
}

func TestSave_AtomicWriteLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "adept.lock.json")
	require.NoError(t, Save(path, New()))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".tmp", "atomic write should clean its temp file")
	}
}

func TestLoad_RejectsSchemaMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "adept.lock.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"schema":99}`), 0o644))
	_, err := Load(path)
	require.Error(t, err)
}

func TestDelete(t *testing.T) {
	l := New()
	l.Set("a", Entry{SHA: "1"})
	l.Set("b", Entry{SHA: "2"})
	l.Delete("a")
	require.Len(t, l.External, 1)
	_, ok := l.Get("a")
	require.False(t, ok)
}

func TestIDs_Sorted(t *testing.T) {
	l := New()
	l.Set("zeta", Entry{SHA: "z"})
	l.Set("alpha", Entry{SHA: "a"})
	require.Equal(t, []string{"alpha", "zeta"}, l.IDs())
}
