package library

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/internal/lockfile"
	"github.com/itaywol/adeptability/pkg/adept"
)

func newLib(t *testing.T) (Library, string) {
	t.Helper()
	root := t.TempDir()
	parser := canonical.NewParser()
	hasher := hash.NewHasher()
	store := lockfile.NewStore(nil)
	w := fsutil.NewWriter()
	return New(root, parser, hasher, store, w), root
}

func sampleSkill(id string) *adept.Skill {
	return &adept.Skill{
		ID:          id,
		Version:     1,
		Description: "desc for " + id,
		Activation:  adept.ActivationAgent,
		Body:        "# " + id + "\n\nBody for " + id + ".\n",
	}
}

func TestDefaultRoot_UsesEnvVar(t *testing.T) {
	t.Setenv(adept.LibraryEnvVar, "/tmp/override-adept-lib")
	require.Equal(t, "/tmp/override-adept-lib", DefaultRoot())
}

func TestDefaultRoot_UsesHomeFallback(t *testing.T) {
	t.Setenv(adept.LibraryEnvVar, "")
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, adept.DefaultLibraryDir), DefaultRoot())
}

func TestLibrary_EmptyHasNoSkills(t *testing.T) {
	lib, _ := newLib(t)
	require.False(t, lib.HasSkill("skill-a"))
	list, err := lib.ListSkills()
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestLibrary_AddSkill_WritesFiles(t *testing.T) {
	lib, root := newLib(t)
	skill := sampleSkill("skill-a")
	require.NoError(t, lib.AddSkill(skill, nil))
	skillPath := filepath.Join(root, adept.SkillsDirName, "skill-a", adept.SkillFileName)
	require.FileExists(t, skillPath)
	require.FileExists(t, filepath.Join(root, adept.LockFileName))
	require.True(t, lib.HasSkill("skill-a"))
}

func TestLibrary_AddSkill_RoundTrip(t *testing.T) {
	lib, _ := newLib(t)
	skill := sampleSkill("skill-a")
	require.NoError(t, lib.AddSkill(skill, nil))
	loaded, err := lib.GetSkill("skill-a")
	require.NoError(t, err)
	require.Equal(t, "skill-a", loaded.ID)
	require.Equal(t, 1, loaded.Version)
	require.Equal(t, "desc for skill-a", loaded.Description)
	require.Contains(t, loaded.Body, "Body for skill-a")
}

func TestLibrary_AddSkill_BumpsVersionOnChange(t *testing.T) {
	lib, _ := newLib(t)
	first := sampleSkill("skill-a")
	require.NoError(t, lib.AddSkill(first, nil))

	// Same content: no bump.
	again := sampleSkill("skill-a")
	require.NoError(t, lib.AddSkill(again, nil))
	loaded, err := lib.GetSkill("skill-a")
	require.NoError(t, err)
	require.Equal(t, 1, loaded.Version)

	// Modified body: version bumps to 2.
	changed := sampleSkill("skill-a")
	changed.Body = "# skill-a\n\nNew body text.\n"
	require.NoError(t, lib.AddSkill(changed, nil))
	loaded, err = lib.GetSkill("skill-a")
	require.NoError(t, err)
	require.Equal(t, 2, loaded.Version)
}

func TestLibrary_AddSkill_PersistsSidecars(t *testing.T) {
	lib, root := newLib(t)
	skill := sampleSkill("skill-a")
	files := []adept.SkillFile{
		{RelPath: "references/howto.md", Bytes: []byte("howto"), Mode: 0o644},
		{RelPath: "scripts/run.sh", Bytes: []byte("#!/bin/sh\n"), Mode: 0o755},
	}
	require.NoError(t, lib.AddSkill(skill, files))
	require.FileExists(t, filepath.Join(root, adept.SkillsDirName, "skill-a", "references/howto.md"))
	require.FileExists(t, filepath.Join(root, adept.SkillsDirName, "skill-a", "scripts/run.sh"))

	loaded, err := lib.GetSkill("skill-a")
	require.NoError(t, err)
	require.Len(t, loaded.Files, 2)
	rels := []string{loaded.Files[0].RelPath, loaded.Files[1].RelPath}
	sort.Strings(rels)
	require.Equal(t, []string{"references/howto.md", "scripts/run.sh"}, rels)
}

func TestLibrary_AddSkill_RejectsEscapingSidecar(t *testing.T) {
	lib, _ := newLib(t)
	skill := sampleSkill("skill-a")
	files := []adept.SkillFile{{RelPath: "../escape.txt", Bytes: []byte("nope")}}
	err := lib.AddSkill(skill, files)
	require.Error(t, err)
}

func TestLibrary_ListSkills_Sorted(t *testing.T) {
	lib, _ := newLib(t)
	for _, id := range []string{"zebra", "alpha", "mango"} {
		require.NoError(t, lib.AddSkill(sampleSkill(id), nil))
	}
	list, err := lib.ListSkills()
	require.NoError(t, err)
	require.Len(t, list, 3)
	require.Equal(t, "alpha", list[0].ID)
	require.Equal(t, "mango", list[1].ID)
	require.Equal(t, "zebra", list[2].ID)
}

func TestLibrary_RemoveSkill(t *testing.T) {
	lib, root := newLib(t)
	require.NoError(t, lib.AddSkill(sampleSkill("skill-a"), nil))
	require.True(t, lib.HasSkill("skill-a"))
	require.NoError(t, lib.RemoveSkill("skill-a"))
	require.False(t, lib.HasSkill("skill-a"))
	_, err := os.Stat(filepath.Join(root, adept.SkillsDirName, "skill-a"))
	require.True(t, os.IsNotExist(err))
}

func TestLibrary_GetSkill_NotFound(t *testing.T) {
	lib, _ := newLib(t)
	_, err := lib.GetSkill("missing")
	require.ErrorIs(t, err, adept.ErrSkillNotFound)
}

func TestLibrary_RemoveSkill_IdempotentOnMissing(t *testing.T) {
	lib, _ := newLib(t)
	require.NoError(t, lib.RemoveSkill("never-added"))
}

func TestLibrary_AddSkill_RejectsEmptyID(t *testing.T) {
	lib, _ := newLib(t)
	err := lib.AddSkill(&adept.Skill{Version: 1, Description: "x"}, nil)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}
