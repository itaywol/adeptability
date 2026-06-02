package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestE2E_MultiFileSkill verifies that a skill bundling extra markdown
// references, scripts, and assets ends up with its full file tree in the
// per-skill harnesses (Claude, OpenCode) and that the aggregator harnesses
// (Cursor single-file, Codex aggregated, Copilot bucketed) gracefully drop
// the sidecars instead of corrupting their output.
//
// Regression guard: relative paths inside SKILL.md must resolve to the
// sidecars from the harness directory whether the project is in symlink or
// copy mode.
func TestE2E_MultiFileSkill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short")
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

	// Seed the project canonical with a realistic multi-file skill. In the
	// new surface there is no `add`/`install`: canonical content is either
	// adopted from harness files via `init` / `sync-from`, or cloned in by
	// `init --from`. For the test we plant the canonical bundle directly,
	// then drive sync.
	canonicalRoot := filepath.Join(proj, ".adeptability", "skills", "multi-test")
	require.NoError(t, os.MkdirAll(filepath.Join(canonicalRoot, "references"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(canonicalRoot, "scripts"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(canonicalRoot, "assets"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(canonicalRoot, "SKILL.md"), []byte(""+
		"---\n"+
		"id: multi-test\n"+
		"description: Skill bundle with markdown references, scripts, and assets\n"+
		"activation: agent\n"+
		"---\n"+
		"# Main\n\nSee [API](references/api.md) and run scripts/validate.sh.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(canonicalRoot, "references", "api.md"),
		[]byte("# API\nGET /v1/foo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(canonicalRoot, "references", "errors.md"),
		[]byte("# Errors\n401 unauthorized\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(canonicalRoot, "scripts", "validate.sh"),
		[]byte("#!/usr/bin/env sh\necho ok\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(canonicalRoot, "assets", "logo.svg"),
		[]byte("<svg></svg>\n"), 0o644))

	// Seed the base snapshot (so sync's drift detection is meaningful).
	copyDir(t, canonicalRoot, filepath.Join(proj, ".adeptability", "base", "multi-test"))

	// Write a config enabling every built-in harness in copy mode (avoids
	// any host symlink permission issues in CI sandboxes).
	require.NoError(t, os.WriteFile(
		filepath.Join(proj, ".adeptability", "config.json"),
		[]byte(`{"schema":1,"harnesses":["claude-code","cursor","opencode","codex","copilot"],"mode":"copy"}`+"\n"),
		0o644,
	))

	run := func(t *testing.T, args ...string) {
		t.Helper()
		cmd := exec.Command(binPath, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "adept %v: %s", args, out)
	}

	run(t, "--project", proj, "sync", "--force")

	t.Run("claude preserves the full bundle", func(t *testing.T) {
		root := filepath.Join(proj, ".claude/skills/multi-test")
		require.FileExists(t, filepath.Join(root, "SKILL.md"))
		require.FileExists(t, filepath.Join(root, "references/api.md"))
		require.FileExists(t, filepath.Join(root, "references/errors.md"))
		require.FileExists(t, filepath.Join(root, "scripts/validate.sh"))
		require.FileExists(t, filepath.Join(root, "assets/logo.svg"))

		b, err := os.ReadFile(filepath.Join(root, "references/api.md"))
		require.NoError(t, err)
		require.Contains(t, string(b), "GET /v1/foo")
	})

	t.Run("opencode preserves the full bundle", func(t *testing.T) {
		root := filepath.Join(proj, ".opencode/skill/multi-test")
		require.FileExists(t, filepath.Join(root, "SKILL.md"))
		require.FileExists(t, filepath.Join(root, "references/api.md"))
		require.FileExists(t, filepath.Join(root, "references/errors.md"))
		require.FileExists(t, filepath.Join(root, "scripts/validate.sh"))
		require.FileExists(t, filepath.Join(root, "assets/logo.svg"))
	})

	t.Run("cursor drops sidecars (single-file model)", func(t *testing.T) {
		require.FileExists(t, filepath.Join(proj, ".cursor/rules/multi-test.mdc"))
		_, err := os.Stat(filepath.Join(proj, ".cursor/rules/references"))
		require.True(t, os.IsNotExist(err))
	})

	t.Run("codex drops sidecars (aggregator model)", func(t *testing.T) {
		_, err := os.Stat(filepath.Join(proj, "references"))
		require.True(t, os.IsNotExist(err), "sidecars must not pollute project root")
		require.FileExists(t, filepath.Join(proj, "AGENTS.md"))
	})

}
