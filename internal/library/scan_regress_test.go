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

// Regression for library/scan.go:139 — when the library copy exists
// (HasSkill true) but cannot be parsed by GetSkill, classify must emit a
// definite Status (Diverged) rather than an empty/blank Status.
func TestClassify_CorruptLibraryCopyMarkedDiverged(t *testing.T) {
	libRoot := t.TempDir()
	// Write a library copy of "foo" with no valid frontmatter: HasSkill is
	// true (SKILL.md exists) but GetSkill will fail to parse it.
	libSkillDir := filepath.Join(libRoot, adept.SkillsDirName, "foo")
	require.NoError(t, os.MkdirAll(libSkillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(libSkillDir, adept.SkillFileName), []byte("garbage no frontmatter\n"), 0o644))

	lib := New(libRoot, canonical.NewParser(), hash.NewHasher(), fsutil.NewWriter())
	scanner := NewScanner(lib, canonical.NewParser(), hash.NewHasher())

	// Source tree carries a VALID foo/SKILL.md.
	srcRoot := t.TempDir()
	srcDir := filepath.Join(srcRoot, "foo")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	doc := "---\nid: foo\ndescription: \"d\"\nactivation: agent\n---\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, adept.SkillFileName), []byte(doc), 0o644))

	results, err := scanner.Scan([]string{srcRoot})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "foo", results[0].SkillID)
	require.Equal(t, adept.StatusDiverged, results[0].Status)
	require.NotEqual(t, adept.Status(""), results[0].Status, "Status must never be left empty for a corrupt library copy")
}
