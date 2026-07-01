# Canonical skills

The **canonical skill** is `adept`'s single source of truth. Everything else — the files in
`.claude/`, `.cursor/`, `AGENTS.md`, and so on — is *rendered output* derived from it.

## On disk

A canonical skill is a directory named by its id, containing a `SKILL.md` plus optional
sidecars:

```
<canonical>/pr-review/
├── SKILL.md
├── scripts/
├── references/
└── assets/
```

Where `<canonical>` lives depends on the project [layout](layouts.md):

- **Consumer layout** (default): `<root>/.adeptability/skills/`
- **Library layout** (`init --as-library`): `<root>/skills/`

## SKILL.md

`SKILL.md` is YAML frontmatter followed by a markdown body. `adept` also accepts a `skill.yaml`
sidecar for the frontmatter. Recognized frontmatter fields:

| Field | Purpose |
| --- | --- |
| `name` / `id` | The skill identity (validated id charset, ≤ 50 chars) |
| `description` | Used by agent-mode harnesses for progressive disclosure / activation |
| `activation` | `always` \| `globs` \| `agent` \| `manual` |
| `globs` | File globs that activate a `globs` skill |
| `allowed-tools` | Tool allowlist carried into Claude Code |
| `tags` | Free-form topic tags |

```markdown
---
name: pr-review
description: Use before opening a PR. Tests, security, performance.
activation: agent
allowed-tools: [Read, Grep]
tags: [review, quality]
---
# PR Review Checklist
...
```

The body is arbitrary markdown. Sidecar files (`scripts/`, `references/`, `assets/`) ship
alongside the body and are preserved by harnesses that support them.

## Identity is content, not version

A skill's identity is `(id, content-hash)`. There are **no version numbers** — the content
hash is the source of truth for "did this change." This is what powers the
[drift detection](drift.md): `adept` compares hashes of the canonical skill, the last-synced
base snapshot, and the rendered harness output to decide whether things are `synced`, `ahead`,
`behind`, or `diverged`.

## Validation

Every skill is validated against `pkg/adeptschema/skill.schema.json` on load. Invalid
frontmatter or an out-of-charset id fails fast with a clear error rather than producing broken
harness output.

## Resolution order

When `adept` builds the set of skills for a project, it resolves:

1. Project canonical skills (highest priority)
2. Skills from each configured [library](libraries.md), in order

A project-canonical skill **shadows** a library skill sharing the same id, and across multiple
libraries the first match wins. Use `adept skill list` to see the resolved set.
