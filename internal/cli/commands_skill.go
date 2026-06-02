package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/pkg/adept"
)

// newSkillCmd registers the `adept skill {add,edit,remove,list}` subtree.
// All writes target the project canonical at .adeptability/skills/<id>/.
// Library content is read-only here: `list` shows the resolved union, but
// `add`/`edit`/`remove` only touch project canonical (per the Model B
// rule that project shadows libraries).
func newSkillCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "skill", Short: "Manage canonical skills in this project"}
	c.AddCommand(
		newSkillAddCmd(d),
		newSkillEditCmd(d),
		newSkillRemoveCmd(d),
		newSkillListCmd(d),
	)
	return c
}

// ---------- skill add ----------

func newSkillAddCmd(d *Deps) *cobra.Command {
	var fromPath string
	var openEditor bool
	c := &cobra.Command{
		Use:   "skill add <id>",
		Short: "Create a new project skill from scratch or import an existing directory",
		Args:  cobra.ExactArgs(1),
	}
	c.Use = "add <id>"
	c.Flags().StringVar(&fromPath, "from", "", "import an existing skill directory into the project")
	c.Flags().BoolVar(&openEditor, "edit", false, "open the new SKILL.md in $EDITOR after creation")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id := args[0]
		if err := validateSkillID(id); err != nil {
			return err
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		if p.HasSkill(id) {
			return fmt.Errorf("skill %q already exists in project (use `adept skill edit %s` to modify)", id, id)
		}

		if fromPath != "" {
			s, err := d.Loader.LoadSkillDir(fromPath)
			if err != nil {
				return fmt.Errorf("import %s: %w", fromPath, err)
			}
			if s.ID != id {
				return fmt.Errorf("import %s: skill id %q does not match requested id %q", fromPath, s.ID, id)
			}
			if err := p.InstallSkill(s, s.Files); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "imported %s from %s\n", id, fromPath)
		} else {
			if err := writeSkillScaffold(p, id); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", id)
		}

		if openEditor {
			return runEditor(skillPath(p, id))
		}
		return nil
	}
	return c
}

// writeSkillScaffold drops a minimal canonical SKILL.md so the user can
// edit instead of staring at a blank file. The frontmatter has just
// enough to parse cleanly.
func writeSkillScaffold(p project.Project, id string) error {
	dir := filepath.Join(p.SkillsDir(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	body := strings.Join([]string{
		"---",
		"id: " + id,
		"description: <one-line summary of when this skill applies>",
		"activation: agent",
		"---",
		"# " + id,
		"",
		"<skill body — replace this paragraph with the instructions you want every",
		"enabled harness to honor when this skill activates>",
		"",
	}, "\n")
	dest := filepath.Join(dir, adept.SkillFileName)
	if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write scaffold: %w", err)
	}
	// Snapshot empty base so future syncs treat it as a fresh local skill.
	baseDir := filepath.Join(p.BaseSnapshotsDir(), id)
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("create base dir: %w", err)
	}
	return nil
}

// ---------- skill edit ----------

func newSkillEditCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:               "edit <id>",
		Short:             "Open the project skill's SKILL.md in $EDITOR",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: projectSkillCompletion(d),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id := args[0]
		p, err := d.Project()
		if err != nil {
			return err
		}
		if !p.HasSkill(id) {
			return fmt.Errorf("skill %q not present in project (run `adept skill add %s` or `adept sync-from`)", id, id)
		}
		return runEditor(skillPath(p, id))
	}
	return c
}

// ---------- skill remove ----------

func newSkillRemoveCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:               "remove <id>",
		Short:             "Remove a skill from the project canonical",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: projectSkillCompletion(d),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id := args[0]
		p, err := d.Project()
		if err != nil {
			return err
		}
		if err := p.UninstallSkill(id); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", id)
		return nil
	}
	return c
}

// ---------- skill list ----------

func newSkillListCmd(d *Deps) *cobra.Command {
	var projectOnly bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List skills resolved for this project (project canonical + libraries)",
		Args:  cobra.NoArgs,
	}
	c.Flags().BoolVar(&projectOnly, "project-only", false, "only show skills present in the project canonical")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		projSkills, err := p.ListSkills()
		if err != nil {
			return err
		}
		projIDs := map[string]struct{}{}
		rows := []skillRow{}
		for _, s := range projSkills {
			rows = append(rows, skillRow{
				ID:          s.ID,
				Source:      "project",
				Description: s.Description,
			})
			projIDs[s.ID] = struct{}{}
		}
		if !projectOnly {
			multi, err := openMultiLibrary(d, p)
			if err != nil {
				return err
			}
			if multi != nil {
				resolutions, err := multi.ListAll()
				if err != nil {
					return err
				}
				for _, r := range resolutions {
					if _, shadowed := projIDs[r.Skill.ID]; shadowed {
						continue
					}
					rows = append(rows, skillRow{
						ID:          r.Skill.ID,
						Source:      "library:" + r.Source,
						Description: r.Skill.Description,
						Shadowed:    r.Shadowed,
					})
				}
			}
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		return d.Print(cmd.OutOrStdout(), &skillListRenderable{Rows: rows})
	}
	return c
}

type skillRow struct {
	ID          string   `json:"id"`
	Source      string   `json:"source"`
	Description string   `json:"description"`
	Shadowed    []string `json:"shadowed,omitempty"`
}

type skillListRenderable struct{ Rows []skillRow }

func (r *skillListRenderable) JSON() any { return r.Rows }
func (r *skillListRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "ID\tSOURCE\tDESCRIPTION")
	for _, row := range r.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", row.ID, row.Source, truncate(row.Description, 64))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	// Warn after the table so it survives piping.
	for _, row := range r.Rows {
		if len(row.Shadowed) > 0 {
			fmt.Fprintf(w, "  warn: %s shadowed by library:%s — also in: %s\n",
				row.ID, row.Source, strings.Join(row.Shadowed, ", "))
		}
	}
	return nil
}

// ---------- helpers ----------

// skillPath returns the canonical SKILL.md path for an installed skill.
func skillPath(p project.Project, id string) string {
	return filepath.Join(p.SkillsDir(), id, adept.SkillFileName)
}

// runEditor opens $EDITOR (or VISUAL, falling back to vi) on path, wired
// to the user's controlling terminal. Returns the editor's exit code as
// an error.
func runEditor(path string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// validateSkillID applies the same pattern enforced by the canonical
// schema so we error early instead of inside the writer.
func validateSkillID(id string) error {
	if id == "" {
		return fmt.Errorf("skill id is required")
	}
	if !skillIDPattern.MatchString(id) {
		return fmt.Errorf("skill id %q does not match %s", id, skillIDPattern.String())
	}
	return nil
}

// projectSkillCompletion completes against the project's canonical skills
// (NOT library resolutions — edit/remove only touch project content).
func projectSkillCompletion(d *Deps) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		p, err := d.Project()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		skills, err := p.ListSkills()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]cobra.Completion, 0, len(skills))
		for _, s := range skills {
			out = append(out, cobra.Completion(s.ID))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// Sanity: make sure fs.ErrNotExist is wired so editor missing-path errors
// surface as proper "not present" messages rather than os-specific text.
var _ = fs.ErrNotExist
var _ = errors.Is
