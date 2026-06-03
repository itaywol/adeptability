package merge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// mergeRegressWrite materializes a side dir with one file. Inlined and
// prefixed with the source basename to avoid colliding with helpers other
// agents may add to this package's test files.
func mergeRegressWrite(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

// findResultFile returns the merged bytes for rel.
func mergeRegressFind(t *testing.T, res Result, rel string) string {
	t.Helper()
	for _, f := range res.Files {
		if f.RelPath == rel {
			return string(f.Bytes)
		}
	}
	t.Fatalf("no result file for %q", rel)
	return ""
}

// Regression for the delete/modify conflict envelope discarding base content.
// When base has the file, ours deletes it and theirs modifies it, the conflict
// envelope must contain the real ancestor content under the base marker, not
// the bogus "(absent in base)" line.
func TestMerge_DeleteModifyConflict_PreservesBase_OursDeleted(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	ours := filepath.Join(root, "ours")
	theirs := filepath.Join(root, "theirs")
	require.NoError(t, os.MkdirAll(ours, 0o755))

	mergeRegressWrite(t, base, "tmp.md", "ANCESTOR CONTENT\n")
	mergeRegressWrite(t, theirs, "tmp.md", "THEIR CHANGE\n")
	// ours deletes tmp.md (simply absent).

	res, err := NewMerger().Merge(base, ours, theirs)
	require.NoError(t, err)

	body := mergeRegressFind(t, res, "tmp.md")
	require.Contains(t, body, "ANCESTOR CONTENT", "base content must be preserved in the conflict envelope")
	require.NotContains(t, body, "(absent in base)", "file existed in base; must not claim it was absent")
	require.Contains(t, body, "THEIR CHANGE")
	require.Contains(t, body, defaultMarkerBase)
}

// Symmetric case: theirs deletes, ours modifies.
func TestMerge_DeleteModifyConflict_PreservesBase_TheirsDeleted(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "base")
	ours := filepath.Join(root, "ours")
	theirs := filepath.Join(root, "theirs")
	require.NoError(t, os.MkdirAll(theirs, 0o755))

	mergeRegressWrite(t, base, "tmp.md", "ANCESTOR CONTENT\n")
	mergeRegressWrite(t, ours, "tmp.md", "OUR CHANGE\n")
	// theirs deletes tmp.md (simply absent).

	res, err := NewMerger().Merge(base, ours, theirs)
	require.NoError(t, err)

	body := mergeRegressFind(t, res, "tmp.md")
	require.Contains(t, body, "ANCESTOR CONTENT", "base content must be preserved in the conflict envelope")
	require.NotContains(t, body, "(absent in base)", "file existed in base; must not claim it was absent")
	require.Contains(t, body, "OUR CHANGE")
	require.Contains(t, body, defaultMarkerBase)
}
