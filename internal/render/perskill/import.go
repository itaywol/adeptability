package perskill

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

// collectSidecars walks a harness skill directory and returns every file
// other than SKILL.md.
func collectSidecars(dir string) ([]adept.SkillFile, error) {
	var out []adept.SkillFile
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		name := filepath.Base(path)
		if strings.HasPrefix(name, ".") {
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
