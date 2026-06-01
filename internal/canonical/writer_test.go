package canonical

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

func TestRenderCanonical_RoundTripsThroughParser(t *testing.T) {
	t.Parallel()
	in := &adept.Skill{
		ID:           "typescript-style",
		Description:  "Project TypeScript conventions. Use when editing .ts or .tsx files.",
		Activation:   adept.ActivationGlobs,
		Globs:        []string{"**/*.ts", "**/*.tsx"},
		AllowedTools: []string{"Read", "Grep"},
		Tags:         []string{"style", "typescript"},
		Metadata:     map[string]string{"owner": "platform-eng"},
		Body:         "# Body\n",
	}
	got, err := RenderCanonical(in)
	require.NoError(t, err)
	// Globs must be quoted so YAML doesn't treat `**` as an alias.
	require.Contains(t, string(got), `- "**/*.ts"`)
	require.Contains(t, string(got), `- "**/*.tsx"`)
	// version and size-hint-kib must never be emitted.
	require.NotContains(t, string(got), "version:")
	require.NotContains(t, string(got), "size-hint-kib:")

	// Round-trip: parsing the rendered bytes yields a Skill equal to the input.
	p := NewParser()
	parsed, _, err := p.ParseFrontmatter(got)
	require.NoError(t, err)
	require.Equal(t, in.ID, parsed.ID)
	require.Equal(t, in.Description, parsed.Description)
	require.Equal(t, in.Globs, parsed.Globs)
	require.Equal(t, in.Tags, parsed.Tags)
	require.Equal(t, in.AllowedTools, parsed.AllowedTools)
	require.Equal(t, in.Metadata, parsed.Metadata)
}

func TestRenderCanonical_EscapesQuotes(t *testing.T) {
	t.Parallel()
	in := &adept.Skill{
		ID:          "esc",
		Description: `Use "quotes" here`,
		Activation:  adept.ActivationAgent,
	}
	got, err := RenderCanonical(in)
	require.NoError(t, err)
	require.Contains(t, string(got), `description: "Use \"quotes\" here"`)
}

func TestRenderCanonical_EmptyIDFails(t *testing.T) {
	t.Parallel()
	_, err := RenderCanonical(&adept.Skill{})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "empty id"))
}

func TestRenderCanonical_FrontmatterFieldOrder(t *testing.T) {
	t.Parallel()
	in := &adept.Skill{
		ID:           "ordered",
		Description:  "verify ordering",
		Activation:   adept.ActivationGlobs,
		Globs:        []string{"**/*.go"},
		AllowedTools: []string{"Read"},
		Targets:      []string{"claude-code"},
		Tags:         []string{"a"},
		Metadata:     map[string]string{"k": "v"},
	}
	got, err := RenderCanonical(in)
	require.NoError(t, err)
	s := string(got)
	order := []string{
		"id:",
		"description:",
		"activation:",
		"globs:",
		"allowed-tools:",
		"targets:",
		"tags:",
		"metadata:",
	}
	prev := -1
	for _, key := range order {
		idx := strings.Index(s, key)
		require.NotEqual(t, -1, idx, "missing key %q in frontmatter", key)
		require.Greater(t, idx, prev, "key %q out of order", key)
		prev = idx
	}
}
