package copilot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/itaywol/adeptability/pkg/adept"
)

var copilotBeginRE = regexp.MustCompile(`<!--\s*adeptability:begin\s+id=([a-z0-9_][a-z0-9_-]{0,49})\s+version=(\d+)\s*-->`)

// Import walks .github/instructions/*.instructions.md. Each bucket file
// carries an `applyTo` frontmatter (canonical activation: globs OR always).
// If the file was produced by adept its `adeptability:begin/end` markers
// split it into per-skill sections; otherwise the whole file becomes one
// skill named after the bucket.
func (a *Adapter) Import(_ context.Context, projectRoot string) ([]adept.ImportedSkill, error) {
	base := filepath.Join(projectRoot, ".github", "instructions")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("copilot import: %w", err)
	}
	var out []adept.ImportedSkill
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".instructions.md") {
			continue
		}
		bucket := strings.TrimSuffix(name, ".instructions.md")
		full := filepath.Join(base, name)
		raw, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		applyTo, body, err := splitApplyTo(raw)
		if err != nil {
			return nil, fmt.Errorf("copilot import %s: %w", bucket, err)
		}
		globs := parseApplyTo(applyTo)
		alwaysApply := applyTo == "**" || (len(globs) == 1 && globs[0] == "**")

		markers := copilotBeginRE.FindAllStringSubmatchIndex(body, -1)
		if len(markers) == 0 {
			// One synthesized skill named after the bucket.
			skill := &adept.Skill{
				ID:          sanitizeID(bucket),
				Version:     1,
				Description: "Imported from Copilot bucket " + bucket,
				Body:        strings.TrimSpace(body),
			}
			if alwaysApply {
				skill.Activation = adept.ActivationAlways
			} else {
				skill.Activation = adept.ActivationGlobs
				skill.Globs = globs
			}
			out = append(out, adept.ImportedSkill{Skill: skill, SourcePath: full})
			continue
		}
		for _, m := range markers {
			id := body[m[2]:m[3]]
			afterMarker := body[m[1]:]
			endIdx := strings.Index(afterMarker, "<!-- adeptability:end")
			if endIdx < 0 {
				return nil, fmt.Errorf("copilot import: unterminated section for %q in %s", id, bucket)
			}
			section := strings.TrimSpace(afterMarker[:endIdx])
			desc := ""
			lines := strings.SplitN(section, "\n", 2)
			if len(lines) > 0 && strings.HasPrefix(lines[0], "## ") {
				desc = strings.TrimSpace(strings.TrimPrefix(lines[0], "## "))
				section = ""
				if len(lines) == 2 {
					section = strings.TrimSpace(lines[1])
				}
			}
			if desc == "" {
				desc = "Imported from Copilot section " + id
			}
			skill := &adept.Skill{
				ID:          id,
				Version:     1,
				Description: desc,
				Body:        section,
			}
			if alwaysApply {
				skill.Activation = adept.ActivationAlways
			} else {
				skill.Activation = adept.ActivationGlobs
				skill.Globs = globs
			}
			out = append(out, adept.ImportedSkill{Skill: skill, SourcePath: full})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Skill.ID < out[j].Skill.ID })
	return out, nil
}

func splitApplyTo(raw []byte) (string, string, error) {
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") {
		return "**", s, nil
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", fmt.Errorf("unterminated frontmatter")
	}
	front := rest[:end]
	body := strings.TrimPrefix(rest[end+4:], "\n")
	var fm struct {
		ApplyTo string `yaml:"applyTo"`
	}
	if err := yaml.Unmarshal([]byte(front), &fm); err != nil {
		return "", "", err
	}
	if fm.ApplyTo == "" {
		fm.ApplyTo = "**"
	}
	return fm.ApplyTo, body, nil
}

func parseApplyTo(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

var idSanitizeRE = regexp.MustCompile(`[^a-z0-9_-]+`)

// sanitizeID transforms a bucket filename into a legal canonical skill id.
func sanitizeID(s string) string {
	s = strings.ToLower(s)
	s = idSanitizeRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "bucket"
	}
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}
