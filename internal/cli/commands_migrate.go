package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
)

// newMigrateCmd registers `adept migrate`: re-clone the project's declared
// libraries into the project-local libs root so resolution stops falling
// back to the machine store. The machine store is never touched — other
// projects and the global scope may still use it.
func newMigrateCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Localize configured libraries into this project's .adeptability/libs/",
		Args:  cobra.NoArgs,
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, isGlobal, err := d.ScopedProject()
		if err != nil {
			return err
		}
		if isGlobal {
			return fmt.Errorf("migrate applies to project scope only (global libraries already live in the machine store)")
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		if len(cfg.Libraries) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "no libraries configured")
			return nil
		}
		libsRoot := d.LibsRootFor(p)
		w := cmd.OutOrStdout()
		for _, ref := range cfg.Libraries {
			dest := filepath.Join(libsRoot, ref.Name)
			if dirExists(dest) {
				fmt.Fprintf(w, "%s: already local\n", ref.Name)
				continue
			}
			gitRef := ref.Ref
			if gitRef == "" {
				gitRef = "main"
			}
			if err := d.Git.CloneOrPull(cmd.Context(), ref.Remote, gitRef, dest); err != nil {
				return fmt.Errorf("%s: %w", ref.Name, err)
			}
			fmt.Fprintf(w, "%s: localized (%s)\n", ref.Name, dest)
		}
		if err := ensureScopeGitignore(d.Writer, p.BaseDir()); err != nil {
			d.Log.Warn("write .adeptability/.gitignore", "err", err)
		}
		return nil
	}
	return c
}
