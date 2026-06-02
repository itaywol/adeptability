package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/config"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/hash"
	"github.com/itaywol/adeptability/internal/log"
	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/pkg/adept"
)

// rendererFunc is a tiny adept.Renderer adapter so tests can supply behaviour
// inline.
type rendererFunc func(ctx context.Context, in adept.RenderInput) (adept.RenderOutput, error)

func (f rendererFunc) Render(ctx context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
	return f(ctx, in)
}

func newProj(t *testing.T) project.Project {
	t.Helper()
	root := t.TempDir()
	return project.New(root, canonical.NewParser(), hash.NewHasher(), config.NewStore(nil), fsutil.NewWriter())
}

func installSkill(t *testing.T, p project.Project, id string) {
	t.Helper()
	s := &adept.Skill{
		ID:          id,
		Description: "d " + id,
		Activation:  adept.ActivationAgent,
		Body:        "# " + id + "\n",
	}
	require.NoError(t, p.InstallSkill(s, nil))
}

func setHarnesses(t *testing.T, p project.Project, ids ...string) {
	t.Helper()
	cfg, err := p.Config()
	require.NoError(t, err)
	cfg.Harnesses = ids
	require.NoError(t, p.SaveConfig(cfg))
}

// setHarnessMode now writes the project-wide Mode (per-harness modes were
// removed in the v0.3 UX refactor — every harness uses the global default).
// The id parameter is preserved so existing call sites compile; the value
// is ignored.
func setHarnessMode(t *testing.T, p project.Project, _ string, mode adept.HarnessMode) {
	t.Helper()
	cfg, err := p.Config()
	require.NoError(t, err)
	cfg.Mode = mode
	require.NoError(t, p.SaveConfig(cfg))
}

func perSkillAdapter(id string, counter *atomic.Int32) *mockAdapter {
	return &mockAdapter{
		spec: adept.HarnessSpec{ID: id, Kind: adept.KindPerSkill, OutputPath: "." + id + "/{id}.md"},
		render: rendererFunc(func(_ context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
			if counter != nil {
				counter.Add(1)
			}
			path := filepath.Join("."+id, in.Skill.ID+".md")
			return adept.RenderOutput{
				Path:    path,
				Bytes:   []byte("body for " + in.Skill.ID),
				Mode:    0o644,
				SkillID: in.Skill.ID,
			}, nil
		}),
	}
}

func aggregatorAdapter(id, outPath string) *mockAdapter {
	return &mockAdapter{
		spec: adept.HarnessSpec{ID: id, Kind: adept.KindAggregatorSingle, OutputPath: outPath},
		render: rendererFunc(func(_ context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
			return adept.RenderOutput{
				Path:    "tmp/" + in.Skill.ID + ".part",
				Bytes:   []byte("part:" + in.Skill.ID + "\n"),
				SkillID: in.Skill.ID,
			}, nil
		}),
		agg: func(_ context.Context, parts []adept.RenderOutput, _ int) ([]adept.RenderOutput, error) {
			merged := []byte{}
			ids := []string{}
			for _, p := range parts {
				merged = append(merged, p.Bytes...)
				ids = append(ids, p.SkillID)
			}
			out := adept.RenderOutput{Path: outPath, Bytes: merged, Mode: 0o644}
			if len(ids) > 0 {
				out.SkillID = ids[0]
			}
			return []adept.RenderOutput{out}, nil
		},
	}
}

func newOrch(t *testing.T, adapters ...adept.HarnessAdapter) Orchestrator {
	t.Helper()
	reg := NewRegistry()
	for _, a := range adapters {
		require.NoError(t, reg.Register(a))
	}
	w := fsutil.NewWriter()
	l := fsutil.NewLinker(w)
	return NewOrchestrator(reg, canonical.NewParser(), w, l, log.Nop())
}

func TestOrchestrator_Sync_PerSkillWritesFiles(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	installSkill(t, p, "skill-b")
	setHarnesses(t, p, "alpha")

	counter := &atomic.Int32{}
	a := perSkillAdapter("alpha", counter)
	orch := newOrch(t, a)
	setHarnessMode(t, p, "alpha", adept.ModeCopy)

	results, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "alpha", results[0].Harness)
	require.Len(t, results[0].Written, 2)
	require.Equal(t, int32(2), counter.Load())

	root := p.Root()
	for _, id := range []string{"skill-a", "skill-b"} {
		path := filepath.Join(root, ".alpha", id+".md")
		require.FileExists(t, path)
	}
}

func TestOrchestrator_Sync_DryRunWritesNothing(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	setHarnesses(t, p, "alpha")

	orch := newOrch(t, perSkillAdapter("alpha", nil))
	results, err := orch.Sync(context.Background(), p, SyncOptions{DryRun: true})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NotEmpty(t, results[0].Written)
	_, err = os.Stat(filepath.Join(p.Root(), ".alpha"))
	require.True(t, os.IsNotExist(err))
}

func TestOrchestrator_Sync_SkipsUnchangedOutputs(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	setHarnesses(t, p, "alpha")
	setHarnessMode(t, p, "alpha", adept.ModeCopy)
	orch := newOrch(t, perSkillAdapter("alpha", nil))

	first, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, first[0].Written, 1)

	second, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Empty(t, second[0].Written)
	require.Len(t, second[0].Skipped, 1)
}

func TestOrchestrator_Sync_ForceRewrites(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	setHarnesses(t, p, "alpha")
	setHarnessMode(t, p, "alpha", adept.ModeCopy)
	orch := newOrch(t, perSkillAdapter("alpha", nil))

	_, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	second, err := orch.Sync(context.Background(), p, SyncOptions{Force: true})
	require.NoError(t, err)
	require.Len(t, second[0].Written, 1)
	require.Empty(t, second[0].Skipped)
}

func TestOrchestrator_Sync_AggregatorPath(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	installSkill(t, p, "skill-b")
	setHarnesses(t, p, "agents")
	setHarnessMode(t, p, "agents", adept.ModeCopy)

	orch := newOrch(t, aggregatorAdapter("agents", "AGENTS.md"))
	results, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, results[0].Written, 1)
	require.Equal(t, "AGENTS.md", results[0].Written[0])
	data, err := os.ReadFile(filepath.Join(p.Root(), "AGENTS.md"))
	require.NoError(t, err)
	require.Contains(t, string(data), "part:skill-a")
	require.Contains(t, string(data), "part:skill-b")
}

// FRICTION BUG 7: when the aggregator drops a skill, the orchestrator must
// surface the dropped skill id in SyncResult.DroppedSkillIDs.
func TestOrchestrator_Sync_AggregatorDropsReported(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-keep")
	installSkill(t, p, "skill-drop")
	setHarnesses(t, p, "agents")
	setHarnessMode(t, p, "agents", adept.ModeCopy)

	// Aggregator returns only skill-keep; skill-drop is silently omitted.
	dropAdapter := &mockAdapter{
		spec: adept.HarnessSpec{ID: "agents", Kind: adept.KindAggregatorSingle, OutputPath: "AGENTS.md"},
		render: rendererFunc(func(_ context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
			return adept.RenderOutput{
				Path: "tmp/" + in.Skill.ID, Bytes: []byte(in.Skill.ID), SkillID: in.Skill.ID,
			}, nil
		}),
		agg: func(_ context.Context, parts []adept.RenderOutput, _ int) ([]adept.RenderOutput, error) {
			for _, p := range parts {
				if p.SkillID == "skill-keep" {
					return []adept.RenderOutput{{Path: "AGENTS.md", Bytes: p.Bytes, Mode: 0o644, SkillID: "skill-keep"}}, nil
				}
			}
			return nil, nil
		},
	}
	orch := newOrch(t, dropAdapter)
	results, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Contains(t, results[0].DroppedSkillIDs, "skill-drop")
	require.NotContains(t, results[0].DroppedSkillIDs, "skill-keep")
}

func TestOrchestrator_Sync_HarnessIDsOverrideConfig(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	setHarnesses(t, p, "alpha", "beta")
	setHarnessMode(t, p, "alpha", adept.ModeCopy)
	setHarnessMode(t, p, "beta", adept.ModeCopy)

	orch := newOrch(t, perSkillAdapter("alpha", nil), perSkillAdapter("beta", nil))
	results, err := orch.Sync(context.Background(), p, SyncOptions{HarnessIDs: []string{"beta"}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "beta", results[0].Harness)
}

func TestOrchestrator_Sync_UnknownHarnessFails(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	setHarnesses(t, p, "ghost")
	orch := newOrch(t)
	_, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.Error(t, err)
}

func TestOrchestrator_Status_NoHarnessesReturnsNil(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	orch := newOrch(t)
	reports, err := orch.Status(context.Background(), p, nil)
	require.NoError(t, err)
	require.Empty(t, reports)
}

func TestOrchestrator_Status_ReportsMissing(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "skill-a")
	setHarnesses(t, p, "alpha")

	a := perSkillAdapter("alpha", nil)
	a.valid = func(root string, expected []adept.RenderOutput) (adept.DriftReport, error) {
		dr := adept.DriftReport{}
		for _, e := range expected {
			abs := filepath.Join(root, e.Path)
			if _, err := os.Stat(abs); err != nil {
				dr.Missing = append(dr.Missing, e.Path)
			} else {
				dr.Synced = append(dr.Synced, e.Path)
			}
		}
		return dr, nil
	}
	orch := newOrch(t, a)
	reports, err := orch.Status(context.Background(), p, nil)
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Equal(t, []string{filepath.Join(".alpha", "skill-a.md")}, reports[0].Missing)
}

func TestOrchestrator_Import_EmptyAdapterReturnsNoSkills(t *testing.T) {
	p := newProj(t)
	orch := newOrch(t, perSkillAdapter("alpha", nil))
	report, err := orch.Import(context.Background(), p, ImportOptions{HarnessIDs: []string{"alpha"}})
	require.NoError(t, err)
	require.Empty(t, report.Imported)
	require.Len(t, report.Skipped, 1)
	require.Equal(t, "alpha", report.Skipped[0].Harness)
}

func TestOrchestrator_Import_FirstWinsOnConflict(t *testing.T) {
	p := newProj(t)
	contribution := func(id, body string) func(context.Context, string) ([]adept.ImportedSkill, error) {
		return func(context.Context, string) ([]adept.ImportedSkill, error) {
			return []adept.ImportedSkill{{
				Skill:      &adept.Skill{ID: id, Description: "x", Activation: adept.ActivationAgent, Body: body},
				SourcePath: "/fake/" + id,
			}}, nil
		}
	}
	a1 := perSkillAdapter("alpha", nil)
	a1.imports = contribution("collide", "from-alpha")
	a2 := perSkillAdapter("beta", nil)
	a2.imports = contribution("collide", "from-beta")
	orch := newOrch(t, a1, a2)
	report, err := orch.Import(context.Background(), p, ImportOptions{Strategy: ImportStrategyFirst})
	require.NoError(t, err)
	require.Len(t, report.Imported, 1)
	require.Equal(t, "alpha", report.Imported[0].Harness)
	require.Len(t, report.Conflicts, 1)
	require.ElementsMatch(t, []string{"alpha", "beta"}, report.Conflicts[0].From)
}

func TestOrchestrator_Import_PreferStrategy(t *testing.T) {
	p := newProj(t)
	contribution := func(id, body string) func(context.Context, string) ([]adept.ImportedSkill, error) {
		return func(context.Context, string) ([]adept.ImportedSkill, error) {
			return []adept.ImportedSkill{{
				Skill:      &adept.Skill{ID: id, Description: "x", Activation: adept.ActivationAgent, Body: body},
				SourcePath: "/fake/" + id,
			}}, nil
		}
	}
	a1 := perSkillAdapter("alpha", nil)
	a1.imports = contribution("collide", "from-alpha")
	a2 := perSkillAdapter("beta", nil)
	a2.imports = contribution("collide", "from-beta")
	orch := newOrch(t, a1, a2)
	report, err := orch.Import(context.Background(), p, ImportOptions{Strategy: ImportStrategyPrefer, PreferHarness: "beta"})
	require.NoError(t, err)
	require.Len(t, report.Imported, 1)
	require.Equal(t, "beta", report.Imported[0].Harness)
}

func TestOrchestrator_Import_CollidesWithProjectCanonical(t *testing.T) {
	p := newProj(t)
	// Pre-install the same id directly.
	installSkill(t, p, "collide")

	contribution := func(id, body string) func(context.Context, string) ([]adept.ImportedSkill, error) {
		return func(context.Context, string) ([]adept.ImportedSkill, error) {
			return []adept.ImportedSkill{{
				Skill:      &adept.Skill{ID: id, Description: "x", Activation: adept.ActivationAgent, Body: body},
				SourcePath: "/fake/" + id,
			}}, nil
		}
	}
	a1 := perSkillAdapter("alpha", nil)
	a1.imports = contribution("collide", "from-alpha")
	orch := newOrch(t, a1)
	report, err := orch.Import(context.Background(), p, ImportOptions{})
	require.NoError(t, err)
	require.Empty(t, report.Imported)
	require.Len(t, report.Conflicts, 1)
	require.Contains(t, report.Conflicts[0].From, "project-canonical")
	require.Contains(t, report.Conflicts[0].Resolved, "kept project canonical")
	require.Contains(t, report.Conflicts[0].Resolved, "alpha")
}

// Covers the merged-conflict branch: multiple harnesses contribute the same
// id AND the project already has a canonical copy. We expect a SINGLE
// conflict row whose Resolved column captures the strategy outcome AND the
// block — never two rows for the same skill.
func TestOrchestrator_Import_MultiHarnessAndProjectCanonicalCollapsesToOneRow(t *testing.T) {
	p := newProj(t)
	installSkill(t, p, "collide")

	contribution := func(id, body string) func(context.Context, string) ([]adept.ImportedSkill, error) {
		return func(context.Context, string) ([]adept.ImportedSkill, error) {
			return []adept.ImportedSkill{{
				Skill:      &adept.Skill{ID: id, Description: "x", Activation: adept.ActivationAgent, Body: body},
				SourcePath: "/fake/" + id,
			}}, nil
		}
	}
	a1 := perSkillAdapter("alpha", nil)
	a1.imports = contribution("collide", "from-alpha")
	a2 := perSkillAdapter("beta", nil)
	a2.imports = contribution("collide", "from-beta")
	orch := newOrch(t, a1, a2)
	report, err := orch.Import(context.Background(), p, ImportOptions{Strategy: ImportStrategyFirst})
	require.NoError(t, err)
	require.Empty(t, report.Imported)
	require.Len(t, report.Conflicts, 1, "must collapse harness conflict + project-canonical block into one row")
	require.ElementsMatch(t, []string{"alpha", "beta"}, report.Conflicts[0].From)
	require.Contains(t, report.Conflicts[0].Resolved, "kept project canonical")
	require.Contains(t, report.Conflicts[0].Resolved, "alpha")
}

func TestOrchestrator_Sync_FiltersByTargets(t *testing.T) {
	p := newProj(t)
	s := &adept.Skill{
		ID: "skill-a", Description: "d", Activation: adept.ActivationAgent,
		Body: "x\n", Targets: []string{"beta"},
	}
	require.NoError(t, p.InstallSkill(s, nil))
	installSkill(t, p, "skill-b")
	setHarnesses(t, p, "alpha")
	setHarnessMode(t, p, "alpha", adept.ModeCopy)

	orch := newOrch(t, perSkillAdapter("alpha", nil))
	results, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, results[0].Written, 1)
	require.Contains(t, results[0].Written[0], "skill-b")
}

func TestOrchestrator_Sync_ConcurrentRenderIsRaceClean(t *testing.T) {
	p := newProj(t)
	for i := 0; i < 16; i++ {
		installSkill(t, p, fmt.Sprintf("s%02d", i))
	}
	setHarnesses(t, p, "alpha")
	setHarnessMode(t, p, "alpha", adept.ModeCopy)

	var mu sync.Mutex
	seen := map[string]int{}
	a := &mockAdapter{
		spec: adept.HarnessSpec{ID: "alpha", Kind: adept.KindPerSkill, OutputPath: ".alpha/{id}.md"},
		render: rendererFunc(func(_ context.Context, in adept.RenderInput) (adept.RenderOutput, error) {
			mu.Lock()
			seen[in.Skill.ID]++
			mu.Unlock()
			return adept.RenderOutput{
				Path: filepath.Join(".alpha", in.Skill.ID+".md"), Bytes: []byte(in.Skill.ID), Mode: 0o644,
			}, nil
		}),
	}
	orch := newOrch(t, a)
	results, err := orch.Sync(context.Background(), p, SyncOptions{})
	require.NoError(t, err)
	require.Len(t, results[0].Written, 16)
	require.Len(t, seen, 16)
}
