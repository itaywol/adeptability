package claude

import (
	"context"
	"fmt"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// AgentOutputPath is the per-agent output template for Claude Code.
const AgentOutputPath = ".claude/agents/{id}.md"

// RenderAgent produces the .claude/agents/<id>.md bytes for one agent.
// Claude Code is the richest agent target: name, description, tools,
// disallowedTools, and model all map natively; only a non-default mode is
// dropped (Claude subagents have no mode concept).
func (r *Renderer) RenderAgent(ctx context.Context, in adept.AgentRenderInput) (adept.RenderOutput, error) {
	if err := ctx.Err(); err != nil {
		return adept.RenderOutput{}, err
	}
	a := in.Agent
	if a == nil {
		return adept.RenderOutput{}, fmt.Errorf("claude render agent: %w: agent is nil", adept.ErrAgentInvalid)
	}
	if a.ID == "" {
		return adept.RenderOutput{}, fmt.Errorf("claude render agent: %w: agent id empty", adept.ErrAgentInvalid)
	}
	if a.Description == "" {
		return adept.RenderOutput{}, fmt.Errorf("claude render agent: %w: agent %q description empty", adept.ErrAgentInvalid, a.ID)
	}

	var warnings []string
	fields := []common.Field{
		{Key: "name", Value: a.ID},
		{Key: "description", Value: strings.TrimSpace(a.Description)},
	}
	if len(a.Tools) > 0 {
		fields = append(fields, common.Field{Key: "tools", Value: strings.Join(a.Tools, ", ")})
	}
	if len(a.DisallowedTools) > 0 {
		fields = append(fields, common.Field{Key: "disallowedTools", Value: strings.Join(a.DisallowedTools, ", ")})
	}
	if a.Model != "" {
		fields = append(fields, common.Field{Key: "model", Value: a.Model})
	}
	if a.Mode != "" && a.Mode != adept.AgentModeSubagent {
		warnings = append(warnings, fmt.Sprintf("claude: agent %q mode %q ignored (Claude Code subagents have no mode concept)", a.ID, a.Mode))
	}

	hid := in.Harness.ID
	if hid == "" {
		hid = Spec().ID
	}
	fields, err := common.MergeOverride(fields, a.Harness[hid])
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("claude render agent %q: %w", a.ID, err)
	}
	front, err := r.fm.Build(fields)
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("claude render agent %q: %w", a.ID, err)
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
