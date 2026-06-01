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

## Quickstart

```bash
# 1. Initialize your local library and a project
adept init --library
cd ./my-project
adept init --project

# 2. Author a canonical skill
mkdir -p ~/.adeptability/skills/pr-review
cat > ~/.adeptability/skills/pr-review/skill.yaml <<'EOF'
id: pr-review
version: 1
description: Apply before opening a PR. Tests, security, performance.
activation: agent
EOF
cat > ~/.adeptability/skills/pr-review/SKILL.md <<'EOF'
# PR Review Checklist

- [ ] Tests added or updated
- [ ] No secrets in diff
- [ ] Public API changes documented
EOF
adept add ~/.adeptability/skills/pr-review

# 3. Install + sync to every harness the project uses
adept install pr-review
adept harness enable --id claude-code
adept harness enable --id cursor
adept harness enable --id codex
adept harness enable --id copilot
adept harness sync
```

You now have:

```
.claude/skills/pr-review/SKILL.md            # Claude Code format
.cursor/rules/pr-review.mdc                  # Cursor MDC with description
AGENTS.md                                    # Codex aggregated (within 32 KiB)
.github/instructions/always.instructions.md  # Copilot with applyTo
```

## Commands

| Command | Purpose |
|---|---|
| `adept init [--library\|--project]` | Initialize library or project |
| `adept add <path>` | Add a skill to the library |
| `adept list` | List skills (library or project, with `--project`) |
| `adept show <id>` | Show a skill's resolved canonical metadata |
| `adept install <id>` | Copy library skill into project |
| `adept uninstall <id>` | Remove skill from project |
| `adept pull <id>` | Pull library updates into project |
| `adept push <id>` | Push project edits back to library |
| `adept status` | Show sync state (synced/ahead/behind/diverged) |
| `adept diff <id>` | Show diff between project and library |
| `adept resolve <id> --strategy library\|project` | Resolve diverged skill |
| `adept harness list` | List built-in + registered harnesses |
| `adept harness enable --id <h>` | Enable a harness for this project |
| `adept harness disable --id <h>` | Disable a harness for this project |
| `adept harness status` | Per-harness drift report |
| `adept harness sync [--id <h>] [--force]` | Render skills into harness paths |
| `adept harness import --id <h>` | Adopt harness-side edits back into project canonical |
| `adept harness add --from <adapter.yaml>` | Register a config-driven harness |
| `adept render --id <skill> --harness <h>` | Debug: print what would be written |
| `adept apply-all --skills <ids> --to '<glob>'` | Fanout sync across many project checkouts |
| `adept org init --remote <url>` | Wire project to a centralized org library |
| `adept org sync` | Pull org-required + selected optional skills |
| `adept migrate import --from <dir>` | Import a prior `skillbook` library |
| `adept doctor [--library\|--project]` | Validate setup; exit 2 on dirty, 1 on broken |
| `adept verify` | Re-hash and verify signatures |
| `adept upgrade` | Self-upgrade to latest release |

All commands accept `--json` for machine output.

## Canonical Skill Format

A skill is a directory with `skill.yaml` + `SKILL.md` (and any sidecar `scripts/` `references/` `assets/`).

```yaml
# skill.yaml
id: pr-review                # ^[a-z0-9_][a-z0-9_-]{0,49}$
version: 3
description: Use before opening a PR. Tests, security, performance.
activation: agent            # always | globs | agent | manual
globs: []                    # required if activation=globs
allowed-tools: [Read, Grep]  # carried into Claude
targets: []                  # nil = all enabled harnesses
tags: [review, quality]
size-hint-kib: 4
metadata:
  owner: platform-eng
```

The schema is published at `pkg/adeptschema/skill.schema.json` and validated on every `add`/`scan`.

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

- **Library** at `~/.adeptability/skills/<id>/` (override with `$ADEPT_LIBRARY`). Plain directory by default; opt into git with `init --library --git`.
- **Project** at `<project>/.adeptability/skills/<id>/`. Lockfile `adeptability.lock.json` tracks `{version, hash, targets, signature?}` per skill.
- **Status state machine**: `synced | ahead | behind | diverged | local-only | library-only` derived from comparing canonical hashes.
- **Renderers** translate one canonical skill into harness-specific bytes. Aggregator renderers (Codex/Copilot) combine and enforce size budgets.
- **Adapters** are pluggable — built-in via Go code, or config-driven via YAML.
- **Signatures** (optional) via cosign keyless OIDC; stored in lockfile.

## Distribution

Pre-built binaries for `darwin/{arm64,amd64}`, `linux/{arm64,amd64}`, `windows/amd64` are published on every release via [goreleaser](https://goreleaser.com), signed with [cosign](https://github.com/sigstore/cosign), and accompanied by SLSA provenance.

## Development

```bash
git clone https://github.com/itaywol/adeptability
cd adeptability
go build ./...
go test -race ./...
```

Pre-commit gates: `go vet ./...`, `go test -race ./...`, ≥80% coverage on `internal/render`, `internal/status`, `internal/budget`, `internal/lockfile`, `internal/canonical`.

## License

MIT — see [LICENSE](./LICENSE).
