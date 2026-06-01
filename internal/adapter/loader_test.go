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
