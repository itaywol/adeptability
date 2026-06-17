package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/harness"
	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/pkg/adept"
)

// hookMarker identifies a pre-commit hook adept wrote, so `install` can
// safely overwrite its own script but never clobber a hand-written one.
const hookMarker = "adept-managed pre-commit hook"

// newHookCmd registers `adept hook` — installing and running the git
// pre-commit drift gate. The installed hook simply execs `adept hook run`;
// all logic lives here so it stays testable and the shell stub stays trivial.
func newHookCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "hook",
		Short: "Manage the git pre-commit drift hook",
		Args:  cobra.NoArgs,
	}
	c.AddCommand(newHookInstallCmd(d), newHookRunCmd(d))
	return c
}

func newHookInstallCmd(d *Deps) *cobra.Command {
	var mode string
	c := &cobra.Command{
		Use:   "install",
		Short: "Install a git pre-commit hook that blocks commits on adept drift",
		Args:  cobra.NoArgs,
		Long: "Writes .git/hooks/pre-commit. In `fail` mode (default) a drifted commit is " +
			"blocked with a suggested fix; in `fix` mode the hook adopts harness edits back " +
			"into canonical (and re-renders), then re-stages so the commit proceeds.",
	}
	c.Flags().StringVar(&mode, "mode", "fail", "hook behavior on drift: fail|fix")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		root, err := d.ResolveProjectRoot()
		if err != nil {
			return err
		}
		path, err := installPreCommitHook(d, root, mode)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "installed pre-commit hook (mode=%s) at %s\n", mode, path)
		return nil
	}
	return c
}

func newHookRunCmd(d *Deps) *cobra.Command {
	var fix bool
	c := &cobra.Command{
		Use:    "run",
		Short:  "Drift gate invoked by the installed pre-commit hook",
		Args:   cobra.NoArgs,
		Hidden: true,
	}
	c.Flags().BoolVar(&fix, "fix", false, "auto-adopt/re-render drift and re-stage instead of failing")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		return runHook(cmd.Context(), d, cmd.OutOrStdout(), fix)
	}
	return c
}

// installPreCommitHook validates mode, resolves <root>/.git/hooks/pre-commit,
// and writes the managed stub. It refuses to overwrite a pre-commit hook that
// adept did not write. Returns the hook path on success.
func installPreCommitHook(d *Deps, root, mode string) (string, error) {
	if mode != "fail" && mode != "fix" {
		return "", fmt.Errorf("invalid hook mode %q (want fail|fix)", mode)
	}
	if !d.Git.IsRepo(root) {
		return "", fmt.Errorf("not a git repository: %s (run `git init` first)", root)
	}
	// ponytail: assumes a standard .git directory. Worktrees/submodules use a
	// .git file pointing elsewhere; parse gitdir only if anyone needs it.
	gitDir := filepath.Join(root, ".git")
	if st, err := os.Stat(gitDir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s is not a standard .git directory; install the hook manually", gitDir)
	}
	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return "", fmt.Errorf("create hooks dir: %w", err)
	}
	hookPath := filepath.Join(hooksDir, "pre-commit")
	if existing, err := os.ReadFile(hookPath); err == nil {
		if !strings.Contains(string(existing), hookMarker) {
			return "", fmt.Errorf("refusing to overwrite existing pre-commit hook at %s "+
				"(not adept-managed) — remove it or install the hook manually", hookPath)
		}
	}
	runLine := "exec adept hook run"
	if mode == "fix" {
		runLine += " --fix"
	}
	// ponytail: calls `adept` from $PATH; that's the install-time contract.
	script := "#!/bin/sh\n" +
		"# " + hookMarker + " — re-run `adept hook install` (or `adept init --git-hook`) to change.\n" +
		runLine + "\n"
	if err := os.WriteFile(hookPath, []byte(script), 0o755); err != nil {
		return "", fmt.Errorf("write pre-commit hook: %w", err)
	}
	return hookPath, nil
}

// runHook is the pre-commit body. It is a no-op (exit 0) outside an
// initialized project or when no harnesses are enabled. Otherwise it reports
// drift and either fails (suggesting a fix) or, with fix=true, reconciles and
// re-stages so the commit can proceed.
func runHook(ctx context.Context, d *Deps, w io.Writer, fix bool) error {
	root, err := d.ResolveProjectRoot()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(filepath.Join(root, adept.BaseDirName)); os.IsNotExist(statErr) {
		return nil // not an adept project — nothing to gate
	} else if statErr != nil {
		return fmt.Errorf("stat project: %w", statErr)
	}
	p, err := d.Project()
	if err != nil {
		return err
	}
	cfg, err := p.Config()
	if err != nil {
		return err
	}
	if len(cfg.Harnesses) == 0 {
		return nil
	}
	if err := d.LoadUserAdapters(); err != nil {
		d.Log.Warn("load user adapters", "err", err)
	}

	drifted, err := driftedHarnesses(ctx, d, p)
	if err != nil {
		return err
	}
	if len(drifted) == 0 {
		return nil
	}

	if !fix {
		fmt.Fprintf(w, "adept: drift detected in %d harness(es): %s\n", len(drifted), strings.Join(drifted, ", "))
		fmt.Fprintln(w, "fix harness-side edits with: adept sync-from --harness <id>")
		fmt.Fprintln(w, "fix canonical edits with:    adept sync")
		fmt.Fprintln(w, "or install the hook in fix mode: adept hook install --mode fix")
		return ErrDirty
	}

	// fix mode: infer direction from staged paths, reconcile, re-stage.
	staged, err := d.Git.StagedFiles(ctx, root)
	if err != nil {
		return err
	}
	reverse := harnessesForStagedPaths(d, cfg.Harnesses, staged)

	if len(reverse) > 0 {
		if _, err := d.Orchestrator.Import(ctx, p, harness.ImportOptions{
			HarnessIDs: reverse,
			Strategy:   harness.ImportStrategyFirst,
			Force:      true,
		}); err != nil {
			return fmt.Errorf("adopt harness edits: %w", err)
		}
	}
	// Always re-render forward afterwards so both sides are consistent — this
	// also covers pure-canonical edits where no reverse import ran.
	skills, err := resolveSkills(d, p)
	if err != nil {
		return err
	}
	if _, err := d.Orchestrator.Sync(ctx, p, harness.SyncOptions{Force: true, Skills: skills}); err != nil {
		return fmt.Errorf("re-render canonical: %w", err)
	}

	// Re-stage canonical + every enabled harness root so the reconciled state
	// lands in this commit.
	for _, rel := range restagePaths(d, root, cfg.Harnesses) {
		if err := d.Git.Add(ctx, root, rel); err != nil {
			return fmt.Errorf("re-stage %s: %w", rel, err)
		}
	}

	still, err := driftedHarnesses(ctx, d, p)
	if err != nil {
		return err
	}
	if len(still) > 0 {
		fmt.Fprintf(w, "adept: drift remains after fix in: %s\n", strings.Join(still, ", "))
		return ErrDirty
	}
	fmt.Fprintf(w, "adept: reconciled drift and re-staged (%s)\n", strings.Join(drifted, ", "))
	return nil
}

// driftedHarnesses returns the sorted ids of enabled harnesses whose on-disk
// state diverges from canonical (drift, missing, or conflict).
func driftedHarnesses(ctx context.Context, d *Deps, p project.Project) ([]string, error) {
	skills, err := resolveSkills(d, p)
	if err != nil {
		return nil, err
	}
	reports, err := d.Orchestrator.Status(ctx, p, harness.StatusOptions{Skills: skills})
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, r := range reports {
		if len(r.Drifted) > 0 || len(r.Missing) > 0 || len(r.Conflict) > 0 {
			out = append(out, r.Harness)
		}
	}
	sort.Strings(out)
	return out, nil
}

// harnessRoots returns the repo-relative path prefixes that belong to a
// harness: its detection BaseDir plus the static (pre-template) prefix of its
// OutputPath (which covers aggregators like AGENTS.md that have no BaseDir).
func harnessRoots(spec adept.HarnessSpec) []string {
	roots := []string{}
	if spec.BaseDir != "" {
		roots = append(roots, filepath.Clean(spec.BaseDir))
	}
	if pfx := staticPrefix(spec.OutputPath); pfx != "" {
		roots = append(roots, pfx)
	}
	return roots
}

// staticPrefix returns the leading literal segment of a path template, i.e.
// everything before the first "{placeholder}". ".claude/skills/{id}/SKILL.md"
// -> ".claude/skills"; "AGENTS.md" -> "AGENTS.md".
func staticPrefix(tmpl string) string {
	if tmpl == "" {
		return ""
	}
	if i := strings.IndexByte(tmpl, '{'); i >= 0 {
		tmpl = tmpl[:i]
	}
	tmpl = strings.TrimRight(tmpl, "/")
	if tmpl == "" {
		return ""
	}
	return filepath.Clean(tmpl)
}

// pathUnder reports whether rel is at or below root (both repo-relative,
// slash-separated). Matches "root", "root/...", but not "rootish".
func pathUnder(rel, root string) bool {
	rel = filepath.Clean(rel)
	return rel == root || strings.HasPrefix(rel, root+"/")
}

// harnessesForStagedPaths returns the enabled harness ids that own at least
// one staged path. Edits to .adeptability/staging/ (symlink-mode rendered
// output) can't be attributed to a single harness, so they map to every
// enabled harness.
//
// ponytail: staging fan-out is a deliberate over-adopt; a precise
// staging-subpath→harness map is the upgrade if it ever matters.
func harnessesForStagedPaths(d *Deps, enabled, staged []string) []string {
	stagingRoot := filepath.Join(adept.BaseDirName, adept.StagingDir)
	stagingTouched := false
	for _, s := range staged {
		if pathUnder(s, stagingRoot) {
			stagingTouched = true
			break
		}
	}

	hit := map[string]bool{}
	for _, id := range enabled {
		a, err := d.Registry.Get(id)
		if err != nil {
			continue
		}
		if stagingTouched {
			hit[id] = true
			continue
		}
		for _, root := range harnessRoots(a.Spec()) {
			for _, s := range staged {
				if pathUnder(s, root) {
					hit[id] = true
				}
			}
		}
	}
	out := make([]string, 0, len(hit))
	for id := range hit {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// restagePaths is the set of repo-relative paths to `git add` after a fix:
// the canonical dir plus every enabled harness root that exists on disk.
func restagePaths(d *Deps, root string, enabled []string) []string {
	seen := map[string]bool{adept.BaseDirName: true}
	for _, id := range enabled {
		a, err := d.Registry.Get(id)
		if err != nil {
			continue
		}
		for _, r := range harnessRoots(a.Spec()) {
			if _, err := os.Stat(filepath.Join(root, r)); err == nil {
				seen[r] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
