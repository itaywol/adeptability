//go:build integration

package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests exercise the real exec runner against the locally installed
// `git` binary. Run with: go test -tags=integration ./internal/git/...

func TestExecRunner_FullCycle(t *testing.T) {
	r := NewExecRunner("")
	c := NewClient(r)
	ctx := context.Background()
	dir := t.TempDir()
	require.NoError(t, c.Init(ctx, dir))
	require.True(t, c.IsRepo(dir))
	require.NoError(t, c.EnsureConfig(ctx, dir, "user.email", "test@example.com"))
	require.NoError(t, c.EnsureConfig(ctx, dir, "user.name", "Tester"))

	file := filepath.Join(dir, "hello.txt")
	require.NoError(t, os.WriteFile(file, []byte("hi"), 0o644))
	dirty, _, err := c.Status(ctx, dir)
	require.NoError(t, err)
	require.True(t, dirty)

	require.NoError(t, c.Add(ctx, dir, "hello.txt"))
	hash, err := c.Commit(ctx, dir, "initial")
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	head, err := c.HeadHash(ctx, dir)
	require.NoError(t, err)
	require.Equal(t, hash, head)

	dirty, _, err = c.Status(ctx, dir)
	require.NoError(t, err)
	require.False(t, dirty)
}
