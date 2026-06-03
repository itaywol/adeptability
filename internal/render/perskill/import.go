package perskill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/itaywol/adeptability/pkg/adept"
)

// disableModelInvocationRE matches a `disable-model-invocation: true`
// frontmatter line, anchored to its own line (case-insensitive). It is applied
// only to the frontmatter region, never the markdown body, so prose or code
// fences that mention the key cannot flip activation to manual.
var disableModelInvocationRE = regexp.MustCompile(`(?im)^\s*disable-model-invocation:\s*true\s*$`)

// Import walks <skills-container>/<id>/SKILL.md under projectRoot,
// reverse-maps the frontmatter back to canonical skills, and collects
// sidecars living next to each SKILL.md.
func (a *Adapter) Import(_ context.Context, projectRoot string) ([]adept.ImportedSkill, error) {
	container := skillsContainer(a.r.spec.OutputPath)
	if container == "" {
		return nil, nil
	}
	base := filepath.Join(projectRoot, container)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s import: read %s: %w", a.r.spec.ID, base, err)
	}
	parser := canonical.NewParser()
	out := make([]adept.ImportedSkill, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		skillPath := filepath.Join(base, id, adept.SkillFileName)
		raw, err := os.ReadFile(skillPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("%s import %s: %w", a.r.spec.ID, id, err)
		}
		skill, body, err := parser.ParseFrontmatter(raw)
		if err != nil {
			return nil, fmt.Errorf("%s import %s: %w", a.r.spec.ID, id, err)
		}
		skill.ID = id
		if skill.Activation == "" {
			skill.Activation = adept.ActivationAgent
		}
		// `disable-model-invocation: true` encodes manual mode. Inspect only
		// the frontmatter region (anchored, per line) so a body that documents
		// the key does not flip activation to manual.
		if disableModelInvocationRE.MatchString(frontmatterRegion(raw)) {
			skill.Activation = adept.ActivationManual
		}
		skill.Body = body

		files, err := common.CollectSidecars(filepath.Join(base, id))
		if err != nil {
			return nil, err
		}
		out = append(out, adept.ImportedSkill{
			Skill:      skill,
			Files:      files,
			SourcePath: skillPath,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Skill.ID < out[j].Skill.ID })
	return out, nil
}

// frontmatterRegion returns the YAML frontmatter block of a SKILL.md document
// (the text between the opening and closing `---` fences), CRLF-normalized. If
// the document has no recognizable frontmatter the empty string is returned so
// callers never accidentally scan the markdown body.
func frontmatterRegion(raw []byte) string {
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return ""
	}
	rest := s[len("---\n"):]
	if end := strings.Index(rest, "\n---"); end >= 0 {
		return rest[:end]
	}
	return ""
}
