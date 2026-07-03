package cursor

import (
	"context"
	"fmt"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// AgentOutputPath is the per-agent output template for Cursor.
const AgentOutputPath = ".cursor/agents/{id}.md"

// RenderAgent produces the .cursor/agents/<id>.md bytes for one agent. Cursor
// agent frontmatter is all-optional: name, description, and model map
// natively; tool lists and non-default mode have no Cursor analog (use a
// `readonly` harness override for read-only agents) and are warn-dropped.
func (r *Renderer) RenderAgent(ctx context.Context, in adept.AgentRenderInput) (adept.RenderOutput, error) {
	if err := ctx.Err(); err != nil {
		return adept.RenderOutput{}, err
	}
	a := in.Agent
	if a == nil {
		return adept.RenderOutput{}, fmt.Errorf("cursor render agent: %w: agent is nil", adept.ErrAgentInvalid)
	}
	if a.ID == "" {
		return adept.RenderOutput{}, fmt.Errorf("cursor render agent: %w: agent id empty", adept.ErrAgentInvalid)
	}
	if a.Description == "" {
		return adept.RenderOutput{}, fmt.Errorf("cursor render agent: %w: agent %q description empty", adept.ErrAgentInvalid, a.ID)
	}

	var warnings []string
	fields := []common.Field{
		{Key: "name", Value: a.ID},
		{Key: "description", Value: strings.TrimSpace(a.Description), Quote: true},
	}
	if a.Model != "" {
		fields = append(fields, common.Field{Key: "model", Value: a.Model})
	}
	if len(a.Tools) > 0 || len(a.DisallowedTools) > 0 {
		warnings = append(warnings, fmt.Sprintf("cursor: agent %q tool lists dropped (Cursor agents have no tools field; consider a readonly harness override)", a.ID))
	}
	if a.Mode != "" && a.Mode != adept.AgentModeSubagent {
		warnings = append(warnings, fmt.Sprintf("cursor: agent %q mode %q ignored (Cursor has no agent mode concept)", a.ID, a.Mode))
	}

	hid := in.Harness.ID
	if hid == "" {
		hid = Spec().ID
	}
	fields, err := common.MergeOverride(fields, a.Harness[hid])
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("cursor render agent %q: %w", a.ID, err)
	}
	front, err := r.fm.Build(fields)
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("cursor render agent %q: %w", a.ID, err)
	}

	body := strings.TrimRight(a.Body, "\n")
	var sb strings.Builder
	sb.WriteString(front)
	sb.WriteString("\n")
	if body != "" {
		sb.WriteString(body)
		sb.WriteString("\n")
	}
	return adept.RenderOutput{
		Path:      r.tp.Resolve(AgentOutputPath, a.ID),
		Bytes:     []byte(sb.String()),
		Mode:      0o644,
		SkillID:   a.ID,
		SkillHash: common.ShortAgentHash(a),
		Warnings:  warnings,
	}, nil
}
