# Authoring a skill

This guide covers writing a canonical skill that renders well across every harness.

## Scaffold

```bash
adept skill add pr-review --edit
```

`add` creates `.adeptability/skills/pr-review/SKILL.md` and `--edit` opens it in `$EDITOR`.
To bring in an existing directory instead of a blank scaffold:

```bash
adept skill add pr-review --from ./legacy/pr-review
```

## Write the frontmatter

```markdown
---
name: pr-review
description: Use before opening a PR. Checks tests, security, and performance.
activation: agent
allowed-tools: [Read, Grep]
tags: [review, quality]
---
```

Guidelines that pay off across harnesses:

- **Write a strong `description`.** Agent-mode harnesses (Claude Code, Cursor "Agent
  Requested") activate a skill by matching the current task against its `description`. Make it
  a crisp *"Use when…"* sentence. A vague description means the skill never fires.
- **Pick the right `activation`:**
    - `always` — foundational rules you always want in context.
    - `globs` — language- or path-specific rules (set `globs: ["**/*.ts"]`).
    - `agent` — the agent decides; the common default.
    - `manual` — only when explicitly invoked.
- **Keep it lean.** Codex aggregates all skills into a single `AGENTS.md` with a **32 KiB
  cap**; oversized skills get dropped lowest-priority-first. Split large skills.
- **Use `allowed-tools`** to scope what Claude Code may do with the skill.

## Add sidecars (optional)

```
.adeptability/skills/pr-review/
├── SKILL.md
├── scripts/check-secrets.sh
└── references/security-checklist.md
```

Claude Code and OpenCode preserve the full sidecar tree. Single-file / aggregate harnesses
(Cursor, Codex, Copilot) drop sidecars and record a warning — keep the essential guidance in
the body itself.

## Render and verify

```bash
adept harness add claude-code
adept harness add cursor
adept sync
adept diff            # confirm clean
```

Inspect the rendered output to confirm the frontmatter translated correctly:

```bash
cat .claude/skills/pr-review/SKILL.md
cat .cursor/rules/pr-review.mdc
```

The [Harness Comparison](../harness-comparison.md) shows the exact translation per activation
mode.

## Iterate safely

If you tweak the skill inside a harness file while testing, don't lose it — pull it back:

```bash
adept sync-from       # adopt the harness edit into canonical
adept sync            # re-publish everywhere
```

## Round-trip check

A good skill round-trips cleanly: `sync` then `sync-from` should produce no changes. Run
`adept diff` after each to confirm the canonical ⇄ harness translation is stable.
