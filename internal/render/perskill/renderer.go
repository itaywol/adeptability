// Package perskill is a generic per-skill renderer parametrized by a
// HarnessSpec. It exists so we can ship adapters for every agent in the
// vercel-labs/skills supported-agents matrix without authoring a near-
// identical renderer package for each one.
//
// Output layout: <OutputPath template>/SKILL.md, where OutputPath holds
// a "{id}" placeholder substituted at render time. Optional sidecars are
// emitted under the same skill directory.
package perskill

import (
	"context"
	"fmt"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Renderer is the generic per-skill renderer.
type Renderer struct {
	spec adept.HarnessSpec
	fm   common.FrontmatterBuilder
	tp   common.PathTemplater
}

// NewRenderer constructs a Renderer from a HarnessSpec. The spec must
// declare Kind=KindPerSkill and an OutputPath ending in /SKILL.md.
func NewRenderer(spec adept.HarnessSpec) *Renderer {
	return &Renderer{
		spec: spec,
		fm:   common.NewFrontmatterBuilder(),
		tp:   common.NewPathTemplater(),
	}
}

var _ adept.Renderer = (*Renderer)(nil)

// Spec returns the harness description this renderer is bound to.
func (r *Renderer) Spec() adept.HarnessSpec { return r.spec }

// Render produces SKILL.md bytes plus any sidecar files for a single skill.
func (r *Renderer) Render(ctx context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
	if err := ctx.Err(); err != nil {
		return adept.RenderOutput{}, err
	}
	if in.Skill == nil {
		return adept.RenderOutput{}, fmt.Errorf("%s render: %w: skill is nil", r.spec.ID, adept.ErrSkillInvalid)
	}
	s := in.Skill
	if s.ID == "" {
		return adept.RenderOutput{}, fmt.Errorf("%s render: %w: skill id empty", r.spec.ID, adept.ErrSkillInvalid)
	}
	if s.Description == "" {
		return adept.RenderOutput{}, fmt.Errorf("%s render: %w: skill %q description empty", r.spec.ID, adept.ErrSkillInvalid, s.ID)
	}

	fields := []common.Field{
		{Key: "name", Value: s.ID},
		{Key: "description", Value: buildDescription(s)},
	}
	if len(s.AllowedTools) > 0 {
		fields = append(fields, common.Field{Key: "allowed-tools", Value: s.AllowedTools})
	}
	if s.Activation == adept.ActivationManual {
		fields = append(fields, common.Field{Key: "disable-model-invocation", Value: true})
	}

	front, err := r.fm.Build(fields)
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("%s render %q: %w", r.spec.ID, s.ID, err)
	}

	body := strings.TrimRight(s.Body, "\n")
	var sb strings.Builder
	sb.WriteString(front)
	sb.WriteString("\n")
	if body != "" {
		sb.WriteString(body)
		sb.WriteString("\n")
	}

	path := r.tp.Resolve(r.spec.OutputPath, s.ID)
	out := adept.RenderOutput{
		Path:      path,
		Bytes:     []byte(sb.String()),
		Mode:      0o644,
		SkillID:   s.ID,
		SkillHash: common.ShortSkillHash(s),
	}
	for _, f := range s.Files {
		out.Sidecars = append(out.Sidecars, adept.SideFile{
			RelPath: f.RelPath,
			Bytes:   f.Bytes,
			Mode:    f.Mode,
		})
	}
	return out, nil
}

// buildDescription embeds glob hints when activation is glob-based —
// most agents have no native "globs" frontmatter field so the hint goes
// in-line where the model will read it.
func buildDescription(s *adept.Skill) string {
	desc := strings.TrimSpace(s.Description)
	if s.Activation == adept.ActivationGlobs && len(s.Globs) > 0 {
		return fmt.Sprintf("%s (matches: %s)", desc, strings.Join(s.Globs, ", "))
	}
	return desc
}
