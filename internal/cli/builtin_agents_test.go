package cli

import (
	"testing"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/harness"
	"github.com/stretchr/testify/require"
)

func TestRegisterVercelAgents_AllRegister(t *testing.T) {
	reg := harness.NewRegistry()
	w := fsutil.NewWriter()
	l := fsutil.NewLinker(w)
	require.NoError(t, registerVercelAgents(reg, w, l))

	// Spot-check several agents the user is likely to hit.
	for _, id := range []string{"junie", "goose", "droid", "openhands", "windsurf", "openclaw", "tabnine-cli"} {
		ad, err := reg.Get(id)
		require.NoError(t, err, id)
		require.Equal(t, id, ad.Spec().ID)
	}
	require.GreaterOrEqual(t, len(reg.List()), len(vercelAgentTable))
}

func TestRegisterVercelAgents_SkipsExisting(t *testing.T) {
	// If an entry conflicts with a previously-registered specialized
	// adapter, registerVercelAgents must be a no-op for that ID.
	reg := harness.NewRegistry()
	w := fsutil.NewWriter()
	l := fsutil.NewLinker(w)
	require.NoError(t, registerBuiltinAdapters(reg, w, l))
	before, err := reg.Get("claude-code")
	require.NoError(t, err)
	beforePath := before.Spec().OutputPath

	require.NoError(t, registerVercelAgents(reg, w, l))
	after, err := reg.Get("claude-code")
	require.NoError(t, err)
	require.Equal(t, beforePath, after.Spec().OutputPath,
		"claude-code spec must not be replaced by table row")
}

func TestVercelAgentTable_AllSpecsValid(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range vercelAgentTable {
		require.NotEmpty(t, a.ID, "id required")
		require.NotEmpty(t, a.Output, "%s: output required", a.ID)
		require.Contains(t, a.Output, "{id}", "%s: output must contain {id}", a.ID)
		require.False(t, seen[a.ID], "duplicate id %s", a.ID)
		seen[a.ID] = true
	}
}
