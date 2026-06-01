package fsutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/itaywol/adeptability/pkg/adept"
)

// Linker manages symlinks, with a copy fallback for filesystems that don't
// support them.
type Linker interface {
	// Symlink creates a relative symlink at linkPath pointing to target.
	// Returns adept.ErrSymlinkUnsupported when the platform/filesystem
	// rejects the operation (EPERM, ENOSYS, EROFS, Windows equivalents).
	Symlink(target, linkPath string) error
	// SymlinkOrCopy attempts to symlink. On adept.ErrSymlinkUnsupported it
	// falls back to copying the target into linkPath. Returns the mode
	// actually used so the caller can record it in the lockfile.
	SymlinkOrCopy(target, linkPath string, isDir bool) (used adept.HarnessMode, err error)
	// ReadSymlink returns the symlink's target as recorded on disk.
	ReadSymlink(linkPath string) (string, error)
	// PathType reports what kind of entry lives at path.
	PathType(path string) PathType
}

type linker struct {
	w Writer
}

// NewLinker wires a Linker that uses w for copy fallback.
func NewLinker(w Writer) Linker {
	if w == nil {
		w = NewWriter()
	}
	return &linker{w: w}
}

func (l *linker) Symlink(target, linkPath string) error {
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return fmt.Errorf("fsutil: ensure parent of %s: %w", linkPath, err)
	}
	// Compute a relative target. If filepath.Rel fails (e.g. different
	// volumes on Windows), fall back to absolute.
	relTarget := target
	if rel, err := filepath.Rel(filepath.Dir(linkPath), target); err == nil {
		relTarget = rel
	}
	// Best-effort cleanup: if linkPath already exists as a symlink, remove
	// it. Don't clobber regular files.
	if pt := l.PathType(linkPath); pt == PathSymlink {
		_ = os.Remove(linkPath)
	}
	if err := os.Symlink(relTarget, linkPath); err != nil {
		if isSymlinkUnsupported(err) {
			return fmt.Errorf("fsutil: symlink %s -> %s: %w", linkPath, target, adept.ErrSymlinkUnsupported)
		}
		return fmt.Errorf("fsutil: symlink %s -> %s: %w", linkPath, target, err)
	}
	return nil
}

func (l *linker) SymlinkOrCopy(target, linkPath string, isDir bool) (adept.HarnessMode, error) {
	err := l.Symlink(target, linkPath)
	if err == nil {
		return adept.ModeSymlink, nil
	}
	if !errors.Is(err, adept.ErrSymlinkUnsupported) {
		return "", err
	}
	// Fall back to copy.
	if isDir {
		if err := l.w.CopyDir(target, linkPath); err != nil {
			return "", err
		}
		return adept.ModeCopy, nil
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", fmt.Errorf("fsutil: read target %s: %w", target, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("fsutil: stat target %s: %w", target, err)
	}
	if err := l.w.AtomicWrite(linkPath, data, info.Mode().Perm()); err != nil {
		return "", err
	}
	return adept.ModeCopy, nil
}

func (l *linker) ReadSymlink(linkPath string) (string, error) {
	target, err := os.Readlink(linkPath)
	if err != nil {
		return "", fmt.Errorf("fsutil: readlink %s: %w", linkPath, err)
	}
	return target, nil
}

func (l *linker) PathType(path string) PathType {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return PathMissing
		}
		return PathMissing
	}
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return PathSymlink
	case mode.IsDir():
		return PathDirectory
	case mode.IsRegular():
		return PathFile
	default:
		return PathFile
	}
}

// isSymlinkUnsupported maps errno-style errors into a single "unsupported"
// classification.
func isSymlinkUnsupported(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.ENOSYS) ||
		errors.Is(err, syscall.EROFS) {
		return true
	}
	// On Windows, os.Symlink may return a *os.LinkError wrapping a syscall
	// errno; the above errors.Is calls cover those cases via Unwrap chains.
	// Fall through: not classified as unsupported.
	return false
}
