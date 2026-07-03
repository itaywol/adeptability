package opencode

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

// opencodeAgentKnownKeys are the frontmatter keys ImportAgents reverse-maps.
var opencodeAgentKnownKeys = map[string]bool{
	"description": true,
	"mode":        true,
	"model":       true,
}

// AgentRenderer returns the OpenCode agent renderer.
func (a *Adapter) AgentRenderer() adept.AgentRenderer { return a.r }

// ValidateAgents computes drift between expected rendered agents and disk.
func (a *Adapter) ValidateAgents(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	return a.differ.Compute(projectRoot, expected)
}

// ImportAgents walks .opencode/agents/*.md; the filename is the agent name by
// OpenCode's own contract, so it is authoritative for canonical identity too.
func (a *Adapter) ImportAgents(_ context.Context, projectRoot string) ([]adept.ImportedAgent, error) {
	base := filepath.Join(projectRoot, ".opencode", "agents")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opencode import agents: read %s: %w", base, err)
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
			return nil, fmt.Errorf("opencode import agent %s: %w", id, err)
		}
		fields, body, err := common.ParseAgentMarkdown(raw)
		if err != nil {
			return nil, fmt.Errorf("opencode import agent %s: %w", id, err)
		}
		mode := adept.AgentMode(common.StringField(fields, "mode"))
		if mode == "" {
			mode = adept.AgentModeSubagent
		}
		agent := &adept.Agent{
			ID:          id,
			Description: common.StringField(fields, "description"),
			Mode:        mode,
			Model:       common.StringField(fields, "model"),
			Body:        body,
		}
		out = append(out, adept.ImportedAgent{
			Agent:      agent,
			SourcePath: path,
			Warnings:   common.ForeignKeyWarnings("opencode", id, fields, opencodeAgentKnownKeys),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent.ID < out[j].Agent.ID })
	return out, nil
}

var _ adept.AgentSupport = (*Adapter)(nil)
