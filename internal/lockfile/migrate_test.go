package lockfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func TestMigrateFromSkillbook_Converts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skillbook.lock.json")
	legacy := []byte(`{
	  "schema": 1,
	  "skills": {
		"alpha": {"version": 1, "hash": "sha256:aaa", "updatedAt": "2024-05-01T00:00:00Z"},
		"beta":  {"version": 7, "hash": "sha256:bbb"}
	  },
	  "harnesses": ["claude-code", "codex"],
	  "harnessModes": {"claude-code": "symlink", "codex": "copy"}
	}`)
	require.NoError(t, os.WriteFile(path, legacy, 0o644))

	out, err := MigrateFromSkillbook(path)
	require.NoError(t, err)
	require.Equal(t, adept.LockSchemaVersion, out.Schema)
	require.Equal(t, 1, out.Skills["alpha"].Version)
	require.Equal(t, "sha256:aaa", out.Skills["alpha"].Hash)
	require.Equal(t, "2024-05-01T00:00:00Z", out.Skills["alpha"].UpdatedAt)
	require.Empty(t, out.Skills["alpha"].Targets)
	require.Equal(t, 7, out.Skills["beta"].Version)
	require.ElementsMatch(t, []string{"claude-code", "codex"}, out.Harnesses)
	require.Equal(t, adept.ModeSymlink, out.HarnessModes["claude-code"])
	require.Equal(t, adept.ModeCopy, out.HarnessModes["codex"])
}

func TestMigrateFromSkillbook_RejectsWrongSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"schema":2,"skills":{}}`), 0o644))
	_, err := MigrateFromSkillbook(path)
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrLockSchemaMismatch)
}

func TestMigrateFromSkillbook_MissingFile(t *testing.T) {
	_, err := MigrateFromSkillbook(filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, err)
}

func TestMigrateFromSkillbook_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o644))
	_, err := MigrateFromSkillbook(path)
	require.Error(t, err)
}
