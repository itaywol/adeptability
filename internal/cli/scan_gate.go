package cli

import (
	"context"

	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/internal/scan"
	"github.com/itaywol/adeptability/pkg/adept"
)

// maybeRunLLMReview runs the LLM intent pass when the project config
// asks for it. Returns the original static report on any opt-out path
// or LLM failure — the safety scan never blocks because the LLM was
// flaky.
func maybeRunLLMReview(ctx context.Context, d *Deps, target scan.Target, static scan.Report) scan.Report {
	p, err := d.Project()
	if err != nil {
		return static
	}
	cfg, err := p.Config()
	if err != nil {
		return static
	}
	prov := d.LLMProvider()
	if !shouldScanOnInstall(cfg, prov) {
		return static
	}
	if err := prov.Available(ctx); err != nil {
		d.Log.Warn("llm provider unavailable, falling back to static scan", "provider", prov.Name(), "err", err)
		return static
	}
	reviewer := &scan.LLMReviewer{Provider: prov}
	merged, err := reviewer.Review(ctx, target, static)
	if err != nil {
		d.Log.Warn("llm review failed, falling back to static scan", "err", err)
		return static
	}
	return merged
}

// shouldScanOnInstall implements the documented default:
//   - Config.Scan.OnInstall explicit true  -> always run when a provider
//     is available (no provider -> log + skip).
//   - Config.Scan.OnInstall explicit false -> never run.
//   - Config.Scan.OnInstall nil (default)  -> run iff provider is set.
func shouldScanOnInstall(cfg *adept.Config, prov interface{ Name() string }) bool {
	if cfg == nil {
		return false
	}
	if cfg.Scan != nil && cfg.Scan.OnInstall != nil {
		if *cfg.Scan.OnInstall {
			return prov != nil
		}
		return false
	}
	return prov != nil
}

// installBlocks returns true when report.Worst() reaches or exceeds the
// configured block threshold (default "critical"). Used by `skill
// install` to gate writes.
//
// An invalid or mis-cased Scan.BlockSeverity must NOT silently disable
// or invert the gate: such values are rejected (logged) and the gate
// falls back to the strictest meaningful default, SeverityCritical, so
// a typo can never weaken the safety boundary.
func installBlocks(d *Deps, p project.Project, report scan.Report) bool {
	threshold := scan.SeverityCritical
	if cfg, err := p.Config(); err == nil && cfg.Scan != nil && cfg.Scan.BlockSeverity != "" {
		if sev, ok := scan.ParseSeverity(cfg.Scan.BlockSeverity); ok {
			threshold = sev
		} else if d != nil && d.Log != nil {
			d.Log.Warn("invalid scan.blockSeverity; defaulting to critical",
				"value", cfg.Scan.BlockSeverity)
		}
	}
	return severityRank(report.Worst()) >= severityRank(threshold)
}

// severityRank mirrors internal/scan but is exposed here so this
// package doesn't reach into scan's unexported helper. Unknown but
// non-empty severities rank as the most severe (4) so the gate fails
// closed rather than treating a stray value as "clean".
func severityRank(s scan.Severity) int {
	switch s {
	case scan.SeverityCritical:
		return 4
	case scan.SeverityHigh:
		return 3
	case scan.SeverityMedium:
		return 2
	case scan.SeverityLow:
		return 1
	case scan.SeverityClean, "":
		return 0
	}
	return 4
}
