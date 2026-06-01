package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/harness"
	"github.com/itaywol/adeptability/pkg/adept"
)

// ---------- harness ----------

func newHarnessCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "harness",
		Short: "Per-harness commands",
	}
	c.AddCommand(
		newHarnessListCmd(d),
		newHarnessStatusCmd(d),
		newHarnessSyncCmd(d),
		newHarnessImportCmd(d),
		newHarnessEnableCmd(d),
		newHarnessDisableCmd(d),
		newHarnessAddCmd(d),
	)
	return c
}

func newHarnessListCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "list", Short: "List registered harnesses"}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		// Pull in user-defined adapters from <library>/adapters/ so list reflects
		// every harness that would participate in a sync.
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		return d.Print(cmd.OutOrStdout(), &harnessListRenderable{Adapters: d.Registry.List()})
	}
	return c
}

type harnessListRenderable struct{ Adapters []adept.HarnessAdapter }

func (r *harnessListRenderable) JSON() any {
	out := []map[string]any{}
	for _, a := range r.Adapters {
		s := a.Spec()
		out = append(out, map[string]any{
			"id":         s.ID,
			"name":       s.Name,
			"kind":       s.Kind,
			"outputPath": s.OutputPath,
			"budgetB":    s.SizeBudgetB,
		})
	}
	return out
}

func (r *harnessListRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "ID\tNAME\tKIND\tOUTPUT")
	for _, a := range r.Adapters {
		s := a.Spec()
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.ID, s.Name, s.Kind, s.OutputPath)
	}
	return tw.Flush()
}

func newHarnessStatusCmd(d *Deps) *cobra.Command {
	var ids []string
	c := &cobra.Command{Use: "status", Short: "Per-harness drift report"}
	c.Flags().StringSliceVar(&ids, "id", nil, "limit to specific harness ids (default: all enabled)")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		ctx := withTimeout(cmd.Context())
		reports, err := d.Orchestrator.Status(ctx, p, ids)
		if err != nil {
			return err
		}
		any := false
		for _, r := range reports {
			if len(r.Drifted) > 0 || len(r.Missing) > 0 || len(r.Conflict) > 0 {
				any = true
				break
			}
		}
		if err := d.Print(cmd.OutOrStdout(), &harnessStatusRenderable{Reports: reports}); err != nil {
			return err
		}
		if any {
			return ErrDirty
		}
		return nil
	}
	return c
}

type harnessStatusRenderable struct{ Reports []adept.DriftReport }

func (r *harnessStatusRenderable) JSON() any { return r.Reports }
func (r *harnessStatusRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "SYNCED\tDRIFTED\tMISSING\tCONFLICT")
	for _, rep := range r.Reports {
		fmt.Fprintf(tw, "%d\t%d\t%d\t%d\n", len(rep.Synced), len(rep.Drifted), len(rep.Missing), len(rep.Conflict))
	}
	return tw.Flush()
}

func newHarnessSyncCmd(d *Deps) *cobra.Command {
	var ids []string
	var force, dryRun bool
	c := &cobra.Command{Use: "sync", Short: "Sync project skills into enabled harnesses"}
	c.Flags().StringSliceVar(&ids, "id", nil, "harness ids (default: all enabled in lockfile)")
	c.Flags().BoolVar(&force, "force", false, "overwrite drifted harness files")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be written, write nothing")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		ctx := withTimeout(cmd.Context())
		results, err := d.Orchestrator.Sync(ctx, p, harness.SyncOptions{
			HarnessIDs: ids,
			Force:      force,
			DryRun:     dryRun,
		})
		if err != nil {
			return err
		}
		return d.Print(cmd.OutOrStdout(), &harnessSyncRenderable{Results: results})
	}
	return c
}

type harnessSyncRenderable struct{ Results []harness.SyncResult }

func (r *harnessSyncRenderable) JSON() any { return r.Results }
func (r *harnessSyncRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "HARNESS\tWRITTEN\tSKIPPED\tDROPPED")
	for _, res := range r.Results {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\n", res.Harness, len(res.Written), len(res.Skipped), len(res.Dropped))
	}
	return tw.Flush()
}

func newHarnessImportCmd(d *Deps) *cobra.Command {
	var id string
	c := &cobra.Command{Use: "import", Short: "Adopt harness-side edits into the project canonical copy"}
	c.Flags().StringVar(&id, "id", "", "harness id (required)")
	_ = c.MarkFlagRequired("id")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		ctx := withTimeout(cmd.Context())
		return d.Orchestrator.Import(ctx, p, id)
	}
	return c
}

func newHarnessEnableCmd(d *Deps) *cobra.Command {
	var id, modeStr string
	c := &cobra.Command{Use: "enable", Short: "Enable a harness for this project"}
	c.Flags().StringVar(&id, "id", "", "harness id (required)")
	c.Flags().StringVar(&modeStr, "mode", string(adept.ModeSymlink), "symlink or copy")
	_ = c.MarkFlagRequired("id")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		lf, err := p.Lock()
		if err != nil {
			return err
		}
		// Dedupe-insert.
		set := map[string]bool{}
		for _, h := range lf.Harnesses {
			set[h] = true
		}
		if !set[id] {
			lf.Harnesses = append(lf.Harnesses, id)
		}
		lf = d.Store.SetHarnessMode(lf, id, adept.HarnessMode(modeStr))
		if err := p.SaveLock(lf); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "enabled %s (mode=%s)\n", id, modeStr)
		return nil
	}
	return c
}

func newHarnessDisableCmd(d *Deps) *cobra.Command {
	var id string
	c := &cobra.Command{Use: "disable", Short: "Disable a harness for this project"}
	c.Flags().StringVar(&id, "id", "", "harness id (required)")
	_ = c.MarkFlagRequired("id")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		lf, err := p.Lock()
		if err != nil {
			return err
		}
		filtered := make([]string, 0, len(lf.Harnesses))
		for _, h := range lf.Harnesses {
			if h != id {
				filtered = append(filtered, h)
			}
		}
		lf.Harnesses = filtered
		if err := p.SaveLock(lf); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "disabled %s\n", id)
		return nil
	}
	return c
}

func newHarnessAddCmd(d *Deps) *cobra.Command {
	var fromFile string
	c := &cobra.Command{Use: "add", Short: "Register a config-driven harness adapter"}
	c.Flags().StringVar(&fromFile, "from", "", "adapter YAML file (required)")
	_ = c.MarkFlagRequired("from")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		a, err := d.AdapterLoader.LoadFile(fromFile)
		if err != nil {
			return err
		}
		// Install to <library>/adapters/<id>.yaml for persistence.
		libRoot, err := d.ResolveLibraryRoot()
		if err != nil {
			return err
		}
		dst := filepath.Join(libRoot, adept.AdaptersDir, a.Spec().ID+".yaml")
		if err := d.Writer.EnsureDir(filepath.Dir(dst)); err != nil {
			return err
		}
		// Copy file bytes to the persistent location.
		if err := copyFileVia(d, fromFile, dst); err != nil {
			return fmt.Errorf("persist adapter: %w", err)
		}
		if err := d.Registry.Register(a); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "registered harness %s (persisted at %s)\n", a.Spec().ID, dst)
		return nil
	}
	return c
}

func copyFileVia(d *Deps, src, dst string) error {
	b, err := readFile(src)
	if err != nil {
		return err
	}
	return d.Writer.AtomicWrite(dst, b, 0o644)
}

// ---------- render (debug) ----------

func newRenderCmd(d *Deps) *cobra.Command {
	var skillID, harnessID, outFile string
	c := &cobra.Command{Use: "render", Short: "Render a skill for one harness and print to stdout"}
	c.Flags().StringVar(&skillID, "id", "", "skill id (required)")
	c.Flags().StringVar(&harnessID, "harness", "", "harness id (required)")
	c.Flags().StringVar(&outFile, "out", "-", "output file or - for stdout")
	_ = c.MarkFlagRequired("id")
	_ = c.MarkFlagRequired("harness")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		s, err := p.GetSkill(skillID)
		if err != nil {
			return err
		}
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		a, err := d.Registry.Get(harnessID)
		if err != nil {
			return err
		}
		out, err := a.Renderer().Render(cmd.Context(), adept.RenderInput{
			Skill:   s,
			Harness: a.Spec(),
			Project: adept.ProjectInfo{Name: filepath.Base(p.Root()), Root: p.Root()},
		})
		if err != nil {
			return err
		}
		if outFile == "-" {
			_, err := cmd.OutOrStdout().Write(out.Bytes)
			return err
		}
		return d.Writer.AtomicWrite(outFile, out.Bytes, 0o644)
	}
	return c
}

// ---------- helpers ----------

func withTimeout(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
