package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/pkg/adept"
)

var (
	beginRE = regexp.MustCompile(`<!--\s*adeptability:begin\s+id=([a-z0-9_][a-z0-9_-]{0,49})\s+hash=([a-f0-9]{8})\s*-->`)
	endRE   = regexp.MustCompile(`<!--\s*adeptability:end\s+id=([a-z0-9_][a-z0-9_-]{0,49})\s*-->`)
)

// Import parses <project>/AGENTS.md. If the file was produced by adept the
// `adeptability:begin/end` markers split it into one canonical skill per
// section. If no markers are present we synthesize a single skill named
// `agents` with the entire file as body — that lets users adopt a
// hand-written AGENTS.md into adept's canonical layout.
func (a *Adapter) Import(_ context.Context, projectRoot string) ([]adept.ImportedSkill, error) {
	path := filepath.Join(projectRoot, "AGENTS.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("codex import: %w", err)
	}
	s := string(raw)
	markers := beginRE.FindAllStringSubmatchIndex(s, -1)
	if len(markers) == 0 {
		return []adept.ImportedSkill{
			{
				Skill: &adept.Skill{
					ID:          "agents",
					Description: "Imported from AGENTS.md",
					Activation:  adept.ActivationAlways,
					Body:        s,
				},
				SourcePath: path,
			},
		}, nil
	}

	var out []adept.ImportedSkill
	for _, m := range markers {
		// m: [fullStart, fullEnd, idStart, idEnd, hashStart, hashEnd]
		bodyStart := m[1]
		id := s[m[2]:m[3]]
		// Find the matching end marker for this id.
		endMatch := endRE.FindStringIndex(s[bodyStart:])
		if endMatch == nil {
			return nil, fmt.Errorf("codex import: unterminated section for %q", id)
		}
		body := strings.TrimSpace(s[bodyStart : bodyStart+endMatch[0]])
		// The renderer prepends `## <description>` to each section. Try to
		// recover the description from the first non-empty line.
		desc := ""
		lines := strings.SplitN(body, "\n", 2)
		if len(lines) > 0 && strings.HasPrefix(lines[0], "## ") {
			desc = strings.TrimSpace(strings.TrimPrefix(lines[0], "## "))
			if len(lines) == 2 {
				body = strings.TrimSpace(lines[1])
			} else {
				body = ""
			}
		}
		if desc == "" {
			desc = "Imported from Codex section " + id
		}
		out = append(out, adept.ImportedSkill{
			Skill: &adept.Skill{
				ID:          id,
				Description: desc,
				Activation:  adept.ActivationAgent,
				Body:        body,
			},
			SourcePath: path,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Skill.ID < out[j].Skill.ID })
	return out, nil
}
