package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/pkg/adept"
)

// newDiffCmd registers `adept diff`. Reports per-harness drift between the
// project canonical and each harness's rendered output on disk. Pure read;
// never writes. Exit code 2 (ErrDirty) when any drift is present so
// scripts can branch on it.
func newDiffCmd(d *Deps) *cobra.Command {
	var harnessIDs []string
	c := &cobra.Command{
		Use:   "diff",
		Short: "Show drift between canonical skills and harness outputs",
		Args:  cobra.NoArgs,
	}
	c.Flags().StringSliceVar(&harnessIDs, "harness", nil, "limit to specific harness ids (default: all enabled)")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		reports, err := d.Orchestrator.Status(cmd.Context(), p, harnessIDs)
		if err != nil {
			return err
		}
		anyDrift := false
		for _, r := range reports {
			if len(r.Drifted) > 0 || len(r.Missing) > 0 || len(r.Conflict) > 0 {
				anyDrift = true
				break
			}
		}
		if err := d.Print(cmd.OutOrStdout(), &diffRenderable{Reports: reports}); err != nil {
			return err
		}
		if anyDrift {
			return ErrDirty
		}
		return nil
	}
	return c
}

type diffRenderable struct{ Reports []adept.DriftReport }

func (r *diffRenderable) JSON() any { return r.Reports }
func (r *diffRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "HARNESS\tSYNCED\tDRIFTED\tMISSING\tCONFLICT")
	for _, rep := range r.Reports {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\n",
			rep.Harness, len(rep.Synced), len(rep.Drifted), len(rep.Missing), len(rep.Conflict))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, rep := range r.Reports {
		if len(rep.Drifted) == 0 && len(rep.Missing) == 0 && len(rep.Conflict) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n[%s]\n", rep.Harness)
		for _, path := range rep.Drifted {
			fmt.Fprintf(w, "  drift    %s\n", path)
		}
		for _, path := range rep.Missing {
			fmt.Fprintf(w, "  missing  %s\n", path)
		}
		for _, path := range rep.Conflict {
			fmt.Fprintf(w, "  conflict %s\n", path)
		}
	}
	return nil
}
