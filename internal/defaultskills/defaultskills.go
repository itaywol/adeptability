// Package defaultskills bundles a small, curated set of canonical skills
// directly into the adept binary. `adept init` seeds them into a fresh
// project (unless opted out) so every project starts knowing how to drive
// adept, how to author a good skill, and how to turn session lessons into
// new skills. The bytes are the source of truth; init writes them verbatim.
package defaultskills

import (
	"embed"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/itaywol/adeptability/pkg/adept"
)

//go:embed all:assets
var assets embed.FS

// SidecarFile is one non-SKILL.md resource shipped alongside a bundled skill
// (e.g. references/setup.md). RelPath is relative to the skill directory.
type SidecarFile struct {
	RelPath string
	Body    []byte
}

// Skill is one bundled canonical skill: its id (== directory name), the raw
// SKILL.md body, and any sidecar files (references/, scripts/, assets/).
type Skill struct {
	ID    string
	Body  []byte
	Files []SidecarFile
}

// All returns the bundled skills, sorted by id for deterministic seeding.
func All() []Skill {
	entries, err := fs.ReadDir(assets, "assets")
	if err != nil {
		// Embedded tree is compiled in; a read error means a build bug.
		panic("defaultskills: read embedded assets: " + err.Error())
	}
	out := make([]Skill, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := path.Join("assets", e.Name())
		body, err := assets.ReadFile(path.Join(skillDir, adept.SkillFileName))
		if err != nil {
			panic("defaultskills: read embedded SKILL.md: " + err.Error())
		}
		out = append(out, Skill{ID: e.Name(), Body: body, Files: sidecars(skillDir)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// sidecars walks a skill directory and returns every file except SKILL.md,
// with paths relative to the skill directory, sorted for deterministic order.
func sidecars(skillDir string) []SidecarFile {
	var files []SidecarFile
	err := fs.WalkDir(assets, skillDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || path.Base(p) == adept.SkillFileName {
			return nil
		}
		body, err := assets.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, SidecarFile{RelPath: strings.TrimPrefix(p, skillDir+"/"), Body: body})
		return nil
	})
	if err != nil {
		panic("defaultskills: walk sidecars: " + err.Error())
	}
	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	return files
}
