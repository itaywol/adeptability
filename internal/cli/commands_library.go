package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/harness"
	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/pkg/adept"
)

// ---------- init ----------
//
// New shape: `adept init [--from <url>] [--mode symlink|copy]`. Always
// initializes a project (in --project / cwd). If the library root does not
// yet exist it is created. When --from is given the URL is cloned into the
// library root (treated as git unless it points at an HTTP manifest, in
// which case the manifest's referenced skills are installed from the
// already-existing library). Finally any pre-existing harness trees
// (`.claude/`, `.cursor/`, `.opencode/`, `AGENTS.md`,
// `.github/instructions/`) are auto-adopted into the canonical layout and
// the corresponding harness ids are recorded in the project config.

func newInitCmd(d *Deps) *cobra.Command {
	var fromURL, ref, modeStr, libName string
	c := &cobra.Command{
		Use:   "init",
		Short: "Initialize an adept project (and optionally clone a remote library)",
		Args:  cobra.NoArgs,
		Long: "Creates .adeptability/{skills,base}/ in the project, writes config.json, " +
			"optionally clones a remote library, and adopts any pre-existing harness skills " +
			"(.claude/, .cursor/, .opencode/, AGENTS.md, .github/instructions/) it finds on disk.",
	}
	c.Flags().StringVar(&fromURL, "from", "", "remote library URL (git remote or local path)")
	c.Flags().StringVar(&ref, "ref", "main", "branch or tag in the remote library")
	c.Flags().StringVar(&libName, "name", "default", "local name for the library added via --from")
	c.Flags().StringVar(&modeStr, "mode", string(adept.ModeSymlink), "harness materialization: symlink|copy")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		w := cmd.OutOrStdout()

		mode := adept.HarnessMode(modeStr)
		if mode != adept.ModeSymlink && mode != adept.ModeCopy {
			return fmt.Errorf("invalid --mode %q (want symlink|copy)", modeStr)
		}

		// 1) project skeleton
		p, err := d.Project()
		if err != nil {
			return err
		}
		if err := d.Writer.EnsureDir(p.SkillsDir()); err != nil {
			return fmt.Errorf("create project skills dir: %w", err)
		}
		if err := d.Writer.EnsureDir(p.BaseSnapshotsDir()); err != nil {
			return fmt.Errorf("create project base dir: %w", err)
		}

		// 2) load (or create) the project config and stamp mode
		cfg, err := readOrEmptyConfig(d, p.ConfigPath())
		if err != nil {
			return err
		}
		cfg.Mode = mode

		// 3) honor --from by cloning into $ADEPT_LIBRARY/libs/<name>/ and
		// appending an entry to cfg.Libraries. Multiple `init --from` calls
		// with distinct --name stack libraries (first-wins on collision).
		if fromURL != "" {
			libsRoot, err := d.ResolveLibrariesRoot()
			if err != nil {
				return err
			}
			if err := d.Writer.EnsureDir(libsRoot); err != nil {
				return fmt.Errorf("create libraries root: %w", err)
			}
			dest := filepath.Join(libsRoot, libName)
			if err := d.Git.CloneOrPull(ctx, fromURL, ref, dest); err != nil {
				return fmt.Errorf("clone library %s into %s: %w", fromURL, dest, err)
			}
			cfg.Libraries = upsertLibraryRef(cfg.Libraries, adept.LibraryRef{
				Name:   libName,
				Remote: fromURL,
				Ref:    ref,
			})
			fmt.Fprintf(w, "library %q cloned from %s into %s\n", libName, fromURL, dest)
		}

		// 4) auto-adopt pre-existing harness trees, if any
		adopted, err := autoAdopt(ctx, d, p)
		if err != nil {
			return fmt.Errorf("auto-adopt harness skills: %w", err)
		}
		for _, hid := range adopted {
			if !containsString(cfg.Harnesses, hid) {
				cfg.Harnesses = append(cfg.Harnesses, hid)
			}
		}
		sort.Strings(cfg.Harnesses)

		// 5) persist config
		if err := p.SaveConfig(cfg); err != nil {
			return fmt.Errorf("write project config: %w", err)
		}

		// 6) summary
		fmt.Fprintf(w, "project initialized at %s (mode=%s)\n", p.Root(), mode)
		if len(adopted) > 0 {
			fmt.Fprintf(w, "adopted harnesses: %s\n", strings.Join(adopted, ", "))
		}
		return nil
	}
	return c
}

// upsertLibraryRef appends or replaces a library entry by name. Used by
// init --from and `adept library add` to keep the slice unique.
func upsertLibraryRef(libs []adept.LibraryRef, in adept.LibraryRef) []adept.LibraryRef {
	for i, l := range libs {
		if l.Name == in.Name {
			libs[i] = in
			return libs
		}
	}
	return append(libs, in)
}

// readOrEmptyConfig returns the current config if one exists, otherwise an
// empty config with the current schema version. Init may be re-run on an
// existing project to add a remote or adopt newly-introduced harness files.
func readOrEmptyConfig(d *Deps, path string) (*adept.Config, error) {
	cfg, err := d.Config.Read(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return d.Config.Empty(), nil
		}
		return nil, err
	}
	return cfg, nil
}

// autoAdopt runs the harness orchestrator's Import with strategy=first and
// returns the set of harness ids that contributed skills. Conflicts with
// existing project canonical content are tolerated (the existing copy
// wins); init is meant to be re-runnable.
func autoAdopt(ctx context.Context, d *Deps, p project.Project) ([]string, error) {
	report, err := d.Orchestrator.Import(ctx, p, harness.ImportOptions{
		Strategy: harness.ImportStrategyFirst,
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, row := range report.Imported {
		seen[row.Harness] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for hid := range seen {
		out = append(out, hid)
	}
	sort.Strings(out)
	return out, nil
}

// truncate cuts a string to n runes, appending an ellipsis when truncated.
// It operates on runes (not bytes) so a multi-byte UTF-8 sequence is never
// split mid-rune into an invalid/garbled glyph.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
