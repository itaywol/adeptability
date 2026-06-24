package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/harness"
	"github.com/itaywol/adeptability/pkg/adept"
)

// newStatusCmd replaces the prior `doctor` command. Single "where am I"
// view: init state, configured libraries, enabled harnesses, plus a
// one-line drift summary. Exits 2 when anything's out of sync so scripts
// can branch on it.
func newStatusCmd(d *Deps) *cobra.Command {
	var fetch bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Show project state at a glance (init, libraries, harnesses, drift)",
		Args:  cobra.NoArgs,
	}
	c.Flags().BoolVar(&fetch, "fetch", false, "fetch library remotes to detect available updates (network)")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		rep, err := collectStatus(cmd.Context(), d, fetch)
		if err != nil {
			return err
		}
		if err := d.Print(cmd.OutOrStdout(), &statusRenderable{Report: rep}); err != nil {
			return err
		}
		if !rep.Initialized || rep.MissingLibraries > 0 || rep.DriftedHarnesses > 0 {
			return ErrDirty
		}
		return nil
	}
	return c
}

type statusLibraryRow struct {
	Name             string `json:"name"`
	Remote           string `json:"remote"`
	Ref              string `json:"ref,omitempty"`
	OnDisk           bool   `json:"onDisk"`
	LocalPath        string `json:"localPath"`
	UpdatesAvailable bool   `json:"updatesAvailable"`
}

type statusHarnessRow struct {
	ID       string `json:"id"`
	Enabled  bool   `json:"enabled"`
	Synced   int    `json:"synced"`
	Drifted  int    `json:"drifted"`
	Missing  int    `json:"missing"`
	Conflict int    `json:"conflict"`
}

type statusReport struct {
	Initialized      bool               `json:"initialized"`
	ProjectRoot      string             `json:"projectRoot"`
	LibraryRoot      string             `json:"libraryRoot"`
	Mode             string             `json:"mode"`
	Libraries        []statusLibraryRow `json:"libraries"`
	Harnesses        []statusHarnessRow `json:"harnesses"`
	SkillsCanonical  int                `json:"skillsCanonical"`
	SkillsPrivate    int                `json:"skillsPrivate"`
	SkillsFromLibs   int                `json:"skillsFromLibraries"`
	MissingLibraries int                `json:"missingLibraries"`
	UpdatableLibs    int                `json:"updatableLibraries"`
	DriftedHarnesses int                `json:"driftedHarnesses"`
}

type statusRenderable struct{ Report statusReport }

func (r *statusRenderable) JSON() any { return r.Report }
func (r *statusRenderable) Plain(w io.Writer) error {
	rep := r.Report
	if !rep.Initialized {
		fmt.Fprintf(w, "adept: NOT initialized in %s — run `adept init`\n", rep.ProjectRoot)
		return nil
	}
	fmt.Fprintf(w, "adept: initialized at %s (mode=%s)\n", rep.ProjectRoot, rep.Mode)
	fmt.Fprintf(w, "library root: %s\n", rep.LibraryRoot)
	if len(rep.Libraries) == 0 {
		fmt.Fprintln(w, "libraries: (none — project-only mode)")
	} else {
		fmt.Fprintln(w, "libraries:")
		tw := NewTabWriter(w)
		fmt.Fprintln(tw, "  NAME\tREF\tON-DISK\tREMOTE")
		for _, l := range rep.Libraries {
			present := "yes"
			switch {
			case !l.OnDisk:
				present = "no (run `adept library add` or `git pull`)"
			case l.UpdatesAvailable:
				present = "yes (update available)"
			}
			ref := l.Ref
			if ref == "" {
				ref = "-"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", l.Name, ref, present, l.Remote)
		}
		_ = tw.Flush()
		if rep.UpdatableLibs > 0 {
			noun := "library has"
			if rep.UpdatableLibs > 1 {
				noun = "libraries have"
			}
			fmt.Fprintf(w, "updates: %d %s newer skills — run `adept library update`\n", rep.UpdatableLibs, noun)
		}
	}
	if rep.SkillsPrivate > 0 {
		fmt.Fprintf(w, "skills: %d published, %d private, %d resolved from libraries\n",
			rep.SkillsCanonical, rep.SkillsPrivate, rep.SkillsFromLibs)
	} else {
		fmt.Fprintf(w, "skills: %d in project canonical, %d resolved from libraries\n",
			rep.SkillsCanonical, rep.SkillsFromLibs)
	}
	if len(rep.Harnesses) == 0 {
		fmt.Fprintln(w, "harnesses: (none enabled — run `adept harness add <id>`)")
		return nil
	}
	fmt.Fprintln(w, "harnesses:")
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "  ID\tENABLED\tSYNCED\tDRIFTED\tMISSING\tCONFLICT")
	for _, h := range rep.Harnesses {
		state := "no"
		if h.Enabled {
			state = "yes"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%d\t%d\t%d\t%d\n", h.ID, state, h.Synced, h.Drifted, h.Missing, h.Conflict)
	}
	_ = tw.Flush()
	if rep.DriftedHarnesses > 0 {
		fmt.Fprintf(w, "\ndrift: %d harness(es) out of sync — run `adept diff` or `adept sync`\n", rep.DriftedHarnesses)
	}
	return nil
}

// libraryHasUpdate reports whether the library clone at dir is behind its
// tracked remote ref. Offline by default: it compares local HEAD against the
// already-fetched origin/<ref> tracking ref, so a stale clone won't show an
// update until something fetches. Pass fetch=true to refresh tracking refs
// first (network). Any git error is treated as "no known update" — status
// must never fail just because a remote is unreachable or has no origin.
//
// ponytail: HEAD != origin/<ref> is the signal; a diverged (non-fast-forward)
// clone also trips it. That's fine — `adept library update` shows the real
// diff and refuses a non-ff pull with a clear error.
func libraryHasUpdate(ctx context.Context, d *Deps, dir, ref string, fetch bool) bool {
	if d.Git == nil || !d.Git.IsRepo(dir) {
		return false
	}
	if ref == "" {
		ref = "main"
	}
	if fetch {
		if err := d.Git.Fetch(ctx, dir); err != nil {
			d.Log.Warn("status fetch library", "dir", dir, "err", err)
			return false
		}
	}
	local, err := d.Git.RevParse(ctx, dir, "HEAD")
	if err != nil {
		return false
	}
	remote, err := d.Git.RevParse(ctx, dir, "origin/"+ref)
	if err != nil {
		return false
	}
	return local != remote
}

// collectStatus assembles the report. Pure read; never mutates state.
// Returns successfully even when the project is not initialized — the
// Initialized=false branch tells the caller to recommend `init`. ctx is
// threaded into the drift/status orchestration so Ctrl-C and command
// timeouts cancel the (potentially slow) Status call.
func collectStatus(ctx context.Context, d *Deps, fetch bool) (statusReport, error) {
	rep := statusReport{}
	libRoot, err := d.ResolveLibraryRoot()
	if err != nil {
		return rep, err
	}
	rep.LibraryRoot = libRoot

	projRoot, err := d.ResolveProjectRoot()
	if err != nil {
		return rep, err
	}
	rep.ProjectRoot = projRoot

	basePath := filepath.Join(projRoot, adept.BaseDirName)
	if _, statErr := os.Stat(basePath); errors.Is(statErr, fs.ErrNotExist) {
		rep.Initialized = false
		return rep, nil
	}
	rep.Initialized = true

	p, err := d.Project()
	if err != nil {
		return rep, err
	}
	cfg, err := p.Config()
	if err != nil {
		return rep, err
	}
	rep.Mode = string(d.Config.GetMode(cfg))

	// Libraries
	libsRoot, err := d.ResolveLibrariesRoot()
	if err != nil {
		return rep, err
	}
	for _, l := range cfg.Libraries {
		local := filepath.Join(libsRoot, l.Name)
		onDisk := false
		updatable := false
		if _, statErr := os.Stat(local); statErr == nil {
			onDisk = true
			updatable = libraryHasUpdate(ctx, d, local, l.Ref, fetch)
			if updatable {
				rep.UpdatableLibs++
			}
		} else {
			rep.MissingLibraries++
		}
		rep.Libraries = append(rep.Libraries, statusLibraryRow{
			Name:             l.Name,
			Remote:           l.Remote,
			Ref:              l.Ref,
			OnDisk:           onDisk,
			LocalPath:        local,
			UpdatesAvailable: updatable,
		})
	}

	// Resolved skill counts
	projSkills, err := p.ListSkills()
	if err != nil {
		return rep, err
	}
	rep.SkillsCanonical = len(projSkills)
	privSkills, err := p.ListPrivateSkills()
	if err != nil {
		return rep, err
	}
	rep.SkillsPrivate = len(privSkills)
	resolved, err := resolveSkills(d, p)
	if err != nil {
		return rep, err
	}
	// resolved = published canonical ∪ private ∪ libraries; subtract the first
	// two so the remainder is genuinely "from libraries".
	rep.SkillsFromLibs = len(resolved) - rep.SkillsCanonical - rep.SkillsPrivate
	if rep.SkillsFromLibs < 0 {
		rep.SkillsFromLibs = 0
	}

	// Harness state — only run the drift report when harnesses are
	// enabled, otherwise we'd render the whole world for nothing.
	if err := d.LoadUserAdapters(); err != nil {
		d.Log.Warn("load user adapters", "err", err)
	}
	enabled := map[string]bool{}
	for _, h := range cfg.Harnesses {
		enabled[h] = true
	}
	if len(cfg.Harnesses) > 0 {
		reports, derr := d.Orchestrator.Status(ctx, p, harness.StatusOptions{Skills: resolved})
		if derr != nil {
			return rep, derr
		}
		for _, dr := range reports {
			row := statusHarnessRow{
				ID:       dr.Harness,
				Enabled:  enabled[dr.Harness],
				Synced:   len(dr.Synced),
				Drifted:  len(dr.Drifted),
				Missing:  len(dr.Missing),
				Conflict: len(dr.Conflict),
			}
			if row.Drifted > 0 || row.Missing > 0 || row.Conflict > 0 {
				rep.DriftedHarnesses++
			}
			rep.Harnesses = append(rep.Harnesses, row)
		}
	}
	return rep, nil
}
