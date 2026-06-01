package codex

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/itaywol/adeptability/internal/budget"
	"github.com/itaywol/adeptability/pkg/adept"
)

// -update regenerates golden files for renderer + aggregator tests.
var updateGolden = flag.Bool("update", false, "regenerate codex golden files")

func TestRenderer_BasicSkill(t *testing.T) {
	r := New()
	s := &adept.Skill{
		ID:          "go-style",
		Description: "Go style guide for project",
		Activation:  adept.ActivationAlways,
		Body:        "Use gofmt. Prefer explicit errors.\n",
	}
	out, err := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if out.Path != OutputFile {
		t.Fatalf("expected path %q, got %q", OutputFile, out.Path)
	}
	if out.SkillID != "go-style" {
		t.Fatalf("expected SkillID preserved, got %q", out.SkillID)
	}
	if len(out.SkillHash) != 8 {
		t.Fatalf("expected 8-char SkillHash, got %q", out.SkillHash)
	}
	got := string(out.Bytes)
	if !regexp.MustCompile(`<!-- adeptability:begin id=go-style hash=[a-f0-9]{8} -->`).MatchString(got) {
		t.Fatalf("missing begin marker with hash:\n%s", got)
	}
	if !strings.Contains(got, "<!-- adeptability:end id=go-style -->") {
		t.Fatalf("missing end marker:\n%s", got)
	}
	if !strings.Contains(got, "## Go style guide for project") {
		t.Fatalf("missing heading:\n%s", got)
	}
	if !strings.Contains(got, "Use gofmt.") {
		t.Fatalf("missing body:\n%s", got)
	}
	if strings.Contains(got, "version=") {
		t.Fatalf("rendered output must not contain version= marker:\n%s", got)
	}
}

func TestRenderer_Golden_Simple(t *testing.T) {
	assertGolden(t, "simple",
		&adept.Skill{
			ID:          "simple-skill",
			Description: "Simple skill description",
			Activation:  adept.ActivationAlways,
			Body:        "Body line one.\nBody line two.\n",
		},
	)
}

func TestRenderer_Golden_EmptyBody(t *testing.T) {
	assertGolden(t, "empty-body",
		&adept.Skill{
			ID:          "headless",
			Description: "Description-only skill",
			Activation:  adept.ActivationAlways,
			Body:        "",
		},
	)
}

func TestRenderer_NilSkill(t *testing.T) {
	r := New()
	_, err := r.Render(context.Background(), adept.RenderInput{Skill: nil})
	if err == nil || !errors.Is(err, adept.ErrSkillInvalid) {
		t.Fatalf("expected ErrSkillInvalid, got %v", err)
	}
}

func TestRenderer_MissingID(t *testing.T) {
	r := New()
	_, err := r.Render(context.Background(), adept.RenderInput{Skill: &adept.Skill{}})
	if err == nil || !errors.Is(err, adept.ErrSkillInvalid) {
		t.Fatalf("expected ErrSkillInvalid, got %v", err)
	}
}

func TestRenderer_EmptyDescriptionFallsBackToID(t *testing.T) {
	r := New()
	s := &adept.Skill{ID: "x", Body: "body\n"}
	out, _ := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if !strings.Contains(string(out.Bytes), "## x") {
		t.Fatalf("expected fallback heading '## x', got:\n%s", string(out.Bytes))
	}
}

func TestRenderer_Idempotent(t *testing.T) {
	r := New()
	s := &adept.Skill{
		ID:          "idem",
		Description: "Idempotent test",
		Body:        "body\n",
	}
	out1, _ := r.Render(context.Background(), adept.RenderInput{Skill: s})
	out2, _ := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if string(out1.Bytes) != string(out2.Bytes) {
		t.Fatalf("Render not idempotent")
	}
}

// TestRenderer_RoundTrip renders a skill, runs the importer over the output,
// and asserts that the recovered skill carries the same id and body.
func TestRenderer_RoundTrip(t *testing.T) {
	r := New()
	a := NewAdapter(r, budget.NewPacker(), nil)
	s := &adept.Skill{
		ID:          "round-trip",
		Description: "Round trip skill",
		Activation:  adept.ActivationAlways,
		Body:        "core body content",
	}
	part, err := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Build the aggregator output and write it to a temp project root.
	root := t.TempDir()
	// Reuse the aggregator on a one-element slice.
	outs, err := a.Aggregate(context.Background(), []adept.RenderOutput{part}, 0)
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("expected one aggregated output, got %d", len(outs))
	}
	agentsPath := filepath.Join(root, OutputFile)
	if err := os.WriteFile(agentsPath, outs[0].Bytes, 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}
	got, err := a.Import(context.Background(), root)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one imported skill, got %d", len(got))
	}
	if got[0].Skill.ID != s.ID {
		t.Fatalf("recovered id mismatch: want %q got %q", s.ID, got[0].Skill.ID)
	}
	if !strings.Contains(got[0].Skill.Body, "core body content") {
		t.Fatalf("recovered body missing original markdown: %q", got[0].Skill.Body)
	}
}

// assertGolden renders a skill and compares against testdata/<name>.golden.
// With -update, regenerates the golden.
func assertGolden(t *testing.T, name string, s *adept.Skill) {
	t.Helper()
	r := New()
	out, err := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	goldenPath := filepath.Join("testdata", name+".golden")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, out.Bytes, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create)", goldenPath, err)
	}
	if string(want) != string(out.Bytes) {
		t.Fatalf("golden mismatch for %s\nwant:\n%s\ngot:\n%s", name, string(want), string(out.Bytes))
	}
}
