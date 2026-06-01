package clitest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Skipped by default — the build-and-run integration tests live in cmd/adept
// once orchestrator + commands land. This sanity check ensures the helpers
// compile and the Result struct round-trips an exit code.
func TestRunnerNoop(t *testing.T) {
	t.Parallel()
	r := &Result{ExitCode: 7, Stdout: "ok", Stderr: ""}
	require.Equal(t, 7, r.ExitCode)
	require.Equal(t, "ok", r.Stdout)
}
