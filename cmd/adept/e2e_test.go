package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestE2E exercises the full happy-path: init -> add -> install -> sync ->
// status. It is gated on the binary being built first; when run via
// `go test ./...` it rebuilds the binary into a temp file.
//
// This is the regression net for the goal: "accurate transfer across every
// harness." If anything breaks the canonical -> per-harness pipeline for any
// of the five built-in harnesses, this test fails.
func TestE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e under -short")
	}

	repoRoot := findRepoRoot(t)
	binPath := filepath.Join(t.TempDir(), "adept")
	buildBinary(t, repoRoot, binPath)

	lib := filepath.Join(t.TempDir(), "lib")
	proj := filepath.Join(t.TempDir(), "proj")
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"ADEPT_LIBRARY=" + lib,
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

	t.Run("init library and project", func(t *testing.T) {
		out, code := run(t, "init", "library")
		require.Equal(t, 0, code, out)
		// New model: library is just a directory tree — no lockfile.
		_, err := os.Stat(filepath.Join(lib, "adeptability.lock.json"))
		require.True(t, os.IsNotExist(err), "library must not write a lockfile")
		out, code = run(t, "--project", proj, "init", "project")
		require.Equal(t, 0, code, out)
		require.FileExists(t, filepath.Join(proj, ".adeptability", "config.json"))
		_, err = os.Stat(filepath.Join(proj, "adeptability.lock.json"))
		require.True(t, os.IsNotExist(err), "project must not write a lockfile")
	})

	exampleDir := filepath.Join(repoRoot, "examples", "skills")
	t.Run("add and install two skills", func(t *testing.T) {
		out, code := run(t, "add", filepath.Join(exampleDir, "pr-review"))
		require.Equal(t, 0, code, out)
		out, code = run(t, "add", filepath.Join(exampleDir, "typescript-style"))
		require.Equal(t, 0, code, out)
		out, code = run(t, "--project", proj, "install", "pr-review")
		require.Equal(t, 0, code, out)
		out, code = run(t, "--project", proj, "install", "typescript-style")
		require.Equal(t, 0, code, out)
	})

	t.Run("enable every harness and sync", func(t *testing.T) {
		for _, h := range []string{"claude-code", "cursor", "opencode", "codex", "copilot"} {
			out, code := run(t, "--project", proj, "harness", "enable", "--id", h)
			require.Equal(t, 0, code, out)
		}
		out, code := run(t, "--project", proj, "harness", "sync", "--force")
		require.Equal(t, 0, code, out)
	})

	t.Run("claude SKILL.md has expected frontmatter", func(t *testing.T) {
		path := filepath.Join(proj, ".claude/skills/pr-review/SKILL.md")
		require.FileExists(t, path)
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		s := string(b)
		require.Contains(t, s, "name: pr-review")
		require.Contains(t, s, "description: Apply before opening a PR")
		require.Contains(t, s, "allowed-tools")
	})

	t.Run("cursor mdc has globs for typescript-style", func(t *testing.T) {
		path := filepath.Join(proj, ".cursor/rules/typescript-style.mdc")
		require.FileExists(t, path)
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		s := string(b)
		require.Contains(t, s, "description:")
		require.Contains(t, s, "globs:")
		require.Contains(t, s, "**/*.ts")
		require.Contains(t, s, "alwaysApply: false")
	})

	t.Run("opencode SKILL.md is plain markdown", func(t *testing.T) {
		path := filepath.Join(proj, ".opencode/skill/pr-review/SKILL.md")
		require.FileExists(t, path)
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		s := string(b)
		require.True(t, strings.HasPrefix(s, "# pr-review"), "expected `# <id>` heading, got: %s", s[:min(80, len(s))])
	})

	t.Run("codex aggregates AGENTS.md within budget", func(t *testing.T) {
		path := filepath.Join(proj, "AGENTS.md")
		require.FileExists(t, path)
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		require.LessOrEqual(t, len(b), 32768, "AGENTS.md must fit 32 KiB")
		s := string(b)
		// Both skills present, both with section markers carrying the new
		// hash form (id=<id> hash=<8hex>) instead of the old version=N.
		require.Regexp(t, regexp.MustCompile(`adeptability:begin id=pr-review hash=[0-9a-f]{8}`), s)
		require.Regexp(t, regexp.MustCompile(`adeptability:begin id=typescript-style hash=[0-9a-f]{8}`), s)
		require.NotContains(t, s, "version=", "section markers must use hash, not version")
	})

	t.Run("copilot bucket has applyTo glob", func(t *testing.T) {
		dir := filepath.Join(proj, ".github/instructions")
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		require.NotEmpty(t, entries)
		var seenApplyTo bool
		for _, e := range entries {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			require.NoError(t, err)
			if strings.Contains(string(b), "applyTo: \"**/*.ts,**/*.tsx\"") {
				seenApplyTo = true
			}
		}
		require.True(t, seenApplyTo, "expected one bucket with applyTo for ts/tsx globs")
	})

	t.Run("status reports synced after fresh install", func(t *testing.T) {
		out, code := run(t, "--project", proj, "status", "--json")
		require.Equal(t, 0, code, out)
		var payload struct {
			Skills []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"skills"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &payload))
		require.NotEmpty(t, payload.Skills)
		for _, s := range payload.Skills {
			require.Equal(t, "synced", s.Status, "skill %s should be synced", s.ID)
		}
	})

	t.Run("status JSON contains no version field", func(t *testing.T) {
		out, code := run(t, "--project", proj, "status", "--json")
		require.Equal(t, 0, code, out)
		require.NotContains(t, out, `"version"`, "v0.2 status output must not surface a version field")
	})

	t.Run("diff equal", func(t *testing.T) {
		out, code := run(t, "--project", proj, "diff", "pr-review")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "equal    true")
	})

	t.Run("render command prints rendered bytes", func(t *testing.T) {
		out, code := run(t, "--project", proj, "render", "--id", "pr-review", "--harness", "claude-code")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "name: pr-review")
	})

	t.Run("config-driven adapter registers and persists", func(t *testing.T) {
		out, code := run(t, "harness", "add", "--from", filepath.Join(repoRoot, "examples/adapters/jetbrains-junie.yaml"))
		require.Equal(t, 0, code, out)
		require.FileExists(t, filepath.Join(lib, "adapters", "jetbrains-junie.yaml"))
		out, code = run(t, "harness", "list", "--json")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "jetbrains-junie")
	})

	// FRICTION BUG 2: uninstalling a missing skill must exit non-zero with
	// a clear message rather than silently succeeding.
	t.Run("uninstall missing skill exits non-zero", func(t *testing.T) {
		out, code := run(t, "--project", proj, "uninstall", "definitely-not-installed")
		require.Equal(t, 1, code, out)
		require.Contains(t, strings.ToLower(out), "skill not found")
	})

	// FRICTION BUG 4: harness enable with unknown id must exit non-zero
	// instead of writing a bogus harness id into the config.
	t.Run("harness enable with unknown id rejected", func(t *testing.T) {
		out, code := run(t, "--project", proj, "harness", "enable", "--id", "totally-fake-harness")
		require.Equal(t, 1, code, out)
		require.Contains(t, strings.ToLower(out), "harness")
	})

	// FRICTION BUG 3: render with unknown harness must surface the harness
	// in the error, not the (still-missing) skill.
	t.Run("render with unknown harness names the harness", func(t *testing.T) {
		out, code := run(t, "--project", proj, "render", "--id", "pr-review", "--harness", "made-up")
		require.Equal(t, 1, code, out)
		require.Contains(t, strings.ToLower(out), "harness")
		require.NotContains(t, strings.ToLower(out), "skill not found")
	})

	// FRICTION BUG 6: scan of a missing root must exit non-zero, not print
	// an empty table and succeed.
	t.Run("scan missing root exits non-zero", func(t *testing.T) {
		out, code := run(t, "scan", filepath.Join(t.TempDir(), "does-not-exist"))
		require.Equal(t, 1, code, out)
	})

	// `migrate` command is intentionally removed in v0.2 — no backward
	// compatibility, no migrations. The CLI must not advertise it.
	t.Run("migrate command is gone", func(t *testing.T) {
		out, code := run(t, "migrate", "--from", "/no/where")
		require.NotEqual(t, 0, code, out)
		require.NotContains(t, strings.ToLower(out), "migrated 0 skill")
	})
}

// findRepoRoot locates the module root by walking up from the current working
// directory until go.mod is found.
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
