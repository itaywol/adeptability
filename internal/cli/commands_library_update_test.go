package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/git"
	"github.com/itaywol/adeptability/pkg/adept"
)

// gitRun runs a git command in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func writeSkillFile(t *testing.T, repo, id, desc string) {
	t.Helper()
	dir := filepath.Join(repo, adept.SkillsDirName, id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, adept.SkillFileName), skillMD(id, desc), 0o644))
}

// setupLibraryClone builds an upstream repo with one skill, then clones it into
// the project's libs/<name>/ so the library is "in sync" at the start. Returns
// the upstream path and the local clone path.
func setupLibraryClone(t *testing.T, d *Deps, name string) (upstream, clone string) {
	t.Helper()
	upstream = t.TempDir()
	gitRun(t, upstream, "init", "-b", "main")
	gitRun(t, upstream, "config", "user.email", "t@example.com")
	gitRun(t, upstream, "config", "user.name", "Test")
	writeSkillFile(t, upstream, "foo", "first skill")
	gitRun(t, upstream, "add", ".")
	gitRun(t, upstream, "commit", "-m", "init")

	libsRoot, err := d.ResolveLibrariesRoot()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(libsRoot, 0o755))
	clone = filepath.Join(libsRoot, name)
	gitRun(t, libsRoot, "clone", upstream, clone)
	return upstream, clone
}

// upstreamCommit adds a new skill to the upstream repo and commits it, so the
// clone falls behind by one commit.
func upstreamCommit(t *testing.T, upstream, skillID string) {
	t.Helper()
	writeSkillFile(t, upstream, skillID, "added upstream")
	gitRun(t, upstream, "add", ".")
	gitRun(t, upstream, "commit", "-m", "add "+skillID)
}

func depsWithRealGit(t *testing.T, projRoot, libRoot string) *Deps {
	t.Helper()
	d := testDeps(t, projRoot, libRoot)
	d.Git = git.NewClient(git.NewExecRunner("git"))
	return d
}

func runLibUpdate(t *testing.T, d *Deps, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newLibraryUpdateCmd(d)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestLibraryHasUpdate_OfflineThenFetch(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)
	upstream, clone := setupLibraryClone(t, d, "lib")
	ctx := context.Background()

	// Freshly cloned and in sync — no update.
	require.False(t, libraryHasUpdate(ctx, d, clone, "main", false))

	upstreamCommit(t, upstream, "bar")

	// Offline: the clone hasn't fetched, so the stale tracking ref still
	// matches HEAD — no update detected.
	require.False(t, libraryHasUpdate(ctx, d, clone, "main", false))

	// --fetch refreshes origin/main and now the clone is behind.
	require.True(t, libraryHasUpdate(ctx, d, clone, "main", true))
}

func TestLibraryUpdate_AppliesWithYes(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)
	upstream, clone := setupLibraryClone(t, d, "lib")
	initProject(t, d, projRoot, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "lib", Remote: upstream, Ref: "main"}},
	})
	upstreamCommit(t, upstream, "bar")

	out, err := runLibUpdate(t, d, "", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "changed skills: bar")
	require.Contains(t, out, "lib: updated to")
	require.Contains(t, out, "adept sync")

	// Clone fast-forwarded to upstream HEAD and the new skill landed.
	local, err := d.Git.RevParse(context.Background(), clone, "HEAD")
	require.NoError(t, err)
	remote, err := d.Git.RevParse(context.Background(), upstream, "HEAD")
	require.NoError(t, err)
	require.Equal(t, remote, local)
	require.FileExists(t, filepath.Join(clone, adept.SkillsDirName, "bar", adept.SkillFileName))
}

func TestLibraryUpdate_PromptDeclineSkips(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)
	upstream, clone := setupLibraryClone(t, d, "lib")
	initProject(t, d, projRoot, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "lib", Remote: upstream, Ref: "main"}},
	})
	before, err := d.Git.RevParse(context.Background(), clone, "HEAD")
	require.NoError(t, err)
	upstreamCommit(t, upstream, "bar")

	out, err := runLibUpdate(t, d, "n\n") // decline the prompt
	require.NoError(t, err)
	require.Contains(t, out, "lib: skipped")

	// Clone untouched.
	after, err := d.Git.RevParse(context.Background(), clone, "HEAD")
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestLibraryUpdate_UpToDate(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)
	upstream, _ := setupLibraryClone(t, d, "lib")
	initProject(t, d, projRoot, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "lib", Remote: upstream, Ref: "main"}},
	})

	out, err := runLibUpdate(t, d, "", "--yes")
	require.NoError(t, err)
	require.Contains(t, out, "up to date")
}

func TestCollectStatus_FlagsLibraryUpdate(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)
	upstream, _ := setupLibraryClone(t, d, "lib")
	initProject(t, d, projRoot, &adept.Config{
		Libraries: []adept.LibraryRef{{Name: "lib", Remote: upstream, Ref: "main"}},
	})
	upstreamCommit(t, upstream, "bar")

	// Offline status: tracking ref still stale, no update surfaced.
	rep, err := collectStatus(context.Background(), d, false)
	require.NoError(t, err)
	require.Equal(t, 0, rep.UpdatableLibs)
	require.False(t, rep.Libraries[0].UpdatesAvailable)

	// --fetch refreshes and surfaces the available update.
	rep, err = collectStatus(context.Background(), d, true)
	require.NoError(t, err)
	require.Equal(t, 1, rep.UpdatableLibs)
	require.True(t, rep.Libraries[0].UpdatesAvailable)
}

func TestLibraryUpdate_UnknownName(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)
	initProject(t, d, projRoot, &adept.Config{})

	_, err := runLibUpdate(t, d, "", "nope")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}
