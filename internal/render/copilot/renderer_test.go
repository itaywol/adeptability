package copilot

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
var updateGolden = flag.Bool("update", false, "regenerate copilot golden files")

func TestRenderer_AlwaysSkill(t *testing.T) {
	r := New()
	s := &adept.Skill{
		ID:          "go-style",
		Version:     2,
		Description: "Always-on Go rules",
		Activation:  adept.ActivationAlways,
		Body:        "Use gofmt.\n",
	}
	out, err := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if out.Path != ".github/instructions/always.instructions.md" {
		t.Fatalf("unexpected path: %s", out.Path)
	}
	if out.SkillID != "go-style" || out.SkillVersion != 2 {
		t.Fatalf("missing skill metadata: %+v", out)
	}
	// Meta sidecar carries applyTo for the aggregator.
	var foundMeta bool
	for _, sc := range out.Sidecars {
		if sc.RelPath == metaSidecarName {
			foundMeta = true
			if string(sc.Bytes) != "**" {
				t.Fatalf("expected applyTo=**, got %q", string(sc.Bytes))
			}
		}
	}
	if !foundMeta {
		t.Fatalf("expected meta sidecar")
	}
}

func TestRenderer_GlobsSkill(t *testing.T) {
	r := New()
	s := &adept.Skill{
		ID:          "ts-rules",
		Version:     1,
		Description: "TypeScript rules",
		Activation:  adept.ActivationGlobs,
		Globs:       []string{"src/**/*.ts", "**/*.tsx"},
		Body:        "Strict mode.\n",
	}
	out, err := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if !strings.HasPrefix(out.Path, ".github/instructions/bucket-") {
		t.Fatalf("expected hashed bucket path, got %s", out.Path)
	}
	// Meta sidecar must carry sorted, comma-joined globs.
	var meta string
	for _, sc := range out.Sidecars {
		if sc.RelPath == metaSidecarName {
			meta = string(sc.Bytes)
		}
	}
	if meta != "**/*.tsx,src/**/*.ts" {
		t.Fatalf("expected sorted applyTo, got %q", meta)
	}
}

func TestRenderer_AgentSkillIsSkipped(t *testing.T) {
	r := New()
	s := &adept.Skill{ID: "x", Version: 1, Activation: adept.ActivationAgent, Body: "x"}
	out, err := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if out.Path != "" || len(out.Bytes) != 0 {
		t.Fatalf("expected empty output for non-eligible skill, got %+v", out)
	}
}

func TestRenderer_ManualSkillIsSkipped(t *testing.T) {
	r := New()
	s := &adept.Skill{ID: "x", Version: 1, Activation: adept.ActivationManual, Body: "x"}
	out, _ := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if out.Path != "" {
		t.Fatalf("expected empty output for manual skill")
	}
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
	_, err := r.Render(context.Background(), adept.RenderInput{Skill: &adept.Skill{Activation: adept.ActivationAlways}})
	if err == nil || !errors.Is(err, adept.ErrSkillInvalid) {
		t.Fatalf("expected ErrSkillInvalid, got %v", err)
	}
}

func TestRenderer_Idempotent(t *testing.T) {
	r := New()
	s := &adept.Skill{ID: "idem", Version: 1, Description: "Idem", Activation: adept.ActivationAlways, Body: "body\n"}
	o1, _ := r.Render(context.Background(), adept.RenderInput{Skill: s})
	o2, _ := r.Render(context.Background(), adept.RenderInput{Skill: s})
	if string(o1.Bytes) != string(o2.Bytes) {
		t.Fatalf("Render not idempotent")
	}
}

func TestRenderer_Golden_Always(t *testing.T) {
	assertGolden(t, "always",
		&adept.Skill{
			ID:          "go-style",
			Version:     2,
			Description: "Go style guide",
			Activation:  adept.ActivationAlways,
			Body:        "Use gofmt.\nPrefer explicit errors.\n",
		},
	)
}

func TestRenderer_Golden_Globs(t *testing.T) {
	assertGolden(t, "globs",
		&adept.Skill{
			ID:          "ts-rules",
			Version:     1,
			Description: "TypeScript rules",
			Activation:  adept.ActivationGlobs,
			Globs:       []string{"src/**/*.ts"},
			Body:        "Strict mode only.\n",
		},
	)
}

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
		t.Fatalf("read golden %s: %v (run -update)", goldenPath, err)
	}
	if string(want) != string(out.Bytes) {
		t.Fatalf("golden mismatch for %s\nwant:\n%s\ngot:\n%s", name, string(want), string(out.Bytes))
	}
}
