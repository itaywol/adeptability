package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Agent store methods. Agents are single canonical files under
// <root>/.adeptability/agents/<id>.md in BOTH layouts — unlike skills, a
// library project has no published/private split for agents (v1 scope cut:
// agents are rendered locally, never published to consumers).
//
// Reads parse but do not schema-validate, mirroring the skill accessors:
// validation happens at the CLI and import boundaries.

// AgentsDir returns the canonical agents directory.
func (p *project) AgentsDir() string {
	return filepath.Join(p.BaseDir(), adept.AgentsDirName)
}

// AgentPath returns the canonical file path for an agent id.
func (p *project) AgentPath(id string) string {
	return filepath.Join(p.AgentsDir(), id+".md")
}

// HasAgent reports whether the canonical agent file exists.
func (p *project) HasAgent(id string) bool {
	if !skillIDRE.MatchString(id) {
		return false
	}
	_, err := os.Stat(p.AgentPath(id))
	return err == nil
}

// GetAgent reads and parses one canonical agent. The filename is
// authoritative for identity, overriding any drifted in-file id.
func (p *project) GetAgent(id string) (*adept.Agent, error) {
	if !skillIDRE.MatchString(id) {
		return nil, fmt.Errorf("project get agent: %w: id %q does not match %s", adept.ErrAgentInvalid, id, adept.AgentIDPattern)
	}
	data, err := os.ReadFile(p.AgentPath(id))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("project get agent %q: %w", id, adept.ErrAgentNotFound)
		}
		return nil, fmt.Errorf("project get agent %q: read: %w", id, err)
	}
	a, _, err := canonical.ParseAgentFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("project get agent %q: %w", id, err)
	}
	a.ID = id
	return a, nil
}

// ListAgents returns every canonical agent, sorted by id. A missing agents
// directory is an empty project, not an error.
func (p *project) ListAgents() ([]*adept.Agent, error) {
	entries, err := os.ReadDir(p.AgentsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("project list agents: %w", err)
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		if !skillIDRE.MatchString(id) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*adept.Agent, 0, len(ids))
	for _, id := range ids {
		a, err := p.GetAgent(id)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// InstallAgent writes the canonical agent file. No base snapshot: agents have
// no 3-way merge in v1 (sync overwrites harness files, drift warns).
func (p *project) InstallAgent(a *adept.Agent) error {
	if a == nil {
		return fmt.Errorf("project install agent: %w: nil agent", adept.ErrAgentInvalid)
	}
	if !skillIDRE.MatchString(a.ID) {
		return fmt.Errorf("project install agent: %w: id %q does not match %s", adept.ErrAgentInvalid, a.ID, adept.AgentIDPattern)
	}
	body, err := canonical.RenderCanonicalAgent(a)
	if err != nil {
		return fmt.Errorf("project install agent %q: %w", a.ID, err)
	}
	if err := p.writer.AtomicWrite(p.AgentPath(a.ID), body, 0o644); err != nil {
		return fmt.Errorf("project install agent %q: %w", a.ID, err)
	}
	return nil
}

// UninstallAgent removes the canonical agent file. Returns
// adept.ErrAgentNotFound when id is not installed.
func (p *project) UninstallAgent(id string) error {
	if !skillIDRE.MatchString(id) {
		return fmt.Errorf("project uninstall agent: %w: id %q does not match %s", adept.ErrAgentInvalid, id, adept.AgentIDPattern)
	}
	if !p.HasAgent(id) {
		return fmt.Errorf("project uninstall agent %q: %w", id, adept.ErrAgentNotFound)
	}
	if err := os.Remove(p.AgentPath(id)); err != nil {
		return fmt.Errorf("project uninstall agent %q: %w", id, err)
	}
	return nil
}
