package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/itaywol/adeptability/internal/library"
	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/pkg/adept"
)

// resolveSkills builds the set of skills visible to the project per the
// "Model B" union: project canonical ∪ all configured libraries, with the
// project shadowing libraries and first-library-wins on cross-library
// collisions. Library shadowing warnings are written to stderr via the
// logger.
//
// This is the single source of truth for "what skills should the
// orchestrator render to harnesses". The orchestrator itself stays
// oblivious to library plumbing — the CLI computes the union and passes
// it through SyncOptions.Skills / StatusOptions.Skills.
func resolveSkills(d *Deps, p project.Project) ([]*adept.Skill, error) {
	projSkills, err := p.ListSkills()
	if err != nil {
		return nil, fmt.Errorf("list project skills: %w", err)
	}
	multi, err := openMultiLibrary(d, p)
	if err != nil {
		return nil, err
	}
	if multi == nil || len(multi.Libraries()) == 0 {
		return projSkills, nil
	}

	// Index project ids so the library walk can skip shadowed entries.
	taken := map[string]struct{}{}
	for _, s := range projSkills {
		taken[s.ID] = struct{}{}
	}

	resolutions, err := multi.ListAll()
	if err != nil {
		return nil, err
	}
	out := append([]*adept.Skill{}, projSkills...)
	for _, r := range resolutions {
		if _, dup := taken[r.Skill.ID]; dup {
			d.Log.Debug("skill shadowed by project canonical", "id", r.Skill.ID, "library", r.Source)
			continue
		}
		if len(r.Shadowed) > 0 {
			d.Log.Warn("skill present in multiple libraries — first wins",
				"id", r.Skill.ID, "winner", r.Source, "shadowed", r.Shadowed)
		}
		out = append(out, r.Skill)
		taken[r.Skill.ID] = struct{}{}
	}
	return out, nil
}

// openMultiLibrary loads every configured library into a library.Multi.
// Returns nil when the project config carries no libraries — the caller
// treats that as "project-only mode" (single-library legacy behavior).
//
// Library directories that do not yet exist on disk are silently dropped
// so a stale config (someone deleted the local clone) does not break sync.
func openMultiLibrary(d *Deps, p project.Project) (library.Multi, error) {
	cfg, err := p.Config()
	if err != nil {
		return nil, err
	}
	if len(cfg.Libraries) == 0 {
		return nil, nil
	}
	libsRoot, err := d.ResolveLibrariesRoot()
	if err != nil {
		return nil, err
	}
	present := make([]adept.LibraryRef, 0, len(cfg.Libraries))
	for _, ref := range cfg.Libraries {
		if _, err := os.Stat(filepath.Join(libsRoot, ref.Name)); err == nil {
			present = append(present, ref)
		} else {
			d.Log.Warn("configured library missing on disk — skipped", "name", ref.Name, "remote", ref.Remote)
		}
	}
	if len(present) == 0 {
		return nil, nil
	}
	return library.NewMultiFromRefs(libsRoot, present, d.Parser, d.Hasher, d.Writer), nil
}
