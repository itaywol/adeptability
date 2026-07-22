# Scopes

A **scope** decides *where* `adept` reads and writes its metadata, skills, and materialized
harness files: a single project, or your whole account (home directory).

## The two scopes

| | Project scope (default) | Global scope (`--global`) |
| --- | --- | --- |
| Metadata root | `<project>/.adeptability/` | `$ADEPT_LIBRARY` (default `~/.adeptability`) |
| Canonical skills | `<project>/.adeptability/skills/` | `~/.adeptability/skills/` |
| Base snapshots | `<project>/.adeptability/base/` | `~/.adeptability/base/` |
| Render staging | `<project>/.adeptability/staging/` | `~/.adeptability/staging/` |
| Library clones | `<project>/.adeptability/libs/` | `~/.adeptability/libs/` (the machine store itself) |
| Render root | `<project>/` | parent of the library root (`$HOME` by default) |
| Agents | rendered | **not rendered** (v1 limitation, see below) |

In global scope, `adept`'s own metadata directory (`~/.adeptability`) *is* the machine-wide
library store — there's no separate project to point at. Rendering targets each harness's
home-level config location instead of a project-relative one, e.g. `~/.claude/skills/<id>/SKILL.md`
instead of `<project>/.claude/skills/<id>/SKILL.md`.

## Scope resolution

Every command resolves its scope in this order:

1. **`--global`** — forces global scope, regardless of cwd.
2. **Explicit `--project <dir>`** — pins that directory; no walk-up.
3. **Walk up from cwd** looking for `.adeptability/config.json` — the nearest ancestor with one
   wins.
4. **Global fallback** — if no project is found anywhere up the tree, `adept` falls back to the
   global scope and logs a one-line warning to stderr:

   ```
   no project found (no .adeptability/config.json up the tree) — operating on global scope; pass --project to target a project
   ```

`--global` and `--project` are mutually exclusive in effect — `--global` always wins if both
are set.

## Global-capable harnesses

Only harnesses with a real home-level config location can be enabled with `--global`:

| Harness | Global output |
| --- | --- |
| `claude-code` | `~/.claude/skills/<id>/SKILL.md` |
| `codex` | `~/.codex/AGENTS.md` |
| `opencode` | `~/.config/opencode/skill/<id>/SKILL.md` |

`cursor` and `copilot` are **not** global-capable: Cursor rules and Copilot's per-glob
instructions buckets are project-scoped concepts with no equivalent home-level file the
respective tool reads, so there's nothing meaningful to render into. Both
`adept --global harness add cursor` and `adept --global harness add copilot` are rejected:

```
harness "cursor": harness has no global config location (project scope only)
```

Config-driven (YAML) adapters can opt into global support too — see
[Harnesses & adapters](harnesses.md#global-support).

## v1 limitation: skills only

Global scope renders **skills only** — canonical agents are not yet given a global target, so
`adept --global sync` skips agent rendering entirely. This is a deliberate v1 narrowing, not an
oversight; agent rendering resumes automatically once a project scope is used.

`adept sync-from --global` is narrower still: it currently only supports harnesses whose global
render path matches their project-relative one byte-for-byte (`claude-code`). Importing
harness-side edits back from other global-capable harnesses is not yet wired up.

## Quickstart

```bash
adept --global harness add claude-code   # enable Claude Code globally
adept --global skill add my-skill --edit # author a skill under ~/.adeptability/skills/
adept --global sync                      # render it to ~/.claude/skills/my-skill/SKILL.md
```

Everything else — `adept --global skill list`, `adept --global status`, `adept --global library
add …` — works the same way: pass `--global` on any command that would otherwise resolve a
project.

## Safety

`adept sync` (global or project) only ever writes to the specific output path it computed for
each skill — it never scans or prunes a harness directory. A global sync into `~/.claude/`
leaves every file it doesn't manage (other skills, your own notes, anything not under a path
`adept` generated) byte-for-byte untouched, including on repeated and forced syncs.

## Related

- [Libraries](libraries.md) — project-local clones vs. the machine store, and how the two
  interact with scope.
- [Harnesses & adapters](harnesses.md) — the full adapter table, including the Global column.
