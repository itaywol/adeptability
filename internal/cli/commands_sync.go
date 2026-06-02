package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/harness"
)

// newSyncCmd registers `adept sync`. Renders every canonical skill out to
// every enabled harness on disk. This is the primary "publish" verb in the
// new UX: edits in `.adeptability/skills/` propagate to `.claude/`,
// `.cursor/`, `AGENTS.md`, etc. on `sync`.
func newSyncCmd(d *Deps) *cobra.Command {
	var harnessIDs []string
	var force, dryRun bool
	c := &cobra.Command{
		Use:   "sync",
		Short: "Push canonical skills to every enabled harness",
		Args:  cobra.NoArgs,
	}
	c.Flags().StringSliceVar(&harnessIDs, "harness", nil, "limit to specific harness ids (default: all enabled)")
	c.Flags().BoolVar(&force, "force", false, "overwrite drifted harness files")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "print what would change, write nothing")
	_ = c.RegisterFlagCompletionFunc("harness", enabledHarnessCompletion(d))
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		verifyExternalLocks(d, p, cmd.ErrOrStderr())
		skills, err := resolveSkills(d, p)
		if err != nil {
			return err
		}
		results, err := d.Orchestrator.Sync(cmd.Context(), p, harness.SyncOptions{
			HarnessIDs: harnessIDs,
			Force:      force,
			DryRun:     dryRun,
			Skills:     skills,
		})
		if err != nil {
			return err
		}
		return d.Print(cmd.OutOrStdout(), &syncRenderable{Results: results})
	}
	return c
}

type syncRenderable struct{ Results []harness.SyncResult }

func (r *syncRenderable) JSON() any { return r.Results }
func (r *syncRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "HARNESS\tWRITTEN\tSKIPPED\tDROPPED")
	for _, res := range r.Results {
		dropped := len(res.DroppedSkillIDs)
		if dropped == 0 {
			dropped = len(res.Dropped)
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\n", res.Harness, len(res.Written), len(res.Skipped), dropped)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, res := range r.Results {
		if len(res.DroppedSkillIDs) > 0 {
			fmt.Fprintf(w, "  %s dropped: %v\n", res.Harness, res.DroppedSkillIDs)
		}
	}
	return nil
}
