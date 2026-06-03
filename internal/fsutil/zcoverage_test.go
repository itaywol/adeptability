package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

// TestIsSymlinkUnsupported exercises the errno classifier that decides
// symlink-vs-copy fallback. White-box (same package) so it can call the
// unexported helper directly. Catches regressions if an errno is dropped or
// the %w-wrap / *os.LinkError Unwrap chain is broken.
func TestIsSymlinkUnsupported(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"eperm", syscall.EPERM, true},
		{"enosys", syscall.ENOSYS, true},
		{"erofs", syscall.EROFS, true},
		{"wrapped_eperm", fmt.Errorf("ctx: %w", syscall.EPERM), true},
		{"wrapped_enosys", fmt.Errorf("ctx: %w", syscall.ENOSYS), true},
		{"wrapped_erofs", fmt.Errorf("ctx: %w", syscall.EROFS), true},
		{"double_wrapped_eperm", fmt.Errorf("a: %w", fmt.Errorf("b: %w", syscall.EPERM)), true},
		{"linkerror_eperm", &os.LinkError{Op: "symlink", Err: syscall.EPERM}, true},
		{"linkerror_erofs", &os.LinkError{Op: "symlink", Err: syscall.EROFS}, true},
		{"eacces_not_unsupported", syscall.EACCES, false},
		{"enoent_not_unsupported", syscall.ENOENT, false},
		{"errnotexist_not_unsupported", os.ErrNotExist, false},
		{"random_not_unsupported", errors.New("random"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isSymlinkUnsupported(tc.err))
		})
	}
}

// TestWriter_AtomicWrite_EmptyPath covers the empty-path guard branch.
func TestWriter_AtomicWrite_EmptyPath(t *testing.T) {
	w := NewWriter()
	err := w.AtomicWrite("", []byte("x"), 0o644)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty path")
}

// TestWriter_AtomicWrite_PreservesMode asserts the explicit os.Chmod step
// preserves the requested perm bits exactly (umask would otherwise mask
// them), across multiple modes. Table-driven.
func TestWriter_AtomicWrite_PreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix perm bits not meaningful on Windows")
	}
	w := NewWriter()
	cases := []struct {
		name string
		mode os.FileMode
	}{
		{"private", 0o600},
		{"executable", 0o755},
		{"group_readable", 0o640},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f")
			require.NoError(t, w.AtomicWrite(path, []byte("data"), tc.mode))
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, tc.mode, info.Mode().Perm())
		})
	}
}

// TestWriter_AtomicWrite_RoundTrip verifies the data written matches the data
// read back, and no temp files are left behind.
func TestWriter_AtomicWrite_RoundTrip(t *testing.T) {
	w := NewWriter()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	payload := []byte("the quick brown fox\x00\x01binary")
	require.NoError(t, w.AtomicWrite(path, payload, 0o644))
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, payload, got)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "no temp residue")
}

// TestWriter_CopyDir_PreservesSymlinks exercises the ModeSymlink branch inside
// WalkDir: a relative symlink in src must be replicated verbatim (not
// dereferenced) in dst.
func TestWriter_CopyDir_PreservesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	w := NewWriter()
	l := NewLinker(w)
	src := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(src, "real.txt"), []byte("payload"), 0o644))
	require.NoError(t, os.Symlink("real.txt", filepath.Join(src, "link.txt")))

	dst := filepath.Join(t.TempDir(), "out")
	require.NoError(t, w.CopyDir(src, dst))

	// The copied link must remain a symlink, not a dereferenced regular file.
	require.Equal(t, PathSymlink, l.PathType(filepath.Join(dst, "link.txt")))
	target, err := os.Readlink(filepath.Join(dst, "link.txt"))
	require.NoError(t, err)
	require.Equal(t, "real.txt", target, "relative target preserved verbatim")
	// And the regular file came across intact.
	data, err := os.ReadFile(filepath.Join(dst, "real.txt"))
	require.NoError(t, err)
	require.Equal(t, "payload", string(data))
}

// TestWriter_CopyDir_PreservesExecAndNesting confirms nested dirs and the
// executable bit survive a recursive copy.
func TestWriter_CopyDir_PreservesExecAndNesting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix perm bits not meaningful on Windows")
	}
	w := NewWriter()
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "bin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "bin", "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(src, "data.txt"), []byte("plain"), 0o644))

	dst := filepath.Join(t.TempDir(), "copied")
	require.NoError(t, w.CopyDir(src, dst))

	info, err := os.Stat(filepath.Join(dst, "bin", "run.sh"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "exec bit preserved")
	plain, err := os.Stat(filepath.Join(dst, "data.txt"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), plain.Mode().Perm())
}

// TestWriter_CopyDir_MissingSrc covers the early lstat-failure branch.
func TestWriter_CopyDir_MissingSrc(t *testing.T) {
	w := NewWriter()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	err := w.CopyDir(missing, filepath.Join(t.TempDir(), "dst"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "lstat")
}

// TestLinker_ReadSymlink_Errors covers the readlink error path for both a
// regular file and a missing path.
func TestLinker_ReadSymlink_Errors(t *testing.T) {
	l := NewLinker(nil)
	dir := t.TempDir()

	regular := filepath.Join(dir, "regular.txt")
	require.NoError(t, os.WriteFile(regular, []byte("x"), 0o644))

	cases := []struct {
		name string
		path string
	}{
		{"regular_file", regular},
		{"missing_path", filepath.Join(dir, "nope")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := l.ReadSymlink(tc.path)
			require.Error(t, err)
			require.Empty(t, got)
			require.Contains(t, err.Error(), "readlink")
		})
	}
}

// TestLinker_Symlink_DoesNotClobberRegularFile proves the PathType==PathSymlink
// guard only removes pre-existing symlinks: a regular file already at linkPath
// must NOT be removed, and os.Symlink must refuse to overwrite it, leaving the
// original bytes intact.
func TestLinker_Symlink_DoesNotClobberRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink overwrite semantics differ on Windows")
	}
	l := NewLinker(nil)
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("target"), 0o644))

	linkPath := filepath.Join(dir, "occupied")
	require.NoError(t, os.WriteFile(linkPath, []byte("ORIGINAL"), 0o644))

	err := l.Symlink(target, linkPath)
	require.Error(t, err, "must not clobber an existing regular file")

	// The original file is untouched (still a regular file with its bytes).
	require.Equal(t, PathFile, l.PathType(linkPath))
	data, err := os.ReadFile(linkPath)
	require.NoError(t, err)
	require.Equal(t, "ORIGINAL", string(data))
}

// TestLinker_PathType_Table is a table-driven sweep over a fixture temp dir,
// including a permission-denied parent (chmod 000) to assert a non-ErrNotExist
// Lstat error surfaces as PathUnknown (NOT PathMissing) so callers never
// clobber a live-but-unreadable entry.
func TestLinker_PathType_Table(t *testing.T) {
	l := NewLinker(nil)
	dir := t.TempDir()

	regular := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(regular, []byte("x"), 0o644))
	subdir := filepath.Join(dir, "subdir")
	require.NoError(t, os.MkdirAll(subdir, 0o755))

	type tc struct {
		name string
		path string
		want PathType
	}
	cases := []tc{
		{"missing", filepath.Join(dir, "nope"), PathMissing},
		{"file", regular, PathFile},
		{"directory", subdir, PathDirectory},
	}

	if runtime.GOOS != "windows" {
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(regular, link))
		cases = append(cases, tc{"symlink", link, PathSymlink})
	}

	// Permission-denied parent: Lstat returns EACCES (not ErrNotExist), so a
	// child path must classify as PathUnknown. Skip when running as root,
	// where the 000 perms are bypassed.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		locked := filepath.Join(dir, "locked")
		require.NoError(t, os.MkdirAll(locked, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(locked, "secret"), []byte("s"), 0o644))
		require.NoError(t, os.Chmod(locked, 0o000))
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
		cases = append(cases, tc{"perm_denied_parent", filepath.Join(locked, "secret"), PathUnknown})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, l.PathType(c.path))
		})
	}
}

// TestLinker_SymlinkOrCopy_RealSymlinkRoundTrip exercises the real (non-fake)
// SymlinkOrCopy happy path end to end: it creates a real relative symlink and
// reports ModeSymlink, and the link resolves back to the target's content.
func TestLinker_SymlinkOrCopy_RealSymlinkRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may be unprivileged-blocked on Windows")
	}
	l := NewLinker(NewWriter())
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("linked-content"), 0o644))
	link := filepath.Join(dir, "nested", "link.txt")

	mode, err := l.SymlinkOrCopy(target, link, false)
	require.NoError(t, err)
	require.Equal(t, adept.ModeSymlink, mode)
	require.Equal(t, PathSymlink, l.PathType(link))
	// Reading through the symlink yields the target bytes.
	data, err := os.ReadFile(link)
	require.NoError(t, err)
	require.Equal(t, "linked-content", string(data))
}

// TestLinker_SymlinkOrCopy_NonUnsupportedErrorPropagates exercises the real
// SymlinkOrCopy early-return for errors that are NOT
// adept.ErrSymlinkUnsupported: such an error must surface verbatim (empty
// mode) and must NOT trigger the copy fallback. Here the link's parent cannot
// be created because a path component is a regular file (ENOTDIR), which the
// classifier does not treat as unsupported.
func TestLinker_SymlinkOrCopy_NonUnsupportedErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ENOTDIR-via-file-parent semantics differ on Windows")
	}
	l := NewLinker(NewWriter())
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o644))

	// A regular file used as a directory component -> MkdirAll fails ENOTDIR.
	fileAsDir := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(fileAsDir, []byte("x"), 0o644))
	link := filepath.Join(fileAsDir, "child", "link.txt")

	mode, err := l.SymlinkOrCopy(target, link, false)
	require.Error(t, err)
	require.Empty(t, string(mode), "no mode recorded on a hard failure")
	require.False(t, errors.Is(err, adept.ErrSymlinkUnsupported),
		"this is a real filesystem error, not an unsupported-symlink fallback")
}
