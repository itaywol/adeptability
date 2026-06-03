package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/locks"
	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/internal/registry"
	gh "github.com/itaywol/adeptability/internal/registry/github"
	"github.com/itaywol/adeptability/internal/registry/skillssh"
	"github.com/itaywol/adeptability/internal/scan"
	"github.com/itaywol/adeptability/pkg/adept"
)

// scanner is the package-level scanner instance. Stateless, so safe to
// share across commands.
var scanner = scan.NewScanner()

// ---------- skill install <slug> ----------

func newSkillInstallCmd(d *Deps) *cobra.Command {
	var yes, allowUnsafe bool
	c := &cobra.Command{
		Use:   "install <owner>/<repo>[#ref]/<skill>",
		Short: "Install a skill from skills.sh / GitHub into the project canonical",
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().BoolVar(&yes, "yes", false, "skip the install preview confirmation")
	c.Flags().BoolVar(&allowUnsafe, "allow-unsafe", false, "install even when the sandbox sniff flags suspicious content")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		slug, err := registry.ParseSlug(args[0])
		if err != nil {
			return err
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		w := cmd.OutOrStdout()

		// Resolve SHA + repo metadata + reputation in parallel-ish.
		sha, err := d.GitHub.ResolveRef(ctx, slug.Owner, slug.Repo, slug.Ref)
		if err != nil {
			return err
		}
		meta, err := d.GitHub.RepoInfo(ctx, slug.Owner, slug.Repo)
		if err != nil {
			return err
		}
		installs := lookupInstalls(ctx, d.SkillsSh, slug)

		// Download tarball + extract the target skill directory.
		body, err := d.GitHub.FetchTarball(ctx, slug.Owner, slug.Repo, sha)
		if err != nil {
			return err
		}
		defer body.Close()
		files, matched, err := gh.ExtractSkillDir(body, slug.Skill, slug.CandidateLayouts())
		if err != nil {
			return err
		}

		// Sanity-check before showing the preview: must contain SKILL.md
		// and must parse as a canonical skill.
		md, ok := files[adept.SkillFileName]
		if !ok {
			return fmt.Errorf("skill %q: SKILL.md missing from %s", slug.Skill, matched)
		}
		skillObj, body2, err := d.Parser.ParseFrontmatter(md)
		if err != nil {
			return fmt.Errorf("skill %q: parse SKILL.md: %w", slug.Skill, err)
		}
		skillObj.Body = body2
		if skillObj.ID == "" {
			skillObj.ID = slug.Skill
		}

		// Structured static scan: prose-rules over SKILL.md + tighter
		// script-only rules over any sidecars in the tarball.
		sideForScan := map[string][]byte{}
		for k, v := range files {
			if k == adept.SkillFileName {
				continue
			}
			sideForScan[k] = v
		}
		target := scan.Target{
			Name:     slug.String(),
			Body:     md,
			Sidecars: sideForScan,
		}
		report := scanner.Scan(target)

		// Phase 2.2 intent pass — runs when the project has both an LLM
		// provider configured AND scan.onInstall enabled (or the
		// default-on-when-llm-configured fallback). Failures degrade
		// gracefully to the static report; we don't want a flaky LLM to
		// block install on its own.
		report = maybeRunLLMReview(ctx, d, target, report)

		// In --json mode the preview/confirm are human-only and would
		// corrupt machine-readable stdout, so suppress them and treat the
		// run as non-interactive (require --yes; never block on a prompt).
		jsonMode := d.Flags != nil && d.Flags.JSON
		if !jsonMode {
			printInstallPreview(w, slug, sha, meta, installs, matched, sortKeys(files), report)
		}

		// The blocking severity is configurable (default critical).
		// Lower severities pass through unless the user has tightened
		// the gate via `adept config set scan.blockSeverity`.
		if blocks := installBlocks(d, p, report); blocks && !allowUnsafe {
			return fmt.Errorf("install aborted by safety scan; review findings and pass --allow-unsafe to override")
		}
		if jsonMode {
			if !yes {
				return fmt.Errorf("install in --json mode is non-interactive; pass --yes to confirm")
			}
		} else if !yes && !confirm(cmd.InOrStdin(), w, "proceed with install?") {
			fmt.Fprintln(w, "install cancelled")
			return nil
		}

		// Write project canonical files. We deliberately overwrite if the
		// skill is already locked (re-install path); a stray manual copy
		// is detected by lockfile absence + existing dir + --force gate.
		_, locked := loadLockOrFail(d, p).Get(skillObj.ID)
		if !locked && p.HasSkill(skillObj.ID) {
			return fmt.Errorf("skill %q: already exists in project canonical without a lock entry; remove it first or rename", skillObj.ID)
		}
		if err := writeExternalSkill(p, skillObj.ID, files); err != nil {
			return err
		}

		// Snapshot base so future syncs treat as freshly installed.
		baseDir := filepath.Join(p.BaseSnapshotsDir(), skillObj.ID)
		_ = os.RemoveAll(baseDir)
		if err := writeExternalSkillAt(baseDir, files); err != nil {
			return fmt.Errorf("snapshot base: %w", err)
		}

		// Persist lockfile entry.
		lock, err := locks.Load(lockPath(p))
		if err != nil {
			return err
		}
		hash := hashFiles(files)
		lock.Set(skillObj.ID, locks.Entry{
			Source:      locks.SourceSkillsSh,
			Slug:        slug.String(),
			Repo:        meta.HTMLURL,
			Ref:         slug.Ref,
			SHA:         sha,
			SkillPath:   matched,
			ContentHash: hash,
			InstalledAt: time.Now().UTC(),
		})
		if err := locks.Save(lockPath(p), lock); err != nil {
			return err
		}

		return d.Print(w, &skillInstallRenderable{
			ID:        skillObj.ID,
			Slug:      slug.String(),
			SHA:       sha,
			Path:      matched,
			Files:     sortKeys(files),
			ScanWorst: string(report.Worst()),
		})
	}
	return c
}

// skillInstallRenderable is the post-install result. In --json mode it is
// the only thing written to stdout (preview/confirm are suppressed); in
// plain mode it prints the familiar one-line summary.
type skillInstallRenderable struct {
	ID        string
	Slug      string
	SHA       string
	Path      string
	Files     []string
	ScanWorst string
}

func (r *skillInstallRenderable) JSON() any {
	return map[string]any{
		"id":        r.ID,
		"slug":      r.Slug,
		"sha":       r.SHA,
		"path":      r.Path,
		"files":     r.Files,
		"scanWorst": r.ScanWorst,
	}
}

func (r *skillInstallRenderable) Plain(w io.Writer) error {
	_, err := fmt.Fprintf(w, "installed %s @ %s (%s)\n", r.ID, shortSHA(r.SHA), r.Slug)
	return err
}

// ---------- skill update [<id>] ----------

func newSkillUpdateCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:               "update [<id>]",
		Short:             "Re-resolve locked external skills against upstream and bump SHA",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: lockedSkillCompletion(d),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		lock, err := locks.Load(lockPath(p))
		if err != nil {
			return err
		}
		ids := args
		if len(ids) == 0 {
			ids = lock.IDs()
		}
		if len(ids) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "nothing locked to update")
			return nil
		}
		ctx := cmd.Context()
		w := cmd.OutOrStdout()
		for _, id := range ids {
			entry, ok := lock.Get(id)
			if !ok {
				fmt.Fprintf(w, "%s: not locked, skipping\n", id)
				continue
			}
			slug, err := registry.ParseSlug(entry.Slug)
			if err != nil {
				return err
			}
			sha, err := d.GitHub.ResolveRef(ctx, slug.Owner, slug.Repo, slug.Ref)
			if err != nil {
				return err
			}
			if sha == entry.SHA {
				fmt.Fprintf(w, "%s: up to date (%s)\n", id, shortSHA(sha))
				continue
			}
			fmt.Fprintf(w, "%s: %s -> %s\n", id, shortSHA(entry.SHA), shortSHA(sha))
			body, err := d.GitHub.FetchTarball(ctx, slug.Owner, slug.Repo, sha)
			if err != nil {
				return err
			}
			files, matched, err := gh.ExtractSkillDir(body, slug.Skill, slug.CandidateLayouts())
			body.Close()
			if err != nil {
				return err
			}
			if err := writeExternalSkill(p, id, files); err != nil {
				return err
			}
			baseDir := filepath.Join(p.BaseSnapshotsDir(), id)
			_ = os.RemoveAll(baseDir)
			if err := writeExternalSkillAt(baseDir, files); err != nil {
				return fmt.Errorf("snapshot base: %w", err)
			}
			entry.SHA = sha
			entry.SkillPath = matched
			entry.ContentHash = hashFiles(files)
			entry.InstalledAt = time.Now().UTC()
			lock.Set(id, entry)
			// Persist after each successful id so a later per-id error
			// (ResolveRef/FetchTarball/Extract) can't leave already-
			// rewritten on-disk skills pointing at the old SHA/hash in
			// the lockfile.
			if err := locks.Save(lockPath(p), lock); err != nil {
				return err
			}
		}
		return nil
	}
	return c
}

// ---------- skill info <slug> ----------

func newSkillInfoCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "info <owner>/<repo>[#ref]/<skill>",
		Short: "Show repo, license, stars, installs, and current SHA for a skill",
		Args:  cobra.ExactArgs(1),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		slug, err := registry.ParseSlug(args[0])
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		sha, err := d.GitHub.ResolveRef(ctx, slug.Owner, slug.Repo, slug.Ref)
		if err != nil {
			return err
		}
		meta, err := d.GitHub.RepoInfo(ctx, slug.Owner, slug.Repo)
		if err != nil {
			return err
		}
		installs := lookupInstalls(ctx, d.SkillsSh, slug)
		return d.Print(cmd.OutOrStdout(), &skillInfoRenderable{
			Slug:     slug.String(),
			SHA:      sha,
			Meta:     meta,
			Installs: installs,
		})
	}
	return c
}

type skillInfoRenderable struct {
	Slug     string
	SHA      string
	Meta     gh.RepoMeta
	Installs int
}

func (r *skillInfoRenderable) JSON() any {
	return map[string]any{
		"slug":          r.Slug,
		"sha":           r.SHA,
		"repo":          r.Meta.HTMLURL,
		"stars":         r.Meta.Stars,
		"forks":         r.Meta.Forks,
		"license":       r.Meta.License,
		"pushedAt":      r.Meta.PushedAt,
		"installs":      r.Installs,
		"defaultBranch": r.Meta.DefaultBranch,
	}
}

func (r *skillInfoRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintf(tw, "SLUG\t%s\n", r.Slug)
	fmt.Fprintf(tw, "REPO\t%s\n", r.Meta.HTMLURL)
	fmt.Fprintf(tw, "SHA\t%s\n", r.SHA)
	fmt.Fprintf(tw, "DEFAULT\t%s\n", r.Meta.DefaultBranch)
	if r.Meta.License != "" {
		fmt.Fprintf(tw, "LICENSE\t%s\n", r.Meta.License)
	} else {
		fmt.Fprintf(tw, "LICENSE\t(none detected)\n")
	}
	fmt.Fprintf(tw, "STARS\t%d\n", r.Meta.Stars)
	if r.Installs > 0 {
		fmt.Fprintf(tw, "INSTALLS\t%d\n", r.Installs)
	}
	if !r.Meta.PushedAt.IsZero() {
		fmt.Fprintf(tw, "PUSHED\t%s\n", r.Meta.PushedAt.Format(time.RFC3339))
	}
	if r.Meta.Description != "" {
		fmt.Fprintf(tw, "DESCRIPTION\t%s\n", r.Meta.Description)
	}
	return tw.Flush()
}

// ---------- skill search <query> ----------

func newSkillSearchCmd(d *Deps) *cobra.Command {
	var limit int
	c := &cobra.Command{
		Use:   "search <query>",
		Short: "Search skills.sh for installable skills",
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().IntVar(&limit, "limit", 20, "max results to display")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		hits, err := d.SkillsSh.Search(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		rows := make([]searchRow, 0, len(hits))
		for _, h := range hits {
			if len(rows) >= limit {
				break
			}
			rows = append(rows, searchRow{
				Slug:        h.ID,
				Name:        h.Name,
				Installs:    h.Installs,
				Source:      h.Source,
				Installable: h.IsGitHubSource(),
			})
		}
		return d.Print(cmd.OutOrStdout(), &searchRenderable{Rows: rows})
	}
	return c
}

type searchRow struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Installs    int    `json:"installs"`
	Source      string `json:"source"`
	Installable bool   `json:"installable"`
}

type searchRenderable struct{ Rows []searchRow }

func (r *searchRenderable) JSON() any { return r.Rows }
func (r *searchRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "INSTALLABLE\tINSTALLS\tSLUG")
	for _, row := range r.Rows {
		marker := "no"
		if row.Installable {
			marker = "yes"
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\n", marker, row.Installs, row.Slug)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nrun `adept skill install <slug>` to install (installable=yes only)\n")
	return nil
}

// ---------- helpers ----------

// lookupInstalls hits skills.sh search using the skill name and picks the
// matching row by slug. Best-effort: a failed lookup yields zero, not an
// error, so install/info can still complete when offline.
func lookupInstalls(ctx context.Context, sc skillssh.Client, slug registry.Slug) int {
	if sc == nil {
		return 0
	}
	hits, err := sc.Search(ctx, slug.Skill)
	if err != nil {
		return 0
	}
	want := slug.Owner + "/" + slug.Repo + "/" + slug.Skill
	for _, h := range hits {
		if h.ID == want {
			return h.Installs
		}
	}
	return 0
}

// writeExternalSkill writes the extracted files under
// .adeptability/skills/<id>/, deleting any stale content first.
func writeExternalSkill(p project.Project, id string, files map[string][]byte) error {
	dst := filepath.Join(p.SkillsDir(), id)
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("clear %s: %w", dst, err)
	}
	return writeExternalSkillAt(dst, files)
}

func writeExternalSkillAt(dst string, files map[string][]byte) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for rel, body := range files {
		// Defense in depth against tar-slip: even though ExtractSkillDir
		// already rejects traversal entries, the file keys are
		// attacker-controlled, so verify the cleaned relative path stays
		// inside dst before writing. filepath.Join cleans `..`, which
		// could otherwise place the write outside the skill dir.
		clean := filepath.Clean(filepath.FromSlash(rel))
		if clean == "." || clean == ".." ||
			strings.HasPrefix(clean, ".."+string(filepath.Separator)) ||
			filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe skill file path %q", rel)
		}
		path := filepath.Join(dst, clean)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// hashFiles returns sha256(concatenated path\x00body) so any reorder or
// content change flips the hash. Determined by sorted path order.
func hashFiles(files map[string][]byte) string {
	keys := sortKeys(files)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write(files[k])
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func sortKeys(files map[string][]byte) []string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// printInstallPreview is the human-readable pre-confirmation summary.
// Kept tabular so the JSON mode (which uses different code paths) can
// be added later without disturbing the human view.
func printInstallPreview(w io.Writer, slug registry.Slug, sha string, meta gh.RepoMeta, installs int, matched string, files []string, report scan.Report) {
	fmt.Fprintln(w, "── install preview ─────────────────────────────────────────────")
	fmt.Fprintf(w, "  slug:     %s\n", slug)
	fmt.Fprintf(w, "  repo:     %s\n", meta.HTMLURL)
	fmt.Fprintf(w, "  sha:      %s\n", shortSHA(sha))
	fmt.Fprintf(w, "  path:     %s/\n", matched)
	fmt.Fprintf(w, "  license:  %s\n", orPlaceholder(meta.License, "(none detected)"))
	fmt.Fprintf(w, "  stars:    %d\n", meta.Stars)
	if installs > 0 {
		fmt.Fprintf(w, "  installs: %d (skills.sh)\n", installs)
	}
	if len(files) > 0 {
		fmt.Fprintln(w, "  files:")
		for _, f := range files {
			fmt.Fprintf(w, "    - %s\n", f)
		}
	}
	if len(report.Findings) > 0 {
		counts := report.Counts()
		fmt.Fprintf(w, "  scan:     worst=%s (critical=%d high=%d medium=%d low=%d)\n",
			report.Worst(),
			counts[scan.SeverityCritical], counts[scan.SeverityHigh],
			counts[scan.SeverityMedium], counts[scan.SeverityLow])
		fmt.Fprintln(w, "  findings:")
		for _, f := range report.Findings {
			fmt.Fprintf(w, "    [%s] %s — %s (%s)\n", f.Severity, f.ID, f.Issue, f.Location)
		}
		fmt.Fprintln(w, "  (run `adept skill check "+slug.String()+" --format=markdown` for full detail)")
	}
	fmt.Fprintln(w, "─────────────────────────────────────────────────────────────────")
}

func orPlaceholder(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}

// confirm prompts y/N and returns true on yes.
func confirm(in io.Reader, w io.Writer, prompt string) bool {
	fmt.Fprintf(w, "%s [y/N] ", prompt)
	br := bufio.NewReader(in)
	line, _ := br.ReadString('\n')
	ans := strings.TrimSpace(strings.ToLower(line))
	return ans == "y" || ans == "yes"
}

// lockPath returns the absolute path to .adeptability/adept.lock.json.
func lockPath(p project.Project) string {
	return filepath.Join(filepath.Dir(p.ConfigPath()), locks.FileName)
}

// loadLockOrFail is a small ergonomic helper for callers that don't have
// a clean way to surface a load error mid-flow. Returns an empty lock on
// any read failure; the surrounding code logs but does not abort.
func loadLockOrFail(d *Deps, p project.Project) *locks.Lock {
	l, err := locks.Load(lockPath(p))
	if err != nil {
		d.Log.Warn("load lockfile", "err", err)
		return locks.New()
	}
	return l
}

// lockedSkillCompletion completes against the lockfile keys (used by
// `skill update <TAB>`).
func lockedSkillCompletion(d *Deps) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		p, err := d.Project()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		l, err := locks.Load(lockPath(p))
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]cobra.Completion, 0, len(l.External))
		out = append(out, l.IDs()...)
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}
