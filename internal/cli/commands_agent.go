package cli

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/canonical"
	"github.com/itaywol/adeptability/internal/project"
	"github.com/itaywol/adeptability/pkg/adept"
)

// newAgentCmd registers the `adept agent {add,edit,remove,list,check}`
// subtree. Canonical agents are single files at .adeptability/agents/<id>.md
// (both layouts — no published/private split; agents are never published to
// library consumers).
//
// NOT to be confused with builtin_agents.go, which registers the ~40 vercel
// harness render TARGETS ("amp", "cline", …) — those are harnesses, not
// subagents.
func newAgentCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "agent", Short: "Manage canonical agents (subagents) in this project"}
	c.AddCommand(
		newAgentAddCmd(d),
		newAgentEditCmd(d),
		newAgentRemoveCmd(d),
		newAgentListCmd(d),
		newAgentCheckCmd(d),
	)
	return c
}

// ---------- agent add ----------

// agentScaffoldTemplates are the built-in `agent add --template` bodies.
// Structure follows the authoring best practices the linter checks for:
// trigger-shaped description, role line, when-invoked steps, output contract,
// and an explicit boundaries section. The evaluator template additionally
// encodes the generator/evaluator pattern: assume broken, act (run things)
// rather than read, verdict format, no praise.
var agentScaffoldTemplates = map[string]string{
	"default": `---
id: {id}
description: <what this agent does AND when to invoke it — e.g. "Reviews Go changes for error-handling bugs. Use proactively after editing internal/ packages.">
# mode: subagent
# tools:
#   - Read
#   - Grep
# model: inherit
---
You are a <role — one line describing the specialist this agent embodies>.

## When invoked

1. <gather the specific context this agent needs first>
2. <do the core work, step by step>
3. <verify the result before reporting>

## Output

Report back with:
- <the exact artifact or answer the caller expects>
- <file paths and line references where relevant>

## Boundaries

- Do: <in-scope actions>
- Do not: <out-of-scope actions — e.g. "modify files", "run destructive commands">
`,
	"evaluator": `---
id: {id}
description: <what this agent reviews AND when — e.g. "Adversarially reviews drafted changes before commit. Use after any sub-agent writes code.">
# tools:
#   - Read
#   - Grep
#   - Bash   # evaluators must ACT (run tests), not just read
---
You are an adversarial reviewer. ASSUME the work is broken until proven otherwise. Do not praise. Find what fails.

## Check, in order

1. Does it run? Execute it — do not judge by reading.
2. Run the tests; paste real output.
3. Hunt edge cases the author skipped.
4. Does behavior match what was asked?

## Verdict

PASS only if every check holds. Otherwise REJECT with each reason listed.

## Boundaries

- Do not fix anything yourself — report only.
- Never approve work you did not execute.
`,
}

func newAgentAddCmd(d *Deps) *cobra.Command {
	var fromPath, template string
	var openEditor bool
	c := &cobra.Command{
		Use:   "add <id>",
		Short: "Create a new project agent from a best-practice template or import an existing file",
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().StringVar(&fromPath, "from", "", "import an existing agent .md file into the project")
	c.Flags().StringVar(&template, "template", "default", "scaffold template: default|evaluator")
	c.Flags().BoolVar(&openEditor, "edit", false, "open the new agent file in $EDITOR after creation")
	_ = c.RegisterFlagCompletionFunc("template", cobra.FixedCompletions([]cobra.Completion{"default", "evaluator"}, cobra.ShellCompDirectiveNoFileComp))
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if err := rejectGlobal(d, "agents"); err != nil {
			return err
		}
		id := args[0]
		if err := validateAgentID(id); err != nil {
			return err
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		if p.HasAgent(id) {
			return fmt.Errorf("agent %q already exists (use `adept agent edit %s` to modify)", id, id)
		}

		if fromPath != "" {
			raw, err := os.ReadFile(fromPath)
			if err != nil {
				return fmt.Errorf("import %s: %w", fromPath, err)
			}
			a, _, err := canonical.ParseAgentFrontmatter(raw)
			if err != nil {
				return fmt.Errorf("import %s: %w", fromPath, err)
			}
			// The requested id wins over any in-file name/filename, so
			// validate AFTER overriding — `--from "/tmp/Code Reviewer.md"`
			// must not fail on the source file's name.
			a.ID = id
			if err := d.AgentValidator.ValidateAgent(a); err != nil {
				return fmt.Errorf("import %s: %w", fromPath, err)
			}
			if err := p.InstallAgent(a); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "imported %s from %s\n", id, fromPath)
		} else {
			if err := writeAgentScaffold(p, id, template); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "created %s (template: %s)\n", id, template)
		}

		if openEditor {
			return runEditor(p.AgentPath(id))
		}
		return nil
	}
	return c
}

// writeAgentScaffold drops the chosen template so the user edits a
// best-practice structure instead of a blank file.
func writeAgentScaffold(p project.Project, id, template string) error {
	body, ok := agentScaffoldTemplates[template]
	if !ok {
		names := make([]string, 0, len(agentScaffoldTemplates))
		for k := range agentScaffoldTemplates {
			names = append(names, k)
		}
		sort.Strings(names)
		return fmt.Errorf("unknown --template %q (want %s)", template, strings.Join(names, "|"))
	}
	if err := os.MkdirAll(p.AgentsDir(), 0o755); err != nil {
		return fmt.Errorf("create agents dir: %w", err)
	}
	content := strings.ReplaceAll(body, "{id}", id)
	if err := os.WriteFile(p.AgentPath(id), []byte(content), 0o644); err != nil {
		return fmt.Errorf("write scaffold: %w", err)
	}
	return nil
}

// ---------- agent edit ----------

func newAgentEditCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:               "edit <id>",
		Short:             "Open the project agent's file in $EDITOR",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: projectAgentCompletion(d),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if err := rejectGlobal(d, "agents"); err != nil {
			return err
		}
		id := args[0]
		p, err := d.Project()
		if err != nil {
			return err
		}
		if !p.HasAgent(id) {
			return fmt.Errorf("agent %q not present in project (run `adept agent add %s` or `adept sync-from`)", id, id)
		}
		return runEditor(p.AgentPath(id))
	}
	return c
}

// ---------- agent remove ----------

func newAgentRemoveCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:               "remove <id>",
		Short:             "Remove an agent from the project canonical",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: projectAgentCompletion(d),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if err := rejectGlobal(d, "agents"); err != nil {
			return err
		}
		id := args[0]
		p, err := d.Project()
		if err != nil {
			return err
		}
		if err := p.UninstallAgent(id); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", id)
		return nil
	}
	return c
}

// ---------- agent list ----------

func newAgentListCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:   "list",
		Short: "List agents in the project canonical",
		Args:  cobra.NoArgs,
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := rejectGlobal(d, "agents"); err != nil {
			return err
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		agents, err := p.ListAgents()
		if err != nil {
			return err
		}
		rows := make([]agentRow, 0, len(agents))
		for _, a := range agents {
			rows = append(rows, agentRow{
				ID:          a.ID,
				Mode:        string(a.Mode),
				Targets:     a.Targets,
				Description: a.Description,
			})
		}
		return d.Print(cmd.OutOrStdout(), &agentListRenderable{Rows: rows})
	}
	return c
}

type agentRow struct {
	ID          string   `json:"id"`
	Mode        string   `json:"mode"`
	Targets     []string `json:"targets,omitempty"`
	Description string   `json:"description"`
}

type agentListRenderable struct{ Rows []agentRow }

func (r *agentListRenderable) JSON() any { return r.Rows }
func (r *agentListRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "ID\tMODE\tTARGETS\tDESCRIPTION")
	for _, row := range r.Rows {
		targets := "all"
		if len(row.Targets) > 0 {
			targets = strings.Join(row.Targets, ",")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.ID, row.Mode, targets, truncate(row.Description, 64))
	}
	return tw.Flush()
}

// ---------- helpers ----------

// validateAgentID applies the canonical agent id pattern (identical to the
// skill pattern) so we error early instead of inside the writer.
func validateAgentID(id string) error {
	if id == "" {
		return fmt.Errorf("agent id is required")
	}
	if !skillIDPattern.MatchString(id) {
		return fmt.Errorf("agent id %q does not match %s", id, adept.AgentIDPattern)
	}
	return nil
}

// projectAgentCompletion completes against the project's canonical agents.
func projectAgentCompletion(d *Deps) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		p, err := d.Project()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		agents, err := p.ListAgents()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		out := make([]cobra.Completion, 0, len(agents))
		for _, a := range agents {
			out = append(out, a.ID)
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}
