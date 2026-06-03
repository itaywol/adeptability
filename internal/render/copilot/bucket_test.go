package copilot

import (
	"strings"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
)

func TestBucketer_AlwaysSkill(t *testing.T) {
	b := NewBucketer()
	s := &adept.Skill{ID: "x", Activation: adept.ActivationAlways}
	spec, ok := b.KeyFor(s)
	if !ok {
		t.Fatalf("expected eligible")
	}
	if spec.Key != AlwaysKey {
		t.Fatalf("expected AlwaysKey, got %q", spec.Key)
	}
	if spec.ApplyTo != "**" {
		t.Fatalf("expected applyTo=**, got %q", spec.ApplyTo)
	}
	if spec.Path != ".github/instructions/always.instructions.md" {
		t.Fatalf("unexpected path: %s", spec.Path)
	}
}

func TestBucketer_GlobsSkill(t *testing.T) {
	b := NewBucketer()
	s := &adept.Skill{
		ID:         "y",
		Activation: adept.ActivationGlobs,
		Globs:      []string{"**/*.ts", "src/**"},
	}
	spec, ok := b.KeyFor(s)
	if !ok {
		t.Fatalf("expected eligible")
	}
	if !strings.HasPrefix(string(spec.Key), "bucket-") {
		t.Fatalf("expected bucket- prefix, got %q", spec.Key)
	}
	if len(spec.Key) != len("bucket-")+globHashHexLen {
		t.Fatalf("expected %d hex chars suffix, got %q", globHashHexLen, spec.Key)
	}
	// Sorted globs are in the applyTo string.
	if spec.ApplyTo != "**/*.ts,src/**" {
		t.Fatalf("expected sorted applyTo, got %q", spec.ApplyTo)
	}
}

func TestBucketer_SameGlobsSameBucket(t *testing.T) {
	b := NewBucketer()
	s1 := &adept.Skill{ID: "a", Activation: adept.ActivationGlobs, Globs: []string{"src/**", "*.go"}}
	s2 := &adept.Skill{ID: "b", Activation: adept.ActivationGlobs, Globs: []string{"*.go", "src/**"}}
	spec1, _ := b.KeyFor(s1)
	spec2, _ := b.KeyFor(s2)
	if spec1.Key != spec2.Key {
		t.Fatalf("expected identical bucket for same globs (different order): %q vs %q", spec1.Key, spec2.Key)
	}
}

func TestBucketer_DifferentGlobsDifferentBucket(t *testing.T) {
	b := NewBucketer()
	s1 := &adept.Skill{ID: "a", Activation: adept.ActivationGlobs, Globs: []string{"src/**"}}
	s2 := &adept.Skill{ID: "b", Activation: adept.ActivationGlobs, Globs: []string{"docs/**"}}
	spec1, _ := b.KeyFor(s1)
	spec2, _ := b.KeyFor(s2)
	if spec1.Key == spec2.Key {
		t.Fatalf("expected different buckets for different globs")
	}
}

func TestBucketer_DeduplicateGlobs(t *testing.T) {
	b := NewBucketer()
	s := &adept.Skill{
		Activation: adept.ActivationGlobs,
		Globs:      []string{"src/**", "src/**", "*.go", "*.go"},
	}
	spec, _ := b.KeyFor(s)
	if spec.ApplyTo != "*.go,src/**" {
		t.Fatalf("expected dedup+sorted, got %q", spec.ApplyTo)
	}
}

func TestBucketer_AgentNotEligible(t *testing.T) {
	b := NewBucketer()
	s := &adept.Skill{ID: "z", Activation: adept.ActivationAgent}
	if _, ok := b.KeyFor(s); ok {
		t.Fatalf("expected agent skill not eligible")
	}
}

func TestBucketer_ManualNotEligible(t *testing.T) {
	b := NewBucketer()
	s := &adept.Skill{ID: "z", Activation: adept.ActivationManual}
	if _, ok := b.KeyFor(s); ok {
		t.Fatalf("expected manual skill not eligible")
	}
}

func TestBucketer_GlobsWithoutGlobsNotEligible(t *testing.T) {
	b := NewBucketer()
	s := &adept.Skill{Activation: adept.ActivationGlobs, Globs: nil}
	if _, ok := b.KeyFor(s); ok {
		t.Fatalf("expected globs activation with empty globs not eligible")
	}
}

func TestBucketer_GlobsWithBlankEntriesIgnored(t *testing.T) {
	b := NewBucketer()
	s := &adept.Skill{Activation: adept.ActivationGlobs, Globs: []string{"  ", "src/**"}}
	spec, ok := b.KeyFor(s)
	if !ok {
		t.Fatalf("expected eligible after dropping blanks")
	}
	if spec.ApplyTo != "src/**" {
		t.Fatalf("expected blanks dropped, got %q", spec.ApplyTo)
	}
}

func TestBucketer_NilSkill(t *testing.T) {
	b := NewBucketer()
	if _, ok := b.KeyFor(nil); ok {
		t.Fatalf("expected nil skill not eligible")
	}
}

func TestBucketer_DeterministicHash(t *testing.T) {
	b := NewBucketer()
	s := &adept.Skill{Activation: adept.ActivationGlobs, Globs: []string{"**/*.go"}}
	spec1, _ := b.KeyFor(s)
	spec2, _ := b.KeyFor(s)
	if spec1.Key != spec2.Key {
		t.Fatalf("hash not deterministic across calls")
	}
}
