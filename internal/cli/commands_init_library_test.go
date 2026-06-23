package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

// TestInitAsLibrary verifies that `adept init --as-library` initializes a
// publishable library: canonical skills land at <root>/skills/ (where a
// consumer's library resolution reads them), config records the layout, and
// the consumer-only seeding/adoption steps are skipped. It also confirms a
// freshly resolved Project() detects the library layout from config alone.
func TestInitAsLibrary(t *testing.T) {
	root := t.TempDir()
	d := testDeps(t, root, t.TempDir())

	cmd := newInitCmd(d)
	cmd.SetArgs([]string{"--as-library"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	// Skills live at <root>/skills/, NOT <root>/.adeptability/skills/.
	require.DirExists(t, filepath.Join(root, adept.SkillsDirName))
	require.NoDirExists(t, filepath.Join(root, adept.BaseDirName, adept.SkillsDirName))

	// Metadata still lives under .adeptability/.
	require.DirExists(t, filepath.Join(root, adept.BaseDirName, adept.BaseSnapDir))
	cfgPath := filepath.Join(root, adept.BaseDirName, adept.ConfigFileName)
	require.FileExists(t, cfgPath)

	cfg, err := d.Config.Read(cfgPath)
	require.NoError(t, err)
	require.Equal(t, adept.LayoutLibrary, cfg.Layout)
	// A library curates its own skills: no defaults seeded, no harness/mode.
	require.Empty(t, cfg.Harnesses)
	require.Empty(t, string(cfg.Mode))
	entries, err := os.ReadDir(filepath.Join(root, adept.SkillsDirName))
	require.NoError(t, err)
	require.Empty(t, entries, "library init must not seed default skills")

	// A freshly resolved project detects the library layout from config and
	// points SkillsDir at the repo-root skills/ dir.
	p, err := d.Project()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, adept.SkillsDirName), p.SkillsDir())

	// Sanity: a skill written there is visible through the project.
	skillDir := filepath.Join(root, adept.SkillsDirName, "greet")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, adept.SkillFileName), skillMD("greet", "say hi"), 0o644))
	require.True(t, p.HasSkill("greet"))
}

// TestProjectLayoutDetection checks that Project() reads the layout from
// config.json: absent/consumer → .adeptability/skills/, library → root/skills/.
func TestProjectLayoutDetection(t *testing.T) {
	t.Run("consumer (no layout)", func(t *testing.T) {
		root := t.TempDir()
		d := testDeps(t, root, t.TempDir())
		initProject(t, d, root, &adept.Config{Schema: adept.ConfigSchemaVersion})

		p, err := d.Project()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(root, adept.BaseDirName, adept.SkillsDirName), p.SkillsDir())
	})

	t.Run("library layout", func(t *testing.T) {
		root := t.TempDir()
		d := testDeps(t, root, t.TempDir())
		// SaveConfig writes to <root>/.adeptability/config.json regardless of
		// layout, so a library project can persist its layout marker there.
		p0, err := d.ProjectWithLayout(true)
		require.NoError(t, err)
		require.NoError(t, p0.SaveConfig(&adept.Config{Schema: adept.ConfigSchemaVersion, Layout: adept.LayoutLibrary}))

		p, err := d.Project()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(root, adept.SkillsDirName), p.SkillsDir())
	})
}
