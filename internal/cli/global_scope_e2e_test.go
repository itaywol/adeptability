package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/render/claude"
	"github.com/itaywol/adeptability/pkg/adept"
)

// TestGlobalScopeEndToEnd drives the full global-scope lifecycle through the
// root CLI surface only — harness add, skill authoring + sync, foreign-file
// safety across two sync cycles, library add + render, status, and project
// isolation — inside a single sequential scenario rooted at t.TempDir().
//
// Isolation: ADEPT_LIBRARY points at tmp/.adeptability so the render root is
// tmp (never the real $HOME); every --global invocation also pins --project to
// an isolated dir so a scope regression away from --global cannot silently
// write into cwd.
func TestGlobalScopeEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	libRoot := filepath.Join(tmp, ".adeptability")
	t.Setenv("ADEPT_LIBRARY", libRoot)

	claudeID := claude.Spec().ID
	pin := t.TempDir() // isolated --project pin; global scope must ignore it.

	runRoot := func(args ...string) error {
		t.Helper()
		root := NewRoot(BuildInfo{Version: "test"})
		root.SetArgs(args)
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		return root.Execute()
	}
	runGlobal := func(args ...string) error {
		t.Helper()
		return runRoot(append([]string{"--global", "--project", pin}, args...)...)
	}
	requireUnchanged := func(path string, want []byte, when string) {
		t.Helper()
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, want, got, "foreign harness file must survive %s byte-identical", when)
	}

	// ---- Step 1: enable the claude harness in the global scope. ----
	require.NoError(t, runGlobal("harness", "add", claudeID))

	cfgPath := filepath.Join(libRoot, adept.ConfigFileName)
	require.FileExists(t, cfgPath, "global config created at library root")
	raw, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	var cfg struct {
		Harnesses []string `json:"harnesses"`
	}
	require.NoError(t, json.Unmarshal(raw, &cfg))
	require.Contains(t, cfg.Harnesses, claudeID, "harness persisted to global config")

	// ---- Foreign-file safety setup: a harness file adept does not own,
	// pre-created before any sync so we can prove sync never touches it. ----
	foreignPath := filepath.Join(tmp, ".claude", "skills", "foreign", adept.SkillFileName)
	require.NoError(t, os.MkdirAll(filepath.Dir(foreignPath), 0o755))
	foreignContent := []byte("---\nname: foreign\ndescription: not managed by adept\n---\nhands off\n")
	require.NoError(t, os.WriteFile(foreignPath, foreignContent, 0o644))

	// ---- Step 2: author a canonical skill globally and sync it out. ----
	require.NoError(t, runGlobal("skill", "add", "hello"))
	require.FileExists(t, filepath.Join(libRoot, adept.SkillsDirName, "hello", adept.SkillFileName),
		"skill add writes into the global canonical (library root)")

	require.NoError(t, runGlobal("sync")) // sync cycle 1
	helloOut := filepath.Join(tmp, ".claude", "skills", "hello", adept.SkillFileName)
	require.FileExists(t, helloOut, "global sync renders into the home-level harness target")
	requireUnchanged(foreignPath, foreignContent, "sync cycle 1")

	// ---- Step 3: a forced re-sync still leaves the foreign file untouched. ----
	require.NoError(t, runGlobal("sync", "--force")) // sync cycle 2 (forced)
	require.FileExists(t, helloOut)
	requireUnchanged(foreignPath, foreignContent, "sync cycle 2 (--force)")

	// ---- Step 4: add a library from a local git fixture, then render it. ----
	upstream := t.TempDir()
	gitRun(t, upstream, "init", "-b", "main")
	gitRun(t, upstream, "config", "user.email", "t@example.com")
	gitRun(t, upstream, "config", "user.name", "Test")
	writeSkillFile(t, upstream, "teamskill", "from the team library")
	gitRun(t, upstream, "add", ".")
	gitRun(t, upstream, "commit", "-m", "init")

	require.NoError(t, runGlobal("library", "add", "team", "--from", upstream))
	clone := filepath.Join(libRoot, "libs", "team")
	require.DirExists(t, clone, "library cloned into the global machine store")
	require.FileExists(t, filepath.Join(clone, adept.SkillsDirName, "teamskill", adept.SkillFileName))

	require.NoError(t, runGlobal("sync")) // sync cycle 3: now renders the library skill too
	teamOut := filepath.Join(tmp, ".claude", "skills", "teamskill", adept.SkillFileName)
	require.FileExists(t, teamOut, "library skill renders into the home-level harness target")
	requireUnchanged(foreignPath, foreignContent, "sync cycle 3 (library)")

	// ---- Step 5: status exits cleanly and reports a fully-synced harness. ----
	var statusOut bytes.Buffer
	statusRootExec := func() error {
		root := NewRoot(BuildInfo{Version: "test"})
		root.SetArgs([]string{"--global", "--project", pin, "--json", "status"})
		root.SetOut(&statusOut)
		root.SetErr(io.Discard)
		return root.Execute()
	}
	require.NoError(t, statusRootExec(), "status must exit cleanly (no drift/missing => no ErrDirty)")

	var rep statusReport
	require.NoError(t, json.Unmarshal(statusOut.Bytes(), &rep))
	require.True(t, rep.Initialized)
	require.Zero(t, rep.MissingLibraries, "the team library resolves on disk")
	var claudeSynced int
	foundHarness := false
	for _, h := range rep.Harnesses {
		if h.ID == claudeID {
			foundHarness = true
			claudeSynced = h.Synced
			require.Zero(t, h.Drifted, "no drift after sync")
			require.Zero(t, h.Missing, "nothing missing after sync")
			require.Zero(t, h.Conflict, "no conflicts after sync")
		}
	}
	require.True(t, foundHarness, "status reports the enabled claude harness")
	require.GreaterOrEqual(t, claudeSynced, 2, "hello + teamskill are both synced")

	// ---- Step 6: a project-scoped sync must not leak into the global tree. ----
	globalBefore := listSkillDirs(t, filepath.Join(tmp, ".claude", "skills"))

	projDir := t.TempDir()
	projBase := filepath.Join(projDir, adept.BaseDirName)
	projSkill := filepath.Join(projBase, adept.SkillsDirName, "projonly")
	require.NoError(t, os.MkdirAll(projSkill, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projSkill, adept.SkillFileName),
		skillMD("projonly", "project scoped only"), 0o644))
	projCfg := map[string]any{"schema": adept.ConfigSchemaVersion, "harnesses": []string{claudeID}}
	projCfgBytes, err := json.Marshal(projCfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(projBase, adept.ConfigFileName), projCfgBytes, 0o644))

	require.NoError(t, runRoot("--project", projDir, "sync"))
	require.FileExists(t, filepath.Join(projDir, ".claude", "skills", "projonly", adept.SkillFileName),
		"project sync renders into the project tree")

	globalAfter := listSkillDirs(t, filepath.Join(tmp, ".claude", "skills"))
	require.Equal(t, globalBefore, globalAfter,
		"project-scoped sync must not add/remove anything in the global render tree")
	require.NotContains(t, globalAfter, "projonly", "the project-only skill never reaches the global tree")
}

// listSkillDirs returns the sorted set of immediate child dir names under dir,
// or nil if dir does not exist.
func listSkillDirs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}
