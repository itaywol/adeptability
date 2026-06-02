package perskill

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Adapter is the generic per-skill HarnessAdapter. It wraps a Renderer
// plus the filesystem dependencies required for Detect/Validate.
type Adapter struct {
	r      *Renderer
	w      common.Writer
	l      common.Linker
	differ common.Differ
}

// NewAdapter constructs an Adapter for a given HarnessSpec.
func NewAdapter(spec adept.HarnessSpec, w common.Writer, l common.Linker) *Adapter {
	return &Adapter{
		r:      NewRenderer(spec),
		w:      w,
		l:      l,
		differ: common.NewDiffer(l),
	}
}

var _ adept.HarnessAdapter = (*Adapter)(nil)

// Spec returns the static harness description.
func (a *Adapter) Spec() adept.HarnessSpec { return a.r.Spec() }

// Renderer returns the underlying per-skill renderer.
func (a *Adapter) Renderer() adept.Renderer { return a.r }

// Aggregate is a no-op for per-skill harnesses; it returns parts unchanged.
func (a *Adapter) Aggregate(_ context.Context, parts []adept.RenderOutput, _ int) ([]adept.RenderOutput, error) {
	return parts, nil
}

// Detect reports whether projectRoot looks like this harness. True when
// either BaseDir exists, or the skill container directory derived from
// OutputPath exists. The second probe is essential for harnesses whose
// BaseDir collides with another harness (e.g. multiple agents sharing
// `.agents/skills/`).
func (a *Adapter) Detect(projectRoot string) (bool, error) {
	if a.l == nil {
		return false, fmt.Errorf("%s detect: %w: no linker", a.r.spec.ID, adept.ErrAdapterInvalid)
	}
	if base := strings.TrimSpace(a.r.spec.BaseDir); base != "" {
		if a.l.PathType(filepath.Join(projectRoot, base)) != common.PathMissing {
			return true, nil
		}
	}
	if container := skillsContainer(a.r.spec.OutputPath); container != "" {
		if a.l.PathType(filepath.Join(projectRoot, container)) != common.PathMissing {
			return true, nil
		}
	}
	return false, nil
}

// Validate computes the drift between expected outputs and on-disk state.
func (a *Adapter) Validate(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	return a.differ.Compute(projectRoot, expected)
}

// skillsContainer extracts the directory portion above the "{id}"
// placeholder. For ".claude/skills/{id}/SKILL.md" it returns
// ".claude/skills". Returns empty when no placeholder is present.
func skillsContainer(template string) string {
	idx := strings.Index(template, "{id}")
	if idx <= 0 {
		return ""
	}
	return strings.TrimRight(template[:idx], "/")
}
