package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// claudeAgentKnownKeys are the frontmatter keys ImportAgents reverse-maps to
// canonical fields. Everything else is warn-dropped (the harness override
// block is never reconstructed, matching skill-import policy).
var claudeAgentKnownKeys = map[string]bool{
	"name":            true,
	"description":     true,
	"tools":           true,
	"disallowedTools": true,
	"model":           true,
}

// AgentRenderer returns the Claude Code agent renderer.
func (a *Adapter) AgentRenderer() adept.AgentRenderer { return a.r }

// ValidateAgents computes drift between expected rendered agents and disk.
func (a *Adapter) ValidateAgents(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	return a.differ.Compute(projectRoot, expected)
}

// ImportAgents walks .claude/agents/*.md and reverse-maps each file into a
// canonical agent. The filename (minus .md) is authoritative for identity.
func (a *Adapter) ImportAgents(_ context.Context, projectRoot string) ([]adept.ImportedAgent, error) {
	base := filepath.Join(projectRoot, ".claude", "agents")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("claude import agents: read %s: %w", base, err)
	}
	out := make([]adept.ImportedAgent, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		path := filepath.Join(base, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("claude import agent %s: %w", id, err)
		}
		fields, body, err := common.ParseAgentMarkdown(raw)
		if err != nil {
			return nil, fmt.Errorf("claude import agent %s: %w", id, err)
		}
		agent := &adept.Agent{
			ID:              id,
			Description:     common.StringField(fields, "description"),
			Mode:            adept.AgentModeSubagent,
			Tools:           common.StringListField(fields, "tools"),
			DisallowedTools: common.StringListField(fields, "disallowedTools"),
			Model:           common.StringField(fields, "model"),
			Body:            body,
		}
		out = append(out, adept.ImportedAgent{
			Agent:      agent,
			SourcePath: path,
			Warnings:   common.ForeignKeyWarnings("claude", id, fields, claudeAgentKnownKeys),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent.ID < out[j].Agent.ID })
	return out, nil
}

var _ adept.AgentSupport = (*Adapter)(nil)
