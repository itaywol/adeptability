package canonical

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/itaywol/adeptability/pkg/adept"
)

// LoadAgentFile reads one canonical agent file (.adeptability/agents/<id>.md),
// parses its frontmatter + body, and validates the result. The filename
// (minus extension) is authoritative for identity, mirroring how skill
// directories name skills.
func LoadAgentFile(path string, v AgentValidator) (*adept.Agent, error) {
	if v == nil {
		return nil, fmt.Errorf("agent loader: %w: missing validator", adept.ErrAgentInvalid)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("agent loader: %w: %s", adept.ErrAgentNotFound, path)
		}
		return nil, fmt.Errorf("agent loader: read %s: %w", path, err)
	}
	a, _, err := ParseAgentFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("agent loader: %s: %w", path, err)
	}
	if a.ID == "" {
		a.ID = AgentIDFromPath(path)
	}
	if err := v.ValidateAgent(a); err != nil {
		return nil, fmt.Errorf("agent loader: %s: %w", path, err)
	}
	return a, nil
}

// AgentIDFromPath derives the agent id from a canonical agent file path: the
// base name with the .md extension stripped.
func AgentIDFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".md")
}
