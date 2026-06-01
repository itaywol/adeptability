// Package opencode renders canonical skills into OpenCode's plain-markdown
// SKILL.md format.
//
// Output layout: .opencode/skill/<id>/SKILL.md with optional sidecars.
// No YAML frontmatter; the renderer emits a `# <id>` heading followed by the
// description and the skill body.
package opencode

import (
	"context"
	"fmt"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Renderer is the OpenCode Renderer implementation.
type Renderer struct {
	tp common.PathTemplater
}

// New constructs a Renderer with the default path templater.
func New() *Renderer { return &Renderer{tp: common.NewPathTemplater()} }

var _ adept.Renderer = (*Renderer)(nil)

// Spec is the static description of the OpenCode harness.
func Spec() adept.HarnessSpec {
	return adept.HarnessSpec{
		ID:         "opencode",
		Name:       "OpenCode",
		Kind:       adept.KindPerSkill,
		OutputPath: ".opencode/skill/{id}/SKILL.md",
		NeedsDir:   true,
		BaseDir:    ".opencode",
	}
}

// Render produces a plain-markdown SKILL.md plus any sidecars. Activation is
// ignored — OpenCode discovers skills through the directory layout alone.
func (r *Renderer) Render(ctx context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
	if err := ctx.Err(); err != nil {
		return adept.RenderOutput{}, err
	}
	if in.Skill == nil {
		return adept.RenderOutput{}, fmt.Errorf("opencode render: %w: skill is nil", adept.ErrSkillInvalid)
	}
	s := in.Skill
	if s.ID == "" {
		return adept.RenderOutput{}, fmt.Errorf("opencode render: %w: skill id empty", adept.ErrSkillInvalid)
	}
	if s.Description == "" {
		return adept.RenderOutput{}, fmt.Errorf("opencode render: %w: skill %q description empty", adept.ErrSkillInvalid, s.ID)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", s.ID))
	sb.WriteString(strings.TrimSpace(s.Description))
	sb.WriteString("\n")
	body := strings.TrimRight(s.Body, "\n")
	if body != "" {
		sb.WriteString("\n")
		sb.WriteString(body)
		sb.WriteString("\n")
	}

	path := r.tp.Resolve(Spec().OutputPath, s.ID)
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
