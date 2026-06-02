package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/config"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/pkg/adept"
)

func newProject(t *testing.T) (Project, string) {
	t.Helper()
	root := t.TempDir()
	return New(root, canonical.NewParser(), hash.NewHasher(), config.NewStore(nil), fsutil.NewWriter()), root
}

func sampleSkill(id string) *adept.Skill {
	return &adept.Skill{
		ID:          id,
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
	cfg, err := p.Config()
	require.NoError(t, err)
	require.Equal(t, adept.ConfigSchemaVersion, cfg.Schema)
	require.Empty(t, cfg.Harnesses)
}

func TestProject_InstallSkill_WritesFiles(t *testing.T) {
	p, root := newProject(t)
	skill := sampleSkill("skill-a")
	require.NoError(t, p.InstallSkill(skill, nil))

	require.FileExists(t, filepath.Join(root, adept.BaseDirName, adept.SkillsDirName, "skill-a", adept.SkillFileName))
	// New model: no lockfile at the project root.
	_, err := os.Stat(filepath.Join(root, "adeptability.lock.json"))
	require.True(t, os.IsNotExist(err), "project must not write a lockfile")
}

func TestProject_HashSkill_NonEmptyAfterInstall(t *testing.T) {
	p, _ := newProject(t)
	require.NoError(t, p.InstallSkill(sampleSkill("skill-a"), nil))
	h, err := p.HashSkill("skill-a")
	require.NoError(t, err)
	require.NotEmpty(t, h)
}

func TestProject_HashSkill_EmptyOnMissing(t *testing.T) {
	p, _ := newProject(t)
	h, err := p.HashSkill("ghost")
	require.NoError(t, err)
	require.Empty(t, h)
}

func TestProject_GetSkill_RoundTrip(t *testing.T) {
	p, _ := newProject(t)
	require.NoError(t, p.InstallSkill(sampleSkill("skill-a"), nil))
	got, err := p.GetSkill("skill-a")
	require.NoError(t, err)
	require.Equal(t, "skill-a", got.ID)
	require.Contains(t, got.Body, "The body.")
}

func TestProject_ListSkills_Sorted(t *testing.T) {
	p, _ := newProject(t)
	for _, id := range []string{"zeta", "alpha", "kappa"} {
		require.NoError(t, p.InstallSkill(sampleSkill(id), nil))
	}
	list, err := p.ListSkills()
	require.NoError(t, err)
	require.Len(t, list, 3)
	require.Equal(t, "alpha", list[0].ID)
	require.Equal(t, "kappa", list[1].ID)
	require.Equal(t, "zeta", list[2].ID)
}

func TestProject_UninstallSkill_Success(t *testing.T) {
	p, root := newProject(t)
	require.NoError(t, p.InstallSkill(sampleSkill("skill-a"), nil))
	require.True(t, p.HasSkill("skill-a"))
	require.NoError(t, p.UninstallSkill("skill-a"))
	require.False(t, p.HasSkill("skill-a"))
	_, err := os.Stat(filepath.Join(root, adept.BaseDirName, adept.SkillsDirName, "skill-a"))
	require.True(t, os.IsNotExist(err))
}

// FRICTION BUG 2 — uninstall of a missing skill must surface ErrSkillNotFound.
func TestProject_UninstallSkill_MissingReturnsTypedError(t *testing.T) {
	p, _ := newProject(t)
	err := p.UninstallSkill("never-installed")
	require.ErrorIs(t, err, adept.ErrSkillNotFound)
}

func TestProject_SaveConfig_RoundTrip(t *testing.T) {
	p, _ := newProject(t)
	desired := &adept.Config{
		Schema:    adept.ConfigSchemaVersion,
		Harnesses: []string{"claude-code", "cursor"},
		Mode:      adept.ModeCopy,
	}
	require.NoError(t, p.SaveConfig(desired))
	loaded, err := p.Config()
	require.NoError(t, err)
	require.Equal(t, desired.Harnesses, loaded.Harnesses)
	require.Equal(t, desired.Mode, loaded.Mode)
}

func TestProject_InstallSkill_RejectsEmptyID(t *testing.T) {
	p, _ := newProject(t)
	err := p.InstallSkill(&adept.Skill{}, nil)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestProject_InstallSkill_RejectsSidecarEscape(t *testing.T) {
	p, _ := newProject(t)
	err := p.InstallSkill(sampleSkill("skill-a"), []adept.SkillFile{{RelPath: "../bad.txt"}})
	require.Error(t, err)
}

func TestProject_BaseDirForSkill_ReturnsCorrectPath(t *testing.T) {
	p, root := newProject(t)
	got := p.BaseDirForSkill("skill-a")
	require.Equal(t, filepath.Join(root, adept.BaseDirName, adept.BaseSnapDir, "skill-a"), got)
	require.False(t, p.HasBaseSnapshot("skill-a"))
}

func TestProject_BaseSnapshotsDir_ReturnsRootOfStore(t *testing.T) {
	p, root := newProject(t)
	require.Equal(t, filepath.Join(root, adept.BaseDirName, adept.BaseSnapDir), p.BaseSnapshotsDir())
}

func TestProject_InstallSkill_WritesBaseSnapshot(t *testing.T) {
	p, root := newProject(t)
	require.NoError(t, p.InstallSkill(sampleSkill("skill-a"), nil))
	require.True(t, p.HasBaseSnapshot("skill-a"))
	want := filepath.Join(root, adept.BaseDirName, adept.BaseSnapDir, "skill-a", adept.SkillFileName)
	require.FileExists(t, want)
	// Snapshot content must match the canonical SKILL.md byte-for-byte.
	canonicalBytes, err := os.ReadFile(filepath.Join(root, adept.BaseDirName, adept.SkillsDirName, "skill-a", adept.SkillFileName))
	require.NoError(t, err)
	snap, err := os.ReadFile(want)
	require.NoError(t, err)
	require.Equal(t, canonicalBytes, snap)
}

func TestProject_InstallSkill_WritesBaseSnapshotWithSidecars(t *testing.T) {
	p, root := newProject(t)
	files := []adept.SkillFile{
		{RelPath: "scripts/run.sh", Mode: 0o644, Bytes: []byte("echo hi\n")},
		{RelPath: "references/notes.md", Mode: 0o644, Bytes: []byte("notes\n")},
	}
	require.NoError(t, p.InstallSkill(sampleSkill("skill-a"), files))
	require.True(t, p.HasBaseSnapshot("skill-a"))
	baseRoot := filepath.Join(root, adept.BaseDirName, adept.BaseSnapDir, "skill-a")
	require.FileExists(t, filepath.Join(baseRoot, "SKILL.md"))
	require.FileExists(t, filepath.Join(baseRoot, "scripts", "run.sh"))
	require.FileExists(t, filepath.Join(baseRoot, "references", "notes.md"))
}

func TestProject_HashBase_MatchesHashSkillAfterInstall(t *testing.T) {
	p, _ := newProject(t)
	require.NoError(t, p.InstallSkill(sampleSkill("skill-a"), nil))
	hSkill, err := p.HashSkill("skill-a")
	require.NoError(t, err)
	hBase, err := p.HashBase("skill-a")
	require.NoError(t, err)
	require.Equal(t, hSkill, hBase, "base snapshot should hash to the just-installed content")
}

func TestProject_SnapshotBase_OverwritesPriorSnapshot(t *testing.T) {
	p, root := newProject(t)
	skill := sampleSkill("skill-a")
	require.NoError(t, p.InstallSkill(skill, []adept.SkillFile{{RelPath: "old.md", Bytes: []byte("v1\n")}}))
	require.FileExists(t, filepath.Join(root, adept.BaseDirName, adept.BaseSnapDir, "skill-a", "old.md"))

	// Re-install with a different sidecar list. The base snapshot must
	// mirror the new on-disk tree, with old.md gone.
	require.NoError(t, os.Remove(filepath.Join(root, adept.BaseDirName, adept.SkillsDirName, "skill-a", "old.md")))
	require.NoError(t, p.InstallSkill(skill, []adept.SkillFile{{RelPath: "new.md", Bytes: []byte("v2\n")}}))
	require.FileExists(t, filepath.Join(root, adept.BaseDirName, adept.BaseSnapDir, "skill-a", "new.md"))
	_, err := os.Stat(filepath.Join(root, adept.BaseDirName, adept.BaseSnapDir, "skill-a", "old.md"))
	require.True(t, os.IsNotExist(err))
}

func TestProject_SnapshotBase_RejectsEmptyID(t *testing.T) {
	p, _ := newProject(t)
	err := p.SnapshotBase("")
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestProject_SnapshotBase_RejectsMissingSkill(t *testing.T) {
	p, _ := newProject(t)
	err := p.SnapshotBase("nope")
	require.ErrorIs(t, err, adept.ErrSkillNotFound)
}

func TestProject_UninstallSkill_RemovesBaseSnapshot(t *testing.T) {
	p, root := newProject(t)
	require.NoError(t, p.InstallSkill(sampleSkill("skill-a"), nil))
	require.True(t, p.HasBaseSnapshot("skill-a"))
	require.NoError(t, p.UninstallSkill("skill-a"))
	require.False(t, p.HasBaseSnapshot("skill-a"))
	_, err := os.Stat(filepath.Join(root, adept.BaseDirName, adept.BaseSnapDir, "skill-a"))
	require.True(t, os.IsNotExist(err))
}
