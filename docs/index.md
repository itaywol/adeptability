# adeptability

**Write an AI coding skill once, run it in every AI coding assistant.**

`adept` is a small Go CLI that lets you author an AI agent skill — a prompt, rule, or
procedure — **once**, in a single canonical format, then render it accurately into Claude
Code, Cursor, GitHub Copilot, OpenAI Codex, OpenCode, and 50+ other AI coding agents.

Every agent loads skills from a different path with a different schema
(`.claude/skills/<id>/SKILL.md`, `.cursor/rules/<id>.mdc`, a single `AGENTS.md` for Codex,
`.github/instructions/*.instructions.md` for Copilot, …). Keeping the same knowledge
consistent across all of them by hand drifts fast. `adept` keeps one source of truth and
handles the per-harness frontmatter, activation rules, aggregation, and size budgets.

Think of it as **dotfiles for your AI coding agents**.

## Why adept

- **One skill, every format.** No copy-pasting incompatible frontmatter into five paths.
- **Drift-proof.** Edit a skill in any harness, pull it back to canonical, re-publish. A
  hash-based 3-way detector flags divergence via `adept status` and `adept diff`.
- **Package-manager libraries.** Share a versioned set of skills across a team by reference —
  the repo commits a pointer, not the bytes.
- **Safe installs.** A static + optional-LLM scanner checks skills from `skills.sh` / GitHub
  before they land; critical findings hard-block by default.
- **Signed & reproducible.** Content-hashed skills, pinned upstream provenance, cosign-signed
  release binaries.

## Where to next

<div class="grid cards" markdown>

- :material-download: **[Install](getting-started/install.md)** — Go, Homebrew, curl, Docker.
- :material-rocket-launch: **[Quickstart](getting-started/quickstart.md)** — author, sync, and see it in your harness in 60 seconds.
- :material-lightbulb-on: **[Concepts](concepts/canonical-skills.md)** — canonical skills, harnesses, libraries, layouts, drift.
- :material-console: **[Command reference](reference/commands.md)** — every command and flag.

</div>

## The core loop

```bash
adept init                          # scaffold, adopt existing harness files, seed default skills
adept skill add lint-style --edit   # author a canonical skill
adept harness add claude-code
adept harness add cursor
adept sync                          # render canonical → every enabled harness
adept status                        # init, libraries, harnesses, and drift at a glance
```

Author in one place, `adept sync`, and each agent gets its own correct format.
