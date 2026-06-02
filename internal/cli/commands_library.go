package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/harness"
	"github.com/itaywol/adeptability/internal/org"
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
	var fromURL, ref, modeStr string
	c := &cobra.Command{
		Use:   "init",
		Short: "Initialize an adept project (and optionally clone a remote library)",
		Args:  cobra.NoArgs,
		Long: "Creates .adeptability/{skills,base}/ in the project, writes config.json, " +
			"optionally clones a remote library, and adopts any pre-existing harness skills " +
			"(.claude/, .cursor/, .opencode/, AGENTS.md, .github/instructions/) it finds on disk.",
	}
	c.Flags().StringVar(&fromURL, "from", "", "remote library URL (git or HTTPS manifest)")
	c.Flags().StringVar(&ref, "ref", "main", "branch or tag in the remote library (git only)")
	c.Flags().StringVar(&modeStr, "mode", string(adept.ModeSymlink), "harness materialization: symlink|copy")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		w := cmd.OutOrStdout()

		mode := adept.HarnessMode(modeStr)
		if mode != adept.ModeSymlink && mode != adept.ModeCopy {
			return fmt.Errorf("invalid --mode %q (want symlink|copy)", modeStr)
		}

		// 1) library skeleton (lazy create if missing)
		libRoot, err := d.ResolveLibraryRoot()
		if err != nil {
			return err
		}
		if err := d.Writer.EnsureDir(filepath.Join(libRoot, adept.SkillsDirName)); err != nil {
			return fmt.Errorf("create library: %w", err)
		}

		// 2) project skeleton
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

		// 3) load (or create) the project config and stamp mode + library ref
		cfg, err := readOrEmptyConfig(d, p.ConfigPath())
		if err != nil {
			return err
		}
		cfg.Mode = mode
		if fromURL != "" {
			cfg.Library = &adept.LibraryRef{Remote: fromURL, Ref: ref}
		}

		// 4) honor --from
		if fromURL != "" {
			scheme := libraryRemoteScheme(fromURL)
			switch scheme {
			case "git":
				if err := d.Git.CloneOrPull(ctx, fromURL, ref, libRoot); err != nil {
					return fmt.Errorf("clone library %s: %w", fromURL, err)
				}
				fmt.Fprintf(w, "cloned library from %s into %s\n", fromURL, libRoot)
			case "http":
				// HTTP manifest: list of skill IDs the org expects; install
				// each from the already-present library.
				if err := installFromHTTPManifest(d, p, libRoot, fromURL, cmd); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown remote scheme for --from %q", fromURL)
			}
		}

		// 5) auto-adopt pre-existing harness trees, if any
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

		// 6) persist config
		if err := p.SaveConfig(cfg); err != nil {
			return fmt.Errorf("write project config: %w", err)
		}

		// 7) summary
		fmt.Fprintf(w, "project initialized at %s (mode=%s)\n", p.Root(), mode)
		if len(adopted) > 0 {
			fmt.Fprintf(w, "adopted harnesses: %s\n", strings.Join(adopted, ", "))
		}
		return nil
	}
	return c
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

// libraryRemoteScheme classifies a --from URL. Git remotes win:
// `git@host:org/repo.git`, anything ending in `.git`, and `file://...` are
// treated as git. Plain `http(s)://` is treated as an HTTP manifest.
func libraryRemoteScheme(remote string) string {
	if strings.HasPrefix(remote, "git@") {
		return "git"
	}
	if strings.HasPrefix(remote, "file://") {
		return "git"
	}
	if strings.HasSuffix(remote, ".git") {
		return "git"
	}
	if u, err := url.Parse(remote); err == nil {
		switch u.Scheme {
		case "http", "https":
			return "http"
		case "ssh", "git":
			return "git"
		}
	}
	return "git"
}

func installFromHTTPManifest(d *Deps, p project.Project, libRoot, remote string, cmd *cobra.Command) error {
	cache := org.NewFileETagCache(filepath.Join(libRoot, ".org-cache"))
	client := org.NewHTTPClient(strings.TrimRight(remote, "/"), d.OrgParser, http.DefaultClient, cache)
	manifest, err := client.Fetch(cmd.Context())
	if err != nil {
		return fmt.Errorf("fetch org manifest: %w", err)
	}
	l, err := d.Library()
	if err != nil {
		return err
	}
	for _, ref := range manifest.Required {
		s, err := l.GetSkill(ref.ID)
		if err != nil {
			return fmt.Errorf("required skill %s: %w", ref.ID, err)
		}
		if err := p.InstallSkill(s, s.Files); err != nil {
			return err
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "installed %d required skill(s) from %s\n", len(manifest.Required), remote)
	return nil
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

// helpers: ---------- list ----------

func newListCmd(d *Deps) *cobra.Command {
	var fromLibrary bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List skills (project canonical by default)",
		Args:  cobra.NoArgs,
	}
	c.Flags().BoolVar(&fromLibrary, "from-library", false, "list skills in the library instead of the project")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		var skills []*adept.Skill
		var src string
		var err error
		if fromLibrary {
			l, lerr := d.Library()
			if lerr != nil {
				return lerr
			}
			skills, err = l.ListSkills()
			src = "library"
		} else {
			p, perr := d.Project()
			if perr != nil {
				return perr
			}
			skills, err = p.ListSkills()
			src = "project"
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
	return struct {
		Source string         `json:"source"`
		Skills []*adept.Skill `json:"skills"`
	}{r.Source, r.Skills}
}

func (r *skillListRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintf(tw, "ID\tDESCRIPTION\n")
	for _, s := range r.Skills {
		fmt.Fprintf(tw, "%s\t%s\n", s.ID, truncate(s.Description, 64))
	}
	return tw.Flush()
}

// ---------- show ----------

func newShowCmd(d *Deps) *cobra.Command {
	var fromLibrary bool
	c := &cobra.Command{
		Use:   "show <id>",
		Short: "Show resolved skill metadata",
		Args:  cobra.ExactArgs(1),
	}
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

// ---------- doctor ----------

type doctorHarness struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type doctorReport struct {
	Library    doctorPath      `json:"library"`
	Project    doctorPath      `json:"project"`
	Mode       string          `json:"mode"`
	Harnesses  []doctorHarness `json:"harnesses"`
	HasIssues  bool            `json:"hasIssues"`
	IssueCount int             `json:"issueCount"`
}

type doctorPath struct {
	Path   string `json:"path"`
	Status string `json:"status"`
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
	fmt.Fprintf(w, "mode: %s\n", rep.Mode)
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
		Args:  cobra.NoArgs,
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
			rep.Library.Hint = "run `adept init` (library is created on first init)"
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

		// Mode comes from the project config when present.
		modeStr := string(adept.ModeSymlink)
		if p, perr := d.Project(); perr == nil {
			if cfg, cerr := p.Config(); cerr == nil {
				modeStr = string(d.Config.GetMode(cfg))
			}
		}
		rep.Mode = modeStr

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

// helpers shared by list

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

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
