package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression: the shadow warning must not double-prefix "library:".
// row.Source already carries the "library:" prefix, so the message must
// read "shadowed by library:<winner>" — not "library:library:<winner>".
func TestSkillList_ShadowWarningNoDoublePrefix_Regress(t *testing.T) {
	r := &skillListRenderable{Rows: []skillRow{
		{
			ID:          "find-skills",
			Source:      "library:default",
			Description: "desc",
			Shadowed:    []string{"library:secondary"},
		},
	}}
	var buf bytes.Buffer
	require.NoError(t, r.Plain(&buf))
	out := buf.String()
	require.NotContains(t, out, "library:library:", "shadow warning must not double-prefix")
	require.Contains(t, out, "shadowed by library:default")
	// The "also in" list is preserved.
	require.Contains(t, out, "also in: library:secondary")
	require.True(t, strings.Contains(out, "warn:"))
}
