package budget

import (
	"strings"
	"testing"
)

func mkPart(id string, size int, prio int) Part {
	return Part{
		SkillID:  id,
		Bytes:    []byte(strings.Repeat("x", size)),
		Priority: prio,
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
		mkPart("a", 1024, 0),
		mkPart("b", 8192, 0),
		mkPart("c", 16384, 0),
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
	parts := []Part{mkPart("a", 500, 0)}
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
	parts := []Part{mkPart("big", 2048, 0)}
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
		mkPart("a", 100, 0),
		mkPart("b", 100, 0),
		mkPart("c", 100, 0),
		mkPart("d", 100, 0),
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
		mkPart("low1", 500, 0),
		mkPart("low2", 500, 0),
		mkPart("high", 500, 100),
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

func TestPack_LenAscTieBreaksAfterPriority(t *testing.T) {
	p := NewPacker()
	parts := []Part{
		mkPart("big", 800, 0),
		mkPart("small", 100, 0),
	}
	// Budget allows only smaller.
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
		mkPart("zeta", 400, 0),
		mkPart("alpha", 400, 0),
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
		mkPart("a", 500, 0),
		mkPart("b", 500, 0),
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
	a := mkPart("a", 200, 0)
	b := mkPart("b", 200, 0)
	c := mkPart("c", 200, 0)
	d := mkPart("d", 200, 0)
	order1 := []Part{a, b, c, d}
	order2 := []Part{d, c, b, a}
	res1, _ := p.Pack(order1, 500, 0) // fits 2
	res2, _ := p.Pack(order2, 500, 0)
	if ids(res1.Kept) != ids(res2.Kept) {
		t.Fatalf("non-deterministic order: %s vs %s", ids(res1.Kept), ids(res2.Kept))
	}
}

func TestPack_DeterministicOrderByIDAndSize(t *testing.T) {
	p := NewPacker()
	// Mixed sizes and ids — verify the documented (len asc, id asc) order.
	parts := []Part{
		{SkillID: "zeta", Bytes: []byte("xxxx"), Priority: 0},  // 4
		{SkillID: "alpha", Bytes: []byte("xx"), Priority: 0},   // 2
		{SkillID: "mid", Bytes: []byte("xxxx"), Priority: 0},   // 4
		{SkillID: "delta", Bytes: []byte("xxxxx"), Priority: 0}, // 5
	}
	res, err := p.Pack(parts, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expected sorted by (len asc, id asc): alpha(2), mid(4), zeta(4), delta(5)
	got := ids(res.Kept)
	want := "alpha,mid,zeta,delta"
	if got != want {
		t.Fatalf("order mismatch: want=%s got=%s", want, got)
	}
}

func TestPack_NeverErrors(t *testing.T) {
	p := NewPacker()
	// Even with extreme inputs, Pack returns nil error.
	parts := []Part{mkPart("oversize", 1024*1024, 0)}
	_, err := p.Pack(parts, 100, 50)
	if err != nil {
		t.Fatalf("Pack should never error; got %v", err)
	}
}

func TestPack_DoesNotMutateInput(t *testing.T) {
	p := NewPacker()
	parts := []Part{
		mkPart("b", 100, 0),
		mkPart("a", 100, 0),
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
