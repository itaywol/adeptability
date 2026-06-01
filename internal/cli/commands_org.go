package cli

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/org"
	"github.com/itaywol/adeptability/pkg/adept"
)

func newOrgCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "org", Short: "Org-wide skill registry commands"}
	c.AddCommand(newOrgInitCmd(d), newOrgSyncCmd(d))
	return c
}

func newOrgInitCmd(d *Deps) *cobra.Command {
	var remote, ref string
	c := &cobra.Command{Use: "init", Short: "Wire project to an org skill registry"}
	c.Flags().StringVar(&remote, "remote", "", "git remote URL pointing at the org library (required)")
	c.Flags().StringVar(&ref, "ref", "main", "branch or tag in the org library")
	_ = c.MarkFlagRequired("remote")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		lf, err := p.Lock()
		if err != nil {
			return err
		}
		lf.Org = &adept.OrgRef{Remote: remote, Ref: ref}
		if err := p.SaveLock(lf); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "wired project to %s (ref %s)\n", remote, ref)
		return nil
	}
	return c
}

func newOrgSyncCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "sync", Short: "Sync required + optional org skills into the project"}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		lf, err := p.Lock()
		if err != nil {
			return err
		}
		if lf.Org == nil {
			return fmt.Errorf("project has no org configured; run `adept org init`")
		}
		// v0.1: read org.yaml from the library root. v0.2 will fetch from remote git.
		libRoot, err := d.ResolveLibraryRoot()
		if err != nil {
			return err
		}
		manifestPath := filepath.Join(libRoot, adept.OrgFileName)
		client := org.NewFileClient(manifestPath, d.OrgParser)
		manifest, err := client.Fetch(cmd.Context())
		if err != nil {
			return fmt.Errorf("fetch org manifest: %w", err)
		}
		// Install required + optional skills already enrolled.
		l, err := d.Library()
		if err != nil {
			return err
		}
		libLock, err := d.Store.Read(l.LockfilePath())
		if err != nil {
			return err
		}
		installed := []string{}
		for _, ref := range manifest.Required {
			s, err := l.GetSkill(ref.ID)
			if err != nil {
				return fmt.Errorf("required skill %s: %w", ref.ID, err)
			}
			entry, ok := libLock.Skills[ref.ID]
			if !ok {
				return fmt.Errorf("library missing lock entry for required %s", ref.ID)
			}
			if ref.MinVersion > 0 && entry.Version < ref.MinVersion {
				return fmt.Errorf("required skill %s v%d < min %d", ref.ID, entry.Version, ref.MinVersion)
			}
			if err := p.InstallSkill(s, s.Files, entry); err != nil {
				return err
			}
			installed = append(installed, ref.ID)
		}
		return d.Print(cmd.OutOrStdout(), &orgSyncRenderable{Required: installed})
	}
	return c
}

type orgSyncRenderable struct {
	Required []string
	Skipped  []string
}

func (r *orgSyncRenderable) JSON() any {
	return map[string]any{"required": r.Required, "skipped": r.Skipped}
}
func (r *orgSyncRenderable) Plain(w io.Writer) error {
	fmt.Fprintf(w, "installed %d required skill(s)\n", len(r.Required))
	for _, id := range r.Required {
		fmt.Fprintf(w, "  + %s\n", id)
	}
	return nil
}
