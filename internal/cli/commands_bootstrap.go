package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/harness"
	"github.com/itaywol/adeptability/pkg/adept"
)

// bootstrap is the first-time adoption flow:
//  1. ensure project canonical layout exists (.adeptability/skills/, lockfile);
//  2. walk every registered harness and reverse-render its on-disk state;
//  3. write the recovered skills into project canonical;
//  4. report imports + conflicts.
//
// After bootstrap the user typically runs `adept push <id>` to publish each
// imported skill to the central library, and `adept harness sync` to verify
// every harness now derives from the same canonical source.
func newBootstrapCmd(d *Deps) *cobra.Command {
	var strategyStr, prefer string
	var dryRun, force bool
	c := &cobra.Command{
		Use:   "bootstrap",
		Short: "Adopt an existing project's harness skills into adeptability",
		Long: "Walks .claude/, .cursor/, .opencode/, AGENTS.md, and\n" +
			".github/instructions/ and reverse-renders every skill it finds back\n" +
			"into the project's canonical .adeptability/skills/. Safe to run\n" +
			"repeatedly; existing canonical skills are reported as conflicts\n" +
			"unless --force is passed.",
	}
	c.Flags().StringVar(&strategyStr, "strategy", "first", "conflict strategy: first|error|prefer")
	c.Flags().StringVar(&prefer, "prefer", "", "harness id to keep when --strategy=prefer")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be imported without writing")
	c.Flags().BoolVar(&force, "force", false, "overwrite existing project canonical skills")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		projRoot, err := d.ResolveProjectRoot()
		if err != nil {
			return err
		}
		// Ensure the project skeleton exists.
		if err := d.Writer.EnsureDir(filepath.Join(projRoot, adept.BaseDirName, adept.SkillsDirName)); err != nil {
			return fmt.Errorf("bootstrap: create project skeleton: %w", err)
		}
		lockPath := filepath.Join(projRoot, adept.LockFileName)
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			if err := d.Store.Write(lockPath, d.Store.Empty()); err != nil {
				return fmt.Errorf("bootstrap: write lockfile: %w", err)
			}
		}

		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}

		p, err := d.Project()
		if err != nil {
			return err
		}
		ctx := withTimeout(cmd.Context())
		report, err := d.Orchestrator.Import(ctx, p, harness.ImportOptions{
			Strategy:      harness.ImportStrategy(strategyStr),
			PreferHarness: prefer,
			DryRun:        dryRun,
			Force:         force,
		})
		if err != nil {
			return err
		}

		// Enable every harness that contributed a skill so a subsequent
		// `adept harness sync` lights up the same harnesses.
		if !dryRun {
			contributed := map[string]bool{}
			for _, row := range report.Imported {
				contributed[row.Harness] = true
			}
			if len(contributed) > 0 {
				lf, err := p.Lock()
				if err != nil {
					return err
				}
				known := map[string]bool{}
				for _, h := range lf.Harnesses {
					known[h] = true
				}
				for h := range contributed {
					if !known[h] {
						lf.Harnesses = append(lf.Harnesses, h)
					}
				}
				if err := p.SaveLock(lf); err != nil {
					return err
				}
			}
		}

		if printErr := d.Print(cmd.OutOrStdout(), &bootstrapRenderable{Report: report}); printErr != nil {
			return printErr
		}
		if len(report.Conflicts) > 0 && !force {
			return ErrDirty
		}
		return nil
	}
	return c
}

type bootstrapRenderable struct{ Report harness.ImportReport }

func (r *bootstrapRenderable) JSON() any { return r.Report }
func (r *bootstrapRenderable) Plain(w io.Writer) error {
	fmt.Fprintf(w, "Imported %d skill(s) across %d harness(es).\n", len(r.Report.Imported), countHarnesses(r.Report))
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "  SKILL\tFROM HARNESS\tSOURCE")
	for _, row := range r.Report.Imported {
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", row.SkillID, row.Harness, row.SourcePath)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(r.Report.Conflicts) > 0 {
		fmt.Fprintln(w, "\nConflicts (re-run with --strategy=prefer --prefer=<harness> or --force):")
		for _, c := range r.Report.Conflicts {
			fmt.Fprintf(w, "  %s — from %v — resolved %s\n", c.SkillID, c.From, c.Resolved)
		}
	}
	if len(r.Report.Skipped) > 0 {
		fmt.Fprintln(w, "\nSkipped harnesses:")
		for _, s := range r.Report.Skipped {
			fmt.Fprintf(w, "  %s — %s\n", s.Harness, s.Reason)
		}
	}
	fmt.Fprintln(w, "\nNext: `adept push <id>` to publish imports to the library; `adept harness sync` to round-trip.")
	return nil
}

func countHarnesses(r harness.ImportReport) int {
	set := map[string]struct{}{}
	for _, row := range r.Imported {
		set[row.Harness] = struct{}{}
	}
	return len(set)
}
