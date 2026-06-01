package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

// fakeLinker bypasses the OS symlink call so we can deterministically
// exercise the copy-fallback path on every platform.
type fallbackLinker struct {
	w Writer
}

func (l *fallbackLinker) Symlink(string, string) error { return adept.ErrSymlinkUnsupported }

func (l *fallbackLinker) SymlinkOrCopy(target, linkPath string, isDir bool) (adept.HarnessMode, error) {
	if err := l.Symlink(target, linkPath); err != nil {
		if !errors.Is(err, adept.ErrSymlinkUnsupported) {
			return "", err
		}
		if isDir {
			if err := l.w.CopyDir(target, linkPath); err != nil {
				return "", err
			}
			return adept.ModeCopy, nil
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(target)
		if err != nil {
			return "", err
		}
		if err := l.w.AtomicWrite(linkPath, data, info.Mode().Perm()); err != nil {
			return "", err
		}
		return adept.ModeCopy, nil
	}
	return adept.ModeSymlink, nil
}

func (l *fallbackLinker) ReadSymlink(linkPath string) (string, error) {
	return "", errors.New("not a symlink in fallback mode")
}

func (l *fallbackLinker) PathType(path string) PathType {
	if _, err := os.Lstat(path); err != nil {
		return PathMissing
	}
	return PathFile
}

func TestLinker_CopyFallback_File(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("content"), 0o644))
	link := filepath.Join(dir, "link.txt")
	l := &fallbackLinker{w: NewWriter()}
	mode, err := l.SymlinkOrCopy(target, link, false)
	require.NoError(t, err)
	require.Equal(t, adept.ModeCopy, mode)
	data, err := os.ReadFile(link)
	require.NoError(t, err)
	require.Equal(t, "content", string(data))
}

func TestLinker_CopyFallback_Dir(t *testing.T) {
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "scripts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755))
	dst := filepath.Join(t.TempDir(), "copied")
	l := &fallbackLinker{w: NewWriter()}
	mode, err := l.SymlinkOrCopy(src, dst, true)
	require.NoError(t, err)
	require.Equal(t, adept.ModeCopy, mode)
	_, err = os.Stat(filepath.Join(dst, "scripts", "run.sh"))
	require.NoError(t, err)
}

func TestLinker_Symlink_RejectsBadParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}
	l := NewLinker(nil)
	// Use a path under a non-directory so MkdirAll fails.
	dir := t.TempDir()
	conflict := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(conflict, []byte("x"), 0o644))
	link := filepath.Join(conflict, "child", "link")
	err := l.Symlink("/some/target", link)
	require.Error(t, err)
}
