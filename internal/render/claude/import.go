package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/pkg/adept"
)

// disableModelInvocationRE matches a `disable-model-invocation: true`
// frontmatter line, anchored to its own line (case-insensitive). It is applied
// only to the frontmatter region, never the markdown body, so prose or code
// fences that mention the key cannot flip activation to manual.
var disableModelInvocationRE = regexp.MustCompile(`(?im)^\s*disable-model-invocation:\s*true\s*$`)

// globHintSuffixRE matches the " (matches: a, b)" suffix buildDescription
// appends to a glob-activated skill's description. The capture group is the
// ", "-joined glob list, so Import can restore Globs + activation=globs and
// keep push -> import -> push idempotent.
var globHintSuffixRE = regexp.MustCompile(` \(matches: (.+)\)$`)

// Import walks .claude/skills/<id>/SKILL.md, parses each file's frontmatter,
// reverse-maps Claude's schema (`name`/`description`/`allowed-tools`/
// `disable-model-invocation`) back to canonical activation and metadata, and
// collects every sidecar living next to SKILL.md.
func (a *Adapter) Import(_ context.Context, projectRoot string) ([]adept.ImportedSkill, error) {
	base := filepath.Join(projectRoot, ".claude", "skills")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("claude import: read %s: %w", base, err)
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
			return nil, fmt.Errorf("claude import %s: %w", id, err)
		}
		skill, body, err := parser.ParseFrontmatter(raw)
		if err != nil {
			return nil, fmt.Errorf("claude import %s: %w", id, err)
		}
		// Reverse-map Claude conventions to canonical.
		skill.ID = id
		if skill.Activation == "" {
			skill.Activation = adept.ActivationAgent
		}
		// `disable-model-invocation: true` in our renderer encodes manual mode.
		// Inspect only the frontmatter region (anchored, per line) so a body
		// that documents the key does not flip activation to manual.
		if disableModelInvocationRE.MatchString(frontmatterRegion(raw)) {
			skill.Activation = adept.ActivationManual
		}
		// Reverse the glob hint buildDescription appends for glob-activated
		// skills: strip the trailing " (matches: …)", restore Globs, and set
		// activation=globs so a rendered glob skill round-trips instead of
		// degrading to agent with a polluted description.
		if skill.Activation != adept.ActivationManual {
			if m := globHintSuffixRE.FindStringSubmatch(skill.Description); m != nil {
				var globs []string
				for _, g := range strings.Split(m[1], ", ") {
					if g = strings.TrimSpace(g); g != "" {
						globs = append(globs, g)
					}
				}
				if len(globs) > 0 {
					skill.Description = strings.TrimSpace(strings.TrimSuffix(skill.Description, m[0]))
					skill.Globs = globs
					skill.Activation = adept.ActivationGlobs
				}
			}
		}
		skill.Body = body

		files, err := collectSidecars(filepath.Join(base, id))
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

// collectSidecars walks a harness skill directory and returns every file other
// than SKILL.md. Symlinks are dereferenced because the on-disk content is what
// the user expects to import.
func collectSidecars(dir string) ([]adept.SkillFile, error) {
	var out []adept.SkillFile
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == adept.SkillFileName {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read sidecar %s: %w", path, err)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, adept.SkillFile{
			RelPath: rel,
			Mode:    info.Mode().Perm(),
			Bytes:   data,
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}
