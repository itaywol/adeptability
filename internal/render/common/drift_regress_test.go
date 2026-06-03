package common_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

// driftRegressFS is an inline FSInspector double (prefixed with the source
// basename) so it cannot collide with doubles added by other agents in this
// package's test files.
type driftRegressFS struct{}

func (driftRegressFS) PathType(path string) fsutil.PathType {
	info, err := os.Lstat(path)
	if err != nil {
		return common.PathMissing
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return common.PathSymlink
	case info.IsDir():
		return common.PathDirectory
	default:
		return common.PathFile
	}
}

func (driftRegressFS) ReadSymlink(linkPath string) (string, error) {
	return os.Readlink(linkPath)
}

// Regression for the high-severity bug where Compute ignored sidecars: a
// corrupt or missing sidecar must be classified as Drifted/Missing instead of
// the whole output being reported Synced.
func TestDiffer_Compute_ClassifiesSidecars(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Main file synced.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".alpha"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".alpha", "skill-a.md"), []byte("MAIN\n"), 0o644))
	// Drifted sidecar.
	require.NoError(t, os.WriteFile(filepath.Join(root, ".alpha", "helper.sh"), []byte("echo CORRUPTED\n"), 0o644))
	// missing.sh deliberately not written.

	differ := common.NewDiffer(driftRegressFS{})
	report, err := differ.Compute(root, []adept.RenderOutput{
		{
			Path:  filepath.Join(".alpha", "skill-a.md"),
			Bytes: []byte("MAIN\n"),
			Sidecars: []adept.SideFile{
				{RelPath: "helper.sh", Bytes: []byte("echo correct\n")},
				{RelPath: "missing.sh", Bytes: []byte("echo gone\n")},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(".alpha", "skill-a.md")}, report.Synced)
	require.Equal(t, []string{filepath.Join(".alpha", "helper.sh")}, report.Drifted,
		"a corrupt sidecar must be reported Drifted, not hidden under a Synced main file")
	require.Equal(t, []string{filepath.Join(".alpha", "missing.sh")}, report.Missing)
}

// Regression for the low-severity bug where a symlink pointing at a directory
// aborted the entire drift pass (EISDIR != ErrNotExist). It must be classified
// as Conflict, and remaining outputs must still be processed.
func TestDiffer_Compute_SymlinkToDirIsConflictNotFatal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Symlink pointing at an existing directory.
	targetDir := filepath.Join(root, "somedir")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))
	link := filepath.Join(root, "link.md")
	require.NoError(t, os.Symlink(targetDir, link))

	// A second, normal synced file that must still be classified after the
	// conflicting symlink — proving the pass did not abort.
	require.NoError(t, os.WriteFile(filepath.Join(root, "ok.md"), []byte("ok\n"), 0o644))

	differ := common.NewDiffer(driftRegressFS{})
	report, err := differ.Compute(root, []adept.RenderOutput{
		{Path: "link.md", Bytes: []byte("x")},
		{Path: "ok.md", Bytes: []byte("ok\n")},
	})
	require.NoError(t, err, "a symlink to a directory must not abort the whole drift pass")
	require.Equal(t, []string{"link.md"}, report.Conflict)
	require.Equal(t, []string{"ok.md"}, report.Synced)
}
