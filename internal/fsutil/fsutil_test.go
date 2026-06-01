package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func TestWriter_AtomicWrite(t *testing.T) {
	w := NewWriter()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	require.NoError(t, w.AtomicWrite(path, []byte("hello"), 0o644))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "hello", string(data))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temp files should be left behind")
}

func TestWriter_AtomicWrite_Overwrite(t *testing.T) {
	w := NewWriter()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	require.NoError(t, w.AtomicWrite(path, []byte("old"), 0o644))
	require.NoError(t, w.AtomicWrite(path, []byte("new"), 0o644))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "new", string(data))
}

func TestWriter_AtomicWrite_CreatesParents(t *testing.T) {
	w := NewWriter()
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "data.txt")
	require.NoError(t, w.AtomicWrite(path, []byte("ok"), 0o644))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, int64(2), info.Size())
}

func TestWriter_EnsureDirAndRemoveAll(t *testing.T) {
	w := NewWriter()
	dir := filepath.Join(t.TempDir(), "x", "y")
	require.NoError(t, w.EnsureDir(dir))
	info, err := os.Stat(dir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	require.NoError(t, w.RemoveAll(dir))
	_, err = os.Stat(dir)
	require.True(t, os.IsNotExist(err))
}

func TestWriter_CopyDir(t *testing.T) {
	w := NewWriter()
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dest")

	require.NoError(t, os.WriteFile(filepath.Join(src, "a.txt"), []byte("alpha"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("beta"), 0o600))

	require.NoError(t, w.CopyDir(src, dst))

	a, err := os.ReadFile(filepath.Join(dst, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "alpha", string(a))
	b, err := os.ReadFile(filepath.Join(dst, "sub", "b.txt"))
	require.NoError(t, err)
	require.Equal(t, "beta", string(b))

	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dst, "sub", "b.txt"))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestLinker_Symlink_AndReadback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows CI; covered by Linux/macOS")
	}
	w := NewWriter()
	l := NewLinker(w)
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o644))
	link := filepath.Join(dir, "link.txt")
	require.NoError(t, l.Symlink(target, link))
	require.Equal(t, PathSymlink, l.PathType(link))
	got, err := l.ReadSymlink(link)
	require.NoError(t, err)
	require.Equal(t, "target.txt", got, "expected relative symlink target")
}

func TestLinker_Symlink_OverwritesExistingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows CI")
	}
	w := NewWriter()
	l := NewLinker(w)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	require.NoError(t, os.WriteFile(a, []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(b, []byte("b"), 0o644))
	link := filepath.Join(dir, "link.txt")
	require.NoError(t, l.Symlink(a, link))
	require.NoError(t, l.Symlink(b, link))
	got, err := l.ReadSymlink(link)
	require.NoError(t, err)
	require.Equal(t, "b.txt", got)
}

func TestLinker_PathType(t *testing.T) {
	w := NewWriter()
	l := NewLinker(w)
	dir := t.TempDir()
	require.Equal(t, PathMissing, l.PathType(filepath.Join(dir, "nope")))

	file := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(file, nil, 0o644))
	require.Equal(t, PathFile, l.PathType(file))

	sub := filepath.Join(dir, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	require.Equal(t, PathDirectory, l.PathType(sub))

	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(file, link))
		require.Equal(t, PathSymlink, l.PathType(link))
	}
}

func TestLinker_SymlinkOrCopy_File(t *testing.T) {
	w := NewWriter()
	l := NewLinker(w)
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o644))
	link := filepath.Join(dir, "link.txt")
	mode, err := l.SymlinkOrCopy(target, link, false)
	require.NoError(t, err)
	// On Linux/macOS we expect symlink; on Windows it may fall back to copy.
	if runtime.GOOS == "windows" {
		require.Contains(t, []adept.HarnessMode{adept.ModeSymlink, adept.ModeCopy}, mode)
	} else {
		require.Equal(t, adept.ModeSymlink, mode)
	}
}

func TestLinker_SymlinkOrCopy_Dir(t *testing.T) {
	w := NewWriter()
	l := NewLinker(w)
	dir := t.TempDir()
	target := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "f.txt"), []byte("x"), 0o644))
	link := filepath.Join(dir, "link")
	mode, err := l.SymlinkOrCopy(target, link, true)
	require.NoError(t, err)
	if runtime.GOOS == "windows" {
		require.Contains(t, []adept.HarnessMode{adept.ModeSymlink, adept.ModeCopy}, mode)
	} else {
		require.Equal(t, adept.ModeSymlink, mode)
	}
}
