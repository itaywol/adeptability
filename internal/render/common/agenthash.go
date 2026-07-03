package common

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/itaywol/adeptability/pkg/adept"
)

// ShortAgentHash returns an 8-hex-character SHA-256 fingerprint computed over
// a stable encoding of the canonical agent: id, description, mode, sorted
// tool lists, model, and body. Deterministic regardless of input slice order.
// A nil agent yields the hash of the empty input.
func ShortAgentHash(a *adept.Agent) string {
	h := sha256.New()
	if a == nil {
		sum := h.Sum(nil)
		return hex.EncodeToString(sum[:4])
	}
	h.Write([]byte(a.ID))
	h.Write([]byte{0})
	h.Write([]byte(a.Description))
	h.Write([]byte{0})
	h.Write([]byte(string(a.Mode)))
	h.Write([]byte{0})
	for _, list := range [][]string{a.Tools, a.DisallowedTools} {
		sorted := append([]string(nil), list...)
		sort.Strings(sorted)
		for _, item := range sorted {
			h.Write([]byte(item))
			h.Write([]byte{0})
		}
		h.Write([]byte{1})
	}
	h.Write([]byte(a.Model))
	h.Write([]byte{0})
	h.Write([]byte(a.Body))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:4])
}
