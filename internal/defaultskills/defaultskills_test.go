package defaultskills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

// Every bundled skill must load+validate through the real canonical loader,
// or `adept sync` would reject what `adept init` seeds. This is the check
// that fails if a default SKILL.md drifts out of schema.
func TestAll_BundledSkillsAreValidCanonical(t *testing.T) {
	skills := All()
	require.NotEmpty(t, skills)

	validator, err := canonical.NewValidator()
	require.NoError(t, err)
	loader := canonical.NewLoader(canonical.NewParser(), validator)

	for _, s := range skills {
		t.Run(s.ID, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), s.ID)
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, adept.SkillFileName), s.Body, 0o644))

			got, err := loader.LoadSkillDir(dir)
			require.NoError(t, err)
			require.Equal(t, s.ID, got.ID, "frontmatter id must match directory name")
			require.NotEmpty(t, got.Description)
		})
	}
}

// All() must be deterministic and id-sorted so init seeding order is stable.
func TestAll_SortedByID(t *testing.T) {
	skills := All()
	for i := 1; i < len(skills); i++ {
		require.Less(t, skills[i-1].ID, skills[i].ID)
	}
}
