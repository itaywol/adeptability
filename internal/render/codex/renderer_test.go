package codex

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
)

// -update regenerates golden files for renderer + aggregator tests.
var updateGolden = flag.Bool("update", false, "regenerate codex golden files")

func TestRenderer_BasicSkill(t *testing.T) {
	r := New()
	s := &adept.Skill{
		ID:          "go-style",
		Version:     2,
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
	if out.SkillID != "go-style" || out.SkillVersion != 2 {
		t.Fatalf("expected SkillID/Version preserved, got %q v%d", out.SkillID, out.SkillVersion)
	}
	got := string(out.Bytes)
	if !strings.Contains(got, "<!-- adeptability:begin id=go-style version=2 -->") {
		t.Fatalf("missing begin marker:\n%s", got)
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
}

func TestRenderer_Golden_Simple(t *testing.T) {
	assertGolden(t, "simple",
		&adept.Skill{
			ID:          "simple-skill",
			Version:     1,
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
			Version:     5,
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
	_, err := r.Render(context.Background(), adept.RenderInput{Skill: &adept.Skill{Version: 1}})
	if err == nil || !errors.Is(err, adept.ErrSkillInvalid) {
		t.Fatalf("expected ErrSkillInvalid, got %v", err)
	}
}

func TestRenderer_EmptyDescriptionFallsBackToID(t *testing.T) {
	r := New()
	s := &adept.Skill{ID: "x", Version: 1, Body: "body\n"}
	out, _ := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if !strings.Contains(string(out.Bytes), "## x") {
		t.Fatalf("expected fallback heading '## x', got:\n%s", string(out.Bytes))
	}
}

func TestRenderer_Idempotent(t *testing.T) {
	r := New()
	s := &adept.Skill{
		ID:          "idem",
		Version:     1,
		Description: "Idempotent test",
		Body:        "body\n",
	}
	out1, _ := r.Render(context.Background(), adept.RenderInput{Skill: s})
	out2, _ := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if string(out1.Bytes) != string(out2.Bytes) {
		t.Fatalf("Render not idempotent")
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
