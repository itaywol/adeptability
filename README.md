# adeptability

The cross-harness skill portability CLI for AI coding assistants.

One canonical library. Every tool gets the right format. Automatically.

## Why

Every AI coding assistant — Claude Code, Cursor, GitHub Copilot, OpenAI Codex, OpenCode — uses a different on-disk format for the rules and skills it loads:

- Claude Code expects `.claude/skills/<id>/SKILL.md` with `name`/`description` frontmatter.
- Cursor expects `.cursor/rules/<id>.mdc` with `description`/`globs`/`alwaysApply`.
- Copilot expects `.github/instructions/*.instructions.md` with `applyTo` globs.
- Codex expects a single hierarchical `AGENTS.md` (no per-skill concept) with a 32 KiB cap.
- OpenCode expects `.opencode/skill/<id>/SKILL.md` (and accepts `AGENTS.md` / `CLAUDE.md` fallbacks).

Maintaining the same procedural knowledge across all of these by hand is impossible to keep consistent. Copying one file into all five paths breaks activation in at least three of them — the frontmatter schemas are mutually incompatible.

`adept` lets you author a skill once in a canonical format, then renders it accurately for every harness in your project — proper frontmatter, proper activation rules, proper aggregation, proper size budgets.

## Install

```bash
# macOS / Linux
brew install itaywol/tap/adeptability

# Windows
scoop install adeptability
# or
winget install itaywol.adeptability

# Any platform (curl)
curl -fsSL https://itaywol.github.io/adeptability/install.sh | sh

# Node ecosystems
npm install -g @itaywol/adeptability

# Containers
docker run --rm -v "$PWD:/work" -w /work ghcr.io/itaywol/adeptability:latest --help
```

## Quickstart — three flows, four verbs

The whole surface is `init`, `sync`, `sync-from`, `diff` (plus `list`, `show`, `doctor` for inspection). Pick whichever flow matches your starting point.

### 1. Empty project

```bash
cd ./my-project
adept init                # creates .adeptability/{skills,base}/ + config.json
# author a canonical skill directly under .adeptability/skills/<id>/
adept sync                # renders to every enabled harness
```

### 2. Project that already has harness skills on disk

```bash
cd ./my-project           # has .claude/, .cursor/, AGENTS.md, etc. already
adept init                # auto-adopts existing skills, enables the matching harnesses
adept diff                # confirm round-trip
adept sync                # publish back out
```

### 3. Clone a remote library

```bash
cd ./my-project
adept init --from git@github.com:my-org/skills.git --ref main
# library is cloned into $ADEPT_LIBRARY (default ~/.adeptability)
# remote URL is persisted; future `adept` runs don't need --from again
adept sync                # renders the org skills into your harnesses
```

The `--mode symlink|copy` flag on `init` picks the global materialization strategy. `symlink` is the default; `copy` is forced automatically on filesystems that reject symlinks (e.g. some NTFS shares).

## Commands

| Command | Purpose |
|---|---|
| `adept init [--from <url>] [--ref <branch>] [--mode symlink\|copy]` | Initialize project. Optionally clone a remote library and/or auto-adopt pre-existing harness skills. |
| `adept sync [--harness <id>] [--force] [--dry-run]` | Render canonical skills → every enabled harness. The primary "publish" verb. |
| `adept sync-from [--harness <id>] [--all] [--force] [--dry-run]` | Adopt harness-side edits back into canonical. Interactive prompt with no flags. |
| `adept diff [--harness <id>]` | Per-harness drift report. Exit 2 when any drift is present. |
| `adept list [--from-library]` | List project (default) or library skills. |
| `adept show <id> [--from-library]` | Resolved skill metadata. |
| `adept doctor` | Validate setup; exit 2 on issues. |

Every command accepts `--json` for machine-readable output, `--log-level debug|info|warn|error`, `--project <path>`, and `--library <path>`.

## Round-trip example

```bash
# bootstrap
adept init --from git@github.com:my-org/skills.git
adept sync

# someone edits .cursor/rules/lint.mdc by hand
adept diff --harness cursor          # → drift detected, exit 2
adept sync-from --harness cursor     # → canonical adopts the edit
adept sync                           # → re-publish to every harness
adept diff                           # → clean
```

## Canonical Skill Format

A skill is a directory under `.adeptability/skills/<id>/` containing a single `SKILL.md` (with YAML frontmatter) and any sidecar files the per-skill harnesses should carry along (`scripts/`, `references/`, `assets/`, …).

```markdown
---
id: pr-review                  # ^[a-z0-9_][a-z0-9_-]{0,49}$
description: Use before opening a PR. Tests, security, performance.
activation: agent              # always | globs | agent | manual
globs: []                      # required if activation=globs
allowed-tools: [Read, Grep]    # carried into Claude
targets: []                    # nil = all enabled harnesses
tags: [review, quality]
metadata:
  owner: platform-eng
---
# PR Review Checklist

- [ ] Tests added or updated
- [ ] No secrets in diff
- [ ] Public API changes documented
```

The schema is published at `pkg/adeptschema/skill.schema.json` and validated on every load.

## Harness Support

| Harness | Output | Format | Activation translation |
|---|---|---|---|
| Claude Code | `.claude/skills/<id>/SKILL.md` | per-skill, full sidecars | `description` drives agent decision; `allowed-tools` carried; `manual` → `disable-model-invocation` |
| Cursor | `.cursor/rules/<id>.mdc` | per-skill, single file | `always`→`alwaysApply:true`, `globs`→`globs:[…]`, `agent`→`description:` only |
| OpenCode | `.opencode/skill/<id>/SKILL.md` | per-skill, full sidecars | body only |
| Codex | `AGENTS.md` | aggregated single file, 32 KiB cap | sections with markers; oldest/largest dropped first under budget |
| GitHub Copilot | `.github/instructions/<bucket>.instructions.md` | aggregated per-glob | `always` and matching glob sets bucket together |

Need another harness? Drop a YAML adapter file in `~/.adeptability/adapters/` and it loads at startup — no recompile.

## Architecture

- **Library** at `~/.adeptability/skills/<id>/` (override with `$ADEPT_LIBRARY`). A plain directory by default; gets cloned in via `init --from <git-url>`.
- **Project** at `<project>/.adeptability/skills/<id>/`. Last-synced snapshots live at `<project>/.adeptability/base/<id>/` — together with the live library they feed the 3-way drift detector.
- **Status state machine**: `synced | ahead | behind | diverged | local-only | library-only` derived purely from hashing the three directories on demand. No lockfile.
- **Renderers** translate one canonical skill into harness-specific bytes. Aggregator renderers (Codex/Copilot) combine and enforce size budgets.
- **Adapters** are pluggable — built-in via Go code, or config-driven via YAML.
- **Mode** (`symlink` or `copy`) is project-wide and stored in `config.json`.

## Distribution

Pre-built binaries for `darwin/{arm64,amd64}`, `linux/{arm64,amd64}`, `windows/amd64` are published on every release via [goreleaser](https://goreleaser.com), signed with [cosign](https://github.com/sigstore/cosign), and accompanied by SLSA provenance.

## Development

```bash
git clone https://github.com/itaywol/adeptability
cd adeptability
go build ./...
go test -race ./...
```

Pre-commit gates: `go vet ./...`, `go test -race ./...`, ≥80% coverage on `internal/render`, `internal/status`, `internal/budget`, `internal/canonical`.

## License

MIT — see [LICENSE](./LICENSE).
