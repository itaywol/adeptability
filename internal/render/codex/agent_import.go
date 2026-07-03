package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// codexAgentKnownKeys are the TOML keys ImportAgents reverse-maps.
var codexAgentKnownKeys = map[string]bool{
	"name":                   true,
	"description":            true,
	"developer_instructions": true,
	"model":                  true,
}

// AgentRenderer returns the Codex agent renderer.
func (a *Adapter) AgentRenderer() adept.AgentRenderer { return a.r }

// ValidateAgents computes drift for expected rendered agents. Codex's skill
// Validate is aggregator-shaped, so agents use the shared per-file helper.
func (a *Adapter) ValidateAgents(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	return common.ComputePerFileDrift(projectRoot, expected, a.f.ReadFile)
}

// ImportAgents walks .codex/agents/*.toml and reverse-maps each file into a
// canonical agent (developer_instructions becomes the body).
func (a *Adapter) ImportAgents(_ context.Context, projectRoot string) ([]adept.ImportedAgent, error) {
	base := filepath.Join(projectRoot, ".codex", "agents")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("codex import agents: read %s: %w", base, err)
	}
	out := make([]adept.ImportedAgent, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".toml") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".toml")
		path := filepath.Join(base, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("codex import agent %s: %w", id, err)
		}
		fields := map[string]any{}
		if err := toml.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("codex import agent %s: parse toml: %w", id, err)
		}
		agent := &adept.Agent{
			ID:          id,
			Description: common.StringField(fields, "description"),
			Mode:        adept.AgentModeSubagent,
			Model:       common.StringField(fields, "model"),
			Body:        common.StringField(fields, "developer_instructions") + "\n",
		}
		out = append(out, adept.ImportedAgent{
			Agent:      agent,
			SourcePath: path,
			Warnings:   common.ForeignKeyWarnings("codex", id, fields, codexAgentKnownKeys),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Agent.ID < out[j].Agent.ID })
	return out, nil
}

var _ adept.AgentSupport = (*Adapter)(nil)
