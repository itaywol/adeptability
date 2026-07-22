package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// newLoopCmd registers `adept loop add`. A loop is not a synced resource —
// it is a composition of things adept already manages: a discovery SKILL
// (what the automation reads and judges each round), an evaluator AGENT (the
// independent check that can say "no"), and a schedule (which lives in the
// harness or CI, not in adept). `loop add` scaffolds the composition in one
// shot and prints the first-loop checklist so the safety half — state file,
// isolation, token cap, human review — is not forgotten.
func newLoopCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "loop", Short: "Compose a loop: discovery skill + evaluator agent + optional schedule"}
	c.AddCommand(newLoopAddCmd(d))
	return c
}

// loopWorkflowTemplate is the optional GitHub Actions cron skeleton —
// the machine-off scheduling option. Local alternatives (Claude Code /loop,
// Codex Automations) are noted inline; the harness invocation is left as an
// explicit fill-in because auth and CLI choice belong to the user.
const loopWorkflowTemplate = `# Schedule for the "{id}" loop — cloud cron runs while your machine is off.
# Local alternatives: Claude Code ` + "`/loop`" + ` (machine must stay on) or the
# Codex Automations tab. A mature loop often uses both: local for tight inner
# checks, cloud for the overnight sweep.
name: adept-loop-{id}
on:
  schedule:
    - cron: "0 6 * * *" # 06:00 UTC daily — adjust
  workflow_dispatch: {}
permissions:
  contents: write
jobs:
  run:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      # DISCOVERY — trigger the named "{id}" skill, not a wall of prompt
      # pasted into this file. Fill in your harness CLI + auth secret, e.g.:
      #   - run: claude -p "Run the {id} skill and act on its findings"
      #     env: { ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }} }
      #
      # PERSISTENCE — commit the state file (and the human inbox) so the
      # next run remembers and uncertain work reaches a person.
      - name: persist loop state
        run: |
          git add -A state/ 2>/dev/null || true
          git add -A inbox/ 2>/dev/null || true
          if ! git diff --cached --quiet; then
            git config user.name adept-loop
            git config user.email adept-loop@users.noreply.github.com
            git commit -m "loop({id}): persist state"
            git push
          fi
`

func newLoopAddCmd(d *Deps) *cobra.Command {
	var withWorkflow, openEditor bool
	c := &cobra.Command{
		Use:   "add <id>",
		Short: "Scaffold a loop: <id> discovery skill, <id>-reviewer evaluator agent, optional cron workflow",
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().BoolVar(&withWorkflow, "workflow", false, "also write a GitHub Actions cron skeleton (.github/workflows/adept-loop-<id>.yml)")
	c.Flags().BoolVar(&openEditor, "edit", false, "open the discovery skill in $EDITOR after creation")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if err := rejectGlobal(d, "loops"); err != nil {
			return err
		}
		id := args[0]
		if err := validateSkillID(id); err != nil {
			return err
		}
		reviewerID := id + "-reviewer"
		if err := validateAgentID(reviewerID); err != nil {
			return fmt.Errorf("derived evaluator id: %w", err)
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		private := isLibraryProject(p)
		if (private && p.HasPrivateSkill(id)) || (!private && p.HasSkill(id)) {
			return fmt.Errorf("skill %q already exists (pick another loop id or edit the existing skill)", id)
		}
		if p.HasAgent(reviewerID) {
			return fmt.Errorf("agent %q already exists (pick another loop id or edit the existing agent)", reviewerID)
		}

		w := cmd.OutOrStdout()
		if err := writeSkillScaffold(p, id, private, "triage"); err != nil {
			return err
		}
		fmt.Fprintf(w, "created discovery skill %s (template: triage)%s\n", id, addedSuffix(private))
		if err := writeAgentScaffold(p, reviewerID, "evaluator"); err != nil {
			return err
		}
		fmt.Fprintf(w, "created evaluator agent %s (template: evaluator)\n", reviewerID)

		workflowPath := filepath.Join(".github", "workflows", "adept-loop-"+id+".yml")
		if withWorkflow {
			abs := filepath.Join(p.Root(), workflowPath)
			if _, err := os.Stat(abs); err == nil {
				return fmt.Errorf("%s already exists — refusing to overwrite", workflowPath)
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return fmt.Errorf("create workflows dir: %w", err)
			}
			if err := os.WriteFile(abs, []byte(strings.ReplaceAll(loopWorkflowTemplate, "{id}", id)), 0o644); err != nil {
				return fmt.Errorf("write workflow: %w", err)
			}
			fmt.Fprintf(w, "created schedule skeleton %s\n", workflowPath)
		}

		// The first-loop checklist: the first two elements decide whether the
		// loop can run; the last four decide whether it gets into trouble
		// once it does. Scaffolding covers discovery + evaluator; the rest
		// are deliberate one-line decisions only the builder can make.
		fmt.Fprintf(w, `
first-loop checklist for %q:
  [x] discovery source — edit the Read/Judge sections of skills/%s/SKILL.md
  [x] evaluator — %s says "no" independently; keep it skeptical, let it ACT (run tests)
  [ ] state file — the skill writes ./state/%s.md; commit it so rounds remember
  [ ] schedule — %s
  [ ] isolation — one git worktree per parallel task (e.g. claude --worktree)
  [ ] token cap + human review — set spend ceilings; open PRs, never auto-merge;
      uncertain work goes to ./inbox/, not into a PR

next: edit the scaffolds, then `+"`adept agent check %s`"+` and `+"`adept sync`"+`.
`, id, id, reviewerID, id, scheduleHint(withWorkflow, workflowPath), reviewerID)

		if openEditor {
			return runEditor(skillPathIn(p, id, private))
		}
		return nil
	}
	return c
}

// scheduleHint names the scheduling next-step for the checklist.
func scheduleHint(written bool, path string) string {
	if written {
		return "fill in the harness invocation in " + path
	}
	return "rerun with --workflow for a cloud cron skeleton, or use your harness's local scheduler"
}
