// Package library — multi-library aggregation.
//
// A project may pull skills from N libraries simultaneously (see
// `adept library add`). The aggregator resolves an id by walking the
// configured libraries in order and returning the first match. Subsequent
// hits are surfaced as Shadowed so the CLI can warn the user about
// cross-library collisions.
//
// The project's own canonical skills (project.Project) ALWAYS win over
// any library copy — that resolution is handled outside this package; the
// aggregator here is purely the library-side stack.
package library

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/pkg/adept"
)

// LibrariesDirName is the parent directory that holds every named library
// under the configured $ADEPT_LIBRARY root: $ADEPT_LIBRARY/libs/<name>/.
const LibrariesDirName = "libs"

// NamedLibrary pairs a Library with the name it was registered under.
type NamedLibrary struct {
	Name    string
	Library Library
}

// Multi aggregates several Libraries into one read-only view that
// resolves skill ids first-match-wins. Writes are routed back to the
// caller-selected NamedLibrary; this type intentionally does not expose
// AddSkill/RemoveSkill so that the caller must pick a specific library
// for any mutation.
type Multi interface {
	// Libraries returns the configured libraries in order.
	Libraries() []NamedLibrary
	// HasSkill is true when any configured library carries the id.
	HasSkill(id string) bool
	// Resolve returns the winning skill + the library it came from. When
	// other libraries also carry the id, their names appear in shadowed.
	Resolve(id string) (skill *adept.Skill, source string, shadowed []string, err error)
	// ListAll returns one Resolution per unique skill id across every
	// library. Shadowed entries are recorded inline so the caller can warn.
	ListAll() ([]Resolution, error)
}

// Resolution describes one skill in the aggregated view: which library
// won, which (if any) were shadowed, and the skill metadata itself.
type Resolution struct {
	Skill    *adept.Skill
	Source   string
	Shadowed []string
}

type multi struct {
	libs []NamedLibrary
}

// NewMulti constructs a Multi from an ordered slice of named libraries.
// The order is preserved and drives first-match-wins resolution.
func NewMulti(libs []NamedLibrary) Multi {
	return &multi{libs: libs}
}

// NewMultiFromRefs constructs Library instances rooted under
// <libsRoot>/<name>/ for each LibraryRef and returns a Multi.
// libsRoot is typically $ADEPT_LIBRARY/libs.
func NewMultiFromRefs(libsRoot string, refs []adept.LibraryRef, parser canonical.Parser, hasher hash.Hasher, w fsutil.Writer) Multi {
	named := make([]NamedLibrary, 0, len(refs))
	for _, r := range refs {
		root := filepath.Join(libsRoot, r.Name)
		named = append(named, NamedLibrary{
			Name:    r.Name,
			Library: New(root, parser, hasher, w),
		})
	}
	return NewMulti(named)
}

func (m *multi) Libraries() []NamedLibrary { return m.libs }

func (m *multi) HasSkill(id string) bool {
	for _, n := range m.libs {
		if n.Library.HasSkill(id) {
			return true
		}
	}
	return false
}

func (m *multi) Resolve(id string) (*adept.Skill, string, []string, error) {
	var winner *adept.Skill
	var winnerName string
	var shadowed []string
	for _, n := range m.libs {
		if !n.Library.HasSkill(id) {
			continue
		}
		s, err := n.Library.GetSkill(id)
		if err != nil {
			return nil, "", nil, fmt.Errorf("library %s: %w", n.Name, err)
		}
		if winner == nil {
			winner = s
			winnerName = n.Name
			continue
		}
		shadowed = append(shadowed, n.Name)
	}
	if winner == nil {
		return nil, "", nil, fmt.Errorf("library lookup %q: %w", id, adept.ErrSkillNotFound)
	}
	return winner, winnerName, shadowed, nil
}

func (m *multi) ListAll() ([]Resolution, error) {
	// Build the unique id set in stable order.
	seen := map[string]struct{}{}
	for _, n := range m.libs {
		skills, err := n.Library.ListSkills()
		if err != nil {
			return nil, fmt.Errorf("library %s: list: %w", n.Name, err)
		}
		for _, s := range skills {
			seen[s.ID] = struct{}{}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]Resolution, 0, len(ids))
	for _, id := range ids {
		skill, source, shadowed, err := m.Resolve(id)
		if err != nil {
			return nil, err
		}
		out = append(out, Resolution{Skill: skill, Source: source, Shadowed: shadowed})
	}
	return out, nil
}
