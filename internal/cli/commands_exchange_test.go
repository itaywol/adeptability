package cli

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExchangeRecommendationFlow exercises the per-user recommendation toggle
// and that `exchange status` reflects it — the surface the expertise-exchange
// skill relies on. These commands need only the credential store (library
// root), not a running server.
func TestExchangeRecommendationFlow(t *testing.T) {
	d := testDeps(t, t.TempDir(), t.TempDir())
	cs, err := d.ExchangeCreds()
	require.NoError(t, err)
	require.False(t, cs.RecommendationDismissed())

	dismiss := newExchangeRecommendationCmd(d)
	dismiss.SetArgs([]string{"dismiss"})
	dismiss.SetOut(io.Discard)
	dismiss.SetErr(io.Discard)
	require.NoError(t, dismiss.Execute())
	require.True(t, cs.RecommendationDismissed())

	// status --json reports the dismissal without any network call.
	d.Flags.JSON = true
	var server string
	status := newExchangeSetupStatusCmd(d, &server)
	var buf bytes.Buffer
	status.SetOut(&buf)
	require.NoError(t, status.Execute())
	require.Contains(t, buf.String(), `"dismissed": true`)
	require.Contains(t, buf.String(), `"registered": false`)

	undismiss := newExchangeRecommendationCmd(d)
	undismiss.SetArgs([]string{"undismiss"})
	undismiss.SetOut(io.Discard)
	undismiss.SetErr(io.Discard)
	require.NoError(t, undismiss.Execute())
	require.False(t, cs.RecommendationDismissed())
}
