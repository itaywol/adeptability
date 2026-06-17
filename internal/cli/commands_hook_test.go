package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/git"
	"github.com/itaywol/adeptability/internal/harness"
	"github.com/itaywol/adeptability/pkg/adept"
)

// ---------- pure helpers ----------

func TestStaticPrefix(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		".claude/skills/{id}/SKILL.md":    ".claude/skills",
		".cursor/rules/{id}.mdc":          ".cursor/rules",
		"AGENTS.md":                       "AGENTS.md",
		".github/instructions/{bucket}.x": ".github/instructions",
		"":                                "",
	}
	for in, want := range cases {
		require.Equalf(t, want, staticPrefix(in), "staticPrefix(%q)", in)
	}
}

func TestPathUnder(t *testing.T) {
	t.Parallel()
	require.True(t, pathUnder(".claude/skills/foo/SKILL.md", ".claude"))
	require.True(t, pathUnder(".claude", ".claude"))
	require.True(t, pathUnder("AGENTS.md", "AGENTS.md"))
	require.False(t, pathUnder(".claudeish/x", ".claude"))
	require.False(t, pathUnder(".cursor/rules/a.mdc", ".claude"))
}

func TestHarnessRoots(t *testing.T) {
	t.Parallel()
	// BaseDir + OutputPath prefix, deduped intent preserved.
	roots := harnessRoots(adept.HarnessSpec{BaseDir: ".claude", OutputPath: ".claude/skills/{id}/SKILL.md"})
	require.Equal(t, []string{".claude", ".claude/skills"}, roots)
	// Aggregator with empty BaseDir falls back to the OutputPath prefix.
	require.Equal(t, []string{"AGENTS.md"}, harnessRoots(adept.HarnessSpec{BaseDir: "", OutputPath: "AGENTS.md"}))
}

// ---------- install ----------

// gitInit makes dir a real git repo so IsRepo and StagedFiles work.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@t.t"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoErrorf(t, cmd.Run(), "git %v", args)
	}
}

func TestInstallPreCommitHook(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gitInit(t, root)
	d := testDepsWithGit(t, root)

	path, err := installPreCommitHook(d, root, "fix")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, ".git", "hooks", "pre-commit"), path)

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(body), hookMarker)
	require.Contains(t, string(body), "adept hook run --fix")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&0o100, "hook must be executable")

	// Re-install (still adept-managed) is allowed and flips mode.
	_, err = installPreCommitHook(d, root, "fail")
	require.NoError(t, err)
	body, _ = os.ReadFile(path)
	require.NotContains(t, string(body), "--fix")
}

func TestInstallPreCommitHook_RefusesForeignHook(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gitInit(t, root)
	d := testDepsWithGit(t, root)

	hookPath := filepath.Join(root, ".git", "hooks", "pre-commit")
	require.NoError(t, os.WriteFile(hookPath, []byte("#!/bin/sh\necho mine\n"), 0o755))

	_, err := installPreCommitHook(d, root, "fail")
	require.ErrorContains(t, err, "not adept-managed")
}

func TestInstallPreCommitHook_RejectsBadMode(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	gitInit(t, root)
	_, err := installPreCommitHook(testDepsWithGit(t, root), root, "nope")
	require.ErrorContains(t, err, "invalid hook mode")
}

func TestInstallPreCommitHook_NotARepo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, err := installPreCommitHook(testDepsWithGit(t, root), root, "fail")
	require.ErrorContains(t, err, "not a git repository")
}

// ---------- runHook (integration with real adapters) ----------

func TestRunHook_NoProjectIsNoop(t *testing.T) {
	t.Parallel()
	root := t.TempDir() // no .adeptability/
	d := testDepsWithGit(t, root)
	require.NoError(t, runHook(context.Background(), d, &bytes.Buffer{}, false))
}

func TestRunHook_FailOnDrift_FixReconciles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	lib := t.TempDir()
	gitInit(t, root)
	d := newFullDeps(t, root, lib)

	// Initialized project with claude-code enabled in copy mode + one skill.
	cfg := &adept.Config{Harnesses: []string{"claude-code"}, Mode: adept.ModeCopy}
	p := initProject(t, d, root, cfg)
	writeProjectSkill(t, p, "alpha", "alpha skill")

	skills, err := resolveSkills(d, p)
	require.NoError(t, err)
	_, err = d.Orchestrator.Sync(context.Background(), p, harness.SyncOptions{Force: true, Skills: skills})
	require.NoError(t, err)

	rendered := filepath.Join(root, ".claude", "skills", "alpha", "SKILL.md")
	require.FileExists(t, rendered)

	// Hand-edit the rendered harness file → drift.
	orig, err := os.ReadFile(rendered)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(rendered, append(orig, []byte("\nhand edit\n")...), 0o644))

	// fail mode: blocks the commit.
	require.ErrorIs(t, runHook(context.Background(), d, &bytes.Buffer{}, false), ErrDirty)

	// Stage the edit, then fix mode reconciles and re-stages.
	stage(t, root, rendered)
	var out bytes.Buffer
	require.NoError(t, runHook(context.Background(), d, &out, true))

	// Canonical now carries the harness edit, and a fresh drift check is clean.
	canon, err := os.ReadFile(filepath.Join(p.SkillsDir(), "alpha", adept.SkillFileName))
	require.NoError(t, err)
	require.Contains(t, string(canon), "hand edit")
	drift, err := driftedHarnesses(context.Background(), d, p)
	require.NoError(t, err)
	require.Empty(t, drift)
}

func TestHarnessesForStagedPaths(t *testing.T) {
	t.Parallel()
	d := newFullDeps(t, t.TempDir(), t.TempDir())
	enabled := []string{"claude-code"}

	// A staged .claude file maps to claude-code.
	got := harnessesForStagedPaths(d, enabled, []string{".claude/skills/x/SKILL.md", "README.md"})
	require.Equal(t, []string{"claude-code"}, got)

	// A staged staging-dir file fans out to every enabled harness.
	got = harnessesForStagedPaths(d, enabled,
		[]string{filepath.Join(adept.BaseDirName, adept.StagingDir, "x.md")})
	require.Equal(t, []string{"claude-code"}, got)

	// Unrelated paths map to nothing.
	require.Empty(t, harnessesForStagedPaths(d, enabled, []string{"src/main.go"}))
}

// ---------- helpers ----------

func stage(t *testing.T, root, path string) {
	t.Helper()
	cmd := exec.Command("git", "add", path)
	cmd.Dir = root
	require.NoError(t, cmd.Run())
}

// testDepsWithGit augments the lightweight testDeps with a real git client,
// enough for install/IsRepo/StagedFiles assertions.
func testDepsWithGit(t *testing.T, root string) *Deps {
	t.Helper()
	d := testDeps(t, root, t.TempDir())
	d.Git = git.NewClient(git.NewExecRunner("git"))
	return d
}

// newFullDeps wires a complete Deps via the production constructor so the
// real harness registry and orchestrator are available, pinned to temp roots.
func newFullDeps(t *testing.T, root, lib string) *Deps {
	t.Helper()
	d, err := NewDeps(&GlobalFlags{ProjectDir: root, LibraryDir: lib, LogLevel: "error"}, BuildInfo{Version: "test"})
	require.NoError(t, err)
	return d
}
