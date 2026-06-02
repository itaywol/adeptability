package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/pkg/adept"
)

// newHarnessCmd registers the `adept harness {add,remove,list}` subtree.
// add/remove flip the project config's Harnesses slice (and warn on
// unknown ids); list shows every adapter registered with the harness
// registry alongside its enabled/disabled state.
func newHarnessCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "harness", Short: "Manage which harnesses are enabled in this project"}
	c.AddCommand(newHarnessAddCmd(d), newHarnessRemoveCmd(d), newHarnessListCmd(d))
	return c
}

func newHarnessAddCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "add <id>",
		Short: "Enable a harness for this project",
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: harnessCompletion(d, func(enabled map[string]bool, id string) bool {
			return !enabled[id]
		}),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id := args[0]
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		if _, err := d.Registry.Get(id); err != nil {
			return fmt.Errorf("harness %q: %w", id, adept.ErrHarnessUnknown)
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		if containsString(cfg.Harnesses, id) {
			fmt.Fprintf(cmd.OutOrStdout(), "harness %s: already enabled\n", id)
			return nil
		}
		cfg.Harnesses = append(cfg.Harnesses, id)
		sort.Strings(cfg.Harnesses)
		if err := p.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "enabled %s\n", id)
		return nil
	}
	return c
}

func newHarnessRemoveCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "remove <id>",
		Short: "Disable a harness for this project",
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: harnessCompletion(d, func(enabled map[string]bool, id string) bool {
			return enabled[id]
		}),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id := args[0]
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		filtered := make([]string, 0, len(cfg.Harnesses))
		removed := false
		for _, h := range cfg.Harnesses {
			if h == id {
				removed = true
				continue
			}
			filtered = append(filtered, h)
		}
		if !removed {
			fmt.Fprintf(cmd.OutOrStdout(), "harness %s: not enabled, nothing to do\n", id)
			return nil
		}
		cfg.Harnesses = filtered
		if err := p.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "disabled %s\n", id)
		return nil
	}
	return c
}

func newHarnessListCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "list",
		Short: "List harnesses (registered + enabled state)",
		Args:  cobra.NoArgs,
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		enabled := map[string]bool{}
		if p, perr := d.Project(); perr == nil {
			if cfg, cerr := p.Config(); cerr == nil {
				for _, h := range cfg.Harnesses {
					enabled[h] = true
				}
			}
		}
		rows := make([]harnessRow, 0, len(d.Registry.List()))
		for _, a := range d.Registry.List() {
			s := a.Spec()
			rows = append(rows, harnessRow{
				ID:      s.ID,
				Name:    s.Name,
				Kind:    string(s.Kind),
				Output:  s.OutputPath,
				Enabled: enabled[s.ID],
			})
		}
		return d.Print(cmd.OutOrStdout(), &harnessListRenderable{Rows: rows})
	}
	return c
}

type harnessRow struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Output  string `json:"output"`
	Enabled bool   `json:"enabled"`
}

type harnessListRenderable struct{ Rows []harnessRow }

func (r *harnessListRenderable) JSON() any { return r.Rows }
func (r *harnessListRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "ID\tNAME\tKIND\tENABLED\tOUTPUT")
	for _, row := range r.Rows {
		state := "no"
		if row.Enabled {
			state = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", row.ID, row.Name, row.Kind, state, row.Output)
	}
	return tw.Flush()
}

// enabledHarnessCompletion shows only harnesses currently enabled in the
// project config. Used for the --harness flag on sync/sync-from/diff so
// tab-completion does not suggest harnesses the project has not opted in
// to.
func enabledHarnessCompletion(d *Deps) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		p, err := d.Project()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		cfg, err := p.Config()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]cobra.Completion, 0, len(cfg.Harnesses))
		for _, h := range cfg.Harnesses {
			out = append(out, cobra.Completion(h))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// harnessCompletion returns a cobra ValidArgsFunction that lists known
// harness ids filtered by the provided predicate (e.g. "not yet enabled"
// for `harness add`, "currently enabled" for `harness remove`).
func harnessCompletion(d *Deps, allow func(enabled map[string]bool, id string) bool) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		_ = d.LoadUserAdapters()
		enabled := map[string]bool{}
		if p, perr := d.Project(); perr == nil {
			if cfg, cerr := p.Config(); cerr == nil {
				for _, h := range cfg.Harnesses {
					enabled[h] = true
				}
			}
		}
		out := []cobra.Completion{}
		for _, a := range d.Registry.List() {
			id := a.Spec().ID
			if allow(enabled, id) {
				out = append(out, cobra.Completion(id))
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}
