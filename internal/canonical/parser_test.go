package canonical

import (
	"errors"
	"strings"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func TestParser_ParseSkillYAML_Valid(t *testing.T) {
	t.Parallel()
	p := NewParser()
	in := []byte(`id: example_skill
description: parses cleanly
activation: agent
tags: [a, b]
allowed-tools: [Read]
metadata:
  owner: x
`)
	got, err := p.ParseSkillYAML(in)
	require.NoError(t, err)
	require.Equal(t, "example_skill", got.ID)
	require.Equal(t, "parses cleanly", got.Description)
	require.Equal(t, adept.ActivationAgent, got.Activation)
	require.Equal(t, []string{"a", "b"}, got.Tags)
	require.Equal(t, []string{"Read"}, got.AllowedTools)
	require.Equal(t, map[string]string{"owner": "x"}, got.Metadata)
}

func TestParser_ParseSkillYAML_DefaultActivation(t *testing.T) {
	t.Parallel()
	p := NewParser()
	in := []byte(`id: defaults
description: defaults applied
`)
	got, err := p.ParseSkillYAML(in)
	require.NoError(t, err)
	require.Equal(t, adept.ActivationAgent, got.Activation)
}

func TestParser_ParseSkillYAML_Empty(t *testing.T) {
	t.Parallel()
	p := NewParser()
	_, err := p.ParseSkillYAML([]byte(""))
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

// Vercel-labs/skills (and most harness-native SKILL.md) uses `name:` in
// frontmatter. Parser must accept it as an alias for `id:` so installed
// skills load without manual rewriting.
func TestParser_ParseSkillYAML_NameAliasesID(t *testing.T) {
	t.Parallel()
	p := NewParser()
	got, err := p.ParseSkillYAML([]byte("name: find-skills\ndescription: vercel style\n"))
	require.NoError(t, err)
	require.Equal(t, "find-skills", got.ID)
	require.Equal(t, "vercel style", got.Description)
}

func TestParser_ParseFrontmatter_NameAliasesID(t *testing.T) {
	t.Parallel()
	p := NewParser()
	md := []byte("---\nname: find-skills\ndescription: vercel style\n---\nbody\n")
	got, _, err := p.ParseFrontmatter(md)
	require.NoError(t, err)
	require.Equal(t, "find-skills", got.ID)
}

// id wins when both are present — explicit canonical key beats the alias.
func TestParser_ParseSkillYAML_ExplicitIDWinsOverName(t *testing.T) {
	t.Parallel()
	p := NewParser()
	got, err := p.ParseSkillYAML([]byte("id: canonical\nname: alias\ndescription: x\n"))
	require.NoError(t, err)
	require.Equal(t, "canonical", got.ID)
}

func TestParser_ParseSkillYAML_InvalidYAML(t *testing.T) {
	t.Parallel()
	p := NewParser()
	_, err := p.ParseSkillYAML([]byte("id: [oops\n"))
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestParser_ParseSkillYAML_UnknownFieldsIgnored(t *testing.T) {
	t.Parallel()
	// yaml.v3 with KnownFields disabled silently drops unknown keys. The
	// validator (schema) is what rejects them. Here we only assert that
	// parse itself does not error on unknown keys.
	p := NewParser()
	in := []byte(`id: ok
description: extra fields silently ignored at parse stage
some_unknown_field: 42
`)
	got, err := p.ParseSkillYAML(in)
	require.NoError(t, err)
	require.Equal(t, "ok", got.ID)
}

func TestParser_ParseFrontmatter_Valid(t *testing.T) {
	t.Parallel()
	p := NewParser()
	md := []byte("---\nid: fm_skill\ndescription: body follows\n---\n# Heading\n\nBody text.\n")
	got, body, err := p.ParseFrontmatter(md)
	require.NoError(t, err)
	require.Equal(t, "fm_skill", got.ID)
	require.True(t, strings.HasPrefix(body, "# Heading"))
}

func TestParser_ParseFrontmatter_CRLF(t *testing.T) {
	t.Parallel()
	p := NewParser()
	md := []byte("---\r\nid: crlf_skill\r\ndescription: ok\r\n---\r\nBody\r\n")
	got, body, err := p.ParseFrontmatter(md)
	require.NoError(t, err)
	require.Equal(t, "crlf_skill", got.ID)
	require.True(t, strings.Contains(body, "Body"))
}

func TestParser_ParseFrontmatter_MissingOpener(t *testing.T) {
	t.Parallel()
	p := NewParser()
	_, _, err := p.ParseFrontmatter([]byte("no frontmatter here\n"))
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestParser_ParseFrontmatter_MissingCloser(t *testing.T) {
	t.Parallel()
	p := NewParser()
	_, _, err := p.ParseFrontmatter([]byte("---\nid: nope\ndescription: x\n"))
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestParser_ParseFrontmatter_Empty(t *testing.T) {
	t.Parallel()
	p := NewParser()
	_, _, err := p.ParseFrontmatter(nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, adept.ErrSkillInvalid))
}
