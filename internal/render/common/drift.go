package common

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Re-export fsutil's PathType constants for convenience.
const (
	PathMissing   = fsutil.PathMissing
	PathFile      = fsutil.PathFile
	PathDirectory = fsutil.PathDirectory
	PathSymlink   = fsutil.PathSymlink
)

// FSInspector is the read-side filesystem dependency the drift helper needs.
// Implemented by internal/fsutil.Linker and by tests.
type FSInspector interface {
	PathType(path string) fsutil.PathType
	ReadSymlink(linkPath string) (string, error)
}

// Differ computes which expected RenderOutputs are in sync with disk and
// which have drifted.
type Differ interface {
	Compute(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error)
}

type differ struct {
	fs FSInspector
}

// NewDiffer returns a Differ backed by the provided filesystem inspector.
func NewDiffer(insp FSInspector) Differ {
	return &differ{fs: insp}
}

// Compute classifies each expected output as Synced, Drifted, Missing, or
// Conflict relative to the actual files at projectRoot.
//
// Classification rules:
//   - missing on disk           → Missing
//   - file with matching bytes  → Synced
//   - file with differing bytes → Drifted
//   - symlink resolving to file with matching bytes → Synced
//   - symlink resolving to file with differing bytes → Drifted
//   - any other shape (dir, broken symlink) → Conflict
func (d *differ) Compute(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	var report adept.DriftReport
	for _, out := range expected {
		abs := filepath.Join(projectRoot, out.Path)
		switch d.fs.PathType(abs) {
		case PathMissing:
			report.Missing = append(report.Missing, out.Path)
		case PathFile:
			same, err := bytesMatch(abs, out.Bytes)
			if err != nil {
				return adept.DriftReport{}, fmt.Errorf("drift: %s: %w", out.Path, err)
			}
			if same {
				report.Synced = append(report.Synced, out.Path)
			} else {
				report.Drifted = append(report.Drifted, out.Path)
			}
		case PathSymlink:
			target, err := d.fs.ReadSymlink(abs)
			if err != nil {
				report.Conflict = append(report.Conflict, out.Path)
				continue
			}
			resolved := target
			if !filepath.IsAbs(resolved) {
				resolved = filepath.Join(filepath.Dir(abs), resolved)
			}
			same, err := bytesMatch(resolved, out.Bytes)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					report.Conflict = append(report.Conflict, out.Path)
					continue
				}
				return adept.DriftReport{}, fmt.Errorf("drift: %s: %w", out.Path, err)
			}
			if same {
				report.Synced = append(report.Synced, out.Path)
			} else {
				report.Drifted = append(report.Drifted, out.Path)
			}
		default:
			report.Conflict = append(report.Conflict, out.Path)
		}
	}
	return report, nil
}

func bytesMatch(path string, want []byte) (bool, error) {
	got, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return bytes.Equal(got, want), nil
}
