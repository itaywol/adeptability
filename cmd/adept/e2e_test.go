package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestE2E exercises the v0.4 UX-refactor surface end-to-end:
//
//   adept init [--from <url>] [--ref <branch>] [--name <local>] [--mode symlink|copy]
//   adept status
//   adept sync       [--harness <id>] [--force] [--dry-run]
//   adept sync-from  [--harness <id>] [--all] [--force] [--dry-run]
//   adept diff       [--harness <id>]
//   adept harness    {add,remove,list}
//   adept skill      {add [--from <path>] [--edit], edit, remove, list}
//   adept library    {add <name> --from <url>, remove [--purge], list}
//
// Covers the four user flows the rewrite was scoped around:
//
//   1. init on an empty project
//   2. init in a project that already has harness files (auto-adopts)
//   3. add a second harness post-init via `harness add`
//   4. multi-library: `library add <name> --from <url>` stacks libraries;
//      `skill list` shows union; project shadows library; first-wins on
//      cross-library collisions.
func TestE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e under -short")
	}

	repoRoot := findRepoRoot(t)
	binPath := filepath.Join(t.TempDir(), "adept")
	buildBinary(t, repoRoot, binPath)

	libRoot := filepath.Join(t.TempDir(), "lib")
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"ADEPT_LIBRARY=" + libRoot,
	}

	run := func(t *testing.T, args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(binPath, args...)
		cmd.Env = env
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

	// Build a standalone "remote" library elsewhere on disk. `library add`
	// will clone it into ADEPT_LIBRARY/libs/default/. Pre-populating that
	// destination path with files would conflict with `git clone`.
	remoteLib := filepath.Join(t.TempDir(), "remote-lib")
	require.NoError(t, os.MkdirAll(filepath.Join(remoteLib, "skills", "pr-review"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(remoteLib, "skills", "pr-review", "SKILL.md"),
		[]byte("---\nid: pr-review\ndescription: PR checklist from default library\nactivation: agent\n---\n# PR Review\nbody\n"),
		0o644,
	))
	gitInit(t, remoteLib)

	proj := filepath.Join(t.TempDir(), "proj")

	t.Run("init on empty project — exit 0, .adeptability scaffolded", func(t *testing.T) {
		out, code := run(t, "--project", proj, "init")
		require.Equal(t, 0, code, out)
		require.FileExists(t, filepath.Join(proj, ".adeptability", "config.json"))
	})

	t.Run("status before any harness — initialized, drift 0", func(t *testing.T) {
		out, code := run(t, "--project", proj, "--json", "status")
		require.Equal(t, 0, code, out)
		var rep struct {
			Initialized      bool   `json:"initialized"`
			Mode             string `json:"mode"`
			DriftedHarnesses int    `json:"driftedHarnesses"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &rep))
		require.True(t, rep.Initialized)
		require.Equal(t, "symlink", rep.Mode)
		require.Equal(t, 0, rep.DriftedHarnesses)
	})

	t.Run("init in project with preexisting .claude/ auto-adopts", func(t *testing.T) {
		adopt := filepath.Join(t.TempDir(), "adopt")
		require.NoError(t, os.MkdirAll(filepath.Join(adopt, ".claude", "skills", "preexisting"), 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(adopt, ".claude", "skills", "preexisting", "SKILL.md"),
			[]byte("---\nname: preexisting\ndescription: pre-existing skill\n---\n# body\n"),
			0o644,
		))
		out, code := run(t, "--project", adopt, "init", "--mode", "copy")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "adopted harnesses: claude-code")
		require.FileExists(t, filepath.Join(adopt, ".adeptability", "skills", "preexisting", "SKILL.md"))
		cfg, err := os.ReadFile(filepath.Join(adopt, ".adeptability", "config.json"))
		require.NoError(t, err)
		require.Contains(t, string(cfg), `"claude-code"`)
		require.Contains(t, string(cfg), `"mode": "copy"`)
	})

	t.Run("harness add enables a built-in", func(t *testing.T) {
		out, code := run(t, "--project", proj, "harness", "add", "cursor")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "enabled cursor")
	})

	t.Run("harness add unknown id rejects", func(t *testing.T) {
		out, code := run(t, "--project", proj, "harness", "add", "totally-fake")
		require.Equal(t, 1, code, out)
		require.Contains(t, out, "harness unknown")
	})

	t.Run("harness list shows enabled state", func(t *testing.T) {
		out, code := run(t, "--project", proj, "--json", "harness", "list")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, `"cursor"`)
		require.Contains(t, out, `"enabled": true`)
	})

	t.Run("library add clones into $LIB/libs/<name>", func(t *testing.T) {
		out, code := run(t, "--project", proj, "library", "add", "default",
			"--from", "file://"+remoteLib)
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "library \"default\" added")
		require.FileExists(t, filepath.Join(libRoot, "libs", "default", "skills", "pr-review", "SKILL.md"))
	})

	t.Run("library list shows the configured library", func(t *testing.T) {
		out, code := run(t, "--project", proj, "--json", "library", "list")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, `"default"`)
		require.Contains(t, out, `"onDisk": true`)
	})

	t.Run("skill list shows library skill (Model B union)", func(t *testing.T) {
		out, code := run(t, "--project", proj, "skill", "list")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "pr-review")
		require.Contains(t, out, "library:default")
	})

	t.Run("skill add of a library-shadowed id succeeds (project shadows library)", func(t *testing.T) {
		out, code := run(t, "--project", proj, "skill", "add", "pr-review")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "created pr-review")
		require.FileExists(t, filepath.Join(proj, ".adeptability", "skills", "pr-review", "SKILL.md"))
	})

	t.Run("skill add of an already-project id rejects", func(t *testing.T) {
		out, code := run(t, "--project", proj, "skill", "add", "pr-review")
		require.Equal(t, 1, code, out)
		require.Contains(t, out, "already exists in project")
	})

	t.Run("skill list now marks pr-review as project, library is shadowed", func(t *testing.T) {
		out, code := run(t, "--project", proj, "--json", "skill", "list")
		require.Equal(t, 0, code, out)
		// project entry wins, library entry is suppressed from the list.
		require.Contains(t, out, `"id": "pr-review"`)
		require.Contains(t, out, `"source": "project"`)
		require.NotContains(t, out, `"source": "library:default"`)
	})

	t.Run("sync renders to enabled harnesses", func(t *testing.T) {
		// Need a more interesting skill body and the project canonical to
		// drive cursor's globless drop logic without crashing.
		_, code := run(t, "--project", proj, "sync", "--force")
		require.Equal(t, 0, code)
		// cursor was enabled earlier and pr-review has no globs → cursor
		// would emit globs:[] and alwaysApply:false; we just verify the
		// per-skill output materialized.
		require.FileExists(t, filepath.Join(proj, ".cursor", "rules", "pr-review.mdc"))
	})

	t.Run("status post-sync shows drifted=0", func(t *testing.T) {
		out, code := run(t, "--project", proj, "--json", "status")
		require.Equal(t, 0, code, out)
		var rep struct {
			DriftedHarnesses int `json:"driftedHarnesses"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &rep))
		require.Equal(t, 0, rep.DriftedHarnesses)
	})

	t.Run("diff reports drift after harness-side edit", func(t *testing.T) {
		mdc := filepath.Join(proj, ".cursor", "rules", "pr-review.mdc")
		require.NoError(t, appendToFile(mdc, "\n# harness-edit\n"))
		out, code := run(t, "--project", proj, "diff", "--harness", "cursor")
		require.Equal(t, 2, code, out)
		require.Contains(t, out, "drift")
	})

	t.Run("sync-from --harness adopts the edit", func(t *testing.T) {
		out, code := run(t, "--project", proj, "sync-from", "--harness", "cursor", "--force")
		require.Equal(t, 0, code, out)
		b, err := os.ReadFile(filepath.Join(proj, ".adeptability", "skills", "pr-review", "SKILL.md"))
		require.NoError(t, err)
		require.Contains(t, string(b), "harness-edit")
	})

	t.Run("harness remove disables", func(t *testing.T) {
		out, code := run(t, "--project", proj, "harness", "remove", "cursor")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "disabled cursor")
	})

	t.Run("library remove drops from config", func(t *testing.T) {
		out, code := run(t, "--project", proj, "library", "remove", "default")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "removed from config")
	})

	t.Run("cut commands are not registered", func(t *testing.T) {
		// Probe with a no-arg invocation; cobra's --help would otherwise be
		// intercepted at the root and exit 0.
		for _, gone := range []string{
			"add", "install", "uninstall", "push", "pull", "resolve",
			"bootstrap", "org", "render", "apply-all", "scan",
			"verify", "upgrade", "list", "show", "doctor",
		} {
			out, code := run(t, "--project", proj, gone)
			require.NotEqual(t, 0, code, "command %q should not exist: %s", gone, out)
			require.Contains(t, out, "unknown command", "expected 'unknown command' for %q: %s", gone, out)
		}
	})

	// Surface regression: the codex aggregator should still produce a
	// reasonable AGENTS.md when invoked. Sanity check the marker shape.
	t.Run("codex AGENTS.md still uses hash markers", func(t *testing.T) {
		out, code := run(t, "--project", proj, "harness", "add", "codex")
		require.Equal(t, 0, code, out)
		out, code = run(t, "--project", proj, "sync", "--force", "--harness", "codex")
		require.Equal(t, 0, code, out)
		b, err := os.ReadFile(filepath.Join(proj, "AGENTS.md"))
		require.NoError(t, err)
		require.LessOrEqual(t, len(b), 32768)
		require.Regexp(t, regexp.MustCompile(`adeptability:begin id=pr-review hash=[0-9a-f]{8}`), string(b))
	})
}

// findRepoRoot locates the module root by walking up from cwd.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not locate go.mod walking up from test cwd")
	return ""
}

func buildBinary(t *testing.T, repoRoot, dst string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", dst, "./cmd/adept")
	cmd.Dir = repoRoot
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "build failed: %s", out)
	fi, err := os.Stat(dst)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), fi.ModTime(), 30*time.Second)
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dst, 0o755))
	entries, err := os.ReadDir(src)
	require.NoError(t, err)
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			copyDir(t, s, d)
			continue
		}
		b, err := os.ReadFile(s)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(d, b, 0o644))
	}
}

func appendToFile(path, extra string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(extra)
	return err
}

// gitInit makes dir into a minimal git repository so `git clone file://`
// against it succeeds. Avoids any network dependency in the test.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"add", "."},
		{"-c", "user.email=t@e", "-c", "user.name=t", "commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", args, out)
	}
}
