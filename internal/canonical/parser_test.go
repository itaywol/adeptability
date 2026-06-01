package canonical

import (
	"errors"
	"strings"
	"testing"

	"github.com/itaywol/adeptability/pkg/adept"
	"github.com/stretchr/testify/require"
)

func TestParser_ParseSkillYAML_Valid(t *testing.T) {
	p := NewParser()
	in := []byte(`id: example_skill
version: 3
description: parses cleanly
activation: agent
tags: [a, b]
allowed-tools: [Read]
size-hint-kib: 8
metadata:
  owner: x
`)
	got, err := p.ParseSkillYAML(in)
	require.NoError(t, err)
	require.Equal(t, "example_skill", got.ID)
	require.Equal(t, 3, got.Version)
	require.Equal(t, "parses cleanly", got.Description)
	require.Equal(t, adept.ActivationAgent, got.Activation)
	require.Equal(t, []string{"a", "b"}, got.Tags)
	require.Equal(t, []string{"Read"}, got.AllowedTools)
	require.Equal(t, 8, got.SizeHintKiB)
	require.Equal(t, map[string]string{"owner": "x"}, got.Metadata)
}

func TestParser_ParseSkillYAML_DefaultActivation(t *testing.T) {
	p := NewParser()
	in := []byte(`id: defaults
version: 1
description: defaults applied
`)
	got, err := p.ParseSkillYAML(in)
	require.NoError(t, err)
	require.Equal(t, adept.ActivationAgent, got.Activation)
}

func TestParser_ParseSkillYAML_Empty(t *testing.T) {
	p := NewParser()
	_, err := p.ParseSkillYAML([]byte(""))
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestParser_ParseSkillYAML_InvalidYAML(t *testing.T) {
	p := NewParser()
	_, err := p.ParseSkillYAML([]byte("id: [oops\n"))
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestParser_ParseFrontmatter_Valid(t *testing.T) {
	p := NewParser()
	md := []byte("---\nid: fm_skill\nversion: 1\ndescription: body follows\n---\n# Heading\n\nBody text.\n")
	got, body, err := p.ParseFrontmatter(md)
	require.NoError(t, err)
	require.Equal(t, "fm_skill", got.ID)
	require.True(t, strings.HasPrefix(body, "# Heading"))
}

func TestParser_ParseFrontmatter_CRLF(t *testing.T) {
	p := NewParser()
	md := []byte("---\r\nid: crlf_skill\r\nversion: 1\r\ndescription: ok\r\n---\r\nBody\r\n")
	got, body, err := p.ParseFrontmatter(md)
	require.NoError(t, err)
	require.Equal(t, "crlf_skill", got.ID)
	require.True(t, strings.Contains(body, "Body"))
}

func TestParser_ParseFrontmatter_MissingOpener(t *testing.T) {
	p := NewParser()
	_, _, err := p.ParseFrontmatter([]byte("no frontmatter here\n"))
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestParser_ParseFrontmatter_MissingCloser(t *testing.T) {
	p := NewParser()
	_, _, err := p.ParseFrontmatter([]byte("---\nid: nope\nversion: 1\ndescription: x\n"))
	require.Error(t, err)
	require.ErrorIs(t, err, adept.ErrSkillInvalid)
}

func TestParser_ParseFrontmatter_Empty(t *testing.T) {
	p := NewParser()
	_, _, err := p.ParseFrontmatter(nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, adept.ErrSkillInvalid))
}
