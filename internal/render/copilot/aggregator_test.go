package copilot

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/itaywol/adeptability/internal/budget"
	"github.com/itaywol/adeptability/pkg/adept"
)

// fakeReader is the same in-memory FileReader used in adapter tests.
type fakeReader struct {
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]struct{}
}

func newFakeReader() *fakeReader {
	return &fakeReader{files: map[string][]byte{}, dirs: map[string]struct{}{}}
}

// normPath maps OS-native lookup paths (built with filepath.Join, so `\` on
// Windows) onto the forward-slash keys the tests populate.
func normPath(p string) string { return filepath.ToSlash(filepath.Clean(p)) }

func (w *fakeReader) ReadFile(path string) ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	b, ok := w.files[normPath(path)]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

func (w *fakeReader) Exists(path string) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.files[normPath(path)]; ok {
		return true, nil
	}
	if _, ok := w.dirs[normPath(path)]; ok {
		return true, nil
	}
	return false, nil
}

func (w *fakeReader) mkdir(p string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dirs[normPath(p)] = struct{}{}
}

func renderAll(t *testing.T, skills []*adept.Skill) []adept.RenderOutput {
	t.Helper()
	r := New()
	parts := make([]adept.RenderOutput, 0, len(skills))
	for _, s := range skills {
		out, err := r.Render(context.Background(), adept.RenderInput{Skill: s})
		if err != nil {
			t.Fatalf("render %s: %v", s.ID, err)
		}
		// Eligible-only.
		if out.Path == "" {
			continue
		}
		parts = append(parts, out)
	}
	return parts
}

func TestAggregate_AlwaysBucket(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{
		{ID: "a", Description: "A", Activation: adept.ActivationAlways, Body: "a body\n"},
	})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(outs))
	}
	if outs[0].Path != ".github/instructions/always.instructions.md" {
		t.Fatalf("unexpected path: %s", outs[0].Path)
	}
	doc := string(outs[0].Bytes)
	if !strings.HasPrefix(doc, "---\napplyTo: \"**\"\n---") {
		t.Fatalf("expected applyTo:** frontmatter, got:\n%s", doc[:80])
	}
	if !strings.Contains(doc, "## A") {
		t.Fatalf("expected heading in doc:\n%s", doc)
	}
}

func TestAggregate_SameGlobsSameBucket(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{
		{ID: "a", Description: "A", Activation: adept.ActivationGlobs, Globs: []string{"src/**"}, Body: "a body\n"},
		{ID: "b", Description: "B", Activation: adept.ActivationGlobs, Globs: []string{"src/**"}, Body: "b body\n"},
	})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(outs))
	}
	doc := string(outs[0].Bytes)
	if !strings.Contains(doc, "## A") || !strings.Contains(doc, "## B") {
		t.Fatalf("expected both skills in same bucket file:\n%s", doc)
	}
	if !strings.Contains(doc, "applyTo: \"src/**\"") {
		t.Fatalf("expected glob applyTo, got:\n%s", doc[:120])
	}
}

func TestAggregate_DifferentGlobsDifferentBuckets(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{
		{ID: "ts", Description: "TS", Activation: adept.ActivationGlobs, Globs: []string{"**/*.ts"}, Body: "ts\n"},
		{ID: "go", Description: "Go", Activation: adept.ActivationGlobs, Globs: []string{"**/*.go"}, Body: "go\n"},
	})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(outs) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(outs))
	}
	// Output ordering must be deterministic (alphabetical by Path).
	if outs[0].Path > outs[1].Path {
		t.Fatalf("buckets not sorted by path: %s vs %s", outs[0].Path, outs[1].Path)
	}
}

func TestAggregate_AlwaysAndGlobsSeparate(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{
		{ID: "always", Description: "Always", Activation: adept.ActivationAlways, Body: "always body\n"},
		{ID: "scoped", Description: "Scoped", Activation: adept.ActivationGlobs, Globs: []string{"src/**"}, Body: "scoped body\n"},
	})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(outs) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(outs))
	}
}

func TestAggregate_AgentAndManualExcluded(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{
		{ID: "always", Description: "Always", Activation: adept.ActivationAlways, Body: "a\n"},
		{ID: "agent-x", Description: "Agent only", Activation: adept.ActivationAgent, Body: "x\n"},
		{ID: "manual-x", Description: "Manual only", Activation: adept.ActivationManual, Body: "x\n"},
	})
	if len(parts) != 1 {
		t.Fatalf("expected only 1 eligible part rendered, got %d", len(parts))
	}
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected 1 bucket (always-only), got %d", len(outs))
	}
}

func TestAggregate_Idempotent(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{
		{ID: "z", Description: "Z", Activation: adept.ActivationAlways, Body: "z\n"},
		{ID: "a", Description: "A", Activation: adept.ActivationAlways, Body: "a\n"},
	})
	o1, _ := a.Aggregate(context.Background(), parts, 0)
	o2, _ := a.Aggregate(context.Background(), parts, 0)
	if len(o1) != len(o2) {
		t.Fatalf("non-idempotent bucket count")
	}
	for i := range o1 {
		if string(o1[i].Bytes) != string(o2[i].Bytes) {
			t.Fatalf("Aggregate not idempotent at index %d", i)
		}
	}
}

func TestAggregate_SkillOrderWithinBucket(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	// Render in reverse-alpha order; bucket body must be alpha.
	parts := renderAll(t, []*adept.Skill{
		{ID: "zeta", Description: "Zeta", Activation: adept.ActivationAlways, Body: "z\n"},
		{ID: "alpha", Description: "Alpha", Activation: adept.ActivationAlways, Body: "a\n"},
	})
	outs, _ := a.Aggregate(context.Background(), parts, 0)
	doc := string(outs[0].Bytes)
	aIdx := strings.Index(doc, "id=alpha")
	zIdx := strings.Index(doc, "id=zeta")
	if !(aIdx < zIdx) {
		t.Fatalf("expected alpha before zeta, got a=%d z=%d", aIdx, zIdx)
	}
}

func TestAggregate_NegativeBudgetErrors(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{
		{ID: "a", Description: "A", Activation: adept.ActivationAlways, Body: "a\n"},
	})
	if _, err := a.Aggregate(context.Background(), parts, -1); err == nil {
		t.Fatalf("expected error for negative budget")
	}
}

func TestAggregate_Golden_Multi(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{
		{ID: "go-style", Description: "Go style", Activation: adept.ActivationAlways, Body: "Use gofmt.\n"},
		{ID: "api-design", Description: "API design", Activation: adept.ActivationAlways, Body: "REST first.\n"},
		{ID: "ts-rules", Description: "TS rules", Activation: adept.ActivationGlobs, Globs: []string{"**/*.ts"}, Body: "Strict mode.\n"},
	})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	for _, o := range outs {
		base := filepath.Base(o.Path)
		// Replace dots so the filename is filesystem-friendly across OSes.
		name := strings.ReplaceAll(base, ".", "_")
		goldenPath := filepath.Join("testdata", "aggregate-"+name+".golden")
		if *updateGolden {
			_ = os.MkdirAll(filepath.Dir(goldenPath), 0o755)
			if err := os.WriteFile(goldenPath, o.Bytes, 0o644); err != nil {
				t.Fatalf("write golden: %v", err)
			}
			continue
		}
		want, err := os.ReadFile(goldenPath)
		if err != nil {
			t.Fatalf("read golden %s: %v (run -update)", goldenPath, err)
		}
		if string(want) != string(o.Bytes) {
			t.Fatalf("golden mismatch for %s\nwant:\n%s\ngot:\n%s", goldenPath, string(want), string(o.Bytes))
		}
	}
}

// TestRoundTrip_RenderAggregateImport renders a skill, aggregates the bucket
// file, writes it to a fake project root, then imports it back. The recovered
// skill must carry the original id and body. This locks in the new
// `hash=<8-hex>` marker format end-to-end.
func TestRoundTrip_RenderAggregateImport(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	parts := renderAll(t, []*adept.Skill{
		{ID: "roundtrip", Description: "Round trip", Activation: adept.ActivationAlways, Body: "original markdown body\n"},
	})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(outs))
	}
	root := t.TempDir()
	abs := filepath.Join(root, outs[0].Path)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, outs[0].Bytes, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := a.Import(context.Background(), root)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 imported skill, got %d", len(got))
	}
	if got[0].Skill.ID != "roundtrip" {
		t.Fatalf("recovered id mismatch: got %q", got[0].Skill.ID)
	}
	if !strings.Contains(got[0].Skill.Body, "original markdown body") {
		t.Fatalf("recovered body missing original markdown: %q", got[0].Skill.Body)
	}
}

func TestSpec(t *testing.T) {
	a := NewAdapter(New(), budget.NewPacker(), nil)
	sp := a.Spec()
	if sp.ID != "copilot" || sp.Kind != adept.KindAggregatorPerGlob {
		t.Fatalf("bad spec: %+v", sp)
	}
	if sp.SizeBudgetB != SizeBudgetB {
		t.Fatalf("size budget: %d", sp.SizeBudgetB)
	}
	if sp.BaseDir != BucketDir {
		t.Fatalf("base dir: %s", sp.BaseDir)
	}
}

func TestDetect_CopilotInstructionsFile(t *testing.T) {
	w := newFakeReader()
	w.files["/proj/.github/copilot-instructions.md"] = []byte("x")
	a := NewAdapterWithDeps(New(), budget.NewPacker(), nil, nil, w)
	ok, err := a.Detect("/proj")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !ok {
		t.Fatalf("expected detect=true")
	}
}

func TestDetect_InstructionsDir(t *testing.T) {
	w := newFakeReader()
	w.mkdir("/proj/.github/instructions")
	a := NewAdapterWithDeps(New(), budget.NewPacker(), nil, nil, w)
	ok, err := a.Detect("/proj")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if !ok {
		t.Fatalf("expected detect=true")
	}
}

func TestDetect_None(t *testing.T) {
	w := newFakeReader()
	a := NewAdapterWithDeps(New(), budget.NewPacker(), nil, nil, w)
	ok, err := a.Detect("/proj")
	if err != nil {
		t.Fatalf("Detect error: %v", err)
	}
	if ok {
		t.Fatalf("expected detect=false")
	}
}

func TestValidate_Synced(t *testing.T) {
	w := newFakeReader()
	a := NewAdapterWithDeps(New(), budget.NewPacker(), nil, nil, w)
	parts := renderAll(t, []*adept.Skill{
		{ID: "x", Description: "X", Activation: adept.ActivationAlways, Body: "body\n"},
	})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	for _, o := range outs {
		w.files["/proj/"+o.Path] = o.Bytes
	}
	rep, err := a.Validate("/proj", outs)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Synced) != 1 || len(rep.Drifted) != 0 || len(rep.Missing) != 0 {
		t.Fatalf("expected single Synced, got %+v", rep)
	}
}

func TestValidate_DriftedAndMissing(t *testing.T) {
	w := newFakeReader()
	a := NewAdapterWithDeps(New(), budget.NewPacker(), nil, nil, w)
	parts := renderAll(t, []*adept.Skill{
		{ID: "x", Description: "X", Activation: adept.ActivationAlways, Body: "body\n"},
		{ID: "y", Description: "Y", Activation: adept.ActivationGlobs, Globs: []string{"src/**"}, Body: "body\n"},
	})
	outs, err := a.Aggregate(context.Background(), parts, 0)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	// First bucket exists but drifted; second missing entirely.
	w.files["/proj/"+outs[0].Path] = []byte("garbage")
	rep, err := a.Validate("/proj", outs)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(rep.Drifted) != 1 || len(rep.Missing) != 1 {
		t.Fatalf("expected 1 drifted, 1 missing, got %+v", rep)
	}
}
