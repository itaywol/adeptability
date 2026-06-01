package cursor_test

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/itaywol/adeptability/internal/render/cursor"
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
	r := cursor.New()

	cases := []string{"always", "globs", "agent", "manual"}
	for _, name := range cases {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			skill := loadFixture(t, name)
			out, err := r.Render(context.Background(), adept.RenderInput{
				Skill:   skill,
				Harness: cursor.Spec(),
				Project: adept.ProjectInfo{Name: "demo"},
			})
			require.NoError(t, err)
			require.Equal(t, ".cursor/rules/"+skill.ID+".mdc", out.Path)
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

func TestRenderer_AlwaysApply(t *testing.T) {
	t.Parallel()
	out, err := cursor.New().Render(context.Background(), adept.RenderInput{
		Skill: loadFixture(t, "always"),
	})
	require.NoError(t, err)
	require.Contains(t, string(out.Bytes), "alwaysApply: true")
	require.NotContains(t, string(out.Bytes), "globs:")
}

func TestRenderer_GlobsBody(t *testing.T) {
	t.Parallel()
	out, err := cursor.New().Render(context.Background(), adept.RenderInput{
		Skill: loadFixture(t, "globs"),
	})
	require.NoError(t, err)
	require.Contains(t, string(out.Bytes), "globs:")
	require.Contains(t, string(out.Bytes), "alwaysApply: false")
}

func TestRenderer_ManualUserHint(t *testing.T) {
	t.Parallel()
	skill := loadFixture(t, "manual")
	out, err := cursor.New().Render(context.Background(), adept.RenderInput{Skill: skill})
	require.NoError(t, err)
	require.Contains(t, string(out.Bytes), "@rotate-secrets")
	require.NotContains(t, string(out.Bytes), "alwaysApply")
}

func TestRenderer_SidecarsDroppedWithWarning(t *testing.T) {
	t.Parallel()
	skill := loadFixture(t, "always")
	skill.Files = []adept.SkillFile{
		{RelPath: "scripts/h.sh", Bytes: []byte("x"), Mode: 0o755},
		{RelPath: "references/a.md", Bytes: []byte("y"), Mode: 0o644},
	}
	out, err := cursor.New().Render(context.Background(), adept.RenderInput{Skill: skill})
	require.NoError(t, err)
	require.Empty(t, out.Sidecars)
	require.Len(t, out.Warnings, 2)
	require.Contains(t, out.Warnings[0], "scripts/h.sh")
}

func TestRenderer_GlobsRequiresGlobs(t *testing.T) {
	t.Parallel()
	skill := loadFixture(t, "globs")
	skill.Globs = nil
	_, err := cursor.New().Render(context.Background(), adept.RenderInput{Skill: skill})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestRenderer_NilSkill(t *testing.T) {
	t.Parallel()
	_, err := cursor.New().Render(context.Background(), adept.RenderInput{Skill: nil})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}
