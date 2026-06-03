package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

// TestImport_PerSkill_RejectsPathTraversalID is a regression test for the path
// traversal bug where a frontmatter `id:` of "../../../tmp/pwned" was carried
// verbatim into the canonical Skill, then joined into a filesystem path by
// project.writeSkillFiles without containment checks. The recovered id must be
// coerced/validated to satisfy adept.SkillIDPattern so it can never contain
// path separators or "..".
func TestImport_PerSkill_RejectsPathTraversalID(t *testing.T) {
	// No frontmatter.include => all keys (including "id") are accepted, which is
	// the configuration that exposed the bug.
	spec := Spec{
		ID:     "junie-traversal",
		Kind:   adept.KindPerSkill,
		Output: ".junie/guidelines/{id}.md",
	}
	a, err := NewSynthetic(spec)
	require.NoError(t, err)

	root := t.TempDir()
	path := filepath.Join(root, ".junie", "guidelines", "alpha.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path,
		[]byte("---\nid: ../../../../../tmp/escape\ndescription: hi\n---\nbody\n"),
		0o644))

	out, err := a.Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)

	got := out[0].Skill.ID
	pattern := skillIDValidRE
	require.True(t, pattern.MatchString(got),
		"recovered id %q must satisfy SkillIDPattern", got)
	require.NotContains(t, got, "..")
	require.NotContains(t, got, "/")
	require.NotContains(t, got, string(filepath.Separator))
}

// TestImport_PerSkill_RejectsTraversalFallbackToken guards the other ingress:
// when frontmatter carries no id, the fallback token comes from the captured
// filename/dir segment. Even a hostile token must be sanitized before it can
// become a skill directory. (Filesystem entry names cannot contain "/", but a
// leading ".." or stray characters must still be coerced.)
func TestImport_PerSkill_RejectsTraversalFallbackToken(t *testing.T) {
	spec := Spec{
		ID:     "junie-fallback",
		Kind:   adept.KindPerSkill,
		Output: ".junie/guidelines/{id}.md",
	}
	a, err := NewSynthetic(spec)
	require.NoError(t, err)

	root := t.TempDir()
	// Entry name "..weird" yields fallback token "..weird"; no frontmatter id.
	path := filepath.Join(root, ".junie", "guidelines", "..weird.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("just a body, no frontmatter\n"), 0o644))

	out, err := a.Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)

	got := out[0].Skill.ID
	require.True(t, skillIDValidRE.MatchString(got),
		"fallback id %q must satisfy SkillIDPattern", got)
	require.False(t, strings.HasPrefix(got, ".."))
}

// TestImport_PerSkill_ValidIDPreserved confirms the sanitizer is non-destructive
// for ids that already satisfy the pattern (no accidental rewriting).
func TestImport_PerSkill_ValidIDPreserved(t *testing.T) {
	spec := Spec{
		ID:     "junie-valid",
		Kind:   adept.KindPerSkill,
		Output: ".junie/guidelines/{id}.md",
	}
	a, err := NewSynthetic(spec)
	require.NoError(t, err)

	root := t.TempDir()
	path := filepath.Join(root, ".junie", "guidelines", "alpha.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path,
		[]byte("---\nid: my_skill-01\ndescription: hi\n---\nbody\n"), 0o644))

	out, err := a.Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "my_skill-01", out[0].Skill.ID)
}

// TestImport_PerSkill_ExplicitAgentActivationNotOverridden is a regression test
// for the activation round-trip bug: an explicitly-declared `activation: agent`
// that also carries `globs:` must remain agent, not be silently rewritten to
// globs.
func TestImport_PerSkill_ExplicitAgentActivationNotOverridden(t *testing.T) {
	spec := Spec{
		ID:     "junie-activation",
		Kind:   adept.KindPerSkill,
		Output: ".junie/guidelines/{id}.md",
	}
	a, err := NewSynthetic(spec)
	require.NoError(t, err)

	root := t.TempDir()
	path := filepath.Join(root, ".junie", "guidelines", "explicit.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path,
		[]byte("---\nactivation: agent\nglobs:\n  - '**/*.go'\ndescription: hi\n---\nbody\n"),
		0o644))

	out, err := a.Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, adept.ActivationAgent, out[0].Skill.Activation,
		"explicit activation:agent must not be overridden to globs")
	require.Equal(t, []string{"**/*.go"}, out[0].Skill.Globs)
}

// TestImport_PerSkill_GlobsInferActivationWhenAbsent confirms the inference path
// still works when `activation` is genuinely absent (globs imply globs mode).
func TestImport_PerSkill_GlobsInferActivationWhenAbsent(t *testing.T) {
	spec := Spec{
		ID:     "junie-infer",
		Kind:   adept.KindPerSkill,
		Output: ".junie/guidelines/{id}.md",
	}
	a, err := NewSynthetic(spec)
	require.NoError(t, err)

	root := t.TempDir()
	path := filepath.Join(root, ".junie", "guidelines", "infer.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path,
		[]byte("---\nglobs:\n  - '**/*.go'\ndescription: hi\n---\nbody\n"), 0o644))

	out, err := a.Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, adept.ActivationGlobs, out[0].Skill.Activation)
}
