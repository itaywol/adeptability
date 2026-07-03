package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/agentlint"
	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/scan"
)

// newAgentCheckCmd is `adept agent check <id>`: one report combining three
// passes over a project canonical agent —
//
//  1. the static safety scanner (malicious-instruction patterns, same rules
//     as `skill check`),
//  2. the optional LLM intent pass (default-on when a provider is
//     configured, mirroring `skill check`),
//  3. the agent best-practice linter (schema, description quality, tool
//     hygiene, broken file references).
//
// All findings flow through one scan.Report so the formatters and the
// exit-code-2 gate (ErrScanFindings) are shared with skill check.
func newAgentCheckCmd(d *Deps) *cobra.Command {
	var format string
	var useLLM, noLLM bool
	c := &cobra.Command{
		Use:               "check <id>",
		Short:             "Scan a project agent for safety issues and best-practice violations",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: projectAgentCompletion(d),
	}
	c.Flags().StringVar(&format, "format", "table", "output format: table|markdown|json")
	c.Flags().BoolVar(&useLLM, "llm", false, "force the LLM intent pass (errors out when no provider configured)")
	c.Flags().BoolVar(&noLLM, "no-llm", false, "skip the LLM intent pass even when a provider is configured")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		id := args[0]
		if err := validateAgentID(id); err != nil {
			return err
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(p.AgentPath(id))
		if err != nil {
			return fmt.Errorf("agent %q not present in project (run `adept agent add %s` or `adept sync-from`): %w", id, id, err)
		}

		// Safety pass — reuses the skill scanner verbatim; agents have no
		// sidecars so only the body is scanned.
		target := scan.Target{Name: "agent:" + id, Body: raw, BodyLabel: id + ".md"}
		report := scan.NewScanner().Scan(target)
		if !noLLM {
			prov := d.LLMProvider()
			if useLLM && prov == nil {
				return fmt.Errorf("--llm requested but no provider configured (run `adept config llm set <provider>`)")
			}
			if prov != nil {
				if availErr := prov.Available(ctx); availErr == nil {
					reviewer := &scan.LLMReviewer{Provider: prov}
					if merged, err := reviewer.Review(ctx, target, report); err == nil {
						report = merged
					} else {
						d.Log.Warn("llm review failed; static-only report", "err", err)
					}
				} else {
					d.Log.Warn("llm provider unavailable; static-only report", "err", availErr)
				}
			}
		}

		// Lint pass. Schema violations surface as one high finding so the
		// report stays uniform instead of aborting the run.
		agent, _, parseErr := canonical.ParseAgentFrontmatter(raw)
		if parseErr != nil {
			report.Findings = append(report.Findings, scan.Finding{
				ID: "AGENT-SCHEMA-001", Category: scan.CategoryFrontmatter,
				Severity: scan.SeverityHigh, Confidence: scan.ConfidenceHigh,
				Location: id + ".md", Issue: "agent file does not parse",
				Evidence:    parseErr.Error(),
				Remediation: "Fix the YAML frontmatter fences/fields so the agent can render at all.",
			})
		} else {
			agent.ID = id
			if d.AgentValidator != nil {
				if err := d.AgentValidator.ValidateAgent(agent); err != nil {
					report.Findings = append(report.Findings, scan.Finding{
						ID: "AGENT-SCHEMA-001", Category: scan.CategoryFrontmatter,
						Severity: scan.SeverityHigh, Confidence: scan.ConfidenceHigh,
						Location: id + ".md", Issue: "agent frontmatter fails schema validation",
						Evidence:    err.Error(),
						Remediation: "Align the frontmatter with pkg/adeptschema/agent.schema.json (use harness: overrides for harness-specific knobs).",
					})
				}
			}
			// Unknown keys are invisible to the schema (the parser drops them
			// before validation), so detect them on the raw frontmatter — a
			// misspelled `tool:` silently means "inherit every tool".
			if unknown, err := canonical.AgentUnknownFrontmatterKeys(raw); err == nil {
				for _, k := range unknown {
					report.Findings = append(report.Findings, scan.Finding{
						ID: "AGENT-SCHEMA-002", Category: scan.CategoryFrontmatter,
						Severity: scan.SeverityMedium, Confidence: scan.ConfidenceHigh,
						Location: id + ".md", Issue: fmt.Sprintf("unknown frontmatter key %q is silently ignored", k),
						Evidence:    k,
						Remediation: "Fix the spelling or move harness-specific knobs under a harness: override block.",
					})
				}
			}
			all, listErr := p.ListAgents()
			if listErr != nil {
				d.Log.Warn("agent check: list agents for overlap rule", "err", listErr)
			}
			report.Findings = append(report.Findings, agentlint.NewLinter().Lint(agentlint.Input{
				Agent:       agent,
				ProjectRoot: p.Root(),
				AllAgents:   all,
			})...)
		}

		// Re-sort merged findings the same way the scanner orders its own.
		sort.Slice(report.Findings, func(i, j int) bool {
			oi := scan.SeverityRank(report.Findings[i].Severity)
			oj := scan.SeverityRank(report.Findings[j].Severity)
			if oi != oj {
				return oi > oj
			}
			return report.Findings[i].ID < report.Findings[j].ID
		})

		w := cmd.OutOrStdout()
		switch format {
		case "table":
			fmt.Fprint(w, scan.FormatTable(report))
		case "markdown":
			fmt.Fprint(w, scan.FormatMarkdown(report))
		case "json":
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			if err := enc.Encode(report); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown --format %q (want table|markdown|json)", format)
		}
		// Same CI gate as skill check: high/critical findings → exit 2.
		switch report.Worst() {
		case scan.SeverityCritical:
			return fmt.Errorf("scan: critical-severity findings present: %w", ErrScanFindings)
		case scan.SeverityHigh:
			return fmt.Errorf("scan: high-severity findings present: %w", ErrScanFindings)
		}
		return nil
	}
	return c
}
