// Package canonical parses skill.yaml and SKILL.md frontmatter into the
// canonical *adept.Skill type and validates results against the embedded
// JSON Schema.
package canonical

import (
	"bytes"
	"fmt"

	"github.com/itaywol/adeptability/pkg/adept"
	"gopkg.in/yaml.v3"
)

// Parser parses skill metadata from raw bytes.
type Parser interface {
	// ParseSkillYAML parses a standalone skill.yaml file body.
	ParseSkillYAML(data []byte) (*adept.Skill, error)
	// ParseFrontmatter parses YAML frontmatter from a SKILL.md document.
	// Returns the parsed Skill and the markdown body that follows the
	// frontmatter block. If no frontmatter is present, returns
	// adept.ErrSkillInvalid wrapped.
	ParseFrontmatter(skillMD []byte) (*adept.Skill, string, error)
}

type parser struct{}

// NewParser returns a Parser implementation backed by gopkg.in/yaml.v3.
func NewParser() Parser {
	return &parser{}
}

func (p *parser) ParseSkillYAML(data []byte) (*adept.Skill, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("parse skill.yaml: %w: empty document", adept.ErrSkillInvalid)
	}
	s := &adept.Skill{}
	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse skill.yaml: %w: %w", adept.ErrSkillInvalid, err)
	}
	// vercel-labs/skills (and most harness-native SKILL.md) writes `name:`
	// where adept canonical writes `id:`. Accept either — directory name
	// remains authoritative at the loader layer, but we map name→id here
	// so a hand-authored or installed file does not fail validation just
	// because it follows the upstream agent-skills convention.
	if s.ID == "" {
		var alias struct {
			Name string `yaml:"name"`
		}
		if err := yaml.Unmarshal(data, &alias); err == nil && alias.Name != "" {
			s.ID = alias.Name
		}
	}
	applyDefaults(s)
	return s, nil
}

// frontmatterDelim is the literal sequence used to denote frontmatter bounds.
var frontmatterDelim = []byte("---")

func (p *parser) ParseFrontmatter(skillMD []byte) (*adept.Skill, string, error) {
	fmYAML, body, err := splitFrontmatter(skillMD, "SKILL.md", adept.ErrSkillInvalid)
	if err != nil {
		return nil, "", err
	}
	s, err := p.ParseSkillYAML(fmYAML)
	if err != nil {
		return nil, "", err
	}
	return s, body, nil
}

// splitFrontmatter separates a `---` fenced YAML frontmatter block from the
// markdown body that follows. label names the document kind in error messages
// ("SKILL.md", "agent file") and sentinel is the invalid-document sentinel to
// wrap. Shared by the skill and agent parsers so the fence grammar cannot
// drift between them.
func splitFrontmatter(data []byte, label string, sentinel error) (fmYAML []byte, body string, err error) {
	if len(data) == 0 {
		return nil, "", fmt.Errorf("parse %s: %w: empty document", label, sentinel)
	}
	// Normalize CRLF to LF before scanning so the parser is OS-agnostic.
	norm := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(norm, append(frontmatterDelim, '\n')) &&
		!bytes.Equal(bytes.TrimRight(norm[:min(len(norm), 3)], "\n"), frontmatterDelim) {
		return nil, "", fmt.Errorf("parse %s: %w: missing frontmatter", label, sentinel)
	}
	// Skip the leading "---\n" (3 bytes + newline).
	rest := norm[len(frontmatterDelim):]
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	} else {
		return nil, "", fmt.Errorf("parse %s: %w: malformed frontmatter opener", label, sentinel)
	}
	// Search for the closing "\n---\n" or "\n---" at EOF.
	closer := []byte("\n---\n")
	endIdx := bytes.Index(rest, closer)
	var bodyStart int
	if endIdx >= 0 {
		bodyStart = endIdx + len(closer)
	} else {
		// Allow EOF closer "\n---".
		if bytes.HasSuffix(rest, []byte("\n---")) {
			endIdx = len(rest) - len("\n---")
			bodyStart = len(rest)
		} else {
			return nil, "", fmt.Errorf("parse %s: %w: missing frontmatter terminator", label, sentinel)
		}
	}
	if bodyStart < len(rest) {
		body = string(rest[bodyStart:])
	}
	return rest[:endIdx], body, nil
}

// applyDefaults fills in fields that the schema models as defaults but YAML
// would otherwise leave zero-valued.
func applyDefaults(s *adept.Skill) {
	if s.Activation == "" {
		s.Activation = adept.ActivationAgent
	}
}
