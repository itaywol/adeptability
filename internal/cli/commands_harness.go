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
	fmt.Fprintln(tw, "HARNESS\tSYNCED\tDRIFTED\tMISSING\tCONFLICT")
	for _, rep := range r.Reports {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\n", rep.Harness, len(rep.Synced), len(rep.Drifted), len(rep.Missing), len(rep.Conflict))
	}
	return tw.Flush()
}

func newHarnessSyncCmd(d *Deps) *cobra.Command {
	var ids []string
	var force, dryRun bool
	c := &cobra.Command{Use: "sync", Short: "Sync project skills into enabled harnesses"}
	c.Flags().StringSliceVar(&ids, "id", nil, "harness ids (default: all enabled)")
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
		// FRICTION BUG 7: DROPPED reflects actual aggregator drops, not
		// pre-aggregation render misses. Surface both counts so users see
		// the full picture when budgets clip skills.
		dropped := len(res.DroppedSkillIDs)
		if dropped == 0 {
			dropped = len(res.Dropped)
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\n", res.Harness, len(res.Written), len(res.Skipped), dropped)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, res := range r.Results {
		if len(res.DroppedSkillIDs) > 0 {
			fmt.Fprintf(w, "  %s dropped: %v\n", res.Harness, res.DroppedSkillIDs)
		}
	}
	return nil
}

func newHarnessImportCmd(d *Deps) *cobra.Command {
	var ids []string
	var strategyStr, prefer string
	var dryRun, force bool
	c := &cobra.Command{
		Use:   "import",
		Short: "Adopt harness-side edits into the project canonical copy",
		Long: "Reverse-renders one or more harnesses' on-disk state back into\n" +
			"the project's canonical .adeptability/skills/. Use --id to limit;\n" +
			"omit --id to walk every registered harness.",
	}
	c.Flags().StringSliceVar(&ids, "id", nil, "harness ids (default: all registered)")
	c.Flags().StringVar(&strategyStr, "strategy", "first", "conflict strategy: first|error|prefer")
	c.Flags().StringVar(&prefer, "prefer", "", "harness id to keep when --strategy=prefer")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "report without writing")
	c.Flags().BoolVar(&force, "force", false, "overwrite existing project canonical skills")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		ctx := withTimeout(cmd.Context())
		report, err := d.Orchestrator.Import(ctx, p, harness.ImportOptions{
			HarnessIDs:    ids,
			Strategy:      harness.ImportStrategy(strategyStr),
			PreferHarness: prefer,
			DryRun:        dryRun,
			Force:         force,
		})
		if err != nil {
			return err
		}
		if printErr := d.Print(cmd.OutOrStdout(), &importRenderable{Report: report}); printErr != nil {
			return printErr
		}
		if len(report.Conflicts) > 0 && !force {
			return ErrDirty
		}
		return nil
	}
	return c
}

type importRenderable struct{ Report harness.ImportReport }

func (r *importRenderable) JSON() any { return r.Report }
func (r *importRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "SKILL\tHARNESS\tSOURCE")
	for _, row := range r.Report.Imported {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", row.SkillID, row.Harness, row.SourcePath)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(r.Report.Conflicts) > 0 {
		fmt.Fprintln(w, "\nCONFLICTS:")
		for _, c := range r.Report.Conflicts {
			fmt.Fprintf(w, "  %s  from=%v  resolved=%s\n", c.SkillID, c.From, c.Resolved)
		}
	}
	if len(r.Report.Skipped) > 0 {
		fmt.Fprintln(w, "\nSKIPPED:")
		for _, s := range r.Report.Skipped {
			fmt.Fprintf(w, "  %s — %s\n", s.Harness, s.Reason)
		}
	}
	return nil
}

func newHarnessEnableCmd(d *Deps) *cobra.Command {
	var id, modeStr string
	c := &cobra.Command{Use: "enable", Short: "Enable a harness for this project"}
	c.Flags().StringVar(&id, "id", "", "harness id (required)")
	c.Flags().StringVar(&modeStr, "mode", string(adept.ModeSymlink), "symlink or copy")
	_ = c.MarkFlagRequired("id")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		// FRICTION BUG 4: validate the harness exists in the registry before
		// touching the config. Otherwise a typo silently writes a bogus id
		// that breaks the next `sync`.
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		if _, err := d.Registry.Get(id); err != nil {
			return fmt.Errorf("harness enable %q: %w", id, adept.ErrHarnessUnknown)
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		set := map[string]bool{}
		for _, h := range cfg.Harnesses {
			set[h] = true
		}
		if !set[id] {
			cfg.Harnesses = append(cfg.Harnesses, id)
		}
		d.Config.SetHarnessMode(cfg, id, adept.HarnessMode(modeStr))
		if err := p.SaveConfig(cfg); err != nil {
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
		// disable is intentionally idempotent — disabling something that
		// isn't enabled is a no-op success.
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		filtered := make([]string, 0, len(cfg.Harnesses))
		for _, h := range cfg.Harnesses {
			if h != id {
				filtered = append(filtered, h)
			}
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
		libRoot, err := d.ResolveLibraryRoot()
		if err != nil {
			return err
		}
		dst := filepath.Join(libRoot, adept.AdaptersDir, a.Spec().ID+".yaml")
		if err := d.Writer.EnsureDir(filepath.Dir(dst)); err != nil {
			return err
		}
		if err := copyFileVia(d, fromFile, dst); err != nil {
			return fmt.Errorf("persist adapter: %w", err)
		}
		if err := d.Registry.Register(a); err != nil {
			return err
		}
		// Record the adapter id in the project config so it round-trips
		// across machines via the same config.json everyone uses.
		p, perr := d.Project()
		if perr == nil {
			cfg, cerr := p.Config()
			if cerr == nil {
				known := map[string]bool{}
				for _, x := range cfg.Adapters {
					known[x] = true
				}
				if !known[a.Spec().ID] {
					cfg.Adapters = append(cfg.Adapters, a.Spec().ID)
					if err := p.SaveConfig(cfg); err != nil {
						d.Log.Warn("save adapter id to project config", "err", err)
					}
				}
			}
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
		// FRICTION BUG 3: validate --harness FIRST so the error mentions
		// the harness, not the (still-missing) skill. Typos on the harness
		// flag used to surface as "skill not found" which sent users
		// chasing the wrong wire.
		if err := d.LoadUserAdapters(); err != nil {
			d.Log.Warn("load user adapters", "err", err)
		}
		a, err := d.Registry.Get(harnessID)
		if err != nil {
			return fmt.Errorf("render: harness %q: %w", harnessID, adept.ErrHarnessUnknown)
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		s, err := p.GetSkill(skillID)
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
		if len(out.Bytes) == 0 {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"render: harness %q produced no output for skill %q (skill may not match this harness's targeting, e.g. missing globs for an aggregator-per-glob harness)\n",
				harnessID, skillID)
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
