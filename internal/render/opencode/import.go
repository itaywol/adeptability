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

// Import walks .opencode/skill/<id>/SKILL.md. OpenCode emits plain markdown
// with no required frontmatter, so we treat the first `# <id>` heading and
// optional one-line description as the skill metadata; everything else is body.
func (a *Adapter) Import(_ context.Context, projectRoot string) ([]adept.ImportedSkill, error) {
	base := filepath.Join(projectRoot, ".opencode", "skill")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opencode import: %w", err)
	}
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
			return nil, err
		}
		desc, body := splitOpenCodeMarkdown(string(raw), id)
		files, err := common.CollectSidecars(filepath.Join(base, id))
		if err != nil {
			return nil, err
		}
		out = append(out, adept.ImportedSkill{
			Skill: &adept.Skill{
				ID:          id,
				Description: desc,
				Activation:  adept.ActivationAgent,
				Body:        body,
			},
			Files:      files,
			SourcePath: skillPath,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Skill.ID < out[j].Skill.ID })
	return out, nil
}

// splitOpenCodeMarkdown extracts (description, body) from an OpenCode-style
// document where the first line is `# <id>` and the next non-empty line is
// the description.
func splitOpenCodeMarkdown(s, id string) (string, string) {
	lines := strings.Split(s, "\n")
	desc := ""
	bodyStart := 0
	if len(lines) > 0 && strings.HasPrefix(lines[0], "# ") {
		bodyStart = 1
	}
	for i := bodyStart; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			bodyStart = i + 1
			continue
		}
		desc = strings.TrimSpace(lines[i])
		bodyStart = i + 1
		break
	}
	// Skip any blank lines between description and body.
	for bodyStart < len(lines) && strings.TrimSpace(lines[bodyStart]) == "" {
		bodyStart++
	}
	if desc == "" {
		desc = "Imported from OpenCode " + id
	}
	return desc, strings.Join(lines[bodyStart:], "\n")
}
