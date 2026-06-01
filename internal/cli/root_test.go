package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootHasAllCommands(t *testing.T) {
	t.Parallel()
	root := NewRoot(BuildInfo{Version: "test", Commit: "abc", Date: "today"})
	want := []string{
		"init", "bootstrap", "list", "add", "show", "install", "uninstall",
		"pull", "push", "status", "diff", "resolve", "harness",
		"render", "apply-all", "org", "migrate", "doctor", "verify",
		"scan", "upgrade",
	}
	got := map[string]bool{}
	for _, c := range root.Commands() {
		got[strings.Fields(c.Use)[0]] = true
	}
	for _, name := range want {
		require.Truef(t, got[name], "missing command: %s", name)
	}
}

func TestRootVersionFlag(t *testing.T) {
	t.Parallel()
	root := NewRoot(BuildInfo{Version: "1.2.3", Commit: "deadbeef", Date: "2026-06-01"})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--version"})
	require.NoError(t, root.Execute())
	require.Contains(t, buf.String(), "1.2.3")
	require.Contains(t, buf.String(), "deadbeef")
}

func TestExitFromError(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, ExitFromError(nil))
	require.Equal(t, 1, ExitFromError(errors.New("boom")))
	require.Equal(t, 2, ExitFromError(ErrDirty))
}

func TestDepsResolveLibraryFromFlag(t *testing.T) {
	t.Parallel()
	d := &Deps{Flags: &GlobalFlags{LibraryDir: "/tmp/x"}}
	got, err := d.ResolveLibraryRoot()
	require.NoError(t, err)
	require.Equal(t, "/tmp/x", got)
}

func TestDepsResolveLibraryFromEnv(t *testing.T) {
	t.Setenv("ADEPT_LIBRARY", "/tmp/y")
	d := &Deps{Flags: &GlobalFlags{}}
	got, err := d.ResolveLibraryRoot()
	require.NoError(t, err)
	require.Equal(t, "/tmp/y", got)
}
