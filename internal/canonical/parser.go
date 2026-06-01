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
		return nil, fmt.Errorf("parse skill.yaml: %w: %v", adept.ErrSkillInvalid, err)
	}
	applyDefaults(s)
	return s, nil
}

// frontmatterDelim is the literal sequence used to denote frontmatter bounds.
var frontmatterDelim = []byte("---")

func (p *parser) ParseFrontmatter(skillMD []byte) (*adept.Skill, string, error) {
	if len(skillMD) == 0 {
		return nil, "", fmt.Errorf("parse SKILL.md: %w: empty document", adept.ErrSkillInvalid)
	}
	// Normalize CRLF to LF before scanning so the parser is OS-agnostic.
	norm := bytes.ReplaceAll(skillMD, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(norm, append(frontmatterDelim, '\n')) &&
		!bytes.Equal(bytes.TrimRight(norm[:min(len(norm), 3)], "\n"), frontmatterDelim) {
		return nil, "", fmt.Errorf("parse SKILL.md: %w: missing frontmatter", adept.ErrSkillInvalid)
	}
	// Skip the leading "---\n" (3 bytes + newline).
	rest := norm[len(frontmatterDelim):]
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	} else {
		return nil, "", fmt.Errorf("parse SKILL.md: %w: malformed frontmatter opener", adept.ErrSkillInvalid)
	}
	// Search for the closing "\n---\n" or "\n---" at EOF.
	closer := []byte("\n---\n")
	endIdx := bytes.Index(rest, closer)
	bodyStart := -1
	if endIdx >= 0 {
		bodyStart = endIdx + len(closer)
	} else {
		// Allow EOF closer "\n---".
		if bytes.HasSuffix(rest, []byte("\n---")) {
			endIdx = len(rest) - len("\n---")
			bodyStart = len(rest)
		} else {
			return nil, "", fmt.Errorf("parse SKILL.md: %w: missing frontmatter terminator", adept.ErrSkillInvalid)
		}
	}
	fmYAML := rest[:endIdx]
	s, err := p.ParseSkillYAML(fmYAML)
	if err != nil {
		return nil, "", err
	}
	body := ""
	if bodyStart < len(rest) {
		body = string(rest[bodyStart:])
	}
	return s, body, nil
}

// applyDefaults fills in fields that the schema models as defaults but YAML
// would otherwise leave zero-valued.
func applyDefaults(s *adept.Skill) {
	if s.Activation == "" {
		s.Activation = adept.ActivationAgent
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
