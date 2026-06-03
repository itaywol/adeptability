package perskill_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/internal/render/perskill"
	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

// TestImport_DisableModelInvocationInBodyDoesNotFlipManual guards against the
// regression where the literal `disable-model-invocation: true` in the markdown
// body was treated as manual activation because the importer scanned the whole
// file rather than just the frontmatter. junieSpec/fakeWriter/osLinker are
// declared in renderer_test.go (same package).
func TestImport_DisableModelInvocationInBodyDoesNotFlipManual(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".junie", "skills", "doc-skill")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	content := "---\nname: doc-skill\ndescription: documents the schema\n---\n\n" +
		"Set `disable-model-invocation: true` in frontmatter to disable.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, adept.SkillFileName), []byte(content), 0o644))

	a := perskill.NewAdapter(junieSpec(), fakeWriter{}, osLinker{})
	out, err := a.Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, adept.ActivationAgent, out[0].Skill.Activation)
}

// TestImport_DisableModelInvocationInFrontmatterFlipsManual confirms the real
// frontmatter field still maps to manual activation.
func TestImport_DisableModelInvocationInFrontmatterFlipsManual(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".junie", "skills", "manual-skill")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	content := "---\nname: manual-skill\ndescription: a manual skill\ndisable-model-invocation: true\n---\n\nBody.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, adept.SkillFileName), []byte(content), 0o644))

	a := perskill.NewAdapter(junieSpec(), fakeWriter{}, osLinker{})
	out, err := a.Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, adept.ActivationManual, out[0].Skill.Activation)
}
