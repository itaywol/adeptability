package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"golang.org/x/sync/errgroup"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/fsutil"
	"github.com/itaywol/adeptability/internal/log"
	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/pkg/adept"
)

// SyncOptions configures a single Sync invocation.
type SyncOptions struct {
	// HarnessIDs selects which adapters to run. Empty means "every harness
	// enabled in the project lockfile".
	HarnessIDs []string
	// Force ignores existing on-disk files: outputs are rewritten even when
	// the current bytes match.
	Force bool
	// DryRun computes the planned outputs but skips writes.
	DryRun bool
}

// SyncResult summarizes what Sync did (or would have done, when DryRun) for
// a single harness.
type SyncResult struct {
	Harness string
	Written []string
	Skipped []string
	Dropped []string
	Drift   adept.DriftReport
}

// Orchestrator drives the harness adapters. It is the only component that
// knows how to fan out rendering across CPUs, aggregate per harness, and
// apply the symlink-or-copy contract.
type Orchestrator interface {
	Sync(ctx context.Context, p project.Project, opts SyncOptions) ([]SyncResult, error)
	Status(ctx context.Context, p project.Project, harnessIDs []string) ([]adept.DriftReport, error)
	Import(ctx context.Context, p project.Project, opts ImportOptions) (ImportReport, error)
}

// NewOrchestrator wires the orchestrator. The canonical.Parser is used by
// Import to ingest harness files back into project canonical form (TODO:
// per-harness reverse rendering lives in the adapter; for v0.1 we leave a
// scaffold).
func NewOrchestrator(reg Registry, parser canonical.Parser, w fsutil.Writer, l fsutil.Linker, lg log.Logger) Orchestrator {
	if lg == nil {
		lg = log.Nop()
	}
	return &orchestrator{
		reg:    reg,
		parser: parser,
		writer: w,
		linker: l,
		log:    lg,
	}
}

type orchestrator struct {
	reg    Registry
	parser canonical.Parser
	writer fsutil.Writer
	linker fsutil.Linker
	log    log.Logger
}

func (o *orchestrator) Sync(ctx context.Context, p project.Project, opts SyncOptions) ([]SyncResult, error) {
	lf, err := p.Lock()
	if err != nil {
		return nil, fmt.Errorf("sync: load lock: %w", err)
	}
	harnessIDs := opts.HarnessIDs
	if len(harnessIDs) == 0 {
		harnessIDs = append([]string{}, lf.Harnesses...)
	}
	if len(harnessIDs) == 0 {
		return nil, nil
	}
	skills, err := p.ListSkills()
	if err != nil {
		return nil, fmt.Errorf("sync: list skills: %w", err)
	}
	results := make([]SyncResult, 0, len(harnessIDs))
	// Per-harness mode bookkeeping: if any symlink falls back to a copy, we
	// persist the mode flip into the lockfile.
	modes := cloneModes(lf.HarnessModes)
	modeChanged := false
	for _, hid := range harnessIDs {
		adapter, err := o.reg.Get(hid)
		if err != nil {
			return results, fmt.Errorf("sync %q: %w", hid, err)
		}
		desiredMode := defaultMode(modes, hid)
		res, flippedMode, err := o.syncHarness(ctx, p, adapter, skills, opts, desiredMode)
		if err != nil {
			return results, fmt.Errorf("sync %q: %w", hid, err)
		}
		results = append(results, res)
		if flippedMode != desiredMode {
			modes[hid] = flippedMode
			modeChanged = true
		}
	}
	if !opts.DryRun && modeChanged {
		lf.HarnessModes = modes
		if err := p.SaveLock(lf); err != nil {
			return results, fmt.Errorf("sync: persist mode flip: %w", err)
		}
	}
	return results, nil
}

func (o *orchestrator) syncHarness(
	ctx context.Context,
	p project.Project,
	adapter adept.HarnessAdapter,
	skills []*adept.Skill,
	opts SyncOptions,
	mode adept.HarnessMode,
) (SyncResult, adept.HarnessMode, error) {
	spec := adapter.Spec()
	res := SyncResult{Harness: spec.ID}
	// Filter by skill.Targets when set.
	applicable := filterTargets(skills, spec.ID)
	if len(applicable) == 0 {
		return res, mode, nil
	}
	// Concurrent render fan-out.
	parts, dropped, err := o.renderAll(ctx, adapter, applicable, p)
	if err != nil {
		return res, mode, err
	}
	res.Dropped = dropped
	// Aggregator step (no-op for per-skill).
	outputs := parts
	if spec.Kind != adept.KindPerSkill {
		aggregated, err := adapter.Aggregate(ctx, parts, spec.SizeBudgetB)
		if err != nil {
			return res, mode, fmt.Errorf("aggregate: %w", err)
		}
		outputs = aggregated
	}
	// Materialize on disk.
	resolvedMode := mode
	for _, out := range outputs {
		absPath := filepath.Join(p.Root(), out.Path)
		// Skip writes that match existing content unless Force is set.
		if !opts.Force && !opts.DryRun {
			same, _ := o.bytesAlreadyOnDisk(absPath, out.Bytes)
			if same {
				res.Skipped = append(res.Skipped, out.Path)
				continue
			}
		}
		if opts.DryRun {
			res.Written = append(res.Written, out.Path)
		} else {
			written, flipped, err := o.write(p.Root(), absPath, out, resolvedMode)
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
		// Sidecars land next to the rendered main file. The same materialization
		// contract applies: in symlink mode bytes live in
		// .adeptability/staging/<harness-path>/<rel>, with the harness path
		// pointing back via a relative symlink — so any tool that reads
		// SKILL.md via the harness path resolves "references/X.md" against the
		// harness directory and finds the sidecar (real symlink or real file).
		// In copy mode the bytes simply go straight to the harness path.
		for _, side := range out.Sidecars {
			sideOutRel := filepath.Join(filepath.Dir(out.Path), side.RelPath)
			sideAbs := filepath.Join(p.Root(), sideOutRel)
			if opts.DryRun {
				res.Written = append(res.Written, sideOutRel)
				continue
			}
			mode := side.Mode
			if mode == 0 {
				mode = 0o644
			}
			sideOut := adept.RenderOutput{Path: sideOutRel, Bytes: side.Bytes, Mode: mode}
			written, flipped, err := o.write(p.Root(), sideAbs, sideOut, resolvedMode)
			if err != nil {
				return res, resolvedMode, fmt.Errorf("write sidecar %q: %w", sideOutRel, err)
			}
			if flipped {
				resolvedMode = adept.ModeCopy
			}
			if written {
				res.Written = append(res.Written, sideOutRel)
			}
		}
	}
	// Validate drift after writing (or for dry-run: against the desired set).
	if drift, err := adapter.Validate(p.Root(), outputs); err == nil {
		res.Drift = drift
	}
	sort.Strings(res.Written)
	sort.Strings(res.Skipped)
	sort.Strings(res.Dropped)
	return res, resolvedMode, nil
}

func (o *orchestrator) renderAll(
	ctx context.Context,
	adapter adept.HarnessAdapter,
	skills []*adept.Skill,
	p project.Project,
) ([]adept.RenderOutput, []string, error) {
	renderer := adapter.Renderer()
	if renderer == nil {
		return nil, nil, fmt.Errorf("adapter %q: %w: nil renderer", adapter.Spec().ID, adept.ErrAdapterInvalid)
	}
	spec := adapter.Spec()
	projInfo := adept.ProjectInfo{Name: filepath.Base(p.Root()), Root: p.Root()}

	outputs := make([]adept.RenderOutput, len(skills))
	g, gctx := errgroup.WithContext(ctx)
	limit := runtime.NumCPU()
	if limit < 1 {
		limit = 1
	}
	g.SetLimit(limit)
	for i, skill := range skills {
		i := i
		skill := skill
		g.Go(func() error {
			out, err := renderer.Render(gctx, adept.RenderInput{Skill: skill, Harness: spec, Project: projInfo})
			if err != nil {
				return fmt.Errorf("render %q for %q: %w", skill.ID, spec.ID, err)
			}
			if out.SkillID == "" {
				out.SkillID = skill.ID
			}
			if out.SkillVersion == 0 {
				out.SkillVersion = skill.Version
			}
			outputs[i] = out
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	// Drop renders that produced empty paths (e.g. skill not applicable for
	// this harness). We surface their ids so callers can report.
	filtered := outputs[:0]
	dropped := []string{}
	for _, o := range outputs {
		if o.Path == "" {
			dropped = append(dropped, o.SkillID)
			continue
		}
		filtered = append(filtered, o)
	}
	return filtered, dropped, nil
}

func (o *orchestrator) write(projectRoot, absPath string, out adept.RenderOutput, mode adept.HarnessMode) (bool, bool, error) {
	fileMode := out.Mode
	if fileMode == 0 {
		fileMode = 0o644
	}
	if mode != adept.ModeSymlink || o.linker == nil {
		if err := o.writer.AtomicWrite(absPath, out.Bytes, fileMode); err != nil {
			return false, false, fmt.Errorf("write %q: %w", absPath, err)
		}
		return true, false, nil
	}
	// Symlink path: the renderer's bytes are the desired link target's
	// contents. Materialize the bytes to a staging file inside the
	// project's .adeptability dir and link to it.
	staging := stagingPathFor(projectRoot, out)
	if err := o.writer.AtomicWrite(staging, out.Bytes, fileMode); err != nil {
		return false, false, fmt.Errorf("stage %q: %w", staging, err)
	}
	used, err := o.linker.SymlinkOrCopy(staging, absPath, false)
	if err != nil {
		return false, false, fmt.Errorf("symlink %q: %w", absPath, err)
	}
	if used == adept.ModeCopy {
		return true, true, nil
	}
	return true, false, nil
}

func (o *orchestrator) bytesAlreadyOnDisk(absPath string, want []byte) (bool, error) {
	got, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if len(got) != len(want) {
		return false, nil
	}
	for i := range got {
		if got[i] != want[i] {
			return false, nil
		}
	}
	return true, nil
}

func (o *orchestrator) Status(ctx context.Context, p project.Project, harnessIDs []string) ([]adept.DriftReport, error) {
	_ = ctx
	lf, err := p.Lock()
	if err != nil {
		return nil, fmt.Errorf("status: load lock: %w", err)
	}
	ids := harnessIDs
	if len(ids) == 0 {
		ids = append([]string{}, lf.Harnesses...)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	skills, err := p.ListSkills()
	if err != nil {
		return nil, fmt.Errorf("status: list skills: %w", err)
	}
	reports := make([]adept.DriftReport, 0, len(ids))
	for _, hid := range ids {
		adapter, err := o.reg.Get(hid)
		if err != nil {
			return reports, fmt.Errorf("status %q: %w", hid, err)
		}
		applicable := filterTargets(skills, hid)
		parts, _, err := o.renderAll(ctx, adapter, applicable, p)
		if err != nil {
			return reports, fmt.Errorf("status %q: %w", hid, err)
		}
		expected := parts
		spec := adapter.Spec()
		if spec.Kind != adept.KindPerSkill {
			expected, err = adapter.Aggregate(ctx, parts, spec.SizeBudgetB)
			if err != nil {
				return reports, fmt.Errorf("status %q: aggregate: %w", hid, err)
			}
		}
		report, err := adapter.Validate(p.Root(), expected)
		if err != nil {
			return reports, fmt.Errorf("status %q: validate: %w", hid, err)
		}
		reports = append(reports, report)
	}
	return reports, nil
}

// ImportStrategy controls how conflicts are resolved when the same skill ID
// appears in multiple harnesses with differing content.
type ImportStrategy string

const (
	// ImportStrategyFirst keeps the first occurrence (harness IDs walked in
	// registry order). Conflicts surface in the report.
	ImportStrategyFirst ImportStrategy = "first"
	// ImportStrategyError fails the import on any conflict.
	ImportStrategyError ImportStrategy = "error"
	// ImportStrategyPrefer keeps the entry from the harness named in
	// ImportOptions.PreferHarness.
	ImportStrategyPrefer ImportStrategy = "prefer"
)

// ImportOptions controls a single Import invocation.
type ImportOptions struct {
	// HarnessIDs limits the import to specific harnesses. Empty = all
	// registered harnesses (skipping those that don't support Import).
	HarnessIDs []string
	// Strategy decides conflict resolution. Default: ImportStrategyFirst.
	Strategy ImportStrategy
	// PreferHarness is read only when Strategy == ImportStrategyPrefer.
	PreferHarness string
	// DryRun reports what would be written without touching the project.
	DryRun bool
	// Force allows overwriting project canonical skills that already exist.
	// Without Force, existing project skills are reported as conflicts.
	Force bool
}

// ImportReport summarizes one import run.
type ImportReport struct {
	Imported  []ImportedRow `json:"imported"`
	Conflicts []ConflictRow `json:"conflicts,omitempty"`
	Skipped   []SkipRow     `json:"skipped,omitempty"`
}

// ImportedRow is one canonical skill written (or that would be written under
// --dry-run) into the project.
type ImportedRow struct {
	SkillID    string `json:"skillId"`
	Harness    string `json:"harness"`
	SourcePath string `json:"sourcePath"`
}

// ConflictRow records a clash between two harnesses, or between a harness
// import and an existing project canonical entry.
type ConflictRow struct {
	SkillID  string   `json:"skillId"`
	From     []string `json:"from"`
	Resolved string   `json:"resolved,omitempty"`
}

// SkipRow records why a harness contributed nothing.
type SkipRow struct {
	Harness string `json:"harness"`
	Reason  string `json:"reason"`
}

func (o *orchestrator) Import(ctx context.Context, p project.Project, opts ImportOptions) (ImportReport, error) {
	if opts.Strategy == "" {
		opts.Strategy = ImportStrategyFirst
	}
	report := ImportReport{}
	adapters := o.selectAdaptersForImport(opts.HarnessIDs)
	if len(adapters) == 0 {
		return report, fmt.Errorf("import: %w", adept.ErrHarnessUnknown)
	}

	contributions := map[string][]importContribution{}
	for _, a := range adapters {
		hid := a.Spec().ID
		skills, err := a.Import(ctx, p.Root())
		if err != nil {
			report.Skipped = append(report.Skipped, SkipRow{Harness: hid, Reason: err.Error()})
			continue
		}
		if len(skills) == 0 {
			report.Skipped = append(report.Skipped, SkipRow{Harness: hid, Reason: "no skills on disk"})
			continue
		}
		for _, s := range skills {
			contributions[s.Skill.ID] = append(contributions[s.Skill.ID], importContribution{harness: hid, imported: s})
		}
	}

	existingLock, err := p.Lock()
	if err != nil {
		return report, fmt.Errorf("import: read project lock: %w", err)
	}

	// Walk in stable id order so output is deterministic.
	ids := make([]string, 0, len(contributions))
	for id := range contributions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		entries := contributions[id]
		chosen, conflict := resolveImport(entries, opts)
		if conflict != nil {
			report.Conflicts = append(report.Conflicts, *conflict)
			if opts.Strategy == ImportStrategyError {
				return report, fmt.Errorf("import: %w: skill %q reported by %v", adept.ErrSkillInvalid, id, conflict.From)
			}
		}
		if chosen == nil {
			continue
		}
		// Detect collision with existing project canonical content.
		if !opts.Force {
			if existing, ok := existingLock.Skills[id]; ok && existing.Hash != "" {
				report.Conflicts = append(report.Conflicts, ConflictRow{
					SkillID: id,
					From:    []string{"project-canonical"},
				})
				continue
			}
		}
		if !opts.DryRun {
			libEntry := adept.LockEntry{Version: chosen.imported.Skill.Version}
			if err := p.InstallSkill(chosen.imported.Skill, chosen.imported.Files, libEntry); err != nil {
				return report, fmt.Errorf("import: install %s: %w", id, err)
			}
		}
		report.Imported = append(report.Imported, ImportedRow{
			SkillID:    id,
			Harness:    chosen.harness,
			SourcePath: chosen.imported.SourcePath,
		})
	}

	return report, nil
}

func (o *orchestrator) selectAdaptersForImport(ids []string) []adept.HarnessAdapter {
	if len(ids) > 0 {
		out := make([]adept.HarnessAdapter, 0, len(ids))
		for _, id := range ids {
			a, err := o.reg.Get(id)
			if err == nil {
				out = append(out, a)
			}
		}
		return out
	}
	return o.reg.List()
}

type importContribution struct {
	harness  string
	imported adept.ImportedSkill
}

func resolveImport(entries []importContribution, opts ImportOptions) (*importContribution, *ConflictRow) {
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
	row := &ConflictRow{SkillID: entries[0].imported.Skill.ID, From: from}
	switch opts.Strategy {
	case ImportStrategyPrefer:
		for i := range entries {
			if entries[i].harness == opts.PreferHarness {
				row.Resolved = opts.PreferHarness
				return &entries[i], row
			}
		}
		// Fall back to first when preferred harness not present.
		row.Resolved = entries[0].harness
		return &entries[0], row
	case ImportStrategyError:
		return nil, row
	default:
		row.Resolved = entries[0].harness
		return &entries[0], row
	}
}

func filterTargets(skills []*adept.Skill, harnessID string) []*adept.Skill {
	out := make([]*adept.Skill, 0, len(skills))
	for _, s := range skills {
		if len(s.Targets) == 0 {
			out = append(out, s)
			continue
		}
		for _, t := range s.Targets {
			if t == harnessID {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

func cloneModes(in map[string]adept.HarnessMode) map[string]adept.HarnessMode {
	out := map[string]adept.HarnessMode{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func defaultMode(modes map[string]adept.HarnessMode, hid string) adept.HarnessMode {
	if m, ok := modes[hid]; ok && m != "" {
		return m
	}
	return adept.ModeSymlink
}

// stagingPathFor derives the absolute path inside <projectRoot>/.adeptability
// where rendered bytes are materialized when running in symlink mode. The
// renderer's output Path is relative to the project root; we mirror it under
// .adeptability/staging/.
func stagingPathFor(projectRoot string, out adept.RenderOutput) string {
	return filepath.Join(projectRoot, adept.BaseDirName, "staging", out.Path)
}
