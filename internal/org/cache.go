package org

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ETagCache is a small content cache keyed by URL. It stores the ETag header
// alongside the cached body so the HTTP client can issue conditional GETs.
//
// Implementations MUST be safe for sequential calls within a single process;
// the file-backed implementation in this package is process-local — concurrent
// adept invocations across separate processes may interleave writes, but each
// write is atomic so the worst case is a stale or rewritten entry rather than
// a corrupt one.
type ETagCache interface {
	Get(url string) (etag string, body []byte, ok bool)
	Put(url, etag string, body []byte) error
}

// FileETagCache stores cache entries as JSON files under a root directory.
// Each URL hashes to a stable filename so we never blow up on URLs containing
// slashes, query strings, or fragments.
type FileETagCache struct {
	root string
}

// NewFileETagCache returns a cache rooted at dir. The directory is created
// lazily on the first Put — Get against a missing dir returns ok=false.
func NewFileETagCache(dir string) *FileETagCache {
	return &FileETagCache{root: dir}
}

type cacheEntry struct {
	URL  string `json:"url"`
	ETag string `json:"etag"`
	Body []byte `json:"body"`
}

func (c *FileETagCache) path(url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(c.root, hex.EncodeToString(sum[:])+".json")
}

// Get loads the cached body+ETag for url. Missing entries return ok=false
// with no error surface (cache miss is normal).
func (c *FileETagCache) Get(url string) (string, []byte, bool) {
	if c == nil || c.root == "" {
		return "", nil, false
	}
	data, err := os.ReadFile(c.path(url))
	if err != nil {
		return "", nil, false
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return "", nil, false
	}
	if entry.URL != url {
		// Hash collision is astronomically unlikely; treat as miss.
		return "", nil, false
	}
	return entry.ETag, entry.Body, true
}

// Put stores body under url tagged with etag. Writes are atomic via
// write-then-rename so concurrent readers never see a partial file.
//
// json.Marshal cannot fail on cacheEntry (all fields are JSON-safe
// primitives) so we do not bother surfacing a marshal-error path.
func (c *FileETagCache) Put(url, etag string, body []byte) error {
	if c == nil || c.root == "" {
		return errors.New("etag cache: empty root")
	}
	if err := os.MkdirAll(c.root, 0o755); err != nil {
		return fmt.Errorf("etag cache: mkdir %q: %w", c.root, err)
	}
	raw, _ := json.Marshal(cacheEntry{URL: url, ETag: etag, Body: body})
	final := c.path(url)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("etag cache: write temp: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("etag cache: rename: %w", err)
	}
	return nil
}
