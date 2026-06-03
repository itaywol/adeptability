package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/pkg/adept"
)

// orchestratorRegressSidecarAdapter builds a per-skill adapter that emits a
// main file plus one sidecar. Defined inline (prefixed with the source
// basename) so it cannot collide with helpers added by other agents editing
// this package.
func orchestratorRegressSidecarAdapter(id, mainBytes, sideRel, sideBytes string) *mockAdapter {
	return &mockAdapter{
		spec: adept.HarnessSpec{ID: id, Kind: adept.KindPerSkill, OutputPath: "." + id + "/{id}.md"},
		render: rendererFunc(func(_ context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
			return adept.RenderOutput{
				Path:    filepath.Join("."+id, in.Skill.ID+".md"),
				Bytes:   []byte(mainBytes),
				Mode:    0o644,
				SkillID: in.Skill.ID,
				Sidecars: []adept.SideFile{
					{RelPath: sideRel, Bytes: []byte(sideBytes), Mode: 0o644},
				},
			}, nil
		}),
	}
}

// Regression for the high-severity bug where a byte-identical main file caused
// the materialize loop to `continue`, skipping the sidecar loop entirely. A
// drifted sidecar must be repaired on `sync` even when the main file matches.
func TestOrchestrator_Sync_RepairsDriftedSidecarWhenMainUnchanged(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	setHarnesses(t, p, "alpha")
	setHarnessMode(t, p, "alpha", adept.ModeCopy)

	a := orchestratorRegressSidecarAdapter("alpha", "MAIN\n", "helper.sh", "echo correct\n")
	orch := newOrch(t, a)

	// First sync writes main + sidecar.
	_, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)

	mainPath := filepath.Join(p.Root(), ".alpha", "skill-a.md")
	sidePath := filepath.Join(p.Root(), ".alpha", "helper.sh")
	require.FileExists(t, mainPath)
	require.FileExists(t, sidePath)

	// Corrupt ONLY the sidecar; leave the main file byte-identical.
	require.NoError(t, os.WriteFile(sidePath, []byte("echo CORRUPTED\n"), 0o644))

	// Re-sync without --force. The main file matches; previously this skipped
	// the sidecar loop and left the corruption in place.
	res, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)

	got, err := os.ReadFile(sidePath)
	require.NoError(t, err)
	require.Equal(t, "echo correct\n", string(got), "drifted sidecar must be repaired")

	// The main file should be reported skipped, the sidecar rewritten.
	require.Contains(t, res[0].Skipped, filepath.Join(".alpha", "skill-a.md"))
	require.Contains(t, res[0].Written, filepath.Join(".alpha", "helper.sh"))
}

// When both main and sidecar are byte-identical, neither should be rewritten:
// the per-sidecar skip must avoid needless churn.
func TestOrchestrator_Sync_SkipsUnchangedSidecar(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	setHarnesses(t, p, "alpha")
	setHarnessMode(t, p, "alpha", adept.ModeCopy)

	a := orchestratorRegressSidecarAdapter("alpha", "MAIN\n", "helper.sh", "echo correct\n")
	orch := newOrch(t, a)

	_, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)

	res, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Empty(t, res[0].Written, "nothing should be rewritten when main+sidecar match")
	require.Contains(t, res[0].Skipped, filepath.Join(".alpha", "helper.sh"))
}

// Regression for the swallowed Validate error: when drift computation fails,
// Sync must surface the error rather than reporting a clean (empty) DriftReport.
func TestOrchestrator_Sync_SurfacesValidateError(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	setHarnesses(t, p, "alpha")
	setHarnessMode(t, p, "alpha", adept.ModeCopy)

	a := perSkillAdapter("alpha", nil)
	a.valid = func(string, []adept.RenderOutput) (adept.DriftReport, error) {
		return adept.DriftReport{}, os.ErrInvalid
	}
	orch := newOrch(t, a)

	_, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.Error(t, err, "a failed drift computation must not be reported as clean")
	require.ErrorContains(t, err, "validate")
}

// orchestratorRegressFailLinker fails SymlinkOrCopy to exercise the
// write-then-rename rollback path: the existing harness file must NOT be
// destroyed when the replacement cannot be produced.
type orchestratorRegressFailLinker struct{}

func (orchestratorRegressFailLinker) Symlink(target, linkPath string) error { return os.ErrPermission }
func (orchestratorRegressFailLinker) SymlinkOrCopy(target, linkPath string, isDir bool) (adept.HarnessMode, error) {
	return "", os.ErrPermission
}
func (orchestratorRegressFailLinker) ReadSymlink(linkPath string) (string, error) {
	return os.Readlink(linkPath)
}
func (orchestratorRegressFailLinker) PathType(path string) fsutil.PathType {
	return fsutil.NewLinker(nil).PathType(path)
}

// Regression for the destroy-then-recreate symlink window: when SymlinkOrCopy
// fails, the pre-existing target file must remain intact (no destructive
// RemoveAll before the replacement is ready).
func TestOrchestrator_Sync_SymlinkFailureDoesNotDestroyTarget(t *testing.T) {
	root := t.TempDir()
	out := adept.RenderOutput{Path: filepath.Join(".alpha", "skill-a.md"), Bytes: []byte("NEW\n"), Mode: 0o644}
	absPath := filepath.Join(root, out.Path)

	// Pre-place an existing target.
	require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o755))
	require.NoError(t, os.WriteFile(absPath, []byte("EXISTING\n"), 0o644))

	o := &orchestrator{
		writer: fsutil.NewWriter(),
		linker: orchestratorRegressFailLinker{},
	}
	_, _, err := o.write(root, absPath, out, adept.ModeSymlink)
	require.Error(t, err)

	// The original file must still be present and unchanged.
	got, readErr := os.ReadFile(absPath)
	require.NoError(t, readErr, "target file must survive a failed symlink replacement")
	require.Equal(t, "EXISTING\n", string(got))
}
