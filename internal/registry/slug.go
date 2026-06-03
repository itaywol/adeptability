// Package registry is the shared layer above the per-source clients.
// Today it just holds slug parsing; later phases (LLM checks) will live
// alongside since they share the same upstream resolution contract.
package registry

import (
	"fmt"
	"strings"
)

// Slug identifies one installable upstream skill. The canonical wire
// form is "<owner>/<repo>/<skill>" matching skills.sh / npx skills add.
// Tags are encoded as "<owner>/<repo>#<ref>/<skill>" — uncommon but
// keeps Pin reproducible when a project tracks a release tag.
type Slug struct {
	Owner string
	Repo  string
	Ref   string // optional; "" means "default branch"
	Skill string
}

// ParseSlug normalizes a user-supplied string into a Slug. Accepts:
//
//   - vercel-labs/skills/find-skills            (default branch)
//   - vercel-labs/skills#main/find-skills       (explicit ref)
//   - vercel-labs/skills@1.4.0/find-skills      (tag/version alias)
//
// Non-GitHub sources (catalog domains skills.sh sometimes references)
// return a typed error so the CLI can suggest `library add` or `git
// clone` instead.
func ParseSlug(raw string) (Slug, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Slug{}, fmt.Errorf("empty slug")
	}
	parts := strings.Split(raw, "/")
	if len(parts) < 3 {
		return Slug{}, fmt.Errorf("slug %q: expected <owner>/<repo>/<skill>", raw)
	}
	owner := parts[0]
	if strings.Contains(owner, ".") {
		// Catalog-domain sources (e.g. skills.volces.com) — we cannot
		// fetch content from them yet. The dot check is scoped to the
		// owner segment so version tags like `repo@v1.4.0` keep working.
		return Slug{}, fmt.Errorf("slug %q: non-GitHub sources not supported yet (use `adept library add` for direct git URLs)", raw)
	}
	repoTail := parts[1]
	skill := strings.Join(parts[2:], "/")
	repo := repoTail
	ref := ""
	for _, sep := range []string{"#", "@"} {
		if before, after, found := strings.Cut(repoTail, sep); found {
			repo, ref = before, after
		}
	}
	if owner == "" || repo == "" || skill == "" {
		return Slug{}, fmt.Errorf("slug %q: owner, repo, and skill must all be non-empty", raw)
	}
	return Slug{Owner: owner, Repo: repo, Ref: ref, Skill: skill}, nil
}

// String renders the slug back in canonical wire form.
func (s Slug) String() string {
	tail := s.Repo
	if s.Ref != "" {
		tail = s.Repo + "#" + s.Ref
	}
	return s.Owner + "/" + tail + "/" + s.Skill
}

// CandidateLayouts returns the paths inside the repo to try when
// extracting the skill. Order matters: first match wins.
func (s Slug) CandidateLayouts() []string {
	return []string{
		s.Skill,             // <repo>/<skill>/SKILL.md
		"skills/" + s.Skill, // <repo>/skills/<skill>/SKILL.md
	}
}
