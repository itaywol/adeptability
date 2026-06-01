package org

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileClient_FetchValid(t *testing.T) {
	parser, err := NewParser()
	require.NoError(t, err)
	c := NewFileClient(filepath.Join("testdata", "org.yaml"), parser)
	m, err := c.Fetch(context.Background())
	require.NoError(t, err)
	require.Equal(t, "acme", m.Name)
}

func TestFileClient_FetchMissing(t *testing.T) {
	parser, err := NewParser()
	require.NoError(t, err)
	c := NewFileClient(filepath.Join(t.TempDir(), "nope.yaml"), parser)
	_, err = c.Fetch(context.Background())
	require.Error(t, err)
}

func TestFileClient_EmptyPathFails(t *testing.T) {
	parser, err := NewParser()
	require.NoError(t, err)
	c := NewFileClient("", parser)
	_, err = c.Fetch(context.Background())
	require.Error(t, err)
}

func TestFileClient_ParserError(t *testing.T) {
	parser, err := NewParser()
	require.NoError(t, err)
	tmp := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(tmp, []byte("name: only\n"), 0o644))
	c := NewFileClient(tmp, parser)
	_, err = c.Fetch(context.Background())
	require.Error(t, err)
}
