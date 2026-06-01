package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itaywol/adeptability/internal/budget"
	"github.com/itaywol/adeptability/pkg/adept"
)

func renderAll(t *testing.T, skills []*adept.Skill) []adept.RenderOutput {
	t.Helper()
	r := New()
	parts := make([]adept.RenderOutput, 0, len(skills))
	for _, s := range skills {
		out, err := r.Render(context.Background(), adept.RenderInput{Skill: s})
		if err != nil {
			t.Fatalf("render %s: %v", s.ID, err)
		}
		parts = append(parts, out)
	}
	return parts
}

func TestAggregate_SingleSkill(t *testing.T) {
	r := New()
	a := NewAdapter(r, budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{{
		ID: "one", Description: "First skill",
		Body: "body\n",
	}})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected 1 RenderOutput, got %d", len(outs))
	}
	if outs[0].Path != OutputFile {
		t.Fatalf("path %q != %q", outs[0].Path, OutputFile)
	}
	if !strings.Contains(string(outs[0].Bytes), "## First skill") {
		t.Fatalf("missing heading in aggregate:\n%s", outs[0].Bytes)
	}
}

func TestAggregate_ThreeSkills_DeterministicOrder(t *testing.T) {
	r := New()
	a := NewAdapter(r, budget.NewPacker(), nil)
	// Render in reverse-alpha order to verify the aggregator re-sorts.
	parts := renderAll(t, []*adept.Skill{
		{ID: "zeta", Description: "Zeta", Body: "z body\n"},
		{ID: "alpha", Description: "Alpha", Body: "a body\n"},
		{ID: "mid", Description: "Mid", Body: "m body\n"},
	})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	doc := string(outs[0].Bytes)
	aIdx := strings.Index(doc, "id=alpha")
	mIdx := strings.Index(doc, "id=mid")
	zIdx := strings.Index(doc, "id=zeta")
	if !(aIdx < mIdx && mIdx < zIdx) {
		t.Fatalf("ordering not deterministic: alpha=%d mid=%d zeta=%d", aIdx, mIdx, zIdx)
	}
}

func TestAggregate_Idempotent(t *testing.T) {
	r := New()
	a := NewAdapter(r, budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{
		{ID: "b", Description: "B", Body: "b body\n"},
		{ID: "a", Description: "A", Body: "a body\n"},
	})
	o1, _ := a.Aggregate(context.Background(), parts, 0)
	o2, _ := a.Aggregate(context.Background(), parts, 0)
	if string(o1[0].Bytes) != string(o2[0].Bytes) {
		t.Fatalf("Aggregate not idempotent")
	}
}

func TestAggregate_BudgetOverflow_TruncationManifest(t *testing.T) {
	r := New()
	a := NewAdapter(r, budget.NewPacker(), nil)
	// Each skill body ~10 KiB. With 32 KiB budget and overhead reservation,
	// at most 2 should fit, one is dropped.
	big := strings.Repeat("y", 10*1024)
	skills := []*adept.Skill{
		{ID: "alpha", Description: "Alpha", Body: big},
		{ID: "bravo", Description: "Bravo", Body: big},
		{ID: "charlie", Description: "Charlie", Body: big},
		{ID: "delta", Description: "Delta", Body: big},
	}
	parts := renderAll(t, skills)
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	doc := string(outs[0].Bytes)
	if !strings.Contains(doc, "<!-- adeptability: omitted") {
		t.Fatalf("expected truncation manifest, got:\n%s", doc[:min(200, len(doc))])
	}
	// At least one dropped id should appear in the manifest.
	mLine := strings.SplitN(doc, "\n", 2)[0]
	if !strings.Contains(mLine, "Dropped:") {
		t.Fatalf("manifest missing 'Dropped:' label: %s", mLine)
	}
	// Total bytes must respect the budget.
	if len(outs[0].Bytes) > SizeBudgetB {
		t.Fatalf("aggregate exceeded budget: %d > %d", len(outs[0].Bytes), SizeBudgetB)
	}
}

func TestAggregate_OversizedSinglePartDropped(t *testing.T) {
	r := New()
	a := NewAdapter(r, budget.NewPacker(), nil)
	// One skill larger than the entire budget should be dropped, no error.
	huge := strings.Repeat("z", 64*1024)
	parts := renderAll(t, []*adept.Skill{
		{ID: "huge", Description: "Huge", Body: huge},
		{ID: "small", Description: "Small", Body: "tiny\n"},
	})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	doc := string(outs[0].Bytes)
	if !strings.Contains(doc, "Dropped: huge") {
		t.Fatalf("expected 'Dropped: huge' in manifest, got:\n%s", doc[:min(200, len(doc))])
	}
	if !strings.Contains(doc, "id=small") {
		t.Fatalf("expected small skill kept, got:\n%s", doc[:min(200, len(doc))])
	}
}

func TestAggregate_NegativeBudgetErrors(t *testing.T) {
	r := New()
	a := NewAdapter(r, budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{{ID: "x", Description: "X", Body: ""}})
	if _, err := a.Aggregate(context.Background(), parts, -1); err == nil {
		t.Fatalf("expected error for negative budget")
	}
}

func TestAggregate_Golden_Multi(t *testing.T) {
	r := New()
	a := NewAdapter(r, budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{
		{ID: "go-style", Description: "Go style guide", Body: "Use gofmt.\nPrefer explicit errors.\n"},
		{ID: "git-flow", Description: "Git workflow", Body: "Commit small.\n"},
		{ID: "api-design", Description: "API design rules", Body: "REST first.\n"},
	})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	goldenPath := filepath.Join("testdata", "aggregate-multi.golden")
	if *updateGolden {
		_ = os.MkdirAll(filepath.Dir(goldenPath), 0o755)
		if err := os.WriteFile(goldenPath, outs[0].Bytes, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run -update)", goldenPath, err)
	}
	if string(want) != string(outs[0].Bytes) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", string(want), string(outs[0].Bytes))
	}
}

func TestSpec(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	sp := a.Spec()
	if sp.ID != "codex" || sp.Kind != adept.KindAggregatorSingle {
		t.Fatalf("bad spec: %+v", sp)
	}
	if sp.OutputPath != OutputFile {
		t.Fatalf("OutputPath: %s", sp.OutputPath)
	}
	if sp.SizeBudgetB != SizeBudgetB {
		t.Fatalf("SizeBudgetB: %d", sp.SizeBudgetB)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
