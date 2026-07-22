package harness

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/pkg/adept"
)

// agentImportIDRE guards ids derived from harness filenames before they reach
// InstallAgent. Importers walk arbitrary user directories, so a stray
// "My-Agent.md" must become a skip row, not an abort mid-import.
var agentImportIDRE = regexp.MustCompile(adept.AgentIDPattern)

// Agent orchestration. Agents piggyback on the skill sync/status/import
// passes: the same SyncResult/DriftReport carry agent paths alongside skill
// paths, so the CLI layer needs no new plumbing. Harnesses opt in by
// implementing adept.AgentSupport; the rest warn-drop, mirroring how
// single-file harnesses drop skill sidecars.
//
// NOTE: unrelated to internal/cli's "vercel agents" — those are harness
// render TARGETS from the vercel-labs skills matrix, not subagents.

// AgentImportReport summarizes one agent import run.
type AgentImportReport struct {
	Imported  []ImportedAgentRow `json:"imported"`
	Conflicts []ConflictRow      `json:"conflicts,omitempty"`
	Skipped   []SkipRow          `json:"skipped,omitempty"`
}

// ImportedAgentRow is one canonical agent written into the project.
type ImportedAgentRow struct {
	AgentID    string `json:"agentId"`
	Harness    string `json:"harness"`
	SourcePath string `json:"sourcePath"`
	// Warnings carries the importer's non-fatal recovery notes (e.g. harness
	// fields with no canonical analog that were dropped).
	Warnings []string `json:"warnings,omitempty"`
}

// syncHarnessAgents renders and materializes every applicable agent for one
// harness. Mirrors syncHarness without sidecars or aggregation: agents are
// per-file on every harness, even aggregator ones.
func (o *orchestrator) syncHarnessAgents(
	ctx context.Context,
	p project.Project,
	adapter adept.HarnessAdapter,
	agents []*adept.Agent,
	opts SyncOptions,
	mode adept.HarnessMode,
) (SyncResult, adept.HarnessMode, error) {
	spec := adapter.Spec()
	res := SyncResult{Harness: spec.ID}
	applicable := filterAgentTargets(agents, spec.ID)
	if len(applicable) == 0 {
		return res, mode, nil
	}
	as, ok := adapter.(adept.AgentSupport)
	if !ok {
		ids := make([]string, 0, len(applicable))
		for _, a := range applicable {
			ids = append(ids, a.ID)
		}
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: harness does not support agents; skipped %d agent(s): %v", spec.ID, len(ids), ids))
		return res, mode, nil
	}
	outputs, warnings, err := o.renderAllAgents(ctx, as, spec, applicable, p)
	if err != nil {
		return res, mode, err
	}
	res.Warnings = append(res.Warnings, warnings...)
	resolvedMode := mode
	for _, out := range outputs {
		absPath := filepath.Join(p.Root(), out.Path)
		if !opts.Force && !opts.DryRun {
			if same, _ := o.bytesAlreadyOnDisk(absPath, out.Bytes); same {
				res.Skipped = append(res.Skipped, out.Path)
				continue
			}
		}
		if opts.DryRun {
			res.Written = append(res.Written, out.Path)
			continue
		}
		written, flipped, err := o.write(p, absPath, out, resolvedMode)
		if err != nil {
			return res, resolvedMode, err
		}
		if flipped {
			resolvedMode = adept.ModeCopy
		}
		if written {
			res.Written = append(res.Written, out.Path)
		}
	}
	drift, err := as.ValidateAgents(p.Root(), outputs)
	if err != nil {
		return res, resolvedMode, fmt.Errorf("validate agents: %w", err)
	}
	res.Drift = drift
	sort.Strings(res.Written)
	sort.Strings(res.Skipped)
	return res, resolvedMode, nil
}

// renderAllAgents fans agent rendering out across CPUs, mirroring renderAll.
// Outputs with an empty Path are dropped (the renderer's "not representable
// on this harness" signal, e.g. codex + empty body); their warnings — and
// every other render warning — are returned separately so callers surface
// them even for dropped outputs.
func (o *orchestrator) renderAllAgents(
	ctx context.Context,
	as adept.AgentSupport,
	spec adept.HarnessSpec,
	agents []*adept.Agent,
	p project.Project,
) ([]adept.RenderOutput, []string, error) {
	renderer := as.AgentRenderer()
	if renderer == nil {
		return nil, nil, fmt.Errorf("adapter %q: %w: nil agent renderer", spec.ID, adept.ErrAdapterInvalid)
	}
	projInfo := adept.ProjectInfo{Name: filepath.Base(p.Root()), Root: p.Root()}
	outputs := make([]adept.RenderOutput, len(agents))
	g, gctx := errgroup.WithContext(ctx)
	limit := runtime.NumCPU()
	if limit < 1 {
		limit = 1
	}
	g.SetLimit(limit)
	for i, agent := range agents {
		g.Go(func() error {
			out, err := renderer.RenderAgent(gctx, adept.AgentRenderInput{Agent: agent, Harness: spec, Project: projInfo})
			if err != nil {
				return fmt.Errorf("render agent %q for %q: %w", agent.ID, spec.ID, err)
			}
			if out.SkillID == "" {
				out.SkillID = agent.ID
			}
			outputs[i] = out
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	var warnings []string
	kept := outputs[:0]
	for _, out := range outputs {
		warnings = append(warnings, out.Warnings...)
		if out.Path == "" {
			continue
		}
		kept = append(kept, out)
	}
	return kept, warnings, nil
}

// mergeAgentSyncResult folds an agent pass result into the harness's skill
// result: written/skipped/warning rows append, drift lists concatenate.
func mergeAgentSyncResult(res *SyncResult, agentRes SyncResult) {
	res.Written = append(res.Written, agentRes.Written...)
	res.Skipped = append(res.Skipped, agentRes.Skipped...)
	res.Warnings = append(res.Warnings, agentRes.Warnings...)
	mergeDrift(&res.Drift, agentRes.Drift)
	sort.Strings(res.Written)
	sort.Strings(res.Skipped)
}

// mergeDrift concatenates the four drift lists, keeping each sorted.
func mergeDrift(dst *adept.DriftReport, src adept.DriftReport) {
	dst.Synced = append(dst.Synced, src.Synced...)
	dst.Drifted = append(dst.Drifted, src.Drifted...)
	dst.Missing = append(dst.Missing, src.Missing...)
	dst.Conflict = append(dst.Conflict, src.Conflict...)
	sort.Strings(dst.Synced)
	sort.Strings(dst.Drifted)
	sort.Strings(dst.Missing)
	sort.Strings(dst.Conflict)
}

// ImportAgents reverse-renders on-disk harness agent files into project
// canonical agents, mirroring Import for skills.
func (o *orchestrator) ImportAgents(ctx context.Context, p project.Project, opts ImportOptions) (AgentImportReport, error) {
	if opts.Strategy == "" {
		opts.Strategy = ImportStrategyFirst
	}
	report := AgentImportReport{}
	adapters := o.selectAdaptersForImport(opts.HarnessIDs)
	if len(adapters) == 0 {
		return report, fmt.Errorf("import agents: %w", adept.ErrHarnessUnknown)
	}

	contributions := map[string][]agentImportContribution{}
	for _, a := range adapters {
		hid := a.Spec().ID
		as, ok := a.(adept.AgentSupport)
		if !ok {
			continue // harness has no agent concept; nothing to report
		}
		agents, err := as.ImportAgents(ctx, p.Root())
		if err != nil {
			report.Skipped = append(report.Skipped, SkipRow{Harness: hid, Reason: err.Error()})
			continue
		}
		if len(agents) == 0 {
			continue
		}
		for _, imp := range agents {
			// Guard filename-derived ids and required fields here, once,
			// instead of aborting the whole run (after partial installs)
			// when InstallAgent or a later render rejects them.
			if imp.Agent == nil || !agentImportIDRE.MatchString(imp.Agent.ID) {
				report.Skipped = append(report.Skipped, SkipRow{Harness: hid, Reason: fmt.Sprintf("skipped %s: id does not match %s", imp.SourcePath, adept.AgentIDPattern)})
				continue
			}
			if strings.TrimSpace(imp.Agent.Description) == "" {
				report.Skipped = append(report.Skipped, SkipRow{Harness: hid, Reason: fmt.Sprintf("skipped %s: missing description (required to render; add one and re-import)", imp.SourcePath)})
				continue
			}
			contributions[imp.Agent.ID] = append(contributions[imp.Agent.ID], agentImportContribution{harness: hid, imported: imp})
		}
	}

	ids := make([]string, 0, len(contributions))
	for id := range contributions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		entries := contributions[id]
		chosen, conflict := resolveAgentImport(entries, opts)
		hadHarnessConflict := conflict != nil
		if hadHarnessConflict {
			report.Conflicts = append(report.Conflicts, *conflict)
			if opts.Strategy == ImportStrategyError {
				return report, fmt.Errorf("import agents: %w: agent %q reported by %v", adept.ErrAgentInvalid, id, conflict.From)
			}
		}
		if chosen == nil {
			continue
		}
		if !opts.Force && p.HasAgent(id) {
			blocked := fmt.Sprintf("kept project canonical (would have applied %s; pass --force to overwrite)", chosen.harness)
			if hadHarnessConflict {
				last := &report.Conflicts[len(report.Conflicts)-1]
				last.Resolved = blocked
			} else {
				report.Conflicts = append(report.Conflicts, ConflictRow{
					SkillID:  id,
					From:     []string{"project-canonical"},
					Resolved: blocked,
				})
			}
			continue
		}
		if !opts.DryRun {
			if err := p.InstallAgent(chosen.imported.Agent); err != nil {
				return report, fmt.Errorf("import agents: install %s: %w", id, err)
			}
		}
		report.Imported = append(report.Imported, ImportedAgentRow{
			AgentID:    id,
			Harness:    chosen.harness,
			SourcePath: chosen.imported.SourcePath,
			Warnings:   chosen.imported.Warnings,
		})
	}
	return report, nil
}

type agentImportContribution struct {
	harness  string
	imported adept.ImportedAgent
}

func resolveAgentImport(entries []agentImportContribution, opts ImportOptions) (*agentImportContribution, *ConflictRow) {
	if len(entries) == 0 {
		return nil, nil
	}
	if len(entries) == 1 {
		return &entries[0], nil
	}
	from := make([]string, 0, len(entries))
	for _, e := range entries {
		from = append(from, e.harness)
	}
	row := &ConflictRow{SkillID: entries[0].imported.Agent.ID, From: from}
	switch opts.Strategy {
	case ImportStrategyPrefer:
		for i := range entries {
			if entries[i].harness == opts.PreferHarness {
				row.Resolved = opts.PreferHarness
				return &entries[i], row
			}
		}
		row.Resolved = entries[0].harness
		return &entries[0], row
	case ImportStrategyError:
		return nil, row
	default:
		row.Resolved = entries[0].harness
		return &entries[0], row
	}
}

func filterAgentTargets(agents []*adept.Agent, harnessID string) []*adept.Agent {
	out := make([]*adept.Agent, 0, len(agents))
	for _, a := range agents {
		if len(a.Targets) == 0 {
			out = append(out, a)
			continue
		}
		for _, t := range a.Targets {
			if t == harnessID {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

// cursorClaudeAgentDedupWarning flags the double-surface case: Cursor also
// reads .claude/agents/, so an agent rendered to BOTH harnesses shows twice
// inside Cursor. Only agents applicable to both count — one scoped
// `targets: [cursor]` never lands in .claude/agents/ and cannot duplicate.
// The user-side fix is `targets:`.
func cursorClaudeAgentDedupWarning(harnessIDs []string, agents []*adept.Agent) string {
	hasCursor, hasClaude := false, false
	for _, id := range harnessIDs {
		switch id {
		case "cursor":
			hasCursor = true
		case "claude-code":
			hasClaude = true
		}
	}
	if !hasCursor || !hasClaude {
		return ""
	}
	claudeIDs := map[string]bool{}
	for _, a := range filterAgentTargets(agents, "claude-code") {
		claudeIDs[a.ID] = true
	}
	overlap := 0
	for _, a := range filterAgentTargets(agents, "cursor") {
		if claudeIDs[a.ID] {
			overlap++
		}
	}
	if overlap == 0 {
		return ""
	}
	return fmt.Sprintf("cursor: Cursor also reads .claude/agents/ — %d agent(s) synced to both harnesses appear twice in Cursor (scope with `targets:` to avoid duplicates)", overlap)
}
