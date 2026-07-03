package canonical

import (
	"bytes"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/itaywol/adeptability/pkg/adept"
)

// ParseAgentYAML parses agent frontmatter YAML into an *adept.Agent. Like the
// skill parser it accepts `name:` as an alias for `id:` so harness-native
// agent files import without editing; the filename remains authoritative at
// the loader layer.
func ParseAgentYAML(data []byte) (*adept.Agent, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("parse agent: %w: empty document", adept.ErrAgentInvalid)
	}
	a := &adept.Agent{}
	if err := yaml.Unmarshal(data, a); err != nil {
		return nil, fmt.Errorf("parse agent: %w: %w", adept.ErrAgentInvalid, err)
	}
	if a.ID == "" {
		var alias struct {
			Name string `yaml:"name"`
		}
		if err := yaml.Unmarshal(data, &alias); err == nil && alias.Name != "" {
			a.ID = alias.Name
		}
	}
	applyAgentDefaults(a)
	return a, nil
}

// ParseAgentFrontmatter parses YAML frontmatter from a canonical agent .md
// document and returns the Agent plus the system-prompt body that follows.
func ParseAgentFrontmatter(agentMD []byte) (*adept.Agent, string, error) {
	fmYAML, body, err := splitFrontmatter(agentMD, "agent file", adept.ErrAgentInvalid)
	if err != nil {
		return nil, "", err
	}
	a, err := ParseAgentYAML(fmYAML)
	if err != nil {
		return nil, "", err
	}
	a.Body = body
	return a, body, nil
}

// applyAgentDefaults fills fields the schema models as defaults but YAML
// leaves zero-valued.
func applyAgentDefaults(a *adept.Agent) {
	if a.Mode == "" {
		a.Mode = adept.AgentModeSubagent
	}
}

// agentKnownKeys are the frontmatter keys the canonical Agent struct decodes
// (plus the `name` alias). Kept next to the parser so the set cannot drift
// from the struct tags without a reviewer noticing.
var agentKnownKeys = map[string]bool{
	"id": true, "name": true, "description": true, "mode": true,
	"tools": true, "disallowed-tools": true, "model": true,
	"targets": true, "tags": true, "metadata": true, "harness": true,
}

// AgentUnknownFrontmatterKeys returns frontmatter keys that the canonical
// parser silently drops (yaml.Unmarshal ignores unknown fields, so the strict
// schema alone can never see them). A misspelled `tool:` would otherwise
// vanish — leaving the agent with an empty allowlist, i.e. every tool.
func AgentUnknownFrontmatterKeys(agentMD []byte) ([]string, error) {
	fmYAML, _, err := splitFrontmatter(agentMD, "agent file", adept.ErrAgentInvalid)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if err := yaml.Unmarshal(fmYAML, &fields); err != nil {
		return nil, fmt.Errorf("parse agent frontmatter: %w: %w", adept.ErrAgentInvalid, err)
	}
	var unknown []string
	for k := range fields {
		if !agentKnownKeys[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	return unknown, nil
}
