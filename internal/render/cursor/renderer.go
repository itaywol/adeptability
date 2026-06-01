// Package cursor renders canonical skills into Cursor's .mdc rule format.
//
// Output layout: .cursor/rules/<id>.mdc (single file per skill).
// Sidecars are dropped because Cursor has no directory layout — the renderer
// records a warning per dropped sidecar on the RenderOutput.
package cursor

import (
	"context"
	"fmt"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Renderer is the Cursor Renderer implementation.
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

// Spec is the static description of the Cursor harness.
func Spec() adept.HarnessSpec {
	return adept.HarnessSpec{
		ID:         "cursor",
		Name:       "Cursor",
		Kind:       adept.KindPerSkill,
		OutputPath: ".cursor/rules/{id}.mdc",
		NeedsDir:   false,
		BaseDir:    ".cursor",
	}
}

// Render produces a single .mdc file per skill. Sidecars are dropped (with
// a warning) because Cursor does not support directory-style rules.
func (r *Renderer) Render(ctx context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
	if err := ctx.Err(); err != nil {
		return adept.RenderOutput{}, err
	}
	if in.Skill == nil {
		return adept.RenderOutput{}, fmt.Errorf("cursor render: %w: skill is nil", adept.ErrSkillInvalid)
	}
	s := in.Skill
	if s.ID == "" {
		return adept.RenderOutput{}, fmt.Errorf("cursor render: %w: skill id empty", adept.ErrSkillInvalid)
	}

	fields, err := fieldsFor(s)
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("cursor render %q: %w", s.ID, err)
	}

	front, err := r.fm.Build(fields)
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("cursor render %q: %w", s.ID, err)
	}

	body := strings.TrimRight(s.Body, "\n")
	var sb strings.Builder
	sb.WriteString(front)
	sb.WriteString("\n")
	if s.Activation == adept.ActivationManual {
		sb.WriteString(fmt.Sprintf("Invoke this rule manually with `@%s`.\n\n", s.ID))
	}
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
		out.Warnings = append(out.Warnings,
			fmt.Sprintf("cursor: dropped sidecar %q (cursor harness is single-file)", f.RelPath))
	}
	return out, nil
}

func fieldsFor(s *adept.Skill) ([]common.Field, error) {
	desc := strings.TrimSpace(s.Description)
	if desc == "" {
		return nil, fmt.Errorf("%w: skill %q description empty", adept.ErrSkillInvalid, s.ID)
	}

	switch s.Activation {
	case adept.ActivationAlways:
		return []common.Field{
			{Key: "description", Value: desc, Quote: strings.ContainsAny(desc, ":#")},
			{Key: "alwaysApply", Value: true},
		}, nil
	case adept.ActivationGlobs:
		if len(s.Globs) == 0 {
			return nil, fmt.Errorf("%w: skill %q activation=globs requires globs", adept.ErrSkillInvalid, s.ID)
		}
		return []common.Field{
			{Key: "description", Value: desc, Quote: strings.ContainsAny(desc, ":#")},
			{Key: "globs", Value: s.Globs},
			{Key: "alwaysApply", Value: false},
		}, nil
	case adept.ActivationAgent, "":
		return []common.Field{
			{Key: "description", Value: desc, Quote: strings.ContainsAny(desc, ":#")},
		}, nil
	case adept.ActivationManual:
		return []common.Field{
			{Key: "description", Value: desc, Quote: strings.ContainsAny(desc, ":#")},
		}, nil
	default:
		return nil, fmt.Errorf("%w: skill %q unknown activation %q", adept.ErrSkillInvalid, s.ID, s.Activation)
	}
}
