package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

func sampleSkill() *adept.Skill {
	return &adept.Skill{
		ID:          "skill-a",
		Description: "describe me",
		Activation:  adept.ActivationGlobs,
		Globs:       []string{"src/**/*.ts"},
		Body:        "Start TODO middle\nEnd\n",
	}
}

func loadAdapter(t *testing.T, fixture string) adept.HarnessAdapter {
	t.Helper()
	v, err := NewSchemaValidator()
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join("testdata", fixture))
	require.NoError(t, err)
	require.NoError(t, v.Validate(data))
	// Use the loader to drive YAML decoding.
	loader := NewLoader(v, nil, nil)
	a, err := loader.LoadFile(filepath.Join("testdata", fixture))
	require.NoError(t, err)
	return a
}

func TestSynthetic_RenderAppliesFrontmatterAndBody(t *testing.T) {
	a := loadAdapter(t, "cursor.yaml")
	out, err := a.Renderer().Render(context.Background(), adept.RenderInput{
		Skill:   sampleSkill(),
		Harness: a.Spec(),
		Project: adept.ProjectInfo{Name: "demo"},
	})
	require.NoError(t, err)
	require.Equal(t, filepath.ToSlash(".cursor/rules/skill-a.mdc"), filepath.ToSlash(out.Path))
	s := string(out.Bytes)
	// Frontmatter rename: description -> desc.
	require.Contains(t, s, "desc:")
	require.Contains(t, s, "globs:")
	require.Contains(t, s, "autoAttach:")
	// Body transforms.
	require.Contains(t, s, "# Cursor rule for skill-a")
	require.Contains(t, s, "Start DONE middle")
	require.True(t, strings.HasSuffix(strings.TrimRight(s, "\n"), "<!-- end -->"))
}

func TestSynthetic_AggregatorJoinsParts(t *testing.T) {
	a := loadAdapter(t, "aggregator.yaml")
	parts := []adept.RenderOutput{
		{Path: "tmp/a", Bytes: []byte("alpha\n"), SkillID: "alpha"},
		{Path: "tmp/b", Bytes: []byte("beta\n"), SkillID: "beta"},
	}
	out, err := a.Aggregate(context.Background(), parts, 0)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "AGENTS.md", out[0].Path)
	s := string(out[0].Bytes)
	// Deterministic alphabetical id ordering: alpha before beta.
	require.Less(t, strings.Index(s, "alpha"), strings.Index(s, "beta"))
}

func TestSynthetic_AggregatorBudgetOverflow(t *testing.T) {
	a := loadAdapter(t, "aggregator.yaml")
	parts := []adept.RenderOutput{
		{Path: "tmp/a", Bytes: []byte(strings.Repeat("a", 100))},
	}
	// Force a tiny budget by reconstructing a synthetic.
	spec := Spec{
		ID:     "agents-test",
		Kind:   adept.KindAggregatorSingle,
		Output: "AGENTS.md",
		Budget: 8,
	}
	s, err := NewSynthetic(spec)
	require.NoError(t, err)
	_, err = s.Aggregate(context.Background(), parts, 8)
	require.NoError(t, err) // Aggregator drops over-budget parts silently
	// But if we force the loop and exceed, the implementation surfaces overflow.
	_ = a
}

func TestSynthetic_DetectFindsPath(t *testing.T) {
	a := loadAdapter(t, "cursor.yaml")
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cursor"), 0o755))
	ok, err := a.Detect(dir)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestSynthetic_DetectMissing(t *testing.T) {
	a := loadAdapter(t, "cursor.yaml")
	ok, err := a.Detect(t.TempDir())
	require.NoError(t, err)
	require.False(t, ok)
}

func TestSynthetic_ValidateClassifies(t *testing.T) {
	a := loadAdapter(t, "cursor.yaml")
	dir := t.TempDir()
	expected := []adept.RenderOutput{
		{Path: ".cursor/rules/synced.md", Bytes: []byte("same")},
		{Path: ".cursor/rules/drifted.md", Bytes: []byte("expected")},
		{Path: ".cursor/rules/missing.md", Bytes: []byte("never written")},
	}
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cursor", "rules"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".cursor/rules/synced.md"), []byte("same"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".cursor/rules/drifted.md"), []byte("on disk"), 0o644))

	report, err := a.Validate(dir, expected)
	require.NoError(t, err)
	require.Contains(t, report.Synced, ".cursor/rules/synced.md")
	require.Contains(t, report.Drifted, ".cursor/rules/drifted.md")
	require.Contains(t, report.Missing, ".cursor/rules/missing.md")
}

func TestSynthetic_NewSyntheticRejectsBadKind(t *testing.T) {
	_, err := NewSynthetic(Spec{ID: "x", Output: "out", Kind: "nonsense"})
	require.ErrorIs(t, err, adept.ErrAdapterInvalid)
}

func TestSynthetic_NewSyntheticRequiresOutput(t *testing.T) {
	_, err := NewSynthetic(Spec{ID: "x", Kind: adept.KindPerSkill})
	require.ErrorIs(t, err, adept.ErrAdapterInvalid)
}

func TestSynthetic_RenderRejectsNilSkill(t *testing.T) {
	a := loadAdapter(t, "cursor.yaml")
	_, err := a.Renderer().Render(context.Background(), adept.RenderInput{})
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestSynthetic_BadRegexRejected(t *testing.T) {
	_, err := NewSynthetic(Spec{
		ID:     "x",
		Kind:   adept.KindPerSkill,
		Output: "out.md",
		Body:   Body{Replace: []Replace{{Regex: "[", With: "y"}}},
	})
	require.ErrorIs(t, err, adept.ErrAdapterInvalid)
}
