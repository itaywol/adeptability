package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

// runMigrate executes `adept migrate` and returns stdout.
func runMigrate(t *testing.T, d *Deps) (string, error) {
	t.Helper()
	cmd := newMigrateCmd(d)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	err := cmd.Execute()
	return out.String(), err
}

// TestMigrateLocalizesLibraries: a project declares library "team" whose
// remote is only cloned into the machine store (never scope-local). `adept
// migrate` should clone it into the project-local libs root, leave the
// machine-store copy untouched, write the scope gitignore, and be a no-op
// (idempotent) on a second run.
func TestMigrateLocalizesLibraries(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)

	upstream := t.TempDir()
	gitRun(t, upstream, "init", "-b", "main")
	gitRun(t, upstream, "config", "user.email", "t@example.com")
	gitRun(t, upstream, "config", "user.name", "Test")
	writeSkillFile(t, upstream, "demo", "first skill")
	gitRun(t, upstream, "add", ".")
	gitRun(t, upstream, "commit", "-m", "init")

	// Clone into the machine store only — never the project-local libs root.
	storeRoot, err := d.ResolveLibrariesRoot()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(storeRoot, 0o755))
	machineClone := filepath.Join(storeRoot, "team")
	gitRun(t, storeRoot, "clone", upstream, machineClone)

	p := initProject(t, d, projRoot, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "team", Remote: upstream, Ref: "main"}},
	})

	out, err := runMigrate(t, d)
	require.NoError(t, err)
	require.Contains(t, out, "team: localized")

	projectClone := filepath.Join(d.LibsRootFor(p), "team")
	require.FileExists(t, filepath.Join(projectClone, adept.SkillsDirName, "demo", adept.SkillFileName))

	// Machine store copy is never deleted.
	require.FileExists(t, filepath.Join(machineClone, adept.SkillsDirName, "demo", adept.SkillFileName))

	// Scope gitignore is written.
	gitignore, err := os.ReadFile(filepath.Join(p.BaseDir(), ".gitignore"))
	require.NoError(t, err)
	require.Contains(t, string(gitignore), "libs/")

	// Second run is idempotent: already-local, no error.
	out, err = runMigrate(t, d)
	require.NoError(t, err)
	require.Contains(t, out, "team: already local")
}

// TestMigrateBrokenPartialCloneDoesNotShortCircuit: a dest directory left
// behind by an interrupted clone (exists on disk, has stray content, but has
// no .git) must NOT be reported as "already local" — that would wedge the
// library forever. It must fall through to CloneOrPull, which either repairs
// it or fails with a clear, actionable error (never a silent success).
func TestMigrateBrokenPartialCloneDoesNotShortCircuit(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)

	upstream := t.TempDir()
	gitRun(t, upstream, "init", "-b", "main")
	gitRun(t, upstream, "config", "user.email", "t@example.com")
	gitRun(t, upstream, "config", "user.name", "Test")
	writeSkillFile(t, upstream, "demo", "first skill")
	gitRun(t, upstream, "add", ".")
	gitRun(t, upstream, "commit", "-m", "init")

	p := initProject(t, d, projRoot, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "team", Remote: upstream, Ref: "main"}},
	})

	// Simulate an interrupted `adept migrate`: dest exists with a stray file
	// but was never actually cloned (no .git).
	dest := filepath.Join(d.LibsRootFor(p), "team")
	require.NoError(t, os.MkdirAll(dest, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "stray.txt"), []byte("leftover"), 0o644))

	// CloneOrPull's underlying `git clone` refuses to clone into an existing
	// non-empty, non-repo directory (verified: exit 128, "already exists and
	// is not an empty directory") — it does not repair in place. migrate
	// must surface that failure with an actionable message, not silently
	// report "already local" and exit 0.
	out, err := runMigrate(t, d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "team")
	require.Contains(t, err.Error(), dest)
	require.Contains(t, err.Error(), "not a valid git clone")
	require.NotContains(t, out, "already local")
}

// TestMigrateRejectsGlobalScope: global scope IS the machine store, so there
// is nothing to localize — `adept migrate --global` must error rather than
// silently no-op.
func TestMigrateRejectsGlobalScope(t *testing.T) {
	libRoot := t.TempDir()
	t.Setenv("ADEPT_LIBRARY", libRoot)

	d, err := NewDeps(&GlobalFlags{Global: true}, BuildInfo{})
	require.NoError(t, err)

	_, err = runMigrate(t, d)
	require.Error(t, err)
	require.Contains(t, err.Error(), "migrate applies to project scope only")
}
