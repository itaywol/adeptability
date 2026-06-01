package common_test

import (
	"testing"

	"github.com/itaywol/adeptability/internal/render/common"
	"github.com/stretchr/testify/require"
)

func TestPathTemplater_Resolve(t *testing.T) {
	t.Parallel()
	tpl := common.NewPathTemplater()

	cases := []struct {
		name     string
		template string
		skillID  string
		want     string
	}{
		{"claude", ".claude/skills/{id}/SKILL.md", "review", ".claude/skills/review/SKILL.md"},
		{"cursor", ".cursor/rules/{id}.mdc", "lint-ts", ".cursor/rules/lint-ts.mdc"},
		{"opencode", ".opencode/skill/{id}/SKILL.md", "deploy", ".opencode/skill/deploy/SKILL.md"},
		{"no placeholder", "AGENTS.md", "ignored", "AGENTS.md"},
		{"multiple placeholders", "{id}/{id}.md", "a", "a/a.md"},
		{"empty id", ".x/{id}/SKILL.md", "", ".x//SKILL.md"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tpl.Resolve(tc.template, tc.skillID)
			require.Equal(t, tc.want, got)
		})
	}
}
