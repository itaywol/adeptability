package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/render/claude"
	"github.com/itaywol/adeptability/pkg/adept"
)

func agentFixture() *adept.Agent {
	return &adept.Agent{
		ID:              "reviewer",
		Description:     "Adversarially reviews drafted changes. Use after any code edit.",
		Mode:            adept.AgentModeSubagent,
		Tools:           []string{"Read", "Grep", "Bash"},
		DisallowedTools: []string{"Write"},
		Model:           "inherit",
		Body:            "You are an adversarial reviewer.\n\n## Check\n\n1. Run the tests.\n",
	}
}

func checkAgentGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	golden := filepath.Join("testdata", name+".golden")
	if *update {
		require.NoError(t, os.WriteFile(golden, got, 0o644))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}

func TestRenderAgent_Golden(t *testing.T) {
	t.Parallel()
	r := claude.New()
	out, err := r.RenderAgent(context.Background(), adept.AgentRenderInput{
		Agent:   agentFixture(),
		Harness: claude.Spec(),
	})
	require.NoError(t, err)
	assert.Equal(t, ".claude/agents/reviewer.md", out.Path)
	assert.Equal(t, "reviewer", out.SkillID)
	assert.NotEmpty(t, out.SkillHash)
	assert.Empty(t, out.Warnings)
	assert.Empty(t, out.Sidecars)
	checkAgentGolden(t, "agent_basic", out.Bytes)
}

func TestRenderAgent_GoldenOverrides(t *testing.T) {
	t.Parallel()
	a := agentFixture()
	a.Harness = map[string]map[string]any{
		"claude-code": {"permissionMode": "plan", "model": "opus"},
	}
	r := claude.New()
	out, err := r.RenderAgent(context.Background(), adept.AgentRenderInput{
		Agent:   a,
		Harness: claude.Spec(),
	})
	require.NoError(t, err)
	checkAgentGolden(t, "agent_overrides", out.Bytes)
}

func TestRenderAgent_Errors(t *testing.T) {
	t.Parallel()
	r := claude.New()
	_, err := r.RenderAgent(context.Background(), adept.AgentRenderInput{})
	require.ErrorIs(t, err, adept.ErrAgentInvalid)
	_, err = r.RenderAgent(context.Background(), adept.AgentRenderInput{Agent: &adept.Agent{ID: "x"}})
	require.ErrorIs(t, err, adept.ErrAgentInvalid)
}

func TestRenderAgent_ModeWarning(t *testing.T) {
	t.Parallel()
	a := agentFixture()
	a.Mode = adept.AgentModePrimary
	r := claude.New()
	out, err := r.RenderAgent(context.Background(), adept.AgentRenderInput{Agent: a, Harness: claude.Spec()})
	require.NoError(t, err)
	require.Len(t, out.Warnings, 1)
	assert.Contains(t, out.Warnings[0], "mode")
}

func TestImportAgents_RoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	r := claude.New()
	src := agentFixture()
	out, err := r.RenderAgent(context.Background(), adept.AgentRenderInput{Agent: src, Harness: claude.Spec()})
	require.NoError(t, err)
	dst := filepath.Join(root, out.Path)
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(dst, out.Bytes, 0o644))

	adapter := claude.NewAdapter(r, fakeWriter{}, osLinker{})
	imported, err := adapter.ImportAgents(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, imported, 1)
	got := imported[0].Agent
	assert.Equal(t, src.ID, got.ID)
	assert.Equal(t, src.Description, got.Description)
	assert.Equal(t, src.Tools, got.Tools)
	assert.Equal(t, src.DisallowedTools, got.DisallowedTools)
	assert.Equal(t, src.Model, got.Model)
	assert.Equal(t, src.Body, got.Body)
	assert.Empty(t, imported[0].Warnings)

	// Drift: on-disk file matches the render → synced.
	rep, err := adapter.ValidateAgents(root, []adept.RenderOutput{out})
	require.NoError(t, err)
	assert.Equal(t, []string{out.Path}, rep.Synced)
}

func TestImportAgents_ForeignKeysWarn(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, ".claude", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	doc := "---\ndescription: d\npermissionMode: plan\n---\nbody\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "custom.md"), []byte(doc), 0o644))

	adapter := claude.NewAdapter(claude.New(), fakeWriter{}, osLinker{})
	imported, err := adapter.ImportAgents(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, imported, 1)
	require.Len(t, imported[0].Warnings, 1)
	assert.Contains(t, imported[0].Warnings[0], "permissionMode")
	assert.Equal(t, "custom", imported[0].Agent.ID)
}
