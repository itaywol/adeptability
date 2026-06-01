package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestE2E_BootstrapAndReverseImport covers the bidirectional flow:
//
//  1. A project starts with existing Claude and Cursor skills (no adept layout).
//  2. `adept bootstrap` reverse-renders everything into project canonical.
//  3. `adept push` publishes the imports to the central library.
//  4. A user edits the harness file directly; `adept harness import --id <h>
//     --force` re-adopts the edit; status reports `diverged` until they push.
//
// This is the regression guard for the import contract — any future change
// that drops a sidecar, mangles reverse frontmatter, or breaks per-harness
// detection fails one of these subtests.
func TestE2E_BootstrapAndReverseImport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short")
	}
	repoRoot := findRepoRoot(t)
	bin := filepath.Join(t.TempDir(), "adept")
	buildBinary(t, repoRoot, bin)

	lib := filepath.Join(t.TempDir(), "lib")
	proj := filepath.Join(t.TempDir(), "proj")
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"ADEPT_LIBRARY=" + lib,
	}

	// Seed the project with an existing Claude skill (including sidecars) and
	// a Cursor rule — exactly what a real adopter has on disk today.
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".claude/skills/legacy/references"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".claude/skills/legacy/SKILL.md"), []byte(""+
		"---\n"+
		"name: legacy\n"+
		"description: Existing Claude skill written before adept adoption\n"+
		"allowed-tools: [Read, Grep]\n"+
		"---\n\n"+
		"# Legacy\n\nBody from .claude/.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".claude/skills/legacy/references/notes.md"),
		[]byte("# Notes\nReference content.\n"), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".cursor/rules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(proj, ".cursor/rules/ts-rule.mdc"), []byte(""+
		"---\n"+
		"description: TypeScript conventions\n"+
		"globs: [\"**/*.ts\", \"**/*.tsx\"]\n"+
		"alwaysApply: false\n"+
		"---\n\n# TS\nUse interfaces.\n"), 0o644))

	run := func(t *testing.T, args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		code := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("adept %v: %v\n%s", args, err, out)
		}
		return string(out), code
	}

	run(t, "init", "library")

	t.Run("bootstrap discovers claude+cursor", func(t *testing.T) {
		out, code := run(t, "--project", proj, "bootstrap")
		require.Equal(t, 0, code, out)
		require.FileExists(t, filepath.Join(proj, ".adeptability/skills/legacy/SKILL.md"))
		require.FileExists(t, filepath.Join(proj, ".adeptability/skills/legacy/references/notes.md"))
		require.FileExists(t, filepath.Join(proj, ".adeptability/skills/ts-rule/SKILL.md"))
	})

	t.Run("imported claude skill reverse-maps frontmatter", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(proj, ".adeptability/skills/legacy/SKILL.md"))
		require.NoError(t, err)
		s := string(b)
		require.Contains(t, s, "id: legacy")
		require.Contains(t, s, "description: \"Existing Claude skill written before adept adoption\"")
		require.Contains(t, s, "allowed-tools:")
		require.Contains(t, s, "- \"Read\"")
		require.Contains(t, s, "Body from .claude/.")
	})

	t.Run("imported cursor rule reverse-maps to activation=globs", func(t *testing.T) {
		b, err := os.ReadFile(filepath.Join(proj, ".adeptability/skills/ts-rule/SKILL.md"))
		require.NoError(t, err)
		s := string(b)
		require.Contains(t, s, "id: ts-rule")
		require.Contains(t, s, "activation: globs")
		require.Contains(t, s, "- \"**/*.ts\"")
		require.Contains(t, s, "- \"**/*.tsx\"")
	})

	t.Run("push imports to library", func(t *testing.T) {
		out, code := run(t, "--project", proj, "push", "legacy")
		require.Equal(t, 0, code, out)
		out, code = run(t, "--project", proj, "push", "ts-rule")
		require.Equal(t, 0, code, out)
		// status now synced because canonical hash = library hash.
		out, code = run(t, "--project", proj, "status", "--json")
		require.Equal(t, 0, code, out)
		var payload struct {
			Skills []struct{ ID, Status string }
		}
		require.NoError(t, json.Unmarshal([]byte(out), &payload))
		for _, s := range payload.Skills {
			require.Equal(t, "synced", s.Status, "skill %s", s.ID)
		}
	})

	t.Run("direct harness edit then re-import shows diverged", func(t *testing.T) {
		// Append a section directly to the Claude file as a user might.
		f, err := os.OpenFile(filepath.Join(proj, ".claude/skills/legacy/SKILL.md"),
			os.O_APPEND|os.O_WRONLY, 0o644)
		require.NoError(t, err)
		_, err = f.WriteString("\n## Hand-edited section\nNew rule.\n")
		require.NoError(t, err)
		require.NoError(t, f.Close())

		out, code := run(t, "--project", proj, "harness", "import", "--id", "claude-code", "--force")
		require.Equal(t, 0, code, out)
		// Project canonical now contains the new section; library still has
		// the previous version, so status is diverged (exit 2).
		_, code = run(t, "--project", proj, "status")
		require.Equal(t, 2, code, "status should signal dirty after reverse-import")
		b, err := os.ReadFile(filepath.Join(proj, ".adeptability/skills/legacy/SKILL.md"))
		require.NoError(t, err)
		require.Contains(t, string(b), "Hand-edited section")
	})

	t.Run("aggregator-only project: codex AGENTS.md import", func(t *testing.T) {
		proj2 := filepath.Join(t.TempDir(), "agents-only")
		require.NoError(t, os.MkdirAll(proj2, 0o755))
		// Write a plain AGENTS.md with no markers.
		require.NoError(t, os.WriteFile(filepath.Join(proj2, "AGENTS.md"),
			[]byte("# Codex Context\n\nBuild with `make`. Test with `go test`.\n"), 0o644))
		out, code := run(t, "--project", proj2, "bootstrap")
		require.Equal(t, 0, code, out)
		require.FileExists(t, filepath.Join(proj2, ".adeptability/skills/agents/SKILL.md"))
		b, err := os.ReadFile(filepath.Join(proj2, ".adeptability/skills/agents/SKILL.md"))
		require.NoError(t, err)
		require.Contains(t, string(b), "Build with `make`")
	})
}
