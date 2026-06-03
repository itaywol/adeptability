package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestImport_EndMarkerMatchedByID guards against the regression where a section
// whose own :end marker was deleted greedily absorbed the following section's
// begin marker + body, and that following section was then imported a second
// time. The first section here (alpha) is missing its :end; beta's is present.
func TestImport_EndMarkerMatchedByID(t *testing.T) {
	root := t.TempDir()
	agents := "<!-- adeptability:begin id=alpha hash=00000000 -->\n" +
		"## Alpha\n\nAlpha body.\n" +
		// alpha's <!-- adeptability:end id=alpha --> is intentionally absent.
		"<!-- adeptability:begin id=beta hash=11111111 -->\n" +
		"## Beta\n\nBeta body.\n" +
		"<!-- adeptability:end id=beta -->\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agents), 0o644))

	// Import does not depend on Adapter's unexported fields.
	out, err := (&Adapter{}).Import(context.Background(), root)
	// alpha has no matching end marker, so import must report it as unterminated
	// rather than swallowing beta and emitting beta twice.
	require.Error(t, err)
	require.Contains(t, err.Error(), "unterminated section for \"alpha\"")
	require.Nil(t, out)
}

// TestImport_AdjacentSectionsNotMerged verifies the happy path: two fully
// terminated, adjacent sections import as two distinct skills with no content
// bleed between them.
func TestImport_AdjacentSectionsNotMerged(t *testing.T) {
	root := t.TempDir()
	agents := "<!-- adeptability:begin id=alpha hash=00000000 -->\n" +
		"## Alpha\n\nAlpha body.\n" +
		"<!-- adeptability:end id=alpha -->\n" +
		"<!-- adeptability:begin id=beta hash=11111111 -->\n" +
		"## Beta\n\nBeta body.\n" +
		"<!-- adeptability:end id=beta -->\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(agents), 0o644))

	out, err := (&Adapter{}).Import(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Equal(t, "alpha", out[0].Skill.ID)
	require.Equal(t, "Alpha body.", out[0].Skill.Body)
	require.NotContains(t, out[0].Skill.Body, "beta")
	require.NotContains(t, out[0].Skill.Body, "Beta")
	require.Equal(t, "beta", out[1].Skill.ID)
	require.Equal(t, "Beta body.", out[1].Skill.Body)
}
