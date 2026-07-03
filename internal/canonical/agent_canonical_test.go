package canonical

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/pkg/adept"
)

func TestParseAgentFrontmatter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		wantErr error
		check   func(t *testing.T, a *adept.Agent, body string)
	}{
		{
			name: "full frontmatter",
			in:   "---\nid: reviewer\ndescription: \"Reviews code. Use after edits.\"\nmode: subagent\ntools:\n  - Read\n  - Bash\ndisallowed-tools:\n  - Write\nmodel: inherit\ntargets:\n  - claude-code\ntags:\n  - review\nmetadata:\n  team: core\nharness:\n  claude-code:\n    permissionMode: plan\n---\nYou are a reviewer.\n",
			check: func(t *testing.T, a *adept.Agent, body string) {
				assert.Equal(t, "reviewer", a.ID)
				assert.Equal(t, "Reviews code. Use after edits.", a.Description)
				assert.Equal(t, adept.AgentModeSubagent, a.Mode)
				assert.Equal(t, []string{"Read", "Bash"}, a.Tools)
				assert.Equal(t, []string{"Write"}, a.DisallowedTools)
				assert.Equal(t, "inherit", a.Model)
				assert.Equal(t, []string{"claude-code"}, a.Targets)
				assert.Equal(t, map[string]string{"team": "core"}, a.Metadata)
				assert.Equal(t, "plan", a.Harness["claude-code"]["permissionMode"])
				assert.Equal(t, "You are a reviewer.\n", body)
				assert.Equal(t, body, a.Body)
			},
		},
		{
			name: "name alias maps to id and mode defaults",
			in:   "---\nname: helper\ndescription: \"x\"\n---\nbody\n",
			check: func(t *testing.T, a *adept.Agent, _ string) {
				assert.Equal(t, "helper", a.ID)
				assert.Equal(t, adept.AgentModeSubagent, a.Mode)
			},
		},
		{
			name:    "missing frontmatter",
			in:      "just a body\n",
			wantErr: adept.ErrAgentInvalid,
		},
		{
			name:    "empty document",
			in:      "",
			wantErr: adept.ErrAgentInvalid,
		},
		{
			name:    "unterminated frontmatter",
			in:      "---\nid: x\ndescription: \"y\"\n",
			wantErr: adept.ErrAgentInvalid,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a, body, err := ParseAgentFrontmatter([]byte(tt.in))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.check(t, a, body)
		})
	}
}

func TestAgentValidator(t *testing.T) {
	t.Parallel()
	v, err := NewAgentValidator()
	require.NoError(t, err)
	tests := []struct {
		name    string
		agent   *adept.Agent
		wantErr bool
	}{
		{
			name:  "valid minimal",
			agent: &adept.Agent{ID: "reviewer", Description: "Reviews code."},
		},
		{
			name:  "valid full",
			agent: &adept.Agent{ID: "r", Description: "d", Mode: adept.AgentModeAll, Tools: []string{"Read"}, Model: "sonnet", Harness: map[string]map[string]any{"cursor": {"readonly": true}}},
		},
		{name: "nil agent", agent: nil, wantErr: true},
		{name: "missing description", agent: &adept.Agent{ID: "x"}, wantErr: true},
		{name: "bad id", agent: &adept.Agent{ID: "Bad_ID", Description: "d"}, wantErr: true},
		{name: "bad mode", agent: &adept.Agent{ID: "x", Description: "d", Mode: "sometimes"}, wantErr: true},
		{
			name:    "harness override cannot set name",
			agent:   &adept.Agent{ID: "x", Description: "d", Harness: map[string]map[string]any{"claude-code": {"name": "evil"}}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := v.ValidateAgent(tt.agent)
			if tt.wantErr {
				require.ErrorIs(t, err, adept.ErrAgentInvalid)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRenderCanonicalAgentRoundTrip(t *testing.T) {
	t.Parallel()
	in := &adept.Agent{
		ID:              "reviewer",
		Description:     `Reviews "Go" code. Use after edits.`,
		Mode:            adept.AgentModeSubagent,
		Tools:           []string{"Read", "Bash"},
		DisallowedTools: []string{"Write"},
		Model:           "inherit",
		Targets:         []string{"claude-code", "opencode"},
		Tags:            []string{"review"},
		Metadata:        map[string]string{"team": "core"},
		Harness:         map[string]map[string]any{"claude-code": {"permissionMode": "plan"}},
		Body:            "You are a reviewer.\n",
	}
	first, err := RenderCanonicalAgent(in)
	require.NoError(t, err)

	parsed, _, err := ParseAgentFrontmatter(first)
	require.NoError(t, err)
	assert.Equal(t, in.ID, parsed.ID)
	assert.Equal(t, in.Description, parsed.Description)
	assert.Equal(t, in.Tools, parsed.Tools)
	assert.Equal(t, in.Body, parsed.Body)

	// Byte-stable: rendering the parsed agent reproduces the first render.
	second, err := RenderCanonicalAgent(parsed)
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second))
}

func TestRenderCanonicalAgentEmptyID(t *testing.T) {
	t.Parallel()
	_, err := RenderCanonicalAgent(&adept.Agent{Description: "d"})
	require.ErrorIs(t, err, adept.ErrAgentInvalid)
}

// Multi-line descriptions (the dominant real-world .claude/agents pattern,
// with <example> blocks) must survive render → reparse byte-exactly — and a
// description line of exactly "---" must not truncate the frontmatter of the
// file we just wrote.
func TestRenderCanonical_MultilineDescriptionRoundTrip(t *testing.T) {
	t.Parallel()
	desc := "Reviews code.\n\n<example>\nuser: review this\n---\nassistant: uses reviewer\n</example>\n\ttabbed"

	t.Run("agent", func(t *testing.T) {
		t.Parallel()
		a := &adept.Agent{ID: "reviewer", Description: desc, Body: "b\n"}
		raw, err := RenderCanonicalAgent(a)
		require.NoError(t, err)
		parsed, _, err := ParseAgentFrontmatter(raw)
		require.NoError(t, err)
		assert.Equal(t, desc, parsed.Description)
		assert.Equal(t, "b\n", parsed.Body)
	})

	t.Run("skill", func(t *testing.T) {
		t.Parallel()
		s := &adept.Skill{ID: "reviewer", Description: desc, Activation: adept.ActivationAgent, Body: "b\n"}
		raw, err := RenderCanonical(s)
		require.NoError(t, err)
		parsed, _, err := NewParser().ParseFrontmatter(raw)
		require.NoError(t, err)
		assert.Equal(t, desc, parsed.Description)
	})
}

func TestAgentUnknownFrontmatterKeys(t *testing.T) {
	t.Parallel()
	unknown, err := AgentUnknownFrontmatterKeys([]byte("---\nid: x\ndescription: d\ntool:\n  - Read\npermissionMode: plan\n---\nb\n"))
	require.NoError(t, err)
	assert.Equal(t, []string{"permissionMode", "tool"}, unknown)

	unknown, err = AgentUnknownFrontmatterKeys([]byte("---\nname: x\ndescription: d\ntools:\n  - Read\n---\nb\n"))
	require.NoError(t, err)
	assert.Empty(t, unknown)
}

func TestLoadAgentFile(t *testing.T) {
	t.Parallel()
	v, err := NewAgentValidator()
	require.NoError(t, err)

	t.Run("valid file, filename fills id", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "reviewer.md")
		require.NoError(t, os.WriteFile(path, []byte("---\ndescription: \"Reviews code.\"\n---\nbody\n"), 0o644))
		a, err := LoadAgentFile(path, v)
		require.NoError(t, err)
		assert.Equal(t, "reviewer", a.ID)
		assert.Equal(t, "body\n", a.Body)
	})

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()
		_, err := LoadAgentFile(filepath.Join(t.TempDir(), "nope.md"), v)
		require.ErrorIs(t, err, adept.ErrAgentNotFound)
	})

	t.Run("invalid frontmatter", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.md")
		require.NoError(t, os.WriteFile(path, []byte("no frontmatter"), 0o644))
		_, err := LoadAgentFile(path, v)
		require.ErrorIs(t, err, adept.ErrAgentInvalid)
	})

	t.Run("nil validator", func(t *testing.T) {
		t.Parallel()
		_, err := LoadAgentFile("x.md", nil)
		require.ErrorIs(t, err, adept.ErrAgentInvalid)
	})
}
