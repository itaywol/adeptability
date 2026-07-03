package copilot

import (
	"context"
	"fmt"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// AgentOutputPath is the per-agent output template for GitHub Copilot.
// Copilot's reference format names agent profiles <name>.agent.md under
// .github/agents/.
const AgentOutputPath = ".github/agents/{id}.agent.md"

// RenderAgent produces the .github/agents/<id>.agent.md bytes for one agent.
// Unlike Copilot's skill path (instruction buckets), agent profiles are
// per-file — so even this aggregator harness renders agents individually.
// name, description, tools, and model map natively; disallowed-tools and a
// non-default mode have no Copilot analog and are warn-dropped.
func (r *Renderer) RenderAgent(ctx context.Context, in adept.AgentRenderInput) (adept.RenderOutput, error) {
	if err := ctx.Err(); err != nil {
		return adept.RenderOutput{}, err
	}
	a := in.Agent
	if a == nil {
		return adept.RenderOutput{}, fmt.Errorf("copilot render agent: %w: agent is nil", adept.ErrAgentInvalid)
	}
	if a.ID == "" {
		return adept.RenderOutput{}, fmt.Errorf("copilot render agent: %w: agent id empty", adept.ErrAgentInvalid)
	}
	if a.Description == "" {
		return adept.RenderOutput{}, fmt.Errorf("copilot render agent: %w: agent %q description empty", adept.ErrAgentInvalid, a.ID)
	}

	var warnings []string
	fields := []common.Field{
		{Key: "name", Value: a.ID},
		{Key: "description", Value: strings.TrimSpace(a.Description)},
	}
	if len(a.Tools) > 0 {
		fields = append(fields, common.Field{Key: "tools", Value: a.Tools})
	}
	if a.Model != "" {
		fields = append(fields, common.Field{Key: "model", Value: a.Model})
	}
	if len(a.DisallowedTools) > 0 {
		warnings = append(warnings, fmt.Sprintf("copilot: agent %q disallowed-tools dropped (Copilot agents have only an allowlist)", a.ID))
	}
	if a.Mode != "" && a.Mode != adept.AgentModeSubagent {
		warnings = append(warnings, fmt.Sprintf("copilot: agent %q mode %q ignored (Copilot has no agent mode concept)", a.ID, a.Mode))
	}

	hid := in.Harness.ID
	if hid == "" {
		hid = "copilot"
	}
	fields, err := common.MergeOverride(fields, a.Harness[hid])
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("copilot render agent %q: %w", a.ID, err)
	}
	front, err := common.NewFrontmatterBuilder().Build(fields)
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("copilot render agent %q: %w", a.ID, err)
	}

	body := strings.TrimRight(a.Body, "\n")
	var sb strings.Builder
	sb.WriteString(front)
	sb.WriteString("\n")
	if body != "" {
		sb.WriteString(body)
		sb.WriteString("\n")
	}
	tp := common.NewPathTemplater()
	return adept.RenderOutput{
		Path:      tp.Resolve(AgentOutputPath, a.ID),
		Bytes:     []byte(sb.String()),
		Mode:      0o644,
		SkillID:   a.ID,
		SkillHash: common.ShortAgentHash(a),
		Warnings:  warnings,
	}, nil
}
