// Package common provides cross-renderer helpers.
package common

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/itaywol/adeptability/pkg/adept"
)

// ShortSkillHash returns an 8-hex-character SHA-256 fingerprint computed over
// a stable encoding of the canonical skill: id, description, activation,
// sorted globs, body, and sorted sidecars (relpath plus content hash).
//
// The encoding is deterministic regardless of input map order or input slice
// order: globs and sidecars are sorted before hashing. This makes the hash
// suitable for use in section markers where users expect rendering twice to
// produce the same bytes.
//
// A nil skill yields the hash of the empty input string.
func ShortSkillHash(s *adept.Skill) string {
	h := sha256.New()
	if s == nil {
		sum := h.Sum(nil)
		return hex.EncodeToString(sum[:4])
	}

	h.Write([]byte(s.ID))
	h.Write([]byte{0})
	h.Write([]byte(s.Description))
	h.Write([]byte{0})
	h.Write([]byte(string(s.Activation)))
	h.Write([]byte{0})

	globs := append([]string(nil), s.Globs...)
	sort.Strings(globs)
	for _, g := range globs {
		h.Write([]byte(g))
		h.Write([]byte{0})
	}

	h.Write([]byte(s.Body))
	h.Write([]byte{0})

	files := append([]adept.SkillFile(nil), s.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	for _, f := range files {
		h.Write([]byte(f.RelPath))
		h.Write([]byte{0})
		inner := sha256.Sum256(f.Bytes)
		h.Write([]byte(hex.EncodeToString(inner[:])))
		h.Write([]byte{0})
	}

	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:4])
}
