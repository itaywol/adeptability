package common

import (
	"bytes"
	"fmt"
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

// Compute classifies each expected output — and each of its sidecars —
// as Synced, Drifted, Missing, or Conflict relative to the actual files at
// projectRoot.
//
// Classification rules:
//   - missing on disk           → Missing
//   - file with matching bytes  → Synced
//   - file with differing bytes → Drifted
//   - symlink resolving to file with matching bytes → Synced
//   - symlink resolving to file with differing bytes → Drifted
//   - any other shape (dir, broken symlink, symlink → dir) → Conflict
func (d *differ) Compute(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	var report adept.DriftReport
	for _, out := range expected {
		if err := d.classify(projectRoot, out.Path, out.Bytes, &report); err != nil {
			return adept.DriftReport{}, err
		}
		// Sidecars carry their own on-disk bytes and can drift independently
		// of the main rendered file, so they must be classified too —
		// otherwise `status`/`verify`/`diff` report a harness as fully Synced
		// while a sidecar script on disk is corrupt or missing. The on-disk
		// path mirrors the orchestrator's materialization
		// (filepath.Dir(out.Path) joined with side.RelPath) so detection and
		// materialization agree.
		for _, side := range out.Sidecars {
			sideRel := filepath.Join(filepath.Dir(out.Path), side.RelPath)
			if err := d.classify(projectRoot, sideRel, side.Bytes, &report); err != nil {
				return adept.DriftReport{}, err
			}
		}
	}
	return report, nil
}

// classify folds a single expected path (main file or sidecar) into report,
// applying the documented Synced/Drifted/Missing/Conflict rules.
func (d *differ) classify(projectRoot, relPath string, want []byte, report *adept.DriftReport) error {
	abs := filepath.Join(projectRoot, relPath)
	switch d.fs.PathType(abs) {
	case PathMissing:
		report.Missing = append(report.Missing, relPath)
	case PathFile:
		same, err := bytesMatch(abs, want)
		if err != nil {
			return fmt.Errorf("drift: %s: %w", relPath, err)
		}
		if same {
			report.Synced = append(report.Synced, relPath)
		} else {
			report.Drifted = append(report.Drifted, relPath)
		}
	case PathSymlink:
		target, err := d.fs.ReadSymlink(abs)
		if err != nil {
			// A symlink we cannot read (e.g. EISDIR / permission) is a
			// Conflict by the documented contract, not a fatal error; we
			// record it and continue with the remaining outputs.
			report.Conflict = append(report.Conflict, relPath)
			return nil //nolint:nilerr // intentional: unreadable symlink -> Conflict, keep scanning
		}
		resolved := target
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(abs), resolved)
		}
		same, err := bytesMatch(resolved, want)
		if err != nil {
			// Any read failure of the resolved target — a broken link
			// (ErrNotExist) OR a target that is not a regular file (e.g. a
			// symlink pointing at a directory yields EISDIR, which is NOT
			// ErrNotExist) — is a Conflict per the documented contract.
			// Previously only ErrNotExist was special-cased and every other
			// error aborted the entire drift pass for all remaining outputs.
			report.Conflict = append(report.Conflict, relPath)
			return nil //nolint:nilerr // intentional: unreadable/EISDIR target -> Conflict, keep scanning
		}
		if same {
			report.Synced = append(report.Synced, relPath)
		} else {
			report.Drifted = append(report.Drifted, relPath)
		}
	default:
		report.Conflict = append(report.Conflict, relPath)
	}
	return nil
}

func bytesMatch(path string, want []byte) (bool, error) {
	got, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return bytes.Equal(got, want), nil
}
