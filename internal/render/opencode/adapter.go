package opencode

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Adapter wires the OpenCode Renderer with filesystem helpers for Detect/Validate.
type Adapter struct {
	r      *Renderer
	w      common.Writer
	l      common.Linker
	differ common.Differ
}

// NewAdapter constructs an Adapter.
func NewAdapter(r *Renderer, w common.Writer, l common.Linker) *Adapter {
	return &Adapter{r: r, w: w, l: l, differ: common.NewDiffer(l)}
}

var _ adept.HarnessAdapter = (*Adapter)(nil)

// Spec returns the static harness description.
func (a *Adapter) Spec() adept.HarnessSpec { return Spec() }

// Renderer returns the underlying per-skill renderer.
func (a *Adapter) Renderer() adept.Renderer { return a.r }

// Aggregate is a no-op for per-skill harnesses.
func (a *Adapter) Aggregate(_ context.Context, parts []adept.RenderOutput, _ int) ([]adept.RenderOutput, error) {
	return parts, nil
}

// Detect reports whether projectRoot looks like an OpenCode project.
// True if .opencode/ or .opencode/skill/ exists.
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	if a.l == nil {
		return false, fmt.Errorf("opencode detect: %w: no linker", adept.ErrAdapterInvalid)
	}
	base := filepath.Join(projectRoot, ".opencode")
	if a.l.PathType(base) != common.PathMissing {
		return true, nil
	}
	skills := filepath.Join(projectRoot, ".opencode", "skill")
	if a.l.PathType(skills) != common.PathMissing {
		return true, nil
	}
	return false, nil
}

// Validate computes drift between expected outputs and the on-disk state.
func (a *Adapter) Validate(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	return a.differ.Compute(projectRoot, expected)
}
