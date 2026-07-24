---
title: "Your first skill"
description: "Build your first canonical skill from scratch and render it into every harness."
---

A skill is a directory containing one `SKILL.md` (or `skill.yaml` + body) and any optional
sidecars. This page walks through creating one from scratch.

## Anatomy of a skill

```
.adeptability/skills/pr-review/
├── SKILL.md          # frontmatter + markdown body (required)
├── scripts/          # optional sidecar: helper scripts
├── references/       # optional sidecar: reference docs
└── assets/           # optional sidecar: images, data files
```

`SKILL.md` is YAML frontmatter followed by the markdown body:

```markdown
---
id: pr-review
description: Use before opening a PR. Tests, security, performance.
activation: agent              # always | globs | agent | manual
allowed-tools: [Read, Grep]    # carried into Claude Code
tags: [review, quality]
---
# PR Review Checklist

- [ ] Tests added or updated
- [ ] No secrets in the diff
- [ ] Public API changes documented
```

The schema lives at `pkg/adeptschema/skill.schema.json` and is validated on every load.

:::note[`id` vs `name`]
The canonical frontmatter key is `id`; `name` is accepted as an alias and mapped to `id`
on load. Skill **ids** are validated against
`^[a-z0-9](?:[a-z0-9-]{0,48}[a-z0-9])?$` — lowercase ASCII letters, digits, and internal
hyphens, max 50 chars. This is the lowest common denominator across harnesses, so a valid
id always renders a valid harness name and directory.
:::

## Activation modes

`activation` tells each harness *when* to surface the skill. `adept` translates it per
harness — see the [Harness Comparison](/docs/harness-comparison/) for the full table.

| Mode | Meaning |
| --- | --- |
| `always` | Always in context |
| `globs` | Active when the current files match the skill's globs |
| `agent` | The agent decides based on `description` (progressive disclosure) |
| `manual` | Only when explicitly invoked |

## Create it

```bash
adept skill add pr-review --edit
```

`--edit` opens the scaffolded `SKILL.md` in `$EDITOR`. To import an existing skill directory
instead of scaffolding a blank one:

```bash
adept skill add pr-review --from ./path/to/existing-skill
```

## Render and inspect

```bash
adept harness add claude-code
adept sync
cat .claude/skills/pr-review/SKILL.md
```

## List what's resolved

```bash
adept skill list                # project canonical + skills resolved from libraries
adept skill list --project-only # only skills in the project canonical
```

## Related

- [Authoring a skill (guide)](/docs/guides/authoring/) — writing effective, portable skills.
- [Canonical skills (concept)](/docs/concepts/canonical-skills/) — the data model in depth.
