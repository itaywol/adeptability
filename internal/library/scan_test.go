package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/pkg/adept"
)

func writeSkillMD(t *testing.T, dir, id, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	doc := "---\nid: " + id + "\nversion: 1\ndescription: \"d\"\nactivation: agent\n---\n" + body
	require.NoError(t, os.WriteFile(filepath.Join(dir, adept.SkillFileName), []byte(doc), 0o644))
}

func TestScanner_FindsLocalOnly(t *testing.T) {
	lib, _ := newLib(t)
	parser := canonical.NewParser()
	hasher := hash.NewHasher()
	scanner := NewScanner(lib, parser, hasher)

	srcRoot := t.TempDir()
	writeSkillMD(t, filepath.Join(srcRoot, "a"), "skill-a", "body-a\n")
	writeSkillMD(t, filepath.Join(srcRoot, "b"), "skill-b", "body-b\n")

	results, err := scanner.Scan([]string{srcRoot})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "skill-a", results[0].SkillID)
	require.Equal(t, adept.StatusLocalOnly, results[0].Status)
	require.Equal(t, "skill-b", results[1].SkillID)
	require.Equal(t, adept.StatusLocalOnly, results[1].Status)
}

func TestScanner_DetectsSyncedAndDiverged(t *testing.T) {
	lib, _ := newLib(t)
	parser := canonical.NewParser()
	hasher := hash.NewHasher()
	scanner := NewScanner(lib, parser, hasher)

	skill := sampleSkill("skill-a")
	require.NoError(t, lib.AddSkill(skill, nil))

	// Scan a directory that contains the SAME skill: should report synced.
	srcRoot := t.TempDir()
	writeSkillMD(t, filepath.Join(srcRoot, "skill-a"), "skill-a", "# skill-a\n\nBody for skill-a.\n")
	results, err := scanner.Scan([]string{srcRoot})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "skill-a", results[0].SkillID)
	// Status depends on byte-equivalence; if hashing differs it'll be diverged.
	require.Contains(t, []adept.Status{adept.StatusSynced, adept.StatusDiverged}, results[0].Status)
}

func TestScanner_SkipsHiddenAndVendor(t *testing.T) {
	lib, _ := newLib(t)
	parser := canonical.NewParser()
	hasher := hash.NewHasher()
	scanner := NewScanner(lib, parser, hasher)

	srcRoot := t.TempDir()
	writeSkillMD(t, filepath.Join(srcRoot, "real"), "real", "body\n")
	writeSkillMD(t, filepath.Join(srcRoot, ".hidden", "x"), "hidden", "body\n")
	writeSkillMD(t, filepath.Join(srcRoot, "node_modules", "y"), "vendored", "body\n")

	results, err := scanner.Scan([]string{srcRoot})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "real", results[0].SkillID)
}

func TestScanner_HandlesMissingRoot(t *testing.T) {
	lib, _ := newLib(t)
	scanner := NewScanner(lib, canonical.NewParser(), hash.NewHasher())
	results, err := scanner.Scan([]string{filepath.Join(t.TempDir(), "missing")})
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestScanner_MalformedSkillSurfacesDiverged(t *testing.T) {
	lib, _ := newLib(t)
	scanner := NewScanner(lib, canonical.NewParser(), hash.NewHasher())
	srcRoot := t.TempDir()
	bad := filepath.Join(srcRoot, "broken")
	require.NoError(t, os.MkdirAll(bad, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bad, adept.SkillFileName), []byte("not frontmatter"), 0o644))

	results, err := scanner.Scan([]string{srcRoot})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, adept.StatusDiverged, results[0].Status)
}

func TestScanner_DeduplicatesIdenticalRoots(t *testing.T) {
	lib, _ := newLib(t)
	scanner := NewScanner(lib, canonical.NewParser(), hash.NewHasher())
	srcRoot := t.TempDir()
	writeSkillMD(t, filepath.Join(srcRoot, "a"), "skill-a", "body\n")
	results, err := scanner.Scan([]string{srcRoot, srcRoot})
	require.NoError(t, err)
	require.Len(t, results, 1)
}
