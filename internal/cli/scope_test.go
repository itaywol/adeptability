package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/log"
)

// TestScopedProjectResolution exercises the four scope-resolution paths:
// --global, explicit --project, walk-up discovery, and the no-project global
// fallback (which emits a one-line notice).
func TestScopedProjectResolution(t *testing.T) {
	t.Run("global flag returns global project", func(t *testing.T) {
		libRoot := t.TempDir()
		t.Setenv("ADEPT_LIBRARY", libRoot)

		d, err := NewDeps(&GlobalFlags{Global: true}, BuildInfo{})
		require.NoError(t, err)

		p, isGlobal, err := d.ScopedProject()
		require.NoError(t, err)
		require.True(t, isGlobal)
		require.Equal(t, filepath.Dir(libRoot), p.Root())
		require.Equal(t, libRoot, p.BaseDir())
	})

	t.Run("explicit project skips walk-up", func(t *testing.T) {
		t.Setenv("ADEPT_LIBRARY", t.TempDir())
		proj := t.TempDir() // no config.json anywhere

		d, err := NewDeps(&GlobalFlags{ProjectDir: proj, projectDirExplicit: true}, BuildInfo{})
		require.NoError(t, err)

		p, isGlobal, err := d.ScopedProject()
		require.NoError(t, err)
		require.False(t, isGlobal)
		require.Equal(t, proj, p.Root())
	})

	t.Run("walk-up finds nearest ancestor config", func(t *testing.T) {
		t.Setenv("ADEPT_LIBRARY", t.TempDir())
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".adeptability"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".adeptability", "config.json"), []byte("{}"), 0o644))
		deep := filepath.Join(root, "nested", "deep")
		require.NoError(t, os.MkdirAll(deep, 0o755))

		// projectDirExplicit=false mirrors PersistentPreRunE defaulting
		// ProjectDir to cwd; the walk-up should still run.
		d, err := NewDeps(&GlobalFlags{ProjectDir: deep}, BuildInfo{})
		require.NoError(t, err)

		p, isGlobal, err := d.ScopedProject()
		require.NoError(t, err)
		require.False(t, isGlobal)
		require.Equal(t, root, p.Root())
	})

	t.Run("no config falls back to global with notice", func(t *testing.T) {
		libRoot := t.TempDir()
		t.Setenv("ADEPT_LIBRARY", libRoot)
		proj := t.TempDir() // isolated: no .adeptability up the tree

		d, err := NewDeps(&GlobalFlags{ProjectDir: proj}, BuildInfo{})
		require.NoError(t, err)

		var buf bytes.Buffer
		d.Log = log.NewLogger(log.LevelInfo, false, &buf)

		p, isGlobal, err := d.ScopedProject()
		require.NoError(t, err)
		require.True(t, isGlobal)
		require.Equal(t, filepath.Dir(libRoot), p.Root())
		require.Equal(t, libRoot, p.BaseDir())

		require.Equal(t, 1, strings.Count(buf.String(), "operating on global scope"),
			"notice must appear exactly once: %q", buf.String())
	})
}
