# Composing loops

A loop is a system that prompts your agents so you don't have to: on a schedule it
**discovers** work worth doing, **hands it off** to an agent, **verifies** the result with
an independent check, **persists** state to disk, and **reschedules** itself. Drop any of
the five moves and the loop fails a predictable way — the most common being a loop with no
real verifier, approving its own output at machine speed.

adept owns two of the loop's six parts as canonical, synced resources:

| Part | Realizes | adept |
| --- | --- | --- |
| **Skill** | Discovery — the automation triggers a *named skill*, not a wall of prompt in a cron job | `adept skill add <id> --template triage` |
| **Sub-agents** | Verification — a generator writes, a separate skeptical evaluator judges | `adept agent add <id> --template evaluator` |
| Automations | Scheduling | harness (`/loop`, Codex Automations) or CI cron |
| Worktrees | Handoff isolation | git / harness flags |
| Connectors | Reach beyond the filesystem | MCP, configured in the harness |
| Memory | Persistence across rounds | a committed `./state/<id>.md` |

## One command

```bash
adept loop add ci-triage --workflow
adept agent check ci-triage-reviewer   # lint the evaluator
adept sync                             # render skill + agent everywhere
```

This scaffolds:

- **`skills/ci-triage/SKILL.md`** — the discovery skill, structured Read → Judge → Write →
  Hand off → **Stop**. The Stop section is the boundary you keep for yourself ("Never merge.
  Never delete. Uncertain work goes to `./inbox/`, not into a PR") — it is the one part of a
  loop the system cannot infer. `activation: manual` keeps it from auto-firing outside the
  automation.
- **`agents/ci-triage-reviewer.md`** — the evaluator: assumes the work is broken until
  proven otherwise, verifies by *acting* (runs the tests, pastes real output), returns
  PASS/REJECT. Tuning a standalone skeptic is far more tractable than making a generator
  critical of its own work.
- **`.github/workflows/adept-loop-ci-triage.yml`** (with `--workflow`) — a cloud cron
  skeleton. Cloud scheduling runs with your machine off at a coarser interval; local
  schedulers (Claude Code `/loop`, Codex Automations) buy frequency and local-file access at
  the cost of keeping the machine on. Mature loops often use both.

## The checklist the scaffold prints

The first two elements decide whether the loop can run; the last four decide whether it gets
into trouble once it does:

1. **Discovery source** — what does it read on a timer? (edit the skill's Read/Judge sections)
2. **Evaluator** — an independent check that can say "no"
3. **State file** — `./state/<id>.md`, committed; the agent forgets, the repo does not
4. **Isolation** — one git worktree per parallel task
5. **Token cap** — per-run and daily budgets set *before* the first unattended run
6. **Human review** — PRs open, never auto-merge; the pause is permanent, not scaffolding

Four costs accrue silently in a running loop — verification debt, comprehension rot,
cognitive surrender, token blowout — and they reinforce each other. The guard for all four
is the same: keep a check inside the loop that can say "no", and stay the kind of engineer
who reads a sample of the loop's output every day.

Start small: one finding handled end-to-end, checks proven against real mistakes, then widen.
