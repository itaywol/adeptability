package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/pkg/adept"
)

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
	var out []adept.ImportedSkill
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
		if strings.Contains(strings.ToLower(string(raw)), "disable-model-invocation: true") {
			skill.Activation = adept.ActivationManual
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
