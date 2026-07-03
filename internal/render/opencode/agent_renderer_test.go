package opencode_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/render/opencode"
	"github.com/itaywol/adeptability/pkg/adept"
)

func agentFixture() *adept.Agent {
	return &adept.Agent{
		ID:          "reviewer",
		Description: "Adversarially reviews drafted changes. Use after any code edit.",
		Mode:        adept.AgentModeSubagent,
		Model:       "anthropic/claude-sonnet-4-20250514",
		Body:        "You are an adversarial reviewer.\n",
	}
}

func TestRenderAgent_Golden(t *testing.T) {
	t.Parallel()
	r := opencode.New()
	out, err := r.RenderAgent(context.Background(), adept.AgentRenderInput{
		Agent:   agentFixture(),
		Harness: opencode.Spec(),
	})
	require.NoError(t, err)
	assert.Equal(t, ".opencode/agents/reviewer.md", out.Path)
	assert.Empty(t, out.Warnings)

	golden := filepath.Join("testdata", "agent_basic.golden")
	if *update {
		require.NoError(t, os.WriteFile(golden, out.Bytes, 0o644))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(out.Bytes))
}

func TestRenderAgent_ToolsDropWarn(t *testing.T) {
	t.Parallel()
	a := agentFixture()
	a.Tools = []string{"Read"}
	a.DisallowedTools = []string{"Write"}
	r := opencode.New()
	out, err := r.RenderAgent(context.Background(), adept.AgentRenderInput{Agent: a, Harness: opencode.Spec()})
	require.NoError(t, err)
	require.Len(t, out.Warnings, 2)
	assert.NotContains(t, string(out.Bytes), "tools")
}

func TestImportAgents_RoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r := opencode.New()
	src := agentFixture()
	out, err := r.RenderAgent(context.Background(), adept.AgentRenderInput{Agent: src, Harness: opencode.Spec()})
	require.NoError(t, err)
	dst := filepath.Join(root, out.Path)
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, out.Bytes, 0o644))

	adapter := opencode.NewAdapter(r, fakeWriter{}, osLinker{})
	imported, err := adapter.ImportAgents(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, imported, 1)
	got := imported[0].Agent
	assert.Equal(t, src.ID, got.ID)
	assert.Equal(t, src.Description, got.Description)
	assert.Equal(t, src.Mode, got.Mode)
	assert.Equal(t, src.Model, got.Model)
	assert.Equal(t, src.Body, got.Body)
	assert.Empty(t, imported[0].Warnings)
}
