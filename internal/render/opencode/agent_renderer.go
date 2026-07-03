package opencode

import (
	"context"
	"fmt"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// AgentOutputPath is the per-agent output template for OpenCode. The filename
// becomes the agent name — OpenCode agent files carry no `name` field.
const AgentOutputPath = ".opencode/agents/{id}.md"

// RenderAgent produces the .opencode/agents/<id>.md bytes for one agent.
// description, mode, and model map natively; tool lists have no OpenCode
// analog (its `tools` map is deprecated in favor of `permission`) and are
// warn-dropped — use a `harness: opencode: permission:` override instead.
func (r *Renderer) RenderAgent(ctx context.Context, in adept.AgentRenderInput) (adept.RenderOutput, error) {
	if err := ctx.Err(); err != nil {
		return adept.RenderOutput{}, err
	}
	a := in.Agent
	if a == nil {
		return adept.RenderOutput{}, fmt.Errorf("opencode render agent: %w: agent is nil", adept.ErrAgentInvalid)
	}
	if a.ID == "" {
		return adept.RenderOutput{}, fmt.Errorf("opencode render agent: %w: agent id empty", adept.ErrAgentInvalid)
	}
	if a.Description == "" {
		return adept.RenderOutput{}, fmt.Errorf("opencode render agent: %w: agent %q description empty", adept.ErrAgentInvalid, a.ID)
	}

	var warnings []string
	mode := a.Mode
	if mode == "" {
		mode = adept.AgentModeSubagent
	}
	fields := []common.Field{
		{Key: "description", Value: strings.TrimSpace(a.Description)},
		{Key: "mode", Value: string(mode)},
	}
	if a.Model != "" {
		fields = append(fields, common.Field{Key: "model", Value: a.Model})
	}
	if len(a.Tools) > 0 {
		warnings = append(warnings, fmt.Sprintf("opencode: agent %q tools dropped (OpenCode uses a permission map; set it via a harness override)", a.ID))
	}
	if len(a.DisallowedTools) > 0 {
		warnings = append(warnings, fmt.Sprintf("opencode: agent %q disallowed-tools dropped (OpenCode uses a permission map; set it via a harness override)", a.ID))
	}

	hid := in.Harness.ID
	if hid == "" {
		hid = Spec().ID
	}
	fields, err := common.MergeOverride(fields, a.Harness[hid])
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("opencode render agent %q: %w", a.ID, err)
	}
	front, err := common.NewFrontmatterBuilder().Build(fields)
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("opencode render agent %q: %w", a.ID, err)
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
