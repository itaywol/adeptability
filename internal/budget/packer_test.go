package budget

import (
	"strings"
	"testing"
)

func mkPart(id string, version int, size int, prio int) Part {
	return Part{
		SkillID:      id,
		SkillVersion: version,
		Bytes:        []byte(strings.Repeat("x", size)),
		Priority:     prio,
	}
}

func TestPack_Empty(t *testing.T) {
	p := NewPacker()
	res, err := p.Pack(nil, 1024, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Kept) != 0 || len(res.Dropped) != 0 || res.TotalB != 0 {
		t.Fatalf("expected empty result, got %+v", res)
	}
}

func TestPack_UnlimitedBudget(t *testing.T) {
	p := NewPacker()
	parts := []Part{
		mkPart("a", 1, 1024, 0),
		mkPart("b", 1, 8192, 0),
		mkPart("c", 1, 16384, 0),
	}
	res, err := p.Pack(parts, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Kept) != 3 {
		t.Fatalf("expected all 3 kept, got %d", len(res.Kept))
	}
	if len(res.Dropped) != 0 {
		t.Fatalf("expected no drops with zero budget, got %d", len(res.Dropped))
	}
	if res.TotalB != 1024+8192+16384 {
		t.Fatalf("totalB wrong: %d", res.TotalB)
	}
}

func TestPack_SinglePartUnderBudget(t *testing.T) {
	p := NewPacker()
	parts := []Part{mkPart("a", 1, 500, 0)}
	res, err := p.Pack(parts, 1024, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Kept) != 1 || len(res.Dropped) != 0 {
		t.Fatalf("expected 1 kept, 0 dropped, got %+v", res)
	}
}

func TestPack_SinglePartOverBudget(t *testing.T) {
	p := NewPacker()
	parts := []Part{mkPart("big", 1, 2048, 0)}
	res, err := p.Pack(parts, 1024, 0)
	if err != nil {
		t.Fatalf("expected no error (caller decides), got %v", err)
	}
	if len(res.Kept) != 0 {
		t.Fatalf("expected 0 kept, got %d", len(res.Kept))
	}
	if len(res.Dropped) != 1 {
		t.Fatalf("expected 1 dropped, got %d", len(res.Dropped))
	}
	if res.Dropped[0].SkillID != "big" {
		t.Fatalf("wrong dropped id: %s", res.Dropped[0].SkillID)
	}
}

func TestPack_ManyPartsAllFit(t *testing.T) {
	p := NewPacker()
	parts := []Part{
		mkPart("a", 1, 100, 0),
		mkPart("b", 1, 100, 0),
		mkPart("c", 1, 100, 0),
		mkPart("d", 1, 100, 0),
	}
	res, err := p.Pack(parts, 1024, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Kept) != 4 || len(res.Dropped) != 0 {
		t.Fatalf("expected all 4 kept, got %+v", res)
	}
	if res.TotalB != 400 {
		t.Fatalf("totalB wrong: %d", res.TotalB)
	}
}

func TestPack_PriorityRespected(t *testing.T) {
	p := NewPacker()
	// Budget fits exactly 2 parts of 500 bytes each.
	parts := []Part{
		mkPart("low1", 1, 500, 0),
		mkPart("low2", 1, 500, 0),
		mkPart("high", 1, 500, 100),
	}
	res, err := p.Pack(parts, 1000, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "high" must always be in kept.
	foundHigh := false
	for _, k := range res.Kept {
		if k.SkillID == "high" {
			foundHigh = true
		}
	}
	if !foundHigh {
		t.Fatalf("priority not respected: high not kept; kept=%v dropped=%v", ids(res.Kept), ids(res.Dropped))
	}
	if len(res.Kept) != 2 || len(res.Dropped) != 1 {
		t.Fatalf("expected 2 kept, 1 dropped, got %d/%d", len(res.Kept), len(res.Dropped))
	}
}

func TestPack_VersionTieBreaksAfterPriority(t *testing.T) {
	p := NewPacker()
	// Same priority, different version: newer wins.
	parts := []Part{
		mkPart("a", 1, 600, 0),
		mkPart("a-v2", 2, 600, 0),
	}
	res, err := p.Pack(parts, 1000, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Kept) != 1 || res.Kept[0].SkillID != "a-v2" {
		t.Fatalf("expected newer version kept, got kept=%v", ids(res.Kept))
	}
}

func TestPack_LenAscTieBreaksAfterVersion(t *testing.T) {
	p := NewPacker()
	parts := []Part{
		mkPart("big", 1, 800, 0),
		mkPart("small", 1, 100, 0),
	}
	// Budget allows only small (100) since big (800) + small (100) = 900 fits exactly.
	// Actually both fit. Use a budget where only smaller fits.
	res, err := p.Pack(parts, 500, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Kept) != 1 || res.Kept[0].SkillID != "small" {
		t.Fatalf("expected smaller kept first, got %v", ids(res.Kept))
	}
}

func TestPack_IDAscTieBreaksLast(t *testing.T) {
	p := NewPacker()
	parts := []Part{
		mkPart("zeta", 1, 400, 0),
		mkPart("alpha", 1, 400, 0),
	}
	// Budget fits one only.
	res, err := p.Pack(parts, 500, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Kept) != 1 || res.Kept[0].SkillID != "alpha" {
		t.Fatalf("expected alphabetical tie-break, got %v", ids(res.Kept))
	}
}

func TestPack_OverheadReserved(t *testing.T) {
	p := NewPacker()
	parts := []Part{
		mkPart("a", 1, 500, 0),
		mkPart("b", 1, 500, 0),
	}
	// Without overhead both fit (1000 = 1000 budget).
	res, err := p.Pack(parts, 1000, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Kept) != 2 {
		t.Fatalf("expected 2 kept without overhead, got %d", len(res.Kept))
	}
	// With 100 bytes overhead only one fits (effective cap=900).
	res, err = p.Pack(parts, 1000, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Kept) != 1 || len(res.Dropped) != 1 {
		t.Fatalf("expected 1 kept with overhead, got %d kept", len(res.Kept))
	}
}

func TestPack_DeterministicOrder(t *testing.T) {
	p := NewPacker()
	// Different input orderings must produce the same pack result.
	a := mkPart("a", 1, 200, 0)
	b := mkPart("b", 1, 200, 0)
	c := mkPart("c", 1, 200, 0)
	d := mkPart("d", 1, 200, 0)
	order1 := []Part{a, b, c, d}
	order2 := []Part{d, c, b, a}
	res1, _ := p.Pack(order1, 500, 0) // fits 2
	res2, _ := p.Pack(order2, 500, 0)
	if ids(res1.Kept) != ids(res2.Kept) {
		t.Fatalf("non-deterministic order: %s vs %s", ids(res1.Kept), ids(res2.Kept))
	}
}

func TestPack_NeverErrors(t *testing.T) {
	p := NewPacker()
	// Even with extreme inputs, Pack returns nil error.
	parts := []Part{mkPart("oversize", 1, 1024*1024, 0)}
	_, err := p.Pack(parts, 100, 50)
	if err != nil {
		t.Fatalf("Pack should never error; got %v", err)
	}
}

func TestPack_DoesNotMutateInput(t *testing.T) {
	p := NewPacker()
	parts := []Part{
		mkPart("b", 1, 100, 0),
		mkPart("a", 1, 100, 0),
	}
	preB := parts[0].SkillID
	preA := parts[1].SkillID
	_, _ = p.Pack(parts, 1024, 0)
	if parts[0].SkillID != preB || parts[1].SkillID != preA {
		t.Fatalf("Pack mutated input")
	}
}

func ids(parts []Part) string {
	var s strings.Builder
	for i, p := range parts {
		if i > 0 {
			s.WriteByte(',')
		}
		s.WriteString(p.SkillID)
	}
	return s.String()
}
