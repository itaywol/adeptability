package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

// ---------- Finding 1: walk-up must not hijack global scope as a project ----------

// TestScopedProjectWalkUpToLibraryRootResolvesGlobal proves that when walk-up
// discovers the library root's own .adeptability/config.json (seeded by prior
// `--global` use), it resolves the GLOBAL scope — not $HOME-as-a-project. The
// found root equals filepath.Dir(libraryRoot), which is the global scope, so
// isGlobal must be true and the project must be the global project.
func TestScopedProjectWalkUpToLibraryRootResolvesGlobal(t *testing.T) {
	tmp := t.TempDir()
	libRoot := filepath.Join(tmp, ".adeptability")
	t.Setenv("ADEPT_LIBRARY", libRoot)

	// A prior `--global` run left config.json at the library root.
	require.NoError(t, os.MkdirAll(libRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(libRoot, adept.ConfigFileName), []byte("{}"), 0o644))

	// cwd is a non-project dir nested under the library root's parent ($HOME).
	nested := filepath.Join(tmp, "some", "nested", "dir")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	// projectDirExplicit=false: mirrors PersistentPreRunE defaulting ProjectDir
	// to cwd, so the walk-up runs (this is the realistic $HOME topology).
	d, err := NewDeps(&GlobalFlags{ProjectDir: nested}, BuildInfo{})
	require.NoError(t, err)

	p, isGlobal, err := d.ScopedProject()
	require.NoError(t, err)
	require.True(t, isGlobal, "walk-up onto the library root must resolve global scope, not a project")
	require.Equal(t, filepath.Dir(libRoot), p.Root())
	require.Equal(t, libRoot, p.BaseDir())
}

// TestScopedProjectVendoredLibRootStaysProject guards against the guard
// over-matching: with the documented vendored-CI topology
// ADEPT_LIBRARY=<project>/.adept-lib, filepath.Dir(libRoot) equals the project
// root, so a parent-equality check would misresolve a REAL project as global
// (empty config -> zero harnesses -> `status` exits 0, defeating the CI drift
// gate). Matching the config dir itself keeps this a project.
func TestScopedProjectVendoredLibRootStaysProject(t *testing.T) {
	proj := t.TempDir()
	// Library root is a NON-".adeptability" dir under the project (vendored CI).
	libRoot := filepath.Join(proj, ".adept-lib")
	t.Setenv("ADEPT_LIBRARY", libRoot)
	require.NoError(t, os.MkdirAll(libRoot, 0o755))

	// A real project: its own .adeptability/config.json.
	require.NoError(t, os.MkdirAll(filepath.Join(proj, adept.BaseDirName), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, adept.BaseDirName, adept.ConfigFileName), []byte("{}"), 0o644))

	nested := filepath.Join(proj, "src", "pkg")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	d, err := NewDeps(&GlobalFlags{ProjectDir: nested}, BuildInfo{})
	require.NoError(t, err)

	p, isGlobal, err := d.ScopedProject()
	require.NoError(t, err)
	require.False(t, isGlobal, "a real project with a vendored .adept-lib library root must resolve as a project, not global")
	require.Equal(t, proj, p.Root())
}

// TestWalkUpToGlobalConfigRendersToGlobalTarget is the end-to-end proof: a bare
// `sync` (no --global, no --project) from a nested cwd under the library root's
// parent renders a codex skill into the GLOBAL target (.codex/AGENTS.md), never
// the project target ($HOME/AGENTS.md).
func TestWalkUpToGlobalConfigRendersToGlobalTarget(t *testing.T) {
	tmp := t.TempDir()
	libRoot := filepath.Join(tmp, ".adeptability")
	t.Setenv("ADEPT_LIBRARY", libRoot)

	// Global config enabling codex, plus a canonical skill at the library root.
	require.NoError(t, os.MkdirAll(libRoot, 0o755))
	cfg := map[string]any{"schema": adept.ConfigSchemaVersion, "harnesses": []string{"codex"}}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(libRoot, adept.ConfigFileName), cfgBytes, 0o644))
	skillDir := filepath.Join(libRoot, adept.SkillsDirName, "demo")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, adept.SkillFileName), skillMD("demo", "a demo skill"), 0o644))

	// Run from a nested cwd with no --project/--global, so scope resolution
	// falls to walk-up (which lands on the library root's config).
	nested := filepath.Join(tmp, "some", "nested", "dir")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	t.Chdir(nested)

	root := NewRoot(BuildInfo{Version: "test"})
	root.SetArgs([]string{"sync"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	require.NoError(t, root.Execute())

	// Rendered into the GLOBAL codex target under the render root (parent of libRoot).
	require.FileExists(t, filepath.Join(tmp, ".codex", "AGENTS.md"),
		"codex skill must render to the global target (.codex/AGENTS.md)")
	// The project target ($HOME/AGENTS.md) must never be written.
	require.NoFileExists(t, filepath.Join(tmp, "AGENTS.md"),
		"codex must not render into $HOME as a fake project")
}

// ---------- Finding 2: init --from clones into project-local libs ----------

func TestInitFromClonesIntoProjectScope(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	// init's consumer path runs autoAdopt (orchestrator Import), so wire a full
	// Deps rather than the bare test double. Real git clones the local fixture.
	d, err := NewDeps(&GlobalFlags{ProjectDir: projRoot, LibraryDir: libRoot, projectDirExplicit: true}, BuildInfo{})
	require.NoError(t, err)
	remote := seedRemote(t, "demo")

	cmd := newInitCmd(d)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--from", remote, "--name", "team"})
	require.NoError(t, cmd.Execute())

	// Clone landed in the project-scope libs/, not the machine store.
	require.FileExists(t, filepath.Join(projRoot, adept.BaseDirName, "libs", "team",
		adept.SkillsDirName, "demo", adept.SkillFileName))
	require.NoDirExists(t, filepath.Join(libRoot, "libs", "team"),
		"init --from must not clone into the machine store")

	// The scope .gitignore keeps the machine-local clone out of version control.
	data, err := os.ReadFile(filepath.Join(projRoot, adept.BaseDirName, ".gitignore"))
	require.NoError(t, err)
	require.Contains(t, string(data), "libs/")
	require.Contains(t, string(data), "staging/")

	// The library ref was recorded in project config.
	p, err := d.Project()
	require.NoError(t, err)
	cfg, err := p.Config()
	require.NoError(t, err)
	require.Len(t, cfg.Libraries, 1)
	require.Equal(t, "team", cfg.Libraries[0].Name)
}

// ---------- Finding 3: --global rejected by project-scope-only commands ----------

func TestGlobalRejectedByProjectScopeCommands(t *testing.T) {
	newGlobalDeps := func(t *testing.T) *Deps {
		t.Helper()
		projRoot, libRoot := t.TempDir(), t.TempDir()
		d := testDeps(t, projRoot, libRoot)
		d.Flags.Global = true
		return d
	}

	t.Run("agent add", func(t *testing.T) {
		d := newGlobalDeps(t)
		cmd := newAgentAddCmd(d)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"reviewer"})
		err := cmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "project-scope only")
	})

	t.Run("agent list", func(t *testing.T) {
		d := newGlobalDeps(t)
		cmd := newAgentListCmd(d)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "project-scope only")
	})

	t.Run("loop add", func(t *testing.T) {
		d := newGlobalDeps(t)
		cmd := newLoopAddCmd(d)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"triage"})
		err := cmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "project-scope only")
	})

	t.Run("hook install", func(t *testing.T) {
		d := newGlobalDeps(t)
		cmd := newHookInstallCmd(d)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		err := cmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "project-scope only")
	})
}

// ---------- Finding 4: library remove --purge is fallback-aware ----------

// TestLibraryRemovePurgeDeletesLocalClone proves --purge deletes a scope-local
// clone (the project owns it) and reports it deleted.
func TestLibraryRemovePurgeDeletesLocalClone(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)
	initProject(t, d, projRoot, &adept.Config{})
	remote := seedRemote(t, "demo")

	_, err := runLibAdd(t, d, "team", "--from", remote)
	require.NoError(t, err)
	clone := filepath.Join(projRoot, adept.BaseDirName, "libs", "team")
	require.DirExists(t, clone)

	out, err := runLibRemove(t, d, "team", "--purge")
	require.NoError(t, err)
	require.Contains(t, out, "clone deleted")
	require.NoDirExists(t, clone, "scope-local clone must be deleted")
}

// TestLibraryRemovePurgeKeepsMachineStoreClone proves --purge on a
// machine-store-resolved (fallback) library leaves the shared clone intact and
// says so — rather than deleting the nonexistent scope path and lying.
func TestLibraryRemovePurgeKeepsMachineStoreClone(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)
	// Config lists the library, but the clone lives ONLY in the machine store.
	initProject(t, d, projRoot, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "team", Remote: "x", Ref: "main"}},
	})
	machineClone := filepath.Join(libRoot, "libs", "team")
	require.NoError(t, os.MkdirAll(filepath.Join(machineClone, adept.SkillsDirName), 0o755))

	out, err := runLibRemove(t, d, "team", "--purge")
	require.NoError(t, err)
	require.Contains(t, out, "machine-store clone left intact")
	require.DirExists(t, machineClone, "machine-store clone must survive --purge")
}

// runLibRemove executes `library remove <args>` against d and returns output.
func runLibRemove(t *testing.T, d *Deps, args ...string) (string, error) {
	t.Helper()
	cmd := newLibraryRemoveCmd(d)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}
