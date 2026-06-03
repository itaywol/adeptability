package copilot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

// importRegressWrite writes a bucket instructions file under projectRoot.
func importRegressWrite(t *testing.T, root, bucket, content string) {
	t.Helper()
	dir := filepath.Join(root, ".github", "instructions")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, bucket+".instructions.md"), []byte(content), 0o644))
}

// TestImport_EndMarkerMatchedByID guards against a section with a missing :end
// absorbing the next section (which would then be imported twice).
func TestImport_EndMarkerMatchedByID(t *testing.T) {
	root := t.TempDir()
	content := "---\napplyTo: \"**\"\n---\n\n" +
		"<!-- adeptability:begin id=alpha hash=00000000 -->\n" +
		"## Alpha\n\nAlpha body.\n" +
		// alpha's end marker intentionally omitted
		"<!-- adeptability:begin id=beta hash=11111111 -->\n" +
		"## Beta\n\nBeta body.\n" +
		"<!-- adeptability:end id=beta -->\n"
	importRegressWrite(t, root, "always", content)

	out, err := (&Adapter{}).Import(context.Background(), root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unterminated section for \"alpha\"")
	require.Nil(t, out)
}

// TestImport_AdjacentSectionsNotMerged verifies two terminated sections import
// cleanly with no content bleed.
func TestImport_AdjacentSectionsNotMerged(t *testing.T) {
	root := t.TempDir()
	content := "---\napplyTo: \"**\"\n---\n\n" +
		"<!-- adeptability:begin id=alpha hash=00000000 -->\n" +
		"## Alpha\n\nAlpha body.\n" +
		"<!-- adeptability:end id=alpha -->\n" +
		"<!-- adeptability:begin id=beta hash=11111111 -->\n" +
		"## Beta\n\nBeta body.\n" +
		"<!-- adeptability:end id=beta -->\n"
	importRegressWrite(t, root, "always", content)

	out, err := (&Adapter{}).Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Equal(t, "alpha", out[0].Skill.ID)
	require.Equal(t, "Alpha body.", out[0].Skill.Body)
	require.NotContains(t, out[0].Skill.Body, "Beta")
	require.Equal(t, "beta", out[1].Skill.ID)
	require.Equal(t, "Beta body.", out[1].Skill.Body)
}

// TestImport_NormalizesCRLFForApplyTo guards against CRLF line endings causing
// the applyTo frontmatter to be dropped (turning a glob-scoped rule global and
// leaking raw frontmatter into the body).
func TestImport_NormalizesCRLFForApplyTo(t *testing.T) {
	root := t.TempDir()
	content := "---\r\napplyTo: \"src/**\"\r\n---\r\n\r\nSome instructions.\r\n"
	importRegressWrite(t, root, "mybucket", content)

	out, err := (&Adapter{}).Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 1)
	sk := out[0].Skill
	require.Equal(t, adept.ActivationGlobs, sk.Activation)
	require.Equal(t, []string{"src/**"}, sk.Globs)
	require.NotContains(t, sk.Body, "applyTo")
	require.NotContains(t, sk.Body, "---")
}

// TestSplitApplyTo_CRLF unit-tests the normalization directly: a CRLF file must
// yield the parsed applyTo (not the default "**") and a body free of frontmatter.
func TestSplitApplyTo_CRLF(t *testing.T) {
	applyTo, body, err := splitApplyTo([]byte("---\r\napplyTo: \"src/**\"\r\n---\r\nBody.\r\n"))
	require.NoError(t, err)
	require.Equal(t, "src/**", applyTo)
	require.Equal(t, "Body.\n", body)
	require.NotContains(t, body, "applyTo")
}
