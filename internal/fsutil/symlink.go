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

// PathUnknown means the path could not be classified because Lstat
// failed with an error other than fs.ErrNotExist (e.g. permission denied
// on a parent component, a non-directory parent, or a symlink loop). The
// entry may well exist on disk, so consumers must NOT treat it as absent:
// drift classifies it as a Conflict (it is != PathMissing) and adapters
// skip it rather than overwriting a live-but-unreadable path.
const PathUnknown PathType = "unknown"

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
		// Clear any pre-existing destination first so the copy is a
		// faithful mirror of target. CopyDir overwrites files it finds
		// in src but never prunes files present only in dst, so without
		// this a stale entry from a prior copy would survive.
		if err := l.w.RemoveAll(linkPath); err != nil {
			return "", fmt.Errorf("fsutil: clear copy dest %s: %w", linkPath, err)
		}
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
		// A real Lstat failure (EACCES on a parent, ENOTDIR, ELOOP,
		// name-too-long, …) means the path may well exist but is
		// unstattable. Collapsing it to PathMissing makes drift report
		// "safe to create" and lets adapters overwrite a live entry.
		// Surface it as PathUnknown so callers treat it as "present /
		// not safe to clobber" (drift.go default -> Conflict; adapter
		// `!= PathMissing` guards skip it).
		return PathUnknown
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
