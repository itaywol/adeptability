package cli

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

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
	c := &cobra.Command{
		Use:   "init",
		Short: "Wire project to an org skill registry (git or HTTPS URL)",
	}
	c.Flags().StringVar(&remote, "remote", "", "git remote (git@host:org/repo.git) or HTTPS URL pointing at the org library (required)")
	c.Flags().StringVar(&ref, "ref", "main", "branch or tag in the org library (ignored for HTTP)")
	_ = c.MarkFlagRequired("remote")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		if remote == "" {
			return fmt.Errorf("--remote is required")
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		cfg.Org = &adept.OrgRef{Remote: remote, Ref: ref}
		if err := p.SaveConfig(cfg); err != nil {
			return err
		}
		scheme := orgRemoteScheme(remote)
		fmt.Fprintf(cmd.OutOrStdout(), "wired project to %s (scheme=%s, ref=%s)\n", remote, scheme, ref)
		return nil
	}
	return c
}

// orgRemoteScheme classifies a remote URL string into the high-level scheme
// adept understands. The classification is what newOrgSyncCmd uses to pick
// between FileClient and HTTPClient.
func orgRemoteScheme(remote string) string {
	switch {
	case strings.HasPrefix(remote, "http://"), strings.HasPrefix(remote, "https://"):
		return "http"
	default:
		return "file"
	}
}

func newOrgSyncCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "sync", Short: "Sync required + optional org skills into the project"}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		if cfg.Org == nil {
			return fmt.Errorf("project has no org configured; run `adept org init`")
		}
		libRoot, err := d.ResolveLibraryRoot()
		if err != nil {
			return err
		}
		client, err := chooseOrgClient(d, cfg.Org.Remote, libRoot)
		if err != nil {
			return fmt.Errorf("select org client: %w", err)
		}
		manifest, err := client.Fetch(cmd.Context())
		if err != nil {
			return fmt.Errorf("fetch org manifest: %w", err)
		}
		l, err := d.Library()
		if err != nil {
			return err
		}
		installed := []string{}
		for _, ref := range manifest.Required {
			s, err := l.GetSkill(ref.ID)
			if err != nil {
				return fmt.Errorf("required skill %s: %w", ref.ID, err)
			}
			if err := p.InstallSkill(s, s.Files); err != nil {
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

// chooseOrgClient resolves the manifest client based on the remote URL scheme.
func chooseOrgClient(d *Deps, remote, libRoot string) (org.Client, error) {
	switch orgRemoteScheme(remote) {
	case "http":
		cache := org.NewFileETagCache(filepath.Join(libRoot, ".org-cache"))
		return org.NewHTTPClient(strings.TrimRight(remote, "/"), d.OrgParser, http.DefaultClient, cache), nil
	default:
		manifestPath := filepath.Join(libRoot, adept.OrgFileName)
		return org.NewFileClient(manifestPath, d.OrgParser), nil
	}
}
