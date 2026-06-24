package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	c.AddCommand(newLibraryAddCmd(d), newLibraryUpdateCmd(d), newLibraryRemoveCmd(d), newLibraryListCmd(d))
	return c
}

// newLibraryUpdateCmd registers `adept library update [name]`. It fetches each
// configured library, shows which skills changed, and applies the update only
// after confirmation (or immediately with --yes). With no name, every library
// is checked. The local clone must fast-forward; diverged clones surface the
// git error rather than being force-reset.
func newLibraryUpdateCmd(d *Deps) *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:               "update [name]",
		Short:             "Fetch newer skills from configured libraries (prompts before applying)",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: configuredLibraryCompletion(d),
	}
	c.Flags().BoolVar(&yes, "yes", false, "apply updates without prompting")
	c.RunE = func(cmd *cobra.Command, args []string) error {
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

		targets := cfg.Libraries
		if len(args) == 1 {
			targets = nil
			for _, l := range cfg.Libraries {
				if l.Name == args[0] {
					targets = append(targets, l)
				}
			}
			if len(targets) == 0 {
				return fmt.Errorf("library %q not configured", args[0])
			}
		}
		if len(targets) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "no libraries configured")
			return nil
		}

		jsonMode := d.Flags != nil && d.Flags.JSON
		w := cmd.OutOrStdout()
		ctx := cmd.Context()
		updated := false
		for _, l := range targets {
			dest := filepath.Join(libsRoot, l.Name)
			if !d.Git.IsRepo(dest) {
				fmt.Fprintf(w, "%s: no local clone — run `adept library add %s --from %s`\n", l.Name, l.Name, l.Remote)
				continue
			}
			ref := l.Ref
			if ref == "" {
				ref = "main"
			}
			if err := d.Git.Fetch(ctx, dest); err != nil {
				return fmt.Errorf("%s: %w", l.Name, err)
			}
			local, err := d.Git.RevParse(ctx, dest, "HEAD")
			if err != nil {
				return fmt.Errorf("%s: %w", l.Name, err)
			}
			remote, err := d.Git.RevParse(ctx, dest, "origin/"+ref)
			if err != nil {
				return fmt.Errorf("%s: %w", l.Name, err)
			}
			if local == remote {
				fmt.Fprintf(w, "%s: up to date (%s)\n", l.Name, shortSHA(local))
				continue
			}
			files, err := d.Git.ChangedFiles(ctx, dest, local, remote)
			if err != nil {
				return fmt.Errorf("%s: %w", l.Name, err)
			}
			fmt.Fprintf(w, "%s: %s -> %s\n", l.Name, shortSHA(local), shortSHA(remote))
			if skills := changedSkillIDs(files); len(skills) > 0 {
				fmt.Fprintf(w, "  changed skills: %s\n", strings.Join(skills, ", "))
			} else {
				fmt.Fprintf(w, "  %d file(s) changed (no skill directories)\n", len(files))
			}
			if !yes {
				if jsonMode {
					return fmt.Errorf("update in --json mode is non-interactive; pass --yes to apply")
				}
				if !confirm(cmd.InOrStdin(), w, fmt.Sprintf("update %q?", l.Name)) {
					fmt.Fprintf(w, "%s: skipped\n", l.Name)
					continue
				}
			}
			if err := d.Git.CloneOrPull(ctx, l.Remote, ref, dest); err != nil {
				return fmt.Errorf("%s: apply update: %w", l.Name, err)
			}
			fmt.Fprintf(w, "%s: updated to %s\n", l.Name, shortSHA(remote))
			updated = true
		}
		if updated {
			fmt.Fprintln(w, "run `adept sync` to re-render updated skills into your harnesses")
		}
		return nil
	}
	return c
}

// changedSkillIDs maps changed repo paths (skills/<id>/...) to a sorted, unique
// list of affected skill ids.
func changedSkillIDs(files []string) []string {
	seen := map[string]bool{}
	var ids []string
	for _, f := range files {
		parts := strings.Split(filepath.ToSlash(f), "/")
		if len(parts) >= 2 && parts[0] == adept.SkillsDirName && !seen[parts[1]] {
			seen[parts[1]] = true
			ids = append(ids, parts[1])
		}
	}
	sort.Strings(ids)
	return ids
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
