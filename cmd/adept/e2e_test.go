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

// TestE2E exercises the new v0.3 surface end-to-end:
//
//   adept init [--from <url>] [--mode symlink|copy]
//   adept sync [--harness <id>] [--force] [--dry-run]
//   adept sync-from [--harness <id>] [--all] [--force]
//   adept diff [--harness <id>]
//   adept list [--from-library]
//   adept show <id> [--from-library]
//   adept doctor
//
// The three target flows are: init on empty project, init with library
// remote, init in a project that already has harness skills on disk
// (auto-adopt). After init, sync/sync-from/diff round-trip canonical and
// harness state.
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

	// Seed the library with two canonical-form skills. The example skills
	// under examples/skills/* are split (skill.yaml + SKILL.md) and rely on
	// `adept add` to consolidate into a single canonical SKILL.md. The new
	// surface has no `add`, so we plant the consolidated form directly.
	require.NoError(t, os.MkdirAll(filepath.Join(lib, "skills", "pr-review"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(lib, "skills", "pr-review", "SKILL.md"),
		[]byte("---\n"+
			"id: pr-review\n"+
			"description: Apply before opening a PR. Tests, security, performance.\n"+
			"activation: agent\n"+
			"allowed-tools: [Read, Grep, Bash]\n"+
			"---\n"+
			"# PR Review Checklist\n\nUse this before requesting review on a pull request.\n"),
		0o644,
	))
	require.NoError(t, os.MkdirAll(filepath.Join(lib, "skills", "typescript-style"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(lib, "skills", "typescript-style", "SKILL.md"),
		[]byte("---\n"+
			"id: typescript-style\n"+
			"description: Project TypeScript conventions. Use when editing .ts or .tsx files.\n"+
			"activation: globs\n"+
			"globs:\n"+
			"  - \"**/*.ts\"\n"+
			"  - \"**/*.tsx\"\n"+
			"---\n"+
			"# TypeScript Style\n\nUse const for values never reassigned.\n"),
		0o644,
	))

	t.Run("init on empty project (no harness files yet)", func(t *testing.T) {
		out, code := run(t, "--project", proj, "init")
		require.Equal(t, 0, code, out)
		require.FileExists(t, filepath.Join(proj, ".adeptability", "config.json"))
		require.NoFileExists(t, filepath.Join(proj, "adeptability.lock.json"))
		// No harness output yet because nothing is enabled — adoption only
		// kicks in when harness files already exist on disk.
		require.NoDirExists(t, filepath.Join(proj, ".claude"))
	})

	t.Run("init seeds harnesses=[] and mode=symlink by default", func(t *testing.T) {
		out, code := run(t, "--project", proj, "doctor", "--json")
		require.Equal(t, 0, code, out)
		var rep struct {
			Mode      string `json:"mode"`
			Harnesses []struct {
				ID string `json:"id"`
			} `json:"harnesses"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &rep))
		require.Equal(t, "symlink", rep.Mode)
		require.NotEmpty(t, rep.Harnesses, "built-in harnesses must be registered")
	})

	t.Run("init in project with preexisting harness files auto-adopts", func(t *testing.T) {
		adoptProj := filepath.Join(t.TempDir(), "adopt")
		require.NoError(t, os.MkdirAll(filepath.Join(adoptProj, ".claude", "skills", "preexisting"), 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(adoptProj, ".claude", "skills", "preexisting", "SKILL.md"),
			[]byte("---\nname: preexisting\ndescription: pre-existing harness skill\n---\n# body\n"),
			0o644,
		))
		out, code := run(t, "--project", adoptProj, "init", "--mode", "copy")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "adopted harnesses: claude-code")
		require.FileExists(t, filepath.Join(adoptProj, ".adeptability", "skills", "preexisting", "SKILL.md"))
		// config should record claude-code + mode=copy
		cfgBytes, err := os.ReadFile(filepath.Join(adoptProj, ".adeptability", "config.json"))
		require.NoError(t, err)
		require.Contains(t, string(cfgBytes), `"claude-code"`)
		require.Contains(t, string(cfgBytes), `"mode": "copy"`)
	})

	// After this point, manually wire one skill + enable a few harnesses
	// so sync/diff/sync-from have content to operate on.
	t.Run("seed canonical skill + enable harnesses", func(t *testing.T) {
		// Copy a skill into project canonical the cheap way (no `install`
		// command anymore — projects normally adopt from a harness or pull
		// the library via init --from).
		copyDir(t, filepath.Join(lib, "skills", "pr-review"), filepath.Join(proj, ".adeptability", "skills", "pr-review"))
		copyDir(t, filepath.Join(lib, "skills", "pr-review"), filepath.Join(proj, ".adeptability", "base", "pr-review"))
		writeJSON(t, filepath.Join(proj, ".adeptability", "config.json"), map[string]any{
			"schema":    1,
			"harnesses": []string{"claude-code", "cursor", "codex", "copilot", "opencode"},
			"mode":      "copy",
		})
	})

	t.Run("sync renders to every enabled harness", func(t *testing.T) {
		out, code := run(t, "--project", proj, "sync", "--force")
		require.Equal(t, 0, code, out)
		require.FileExists(t, filepath.Join(proj, ".claude/skills/pr-review/SKILL.md"))
		require.FileExists(t, filepath.Join(proj, ".cursor/rules/pr-review.mdc"))
		require.FileExists(t, filepath.Join(proj, ".opencode/skill/pr-review/SKILL.md"))
		require.FileExists(t, filepath.Join(proj, "AGENTS.md"))
	})

	t.Run("claude SKILL.md carries the expected frontmatter", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(proj, ".claude/skills/pr-review/SKILL.md"))
		require.NoError(t, err)
		s := string(b)
		require.Contains(t, s, "name: pr-review")
		require.Contains(t, s, "description: Apply before opening a PR")
		require.Contains(t, s, "allowed-tools")
	})

	t.Run("codex AGENTS.md uses hash markers, fits 32 KiB", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(proj, "AGENTS.md"))
		require.NoError(t, err)
		require.LessOrEqual(t, len(b), 32768)
		require.Regexp(t, regexp.MustCompile(`adeptability:begin id=pr-review hash=[0-9a-f]{8}`), string(b))
		require.NotContains(t, string(b), "version=", "section markers must use hash, not version")
	})

	t.Run("diff is clean after fresh sync", func(t *testing.T) {
		out, code := run(t, "--project", proj, "diff", "--json")
		require.Equal(t, 0, code, out)
		var reports []struct {
			Harness string   `json:"harness"`
			Drifted []string `json:"drifted"`
		}
		require.NoError(t, json.Unmarshal([]byte(out), &reports))
		for _, r := range reports {
			require.Empty(t, r.Drifted, "harness %s should be clean", r.Harness)
		}
	})

	t.Run("diff reports drift after harness-side edit", func(t *testing.T) {
		require.NoError(t, appendToFile(
			filepath.Join(proj, ".claude/skills/pr-review/SKILL.md"),
			"\n## harness-edit\n",
		))
		out, code := run(t, "--project", proj, "diff", "--harness", "claude-code")
		require.Equal(t, 2, code, out)
		require.Contains(t, out, "drift")
	})

	t.Run("sync-from --harness adopts that harness's edit", func(t *testing.T) {
		out, code := run(t, "--project", proj, "sync-from", "--harness", "claude-code", "--force")
		require.Equal(t, 0, code, out)
		b, err := os.ReadFile(filepath.Join(proj, ".adeptability", "skills", "pr-review", "SKILL.md"))
		require.NoError(t, err)
		require.Contains(t, string(b), "harness-edit")
	})

	t.Run("list shows the canonical project skill", func(t *testing.T) {
		out, code := run(t, "--project", proj, "list")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "pr-review")
	})

	t.Run("list --from-library shows library skills", func(t *testing.T) {
		out, code := run(t, "list", "--from-library")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "pr-review")
		require.Contains(t, out, "typescript-style")
	})

	t.Run("show emits a labeled table by default", func(t *testing.T) {
		out, code := run(t, "--project", proj, "show", "pr-review")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "ID")
		require.Contains(t, out, "pr-review")
		// Plain output is not JSON.
		require.False(t, strings.HasPrefix(strings.TrimSpace(out), "{"))
	})

	t.Run("show --json emits structured output", func(t *testing.T) {
		out, code := run(t, "--project", proj, "--json", "show", "pr-review")
		require.Equal(t, 0, code, out)
		var s map[string]any
		require.NoError(t, json.Unmarshal([]byte(out), &s))
		require.Equal(t, "pr-review", s["id"])
	})

	t.Run("doctor reports project + library status", func(t *testing.T) {
		out, code := run(t, "--project", proj, "doctor")
		require.Equal(t, 0, code, out)
		require.Contains(t, out, "library: ok")
		require.Contains(t, out, "project: ok")
		require.Contains(t, out, "mode: copy")
	})

	t.Run("cut commands are not registered", func(t *testing.T) {
		for _, gone := range []string{
			"add", "install", "uninstall", "push", "pull", "status", "resolve",
			"bootstrap", "harness", "org", "render", "apply-all", "scan",
			"verify", "upgrade",
		} {
			out, code := run(t, gone, "--help")
			require.NotEqual(t, 0, code, "command %q should not exist: %s", gone, out)
		}
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

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(b, '\n'), 0o644))
}
