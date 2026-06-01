package claude_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/render/claude"
	"github.com/itaywol/adeptability/internal/render/common"
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

func newAdapter() *claude.Adapter {
	return claude.NewAdapter(claude.New(), fakeWriter{}, osLinker{})
}

func TestAdapter_Spec(t *testing.T) {
	t.Parallel()
	a := newAdapter()
	spec := a.Spec()
	require.Equal(t, "claude-code", spec.ID)
	require.Equal(t, adept.KindPerSkill, spec.Kind)
	require.True(t, spec.NeedsDir)
}

func TestAdapter_Aggregate_PassesThrough(t *testing.T) {
	t.Parallel()
	a := newAdapter()
	in := []adept.RenderOutput{{Path: "x"}, {Path: "y"}}
	out, err := a.Aggregate(context.Background(), in, 0)
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

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude"), 0o755))
	ok, err = a.Detect(root)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAdapter_Detect_SkillsOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := newAdapter()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".claude", "skills"), 0o755))
	ok, err := a.Detect(root)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAdapter_Validate_DetectsDrift(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	a := newAdapter()

	target := ".claude/skills/foo/SKILL.md"
	abs := filepath.Join(root, target)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("DIFFERENT"), 0o644))

	report, err := a.Validate(root, []adept.RenderOutput{
		{Path: target, Bytes: []byte("EXPECTED")},
	})
	require.NoError(t, err)
	require.Equal(t, []string{target}, report.Drifted)
}
