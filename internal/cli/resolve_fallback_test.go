package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/log"
	"github.com/itaywol/adeptability/pkg/adept"
)

// writeScopeLocalSkill writes a parseable skill under the scope-local libs root
// (<project>/.adeptability/libs/<lib>/skills/<id>/), the location a project
// resolves before falling back to the machine store.
func writeScopeLocalSkill(t *testing.T, libsRoot, lib, id, desc string) {
	t.Helper()
	dir := filepath.Join(libsRoot, lib, adept.SkillsDirName, id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, adept.SkillFileName), skillMD(id, desc), 0o644))
}

// TestOpenMultiLibraryFallsBackToMachineStore is the brief's named test: a
// project-scoped config resolves a library from the machine store when no
// scope-local clone exists, and the scope-local clone wins once it does.
func TestOpenMultiLibraryFallsBackToMachineStore(t *testing.T) {
	root := t.TempDir()
	libRoot := t.TempDir()
	d := testDeps(t, root, libRoot)
	p := initProject(t, d, root, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "team", Remote: "r"}},
	})

	// Only the machine store (libRoot/libs/team) carries "demo".
	writeLibrarySkill(t, libRoot, "team", "demo", "machine store copy")

	skills, err := resolveSkills(d, p)
	require.NoError(t, err)
	byID := map[string]*adept.Skill{}
	for _, s := range skills {
		byID[s.ID] = s
	}
	require.Contains(t, byID, "demo", "fallback to machine store resolves the library")
	require.Contains(t, byID["demo"].Description, "machine store copy")

	// Now a scope-local clone appears with a different body — it must win.
	writeScopeLocalSkill(t, d.LibsRootFor(p), "team", "demo", "project local copy")

	skills, err = resolveSkills(d, p)
	require.NoError(t, err)
	byID = map[string]*adept.Skill{}
	for _, s := range skills {
		byID[s.ID] = s
	}
	require.Contains(t, byID, "demo")
	require.Contains(t, byID["demo"].Description, "project local copy",
		"scope-local clone shadows the machine-store fallback")
}

// TestOpenMultiLibraryWarnsOnMachineStoreFallback asserts the migrate hint
// fires (with name + path) when a project resolves a library from the machine
// store, and stays silent when a scope-local clone is present.
func TestOpenMultiLibraryWarnsOnMachineStoreFallback(t *testing.T) {
	root := t.TempDir()
	libRoot := t.TempDir()
	d := testDeps(t, root, libRoot)
	p := initProject(t, d, root, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "team", Remote: "r"}},
	})
	writeLibrarySkill(t, libRoot, "team", "demo", "machine store copy")

	var buf bytes.Buffer
	d.Log = log.NewLogger(log.LevelWarn, false, &buf)

	_, err := resolveSkills(d, p)
	require.NoError(t, err)

	out := buf.String()
	require.Contains(t, out, "run `adept migrate` to localize")
	require.Contains(t, out, "name=team")
	require.Contains(t, out, filepath.Join(libRoot, "libs", "team"))
}

// TestOpenMultiLibraryNoWarnWhenLocal asserts a scope-local clone resolves
// without any migrate hint.
func TestOpenMultiLibraryNoWarnWhenLocal(t *testing.T) {
	root := t.TempDir()
	libRoot := t.TempDir()
	d := testDeps(t, root, libRoot)
	p := initProject(t, d, root, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "team", Remote: "r"}},
	})
	writeScopeLocalSkill(t, d.LibsRootFor(p), "team", "demo", "project local copy")

	var buf bytes.Buffer
	d.Log = log.NewLogger(log.LevelWarn, false, &buf)

	_, err := resolveSkills(d, p)
	require.NoError(t, err)
	require.NotContains(t, buf.String(), "adept migrate")
}

// TestResolveLibDirSourceGlobalScopeIsLocal guards the "no warning when
// scope-local == machine store" case: in global scope the two roots coincide,
// so a resolved library is labelled local, never fallback.
func TestResolveLibDirSourceGlobalScopeIsLocal(t *testing.T) {
	libRoot := t.TempDir()
	t.Setenv("ADEPT_LIBRARY", libRoot)

	d, err := NewDeps(&GlobalFlags{Global: true}, BuildInfo{})
	require.NoError(t, err)
	gp, isGlobal, err := d.ScopedProject()
	require.NoError(t, err)
	require.True(t, isGlobal)

	// Global scope clones into libRoot/libs directly.
	require.NoError(t, os.MkdirAll(filepath.Join(libRoot, "libs", "team"), 0o755))

	dir, src := resolveLibDirSource(d, gp, "team")
	require.Equal(t, libLocal, src, "global scope resolves as local, not fallback")
	require.Equal(t, filepath.Join(libRoot, "libs", "team"), dir)
}

// runLibList executes `library list` (optionally JSON) and returns output.
func runLibList(t *testing.T, d *Deps) []libraryRow {
	t.Helper()
	prev := d.Flags.JSON
	d.Flags.JSON = true
	defer func() { d.Flags.JSON = prev }()
	cmd := newLibraryListCmd(d)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)
	require.NoError(t, cmd.Execute())
	var rows []libraryRow
	require.NoError(t, json.Unmarshal(out.Bytes(), &rows))
	return rows
}

// TestLibraryListShowsMachineStoreFallback asserts `library list` reports a
// machine-store-resolved library as on-disk with its real resolved path.
func TestLibraryListShowsMachineStoreFallback(t *testing.T) {
	root := t.TempDir()
	libRoot := t.TempDir()
	d := testDeps(t, root, libRoot)
	initProject(t, d, root, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "team", Remote: "r"}},
	})
	writeLibrarySkill(t, libRoot, "team", "demo", "machine store copy")

	rows := runLibList(t, d)
	require.Len(t, rows, 1)
	require.Equal(t, "team", rows[0].Name)
	require.True(t, rows[0].OnDisk, "machine-store fallback counts as on-disk")
	require.Equal(t, filepath.Join(libRoot, "libs", "team"), rows[0].LocalPath)
}

// TestLibraryUpdateUsesMachineStoreFallback asserts `library update` operates on
// the machine-store clone rather than reporting "no local clone".
func TestLibraryUpdateUsesMachineStoreFallback(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)

	// Build an upstream and clone it into the machine store (libRoot/libs/lib).
	upstream := t.TempDir()
	gitRun(t, upstream, "init", "-b", "main")
	gitRun(t, upstream, "config", "user.email", "t@example.com")
	gitRun(t, upstream, "config", "user.name", "Test")
	writeSkillFile(t, upstream, "foo", "first skill")
	gitRun(t, upstream, "add", ".")
	gitRun(t, upstream, "commit", "-m", "init")

	storeRoot, err := d.ResolveLibrariesRoot()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(storeRoot, 0o755))
	clone := filepath.Join(storeRoot, "lib")
	gitRun(t, storeRoot, "clone", upstream, clone)

	initProject(t, d, projRoot, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "lib", Remote: upstream, Ref: "main"}},
	})
	upstreamCommit(t, upstream, "bar")

	out, err := runLibUpdate(t, d, "", "--yes")
	require.NoError(t, err)
	require.NotContains(t, out, "no local clone")
	require.Contains(t, out, "changed skills: bar")
	require.Contains(t, out, "lib: updated to")

	// The machine-store clone fast-forwarded.
	require.FileExists(t, filepath.Join(clone, adept.SkillsDirName, "bar", adept.SkillFileName))
}
