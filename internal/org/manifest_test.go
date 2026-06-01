package org

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

func TestParser_AcceptsValidManifest(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join("testdata", "org.yaml"))
	require.NoError(t, err)
	m, err := p.Parse(data)
	require.NoError(t, err)
	require.Equal(t, 1, m.Version)
	require.Equal(t, "acme", m.Name)
	require.Len(t, m.Required, 2)
	require.Equal(t, "skill-a", m.Required[0].ID)
	require.Len(t, m.Optional, 1)
	require.Equal(t, "skill-c", m.Optional[0].ID)
}

func TestParser_RejectsMissingVersion(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)
	_, err = p.Parse([]byte("name: foo\nskills: {}\n"))
	require.ErrorIs(t, err, adept.ErrAdapterInvalid)
}

func TestParser_RejectsEmpty(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)
	_, err = p.Parse(nil)
	require.Error(t, err)
}

func TestParser_RejectsMalformedYAML(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)
	_, err = p.Parse([]byte("not: : yaml"))
	require.Error(t, err)
}

func TestParser_AcceptsMinimalManifest(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)
	m, err := p.Parse([]byte("version: 1\nskills: {}\n"))
	require.NoError(t, err)
	require.Equal(t, 1, m.Version)
	require.Empty(t, m.Required)
	require.Empty(t, m.Optional)
}
