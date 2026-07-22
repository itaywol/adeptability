package adapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/pkg/adept"
)

func newLoader(t *testing.T) Loader {
	t.Helper()
	v, err := NewSchemaValidator()
	require.NoError(t, err)
	w := fsutil.NewWriter()
	l := fsutil.NewLinker(w)
	return NewLoader(v, w, l)
}

func TestLoader_LoadDir_TestData(t *testing.T) {
	loader := newLoader(t)
	dir := filepath.Join("testdata")
	// Stage only the valid yamls into a fresh dir so the invalid sample
	// doesn't trip the loader. Copy by content.
	staging := t.TempDir()
	for _, name := range []string{"cursor.yaml", "aggregator.yaml"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(staging, name), data, 0o644))
	}
	adapters, err := loader.LoadDir(staging)
	require.NoError(t, err)
	require.Len(t, adapters, 2)
	require.Equal(t, "agents-test", adapters[0].Spec().ID)
	require.Equal(t, "cursor-test", adapters[1].Spec().ID)
}

func TestLoader_LoadDir_MissingDirReturnsEmpty(t *testing.T) {
	loader := newLoader(t)
	adapters, err := loader.LoadDir(filepath.Join(t.TempDir(), "absent"))
	require.NoError(t, err)
	require.Empty(t, adapters)
}

func TestLoader_LoadFile_Valid(t *testing.T) {
	loader := newLoader(t)
	a, err := loader.LoadFile(filepath.Join("testdata", "cursor.yaml"))
	require.NoError(t, err)
	require.Equal(t, "cursor-test", a.Spec().ID)
	require.Equal(t, adept.KindPerSkill, a.Spec().Kind)
	require.True(t, a.Spec().NeedsDir)
	require.Equal(t, ".cursor", a.Spec().BaseDir)
}

func TestLoader_LoadFile_GlobalOutput(t *testing.T) {
	loader := newLoader(t)
	dir := t.TempDir()
	data := []byte(`id: my-agent
name: "My Agent"
kind: per-skill
output: .myagent/rules/{id}.rule
base-dir: .myagent
global-output: .config/myagent/rules/{id}.rule
global-base-dir: .config/myagent
`)
	path := filepath.Join(dir, "my-agent.yaml")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	a, err := loader.LoadFile(path)
	require.NoError(t, err)
	require.Equal(t, ".config/myagent/rules/{id}.rule", a.Spec().GlobalOutput)
	require.Equal(t, ".config/myagent", a.Spec().GlobalBaseDir)
}

func TestLoader_LoadFile_GlobalOutputAbsent_ZeroValue(t *testing.T) {
	loader := newLoader(t)
	// cursor.yaml has no global-output/global-base-dir keys; both fields must
	// still load with zero values rather than erroring or being required.
	a, err := loader.LoadFile(filepath.Join("testdata", "cursor.yaml"))
	require.NoError(t, err)
	require.Empty(t, a.Spec().GlobalOutput)
	require.Empty(t, a.Spec().GlobalBaseDir)
}

func TestLoader_LoadFile_InvalidRejected(t *testing.T) {
	loader := newLoader(t)
	_, err := loader.LoadFile(filepath.Join("testdata", "invalid.yaml"))
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrAdapterInvalid)
}

func TestLoader_LoadFile_Missing(t *testing.T) {
	loader := newLoader(t)
	_, err := loader.LoadFile(filepath.Join(t.TempDir(), "nope.yaml"))
	require.Error(t, err)
}

func TestSchemaValidator_RejectsBrokenSpec(t *testing.T) {
	v, err := NewSchemaValidator()
	require.NoError(t, err)
	err = v.Validate([]byte("name: nope\n"))
	require.ErrorIs(t, err, adept.ErrAdapterInvalid)
}

func TestSchemaValidator_AcceptsGoodSpec(t *testing.T) {
	v, err := NewSchemaValidator()
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join("testdata", "cursor.yaml"))
	require.NoError(t, err)
	require.NoError(t, v.Validate(data))
}
