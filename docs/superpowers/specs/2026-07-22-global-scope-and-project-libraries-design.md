# Design: Global scope harness management + project-scoped libraries by default

Date: 2026-07-22
Status: Approved (design review with itay)

## Problem

1. `adept sync` only renders skills into project-relative harness paths. There is no way for
   adept to manage home-level harness config (`~/.claude/skills/`, `~/.codex/AGENTS.md`), so
   skills a user wants in *every* session cannot be managed by adept at all.
2. Library clones always land in the machine-wide store (`~/.adeptability/libs/`). Projects
   reference shared mutable state, are not self-contained, and the only workaround
   (`ADEPT_LIBRARY=./path`) redirects the entire store per-invocation and persists nothing.

Goal: libraries install **into the project by default**; a **global scope** exists as an
explicit opt-in that renders skills into real home-level harness locations.

## Decision summary

- Global scope is a real scope: own config, own enabled harnesses, renders to home-level
  harness dirs. (Not just a shared cache.)
- Project-scoped library clones live at `<project>/.adeptability/libs/<name>/`, gitignored by
  default. Vendoring = remove the ignore entry and commit.
- CLI: `--global` persistent flag on existing verbs. Outside a project root, commands default
  to global scope with a one-line notice.
- v1 global-capable harnesses: those with real home config locations (claude-code, codex,
  opencode). Others error at `harness add --global`.
- Migration: resolution auto-falls back to the machine store when a project-local clone is
  missing (warn + hint); `adept migrate` re-clones into the project.

## 1. Scope model

Two scopes, both backed by the existing `project.Project` abstraction (approach A: global
scope is a second Project instance — maximum reuse of orchestrator, staging, drift, status).

|                  | Project scope                      | Global scope                          |
| ---------------- | ---------------------------------- | ------------------------------------- |
| Root             | `<repo>/.adeptability/`            | `$ADEPT_LIBRARY` (default `~/.adeptability/`) |
| Config           | `.adeptability/config.json`        | `~/.adeptability/config.json` (new)   |
| Canonical skills | `.adeptability/skills/`            | `~/.adeptability/skills/` (new)       |
| Libraries        | `.adeptability/libs/` (new default)| `~/.adeptability/libs/` (as today)    |
| Base / staging   | as today                           | `~/.adeptability/{base,staging}/` (new) |
| Render target root | project root                     | `$HOME` (adapter global templates)    |

Scope resolution per invocation:

1. `--global` → global scope.
2. Else walk up from CWD for a project root (unchanged).
3. Else global scope, with a one-line notice.

Existing `~/.adeptability` contents (`libs/`, `adapters/`, `exchange/`) coexist untouched;
global scope adds `config.json`, `skills/`, `base/`, `staging/` alongside. First global-scope
use bootstraps the global config automatically — no separate bootstrap step.

No per-library `scope` config field. Scope is determined by which config declares the
library; clone location follows. No config schema bump.

## 2. Library scope default flip

- `adept library add` in project scope clones to `<project>/.adeptability/libs/<name>/` and
  writes the existing `LibraryRef` shape (`name`/`remote`/`ref`) to the project config.
- `.adeptability/.gitignore` is auto-managed to ignore `libs/` and `staging/`. Vendoring a
  library is now: delete the ignore line, commit. `docs/guides/vendoring.md` shrinks to that.
- `adept library add --global` clones to `~/.adeptability/libs/<name>/` and registers it in
  the global config; global sync then renders those skills to home harness dirs.

## 3. Resolution and migration

Per scope, first-match wins:

1. Scope canonical skills.
2. Scope-local `libs/`, in config order.
3. **Project scope only:** machine-store fallback — if the project-local clone is missing but
   `~/.adeptability/libs/<name>/` exists, use it and warn once with a migrate hint.

`adept migrate` (project scope): re-clones the project's declared libraries into
`<project>/.adeptability/libs/`, after which the fallback warning stops. The machine store is
never deleted by migrate — global scope and other projects may still use it.

Day-one behavior for existing projects: nothing breaks; they resolve via fallback until
migrated.

## 4. Adapter global targets

`Spec` gains optional `GlobalOutput` (path template) and `GlobalBaseDir` (detection),
resolved against `$HOME` / XDG rather than the project root.

| Harness     | Global output                                              |
| ----------- | ---------------------------------------------------------- |
| claude-code | `~/.claude/skills/{id}/SKILL.md`                            |
| codex       | `~/.codex/AGENTS.md` (aggregated, same 32 KiB budget)       |
| opencode    | `~/.config/opencode/…` (exact path verified during implementation against opencode docs) |
| cursor      | none — global-incapable                                     |
| copilot     | none — global-incapable                                     |

`adept harness add <id> --global` for a global-incapable harness errors with a clear
"no global config location" message.

User-defined YAML adapters: optional `global-output` (and `global-base-dir`) keys; absent
means not global-capable.

## 5. Orchestrator changes

One new concept: **target root** — project root for project scope, `$HOME` for global scope.
Everything else is reused as-is:

- Staging stays inside the scope root (`~/.adeptability/staging/` for global).
- Relative symlinks from e.g. `~/.claude/skills/x` into `~/.adeptability/staging/…` work as
  today; copy-mode fallback unchanged.
- Atomic rename materialization unchanged.

**Safety invariant (critical):** global sync manages only the skill ids it tracks (via
`base/` snapshots and config). It must never prune, list-diff, or overwrite foreign entries
in shared home dirs like `~/.claude/skills/` — users have hand-installed skills there. A
pre-existing file at a path adept is about to manage goes through the same adopt/conflict
flow as project sync (`sync-from` to pull it into canonical, `--force` to overwrite).

## 6. CLI surface

`--global` persistent flag honored by: `library add/remove/list/update`,
`harness add/remove/list`, `sync`, `sync-from`, `status`, `config set`, `skill add`,
`import`. No new command tree. New command: `adept migrate` (project scope, §3).

## 7. Testing

- Unit: scope resolution precedence; machine-store fallback warning; adapter global template
  expansion (`~`, `$XDG_CONFIG_HOME`); `.adeptability/.gitignore` management; global-incapable
  harness rejection.
- Integration (temp `$HOME`): global sync renders claude/codex targets; foreign files in
  `~/.claude/skills/` untouched across sync cycles; project fallback → `adept migrate` flow;
  `status`/`sync-from`/drift parity in global scope; aggregated `~/.codex/AGENTS.md` adopt
  flow when a hand-written file exists.

## 8. Docs

- New `docs/concepts/scopes.md` (scope model, resolution, migration).
- Update `docs/concepts/libraries.md` (project-default clones), `docs/guides/vendoring.md`
  (shrinks to "commit `.adeptability/libs/`"), `docs/concepts/harnesses.md` (global-output
  column), README quickstart (global skills example).

## Out of scope

- Lockfile / version pinning beyond git refs (unchanged).
- Global targets for cursor/copilot (no real home config location today).
- XDG relocation of `~/.adeptability` itself.
- Cosign signing changes.
