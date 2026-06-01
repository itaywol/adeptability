package cli

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/merge"
	"github.com/itaywol/adeptability/internal/sign"
	"github.com/itaywol/adeptability/internal/status"
	"github.com/itaywol/adeptability/pkg/adept"
)

// ---------- init ----------

// init uses positional scope to avoid clashing with the global --project
// path flag. `adept init library` / `adept init project`.
func newInitCmd(d *Deps) *cobra.Command {
	var useGit bool
	c := &cobra.Command{
		Use:       "init <library|project>",
		Short:     "Initialize a library or project",
		Args:      cobra.ExactValidArgs(1),
		ValidArgs: []string{"library", "project"},
	}
	c.Flags().BoolVar(&useGit, "git", false, "initialize as a git repository (library only)")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		switch args[0] {
		case "library":
			root, err := d.ResolveLibraryRoot()
			if err != nil {
				return err
			}
			if err := d.Writer.EnsureDir(filepath.Join(root, adept.SkillsDirName)); err != nil {
				return fmt.Errorf("create library: %w", err)
			}
			if useGit {
				if err := d.Git.Init(ctx, root); err != nil {
					return fmt.Errorf("git init: %w", err)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "library initialized at %s\n", root)
		case "project":
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
			// Persist an empty config so the file exists for tooling that
			// expects it (and so cfg.Schema is on disk for future reads).
			if _, err := os.Stat(p.ConfigPath()); os.IsNotExist(err) {
				if err := p.SaveConfig(d.Config.Empty()); err != nil {
					return fmt.Errorf("write project config: %w", err)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "project initialized at %s\n", p.Root())
		}
		return nil
	}
	return c
}

// ---------- list ----------

func newListCmd(d *Deps) *cobra.Command {
	var fromProject bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List skills in library or project",
		// list takes no positional args. Reject typos like `adept list library`
		// — they look like they should work but used to silently fall through
		// to the (library) default and confuse users into thinking the flag
		// was honored.
		Args: cobra.NoArgs,
	}
	c.Flags().BoolVar(&fromProject, "project-only", false, "list only project skills")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		var skills []*adept.Skill
		var src string
		var err error
		if fromProject {
			p, perr := d.Project()
			if perr != nil {
				return perr
			}
			skills, err = p.ListSkills()
			src = "project"
		} else {
			l, lerr := d.Library()
			if lerr != nil {
				return lerr
			}
			skills, err = l.ListSkills()
			src = "library"
		}
		if err != nil {
			return fmt.Errorf("list skills: %w", err)
		}
		return d.Print(cmd.OutOrStdout(), &skillListRenderable{Source: src, Skills: skills})
	}
	return c
}

type skillListRenderable struct {
	Source string
	Skills []*adept.Skill
}

func (r *skillListRenderable) JSON() any {
	out := struct {
		Source string         `json:"source"`
		Skills []*adept.Skill `json:"skills"`
	}{r.Source, r.Skills}
	return out
}

func (r *skillListRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintf(tw, "ID\tDESCRIPTION\n")
	for _, s := range r.Skills {
		fmt.Fprintf(tw, "%s\t%s\n", s.ID, truncate(s.Description, 64))
	}
	return tw.Flush()
}

// ---------- add ----------

func newAddCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "add <path>",
		Short: "Add a skill from a directory into the library",
		Args:  cobra.ExactArgs(1),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		src := args[0]
		s, err := d.Loader.LoadSkillDir(src)
		if err != nil {
			return fmt.Errorf("load skill: %w", err)
		}
		l, err := d.Library()
		if err != nil {
			return err
		}
		if err := l.AddSkill(s, s.Files); err != nil {
			return fmt.Errorf("add to library: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "added %s\n", s.ID)
		return nil
	}
	return c
}

// ---------- show ----------

func newShowCmd(d *Deps) *cobra.Command {
	var fromLibrary bool
	c := &cobra.Command{
		Use:   "show <id>",
		Short: "Show resolved skill metadata",
		Args:  cobra.ExactArgs(1),
	}
	// Renamed from --library to --from-library so we never shadow the global
	// persistent --library string flag.
	c.Flags().BoolVar(&fromLibrary, "from-library", false, "look up in library instead of project")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id := args[0]
		var s *adept.Skill
		var err error
		if fromLibrary {
			l, lerr := d.Library()
			if lerr != nil {
				return lerr
			}
			s, err = l.GetSkill(id)
		} else {
			p, perr := d.Project()
			if perr != nil {
				return perr
			}
			s, err = p.GetSkill(id)
		}
		if err != nil {
			return err
		}
		return d.Print(cmd.OutOrStdout(), &skillShowRenderable{Skill: s})
	}
	return c
}

type skillShowRenderable struct{ Skill *adept.Skill }

func (r *skillShowRenderable) JSON() any { return r.Skill }

func (r *skillShowRenderable) Plain(w io.Writer) error {
	s := r.Skill
	tw := NewTabWriter(w)
	fmt.Fprintf(tw, "ID\t%s\n", s.ID)
	fmt.Fprintf(tw, "DESCRIPTION\t%s\n", s.Description)
	fmt.Fprintf(tw, "ACTIVATION\t%s\n", s.Activation)
	if len(s.Globs) > 0 {
		fmt.Fprintf(tw, "GLOBS\t%s\n", strings.Join(s.Globs, ", "))
	}
	if len(s.AllowedTools) > 0 {
		fmt.Fprintf(tw, "ALLOWED-TOOLS\t%s\n", strings.Join(s.AllowedTools, ", "))
	}
	if len(s.Targets) > 0 {
		fmt.Fprintf(tw, "TARGETS\t%s\n", strings.Join(s.Targets, ", "))
	}
	if len(s.Tags) > 0 {
		fmt.Fprintf(tw, "TAGS\t%s\n", strings.Join(s.Tags, ", "))
	}
	if len(s.Metadata) > 0 {
		keys := make([]string, 0, len(s.Metadata))
		for k := range s.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(tw, "META[%s]\t%s\n", k, s.Metadata[k])
		}
	}
	return tw.Flush()
}

// ---------- install ----------

func newInstallCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "install <id>",
		Short: "Copy a library skill into the project",
		Args:  cobra.ExactArgs(1),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id := args[0]
		l, err := d.Library()
		if err != nil {
			return err
		}
		s, err := l.GetSkill(id)
		if err != nil {
			return err
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		if err := p.InstallSkill(s, s.Files); err != nil {
			return fmt.Errorf("install: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "installed %s\n", id)
		return nil
	}
	return c
}

// ---------- uninstall ----------

func newUninstallCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "uninstall <id>",
		Short: "Remove a skill from the project",
		Args:  cobra.ExactArgs(1),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		// FRICTION BUG 2: missing skill surfaces as ErrSkillNotFound.
		// project.UninstallSkill already returns this; the CLI lets cobra
		// exit non-zero via ExitFromError.
		if err := p.UninstallSkill(args[0]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "uninstalled %s\n", args[0])
		return nil
	}
	return c
}

// ---------- pull / push / resolve / status / diff ----------

func newPullCmd(d *Deps) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "pull <id>",
		Short: "Pull library updates into the project (when behind)",
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().BoolVar(&force, "force", false, "overwrite project even when not behind (data-loss risk)")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id := args[0]
		st, err := computeSkillStatus(d, id)
		if err != nil {
			return err
		}
		// LocalOnly = library has no source to pull from. --force cannot
		// materialize content out of nothing, so reject before any write.
		if st == adept.StatusLocalOnly {
			return fmt.Errorf("pull %s: skill not present in library", id)
		}
		if !force {
			switch st {
			case adept.StatusSynced:
				fmt.Fprintf(cmd.OutOrStdout(), "pull %s: already synced, nothing to do\n", id)
				return nil
			case adept.StatusAhead:
				return fmt.Errorf("pull %s: project is ahead of library — run `adept push %s` or pass --force to overwrite project edits", id, id)
			case adept.StatusDiverged:
				return fmt.Errorf("pull %s: project and library diverged — run `adept resolve %s --strategy merge` or pass --force to overwrite project edits", id, id)
			case adept.StatusBehind, adept.StatusLibraryOnly:
				// expected pull targets
			}
		}
		return syncSkill(d, cmd.OutOrStdout(), id, pullDir)
	}
	return c
}

func newPushCmd(d *Deps) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "push <id>",
		Short: "Push project edits into the library (when ahead)",
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().BoolVar(&force, "force", false, "overwrite library even when not ahead (data-loss risk)")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id := args[0]
		st, err := computeSkillStatus(d, id)
		if err != nil {
			return err
		}
		// LibraryOnly = project has no source to push from. --force cannot
		// materialize content out of nothing, so reject before any write.
		if st == adept.StatusLibraryOnly {
			return fmt.Errorf("push %s: skill not present in project", id)
		}
		if !force {
			switch st {
			case adept.StatusSynced:
				fmt.Fprintf(cmd.OutOrStdout(), "push %s: already synced, nothing to do\n", id)
				return nil
			case adept.StatusBehind:
				return fmt.Errorf("push %s: library is ahead of project — run `adept pull %s` or pass --force to overwrite library edits", id, id)
			case adept.StatusDiverged:
				return fmt.Errorf("push %s: project and library diverged — run `adept resolve %s --strategy merge` or pass --force to overwrite library edits", id, id)
			case adept.StatusAhead, adept.StatusLocalOnly:
				// expected push sources
			}
		}
		return syncSkill(d, cmd.OutOrStdout(), id, pushDir)
	}
	return c
}

// computeSkillStatus hashes project, base, and library for one skill and
// returns the resolved status. Used by pull/push to gate destructive writes.
func computeSkillStatus(d *Deps, id string) (adept.Status, error) {
	l, err := d.Library()
	if err != nil {
		return "", err
	}
	p, err := d.Project()
	if err != nil {
		return "", err
	}
	projHash, err := p.HashSkill(id)
	if err != nil {
		return "", fmt.Errorf("hash project %s: %w", id, err)
	}
	baseHash, err := p.HashBase(id)
	if err != nil {
		return "", fmt.Errorf("hash base %s: %w", id, err)
	}
	libHash, err := l.HashSkill(id)
	if err != nil {
		return "", fmt.Errorf("hash library %s: %w", id, err)
	}
	return d.Status.Resolve(status.Input{
		ProjectHash: projHash,
		BaseHash:    baseHash,
		LibraryHash: libHash,
	}), nil
}

func newResolveCmd(d *Deps) *cobra.Command {
	var strategy string
	c := &cobra.Command{
		Use:   "resolve <id>",
		Short: "Resolve a diverged skill",
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().StringVar(&strategy, "strategy", "library", "library|project|merge")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		switch strategy {
		case "library":
			return syncSkill(d, cmd.OutOrStdout(), args[0], pullDir)
		case "project":
			return syncSkill(d, cmd.OutOrStdout(), args[0], pushDir)
		case "merge":
			return resolveMerge(d, cmd.OutOrStdout(), args[0], merge.NewMerger())
		default:
			return fmt.Errorf("unknown strategy %q (use library, project, or merge)", strategy)
		}
	}
	return c
}

// resolveMerge runs a 3-way merge between the base snapshot, the project
// canonical state ("ours"), and the library canonical state ("theirs").
// Outputs are written through fsutil.AtomicWrite. When the merger reports
// conflicts we surface their paths, return ErrMergeConflict, and leave the
// project tree carrying conflict markers. A clean merge re-snapshots the
// new base.
func resolveMerge(d *Deps, w io.Writer, id string, mrg merge.Merger) error {
	l, err := d.Library()
	if err != nil {
		return err
	}
	p, err := d.Project()
	if err != nil {
		return err
	}
	if !l.HasSkill(id) {
		return fmt.Errorf("library missing skill %s: %w", id, adept.ErrSkillNotFound)
	}
	if !p.HasSkill(id) {
		return fmt.Errorf("project missing skill %s: %w", id, adept.ErrSkillNotFound)
	}
	if !p.HasBaseSnapshot(id) {
		return fmt.Errorf("resolve merge %s: %w (re-run `adept install %s` or `adept pull %s` to seed)", id, adept.ErrMergeBaseMissing, id, id)
	}

	baseDir := p.BaseDirForSkill(id)
	oursDir := filepath.Join(p.SkillsDir(), id)
	theirsDir := filepath.Join(l.SkillsDir(), id)

	res, err := mrg.Merge(baseDir, oursDir, theirsDir)
	if err != nil {
		return fmt.Errorf("merge %s: %w", id, err)
	}

	for _, f := range res.Files {
		dst := filepath.Join(oursDir, filepath.FromSlash(f.RelPath))
		if f.Deleted {
			if err := d.Writer.RemoveAll(dst); err != nil {
				return fmt.Errorf("merge %s: remove %s: %w", id, f.RelPath, err)
			}
			continue
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := d.Writer.AtomicWrite(dst, f.Bytes, mode); err != nil {
			return fmt.Errorf("merge %s: write %s: %w", id, f.RelPath, err)
		}
	}

	if len(res.Conflicts) > 0 {
		fmt.Fprintf(w, "merge %s: %d conflict(s) — resolve markers and run `adept push %s`:\n", id, len(res.Conflicts), id)
		for _, cf := range res.Conflicts {
			fmt.Fprintf(w, "  CONFLICT %s\n", cf.Path)
		}
		return fmt.Errorf("%w: %d file(s)", adept.ErrMergeConflict, len(res.Conflicts))
	}

	// Clean merge — re-snapshot the new base.
	if err := p.SnapshotBase(id); err != nil {
		return fmt.Errorf("snapshot base: %w", err)
	}
	fmt.Fprintf(w, "merged %s with library (no conflicts)\n", id)
	return nil
}

type direction int

const (
	pullDir direction = iota // library -> project
	pushDir                  // project -> library
)

func syncSkill(d *Deps, w io.Writer, id string, dir direction) error {
	l, err := d.Library()
	if err != nil {
		return err
	}
	p, err := d.Project()
	if err != nil {
		return err
	}

	switch dir {
	case pullDir:
		s, err := l.GetSkill(id)
		if err != nil {
			return err
		}
		if err := p.InstallSkill(s, s.Files); err != nil {
			return err
		}
		fmt.Fprintf(w, "pulled %s into project\n", id)
	case pushDir:
		s, err := p.GetSkill(id)
		if err != nil {
			return err
		}
		if err := l.AddSkill(s, s.Files); err != nil {
			return err
		}
		if err := p.SnapshotBase(id); err != nil {
			return fmt.Errorf("snapshot base: %w", err)
		}
		fmt.Fprintf(w, "pushed %s to library\n", id)
	}
	return nil
}

func newStatusCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "status",
		Short: "Show sync state for every project skill",
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		l, err := d.Library()
		if err != nil {
			return err
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		// Aggregate ids from project + library on-disk content.
		idSet := map[string]struct{}{}
		projSkills, err := p.ListSkills()
		if err != nil {
			return err
		}
		for _, s := range projSkills {
			idSet[s.ID] = struct{}{}
		}
		libSkills, err := l.ListSkills()
		if err != nil {
			return err
		}
		for _, s := range libSkills {
			idSet[s.ID] = struct{}{}
		}
		ids := make([]string, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		out := make([]skillStatusRow, 0, len(ids))
		for _, id := range ids {
			projHash, err := p.HashSkill(id)
			if err != nil {
				return fmt.Errorf("hash project %s: %w", id, err)
			}
			baseHash, err := p.HashBase(id)
			if err != nil {
				return fmt.Errorf("hash base %s: %w", id, err)
			}
			libHash, err := l.HashSkill(id)
			if err != nil {
				return fmt.Errorf("hash library %s: %w", id, err)
			}
			st := d.Status.Resolve(status.Input{
				ProjectHash: projHash,
				BaseHash:    baseHash,
				LibraryHash: libHash,
			})
			out = append(out, skillStatusRow{ID: id, Status: string(st)})
		}
		var dirty bool
		for _, row := range out {
			if row.Status != string(adept.StatusSynced) {
				dirty = true
			}
		}
		if err := d.Print(cmd.OutOrStdout(), &statusRenderable{Rows: out}); err != nil {
			return err
		}
		if dirty {
			return ErrDirty
		}
		return nil
	}
	return c
}

type skillStatusRow struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type statusRenderable struct{ Rows []skillStatusRow }

func (r *statusRenderable) JSON() any { return map[string]any{"skills": r.Rows} }
func (r *statusRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "ID\tSTATUS")
	for _, row := range r.Rows {
		fmt.Fprintf(tw, "%s\t%s\n", row.ID, row.Status)
	}
	return tw.Flush()
}

func newDiffCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "diff <id>",
		Short: "Diff a project skill against the library copy",
		Args:  cobra.ExactArgs(1),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id := args[0]
		l, err := d.Library()
		if err != nil {
			return err
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		libHash, lerr := l.HashSkill(id)
		if lerr != nil {
			return fmt.Errorf("library hash: %w", lerr)
		}
		projHash, perr := p.HashSkill(id)
		if perr != nil {
			return fmt.Errorf("project hash: %w", perr)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "library  %s\nproject  %s\nequal    %t\n", libHash, projHash, libHash == projHash)
		if libHash != projHash {
			return ErrDirty
		}
		return nil
	}
	return c
}

// ---------- doctor ----------

// doctorHarness is the per-harness summary surfaced by doctor.
type doctorHarness struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// doctorReport is the structured output of `adept doctor`.
type doctorReport struct {
	Library    doctorPath      `json:"library"`
	Project    doctorPath      `json:"project"`
	Harnesses  []doctorHarness `json:"harnesses"`
	HasIssues  bool            `json:"hasIssues"`
	IssueCount int             `json:"issueCount"`
}

type doctorPath struct {
	Path   string `json:"path"`
	Status string `json:"status"` // "ok" | "missing"
	Hint   string `json:"hint,omitempty"`
}

type doctorRenderable struct{ Report doctorReport }

func (r *doctorRenderable) JSON() any { return r.Report }
func (r *doctorRenderable) Plain(w io.Writer) error {
	rep := r.Report
	if rep.Library.Status == "ok" {
		fmt.Fprintf(w, "library: ok (%s)\n", rep.Library.Path)
	} else {
		fmt.Fprintf(w, "library: MISSING at %s — %s\n", rep.Library.Path, rep.Library.Hint)
	}
	if rep.Project.Status == "ok" {
		fmt.Fprintf(w, "project: ok (%s)\n", rep.Project.Path)
	} else {
		fmt.Fprintf(w, "project: MISSING %s in %s\n", rep.Project.Hint, rep.Project.Path)
	}
	fmt.Fprintf(w, "harnesses registered: %d\n", len(rep.Harnesses))
	for _, h := range rep.Harnesses {
		fmt.Fprintf(w, "  - %s (%s)\n", h.ID, h.Kind)
	}
	return nil
}

func newDoctorCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Validate library + project setup",
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		rep := doctorReport{}
		libRoot, err := d.ResolveLibraryRoot()
		if err != nil {
			return err
		}
		rep.Library.Path = libRoot
		switch _, statErr := os.Stat(libRoot); {
		case statErr == nil:
			rep.Library.Status = "ok"
		case errors.Is(statErr, fs.ErrNotExist):
			rep.Library.Status = "missing"
			rep.Library.Hint = "run `adept init library`"
			rep.IssueCount++
		default:
			return fmt.Errorf("stat library %s: %w", libRoot, statErr)
		}
		projRoot, err := d.ResolveProjectRoot()
		if err != nil {
			return err
		}
		rep.Project.Path = projRoot
		basePath := filepath.Join(projRoot, adept.BaseDirName)
		switch _, statErr := os.Stat(basePath); {
		case statErr == nil:
			rep.Project.Status = "ok"
		case errors.Is(statErr, fs.ErrNotExist):
			rep.Project.Status = "missing"
			rep.Project.Hint = adept.BaseDirName
			rep.IssueCount++
		default:
			return fmt.Errorf("stat project %s: %w", basePath, statErr)
		}
		for _, a := range d.Registry.List() {
			rep.Harnesses = append(rep.Harnesses, doctorHarness{ID: a.Spec().ID, Kind: string(a.Spec().Kind)})
		}
		rep.HasIssues = rep.IssueCount > 0
		if err := d.Print(cmd.OutOrStdout(), &doctorRenderable{Report: rep}); err != nil {
			return err
		}
		if rep.HasIssues {
			return ErrDirty
		}
		return nil
	}
	return c
}

// ---------- verify ----------

// verifyRow captures the verification outcome for a single skill.
type verifyRow struct {
	ID      string `json:"id"`
	Result  string `json:"result"` // "ok" | "failed" | "unsigned" | "unsupported"
	Message string `json:"message,omitempty"`
}

type verifyRenderable struct {
	Backend string      `json:"backend"`
	Rows    []verifyRow `json:"rows"`
}

func (r *verifyRenderable) JSON() any { return r }

func (r *verifyRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintf(tw, "ID\tRESULT\tDETAIL\n")
	for _, row := range r.Rows {
		detail := row.Message
		if detail == "" {
			detail = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", row.ID, row.Result, detail)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "backend=%s\n", r.Backend)
	return nil
}

// signatureFor reads <skillsDir>/<id>/.signature if present.
func signatureFor(skillsDir, id string) (string, error) {
	path := filepath.Join(skillsDir, id, adept.SignatureName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func newVerifyCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "verify",
		Short: "Verify cosign signatures on every signed project skill",
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		p, err := d.Project()
		if err != nil {
			return err
		}
		skills, err := p.ListSkills()
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(skills))
		for _, s := range skills {
			ids = append(ids, s.ID)
		}
		sort.Strings(ids)

		rows := make([]verifyRow, 0, len(ids))
		anyFailed := false
		for _, id := range ids {
			sigVal, sigErr := signatureFor(p.SkillsDir(), id)
			if sigErr != nil {
				rows = append(rows, verifyRow{ID: id, Result: "failed", Message: sigErr.Error()})
				anyFailed = true
				continue
			}
			if sigVal == "" {
				rows = append(rows, verifyRow{ID: id, Result: "unsigned"})
				continue
			}
			blob, herr := readSkillBlob(p.SkillsDir(), id)
			if herr != nil {
				rows = append(rows, verifyRow{ID: id, Result: "failed", Message: herr.Error()})
				anyFailed = true
				continue
			}
			sigBytes, certBytes, perr := parseCosignSignature(sigVal)
			if perr != nil {
				rows = append(rows, verifyRow{ID: id, Result: "unsupported", Message: perr.Error()})
				continue
			}
			if err := d.Verifier.Verify(ctx, blob, sigBytes, certBytes); err != nil {
				rows = append(rows, verifyRow{ID: id, Result: "failed", Message: err.Error()})
				anyFailed = true
				continue
			}
			rows = append(rows, verifyRow{ID: id, Result: "ok"})
		}

		backend := string(d.SignBackend)
		if backend == "" {
			backend = string(sign.BackendNoop)
		}
		if err := d.Print(cmd.OutOrStdout(), &verifyRenderable{Backend: backend, Rows: rows}); err != nil {
			return err
		}
		if anyFailed {
			return ErrDirty
		}
		return nil
	}
	return c
}

// readSkillBlob loads the canonical SKILL.md content used as the signing
// subject.
func readSkillBlob(skillsDir, id string) ([]byte, error) {
	path := filepath.Join(skillsDir, id, adept.SkillFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

// parseCosignSignature splits a "cosign:<b64-sig>:<b64-cert>" value into raw
// byte buffers. Returns a descriptive error when the format is anything else.
func parseCosignSignature(s string) (sig, cert []byte, err error) {
	const prefix = "cosign:"
	if !strings.HasPrefix(s, prefix) {
		return nil, nil, fmt.Errorf("signature scheme not cosign (got %q)", schemePrefix(s))
	}
	body := s[len(prefix):]
	parts := strings.SplitN(body, ":", 2)
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("malformed cosign signature: want <b64-sig>:<b64-cert>")
	}
	sig, err = base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, nil, fmt.Errorf("decode sig: %w", err)
	}
	cert, err = base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, nil, fmt.Errorf("decode cert: %w", err)
	}
	return sig, cert, nil
}

func schemePrefix(s string) string {
	if i := strings.Index(s, ":"); i >= 0 {
		return s[:i]
	}
	if len(s) > 16 {
		return s[:16] + "…"
	}
	return s
}

// ---------- upgrade ----------

func newUpgradeCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade to the latest release",
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		fmt.Fprintf(cmd.OutOrStdout(),
			"adept upgrade: run `brew upgrade adeptability`, `scoop update adeptability`, or re-run the curl installer.\nCurrent version: %s\n",
			d.Build.Version)
		return nil
	}
	return c
}

// truncate cuts a string to n runes, appending an ellipsis when truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return strings.TrimSpace(s[:n-1]) + "…"
}
