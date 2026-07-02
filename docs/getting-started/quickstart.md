# Quickstart

Author a skill once and watch it materialize into every enabled harness in about a minute.

## 1. Initialize a project

```bash
cd ./my-project
adept init
```

This creates `.adeptability/{skills,base}/`, writes `.adeptability/config.json`, and — if it
finds any pre-existing harness files (`.claude/`, `.cursor/`, `.opencode/`, `AGENTS.md`,
`.github/instructions/`) — **adopts them into canonical** and records the matching harness
ids. It also seeds four bundled default skills:

- `using-adept` — how to drive the CLI
- `authoring-adept-skills` — how to write a good, portable skill
- `adept-self-improve` — capture a session lesson as a skill, then sync it everywhere
- `expertise-exchange` — ask teammates for expertise via the `adept exchange` board

Skip the seeds with `adept init --no-default-skills`.

## 2. Author a skill

```bash
adept skill add lint-style --edit
```

`add` scaffolds `.adeptability/skills/lint-style/SKILL.md`; `--edit` opens it in `$EDITOR`.
A skill is YAML frontmatter plus a markdown body:

```markdown
---
id: lint-style
description: Apply when writing or reviewing code. Enforce the house lint rules.
activation: agent
allowed-tools: [Read, Grep]
---
# House style

- Prefer early returns over nested conditionals.
- No unused imports.
```

## 3. Enable harnesses

```bash
adept harness add claude-code
adept harness add cursor
adept harness list        # see every registered harness + enabled state
```

## 4. Sync

```bash
adept sync
```

`adept` renders the canonical skill into each enabled harness's native format:

```
.claude/skills/lint-style/SKILL.md      # Claude Code
.cursor/rules/lint-style.mdc            # Cursor (frontmatter translated per activation mode)
```

## 5. Check state

```bash
adept status
```

```
adept: initialized at /home/you/my-project (mode=symlink)
library root: /home/you/.adeptability
libraries: (none — project-only mode)
skills: 5 in project canonical, 0 resolved from libraries
harnesses:
  ID           ENABLED  SYNCED  DRIFTED  MISSING  CONFLICT
  claude-code  yes      5       0        0        0
  cursor       yes      5       0        0        0
```

`status` exits `2` when anything is out of sync, so scripts and CI can branch on it.

## The reverse direction

Edited a skill directly inside a harness file? Pull it back to canonical, then re-publish:

```bash
adept sync-from          # adopt harness-side edits into canonical (interactive)
adept sync               # re-render canonical → every harness
```

## Next

- [Author a robust, portable skill](../guides/authoring.md)
- [Share a library across your team](../guides/sharing-a-library.md)
- [Full command reference](../reference/commands.md)
