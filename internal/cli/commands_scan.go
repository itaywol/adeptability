package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/library"
)

// scan walks the given path for SKILL.md files and reports their state
// relative to the library — without writing anything. Replaces the prior
// art's TUI: same information, JSON or table output, scriptable.
func newScanCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "scan [path]",
		Short: "Scan a directory tree for skills",
		Args:  cobra.MaximumNArgs(1),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		root, err := d.ResolveProjectRoot()
		if err != nil {
			return err
		}
		if len(args) == 1 {
			root = args[0]
		}
		l, err := d.Library()
		if err != nil {
			return err
		}
		s := library.NewScanner(l, d.Parser, d.Hasher)
		results, err := s.Scan([]string{root})
		if err != nil {
			return err
		}
		return d.Print(cmd.OutOrStdout(), &scanRenderable{Results: results})
	}
	return c
}

type scanRenderable struct{ Results []library.ScanResult }

func (r *scanRenderable) JSON() any { return r.Results }
func (r *scanRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "SKILL\tSOURCE\tSTATUS")
	for _, res := range r.Results {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", res.SkillID, res.SourcePath, res.Status)
	}
	return tw.Flush()
}
