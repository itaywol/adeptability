package claude

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Adapter wires the Claude Code Renderer with the filesystem dependencies
// needed for Detect and Validate.
type Adapter struct {
	r      *Renderer
	w      common.Writer
	l      common.Linker
	differ common.Differ
}

// NewAdapter constructs an Adapter. The writer is currently unused by
// Detect/Validate but is held so wiring stays uniform across harnesses.
func NewAdapter(r *Renderer, w common.Writer, l common.Linker) *Adapter {
	return &Adapter{
		r:      r,
		w:      w,
		l:      l,
		differ: common.NewDiffer(l),
	}
}

var _ adept.HarnessAdapter = (*Adapter)(nil)

// Spec returns the static harness description.
func (a *Adapter) Spec() adept.HarnessSpec { return Spec() }

// Renderer returns the underlying per-skill renderer.
func (a *Adapter) Renderer() adept.Renderer { return a.r }

// Aggregate is a no-op for per-skill harnesses; it returns parts unchanged.
// budgetB is accepted for interface compatibility but ignored.
func (a *Adapter) Aggregate(_ context.Context, parts []adept.RenderOutput, _ int) ([]adept.RenderOutput, error) {
	return parts, nil
}

// Detect reports whether projectRoot looks like a Claude Code project.
// True if .claude/ or .claude/skills/ exists in any form (file or directory).
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	if a.l == nil {
		return false, fmt.Errorf("claude detect: %w: no linker", adept.ErrAdapterInvalid)
	}
	base := filepath.Join(projectRoot, ".claude")
	if a.l.PathType(base) != common.PathMissing {
		return true, nil
	}
	skills := filepath.Join(projectRoot, ".claude", "skills")
	if a.l.PathType(skills) != common.PathMissing {
		return true, nil
	}
	return false, nil
}

// Validate computes the drift between expected outputs and the on-disk state.
func (a *Adapter) Validate(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	return a.differ.Compute(projectRoot, expected)
}
