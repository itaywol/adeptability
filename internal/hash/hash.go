// Package hash computes deterministic content hashes for skills.
//
// Determinism across operating systems is achieved by:
//   - Sorting walked file paths lexicographically.
//   - Normalizing line endings (CRLF -> LF) before hashing content.
//   - Framing entries with explicit length prefixes so directory walks can't
//     collide with file contents.
//   - Excluding metadata-only paths (.adeptignore, config.json, .signature,
//     staging dir, .git).
package hash

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Hasher produces "sha256:<hex>" digests for skills.
type Hasher interface {
	HashSkillDir(dir string) (string, error)
	HashSkill(s *adept.Skill) (string, error)
}

type hasher struct{}

// NewHasher returns a stateless hasher implementation.
func NewHasher() Hasher {
	return &hasher{}
}

const sha256Prefix = "sha256:"

// HashSkillDir walks dir, sorts file paths, and hashes a framed stream of
// (relpath, content) pairs. Returns "sha256:<hex>".
func (h *hasher) HashSkillDir(dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("hash: stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("hash: %s is not a directory", dir)
	}
	patterns, err := readIgnore(filepath.Join(dir, adept.IgnoreFileName))
	if err != nil {
		return "", err
	}

	type entry struct {
		rel  string
		path string
	}
	var entries []entry
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		base := filepath.Base(path)
		if d.IsDir() {
			// Skip dot-directories entirely (e.g. .git, .anything).
			if strings.HasPrefix(base, ".") {
				return fs.SkipDir
			}
			// Exclude the staging dir, which holds transient pull/merge state.
			if rel == adept.StagingDir {
				return fs.SkipDir
			}
			return nil
		}
		// Exclude metadata-only files: ignore file, project config, signature.
		if rel == adept.IgnoreFileName ||
			rel == adept.ConfigFileName ||
			rel == adept.SignatureName {
			return nil
		}
		if matchAny(patterns, rel) {
			return nil
		}
		entries = append(entries, entry{rel: rel, path: path})
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("hash: walk %s: %w", dir, walkErr)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	w := sha256.New()
	for _, e := range entries {
		content, err := os.ReadFile(e.path)
		if err != nil {
			return "", fmt.Errorf("hash: read %s: %w", e.path, err)
		}
		content = normalizeLineEndings(content)
		relBytes := []byte(e.rel)
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(relBytes)))
		if _, err := w.Write(lenBuf[:]); err != nil {
			return "", err
		}
		if _, err := w.Write(relBytes); err != nil {
			return "", err
		}
		if _, err := w.Write([]byte{0x00}); err != nil {
			return "", err
		}
		var sizeBuf [8]byte
		binary.BigEndian.PutUint64(sizeBuf[:], uint64(len(content)))
		if _, err := w.Write(sizeBuf[:]); err != nil {
			return "", err
		}
		if _, err := w.Write(content); err != nil {
			return "", err
		}
	}
	return sha256Prefix + hex.EncodeToString(w.Sum(nil)), nil
}

// HashSkill hashes the canonical Skill struct via a key-sorted JSON encoding
// derived from json.Marshal (which already sorts map keys). Body is LF
// normalized before hashing.
func (h *hasher) HashSkill(s *adept.Skill) (string, error) {
	if s == nil {
		return "", fmt.Errorf("hash: nil skill")
	}
	clone := *s
	clone.Body = string(normalizeLineEndings([]byte(s.Body)))
	// Clear sidecar bytes from the canonical envelope; sidecars hash through
	// their own normalized envelope so callers can prove content-equivalence.
	var sidecars []adept.SkillFile
	if len(clone.Files) > 0 {
		sidecars = make([]adept.SkillFile, len(clone.Files))
		copy(sidecars, clone.Files)
		sort.Slice(sidecars, func(i, j int) bool { return sidecars[i].RelPath < sidecars[j].RelPath })
		for i := range sidecars {
			sidecars[i].Bytes = normalizeLineEndings(sidecars[i].Bytes)
		}
	}
	envelope := struct {
		Skill    adept.Skill       `json:"skill"`
		Sidecars []adept.SkillFile `json:"sidecars,omitempty"`
	}{
		Skill:    clone,
		Sidecars: sidecars,
	}
	// Drop the Body and Files from the marshalled Skill (they have json:"-"
	// tags). Re-attach Body explicitly so the hash sees it.
	raw, err := json.Marshal(struct {
		Envelope any    `json:"envelope"`
		Body     string `json:"body"`
	}{Envelope: envelope, Body: clone.Body})
	if err != nil {
		return "", fmt.Errorf("hash: marshal skill: %w", err)
	}
	// canonicalize via re-marshal to ensure stable key order from go std lib.
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return "", err
	}
	canon, err := json.Marshal(generic)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return sha256Prefix + hex.EncodeToString(sum[:]), nil
}

// normalizeLineEndings converts CRLF to LF for OS-portable hashing.
func normalizeLineEndings(b []byte) []byte {
	if !bytes.Contains(b, []byte{'\r'}) {
		return b
	}
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

func readIgnore(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("hash: read %s: %w", path, err)
	}
	var patterns []string
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		patterns = append(patterns, trimmed)
	}
	return patterns, nil
}

func matchAny(patterns []string, rel string) bool {
	for _, p := range patterns {
		ok, err := doublestar.Match(p, rel)
		if err == nil && ok {
			return true
		}
	}
	return false
}
