package cli

import (
	"reflect"
	"testing"
)

func TestChangedSkillIDs(t *testing.T) {
	got := changedSkillIDs([]string{
		"skills/pr-review/SKILL.md",
		"skills/pr-review/scripts/run.sh", // same skill, second file -> deduped
		"skills/lint-style/SKILL.md",
		"README.md",          // not under skills/ -> ignored
		"skills",             // bare dir, no id -> ignored
		"docs/skills/x/y.md", // skills/ not the first segment -> ignored
	})
	want := []string{"lint-style", "pr-review"} // sorted, unique
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changedSkillIDs = %v, want %v", got, want)
	}
}
