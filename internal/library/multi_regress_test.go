package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/pkg/adept"
)

// multiRegressWriteSkill writes a SKILL.md under <root>/skills/<id>/ with the
// given raw document bytes (caller controls validity). Prefixed to avoid
// colliding with helpers other agents may add to this package's tests.
func multiRegressWriteSkill(t *testing.T, root, id, doc string) {
	t.Helper()
	dir := filepath.Join(root, adept.SkillsDirName, id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, adept.SkillFileName), []byte(doc), 0o644))
}

func multiRegressNamedLib(name, root string) NamedLibrary {
	return NamedLibrary{
		Name:    name,
		Library: New(root, canonical.NewParser(), hash.NewHasher(), fsutil.NewWriter()),
	}
}

const multiRegressValidDoc = "---\nid: foo\ndescription: \"d\"\nactivation: agent\n---\nbody\n"

// Regression for multi.go:105 — Resolve must return the valid winner from an
// earlier (higher-priority) library even when a later, shadowed library holds
// a corrupt copy of the same id.
func TestResolve_ShadowedCorruptCopyDoesNotPoisonWinner(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	multiRegressWriteSkill(t, rootA, "foo", multiRegressValidDoc)
	multiRegressWriteSkill(t, rootB, "foo", "garbage with no frontmatter\n")

	m := NewMulti([]NamedLibrary{
		multiRegressNamedLib("A", rootA),
		multiRegressNamedLib("B", rootB),
	})

	skill, source, shadowed, err := m.Resolve("foo")
	require.NoError(t, err)
	require.NotNil(t, skill)
	require.Equal(t, "foo", skill.ID)
	require.Equal(t, "A", source)
	require.Equal(t, []string{"B"}, shadowed)
}

// Regression for multi.go:105 — when the first (highest-priority) library's
// copy is corrupt, Resolve should fall through to a readable copy in a
// lower-priority library rather than aborting.
func TestResolve_CorruptWinnerFallsThroughToReadableCopy(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	multiRegressWriteSkill(t, rootA, "foo", "garbage with no frontmatter\n")
	multiRegressWriteSkill(t, rootB, "foo", multiRegressValidDoc)

	m := NewMulti([]NamedLibrary{
		multiRegressNamedLib("A", rootA),
		multiRegressNamedLib("B", rootB),
	})

	skill, source, _, err := m.Resolve("foo")
	require.NoError(t, err)
	require.NotNil(t, skill)
	require.Equal(t, "B", source)
}

// Regression for multi.go:126 — ListAll must skip a corrupt skill and still
// return every other valid skill rather than aborting the whole listing.
func TestListAll_SkipsCorruptSkillKeepsValid(t *testing.T) {
	root := t.TempDir()
	multiRegressWriteSkill(t, root, "good", "---\nid: good\ndescription: \"d\"\nactivation: agent\n---\nbody\n")
	multiRegressWriteSkill(t, root, "bad", "no frontmatter here\n")

	m := NewMulti([]NamedLibrary{multiRegressNamedLib("A", root)})

	resolutions, err := m.ListAll()
	require.NoError(t, err)
	require.Len(t, resolutions, 1)
	require.Equal(t, "good", resolutions[0].Skill.ID)
	require.Equal(t, "A", resolutions[0].Source)
}
