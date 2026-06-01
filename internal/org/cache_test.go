package org

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileETagCache_PutThenGet(t *testing.T) {
	dir := t.TempDir()
	c := NewFileETagCache(dir)

	require.NoError(t, c.Put("https://example.com/org.yaml", `"etag-1"`, []byte("body-1")))
	etag, body, ok := c.Get("https://example.com/org.yaml")
	require.True(t, ok)
	require.Equal(t, `"etag-1"`, etag)
	require.Equal(t, "body-1", string(body))
}

func TestFileETagCache_PutOverwrites(t *testing.T) {
	dir := t.TempDir()
	c := NewFileETagCache(dir)

	require.NoError(t, c.Put("https://example.com/x", `"v1"`, []byte("first")))
	require.NoError(t, c.Put("https://example.com/x", `"v2"`, []byte("second")))

	etag, body, ok := c.Get("https://example.com/x")
	require.True(t, ok)
	require.Equal(t, `"v2"`, etag)
	require.Equal(t, "second", string(body))
}

func TestFileETagCache_MissReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	c := NewFileETagCache(dir)

	etag, body, ok := c.Get("https://nope")
	require.False(t, ok)
	require.Empty(t, etag)
	require.Nil(t, body)
}

func TestFileETagCache_DifferentURLsDontCollide(t *testing.T) {
	dir := t.TempDir()
	c := NewFileETagCache(dir)

	require.NoError(t, c.Put("https://a.example/x", `"a"`, []byte("AAA")))
	require.NoError(t, c.Put("https://b.example/y", `"b"`, []byte("BBB")))

	etagA, bodyA, okA := c.Get("https://a.example/x")
	require.True(t, okA)
	require.Equal(t, `"a"`, etagA)
	require.Equal(t, "AAA", string(bodyA))

	etagB, bodyB, okB := c.Get("https://b.example/y")
	require.True(t, okB)
	require.Equal(t, `"b"`, etagB)
	require.Equal(t, "BBB", string(bodyB))
}

func TestFileETagCache_LazyDirCreation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "cache")
	// dir does not exist yet.
	c := NewFileETagCache(dir)

	_, _, ok := c.Get("https://x")
	require.False(t, ok, "Get on missing dir should be a miss, not an error")

	require.NoError(t, c.Put("https://x", `"e"`, []byte("body")))
	st, err := os.Stat(dir)
	require.NoError(t, err)
	require.True(t, st.IsDir())
}

func TestFileETagCache_NilReceiverSafe(t *testing.T) {
	var c *FileETagCache
	etag, body, ok := c.Get("https://anything")
	require.False(t, ok)
	require.Empty(t, etag)
	require.Nil(t, body)
}

func TestFileETagCache_EmptyRootPutFails(t *testing.T) {
	c := NewFileETagCache("")
	err := c.Put("https://x", `"e"`, []byte("body"))
	require.Error(t, err)
}

func TestFileETagCache_CorruptEntryReadAsMiss(t *testing.T) {
	dir := t.TempDir()
	c := NewFileETagCache(dir)
	// Write corrupt JSON at the cache path.
	require.NoError(t, c.Put("https://x", `"e"`, []byte("ok")))
	corrupt := c.path("https://x")
	require.NoError(t, os.WriteFile(corrupt, []byte("{ broken"), 0o600))

	_, _, ok := c.Get("https://x")
	require.False(t, ok, "corrupt cache entry should read as miss")
}

func TestFileETagCache_PutMkdirFailure(t *testing.T) {
	// Point the root at a path that cannot be created (under a regular file).
	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	c := NewFileETagCache(filepath.Join(blocker, "subdir"))
	err := c.Put("https://x", `"e"`, []byte("body"))
	require.Error(t, err)
}

func TestFileETagCache_PutCreateTempFailure(t *testing.T) {
	// Make the root exist as a regular file (not a dir). MkdirAll on the
	// dir name itself will then fail because the path is occupied by a file.
	parent := t.TempDir()
	occupied := filepath.Join(parent, "rootfile")
	require.NoError(t, os.WriteFile(occupied, []byte(""), 0o644))
	c := NewFileETagCache(occupied)
	err := c.Put("https://x", `"e"`, []byte("body"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "etag cache")
}

func TestFileETagCache_PutRenameFailure(t *testing.T) {
	// Put once to establish the directory, then occupy the target path
	// with a directory so the second rename cannot replace it.
	dir := t.TempDir()
	c := NewFileETagCache(dir)
	require.NoError(t, c.Put("https://x", `"e"`, []byte("body")))
	// Replace the final cache file with a directory of the same name.
	target := c.path("https://x")
	require.NoError(t, os.Remove(target))
	require.NoError(t, os.Mkdir(target, 0o755))
	// Drop a child so the dir isn't empty — rename-onto-non-empty-dir fails
	// on Linux even when both source and dest are on the same filesystem.
	require.NoError(t, os.WriteFile(filepath.Join(target, "blocker"), []byte("x"), 0o644))
	err := c.Put("https://x", `"e2"`, []byte("body2"))
	require.Error(t, err)
}

func TestFileETagCache_GetWithMismatchedURLInEntry(t *testing.T) {
	dir := t.TempDir()
	c := NewFileETagCache(dir)
	// Put under one URL, then poison the on-disk record so the embedded URL
	// no longer matches the lookup key.
	require.NoError(t, c.Put("https://x", `"e"`, []byte("payload")))
	corrupt := c.path("https://x")
	require.NoError(t, os.WriteFile(corrupt, []byte(`{"url":"https://other","etag":"\"e\"","body":"cGF5bG9hZA=="}`), 0o600))
	_, _, ok := c.Get("https://x")
	require.False(t, ok, "mismatched-URL entry must read as miss")
}
