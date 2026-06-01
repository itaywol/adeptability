package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/lockfile"
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
			if err := writeEmptyLock(d, filepath.Join(root, adept.LockFileName)); err != nil {
				return err
			}
			if useGit {
				if err := d.Git.Init(ctx, root); err != nil {
					return fmt.Errorf("git init: %w", err)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "library initialized at %s\n", root)
		case "project":
			root, err := d.ResolveProjectRoot()
			if err != nil {
				return err
			}
			base := filepath.Join(root, adept.BaseDirName, adept.SkillsDirName)
			if err := d.Writer.EnsureDir(base); err != nil {
				return fmt.Errorf("create project: %w", err)
			}
			if err := writeEmptyLock(d, filepath.Join(root, adept.LockFileName)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "project initialized at %s\n", root)
		}
		return nil
	}
	return c
}

func writeEmptyLock(d *Deps, path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	lf := d.Store.Empty()
	return d.Store.Write(path, lf)
}

// ---------- list ----------

func newListCmd(d *Deps) *cobra.Command {
	var fromProject bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List skills in library or project",
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
	fmt.Fprintf(tw, "ID\tVERSION\tDESCRIPTION\n")
	for _, s := range r.Skills {
		fmt.Fprintf(tw, "%s\t%d\t%s\n", s.ID, s.Version, truncate(s.Description, 64))
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
		fmt.Fprintf(cmd.OutOrStdout(), "added %s v%d\n", s.ID, s.Version)
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
	c.Flags().BoolVar(&fromLibrary, "library", false, "look up in library (default: project)")
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
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r.Skill)
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
		libLock, err := d.Store.Read(l.LockfilePath())
		if err != nil {
			return fmt.Errorf("read library lock: %w", err)
		}
		entry, ok := libLock.Skills[id]
		if !ok {
			return fmt.Errorf("library has skill %q but no lock entry; run `adept add` again", id)
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		if err := p.InstallSkill(s, s.Files, entry); err != nil {
			return fmt.Errorf("install: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "installed %s v%d\n", id, entry.Version)
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
	c := &cobra.Command{
		Use:   "pull <id>",
		Short: "Pull library updates into the project (when behind)",
		Args:  cobra.ExactArgs(1),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return syncSkill(d, cmd.OutOrStdout(), args[0], pullDir)
	}
	return c
}

func newPushCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "push <id>",
		Short: "Push project edits into the library (when ahead)",
		Args:  cobra.ExactArgs(1),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return syncSkill(d, cmd.OutOrStdout(), args[0], pushDir)
	}
	return c
}

func newResolveCmd(d *Deps) *cobra.Command {
	var strategy string
	c := &cobra.Command{
		Use:   "resolve <id>",
		Short: "Resolve a diverged skill",
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().StringVar(&strategy, "strategy", "library", "library|project")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		switch strategy {
		case "library":
			return syncSkill(d, cmd.OutOrStdout(), args[0], pullDir)
		case "project":
			return syncSkill(d, cmd.OutOrStdout(), args[0], pushDir)
		default:
			return fmt.Errorf("unknown strategy %q (use library or project)", strategy)
		}
	}
	return c
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
	libLock, err := d.Store.Read(l.LockfilePath())
	if err != nil {
		return fmt.Errorf("read library lock: %w", err)
	}
	projLock, err := d.Store.Read(p.LockfilePath())
	if err != nil {
		return fmt.Errorf("read project lock: %w", err)
	}

	switch dir {
	case pullDir:
		s, err := l.GetSkill(id)
		if err != nil {
			return err
		}
		entry, ok := libLock.Skills[id]
		if !ok {
			return fmt.Errorf("library missing lock entry for %s", id)
		}
		if err := p.InstallSkill(s, s.Files, entry); err != nil {
			return err
		}
		fmt.Fprintf(w, "pulled %s v%d into project\n", id, entry.Version)
	case pushDir:
		s, err := p.GetSkill(id)
		if err != nil {
			return err
		}
		// Bump version when content changed; otherwise keep existing version.
		prev, hadPrev := libLock.Skills[id]
		newVersion := s.Version
		if hadPrev {
			newHash, herr := d.Hasher.HashSkillDir(filepath.Join(p.SkillsDir(), id))
			if herr != nil {
				return fmt.Errorf("hash project skill: %w", herr)
			}
			if newHash != prev.Hash {
				newVersion = prev.Version + 1
				s.Version = newVersion
			} else {
				newVersion = prev.Version
				s.Version = prev.Version
			}
		}
		if err := l.AddSkill(s, s.Files); err != nil {
			return err
		}
		// Refresh project lock with the new library version + hash.
		updatedLibLock, err := d.Store.Read(l.LockfilePath())
		if err != nil {
			return err
		}
		newEntry := updatedLibLock.Skills[id]
		projLock = d.Store.SetEntry(projLock, id, newEntry)
		if err := d.Store.Write(p.LockfilePath(), projLock); err != nil {
			return err
		}
		fmt.Fprintf(w, "pushed %s v%d to library\n", id, newVersion)
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
		libLock, err := d.Store.Read(l.LockfilePath())
		if err != nil {
			return err
		}
		projLock, err := d.Store.Read(p.LockfilePath())
		if err != nil {
			return err
		}
		projSkills, err := p.ListSkills()
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		out := []skillStatusRow{}
		for _, s := range projSkills {
			seen[s.ID] = true
			projHash, herr := d.Hasher.HashSkillDir(filepath.Join(p.SkillsDir(), s.ID))
			if herr != nil {
				return fmt.Errorf("hash %s: %w", s.ID, herr)
			}
			projEntry := entryPtr(projLock, s.ID)
			libEntry := entryPtr(libLock, s.ID)
			st := d.Status.Resolve(status.Input{
				ProjectHash:  projHash,
				ProjectEntry: projEntry,
				LibraryEntry: libEntry,
			})
			out = append(out, skillStatusRow{ID: s.ID, Status: string(st)})
		}
		// Surface library-only skills as informational rows.
		for id := range libLock.Skills {
			if seen[id] {
				continue
			}
			out = append(out, skillStatusRow{ID: id, Status: string(adept.StatusLibraryOnly)})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
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

func entryPtr(lf *adept.LockFile, id string) *adept.LockEntry {
	if lf == nil {
		return nil
	}
	if e, ok := lf.Skills[id]; ok {
		return &e
	}
	return nil
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
		libHash, lerr := d.Hasher.HashSkillDir(filepath.Join(l.SkillsDir(), id))
		if lerr != nil {
			return fmt.Errorf("library hash: %w", lerr)
		}
		projHash, perr := d.Hasher.HashSkillDir(filepath.Join(p.SkillsDir(), id))
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

// ---------- migrate ----------

func newMigrateCmd(d *Deps) *cobra.Command {
	var importFrom string
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate from a prior skillbook library",
	}
	c.Flags().StringVar(&importFrom, "from", "", "skillbook library directory (containing skills/ + skillbook.lock.json)")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		if importFrom == "" {
			return fmt.Errorf("--from is required")
		}
		srcLock := filepath.Join(importFrom, "skillbook.lock.json")
		lf, err := lockfile.MigrateFromSkillbook(srcLock)
		if err != nil {
			return err
		}
		libRoot, err := d.ResolveLibraryRoot()
		if err != nil {
			return err
		}
		if err := d.Writer.EnsureDir(libRoot); err != nil {
			return err
		}
		if err := d.Store.Write(filepath.Join(libRoot, adept.LockFileName), lf); err != nil {
			return err
		}
		// Copy skills/ tree.
		srcSkills := filepath.Join(importFrom, "skills")
		dstSkills := filepath.Join(libRoot, adept.SkillsDirName)
		if _, err := os.Stat(srcSkills); err == nil {
			if err := d.Writer.CopyDir(srcSkills, dstSkills); err != nil {
				return fmt.Errorf("copy skills: %w", err)
			}
		}
		fmt.Fprintf(cmd.OutOrStdout(), "migrated %d skill(s) from %s\n", len(lf.Skills), importFrom)
		return nil
	}
	return c
}

// ---------- doctor ----------

func newDoctorCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "doctor",
		Short: "Validate library + project setup",
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		w := cmd.OutOrStdout()
		warnings := 0
		// Library
		libRoot, err := d.ResolveLibraryRoot()
		if err != nil {
			return err
		}
		if _, err := os.Stat(libRoot); os.IsNotExist(err) {
			fmt.Fprintf(w, "library: MISSING at %s — run `adept init --library`\n", libRoot)
			warnings++
		} else {
			fmt.Fprintf(w, "library: ok (%s)\n", libRoot)
		}
		// Project
		projRoot, err := d.ResolveProjectRoot()
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(projRoot, adept.BaseDirName)); os.IsNotExist(err) {
			fmt.Fprintf(w, "project: MISSING %s in %s\n", adept.BaseDirName, projRoot)
			warnings++
		} else {
			fmt.Fprintf(w, "project: ok (%s)\n", projRoot)
		}
		// Registered harnesses
		fmt.Fprintf(w, "harnesses registered: %d\n", len(d.Registry.List()))
		for _, a := range d.Registry.List() {
			fmt.Fprintf(w, "  - %s (%s)\n", a.Spec().ID, a.Spec().Kind)
		}
		if warnings > 0 {
			return ErrDirty
		}
		return nil
	}
	return c
}

// ---------- verify ----------

func newVerifyCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "verify",
		Short: "Verify project signatures",
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		// v0.1: Noop verifier — every signature passes. Real cosign keyless impl is v0.2.
		fmt.Fprintf(cmd.OutOrStdout(), "verifier=noop; no signatures verified (v0.2 brings cosign)\n")
		return nil
	}
	return c
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

// Build timestamp helper for migrate (kept here to avoid an unused import).
var _ = time.Now
