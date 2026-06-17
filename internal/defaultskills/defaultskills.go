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
)

//go:embed assets/*/SKILL.md
var assets embed.FS

// Skill is one bundled canonical skill: its id (== directory name) and the
// raw SKILL.md body to write to disk.
type Skill struct {
	ID   string
	Body []byte
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
		body, err := assets.ReadFile(path.Join("assets", e.Name(), "SKILL.md"))
		if err != nil {
			panic("defaultskills: read embedded SKILL.md: " + err.Error())
		}
		out = append(out, Skill{ID: e.Name(), Body: body})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
