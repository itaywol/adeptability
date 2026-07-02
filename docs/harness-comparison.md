# Harness comparison

A single canonical skill produces correct output for every harness — proper frontmatter
schema, proper activation, proper aggregation, proper size budgets. This page shows what gets
written and why.

## Canonical input

```yaml
# skill.yaml
id: pr-review
description: Apply before opening a PR. Tests, security, performance.
activation: agent
allowed-tools: [Read, Grep]
```

```markdown
# SKILL.md
## Tests
- [ ] Unit tests added
## Security
- [ ] No secrets in diff
```

## Rendered output per harness

=== "Claude Code"

    `.claude/skills/pr-review/SKILL.md`

    ```markdown
    ---
    name: pr-review
    description: Apply before opening a PR. Tests, security, performance.
    allowed-tools: [Read, Grep]
    ---

    ## Tests
    - [ ] Unit tests added
    ## Security
    - [ ] No secrets in diff
    ```

    **Why correct:** Claude Code's progressive disclosure relies on `name` + `description` for
    activation; `allowed-tools` enforces the security boundary.

=== "Cursor"

    `.cursor/rules/pr-review.mdc`

    ```markdown
    ---
    description: Apply before opening a PR. Tests, security, performance.
    ---

    ## Tests
    - [ ] Unit tests added
    ## Security
    - [ ] No secrets in diff
    ```

    **Why correct:** Cursor's "Agent Requested" mode activates rules whose `description`
    matches the current task — description-only frontmatter, no `alwaysApply`. (`always`
    skills get `alwaysApply: true`; `globs` skills get `globs:` + `alwaysApply: false`.)
    Verbatim Claude frontmatter would leave `description` valid but add unrecognized
    `name`/`allowed-tools` keys.

=== "OpenCode"

    `.opencode/skill/pr-review/SKILL.md`

    ```markdown
    # pr-review

    Apply before opening a PR. Tests, security, performance.

    ## Tests
    - [ ] Unit tests added
    ## Security
    - [ ] No secrets in diff
    ```

    **Why correct:** OpenCode doesn't require frontmatter and prefers narrative markdown.

=== "Codex"

    `AGENTS.md` (aggregated)

    ```markdown
    <!-- adeptability:begin id=pr-review hash=a1b2c3d4 -->
    ## Apply before opening a PR. Tests, security, performance.

    ## Tests
    - [ ] Unit tests added
    ## Security
    - [ ] No secrets in diff
    <!-- adeptability:end id=pr-review -->
    ```

    When multiple skills are enabled, they are packed by priority descending then byte size
    ascending, emitted in skill-id order. If the total exceeds **32 KiB** (Codex's
    `project_doc_max_bytes`), the lowest-priority skills drop and a truncation manifest appears
    at the top:

    ```markdown
    <!-- adeptability: omitted 2 skill(s) due to 32KiB budget. Trim or split skills to fit. Dropped: legacy-style,old-runbook -->
    ```

    **Why correct:** Codex has *no per-skill concept*. It reads one or more `AGENTS.md` files
    walking up from cwd. Naive copy-as-`SKILL.md` is invisible to Codex; the 32 KiB cap means
    quiet truncation if you don't manage it.

=== "GitHub Copilot"

    Copilot has no agent-requested mode, so this `activation: agent` skill is **skipped**
    (see the activation table below). A skill with `activation: always` renders into
    `.github/instructions/always.instructions.md`:

    ```markdown
    ---
    applyTo: "**"
    ---

    <!-- adeptability:begin id=pr-review hash=a1b2c3d4 -->
    ## Apply before opening a PR. Tests, security, performance.

    ## Tests
    - [ ] Unit tests added
    ## Security
    - [ ] No secrets in diff
    <!-- adeptability:end id=pr-review -->
    ```

    Skills with `activation: globs` bucket into a per-glob file like
    `.github/instructions/bucket-a63d8819677675ea.instructions.md` with
    `applyTo: "**/*.ts,**/*.tsx"`.

    **Why correct:** Copilot uses `applyTo` glob match to decide when to inject the
    instructions. Without it, the file is loaded for the wrong files or ignored entirely.

## Activation translation table

| Canonical activation | Claude | Cursor | OpenCode | Codex | Copilot |
| --- | --- | --- | --- | --- | --- |
| `always` | description-only | `alwaysApply: true` | `# id` + desc + body | aggregated body | `applyTo: "**"` |
| `globs: [...]` | desc + glob hint | `globs:` + `alwaysApply:false` | `# id` + desc + body | aggregated body | bucketed by globs |
| `agent` | description-only | description-only | `# id` + desc + body | aggregated body | *skipped* (no agent mode) |
| `manual` | `disable-model-invocation:true` | desc + `@id` hint | `# id` + desc + body | aggregated body | *skipped* |

## Sidecars

Skills can bundle `scripts/`, `references/`, `assets/`. Where supported:

- **Claude Code:** full sidecar tree preserved.
- **OpenCode:** full sidecar tree preserved.
- **Cursor:** dropped (single-file); a warning is reported on `sync`.
- **Codex:** dropped (aggregator file model); a warning is reported on `sync`.
- **Copilot:** dropped (aggregator file model); a warning is reported on `sync`.

## Beyond the specialized five

Everything above describes the five **specialized renderers**. 50+ more agents (Windsurf,
Gemini CLI, Cline, Continue, Roo, Goose, Junie, …) render through generic per-skill adapters
that write `SKILL.md` into that agent's convention. Run `adept harness list` for the live
registry, or add your own with a [config-driven adapter](concepts/harnesses.md#config-driven-adapters-no-recompile).

## Why this matters

Prior art treated multi-harness sync as a path-multiplexing problem (write one file into N
paths). That breaks activation in Cursor and Copilot, produces nothing usable in Codex, and
silently loses size budget. `adept` makes content fidelity to each harness's actual loader the
headline guarantee — and proves it with golden files any contributor can inspect.
