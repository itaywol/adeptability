package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/defaultskills"
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

	// Published canonical lives at <root>/skills/ and starts EMPTY — the
	// library's own skills are added with `skill add --publish`, not seeded.
	require.DirExists(t, filepath.Join(root, adept.SkillsDirName))
	published, err := os.ReadDir(filepath.Join(root, adept.SkillsDirName))
	require.NoError(t, err)
	require.Empty(t, published, "published skills/ must start empty")

	// The library default helpers are seeded into the PRIVATE dev-canonical at
	// <root>/.adeptability/skills/ (rendered locally, never published).
	privDir := filepath.Join(root, adept.BaseDirName, adept.SkillsDirName)
	require.DirExists(t, privDir)
	require.FileExists(t, filepath.Join(privDir, defaultskills.ManagingLibraryID, adept.SkillFileName))

	// Metadata still lives under .adeptability/.
	require.DirExists(t, filepath.Join(root, adept.BaseDirName, adept.BaseSnapDir))
	cfgPath := filepath.Join(root, adept.BaseDirName, adept.ConfigFileName)
	require.FileExists(t, cfgPath)

	cfg, err := d.Config.Read(cfgPath)
	require.NoError(t, err)
	require.Equal(t, adept.LayoutLibrary, cfg.Layout)
	// No harness/mode stamped for a library.
	require.Empty(t, cfg.Harnesses)
	require.Empty(t, string(cfg.Mode))

	// A freshly resolved project detects the library layout from config: the
	// published canonical is repo-root skills/, the private one is under
	// .adeptability/, and the seeded helper resolves as a private skill.
	p, err := d.Project()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, adept.SkillsDirName), p.SkillsDir())
	require.Equal(t, privDir, p.PrivateSkillsDir())
	require.True(t, p.HasPrivateSkill(defaultskills.ManagingLibraryID))

	// Sanity: a published skill written at root is visible through the project.
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
