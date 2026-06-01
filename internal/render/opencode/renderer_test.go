package opencode_test

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/internal/render/opencode"
	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "update golden files")

type skillFixture struct {
	ID          string               `yaml:"id"`
	Description string               `yaml:"description"`
	Activation  adept.ActivationMode `yaml:"activation"`
	Globs       []string             `yaml:"globs,omitempty"`
	Body        string               `yaml:"body"`
}

func loadFixture(t *testing.T, name string) *adept.Skill {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name+".skill.yaml"))
	require.NoError(t, err)
	var f skillFixture
	require.NoError(t, yaml.Unmarshal(raw, &f))
	return &adept.Skill{
		ID:          f.ID,
		Description: f.Description,
		Activation:  f.Activation,
		Globs:       f.Globs,
		Body:        f.Body,
	}
}

func TestRenderer_Golden(t *testing.T) {
	t.Parallel()
	r := opencode.New()

	cases := []string{"always", "globs", "agent", "manual"}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			skill := loadFixture(t, name)
			out, err := r.Render(context.Background(), adept.RenderInput{
				Skill:   skill,
				Harness: opencode.Spec(),
				Project: adept.ProjectInfo{Name: "demo"},
			})
			require.NoError(t, err)
			require.Equal(t, ".opencode/skill/"+skill.ID+"/SKILL.md", out.Path)
			require.Equal(t, skill.ID, out.SkillID)

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

func TestRenderer_HasHeading(t *testing.T) {
	t.Parallel()
	out, err := opencode.New().Render(context.Background(), adept.RenderInput{Skill: loadFixture(t, "always")})
	require.NoError(t, err)
	require.True(t, len(out.Bytes) > 0)
	require.Equal(t, "# read-first\n", string(out.Bytes[:len("# read-first\n")]))
}

func TestRenderer_NoFrontmatter(t *testing.T) {
	t.Parallel()
	out, err := opencode.New().Render(context.Background(), adept.RenderInput{Skill: loadFixture(t, "globs")})
	require.NoError(t, err)
	require.NotContains(t, string(out.Bytes), "---\n")
}

func TestRenderer_Sidecars(t *testing.T) {
	t.Parallel()
	skill := loadFixture(t, "always")
	skill.Files = []adept.SkillFile{
		{RelPath: "scripts/run.sh", Bytes: []byte("#!/bin/sh\n"), Mode: 0o755},
	}
	out, err := opencode.New().Render(context.Background(), adept.RenderInput{Skill: skill})
	require.NoError(t, err)
	require.Len(t, out.Sidecars, 1)
	require.Equal(t, "scripts/run.sh", out.Sidecars[0].RelPath)
}

func TestRenderer_Errors(t *testing.T) {
	t.Parallel()
	r := opencode.New()
	_, err := r.Render(context.Background(), adept.RenderInput{Skill: nil})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
	_, err = r.Render(context.Background(), adept.RenderInput{Skill: &adept.Skill{}})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
	_, err = r.Render(context.Background(), adept.RenderInput{Skill: &adept.Skill{ID: "x"}})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}
