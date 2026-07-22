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

	// Library layout adds a private dev-canonical (<root>/.adeptability/skills)
	// that renders to local harnesses but is never published. Published skills
	// shadow private ones on id collision. Nil in the consumer layout.
	privSkills, err := p.ListPrivateSkills()
	if err != nil {
		return nil, fmt.Errorf("list private skills: %w", err)
	}
	taken := map[string]struct{}{}
	for _, s := range projSkills {
		taken[s.ID] = struct{}{}
	}
	out := append([]*adept.Skill{}, projSkills...)
	for _, s := range privSkills {
		if _, dup := taken[s.ID]; dup {
			d.Log.Debug("private skill shadowed by published canonical", "id", s.ID)
			continue
		}
		out = append(out, s)
		taken[s.ID] = struct{}{}
	}

	multi, err := openMultiLibrary(d, p)
	if err != nil {
		return nil, err
	}
	if multi == nil || len(multi.Libraries()) == 0 {
		return out, nil
	}

	resolutions, err := multi.ListAll()
	if err != nil {
		return nil, err
	}
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
// Each library is resolved scope-locally first (<scope>/libs/<name>) and
// falls back to the machine store (~/.adeptability/libs/<name>), so global
// clones stay resolvable from a project scope. Library directories that do
// not exist in either location are silently dropped so a stale config
// (someone deleted the local clone) does not break sync.
func openMultiLibrary(d *Deps, p project.Project) (library.Multi, error) {
	cfg, err := p.Config()
	if err != nil {
		return nil, err
	}
	if len(cfg.Libraries) == 0 {
		return nil, nil
	}
	named := make([]library.NamedLibrary, 0, len(cfg.Libraries))
	for _, ref := range cfg.Libraries {
		dir, src := resolveLibDirSource(d, p, ref.Name)
		switch src {
		case libMissing:
			d.Log.Warn("configured library missing on disk — skipped", "name", ref.Name, "remote", ref.Remote)
			continue
		case libFallback:
			d.Log.Warn("library resolved from machine store — run `adept migrate` to localize",
				"name", ref.Name, "path", dir)
		}
		named = append(named, library.NamedLibrary{
			Name:    ref.Name,
			Library: library.New(dir, d.Parser, d.Hasher, d.Writer),
		})
	}
	if len(named) == 0 {
		return nil, nil
	}
	return library.NewMulti(named), nil
}

// dirExists reports whether path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// libResolveSource classifies how resolveLibDirSource located a library.
type libResolveSource int

const (
	// libMissing: the library exists in neither the scope-local clone root
	// nor the machine store.
	libMissing libResolveSource = iota
	// libLocal: resolved from the scope-local clone (<scope>/libs/<name>).
	// In global scope the scope-local root *is* the machine store, so a
	// resolved global library is always local — never fallback.
	libLocal
	// libFallback: resolved from the machine store because no scope-local
	// clone exists (project scope only).
	libFallback
)

// resolveLibDir returns the on-disk directory for a configured library and
// whether it exists. It prefers the scope-local clone (<scope>/libs/<name>)
// and falls back to the machine store (~/.adeptability/libs/<name>). When the
// library is absent from both, the scope-local path is returned so callers can
// surface the location a fresh `library add` would clone into.
func resolveLibDir(d *Deps, p project.Project, name string) (string, bool) {
	dir, src := resolveLibDirSource(d, p, name)
	return dir, src != libMissing
}

// resolveLibDirSource is resolveLibDir with the resolution source exposed, so
// callers can distinguish a scope-local hit from a machine-store fallback (and
// emit the migrate hint on the latter). The scoped path is returned when the
// library is missing from both locations.
func resolveLibDirSource(d *Deps, p project.Project, name string) (string, libResolveSource) {
	scoped := filepath.Join(d.LibsRootFor(p), name)
	if _, err := os.Stat(scoped); err == nil {
		return scoped, libLocal
	}
	if machineRoot, err := d.ResolveLibrariesRoot(); err == nil {
		fallback := filepath.Join(machineRoot, name)
		// In global scope scoped == fallback (both under the machine store),
		// so a distinct fallback only ever occurs in project scope — that
		// guard keeps the migrate hint from firing globally.
		if fallback != scoped {
			if _, err := os.Stat(fallback); err == nil {
				return fallback, libFallback
			}
		}
	}
	return scoped, libMissing
}
