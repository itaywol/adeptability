package claude_test

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/internal/render/claude"
	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "update golden files")

type skillFixture struct {
	ID           string               `yaml:"id"`
	Description  string               `yaml:"description"`
	Activation   adept.ActivationMode `yaml:"activation"`
	Globs        []string             `yaml:"globs,omitempty"`
	AllowedTools []string             `yaml:"allowed-tools,omitempty"`
	Body         string               `yaml:"body"`
}

func loadFixture(t *testing.T, name string) *adept.Skill {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".skill.yaml"))
	require.NoError(t, err)
	var f skillFixture
	require.NoError(t, yaml.Unmarshal(raw, &f))
	return &adept.Skill{
		ID:           f.ID,
		Description:  f.Description,
		Activation:   f.Activation,
		Globs:        f.Globs,
		AllowedTools: f.AllowedTools,
		Body:         f.Body,
	}
}

func TestRenderer_Golden(t *testing.T) {
	t.Parallel()
	r := claude.New()

	cases := []string{"always", "globs", "agent", "manual"}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			skill := loadFixture(t, name)
			out, err := r.Render(context.Background(), adept.RenderInput{
				Skill:   skill,
				Harness: claude.Spec(),
				Project: adept.ProjectInfo{Name: "demo", Root: "/tmp/demo"},
			})
			require.NoError(t, err)
			require.Equal(t, ".claude/skills/"+skill.ID+"/SKILL.md", out.Path)
			require.Equal(t, skill.ID, out.SkillID)
			require.Len(t, out.SkillHash, 8)

			goldenPath := filepath.Join("testdata", name+".golden")
			if *update {
				require.NoError(t, os.WriteFile(goldenPath, out.Bytes, 0o644))
				return
			}
			want, err := os.ReadFile(goldenPath)
			require.NoError(t, err, "missing golden; rerun with -update")
			require.Equal(t, string(want), string(out.Bytes))
		})
	}
}

func TestRenderer_ManualSetsDisable(t *testing.T) {
	t.Parallel()
	skill := loadFixture(t, "manual")
	out, err := claude.New().Render(context.Background(), adept.RenderInput{Skill: skill, Harness: claude.Spec()})
	require.NoError(t, err)
	require.Contains(t, string(out.Bytes), "disable-model-invocation: true")
}

func TestRenderer_GlobsEmbedsHint(t *testing.T) {
	t.Parallel()
	skill := loadFixture(t, "globs")
	out, err := claude.New().Render(context.Background(), adept.RenderInput{Skill: skill, Harness: claude.Spec()})
	require.NoError(t, err)
	require.Contains(t, string(out.Bytes), "matches: **/*.ts, **/*.tsx")
}

func TestRenderer_PreservesAllowedTools(t *testing.T) {
	t.Parallel()
	skill := loadFixture(t, "always")
	out, err := claude.New().Render(context.Background(), adept.RenderInput{Skill: skill, Harness: claude.Spec()})
	require.NoError(t, err)
	require.Contains(t, string(out.Bytes), "allowed-tools: [Read, Grep]")
}

func TestRenderer_Errors(t *testing.T) {
	t.Parallel()
	r := claude.New()
	_, err := r.Render(context.Background(), adept.RenderInput{Skill: nil})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)

	_, err = r.Render(context.Background(), adept.RenderInput{Skill: &adept.Skill{}})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)

	_, err = r.Render(context.Background(), adept.RenderInput{Skill: &adept.Skill{ID: "x"}})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestRenderer_Sidecars(t *testing.T) {
	t.Parallel()
	skill := loadFixture(t, "always")
	skill.Files = []adept.SkillFile{
		{RelPath: "scripts/helper.sh", Bytes: []byte("#!/bin/sh\n"), Mode: 0o755},
		{RelPath: "references/spec.md", Bytes: []byte("see spec"), Mode: 0o644},
	}
	out, err := claude.New().Render(context.Background(), adept.RenderInput{Skill: skill, Harness: claude.Spec()})
	require.NoError(t, err)
	require.Len(t, out.Sidecars, 2)
	require.Equal(t, "scripts/helper.sh", out.Sidecars[0].RelPath)
}
