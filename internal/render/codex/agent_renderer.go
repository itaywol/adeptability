package codex

import (
	"context"
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// AgentOutputPath is the per-agent output template for Codex. Codex custom
// agents are TOML config layers, one file per agent — unlike Codex skills,
// which aggregate into AGENTS.md.
const AgentOutputPath = ".codex/agents/{id}.toml"

// RenderAgent produces the .codex/agents/<id>.toml bytes for one agent. The
// markdown body becomes developer_instructions (required by Codex, so an
// empty body is an error here even though markdown targets allow it). Tool
// lists and a non-default mode have no Codex analog and are warn-dropped
// (Codex controls capability via sandbox_mode — a harness override).
func (r *Renderer) RenderAgent(ctx context.Context, in adept.AgentRenderInput) (adept.RenderOutput, error) {
	if err := ctx.Err(); err != nil {
		return adept.RenderOutput{}, err
	}
	a := in.Agent
	if a == nil {
		return adept.RenderOutput{}, fmt.Errorf("codex render agent: %w: agent is nil", adept.ErrAgentInvalid)
	}
	if a.ID == "" {
		return adept.RenderOutput{}, fmt.Errorf("codex render agent: %w: agent id empty", adept.ErrAgentInvalid)
	}
	if a.Description == "" {
		return adept.RenderOutput{}, fmt.Errorf("codex render agent: %w: agent %q description empty", adept.ErrAgentInvalid, a.ID)
	}
	instructions := strings.TrimSpace(a.Body)
	if instructions == "" {
		// Codex hard-requires developer_instructions; an empty body is valid
		// on the markdown harnesses, so warn-drop this one target instead of
		// failing the whole sync (empty Path = dropped, mirroring how skill
		// renderers signal "not applicable here").
		return adept.RenderOutput{
			SkillID:  a.ID,
			Warnings: []string{fmt.Sprintf("codex: agent %q dropped (empty body; codex requires developer_instructions)", a.ID)},
		}, nil
	}

	var warnings []string
	doc := map[string]any{
		"name":                   a.ID,
		"description":            strings.TrimSpace(a.Description),
		"developer_instructions": instructions + "\n",
	}
	if a.Model != "" {
		doc["model"] = a.Model
	}
	if len(a.Tools) > 0 || len(a.DisallowedTools) > 0 {
		warnings = append(warnings, fmt.Sprintf("codex: agent %q tool lists dropped (Codex controls capability via sandbox_mode; set it via a harness override)", a.ID))
	}
	if a.Mode != "" && a.Mode != adept.AgentModeSubagent {
		warnings = append(warnings, fmt.Sprintf("codex: agent %q mode %q ignored (Codex has no agent mode concept)", a.ID, a.Mode))
	}
	// Per-harness overrides merge as top-level TOML keys, last-wins. The
	// schema forbids overriding id/name/description, but the sync path does
	// not re-validate hand-edited canonical files — so guard the identity
	// keys here too instead of letting them be silently clobbered.
	hid := in.Harness.ID
	if hid == "" {
		hid = "codex"
	}
	for k, v := range a.Harness[hid] {
		switch k {
		case "id", "name", "description":
			warnings = append(warnings, fmt.Sprintf("codex: agent %q override %q ignored (identity fields cannot be overridden)", a.ID, k))
			continue
		}
		doc[k] = v
	}

	// go-toml/v2 sorts map keys, so output is deterministic.
	raw, err := toml.Marshal(doc)
	if err != nil {
		return adept.RenderOutput{}, fmt.Errorf("codex render agent %q: marshal: %w", a.ID, err)
	}
	return adept.RenderOutput{
		Path:      strings.ReplaceAll(AgentOutputPath, "{id}", a.ID),
		Bytes:     raw,
		Mode:      0o644,
		SkillID:   a.ID,
		SkillHash: common.ShortAgentHash(a),
		Warnings:  warnings,
	}, nil
}
