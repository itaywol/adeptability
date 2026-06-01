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

// stubFS is a minimal FSInspector test double.
type stubFS struct{}

func (stubFS) PathType(path string) fsutil.PathType {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return common.PathMissing
		}
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

func (stubFS) ReadSymlink(linkPath string) (string, error) {
	return os.Readlink(linkPath)
}

func TestDiffer_Compute_AllStates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// synced file
	syncedPath := filepath.Join(root, "a.md")
	require.NoError(t, os.WriteFile(syncedPath, []byte("hello"), 0o644))
	// drifted file
	driftedPath := filepath.Join(root, "b.md")
	require.NoError(t, os.WriteFile(driftedPath, []byte("OLD"), 0o644))
	// conflict (directory where file expected)
	conflictPath := filepath.Join(root, "c.md")
	require.NoError(t, os.MkdirAll(conflictPath, 0o755))

	differ := common.NewDiffer(stubFS{})
	report, err := differ.Compute(root, []adept.RenderOutput{
		{Path: "a.md", Bytes: []byte("hello")},
		{Path: "b.md", Bytes: []byte("NEW")},
		{Path: "c.md", Bytes: []byte("anything")},
		{Path: "d.md", Bytes: []byte("missing")},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"a.md"}, report.Synced)
	require.Equal(t, []string{"b.md"}, report.Drifted)
	require.Equal(t, []string{"c.md"}, report.Conflict)
	require.Equal(t, []string{"d.md"}, report.Missing)
}

func TestDiffer_Compute_Symlink_Synced(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src.md")
	require.NoError(t, os.WriteFile(src, []byte("payload"), 0o644))

	link := filepath.Join(root, "link.md")
	require.NoError(t, os.Symlink(src, link))

	differ := common.NewDiffer(stubFS{})
	report, err := differ.Compute(root, []adept.RenderOutput{
		{Path: "link.md", Bytes: []byte("payload")},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"link.md"}, report.Synced)
}

func TestDiffer_Compute_Symlink_Drifted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src.md")
	require.NoError(t, os.WriteFile(src, []byte("OLD"), 0o644))

	link := filepath.Join(root, "link.md")
	require.NoError(t, os.Symlink(src, link))

	differ := common.NewDiffer(stubFS{})
	report, err := differ.Compute(root, []adept.RenderOutput{
		{Path: "link.md", Bytes: []byte("NEW")},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"link.md"}, report.Drifted)
}

func TestDiffer_Compute_Symlink_Broken(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	link := filepath.Join(root, "link.md")
	require.NoError(t, os.Symlink(filepath.Join(root, "nonexistent"), link))

	differ := common.NewDiffer(stubFS{})
	report, err := differ.Compute(root, []adept.RenderOutput{
		{Path: "link.md", Bytes: []byte("x")},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"link.md"}, report.Conflict)
}

func TestDiffer_Compute_Empty(t *testing.T) {
	t.Parallel()
	differ := common.NewDiffer(stubFS{})
	report, err := differ.Compute(t.TempDir(), nil)
	require.NoError(t, err)
	require.Empty(t, report.Synced)
	require.Empty(t, report.Drifted)
	require.Empty(t, report.Missing)
	require.Empty(t, report.Conflict)
}
