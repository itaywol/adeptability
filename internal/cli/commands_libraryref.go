package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/pkg/adept"
)

// newLibraryCmd registers `adept library {add,remove,list}`. Libraries are
// remote skill sources cloned into $ADEPT_LIBRARY/libs/<name>/. Multiple
// libraries can be stacked; first-match-wins resolution drives shadowing.
//
// Private remotes lean on git's own credential chain (ssh-agent, .netrc,
// credential.helper). adept never sees secrets; if `git clone` prompts,
// it prompts the user directly.
func newLibraryCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "library", Short: "Manage the project's library remotes"}
	c.AddCommand(newLibraryAddCmd(d), newLibraryRemoveCmd(d), newLibraryListCmd(d))
	return c
}

func newLibraryAddCmd(d *Deps) *cobra.Command {
	var fromURL, ref string
	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Clone a remote library into the project",
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().StringVar(&fromURL, "from", "", "remote URL (git remote or local path) — required")
	c.Flags().StringVar(&ref, "ref", "main", "branch or tag to track")
	_ = c.MarkFlagRequired("from")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if !libraryNamePattern.MatchString(name) {
			return fmt.Errorf("library name %q does not match %s", name, libraryNamePattern.String())
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		libsRoot, err := d.ResolveLibrariesRoot()
		if err != nil {
			return err
		}
		if err := d.Writer.EnsureDir(libsRoot); err != nil {
			return fmt.Errorf("create libraries root: %w", err)
		}
		dest := filepath.Join(libsRoot, name)
		if err := d.Git.CloneOrPull(cmd.Context(), fromURL, ref, dest); err != nil {
			return fmt.Errorf("clone %s into %s: %w", fromURL, dest, err)
		}
		cfg.Libraries = upsertLibraryRef(cfg.Libraries, adept.LibraryRef{
			Name:   name,
			Remote: fromURL,
			Ref:    ref,
		})
		if err := p.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "library %q added (clone: %s)\n", name, dest)
		return nil
	}
	return c
}

func newLibraryRemoveCmd(d *Deps) *cobra.Command {
	var deleteClone bool
	c := &cobra.Command{
		Use:               "remove <name>",
		Short:             "Drop a library from the project (does not delete the local clone unless --purge)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: configuredLibraryCompletion(d),
	}
	c.Flags().BoolVar(&deleteClone, "purge", false, "also delete the local clone directory")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		name := args[0]
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		filtered := make([]adept.LibraryRef, 0, len(cfg.Libraries))
		removed := false
		for _, l := range cfg.Libraries {
			if l.Name == name {
				removed = true
				continue
			}
			filtered = append(filtered, l)
		}
		if !removed {
			return fmt.Errorf("library %q not configured", name)
		}
		cfg.Libraries = filtered
		if err := p.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "library %q removed from config\n", name)
		if deleteClone {
			libsRoot, err := d.ResolveLibrariesRoot()
			if err != nil {
				return err
			}
			dest := filepath.Join(libsRoot, name)
			if err := os.RemoveAll(dest); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("delete clone %s: %w", dest, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "library %q clone deleted (%s)\n", name, dest)
		}
		return nil
	}
	return c
}

func newLibraryListCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "list",
		Short: "List configured libraries",
		Args:  cobra.NoArgs,
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		libsRoot, err := d.ResolveLibrariesRoot()
		if err != nil {
			return err
		}
		rows := make([]libraryRow, 0, len(cfg.Libraries))
		for _, l := range cfg.Libraries {
			local := filepath.Join(libsRoot, l.Name)
			onDisk := false
			if _, statErr := os.Stat(local); statErr == nil {
				onDisk = true
			}
			rows = append(rows, libraryRow{
				Name:      l.Name,
				Remote:    l.Remote,
				Ref:       l.Ref,
				LocalPath: local,
				OnDisk:    onDisk,
			})
		}
		return d.Print(cmd.OutOrStdout(), &libraryListRenderable{Rows: rows})
	}
	return c
}

type libraryRow struct {
	Name      string `json:"name"`
	Remote    string `json:"remote"`
	Ref       string `json:"ref,omitempty"`
	LocalPath string `json:"localPath"`
	OnDisk    bool   `json:"onDisk"`
}

type libraryListRenderable struct{ Rows []libraryRow }

func (r *libraryListRenderable) JSON() any { return r.Rows }
func (r *libraryListRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "NAME\tREF\tON-DISK\tREMOTE")
	for _, row := range r.Rows {
		present := "no"
		if row.OnDisk {
			present = "yes"
		}
		ref := row.Ref
		if ref == "" {
			ref = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.Name, ref, present, row.Remote)
	}
	return tw.Flush()
}

// configuredLibraryCompletion lists library names from the project config.
// Used for `library remove` so users can tab through what they configured.
func configuredLibraryCompletion(d *Deps) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		p, err := d.Project()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		cfg, err := p.Config()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]cobra.Completion, 0, len(cfg.Libraries))
		for _, l := range cfg.Libraries {
			out = append(out, l.Name)
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}
