package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/harness"
	"github.com/itaywol/adeptability/internal/project"
)

// newSyncFromCmd registers `adept sync-from`. Adopts harness-side edits
// back into the project canonical. Interactive by default: walks the drift
// report and prompts for each drifted skill. Non-interactive when --harness
// or --all is passed.
func newSyncFromCmd(d *Deps) *cobra.Command {
	var harnessIDs []string
	var all, dryRun, force bool
	c := &cobra.Command{
		Use:   "sync-from",
		Short: "Adopt harness-side edits into the canonical project skills",
		Args:  cobra.NoArgs,
		Long: "Reverse direction of `sync`. Walks each enabled harness's on-disk state and " +
			"writes whatever it finds back to .adeptability/skills/. With no flags, prompts " +
			"interactively per drifted skill; --harness <id> takes that harness non-interactively; " +
			"--all takes every harness (strategy=first).",
	}
	c.Flags().StringSliceVar(&harnessIDs, "harness", nil, "limit to specific harness ids")
	c.Flags().BoolVar(&all, "all", false, "non-interactive: adopt from every harness (strategy=first)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be imported, write nothing")
	c.Flags().BoolVar(&force, "force", false, "overwrite existing project canonical content")
	_ = c.RegisterFlagCompletionFunc("harness", enabledHarnessCompletion(d))
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}

		// Non-interactive paths first.
		if all || len(harnessIDs) > 0 {
			importOpts := harness.ImportOptions{
				HarnessIDs: harnessIDs,
				Strategy:   harness.ImportStrategyFirst,
				DryRun:     dryRun,
				Force:      force,
			}
			report, err := d.Orchestrator.Import(cmd.Context(), p, importOpts)
			if err != nil {
				return err
			}
			agentReport, err := d.Orchestrator.ImportAgents(cmd.Context(), p, importOpts)
			if err != nil {
				return err
			}
			if err := d.Print(cmd.OutOrStdout(), &syncFromRenderable{Report: report, Agents: agentReport}); err != nil {
				return err
			}
			if (len(report.Conflicts) > 0 || len(agentReport.Conflicts) > 0) && !force {
				return ErrDirty
			}
			return nil
		}

		// Interactive: list drift, prompt per harness.
		return runInteractiveSyncFrom(cmd.Context(), d, p, cmd.OutOrStdout(), cmd.InOrStdin(), dryRun, force)
	}
	return c
}

type syncFromRenderable struct {
	Report harness.ImportReport
	Agents harness.AgentImportReport
}

// JSON keeps the historical top-level ImportReport shape and nests the agent
// report under "agents".
func (r *syncFromRenderable) JSON() any {
	return struct {
		harness.ImportReport
		Agents harness.AgentImportReport `json:"agents"`
	}{ImportReport: r.Report, Agents: r.Agents}
}

func (r *syncFromRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "SKILL\tHARNESS\tSOURCE")
	for _, row := range r.Report.Imported {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", row.SkillID, row.Harness, row.SourcePath)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(r.Agents.Imported) > 0 {
		fmt.Fprintln(w)
		tw = NewTabWriter(w)
		fmt.Fprintln(tw, "AGENT\tHARNESS\tSOURCE")
		for _, row := range r.Agents.Imported {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", row.AgentID, row.Harness, row.SourcePath)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		// Importer recovery notes (e.g. harness-only fields that were
		// dropped) — the transform is lossy and the user must see where.
		for _, row := range r.Agents.Imported {
			for _, warning := range row.Warnings {
				fmt.Fprintf(w, "  warn: %s\n", warning)
			}
		}
	}
	conflicts := append(append([]harness.ConflictRow{}, r.Report.Conflicts...), r.Agents.Conflicts...)
	if len(conflicts) > 0 {
		fmt.Fprintln(w, "\nCONFLICTS:")
		for _, c := range conflicts {
			fmt.Fprintf(w, "  %s  from=%v  resolved=%s\n", c.SkillID, c.From, c.Resolved)
		}
	}
	skipped := append(append([]harness.SkipRow{}, r.Report.Skipped...), r.Agents.Skipped...)
	if len(skipped) > 0 {
		fmt.Fprintln(w, "\nSKIPPED:")
		for _, s := range skipped {
			fmt.Fprintf(w, "  %s — %s\n", s.Harness, s.Reason)
		}
	}
	return nil
}

// runInteractiveSyncFrom asks the orchestrator for the current drift, then
// prompts per drifted harness whether to adopt. Selected harnesses are
// passed back through Import with strategy=first.
func runInteractiveSyncFrom(ctx context.Context, d *Deps, p project.Project, w io.Writer, in io.Reader, dryRun, force bool) error {
	skills, err := resolveSkills(d, p)
	if err != nil {
		return err
	}
	reports, err := d.Orchestrator.Status(ctx, p, harness.StatusOptions{Skills: skills})
	if err != nil {
		return err
	}
	drifted := []string{}
	for _, rep := range reports {
		if len(rep.Drifted) > 0 || len(rep.Conflict) > 0 {
			drifted = append(drifted, rep.Harness)
		}
	}
	sort.Strings(drifted)
	if len(drifted) == 0 {
		fmt.Fprintln(w, "sync-from: no drift detected, nothing to adopt")
		return nil
	}

	fmt.Fprintf(w, "drifted harnesses: %s\n", strings.Join(drifted, ", "))
	reader := bufio.NewReader(in)
	chosen := []string{}
	for _, hid := range drifted {
		fmt.Fprintf(w, "adopt from %s? [y/N] ", hid)
		line, _ := reader.ReadString('\n')
		ans := strings.TrimSpace(strings.ToLower(line))
		if ans == "y" || ans == "yes" {
			chosen = append(chosen, hid)
		}
	}
	if len(chosen) == 0 {
		fmt.Fprintln(w, "sync-from: nothing selected")
		return nil
	}

	importOpts := harness.ImportOptions{
		HarnessIDs: chosen,
		Strategy:   harness.ImportStrategyFirst,
		DryRun:     dryRun,
		Force:      force,
	}
	report, err := d.Orchestrator.Import(ctx, p, importOpts)
	if err != nil {
		return err
	}
	agentReport, err := d.Orchestrator.ImportAgents(ctx, p, importOpts)
	if err != nil {
		return err
	}
	if err := d.Print(w, &syncFromRenderable{Report: report, Agents: agentReport}); err != nil {
		return err
	}
	if (len(report.Conflicts) > 0 || len(agentReport.Conflicts) > 0) && !force {
		return ErrDirty
	}
	return nil
}

// Sanity: ensure os.Stdin is wired through cmd.InOrStdin so tests can
// inject input. The constant is referenced solely to keep the os import
// from being optimized away when tests stub stdin.
var _ = os.Stdin
