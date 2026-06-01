package common

import (
	"io/fs"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Writer is the write-side filesystem dependency.
// Implementations live in internal/fsutil and tests.
type Writer interface {
	AtomicWrite(path string, data []byte, mode fs.FileMode) error
	EnsureDir(path string) error
}

// Linker manages symlinks or copy fallbacks for harness output. The
// signature matches a subset of fsutil.Linker so the canonical
// implementation in internal/fsutil satisfies it directly.
type Linker interface {
	SymlinkOrCopy(target, linkPath string, isDir bool) (adept.HarnessMode, error)
	ReadSymlink(linkPath string) (string, error)
	PathType(path string) fsutil.PathType
}
