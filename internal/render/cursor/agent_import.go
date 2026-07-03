package cursor

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

// cursorAgentKnownKeys are the frontmatter keys ImportAgents reverse-maps.
var cursorAgentKnownKeys = map[string]bool{
	"name":        true,
	"description": true,
	"model":       true,
}

// AgentRenderer returns the Cursor agent renderer.
func (a *Adapter) AgentRenderer() adept.AgentRenderer { return a.r }

// ValidateAgents computes drift between expected rendered agents and disk.
func (a *Adapter) ValidateAgents(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	return a.differ.Compute(projectRoot, expected)
}

// ImportAgents walks .cursor/agents/*.md and reverse-maps each file into a
// canonical agent. Cursor also reads .claude/agents/ for compatibility, but
// import deliberately only claims Cursor's native directory — the claude
// adapter owns its own tree.
func (a *Adapter) ImportAgents(_ context.Context, projectRoot string) ([]adept.ImportedAgent, error) {
	base := filepath.Join(projectRoot, ".cursor", "agents")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cursor import agents: read %s: %w", base, err)
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
			return nil, fmt.Errorf("cursor import agent %s: %w", id, err)
		}
		fields, body, err := common.ParseAgentMarkdown(raw)
		if err != nil {
			return nil, fmt.Errorf("cursor import agent %s: %w", id, err)
		}
		agent := &adept.Agent{
			ID:          id,
			Description: common.StringField(fields, "description"),
			Mode:        adept.AgentModeSubagent,
			Model:       common.StringField(fields, "model"),
			Body:        body,
		}
		out = append(out, adept.ImportedAgent{
			Agent:      agent,
			SourcePath: path,
			Warnings:   common.ForeignKeyWarnings("cursor", id, fields, cursorAgentKnownKeys),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent.ID < out[j].Agent.ID })
	return out, nil
}

var _ adept.AgentSupport = (*Adapter)(nil)
