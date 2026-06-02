package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSlug_Canonical(t *testing.T) {
	s, err := ParseSlug("vercel-labs/skills/find-skills")
	require.NoError(t, err)
	require.Equal(t, "vercel-labs", s.Owner)
	require.Equal(t, "skills", s.Repo)
	require.Equal(t, "find-skills", s.Skill)
	require.Empty(t, s.Ref)
	require.Equal(t, "vercel-labs/skills/find-skills", s.String())
}

func TestParseSlug_WithRef(t *testing.T) {
	s, err := ParseSlug("vercel-labs/skills#main/find-skills")
	require.NoError(t, err)
	require.Equal(t, "main", s.Ref)
	require.Equal(t, "vercel-labs/skills#main/find-skills", s.String())
}

func TestParseSlug_WithTag(t *testing.T) {
	s, err := ParseSlug("vercel-labs/skills@v1.4.0/find-skills")
	require.NoError(t, err)
	require.Equal(t, "v1.4.0", s.Ref)
}

func TestParseSlug_NestedSkillPath(t *testing.T) {
	s, err := ParseSlug("owner/repo/group/sub-skill")
	require.NoError(t, err)
	// All path components after owner/repo become the skill name to
	// support repos that nest skills inside subgroups.
	require.Equal(t, "group/sub-skill", s.Skill)
}

func TestParseSlug_RejectsCatalogDomains(t *testing.T) {
	// Catalog domain sources are surfaced as <domain>/<skill> by
	// skills.sh (only two segments). They must fail clearly so the user
	// is steered to library add instead.
	_, err := ParseSlug("skills.volces.com/find-skills/extra")
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-GitHub")
}

func TestParseSlug_RejectsTooShort(t *testing.T) {
	_, err := ParseSlug("owner/repo")
	require.Error(t, err)
}

func TestParseSlug_RejectsEmpty(t *testing.T) {
	_, err := ParseSlug("")
	require.Error(t, err)
}

func TestSlug_CandidateLayouts(t *testing.T) {
	s, err := ParseSlug("owner/repo/find-skills")
	require.NoError(t, err)
	got := s.CandidateLayouts()
	require.Equal(t, []string{"find-skills", "skills/find-skills"}, got)
}
