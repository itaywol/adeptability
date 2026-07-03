// Package agentlint statically checks canonical agents against authoring
// best practices distilled from the harness docs (Claude Code, OpenCode,
// Cursor, Copilot, Codex) and the generator/evaluator literature. Findings
// reuse internal/scan's Finding/Severity types so `adept agent check` can
// merge lint output with the safety scanner's report and share formatters
// and exit-code gating.
//
// Severity mapping: lint error → SeverityHigh (gates exit 2), lint warning →
// SeverityMedium, lint info → SeverityLow.
package agentlint

import (
	"sort"

	"github.com/itaywol/adeptability/internal/scan"
	"github.com/itaywol/adeptability/pkg/adept"
)

// Lint-specific categories, extending the scan taxonomy.
const (
	// CategoryBestPractice marks authoring-quality findings.
	CategoryBestPractice scan.Category = "best-practice"
	// CategoryReference marks file references in the body that do not
	// resolve on disk.
	CategoryReference scan.Category = "file-reference"
)

// Input is everything one lint pass may consult.
type Input struct {
	// Agent is the parsed canonical agent (required).
	Agent *adept.Agent
	// ProjectRoot anchors file-reference existence checks. Empty disables
	// them.
	ProjectRoot string
	// AllAgents is the full project agent set for cross-agent rules
	// (overlapping descriptions). May be nil.
	AllAgents []*adept.Agent
}

// Linter runs the rule set over one agent.
type Linter struct {
	rules []rule
}

// NewLinter returns a Linter wired with the default rule set.
func NewLinter() *Linter { return &Linter{rules: defaultRules()} }

// Lint returns every finding for the input, ordered by severity (desc) then
// rule ID — the same ordering the safety scanner uses.
func (l *Linter) Lint(in Input) []scan.Finding {
	if in.Agent == nil {
		return nil
	}
	var findings []scan.Finding
	for _, r := range l.rules {
		findings = append(findings, r(in)...)
	}
	sort.Slice(findings, func(i, j int) bool {
		oi := scan.SeverityRank(findings[i].Severity)
		oj := scan.SeverityRank(findings[j].Severity)
		if oi != oj {
			return oi > oj
		}
		return findings[i].ID < findings[j].ID
	})
	return findings
}

// rule is one lint check. Rules return zero or more findings.
type rule func(in Input) []scan.Finding

// location returns the canonical file label for an agent finding.
func location(a *adept.Agent) string { return a.ID + ".md" }

// finding builds a Finding with the shared boilerplate filled in.
func finding(a *adept.Agent, id string, cat scan.Category, sev scan.Severity, conf scan.Confidence, issue, evidence, remediation string) scan.Finding {
	return scan.Finding{
		ID:          id,
		Category:    cat,
		Severity:    sev,
		Confidence:  conf,
		Location:    location(a),
		Issue:       issue,
		Evidence:    evidence,
		Remediation: remediation,
	}
}
