package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/internal/lockfile"
	"github.com/itaywol/adeptability/pkg/adept"
)

func newProject(t *testing.T) (Project, string) {
	t.Helper()
	root := t.TempDir()
	return New(root, canonical.NewParser(), hash.NewHasher(), lockfile.NewStore(nil), fsutil.NewWriter()), root
}

func sampleSkill(id string) *adept.Skill {
	return &adept.Skill{
		ID:          id,
		Version:     2,
		Description: "desc " + id,
		Activation:  adept.ActivationAgent,
		Body:        "# " + id + "\n\nThe body.\n",
	}
}

func TestProject_EmptyState(t *testing.T) {
	p, _ := newProject(t)
	require.False(t, p.HasSkill("skill-a"))
	list, err := p.ListSkills()
	require.NoError(t, err)
	require.Empty(t, list)
	lf, err := p.Lock()
	require.NoError(t, err)
	require.Equal(t, adept.LockSchemaVersion, lf.Schema)
	require.Empty(t, lf.Skills)
}

func TestProject_InstallSkill_WritesFilesAndLockfile(t *testing.T) {
	p, root := newProject(t)
	skill := sampleSkill("skill-a")
	entry := adept.LockEntry{Version: 2, Hash: "sha256:cafebabe"}
	require.NoError(t, p.InstallSkill(skill, nil, entry))

	require.FileExists(t, filepath.Join(root, adept.BaseDirName, adept.SkillsDirName, "skill-a", adept.SkillFileName))
	require.FileExists(t, filepath.Join(root, adept.LockFileName))

	lf, err := p.Lock()
	require.NoError(t, err)
	got, ok := lf.Skills["skill-a"]
	require.True(t, ok)
	require.Equal(t, 2, got.Version)
	require.Equal(t, "sha256:cafebabe", got.Hash)
	require.NotEmpty(t, got.UpdatedAt)
}

func TestProject_InstallSkill_ComputesHashWhenLockEntryEmpty(t *testing.T) {
	p, _ := newProject(t)
	skill := sampleSkill("skill-a")
	require.NoError(t, p.InstallSkill(skill, nil, adept.LockEntry{}))
	lf, err := p.Lock()
	require.NoError(t, err)
	got := lf.Skills["skill-a"]
	require.NotEmpty(t, got.Hash)
	require.Equal(t, 2, got.Version)
}

func TestProject_GetSkill_RoundTrip(t *testing.T) {
	p, _ := newProject(t)
	require.NoError(t, p.InstallSkill(sampleSkill("skill-a"), nil, adept.LockEntry{Version: 2, Hash: "sha256:x"}))
	got, err := p.GetSkill("skill-a")
	require.NoError(t, err)
	require.Equal(t, "skill-a", got.ID)
	require.Equal(t, 2, got.Version)
	require.Contains(t, got.Body, "The body.")
}

func TestProject_ListSkills_Sorted(t *testing.T) {
	p, _ := newProject(t)
	for _, id := range []string{"zeta", "alpha", "kappa"} {
		require.NoError(t, p.InstallSkill(sampleSkill(id), nil, adept.LockEntry{Version: 1, Hash: "h"}))
	}
	list, err := p.ListSkills()
	require.NoError(t, err)
	require.Len(t, list, 3)
	require.Equal(t, "alpha", list[0].ID)
	require.Equal(t, "kappa", list[1].ID)
	require.Equal(t, "zeta", list[2].ID)
}

func TestProject_UninstallSkill(t *testing.T) {
	p, root := newProject(t)
	require.NoError(t, p.InstallSkill(sampleSkill("skill-a"), nil, adept.LockEntry{Version: 1, Hash: "h"}))
	require.True(t, p.HasSkill("skill-a"))
	require.NoError(t, p.UninstallSkill("skill-a"))
	require.False(t, p.HasSkill("skill-a"))
	lf, err := p.Lock()
	require.NoError(t, err)
	_, present := lf.Skills["skill-a"]
	require.False(t, present)
	_, err = os.Stat(filepath.Join(root, adept.BaseDirName, adept.SkillsDirName, "skill-a"))
	require.True(t, os.IsNotExist(err))
}

func TestProject_Lock_RoundTrip(t *testing.T) {
	p, _ := newProject(t)
	desired := &adept.LockFile{
		Schema:    adept.LockSchemaVersion,
		Harnesses: []string{"claude-code", "cursor"},
		HarnessModes: map[string]adept.HarnessMode{
			"claude-code": adept.ModeSymlink,
			"cursor":      adept.ModeCopy,
		},
		Skills: map[string]adept.LockEntry{
			"skill-a": {Version: 1, Hash: "h1"},
		},
	}
	require.NoError(t, p.SaveLock(desired))
	loaded, err := p.Lock()
	require.NoError(t, err)
	require.Equal(t, desired.Harnesses, loaded.Harnesses)
	require.Equal(t, desired.HarnessModes["cursor"], loaded.HarnessModes["cursor"])
	require.Equal(t, "h1", loaded.Skills["skill-a"].Hash)
}

func TestProject_InstallSkill_RejectsEmptyID(t *testing.T) {
	p, _ := newProject(t)
	err := p.InstallSkill(&adept.Skill{Version: 1}, nil, adept.LockEntry{})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestProject_InstallSkill_RejectsSidecarEscape(t *testing.T) {
	p, _ := newProject(t)
	err := p.InstallSkill(sampleSkill("skill-a"), []adept.SkillFile{{RelPath: "../bad.txt"}}, adept.LockEntry{})
	require.Error(t, err)
}
