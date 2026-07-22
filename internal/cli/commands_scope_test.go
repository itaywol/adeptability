package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

// TestSyncGlobalRendersToHomeTargets proves `adept --global sync` renders the
// canonical skills at the library root into the home-level harness targets
// (claude's GlobalOutput == OutputPath, so .claude/skills/<id>/SKILL.md under
// the render root), and that a pre-existing foreign harness file untouched by
// canonical survives the sync byte-identical.
func TestSyncGlobalRendersToHomeTargets(t *testing.T) {
	tmp := t.TempDir()
	libRoot := filepath.Join(tmp, ".adeptability")
	t.Setenv("ADEPT_LIBRARY", libRoot)

	// Seed global config enabling the claude harness.
	require.NoError(t, os.MkdirAll(libRoot, 0o755))
	cfg := map[string]any{"schema": adept.ConfigSchemaVersion, "harnesses": []string{"claude-code"}}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(libRoot, adept.ConfigFileName), cfgBytes, 0o644))

	// Seed a canonical skill at the library-root skills dir (global BaseDir).
	skillDir := filepath.Join(libRoot, adept.SkillsDirName, "demo")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, adept.SkillFileName), skillMD("demo", "a demo skill"), 0o644))

	// Foreign-file safety: pre-create a harness file adept does not own.
	foreignPath := filepath.Join(tmp, ".claude", "skills", "foreign", adept.SkillFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(foreignPath), 0o755))
	foreignContent := []byte("---\nname: foreign\ndescription: not managed by adept\n---\nhands off\n")
	require.NoError(t, os.WriteFile(foreignPath, foreignContent, 0o644))

	// Pin --project to an isolated temp dir so nothing can ever write into the
	// package cwd, even if scope resolution regresses away from --global.
	root := NewRoot(BuildInfo{Version: "test"})
	root.SetArgs([]string{"--global", "--project", t.TempDir(), "sync"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	require.NoError(t, root.Execute())

	// Rendered into the home-level target (render root = parent of libRoot).
	require.FileExists(t, filepath.Join(tmp, ".claude", "skills", "demo", adept.SkillFileName))

	// Foreign file survived, byte-identical.
	got, err := os.ReadFile(foreignPath)
	require.NoError(t, err)
	require.Equal(t, foreignContent, got, "foreign harness file must survive sync untouched")
}

// TestHarnessAddGlobalRejectsNonCapable proves `adept --global harness add
// cursor` fails (cursor declares no GlobalOutput) with an error wrapping
// adept.ErrNotGlobalCapable and leaves the global config's harness list empty.
func TestHarnessAddGlobalRejectsNonCapable(t *testing.T) {
	tmp := t.TempDir()
	libRoot := filepath.Join(tmp, ".adeptability")
	t.Setenv("ADEPT_LIBRARY", libRoot)

	require.NoError(t, os.MkdirAll(libRoot, 0o755))
	cfg := map[string]any{"schema": adept.ConfigSchemaVersion, "harnesses": []string{}}
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	cfgPath := filepath.Join(libRoot, adept.ConfigFileName)
	require.NoError(t, os.WriteFile(cfgPath, cfgBytes, 0o644))

	// Pin --project to an isolated temp dir so a project-scoped fallback can
	// never write into the package cwd (guards against RED-phase pollution).
	root := NewRoot(BuildInfo{Version: "test"})
	root.SetArgs([]string{"--global", "--project", t.TempDir(), "harness", "add", "cursor"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err = root.Execute()
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrNotGlobalCapable)

	// Global config's harness list stayed empty.
	raw, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	var out struct {
		Harnesses []string `json:"harnesses"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Empty(t, out.Harnesses, "cursor must not have been persisted to global config")
}
