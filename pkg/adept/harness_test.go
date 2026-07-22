package adept_test

import (
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
)

// TestHarnessSpec_GlobalFields verifies the HarnessSpec struct exposes the
// GlobalOutput/GlobalBaseDir fields introduced for global scope support, and
// that they default to the empty string (not global-capable) when unset.
func TestHarnessSpec_GlobalFields(t *testing.T) {
	t.Parallel()
	var s adept.HarnessSpec
	if s.GlobalOutput != "" {
		t.Fatalf("zero-value GlobalOutput = %q, want empty", s.GlobalOutput)
	}
	if s.GlobalBaseDir != "" {
		t.Fatalf("zero-value GlobalBaseDir = %q, want empty", s.GlobalBaseDir)
	}

	s = adept.HarnessSpec{
		GlobalOutput:  ".claude/skills/{id}/SKILL.md",
		GlobalBaseDir: ".claude",
	}
	if s.GlobalOutput != ".claude/skills/{id}/SKILL.md" {
		t.Fatalf("GlobalOutput = %q", s.GlobalOutput)
	}
	if s.GlobalBaseDir != ".claude" {
		t.Fatalf("GlobalBaseDir = %q", s.GlobalBaseDir)
	}
}
