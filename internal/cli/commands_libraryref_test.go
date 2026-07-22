package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/pkg/adept"
)

// seedRemote builds a local-path git "remote" holding one skill so `library
// add --from <remote>` has something to clone.
func seedRemote(t *testing.T, skillID string) string {
	t.Helper()
	remote := t.TempDir()
	gitRun(t, remote, "init", "-b", "main")
	gitRun(t, remote, "config", "user.email", "t@example.com")
	gitRun(t, remote, "config", "user.name", "Test")
	writeSkillFile(t, remote, skillID, "seed skill")
	gitRun(t, remote, "add", ".")
	gitRun(t, remote, "commit", "-m", "init")
	return remote
}

// runLibAdd executes `library add <args>` against d and returns combined output.
func runLibAdd(t *testing.T, d *Deps, args ...string) (string, error) {
	t.Helper()
	cmd := newLibraryAddCmd(d)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestLibraryAddClonesIntoProjectScope(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)
	initProject(t, d, projRoot, &adept.Config{})
	remote := seedRemote(t, "demo")

	_, err := runLibAdd(t, d, "team", "--from", remote)
	require.NoError(t, err)

	// Clone landed in the project-scope libs/, not the machine store.
	require.FileExists(t, filepath.Join(projRoot, adept.BaseDirName, "libs", "team",
		adept.SkillsDirName, "demo", adept.SkillFileName))
	require.NoDirExists(t, filepath.Join(libRoot, "libs", "team"))

	// .adeptability/.gitignore manages machine-local subpaths.
	data, err := os.ReadFile(filepath.Join(projRoot, adept.BaseDirName, ".gitignore"))
	require.NoError(t, err)
	require.Contains(t, string(data), "libs/")
	require.Contains(t, string(data), "staging/")
}

func TestLibraryAddGlobalClonesIntoMachineStore(t *testing.T) {
	projRoot, libRoot := t.TempDir(), t.TempDir()
	d := depsWithRealGit(t, projRoot, libRoot)
	d.Flags.Global = true
	remote := seedRemote(t, "demo")

	_, err := runLibAdd(t, d, "team", "--from", remote)
	require.NoError(t, err)

	// Global scope clones into the machine store (~/.adeptability/libs).
	require.FileExists(t, filepath.Join(libRoot, "libs", "team",
		adept.SkillsDirName, "demo", adept.SkillFileName))

	// Global config.json (at the library root) gained the ref.
	gp, err := d.GlobalProject()
	require.NoError(t, err)
	cfg, err := gp.Config()
	require.NoError(t, err)
	require.Len(t, cfg.Libraries, 1)
	require.Equal(t, "team", cfg.Libraries[0].Name)
}

func TestEnsureScopeGitignoreIdempotent(t *testing.T) {
	base := t.TempDir()
	w := fsutil.NewWriter()
	path := filepath.Join(base, ".gitignore")

	// Pre-existing custom content must survive.
	require.NoError(t, os.WriteFile(path, []byte("# custom\nnode_modules/\n"), 0o644))

	require.NoError(t, ensureScopeGitignore(w, base))
	require.NoError(t, ensureScopeGitignore(w, base)) // second call is a no-op

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(data)

	require.Equal(t, 1, bytes.Count(data, []byte("libs/\n")))
	require.Equal(t, 1, bytes.Count(data, []byte("staging/\n")))
	require.Contains(t, got, "# custom")
	require.Contains(t, got, "node_modules/")
}

func TestChangedSkillIDs(t *testing.T) {
	got := changedSkillIDs([]string{
		"skills/pr-review/SKILL.md",
		"skills/pr-review/scripts/run.sh", // same skill, second file -> deduped
		"skills/lint-style/SKILL.md",
		"README.md",          // not under skills/ -> ignored
		"skills",             // bare dir, no id -> ignored
		"docs/skills/x/y.md", // skills/ not the first segment -> ignored
	})
	want := []string{"lint-style", "pr-review"} // sorted, unique
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changedSkillIDs = %v, want %v", got, want)
	}
}
