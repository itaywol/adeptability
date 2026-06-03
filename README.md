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

## Command surface

Five top-level verbs + three subcommand groups. Everything else folds into them.

```
adept init     [--from <url>] [--ref <branch>] [--name <local>] [--mode symlink|copy]
adept status
adept sync     [--harness <id>] [--force] [--dry-run]
adept sync-from [--harness <id>] [--all] [--force] [--dry-run]
adept diff     [--harness <id>]

adept harness  add <id> | remove <id> | list
adept skill    add <id> [--from <path>] [--edit]                       # local scaffold/import
               | install <owner>/<repo>[#ref]/<skill> [--yes] [--allow-unsafe]   # from skills.sh / GitHub
               | update [<id>]                                          # bump locked pin
               | info <slug>                                            # repo, stars, license, sha, installs
               | search <query>                                         # skills.sh search
               | check <target> [--format=table|markdown|json]          # static safety scan
               | edit <id> | remove <id> | list
adept library  add <name> --from <url> [--ref <branch>] | remove <name> [--purge] | list
adept config   list | get <key> | set <key> <value> | unset <key>
               | llm set <provider> [--model …] [--endpoint …] | llm unset | llm test
```

### Configurable keys (strict-typed)

| Key | Allowed | Default |
|---|---|---|
| `mode` | `symlink` \| `copy` | `symlink` |
| `scan.onInstall` | `true` \| `false` | on when an LLM provider is configured, off otherwise |
| `scan.blockSeverity` | `critical` \| `high` \| `medium` | `critical` |
| `llm.provider` | `anthropic` \| `ollama` | unset |
| `llm.model` | provider-specific (e.g. `claude-haiku-4-5`, `llama3.1`) | provider default |
| `llm.endpoint` | URL (mainly for self-hosted ollama) | provider default |

API keys are read from the environment at call time (`ANTHROPIC_API_KEY` for Anthropic). adept never stores secrets in `config.json`.

Global flags: `--json`, `--log-level debug|info|warn|error`, `--project <path>`, `--library <path>`.

Dynamic shell completion is shipped via cobra; run `adept completion zsh > "${fpath[1]}/_adept"` (or the equivalent for bash/fish) once and tab will fill in skill ids, harness ids, and library names.

## The four user flows

### 1. Empty project — author your own skills

```bash
cd ./my-project
adept init
adept skill add lint-style --edit       # scaffolds + opens $EDITOR
adept harness add claude-code
adept harness add cursor
adept sync
```

### 2. Existing project with harness skills on disk

```bash
cd ./my-project              # already has .claude/, .cursor/, AGENTS.md, etc.
adept init                   # auto-detects and adopts existing harness skills
adept status                 # confirm what got enabled
adept diff                   # confirm round-trip is clean
```

### 3. Clone a remote library and use its skills

```bash
adept init --from git@github.com:my-org/skills.git --name shared
adept harness add claude-code
adept sync                   # library skills + project canonical → harnesses
```

Authenticate via git's native chain — SSH agent, `~/.netrc`, or the configured `credential.helper` (gh CLI, osxkeychain, libsecret). `adept` never sees the secret; if git prompts, it prompts you directly.

### 4. Install a single skill from skills.sh

```bash
adept skill search find-skills              # query skills.sh
adept skill info  vercel-labs/skills/find-skills
adept skill install vercel-labs/skills/find-skills   # preview + Y/n
adept skill update                          # bump every locked external skill
```

`skill install` clones the upstream GitHub repo at a resolved SHA, extracts the requested skill directory only, writes it into `.adeptability/skills/<id>/`, and pins the upstream provenance in the lockfile (`source`, `slug`, `repo`, `ref`, `sha`, `skillPath`, `contentHash`, `installedAt`).

Before every install the CLI prints a preview (repo URL, SHA, stars, install count, license, file list) and runs a sandbox sniff over the SKILL.md body for known dangerous patterns (`curl ... | sh`, `rm -rf /`, `sudo`, secret references, base64 decode pipelines). Findings at or above `scan.blockSeverity` (default `critical`) hard-block the install unless `--allow-unsafe` is passed; lower-severity findings surface in the preview and require the usual y/N confirm. `--yes` skips that confirm but never bypasses the scan.

On every `adept sync`, locked external skills are re-hashed against `contentHash` — local edits surface as a warning with a pointer to `adept skill update <id>` or remove + re-install.

Non-GitHub catalog sources surfaced by skills.sh (e.g. `skills.volces.com`) are not installable yet; use `adept library add` against the upstream git URL instead.

### Safety scans (`adept skill check`)

```bash
adept skill check pr-review                               # project canonical
adept skill check library:default:pr-review               # library-resolved
adept skill check vercel-labs/skills/find-skills          # remote — fetches & discards
adept skill check getsentry/skills/skill-scanner --format=markdown
```

Phase 2.1 ships a static (regex / frontmatter-aware) scanner with structured `Finding` output. Categories mirror getsentry/skills/skill-scanner (`prompt-injection`, `malicious-code`, `excessive-permissions`, `secret-exposure`, `supply-chain`, `url-analysis`, `frontmatter`) and severities `critical / high / medium / low / clean`. Each finding carries `id`, `category`, `severity`, `confidence`, `location`, `issue`, `evidence`, `risk`, `remediation`.

Exit code matches the worst severity:

- `clean` / `low` → `0`
- `medium` → `0` (informational; still warned)
- `high` → `1` (non-zero so CI can branch)
- `critical` → `2` (same dirty-state code as `status` / `diff`)

`skill install` runs the same scanner before writing; **critical findings hard-block** the install unless `--allow-unsafe` is passed. Lower severities surface in the install preview and require the usual `y/N` confirm.

### LLM intent pass

`adept skill check` and `adept skill install` automatically run an LLM intent pass on top of the static report when a provider is configured:

```bash
export ANTHROPIC_API_KEY=sk-...
adept config llm set anthropic --model claude-haiku-4-5-20251001
adept config llm test                        # health-pings the provider
adept skill check vercel-labs/skills/find-skills    # static + LLM merged
adept skill check ... --no-llm               # opt out per-call
adept skill check ... --llm                  # error out if not configured
```

The reviewer reads each static finding, drops false positives, and adds intent-level findings the regex layer cannot catch (politely-worded jailbreaks, multi-step exfiltration, polyglot payloads). LLM-originated findings get `SKILL-LLM-NNN` ids and otherwise share the `Finding` shape so renderers and exit-code mapping are unchanged.

`scan.onInstall=true` makes the install gate consult the merged report; `scan.blockSeverity` (default `critical`) sets the threshold that hard-blocks the install. Provider failures (no key, unreachable Ollama, parsing error) degrade gracefully — install continues with the static-only report and a warning on stderr.

Providers:

- **Anthropic** — `ANTHROPIC_API_KEY` env var. Default model `claude-haiku-4-5-20251001`. Override with `--model` at config time.
- **Ollama** — local, no key. Default endpoint `http://127.0.0.1:11434`, default model `llama3.1`. Override with `--endpoint` for self-hosted instances.

Adding a new provider = implement `llm.Provider` (4 methods) + register in `internal/cli/deps.go`.

### 5. Stack multiple libraries (Model B union)

```bash
adept library add core   --from git@github.com:my-org/core-skills.git
adept library add team-a --from git@github.com:my-org/team-a.git
adept library list

# Resolution order: project canonical first, then each library in
# `library list` order. First-wins on cross-library collisions; the
# shadowed copy is reported via `adept status` and `adept skill list`.
adept skill list
```

Override a library skill locally:

```bash
adept skill add my-shared-skill              # creates project canonical;
                                             # automatically shadows the library copy
adept sync
```

Reload changes from upstream:

```bash
cd $ADEPT_LIBRARY/libs/core && git pull      # or:
adept library add core --from <same-url>     # re-clones / fetches
```

## Round-trip example

```bash
# bootstrap
adept init --from git@github.com:my-org/skills.git
adept harness add claude-code cursor
adept sync

# someone edits .cursor/rules/lint.mdc by hand
adept diff --harness cursor            # → drift detected, exit 2
adept sync-from --harness cursor       # → canonical adopts the edit
adept sync                             # → re-publish to every harness
adept diff                             # → clean
```

## Canonical Skill Format

A skill is a directory under `.adeptability/skills/<id>/` containing a single `SKILL.md` (with YAML frontmatter) and any sidecar files the per-skill harnesses should carry along (`scripts/`, `references/`, `assets/`, …).

```markdown
---
id: pr-review                  # ^[a-z0-9](?:[a-z0-9-]{0,48}[a-z0-9])?$
description: Use before opening a PR. Tests, security, performance.
activation: agent              # always | globs | agent | manual
globs: []                      # required if activation=globs
allowed-tools: [Read, Grep]    # carried into Claude
targets: []                    # nil = all enabled harnesses
tags: [review, quality]
metadata:
  owner: platform-eng
model: claude-opus-4-8         # optional model hint (Claude consumes it; others ignore)
harness:                       # optional per-harness, per-skill overrides
  claude-code:
    effort: high
    user-invocable: false
  cursor:
    alwaysApply: false
---
# PR Review Checklist

- [ ] Tests added or updated
- [ ] No secrets in diff
- [ ] Public API changes documented
```

The schema is published at `pkg/adeptschema/skill.schema.json` and validated on every load.

### Per-skill, per-harness configuration

Most skills need none of this. When a harness has a knob with no canonical
equivalent — Claude's `effort`/`user-invocable`/`model`, a Cursor-only
`alwaysApply` — set it once in the `harness:` block keyed by harness id. Each
renderer merges its own entry **last** over the fields it derives from the
canonical skill (last-wins; new keys appended), and a harness simply ignores
keys it does not understand, so the override degrades safely. The block is
schema-validated (it cannot override the identity fields `id`/`name`/
`description`) and round-trips through the canonical writer. `model` is
promoted to a top-level field because it is the one knob commonly wanted with
no analog elsewhere. Today Claude Code and Cursor consume overrides.

## Harness Support

### Specialized adapters

These harnesses ship richer renderers (sidecar handling, glob translation, aggregation, size budgets):

| Harness | Output | Format | Activation translation |
|---|---|---|---|
| Claude Code | `.claude/skills/<id>/SKILL.md` | per-skill, full sidecars | `description` drives agent decision; `allowed-tools` carried; `manual` → `disable-model-invocation` |
| Cursor | `.cursor/rules/<id>.mdc` | per-skill, single file | `always`→`alwaysApply:true`, `globs`→`globs:[…]`, `agent`→`description:` only |
| OpenCode | `.opencode/skill/<id>/SKILL.md` | per-skill, full sidecars | `# id` heading + description + body |
| Codex | `AGENTS.md` | aggregated single file, 32 KiB cap | sections with markers; lowest-priority skills dropped first under budget |
| GitHub Copilot | `.github/instructions/<bucket>.instructions.md` | aggregated per-glob | `always` and matching glob sets bucket together |

### Generic per-skill adapters (vercel-labs/skills agent set)

Every harness in the [vercel-labs/skills supported-agents matrix](https://github.com/vercel-labs/skills#supported-agents) is registered out of the box via the generic per-skill adapter. The adapter writes `SKILL.md` (with `name`/`description`/`allowed-tools`) plus any sidecars, and reverse-imports the same shape.

| `--harness` id | Output |
|---|---|
| `aider-desk` | `.aider-desk/skills/<id>/SKILL.md` |
| `amp`, `kimi-cli`, `replit`, `universal` | `.agents/skills/<id>/SKILL.md` |
| `antigravity` | `.agents/skills/<id>/SKILL.md` |
| `augment` | `.augment/skills/<id>/SKILL.md` |
| `bob` | `.bob/skills/<id>/SKILL.md` |
| `openclaw` | `skills/<id>/SKILL.md` |
| `cline`, `dexto`, `warp` | `.agents/skills/<id>/SKILL.md` |
| `codearts-agent` | `.codeartsdoer/skills/<id>/SKILL.md` |
| `codebuddy` | `.codebuddy/skills/<id>/SKILL.md` |
| `codemaker` | `.codemaker/skills/<id>/SKILL.md` |
| `codestudio` | `.codestudio/skills/<id>/SKILL.md` |
| `command-code` | `.commandcode/skills/<id>/SKILL.md` |
| `continue` | `.continue/skills/<id>/SKILL.md` |
| `cortex` | `.cortex/skills/<id>/SKILL.md` |
| `crush` | `.crush/skills/<id>/SKILL.md` |
| `deepagents`, `firebender`, `gemini-cli`, `github-copilot` | `.agents/skills/<id>/SKILL.md` |
| `devin` | `.devin/skills/<id>/SKILL.md` |
| `droid` | `.factory/skills/<id>/SKILL.md` |
| `forgecode` | `.forge/skills/<id>/SKILL.md` |
| `goose` | `.goose/skills/<id>/SKILL.md` |
| `hermes-agent` | `.hermes/skills/<id>/SKILL.md` |
| `junie` | `.junie/skills/<id>/SKILL.md` |
| `iflow-cli` | `.iflow/skills/<id>/SKILL.md` |
| `kilo` | `.kilocode/skills/<id>/SKILL.md` |
| `kiro-cli` | `.kiro/skills/<id>/SKILL.md` |
| `kode` | `.kode/skills/<id>/SKILL.md` |
| `mcpjam` | `.mcpjam/skills/<id>/SKILL.md` |
| `mistral-vibe` | `.vibe/skills/<id>/SKILL.md` |
| `mux` | `.mux/skills/<id>/SKILL.md` |
| `openhands` | `.openhands/skills/<id>/SKILL.md` |
| `pi` | `.pi/skills/<id>/SKILL.md` |
| `qoder` | `.qoder/skills/<id>/SKILL.md` |
| `qwen-code` | `.qwen/skills/<id>/SKILL.md` |
| `rovodev` | `.rovodev/skills/<id>/SKILL.md` |
| `roo` | `.roo/skills/<id>/SKILL.md` |
| `tabnine-cli` | `.tabnine/agent/skills/<id>/SKILL.md` |
| `trae`, `trae-cn` | `.trae/skills/<id>/SKILL.md` |
| `windsurf` | `.windsurf/skills/<id>/SKILL.md` |
| `zencoder` | `.zencoder/skills/<id>/SKILL.md` |
| `neovate` | `.neovate/skills/<id>/SKILL.md` |
| `pochi` | `.pochi/skills/<id>/SKILL.md` |
| `adal` | `.adal/skills/<id>/SKILL.md` |

Run `adept harness list` for the live registry. Multiple agents sharing the same on-disk path (e.g. the `.agents/skills/` cohort) are registered as distinct ids so you can opt-in by name; their writes are idempotent.

Need yet another harness? Drop a YAML adapter file in `$ADEPT_LIBRARY/libs/<name>/adapters/` and it loads at startup — no recompile.

## Architecture

- **Library** at `$ADEPT_LIBRARY/libs/<name>/skills/<id>/` (root default: `$HOME/.adeptability`). Stack as many libraries as you want; each is a git clone (or local path) registered via `library add`.
- **Project** at `<project>/.adeptability/skills/<id>/`. Last-synced snapshots live at `<project>/.adeptability/base/<id>/` — together with the live library they feed the 3-way drift detector.
- **Resolution** (Model B union): project canonical first, then each library in config order. Project shadows library; first-wins on cross-library collisions. Warnings surface in `adept status` and `adept skill list`.
- **Status state machine**: `synced | ahead | behind | diverged | local-only | library-only` derived purely from hashing the relevant directories on demand. No lockfile.
- **Renderers** translate one canonical skill into harness-specific bytes. Aggregator renderers (Codex/Copilot) combine and enforce size budgets.
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
