// Package budget implements deterministic byte-budget packing for aggregator
// harness adapters (Codex AGENTS.md, Copilot per-glob bucket files).
//
// The packer is intentionally pure-logic: it takes []Part with priorities and
// returns which parts fit under a byte budget. Callers decide whether dropping
// any parts is a hard error.
package budget

import "sort"

// Part is a single skill fragment competing for budget space.
type Part struct {
	// SkillID is the canonical skill id (used for stable ordering and reporting).
	SkillID string
	// Bytes is the rendered fragment payload.
	Bytes []byte
	// Priority controls retention under budget pressure. Higher wins.
	Priority int
}

// PackResult is the deterministic output of a Pack call.
type PackResult struct {
	// Kept lists the parts that fit under the budget, in the deterministic
	// pack order (priority desc, len asc, id asc).
	Kept []Part
	// Dropped lists the parts that did NOT fit, in the same deterministic
	// ordering as Kept relative to the input.
	Dropped []Part
	// TotalB is the sum of len(Kept[i].Bytes) — does not include overhead.
	TotalB int
}

// Packer fits []Part within a byte budget, preserving high-priority parts.
//
// budgetB == 0 means "no budget" — everything is kept and Dropped is empty.
// overheadB is bytes reserved for caller-supplied framing (e.g. truncation
// manifest comment). The effective per-part capacity is budgetB - overheadB.
//
// Pack never returns an error in v0.1; callers inspect Dropped and decide
// whether strict mode should escalate via adept.ErrBudgetOverflow.
type Packer interface {
	Pack(parts []Part, budgetB, overheadB int) (PackResult, error)
}

type packer struct{}

// NewPacker returns the default Packer implementation.
func NewPacker() Packer { return &packer{} }

// Pack implements the greedy fit-by-priority algorithm.
//
// Sort order: Priority desc, len(Bytes) asc, SkillID asc.
func (p *packer) Pack(parts []Part, budgetB, overheadB int) (PackResult, error) {
	if len(parts) == 0 {
		return PackResult{Kept: nil, Dropped: nil, TotalB: 0}, nil
	}

	// Copy so we never mutate caller storage.
	in := make([]Part, len(parts))
	copy(in, parts)

	sort.SliceStable(in, func(i, j int) bool {
		a, b := in[i], in[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if la, lb := len(a.Bytes), len(b.Bytes); la != lb {
			return la < lb
		}
		return a.SkillID < b.SkillID
	})

	// budgetB == 0 means unlimited: keep everything.
	if budgetB == 0 {
		total := 0
		for i := range in {
			total += len(in[i].Bytes)
		}
		return PackResult{Kept: in, Dropped: nil, TotalB: total}, nil
	}

	cap := budgetB - overheadB
	if cap < 0 {
		cap = 0
	}

	kept := make([]Part, 0, len(in))
	dropped := make([]Part, 0)
	total := 0
	for _, part := range in {
		size := len(part.Bytes)
		if total+size <= cap {
			kept = append(kept, part)
			total += size
		} else {
			dropped = append(dropped, part)
		}
	}

	return PackResult{Kept: kept, Dropped: dropped, TotalB: total}, nil
}
