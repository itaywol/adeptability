package cursor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/itaywol/adeptability/pkg/adept"
)

// Import walks .cursor/rules/*.mdc, parses Cursor's frontmatter
// (`description`/`globs`/`alwaysApply`), and reverse-maps it to canonical
// activation modes. Cursor is single-file so no sidecars to import.
func (a *Adapter) Import(_ context.Context, projectRoot string) ([]adept.ImportedSkill, error) {
	base := filepath.Join(projectRoot, ".cursor", "rules")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cursor import: %w", err)
	}
	var out []adept.ImportedSkill
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".mdc") || e.IsDir() {
			continue
		}
		id := strings.TrimSuffix(name, ".mdc")
		fullPath := filepath.Join(base, name)
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, err
		}
		front, body, err := splitFrontmatter(raw)
		if err != nil {
			return nil, fmt.Errorf("cursor import %s: %w", id, err)
		}
		var fm cursorFrontmatter
		if len(front) > 0 {
			if err := yaml.Unmarshal(front, &fm); err != nil {
				return nil, fmt.Errorf("cursor import %s: parse frontmatter: %w", id, err)
			}
		}
		skill := &adept.Skill{
			ID:          id,
			Description: fm.Description,
			Body:        body,
		}
		switch {
		case fm.AlwaysApply:
			skill.Activation = adept.ActivationAlways
		case len(fm.Globs) > 0:
			skill.Activation = adept.ActivationGlobs
			skill.Globs = fm.Globs
		default:
			skill.Activation = adept.ActivationAgent
		}
		if skill.Description == "" {
			skill.Description = "Imported from Cursor " + id
		}
		out = append(out, adept.ImportedSkill{
			Skill:      skill,
			SourcePath: fullPath,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Skill.ID < out[j].Skill.ID })
	return out, nil
}

type cursorFrontmatter struct {
	Description string   `yaml:"description"`
	Globs       []string `yaml:"globs"`
	AlwaysApply bool     `yaml:"alwaysApply"`
}

// splitFrontmatter pulls the YAML between leading `---\n` and `\n---\n`. If
// there's no frontmatter, returns (nil, raw, nil).
func splitFrontmatter(raw []byte) ([]byte, string, error) {
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") {
		return nil, s, nil
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, "", fmt.Errorf("unterminated frontmatter")
	}
	front := rest[:end]
	body := strings.TrimPrefix(rest[end+4:], "\n")
	return []byte(front), body, nil
}
