package copilot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/budget"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/pkg/adept"
)

func agentFixture() *adept.Agent {
	return &adept.Agent{
		ID:          "reviewer",
		Description: "Adversarially reviews drafted changes. Use after any code edit.",
		Mode:        adept.AgentModeSubagent,
		Tools:       []string{"read", "search"},
		Model:       "inherit",
		Body:        "You are an adversarial reviewer.\n",
	}
}

func TestRenderAgent_Golden(t *testing.T) {
	t.Parallel()
	r := New()
	out, err := r.RenderAgent(context.Background(), adept.AgentRenderInput{Agent: agentFixture()})
	require.NoError(t, err)
	assert.Equal(t, ".github/agents/reviewer.agent.md", out.Path)
	assert.Empty(t, out.Warnings)

	golden := filepath.Join("testdata", "agent_basic.golden")
	if *updateGolden {
		require.NoError(t, os.WriteFile(golden, out.Bytes, 0o644))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(out.Bytes))
}

func TestRenderAgent_DropWarnings(t *testing.T) {
	t.Parallel()
	a := agentFixture()
	a.DisallowedTools = []string{"edit"}
	a.Mode = adept.AgentModeAll
	out, err := New().RenderAgent(context.Background(), adept.AgentRenderInput{Agent: a})
	require.NoError(t, err)
	require.Len(t, out.Warnings, 2)
}

func TestImportAgents_PrefersAgentMDVariant(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, ".github", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.agent.md"), []byte("---\ndescription: from agent.md\n---\nb\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.md"), []byte("---\ndescription: from plain md\n---\nb\n"), 0o644))

	adapter := NewAdapter(New(), budget.NewPacker(), fsutil.NewWriter())
	imported, err := adapter.ImportAgents(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, imported, 1, "must not self-conflict on the two variants")
	assert.Equal(t, "from agent.md", imported[0].Agent.Description)
}

func TestImportAgents_RoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r := New()
	src := agentFixture()
	out, err := r.RenderAgent(context.Background(), adept.AgentRenderInput{Agent: src})
	require.NoError(t, err)
	dst := filepath.Join(root, out.Path)
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, out.Bytes, 0o644))

	adapter := NewAdapter(r, budget.NewPacker(), fsutil.NewWriter())
	imported, err := adapter.ImportAgents(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, imported, 1)
	got := imported[0].Agent
	assert.Equal(t, src.ID, got.ID)
	assert.Equal(t, src.Description, got.Description)
	assert.Equal(t, src.Tools, got.Tools)
	assert.Equal(t, src.Body, got.Body)
	assert.Empty(t, imported[0].Warnings)

	rep, err := adapter.ValidateAgents(root, []adept.RenderOutput{out})
	require.NoError(t, err)
	assert.Equal(t, []string{out.Path}, rep.Synced)
}
