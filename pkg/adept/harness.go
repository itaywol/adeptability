package adept

import "context"

// HarnessKind classifies how a harness consumes skills.
type HarnessKind string

const (
	// KindPerSkill writes one file per skill (Claude Code, Cursor, OpenCode).
	KindPerSkill HarnessKind = "per-skill"
	// KindAggregatorSingle merges all skills into one file (Codex AGENTS.md).
	KindAggregatorSingle HarnessKind = "aggregator-single"
	// KindAggregatorPerGlob buckets skills by their globs into one file per bucket (Copilot).
	KindAggregatorPerGlob HarnessKind = "aggregator-per-glob"
)

// HarnessSpec is the static description of a harness adapter.
type HarnessSpec struct {
	ID          string
	Name        string
	Kind        HarnessKind
	OutputPath  string // template; may include {id}
	SizeBudgetB int    // 0 = unlimited
	NeedsDir    bool
	BaseDir     string // detection root (e.g. ".claude" or ".cursor")
}

// DriftReport summarizes what a Detect/Validate pass found on disk.
type DriftReport struct {
	Synced   []string
	Drifted  []string
	Missing  []string
	Conflict []string
}

// ImportedSkill is one canonical skill recovered from a harness's on-disk
// representation. SourcePath is the harness file (or aggregator bucket) the
// content came from; the orchestrator records it for conflict reporting.
type ImportedSkill struct {
	Skill      *Skill
	Files      []SkillFile
	SourcePath string
}

// HarnessAdapter is the contract every built-in or config-driven harness implements.
type HarnessAdapter interface {
	Spec() HarnessSpec
	Renderer() Renderer
	// Aggregate is nil for KindPerSkill adapters.
	Aggregate(ctx context.Context, parts []RenderOutput, budgetB int) ([]RenderOutput, error)
	Detect(projectRoot string) (bool, error)
	Validate(projectRoot string, expected []RenderOutput) (DriftReport, error)
	// Import reverse-renders the harness's on-disk state into canonical
	// skills. Aggregator harnesses must parse their own section markers (or
	// fall back to a single synthesized skill when markers are absent).
	Import(ctx context.Context, projectRoot string) ([]ImportedSkill, error)
}
