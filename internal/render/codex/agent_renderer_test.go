package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/budget"
	"github.com/itaywol/adeptability/pkg/adept"
)

func agentFixture() *adept.Agent {
	return &adept.Agent{
		ID:          "reviewer",
		Description: "Adversarially reviews drafted changes. Use after any code edit.",
		Mode:        adept.AgentModeSubagent,
		Model:       "gpt-5.2-codex",
		Body:        "You are an adversarial reviewer.\n\nRun the tests before judging.\n",
	}
}

func TestRenderAgent_Golden(t *testing.T) {
	t.Parallel()
	r := New()
	out, err := r.RenderAgent(context.Background(), adept.AgentRenderInput{Agent: agentFixture()})
	require.NoError(t, err)
	assert.Equal(t, ".codex/agents/reviewer.toml", out.Path)
	assert.Empty(t, out.Warnings)

	golden := filepath.Join("testdata", "agent_basic.golden")
	if *updateGolden {
		require.NoError(t, os.WriteFile(golden, out.Bytes, 0o644))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(out.Bytes))
}

func TestRenderAgent_EmptyBodyWarnDrops(t *testing.T) {
	t.Parallel()
	a := agentFixture()
	a.Body = ""
	out, err := New().RenderAgent(context.Background(), adept.AgentRenderInput{Agent: a})
	require.NoError(t, err, "empty body must warn-drop for codex, not fail the sync")
	assert.Empty(t, out.Path, "empty path signals dropped")
	require.Len(t, out.Warnings, 1)
	assert.Contains(t, out.Warnings[0], "developer_instructions")
}

func TestRenderAgent_IdentityOverrideIgnored(t *testing.T) {
	t.Parallel()
	a := agentFixture()
	a.Harness = map[string]map[string]any{"codex": {"name": "evil", "sandbox_mode": "read-only"}}
	out, err := New().RenderAgent(context.Background(), adept.AgentRenderInput{Agent: a})
	require.NoError(t, err)
	assert.Contains(t, string(out.Bytes), "name = 'reviewer'")
	assert.NotContains(t, string(out.Bytes), "evil")
	require.Len(t, out.Warnings, 1)
	assert.Contains(t, out.Warnings[0], "identity fields")
}

func TestRenderAgent_OverridesMerge(t *testing.T) {
	t.Parallel()
	a := agentFixture()
	a.Harness = map[string]map[string]any{"codex": {"sandbox_mode": "read-only"}}
	out, err := New().RenderAgent(context.Background(), adept.AgentRenderInput{Agent: a})
	require.NoError(t, err)
	assert.Contains(t, string(out.Bytes), "sandbox_mode = 'read-only'")
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

	adapter := NewAdapter(r, budget.NewPacker(), nil)
	imported, err := adapter.ImportAgents(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, imported, 1)
	got := imported[0].Agent
	assert.Equal(t, src.ID, got.ID)
	assert.Equal(t, src.Description, got.Description)
	assert.Equal(t, src.Model, got.Model)
	assert.Equal(t, "You are an adversarial reviewer.\n\nRun the tests before judging.\n", got.Body)
	assert.Empty(t, imported[0].Warnings)

	// Drift via the per-file helper.
	rep, err := adapter.ValidateAgents(root, []adept.RenderOutput{out})
	require.NoError(t, err)
	assert.Equal(t, []string{out.Path}, rep.Synced)
}

func TestImportAgents_ForeignKeysWarn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, ".codex", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	doc := "name = 'x'\ndescription = 'd'\ndeveloper_instructions = 'body'\nsandbox_mode = 'read-only'\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.toml"), []byte(doc), 0o644))

	adapter := NewAdapter(New(), budget.NewPacker(), nil)
	imported, err := adapter.ImportAgents(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, imported, 1)
	require.Len(t, imported[0].Warnings, 1)
	assert.Contains(t, imported[0].Warnings[0], "sandbox_mode")
}
