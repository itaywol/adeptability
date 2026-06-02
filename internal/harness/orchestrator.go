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
	// enabled in the project config".
	HarnessIDs []string
	// Force ignores existing on-disk files: outputs are rewritten even when
	// the current bytes match.
	Force bool
	// DryRun computes the planned outputs but skips writes.
	DryRun bool
	// Skills pre-resolves the skill set passed to the harness adapters.
	// When nil, orchestrator falls back to project.ListSkills (project-only
	// mode). The CLI layer fills this in with the project ∪ library union
	// so multi-library projects render library skills too.
	Skills []*adept.Skill
}

// SyncResult summarizes what Sync did (or would have done, when DryRun) for
// a single harness.
type SyncResult struct {
	Harness string
	Written []string
	Skipped []string
	// Dropped lists skill IDs that produced no render output (e.g. not
	// applicable to this harness). Used for diagnostics only.
	Dropped []string
	// DroppedSkillIDs lists skill IDs the aggregator dropped due to budget
	// pressure (FRICTION BUG 7). Distinct from Dropped which covers
	// pre-aggregation render misses; this surfaces post-aggregation losses
	// so users can see exactly what didn't fit.
	DroppedSkillIDs []string
	Drift           adept.DriftReport
}

// StatusOptions configures a single Status invocation.
type StatusOptions struct {
	// HarnessIDs limits the drift report to specific harnesses. Empty means
	// "every harness enabled in the project config".
	HarnessIDs []string
	// Skills pre-resolves the skill set used for the drift comparison.
	// When nil, orchestrator falls back to project.ListSkills (project-only
	// mode). The CLI fills this with the resolved union so multi-library
	// projects do not report library skills as "missing".
	Skills []*adept.Skill
}

// Orchestrator drives the harness adapters. It is the only component that
// knows how to fan out rendering across CPUs, aggregate per harness, and
// apply the symlink-or-copy contract.
type Orchestrator interface {
	Sync(ctx context.Context, p project.Project, opts SyncOptions) ([]SyncResult, error)
	Status(ctx context.Context, p project.Project, opts StatusOptions) ([]adept.DriftReport, error)
	Import(ctx context.Context, p project.Project, opts ImportOptions) (ImportReport, error)
}

// NewOrchestrator wires the orchestrator. The canonical.Parser is used by
// Import to ingest harness files back into project canonical form.
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
	cfg, err := p.Config()
	if err != nil {
		return nil, fmt.Errorf("sync: load config: %w", err)
	}
	harnessIDs := opts.HarnessIDs
	if len(harnessIDs) == 0 {
		harnessIDs = append([]string{}, cfg.Harnesses...)
	}
	if len(harnessIDs) == 0 {
		return nil, nil
	}
	// Resolved set = project canonical ∪ external sources (libraries),
	// with project shadowing libraries on shared ids. When opts.Skills is
	// non-nil the caller has already resolved; orchestrator stays oblivious
	// to library plumbing.
	var skills []*adept.Skill
	if opts.Skills != nil {
		skills = opts.Skills
	} else {
		skills, err = p.ListSkills()
		if err != nil {
			return nil, fmt.Errorf("sync: list skills: %w", err)
		}
	}
	results := make([]SyncResult, 0, len(harnessIDs))
	// Mode is a single global default applied to every harness in this
	// project. If a symlink falls back to a copy (FS does not support
	// symlinks), we persist the flip into the project config so subsequent
	// runs are consistent.
	desiredMode := cfg.Mode
	if desiredMode == "" {
		desiredMode = adept.ModeSymlink
	}
	resolvedMode := desiredMode
	for _, hid := range harnessIDs {
		adapter, err := o.reg.Get(hid)
		if err != nil {
			return results, fmt.Errorf("sync %q: %w", hid, err)
		}
		res, flippedMode, err := o.syncHarness(ctx, p, adapter, skills, opts, resolvedMode)
		if err != nil {
			return results, fmt.Errorf("sync %q: %w", hid, err)
		}
		results = append(results, res)
		if flippedMode == adept.ModeCopy && resolvedMode != adept.ModeCopy {
			resolvedMode = adept.ModeCopy
		}
	}
	if !opts.DryRun && resolvedMode != desiredMode {
		cfg.Mode = resolvedMode
		if err := p.SaveConfig(cfg); err != nil {
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
		// FRICTION BUG 7: track which skills the aggregator dropped due to
		// budget pressure by diffing the input vs surfaced-output skill ids.
		res.DroppedSkillIDs = aggregatorDrops(parts, aggregated)
		outputs = aggregated
	}
	// Materialize on disk.
	resolvedMode := mode
	for _, out := range outputs {
		absPath := filepath.Join(p.Root(), out.Path)
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
	sort.Strings(res.DroppedSkillIDs)
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
	staging := stagingPathFor(projectRoot, out)
	if err := o.writer.AtomicWrite(staging, out.Bytes, fileMode); err != nil {
		return false, false, fmt.Errorf("stage %q: %w", staging, err)
	}
	if err := o.writer.RemoveAll(absPath); err != nil {
		return false, false, fmt.Errorf("clear target %q: %w", absPath, err)
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

func (o *orchestrator) Status(ctx context.Context, p project.Project, opts StatusOptions) ([]adept.DriftReport, error) {
	_ = ctx
	cfg, err := p.Config()
	if err != nil {
		return nil, fmt.Errorf("status: load config: %w", err)
	}
	ids := opts.HarnessIDs
	if len(ids) == 0 {
		ids = append([]string{}, cfg.Harnesses...)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var skills []*adept.Skill
	if opts.Skills != nil {
		skills = opts.Skills
	} else {
		skills, err = p.ListSkills()
		if err != nil {
			return nil, fmt.Errorf("status: list skills: %w", err)
		}
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
		report.Harness = hid
		reports = append(reports, report)
	}
	return reports, nil
}

// ImportStrategy controls how conflicts are resolved when the same skill ID
// appears in multiple harnesses with differing content.
type ImportStrategy string

const (
	ImportStrategyFirst  ImportStrategy = "first"
	ImportStrategyError  ImportStrategy = "error"
	ImportStrategyPrefer ImportStrategy = "prefer"
)

// ImportOptions controls a single Import invocation.
type ImportOptions struct {
	HarnessIDs    []string
	Strategy      ImportStrategy
	PreferHarness string
	DryRun        bool
	Force         bool
}

// ImportReport summarizes one import run.
type ImportReport struct {
	Imported  []ImportedRow `json:"imported"`
	Conflicts []ConflictRow `json:"conflicts,omitempty"`
	Skipped   []SkipRow     `json:"skipped,omitempty"`
}

// ImportedRow is one canonical skill written into the project.
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

	ids := make([]string, 0, len(contributions))
	for id := range contributions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		entries := contributions[id]
		chosen, conflict := resolveImport(entries, opts)
		hadHarnessConflict := conflict != nil
		if hadHarnessConflict {
			report.Conflicts = append(report.Conflicts, *conflict)
			if opts.Strategy == ImportStrategyError {
				return report, fmt.Errorf("import: %w: skill %q reported by %v", adept.ErrSkillInvalid, id, conflict.From)
			}
		}
		if chosen == nil {
			continue
		}
		// Detect collision with existing project canonical content (hash-based).
		// When this fires we did not write anything: clarify the prior
		// "resolved" column (or emit a fresh one) so the user sees the
		// project canonical was kept and which harness would have applied.
		// chosen is non-nil here (guarded above), but build the message
		// without dereferencing fields more than once for clarity.
		if !opts.Force && p.HasSkill(id) {
			chosenHarness := chosen.harness
			blocked := fmt.Sprintf("kept project canonical (would have applied %s; pass --force to overwrite)", chosenHarness)
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
			if err := p.InstallSkill(chosen.imported.Skill, chosen.imported.Files); err != nil {
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

func stagingPathFor(projectRoot string, out adept.RenderOutput) string {
	return filepath.Join(projectRoot, adept.BaseDirName, "staging", out.Path)
}

// aggregatorDrops returns the SkillIDs that appear in inputs but not in
// outputs. The packer-based aggregators (codex, copilot) drop entire skills
// when the budget overflows, so a pre/post diff on SkillID is sufficient.
// Per-glob aggregators may legitimately split one input into several outputs
// — the diff still reports zero drops because every input id is preserved.
func aggregatorDrops(inputs, outputs []adept.RenderOutput) []string {
	if len(inputs) == 0 {
		return nil
	}
	keep := map[string]bool{}
	for _, o := range outputs {
		if o.SkillID != "" {
			keep[o.SkillID] = true
			continue
		}
		// Aggregators usually emit single combined outputs without a
		// SkillID; surfacing the manifest list is the adapter's job.
		// Without per-output SkillIDs we cannot diff reliably — bail out.
		return nil
	}
	seenInput := map[string]bool{}
	dropped := []string{}
	for _, in := range inputs {
		if in.SkillID == "" || seenInput[in.SkillID] {
			continue
		}
		seenInput[in.SkillID] = true
		if !keep[in.SkillID] {
			dropped = append(dropped, in.SkillID)
		}
	}
	sort.Strings(dropped)
	return dropped
}
