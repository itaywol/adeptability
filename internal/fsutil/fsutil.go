// Package fsutil provides filesystem primitives: atomic writes, directory
// copy, removal, and path-type detection. Symlink handling lives in
// symlink.go.
//
// All operations operate on absolute or already-resolved paths; callers are
// expected to clean inputs.
package fsutil

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// PathType describes a path on disk.
type PathType string

const (
	// PathMissing means the path does not exist.
	PathMissing PathType = "missing"
	// PathFile means a regular file.
	PathFile PathType = "file"
	// PathDirectory means a directory.
	PathDirectory PathType = "directory"
	// PathSymlink means a symbolic link (target not resolved).
	PathSymlink PathType = "symlink"
)

// Writer covers the on-disk write surface.
type Writer interface {
	// AtomicWrite writes data to path through a temp file and rename. The
	// resulting file has the requested mode.
	AtomicWrite(path string, data []byte, mode os.FileMode) error
	// EnsureDir creates path and any missing parents.
	EnsureDir(path string) error
	// CopyDir recursively copies src to dst, preserving file modes.
	CopyDir(src, dst string) error
	// RemoveAll removes path and its descendants.
	RemoveAll(path string) error
}

type writer struct{}

// NewWriter returns a Writer backed by os.* primitives.
func NewWriter() Writer {
	return &writer{}
}

func (w *writer) AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if path == "" {
		return errors.New("fsutil: AtomicWrite: empty path")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("fsutil: ensure dir %s: %w", dir, err)
	}
	tmpName, err := tmpInDir(dir, ".tmp-fsutil-")
	if err != nil {
		return err
	}
	cleanup := func() { _ = os.Remove(tmpName) }
	f, err := os.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("fsutil: open tmp %s: %w", tmpName, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("fsutil: write tmp %s: %w", tmpName, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		cleanup()
		return fmt.Errorf("fsutil: sync tmp %s: %w", tmpName, err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return fmt.Errorf("fsutil: close tmp %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		cleanup()
		return fmt.Errorf("fsutil: chmod tmp %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("fsutil: rename %s -> %s: %w", tmpName, path, err)
	}
	// fsync the destination dir so the rename is durable.
	if d, openErr := os.Open(dir); openErr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func (w *writer) EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("fsutil: mkdir %s: %w", path, err)
	}
	return nil
}

func (w *writer) RemoveAll(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("fsutil: remove %s: %w", path, err)
	}
	return nil
}

func (w *writer) CopyDir(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("fsutil: lstat %s: %w", src, err)
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return fmt.Errorf("fsutil: mkdir %s: %w", dst, err)
		}
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		fi, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if rel == "." {
				return nil
			}
			return os.MkdirAll(target, fi.Mode().Perm())
		case fi.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("fsutil: readlink %s: %w", path, err)
			}
			return os.Symlink(link, target)
		default:
			return copyFile(path, target, fi.Mode().Perm())
		}
	})
}

// copyFile copies src to dst with mode.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("fsutil: open %s: %w", src, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("fsutil: mkdir for %s: %w", dst, err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("fsutil: create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("fsutil: copy %s -> %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("fsutil: close %s: %w", dst, err)
	}
	return os.Chmod(dst, mode)
}

// tmpInDir returns a unique unused name in dir with the given prefix.
func tmpInDir(dir, prefix string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("fsutil: rand: %w", err)
	}
	return filepath.Join(dir, prefix+hex.EncodeToString(buf[:])+".tmp"), nil
}
