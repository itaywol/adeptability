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

	out := make([]adept.ImportedSkill, 0, len(markers))
	for _, m := range markers {
		// m: [fullStart, fullEnd, idStart, idEnd, hashStart, hashEnd]
		bodyStart := m[1]
		id := s[m[2]:m[3]]
		// Find the end marker whose captured id equals this section's id.
		// Matching by id (not by first occurrence) prevents a section with a
		// missing :end from greedily absorbing the next section's content and
		// causing that section to be imported twice.
		endOffset := matchingEndOffset(s[bodyStart:], id)
		if endOffset < 0 {
			return nil, fmt.Errorf("codex import: unterminated section for %q", id)
		}
		body := strings.TrimSpace(s[bodyStart : bodyStart+endOffset])
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

// matchingEndOffset returns the byte offset within s of the first
// `adeptability:end` marker whose captured id equals id, or -1 if none.
// Matching by id (rather than the first end marker found) ensures a section
// whose own :end was deleted does not silently swallow following sections.
func matchingEndOffset(s, id string) int {
	for _, em := range endRE.FindAllStringSubmatchIndex(s, -1) {
		// em: [fullStart, fullEnd, idStart, idEnd]
		if s[em[2]:em[3]] == id {
			return em[0]
		}
	}
	return -1
}
