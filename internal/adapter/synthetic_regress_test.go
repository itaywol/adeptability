package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

// TestSynthetic_AggregatorEmitsTruncationManifestOnDrop is a regression test for
// the silent-drop bug: when a size budget forces skills to be omitted from an
// aggregator-single output, the dropped skill ids must be surfaced in a leading
// truncation manifest comment (like the codex aggregator) rather than vanishing
// without a trace.
func TestSynthetic_AggregatorEmitsTruncationManifestOnDrop(t *testing.T) {
	spec := Spec{
		ID:     "agents-trunc",
		Kind:   adept.KindAggregatorSingle,
		Output: "AGENTS.md",
		Budget: 40,
	}
	a, err := NewSynthetic(spec)
	require.NoError(t, err)

	parts := []adept.RenderOutput{
		{Path: "AGENTS.md", Bytes: []byte("alpha body\n"), SkillID: "alpha"},
		{Path: "AGENTS.md", Bytes: []byte(strings.Repeat("z", 200) + "\n"), SkillID: "huge"},
		{Path: "AGENTS.md", Bytes: []byte("gamma body\n"), SkillID: "gamma"},
	}

	out, err := a.Aggregate(context.Background(), parts, 40)
	require.NoError(t, err)
	require.Len(t, out, 1)
	s := string(out[0].Bytes)

	// The over-budget "huge" skill is dropped and named in the manifest.
	require.Contains(t, s, "adeptability: omitted")
	require.Contains(t, s, "huge")
	// Smaller skills that fit after skipping the huge one are still kept
	// (regression on the old `break`, which would have dropped gamma too).
	require.Contains(t, s, "alpha body")
	require.Contains(t, s, "gamma body")
	require.NotContains(t, s, strings.Repeat("z", 200))
}

// TestSynthetic_AggregatorNoManifestWhenAllFit confirms the manifest is only
// emitted when something is actually dropped — clean output otherwise.
func TestSynthetic_AggregatorNoManifestWhenAllFit(t *testing.T) {
	spec := Spec{
		ID:     "agents-fit",
		Kind:   adept.KindAggregatorSingle,
		Output: "AGENTS.md",
		Budget: 0, // unlimited
	}
	a, err := NewSynthetic(spec)
	require.NoError(t, err)

	parts := []adept.RenderOutput{
		{Path: "AGENTS.md", Bytes: []byte("alpha\n"), SkillID: "alpha"},
		{Path: "AGENTS.md", Bytes: []byte("beta\n"), SkillID: "beta"},
	}
	out, err := a.Aggregate(context.Background(), parts, 0)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.NotContains(t, string(out[0].Bytes), "adeptability: omitted")
}

// TestSynthetic_AggregatorManifestRoundTripsOnImport ensures the truncation
// manifest comment is inert on re-import: it is not mistaken for an adept
// section marker, so the kept skills still import and the dropped one simply
// does not reappear (it is documented in the comment, not parsed as a skill).
func TestSynthetic_AggregatorManifestRoundTripsOnImport(t *testing.T) {
	spec := Spec{
		ID:     "agents-rt",
		Kind:   adept.KindAggregatorSingle,
		Output: "AGENTS.md",
		Budget: 60,
	}
	a, err := NewSynthetic(spec)
	require.NoError(t, err)

	// Use real adept section markers so the kept content imports as skills.
	keep := "<!-- adeptability:begin id=alpha hash=deadbeef -->\n## alpha\nbody\n<!-- adeptability:end id=alpha -->\n"
	parts := []adept.RenderOutput{
		{Path: "AGENTS.md", Bytes: []byte(keep), SkillID: "alpha"},
		{Path: "AGENTS.md", Bytes: []byte(strings.Repeat("z", 300) + "\n"), SkillID: "huge"},
	}
	out, err := a.Aggregate(context.Background(), parts, 200)
	require.NoError(t, err)
	require.Len(t, out, 1)
	body := string(out[0].Bytes)
	require.Contains(t, body, "adeptability: omitted")
	require.Contains(t, body, "huge")

	// The manifest comment must not be parsed as a begin marker.
	require.Equal(t, 1, strings.Count(body, "adeptability:begin"))
}
