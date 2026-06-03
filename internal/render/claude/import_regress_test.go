package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

// importRegressWriteSkill writes a SKILL.md for id under projectRoot.
func importRegressWriteSkill(t *testing.T, root, id, content string) {
	t.Helper()
	dir := filepath.Join(root, ".claude", "skills", id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, adept.SkillFileName), []byte(content), 0o644))
}

// TestImport_DisableModelInvocationInBodyDoesNotFlipManual guards against the
// regression where the literal `disable-model-invocation: true` appearing in
// the markdown body (e.g. documentation or a code fence) was wrongly treated as
// manual activation because the importer scanned the whole file.
func TestImport_DisableModelInvocationInBodyDoesNotFlipManual(t *testing.T) {
	root := t.TempDir()
	content := "---\nname: doc-skill\ndescription: documents the schema\n---\n\n" +
		"Set `disable-model-invocation: true` in frontmatter to disable.\n"
	importRegressWriteSkill(t, root, "doc-skill", content)

	out, err := (&Adapter{}).Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, adept.ActivationAgent, out[0].Skill.Activation)
}

// TestImport_DisableModelInvocationInFrontmatterFlipsManual confirms the real
// frontmatter field still maps to manual activation.
func TestImport_DisableModelInvocationInFrontmatterFlipsManual(t *testing.T) {
	root := t.TempDir()
	content := "---\nname: manual-skill\ndescription: a manual skill\ndisable-model-invocation: true\n---\n\n" +
		"Body.\n"
	importRegressWriteSkill(t, root, "manual-skill", content)

	out, err := (&Adapter{}).Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, adept.ActivationManual, out[0].Skill.Activation)
}

// TestImport_GlobActivationRoundTrips renders a glob-activated skill and imports
// the result, asserting that the " (matches: …)" hint the renderer appends to
// the description is stripped and Globs + activation=globs are recovered, so
// push -> import -> push is idempotent.
func TestImport_GlobActivationRoundTrips(t *testing.T) {
	root := t.TempDir()
	orig := &adept.Skill{
		ID:          "glob-rt",
		Description: "Go lint guardrails",
		Activation:  adept.ActivationGlobs,
		Globs:       []string{"cmd/**/*.go", "internal/**/*.go"},
		Body:        "Run go vet.",
	}
	rendered, err := New().Render(context.Background(), adept.RenderInput{Skill: orig, Harness: Spec()})
	require.NoError(t, err)
	importRegressWriteSkill(t, root, orig.ID, string(rendered.Bytes))

	out, err := (&Adapter{}).Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)
	got := out[0].Skill
	require.Equal(t, adept.ActivationGlobs, got.Activation)
	require.Equal(t, orig.Globs, got.Globs)
	require.Equal(t, "Go lint guardrails", got.Description, "the (matches: …) hint must be stripped")
}

// TestImport_DisableModelInvocationFalseStaysAgent confirms a `false` value is
// not flipped even when the body mentions the literal `true` string.
func TestImport_DisableModelInvocationFalseStaysAgent(t *testing.T) {
	root := t.TempDir()
	content := "---\nname: nf\ndescription: not flipped\ndisable-model-invocation: false\n---\n\n" +
		"docs: disable-model-invocation: true\n"
	importRegressWriteSkill(t, root, "nf", content)

	out, err := (&Adapter{}).Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, adept.ActivationAgent, out[0].Skill.Activation)
}
