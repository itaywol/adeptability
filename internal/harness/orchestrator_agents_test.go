package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/pkg/adept"
)

// agentMockAdapter is a mockAdapter that also implements adept.AgentSupport.
type agentMockAdapter struct {
	*mockAdapter
	agentImports func(context.Context, string) ([]adept.ImportedAgent, error)
	// renderAgent overrides the default render behavior when set.
	renderAgent func(in adept.AgentRenderInput) (adept.RenderOutput, error)
}

func (m *agentMockAdapter) AgentRenderer() adept.AgentRenderer { return m }

func (m *agentMockAdapter) RenderAgent(_ context.Context, in adept.AgentRenderInput) (adept.RenderOutput, error) {
	if m.renderAgent != nil {
		return m.renderAgent(in)
	}
	return adept.RenderOutput{
		Path:    filepath.Join("."+m.spec.ID, "agents", in.Agent.ID+".md"),
		Bytes:   []byte("agent body for " + in.Agent.ID),
		Mode:    0o644,
		SkillID: in.Agent.ID,
	}, nil
}

func (m *agentMockAdapter) ValidateAgents(projectRoot string, expected []adept.RenderOutput) (adept.DriftReport, error) {
	var rep adept.DriftReport
	for _, out := range expected {
		got, err := os.ReadFile(filepath.Join(projectRoot, out.Path))
		switch {
		case err != nil:
			rep.Missing = append(rep.Missing, out.Path)
		case string(got) == string(out.Bytes):
			rep.Synced = append(rep.Synced, out.Path)
		default:
			rep.Drifted = append(rep.Drifted, out.Path)
		}
	}
	return rep, nil
}

func (m *agentMockAdapter) ImportAgents(ctx context.Context, root string) ([]adept.ImportedAgent, error) {
	if m.agentImports == nil {
		return nil, nil
	}
	return m.agentImports(ctx, root)
}

var _ adept.AgentSupport = (*agentMockAdapter)(nil)

func installAgent(t *testing.T, p project.Project, id string, targets ...string) {
	t.Helper()
	require.NoError(t, p.InstallAgent(&adept.Agent{
		ID:          id,
		Description: "d " + id,
		Mode:        adept.AgentModeSubagent,
		Targets:     targets,
		Body:        "You are " + id + ".\n",
	}))
}

func TestSync_AgentsUnsupportedHarnessWarns(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	installAgent(t, p, "reviewer")
	setHarnesses(t, p, "plain")

	o := newOrch(t, perSkillAdapter("plain", nil))
	results, err := o.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Len(t, results[0].Warnings, 1)
	assert.Contains(t, results[0].Warnings[0], "does not support agents")
	assert.NoFileExists(t, filepath.Join(p.Root(), ".plain", "agents", "reviewer.md"))
}

func TestSync_AgentsRenderedAndMerged(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	installAgent(t, p, "reviewer")
	setHarnesses(t, p, "withagents")

	adapter := &agentMockAdapter{mockAdapter: perSkillAdapter("withagents", nil)}
	o := newOrch(t, adapter)
	results, err := o.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	res := results[0]
	agentPath := filepath.Join(".withagents", "agents", "reviewer.md")
	assert.Contains(t, res.Written, agentPath)
	assert.FileExists(t, filepath.Join(p.Root(), agentPath))
	assert.Contains(t, res.Drift.Synced, agentPath)
	assert.Empty(t, res.Warnings)

	// Second sync: byte-identical agent file is skipped, not rewritten.
	results, err = o.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	assert.Contains(t, results[0].Skipped, agentPath)
}

func TestSync_AgentTargetsFilter(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	installAgent(t, p, "reviewer", "other-harness")
	setHarnesses(t, p, "withagents")

	adapter := &agentMockAdapter{mockAdapter: perSkillAdapter("withagents", nil)}
	o := newOrch(t, adapter)
	results, err := o.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(p.Root(), ".withagents", "agents", "reviewer.md"))
	assert.Empty(t, results[0].Warnings)
}

func TestStatus_AgentDriftMerged(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	installAgent(t, p, "reviewer")
	setHarnesses(t, p, "withagents")

	adapter := &agentMockAdapter{mockAdapter: perSkillAdapter("withagents", nil)}
	o := newOrch(t, adapter)
	_, err := o.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)

	agentPath := filepath.Join(".withagents", "agents", "reviewer.md")
	require.NoError(t, os.Remove(filepath.Join(p.Root(), agentPath)))

	reports, err := o.Status(context.Background(), p, StatusOptions{})
	require.NoError(t, err)
	require.Len(t, reports, 1)
	assert.Contains(t, reports[0].Missing, agentPath)
}

// TestStatus_GlobalSkipsAgentDrift proves global Status does not fold in agent
// drift. Global Sync renders skills only (agents are project-scope in v1), so
// folding the agent file into Status would report a Missing that global Sync
// can never clear — a permanent exit-2 drift. The fold must be gated on
// !opts.Global, mirroring Sync.
func TestStatus_GlobalSkipsAgentDrift(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	installAgent(t, p, "reviewer")
	setHarnesses(t, p, "withagents")

	base := perSkillAdapter("withagents", nil)
	base.spec.GlobalOutput = base.spec.OutputPath // make the harness global-capable
	adapter := &agentMockAdapter{mockAdapter: base}
	o := newOrch(t, adapter)

	// Global sync renders the skill but never the agent file.
	_, err := o.Sync(context.Background(), p, SyncOptions{Global: true})
	require.NoError(t, err)

	reports, err := o.Status(context.Background(), p, StatusOptions{Global: true})
	require.NoError(t, err)
	require.Len(t, reports, 1)
	agentPath := filepath.Join(".withagents", "agents", "reviewer.md")
	assert.NotContains(t, reports[0].Missing, agentPath, "global status must not report agent drift")
	assert.Empty(t, reports[0].Missing, "skill synced, agent skipped => nothing missing")

	// Sanity: in project scope the same setup DOES fold agent drift in (the
	// agent file was never rendered in global mode), so the gate is real.
	projReports, err := o.Status(context.Background(), p, StatusOptions{})
	require.NoError(t, err)
	require.Len(t, projReports, 1)
	assert.Contains(t, projReports[0].Missing, agentPath, "project status still folds agent drift")
}

func TestImportAgents_InstallAndConflicts(t *testing.T) {
	imported := func(id, harness string) []adept.ImportedAgent {
		return []adept.ImportedAgent{{
			Agent:      &adept.Agent{ID: id, Description: "from " + harness, Mode: adept.AgentModeSubagent, Body: "b\n"},
			SourcePath: "." + harness + "/agents/" + id + ".md",
		}}
	}
	newAdapters := func() (adept.HarnessAdapter, adept.HarnessAdapter) {
		a := &agentMockAdapter{
			mockAdapter:  perSkillAdapter("ha", nil),
			agentImports: func(context.Context, string) ([]adept.ImportedAgent, error) { return imported("reviewer", "ha"), nil },
		}
		b := &agentMockAdapter{
			mockAdapter:  perSkillAdapter("hb", nil),
			agentImports: func(context.Context, string) ([]adept.ImportedAgent, error) { return imported("reviewer", "hb"), nil },
		}
		return a, b
	}

	t.Run("single harness installs", func(t *testing.T) {
		p := newProj(t)
		a, _ := newAdapters()
		o := newOrch(t, a)
		rep, err := o.ImportAgents(context.Background(), p, ImportOptions{HarnessIDs: []string{"ha"}})
		require.NoError(t, err)
		require.Len(t, rep.Imported, 1)
		assert.Equal(t, "reviewer", rep.Imported[0].AgentID)
		assert.True(t, p.HasAgent("reviewer"))
	})

	t.Run("existing canonical kept without force", func(t *testing.T) {
		p := newProj(t)
		installAgent(t, p, "reviewer")
		a, _ := newAdapters()
		o := newOrch(t, a)
		rep, err := o.ImportAgents(context.Background(), p, ImportOptions{HarnessIDs: []string{"ha"}})
		require.NoError(t, err)
		assert.Empty(t, rep.Imported)
		require.Len(t, rep.Conflicts, 1)
		assert.Contains(t, rep.Conflicts[0].Resolved, "kept project canonical")
		got, err := p.GetAgent("reviewer")
		require.NoError(t, err)
		assert.Equal(t, "d reviewer", got.Description)
	})

	t.Run("force overwrites canonical", func(t *testing.T) {
		p := newProj(t)
		installAgent(t, p, "reviewer")
		a, _ := newAdapters()
		o := newOrch(t, a)
		rep, err := o.ImportAgents(context.Background(), p, ImportOptions{HarnessIDs: []string{"ha"}, Force: true})
		require.NoError(t, err)
		require.Len(t, rep.Imported, 1)
		got, err := p.GetAgent("reviewer")
		require.NoError(t, err)
		assert.Equal(t, "from ha", got.Description)
	})

	t.Run("cross-harness conflict resolves via prefer", func(t *testing.T) {
		p := newProj(t)
		a, b := newAdapters()
		o := newOrch(t, a, b)
		rep, err := o.ImportAgents(context.Background(), p, ImportOptions{
			Strategy:      ImportStrategyPrefer,
			PreferHarness: "hb",
		})
		require.NoError(t, err)
		require.Len(t, rep.Imported, 1)
		assert.Equal(t, "hb", rep.Imported[0].Harness)
		require.Len(t, rep.Conflicts, 1)
	})

	t.Run("invalid ids and missing descriptions become skip rows", func(t *testing.T) {
		p := newProj(t)
		a := &agentMockAdapter{
			mockAdapter: perSkillAdapter("ha", nil),
			agentImports: func(context.Context, string) ([]adept.ImportedAgent, error) {
				return []adept.ImportedAgent{
					{Agent: &adept.Agent{ID: "My-Agent", Description: "d", Body: "b\n"}, SourcePath: ".ha/agents/My-Agent.md"},
					{Agent: &adept.Agent{ID: "", Description: "d", Body: "b\n"}, SourcePath: ".ha/agents/.md"},
					{Agent: &adept.Agent{ID: "no-desc", Description: "  ", Body: "b\n"}, SourcePath: ".ha/agents/no-desc.md"},
					{Agent: &adept.Agent{ID: "good", Description: "fine", Body: "b\n"}, SourcePath: ".ha/agents/good.md"},
				}, nil
			},
		}
		o := newOrch(t, a)
		rep, err := o.ImportAgents(context.Background(), p, ImportOptions{HarnessIDs: []string{"ha"}})
		require.NoError(t, err, "bad files must not abort the run")
		require.Len(t, rep.Imported, 1)
		assert.Equal(t, "good", rep.Imported[0].AgentID)
		require.Len(t, rep.Skipped, 3)
		assert.True(t, p.HasAgent("good"))
		assert.False(t, p.HasAgent("no-desc"))
	})

	t.Run("importer warnings surface on the imported row", func(t *testing.T) {
		p := newProj(t)
		a := &agentMockAdapter{
			mockAdapter: perSkillAdapter("ha", nil),
			agentImports: func(context.Context, string) ([]adept.ImportedAgent, error) {
				return []adept.ImportedAgent{{
					Agent:      &adept.Agent{ID: "x", Description: "d", Body: "b\n"},
					SourcePath: ".ha/agents/x.md",
					Warnings:   []string{"ha import x: dropped field \"permissionMode\""},
				}}, nil
			},
		}
		o := newOrch(t, a)
		rep, err := o.ImportAgents(context.Background(), p, ImportOptions{HarnessIDs: []string{"ha"}})
		require.NoError(t, err)
		require.Len(t, rep.Imported, 1)
		require.Len(t, rep.Imported[0].Warnings, 1)
	})

	t.Run("dry run writes nothing", func(t *testing.T) {
		p := newProj(t)
		a, _ := newAdapters()
		o := newOrch(t, a)
		rep, err := o.ImportAgents(context.Background(), p, ImportOptions{HarnessIDs: []string{"ha"}, DryRun: true})
		require.NoError(t, err)
		require.Len(t, rep.Imported, 1)
		assert.False(t, p.HasAgent("reviewer"))
	})
}

func TestSync_CursorClaudeDedupWarning(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	installAgent(t, p, "reviewer")
	setHarnesses(t, p, "claude-code", "cursor")

	claude := &agentMockAdapter{mockAdapter: perSkillAdapter("claude-code", nil)}
	cursor := &agentMockAdapter{mockAdapter: perSkillAdapter("cursor", nil)}
	o := newOrch(t, claude, cursor)
	results, err := o.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, results, 2)
	var cursorRes SyncResult
	for _, r := range results {
		if r.Harness == "cursor" {
			cursorRes = r
		}
	}
	require.NotEmpty(t, cursorRes.Warnings)
	assert.Contains(t, cursorRes.Warnings[0], "appear twice")
}

// An agent scoped to cursor only never lands in .claude/agents/ and must not
// trip the dedup warning.
func TestSync_CursorOnlyAgentNoDedupWarning(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	installAgent(t, p, "reviewer", "cursor")
	setHarnesses(t, p, "claude-code", "cursor")

	claude := &agentMockAdapter{mockAdapter: perSkillAdapter("claude-code", nil)}
	cursor := &agentMockAdapter{mockAdapter: perSkillAdapter("cursor", nil)}
	o := newOrch(t, claude, cursor)
	results, err := o.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	for _, r := range results {
		for _, w := range r.Warnings {
			assert.NotContains(t, w, "appear twice")
		}
	}
}

// The flagship flow: a real-world agent (multi-line description with
// <example> blocks, the dominant .claude/agents pattern) imported via
// ImportAgents must survive the canonical write, reload byte-exactly, and
// render on a subsequent Sync.
func TestImportAgents_ThenSyncPipeline(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	setHarnesses(t, p, "ha")

	desc := "Reviews code changes.\n\n<example>\nuser: review my diff\n---\nassistant: uses the reviewer\n</example>\n\nUse proactively."
	adapter := &agentMockAdapter{
		mockAdapter: perSkillAdapter("ha", nil),
		agentImports: func(context.Context, string) ([]adept.ImportedAgent, error) {
			return []adept.ImportedAgent{{
				Agent:      &adept.Agent{ID: "reviewer", Description: desc, Mode: adept.AgentModeSubagent, Body: "You review.\n"},
				SourcePath: ".ha/agents/reviewer.md",
			}}, nil
		},
	}
	o := newOrch(t, adapter)
	rep, err := o.ImportAgents(context.Background(), p, ImportOptions{HarnessIDs: []string{"ha"}})
	require.NoError(t, err)
	require.Len(t, rep.Imported, 1)

	// Reload from disk: the canonical writer must not have mangled the
	// multi-line description (newline folding) or truncated the frontmatter
	// (the "---" line inside the <example> block).
	got, err := p.GetAgent("reviewer")
	require.NoError(t, err)
	require.Equal(t, desc, got.Description)
	require.Equal(t, "You review.\n", got.Body)

	// And the imported agent must render on the next sync.
	results, err := o.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Written, filepath.Join(".ha", "agents", "reviewer.md"))
}

// A canonical agent with an empty body is valid on the markdown harnesses but
// unrepresentable on codex — sync must warn-drop that one target, not fail.
func TestSync_EmptyBodyAgentWarnDropped(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	require.NoError(t, p.InstallAgent(&adept.Agent{ID: "stub", Description: "d", Mode: adept.AgentModeSubagent}))
	setHarnesses(t, p, "dropper")

	adapter := &agentMockAdapter{mockAdapter: perSkillAdapter("dropper", nil)}
	adapter.renderAgent = func(in adept.AgentRenderInput) (adept.RenderOutput, error) {
		if in.Agent.Body == "" {
			return adept.RenderOutput{SkillID: in.Agent.ID, Warnings: []string{"dropper: empty body dropped"}}, nil
		}
		return adept.RenderOutput{Path: ".dropper/agents/" + in.Agent.ID + ".md", Bytes: []byte("x"), Mode: 0o644, SkillID: in.Agent.ID}, nil
	}
	o := newOrch(t, adapter)
	results, err := o.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err, "unrepresentable agent must not fail the sync")
	require.Len(t, results, 1)
	require.NotEmpty(t, results[0].Warnings)
	assert.Contains(t, results[0].Warnings[0], "empty body dropped")
	assert.NoFileExists(t, filepath.Join(p.Root(), ".dropper", "agents", "stub.md"))
}
