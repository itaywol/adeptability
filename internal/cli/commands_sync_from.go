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
			report, err := d.Orchestrator.Import(cmd.Context(), p, harness.ImportOptions{
				HarnessIDs: harnessIDs,
				Strategy:   harness.ImportStrategyFirst,
				DryRun:     dryRun,
				Force:      force,
			})
			if err != nil {
				return err
			}
			if err := d.Print(cmd.OutOrStdout(), &syncFromRenderable{Report: report}); err != nil {
				return err
			}
			if len(report.Conflicts) > 0 && !force {
				return ErrDirty
			}
			return nil
		}

		// Interactive: list drift, prompt per harness.
		return runInteractiveSyncFrom(cmd.Context(), d, p, cmd.OutOrStdout(), cmd.InOrStdin(), dryRun, force)
	}
	return c
}

type syncFromRenderable struct{ Report harness.ImportReport }

func (r *syncFromRenderable) JSON() any { return r.Report }
func (r *syncFromRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "SKILL\tHARNESS\tSOURCE")
	for _, row := range r.Report.Imported {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", row.SkillID, row.Harness, row.SourcePath)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(r.Report.Conflicts) > 0 {
		fmt.Fprintln(w, "\nCONFLICTS:")
		for _, c := range r.Report.Conflicts {
			fmt.Fprintf(w, "  %s  from=%v  resolved=%s\n", c.SkillID, c.From, c.Resolved)
		}
	}
	if len(r.Report.Skipped) > 0 {
		fmt.Fprintln(w, "\nSKIPPED:")
		for _, s := range r.Report.Skipped {
			fmt.Fprintf(w, "  %s — %s\n", s.Harness, s.Reason)
		}
	}
	return nil
}

// runInteractiveSyncFrom asks the orchestrator for the current drift, then
// prompts per drifted harness whether to adopt. Selected harnesses are
// passed back through Import with strategy=first.
func runInteractiveSyncFrom(ctx context.Context, d *Deps, p project.Project, w io.Writer, in io.Reader, dryRun, force bool) error {
	reports, err := d.Orchestrator.Status(ctx, p, nil)
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

	report, err := d.Orchestrator.Import(ctx, p, harness.ImportOptions{
		HarnessIDs: chosen,
		Strategy:   harness.ImportStrategyFirst,
		DryRun:     dryRun,
		Force:      force,
	})
	if err != nil {
		return err
	}
	if err := d.Print(w, &syncFromRenderable{Report: report}); err != nil {
		return err
	}
	if len(report.Conflicts) > 0 && !force {
		return ErrDirty
	}
	return nil
}

// Sanity: ensure os.Stdin is wired through cmd.InOrStdin so tests can
// inject input. The constant is referenced solely to keep the os import
// from being optimized away when tests stub stdin.
var _ = os.Stdin
