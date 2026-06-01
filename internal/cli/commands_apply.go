package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/itaywol/adeptability/internal/harness"
	"github.com/itaywol/adeptability/internal/project"
)

// apply-all fans out a sync across many project checkouts matching a glob.
//
// Push v2 of a skill into the library once, then
// `adept apply-all --skills my-skill --to '~/code/*'` to touch every consumer.
func newApplyAllCmd(d *Deps) *cobra.Command {
	var skills []string
	var toGlob string
	var dryRun bool
	c := &cobra.Command{
		Use:   "apply-all",
		Short: "Fanout sync across many project checkouts",
	}
	c.Flags().StringSliceVar(&skills, "skills", nil, "skill ids to install/refresh in each project")
	c.Flags().StringVar(&toGlob, "to", "", "project directory glob (e.g. '~/code/*')")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "list matched projects and exit without writing")
	_ = c.MarkFlagRequired("to")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		expanded := expandHome(toGlob)
		matches, err := filepath.Glob(expanded)
		if err != nil {
			return fmt.Errorf("glob: %w", err)
		}
		projects := make([]string, 0, len(matches))
		for _, m := range matches {
			fi, err := os.Stat(m)
			if err != nil || !fi.IsDir() {
				continue
			}
			projects = append(projects, m)
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "matched %d project(s)\n", len(projects))
		if dryRun {
			for _, p := range projects {
				fmt.Fprintf(out, "  - %s\n", p)
			}
			return nil
		}
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		var g errgroup.Group
		g.SetLimit(maxConcurrentProjects())
		summaries := make([]projectApplyResult, len(projects))
		for i, projRoot := range projects {
			i, projRoot := i, projRoot
			g.Go(func() error {
				summary, err := applyToProject(d, projRoot, skills)
				summary.Project = projRoot
				if err != nil {
					summary.Error = err.Error()
				}
				summaries[i] = summary
				return nil
			})
		}
		_ = g.Wait()
		return d.Print(out, &applyAllRenderable{Results: summaries})
	}
	return c
}

type projectApplyResult struct {
	Project   string               `json:"project"`
	Installed []string             `json:"installed,omitempty"`
	Synced    []harness.SyncResult `json:"synced,omitempty"`
	Error     string               `json:"error,omitempty"`
}

type applyAllRenderable struct{ Results []projectApplyResult }

func (r *applyAllRenderable) JSON() any { return r.Results }
func (r *applyAllRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "PROJECT\tINSTALLED\tHARNESSES\tERROR")
	for _, res := range r.Results {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%s\n",
			res.Project, len(res.Installed), len(res.Synced), res.Error)
	}
	return tw.Flush()
}

func applyToProject(d *Deps, projRoot string, skillIDs []string) (projectApplyResult, error) {
	res := projectApplyResult{}
	p := project.New(projRoot, d.Parser, d.Hasher, d.Config, d.Writer)
	l, err := d.Library()
	if err != nil {
		return res, err
	}
	for _, id := range skillIDs {
		s, err := l.GetSkill(id)
		if err != nil {
			return res, fmt.Errorf("skill %s: %w", id, err)
		}
		if err := p.InstallSkill(s, s.Files); err != nil {
			return res, fmt.Errorf("install %s into %s: %w", id, projRoot, err)
		}
		res.Installed = append(res.Installed, id)
	}
	syncResults, err := d.Orchestrator.Sync(Context(), p, harness.SyncOptions{})
	if err != nil {
		return res, fmt.Errorf("sync harnesses: %w", err)
	}
	res.Synced = syncResults
	return res, nil
}

func expandHome(p string) string {
	if len(p) > 0 && p[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// maxConcurrentProjects caps how many project rollouts run in parallel.
func maxConcurrentProjects() int { return 8 }
