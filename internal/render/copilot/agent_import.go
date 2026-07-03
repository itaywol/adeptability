package copilot

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

// copilotAgentKnownKeys are the frontmatter keys ImportAgents reverse-maps.
var copilotAgentKnownKeys = map[string]bool{
	"name":        true,
	"description": true,
	"tools":       true,
	"model":       true,
}

// AgentRenderer returns the Copilot agent renderer.
func (a *Adapter) AgentRenderer() adept.AgentRenderer { return a.r }

// ValidateAgents computes drift for expected rendered agents. Copilot's skill
// Validate is bucket-shaped, so agents use the shared per-file helper.
func (a *Adapter) ValidateAgents(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	return common.ComputePerFileDrift(projectRoot, expected, a.f.ReadFile)
}

// ImportAgents walks .github/agents/ reading both <name>.agent.md (reference
// format) and plain <name>.md (also accepted at repo level).
func (a *Adapter) ImportAgents(_ context.Context, projectRoot string) ([]adept.ImportedAgent, error) {
	base := filepath.Join(projectRoot, ".github", "agents")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("copilot import agents: read %s: %w", base, err)
	}
	// Both <id>.agent.md (reference format) and plain <id>.md map to the same
	// id; when both exist, prefer .agent.md instead of emitting a self-
	// conflict resolved by directory order.
	hasAgentMD := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".agent.md") && !strings.HasPrefix(e.Name(), ".") {
			hasAgentMD[strings.TrimSuffix(e.Name(), ".agent.md")] = true
		}
	}
	out := make([]adept.ImportedAgent, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimSuffix(e.Name(), ".md"), ".agent")
		if !strings.HasSuffix(e.Name(), ".agent.md") && hasAgentMD[id] {
			continue // shadowed by the .agent.md variant
		}
		path := filepath.Join(base, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("copilot import agent %s: %w", id, err)
		}
		fields, body, err := common.ParseAgentMarkdown(raw)
		if err != nil {
			return nil, fmt.Errorf("copilot import agent %s: %w", id, err)
		}
		agent := &adept.Agent{
			ID:          id,
			Description: common.StringField(fields, "description"),
			Mode:        adept.AgentModeSubagent,
			Tools:       common.StringListField(fields, "tools"),
			Model:       common.StringField(fields, "model"),
			Body:        body,
		}
		out = append(out, adept.ImportedAgent{
			Agent:      agent,
			SourcePath: path,
			Warnings:   common.ForeignKeyWarnings("copilot", id, fields, copilotAgentKnownKeys),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent.ID < out[j].Agent.ID })
	return out, nil
}

var _ adept.AgentSupport = (*Adapter)(nil)
