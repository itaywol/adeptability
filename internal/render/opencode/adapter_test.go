package opencode_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/internal/render/opencode"
	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

type fakeWriter struct{}

func (fakeWriter) AtomicWrite(_ string, _ []byte, _ fs.FileMode) error { return nil }
func (fakeWriter) EnsureDir(_ string) error                            { return nil }

type osLinker struct{}

func (osLinker) SymlinkOrCopy(target, linkPath string, _ bool) (adept.HarnessMode, error) {
	return adept.ModeSymlink, os.Symlink(target, linkPath)
}
func (osLinker) ReadSymlink(p string) (string, error) { return os.Readlink(p) }
func (osLinker) PathType(p string) fsutil.PathType {
	info, err := os.Lstat(p)
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

func newAdapter() *opencode.Adapter {
	return opencode.NewAdapter(opencode.New(), fakeWriter{}, osLinker{})
}

func TestAdapter_Spec(t *testing.T) {
	t.Parallel()
	spec := newAdapter().Spec()
	require.Equal(t, "opencode", spec.ID)
	require.Equal(t, adept.KindPerSkill, spec.Kind)
	require.True(t, spec.NeedsDir)
}

func TestAdapter_Aggregate_PassesThrough(t *testing.T) {
	t.Parallel()
	in := []adept.RenderOutput{{Path: "x"}}
	out, err := newAdapter().Aggregate(context.Background(), in, 0)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestAdapter_Detect(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := newAdapter()
	ok, err := a.Detect(root)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".opencode", "skill"), 0o755))
	ok, err = a.Detect(root)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAdapter_Validate_Missing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	report, err := newAdapter().Validate(root, []adept.RenderOutput{
		{Path: ".opencode/skill/x/SKILL.md", Bytes: []byte("payload")},
	})
	require.NoError(t, err)
	require.Equal(t, []string{".opencode/skill/x/SKILL.md"}, report.Missing)
}
