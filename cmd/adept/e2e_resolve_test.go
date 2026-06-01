package main_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestE2E_ResolveMerge exercises the new --strategy merge path end to end.
//
// Scenario:
//   - Add a skill to the library, install it into the project (which
//     also seeds the base snapshot).
//   - Edit the project copy and the library copy in non-overlapping
//     places, then run `adept resolve <id> --strategy merge`.
//   - The merge must succeed cleanly (exit 0) and the project SKILL.md
//     must contain BOTH edits.
//
// A second subtest reuses the same setup but makes overlapping edits;
// resolve must surface conflict markers, exit 2, and leave the lockfile
// unchanged.
func TestE2E_ResolveMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e under -short")
	}

	repoRoot := findRepoRoot(t)
	binPath := filepath.Join(t.TempDir(), "adept")
	buildBinary(t, repoRoot, binPath)

	type env struct {
		lib  string
		proj string
		home string
		env  []string
	}

	newEnv := func(t *testing.T) *env {
		t.Helper()
		lib := filepath.Join(t.TempDir(), "lib")
		proj := filepath.Join(t.TempDir(), "proj")
		home := t.TempDir()
		return &env{
			lib:  lib,
			proj: proj,
			home: home,
			env: []string{
				"PATH=" + os.Getenv("PATH"),
				"HOME=" + home,
				"ADEPT_LIBRARY=" + lib,
			},
		}
	}

	run := func(t *testing.T, e *env, args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(binPath, args...)
		cmd.Env = e.env
		var out, errBuf bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
		err := cmd.Run()
		code := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("run adept %v: %v\nstderr: %s", args, err, errBuf.String())
		}
		return out.String() + errBuf.String(), code
	}

	bootstrap := func(t *testing.T, e *env) {
		t.Helper()
		exampleDir := filepath.Join(repoRoot, "examples", "skills", "pr-review")
		out, code := run(t, e, "init", "library")
		require.Equal(t, 0, code, out)
		out, code = run(t, e, "--project", e.proj, "init", "project")
		require.Equal(t, 0, code, out)
		out, code = run(t, e, "add", exampleDir)
		require.Equal(t, 0, code, out)
		out, code = run(t, e, "--project", e.proj, "install", "pr-review")
		require.Equal(t, 0, code, out)
	}

	projSkillPath := func(e *env) string {
		return filepath.Join(e.proj, ".adeptability", "skills", "pr-review", "SKILL.md")
	}
	libSkillPath := func(e *env) string {
		return filepath.Join(e.lib, "skills", "pr-review", "SKILL.md")
	}
	basePath := func(e *env) string {
		return filepath.Join(e.proj, ".adeptability", "base", "pr-review", "SKILL.md")
	}

	t.Run("non-overlapping edits merge cleanly", func(t *testing.T) {
		e := newEnv(t)
		bootstrap(t, e)
		require.FileExists(t, basePath(e), "install must seed base snapshot")

		// Edit project: append a sentence to the body intro.
		projBody, err := os.ReadFile(projSkillPath(e))
		require.NoError(t, err)
		require.Contains(t, string(projBody), "Use this before requesting review on a pull request.")
		newProj := strings.Replace(
			string(projBody),
			"Use this before requesting review on a pull request.",
			"Use this before requesting review on a pull request.\nLocal-only addition by ours.",
			1,
		)
		require.NoError(t, os.WriteFile(projSkillPath(e), []byte(newProj), 0o644))

		// Edit library: append a different sentence elsewhere in the file.
		libBody, err := os.ReadFile(libSkillPath(e))
		require.NoError(t, err)
		require.Contains(t, string(libBody), "## Docs")
		newLib := strings.Replace(
			string(libBody),
			"## Docs",
			"## Docs\nLibrary-only addition by theirs.",
			1,
		)
		require.NoError(t, os.WriteFile(libSkillPath(e), []byte(newLib), 0o644))

		out, code := run(t, e, "--project", e.proj, "resolve", "pr-review", "--strategy", "merge")
		require.Equalf(t, 0, code, "merge should exit 0 cleanly; output:\n%s", out)
		require.Contains(t, out, "merged pr-review")

		merged, err := os.ReadFile(projSkillPath(e))
		require.NoError(t, err)
		require.Contains(t, string(merged), "Local-only addition by ours.")
		require.Contains(t, string(merged), "Library-only addition by theirs.")
		require.NotContains(t, string(merged), "<<<<<<< ours")

		// After a clean merge the base snapshot must be refreshed to
		// the new state so subsequent diverges have an accurate
		// ancestor.
		baseAfter, err := os.ReadFile(basePath(e))
		require.NoError(t, err)
		require.Equal(t, merged, baseAfter)
	})

	t.Run("overlapping edits surface conflict markers and exit 2", func(t *testing.T) {
		e := newEnv(t)
		bootstrap(t, e)

		projBody, err := os.ReadFile(projSkillPath(e))
		require.NoError(t, err)
		libBody, err := os.ReadFile(libSkillPath(e))
		require.NoError(t, err)
		// Both sides modify the same line differently.
		ours := strings.Replace(string(projBody),
			"Use this before requesting review on a pull request.",
			"OURS edit of intro line.",
			1,
		)
		theirs := strings.Replace(string(libBody),
			"Use this before requesting review on a pull request.",
			"THEIRS edit of intro line.",
			1,
		)
		require.NoError(t, os.WriteFile(projSkillPath(e), []byte(ours), 0o644))
		require.NoError(t, os.WriteFile(libSkillPath(e), []byte(theirs), 0o644))

		out, code := run(t, e, "--project", e.proj, "resolve", "pr-review", "--strategy", "merge")
		require.Equalf(t, 2, code, "conflict run should exit 2; output:\n%s", out)
		require.Contains(t, out, "CONFLICT SKILL.md")

		merged, err := os.ReadFile(projSkillPath(e))
		require.NoError(t, err)
		body := string(merged)
		require.Contains(t, body, "<<<<<<< ours")
		require.Contains(t, body, "||||||| base")
		require.Contains(t, body, "=======")
		require.Contains(t, body, ">>>>>>> theirs")
		require.Contains(t, body, "OURS edit of intro line.")
		require.Contains(t, body, "THEIRS edit of intro line.")

		// Lockfile must NOT have advanced because conflicts remain
		// unresolved. The base snapshot must also be untouched.
		baseAfter, err := os.ReadFile(basePath(e))
		require.NoError(t, err)
		require.NotContains(t, string(baseAfter), "<<<<<<< ours", "base must not carry conflict markers")
	})

	t.Run("missing base snapshot is rejected", func(t *testing.T) {
		e := newEnv(t)
		bootstrap(t, e)
		// Remove the base snapshot to simulate a legacy install.
		require.NoError(t, os.RemoveAll(filepath.Join(e.proj, ".adeptability", "base", "pr-review")))
		out, code := run(t, e, "--project", e.proj, "resolve", "pr-review", "--strategy", "merge")
		require.Equalf(t, 1, code, "missing base must surface as a hard error; output:\n%s", out)
		require.Contains(t, strings.ToLower(out), "merge base snapshot missing")
	})

	t.Run("unknown strategy is rejected", func(t *testing.T) {
		e := newEnv(t)
		bootstrap(t, e)
		out, code := run(t, e, "--project", e.proj, "resolve", "pr-review", "--strategy", "bogus")
		require.NotEqual(t, 0, code)
		require.Contains(t, out, "unknown strategy")
	})
}
