// Package claude renders canonical skills into Claude Code's per-skill
// SKILL.md format.
//
// Output layout: .claude/skills/<id>/SKILL.md with optional sidecars under
// scripts/, references/, assets/.
package claude

import (
	"context"
	"fmt"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Renderer is the Claude Code Renderer implementation.
type Renderer struct {
	fm common.FrontmatterBuilder
	tp common.PathTemplater
}

// New constructs a Renderer with the default frontmatter and path helpers.
func New() *Renderer {
	return &Renderer{
		fm: common.NewFrontmatterBuilder(),
		tp: common.NewPathTemplater(),
	}
}

var _ adept.Renderer = (*Renderer)(nil)

// Spec is the static description of the Claude Code harness.
func Spec() adept.HarnessSpec {
	return adept.HarnessSpec{
		ID:         "claude-code",
		Name:       "Claude Code",
		Kind:       adept.KindPerSkill,
		OutputPath: ".claude/skills/{id}/SKILL.md",
		NeedsDir:   true,
		BaseDir:    ".claude",
	}
}

// Render produces the SKILL.md bytes plus any sidecar files for a single
// skill. The output path is resolved from the harness spec template.
func (r *Renderer) Render(ctx context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
	if err := ctx.Err(); err != nil {
		return adept.RenderOutput{}, err
	}
	if in.Skill == nil {
		return adept.RenderOutput{}, fmt.Errorf("claude render: %w: skill is nil", adept.ErrSkillInvalid)
	}
	s := in.Skill
	if s.ID == "" {
		return adept.RenderOutput{}, fmt.Errorf("claude render: %w: skill id empty", adept.ErrSkillInvalid)
	}
	if s.Description == "" {
		return adept.RenderOutput{}, fmt.Errorf("claude render: %w: skill %q description empty", adept.ErrSkillInvalid, s.ID)
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
		return adept.RenderOutput{}, fmt.Errorf("claude render %q: %w", s.ID, err)
	}

	body := strings.TrimRight(s.Body, "\n")
	var sb strings.Builder
	sb.WriteString(front)
	sb.WriteString("\n")
	if body != "" {
		sb.WriteString(body)
		sb.WriteString("\n")
	}

	path := r.tp.Resolve(Spec().OutputPath, s.ID)
	out := adept.RenderOutput{
		Path:         path,
		Bytes:        []byte(sb.String()),
		Mode:         0o644,
		SkillID:      s.ID,
		SkillVersion: s.Version,
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

// buildDescription appends a glob hint to the description when the skill is
// glob-activated. Claude has no native globs field; we embed the hint so the
// model can decide on the right surface.
func buildDescription(s *adept.Skill) string {
	desc := strings.TrimSpace(s.Description)
	if s.Activation == adept.ActivationGlobs && len(s.Globs) > 0 {
		return fmt.Sprintf("%s (matches: %s)", desc, strings.Join(s.Globs, ", "))
	}
	return desc
}
