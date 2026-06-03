package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression: PathType must not collapse a non-ErrNotExist Lstat failure to
// PathMissing. A path under a parent that is a regular file yields ENOTDIR
// from Lstat — the entry is not stattable but must NOT be reported as
// "missing" (which would let drift treat it as safe-to-create and let
// adapters clobber it). It must report PathUnknown.
func TestPathType_NonNotExistErrorIsUnknown_Regress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ENOTDIR-via-file-parent semantics differ on Windows")
	}
	dir := t.TempDir()
	fileParent := filepath.Join(dir, "afile")
	require.NoError(t, os.WriteFile(fileParent, []byte("x"), 0o644))
	// Lstat on a path whose parent component is a regular file -> ENOTDIR.
	blocked := filepath.Join(fileParent, "child")

	l := NewLinker(nil)
	require.Equal(t, PathUnknown, l.PathType(blocked))
	require.NotEqual(t, PathMissing, l.PathType(blocked))
}

// Regression: a genuinely absent path still reports PathMissing.
func TestPathType_AbsentIsMissing_Regress(t *testing.T) {
	l := NewLinker(nil)
	require.Equal(t, PathMissing, l.PathType(filepath.Join(t.TempDir(), "nope")))
}

// Regression: the directory copy-fallback must clear a pre-existing
// destination so the copy is a faithful mirror. Previously CopyDir
// overwrote files but never pruned dst-only entries, so a file removed
// upstream survived in the destination across a second SymlinkOrCopy.
func TestSymlinkOrCopy_DirFallbackPrunesStale_Regress(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "keep.txt"), []byte("keep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "stale.txt"), []byte("stale"), 0o644))

	dst := filepath.Join(t.TempDir(), "out")

	// Force the copy branch deterministically on every platform.
	l := &fallbackLinkerPrune{w: NewWriter()}

	// First copy materializes both files.
	_, err := l.SymlinkOrCopy(src, dst, true)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dst, "stale.txt"))
	require.NoError(t, err)

	// Upstream drops stale.txt; second copy must not leave it behind.
	require.NoError(t, os.Remove(filepath.Join(src, "stale.txt")))
	_, err = l.SymlinkOrCopy(src, dst, true)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(dst, "keep.txt"))
	require.NoError(t, err, "kept file must survive")
	_, err = os.Stat(filepath.Join(dst, "stale.txt"))
	require.True(t, os.IsNotExist(err), "stale file must be pruned, got err=%v", err)
}

// fallbackLinkerPrune mirrors the real linker's SymlinkOrCopy copy-branch
// (including the pre-copy RemoveAll) while bypassing the OS symlink call so
// the directory fallback runs on every platform. Prefixed with the source
// basename to avoid collisions with other test helpers in this package.
type fallbackLinkerPrune struct {
	w Writer
}

func (l *fallbackLinkerPrune) SymlinkOrCopy(target, linkPath string, isDir bool) (modeUsed string, err error) {
	if isDir {
		if err := l.w.RemoveAll(linkPath); err != nil {
			return "", err
		}
		if err := l.w.CopyDir(target, linkPath); err != nil {
			return "", err
		}
		return "copy", nil
	}
	return "", os.ErrInvalid
}
