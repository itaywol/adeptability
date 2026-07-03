package common

import (
	"bytes"
	"errors"
	"io/fs"
	"path/filepath"

	"github.com/itaywol/adeptability/pkg/adept"
)

// ComputePerFileDrift classifies expected per-file outputs against disk using
// a plain read function (fs.ErrNotExist for absent files). It is the agent
// drift helper for adapters whose skill Validate is aggregator-shaped (codex,
// copilot) and therefore have no common.Differ: agent outputs are always
// per-file, even on aggregator harnesses.
func ComputePerFileDrift(projectRoot string, expected []adept.RenderOutput, read func(string) ([]byte, error)) (adept.DriftReport, error) {
	var report adept.DriftReport
	for _, out := range expected {
		got, err := read(filepath.Join(projectRoot, out.Path))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			report.Missing = append(report.Missing, out.Path)
		case err != nil:
			// Unreadable-but-present (EISDIR, permission) is a Conflict per
			// the drift contract, not a fatal error.
			report.Conflict = append(report.Conflict, out.Path)
		case bytes.Equal(got, out.Bytes):
			report.Synced = append(report.Synced, out.Path)
		default:
			report.Drifted = append(report.Drifted, out.Path)
		}
	}
	return report, nil
}
