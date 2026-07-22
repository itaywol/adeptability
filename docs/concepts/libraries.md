# Libraries

A **library** is a shared, versioned set of skills. Libraries are how a team authors skills
once and consumes them across many projects — resolved **package-manager style**.

## The package-manager model

This is the single most important mental model in `adept`, and the most common source of
confusion. A consumed library is **not committed into your project's canonical skills**.
Instead:

- The library's skills are cloned **into the project**, at
  `<project>/.adeptability/libs/<name>/skills/` — but that clone directory is auto-gitignored
  (`adept` appends `libs/` and `staging/` to `.adeptability/.gitignore` the first time it's
  created), so the clone itself never enters version control.
- Your project commits only a **reference** in `.adeptability/config.json`:

  ```json
  {
    "schema": 1,
    "libraries": [
      { "name": "team-skills", "remote": "git@github.com:acme/skills.git", "ref": "main" }
    ]
  }
  ```

The `libraries[]` entry is a *pointer* (`{name, remote, ref}`), not the skill bytes. Think of
it like a lockfile-driven dependency: the project declares what it depends on, and `adept
library add` / `adept migrate` re-materialize the clone from that pointer on any machine.

```
project/.adeptability/config.json      →  libraries: [{name, remote, ref}]   (committed)
                                             │ resolves to
                                             ▼
project/.adeptability/libs/<name>/skills/  →  the actual skill content       (gitignored clone)
                                             │ rendered by `adept sync`
                                             ▼
project/.claude/  .cursor/  AGENTS.md  …     materialized harness output     (gitignored / generated)
```

### The machine store

`$ADEPT_LIBRARY` (default `~/.adeptability`) is the **machine store** — it's also where the
[global scope](scopes.md) keeps its own skills, config, and library clones. Two things fall
back to it from project scope:

- **Resolution fallback**: if a configured library has no clone under
  `<project>/.adeptability/libs/<name>/`, `adept` looks for one at
  `~/.adeptability/libs/<name>/` and uses it if found — logging a warning that the library
  resolved from the machine store and suggesting `adept migrate` to localize it. This keeps a
  library added under `--global` (or a legacy project) usable from any project without
  re-cloning.
- **`adept migrate`**: re-clones every configured library into
  `<project>/.adeptability/libs/` so resolution stops depending on the machine store, then
  ensures the project's `.gitignore` covers it. It only touches the project's own clone
  directory — the machine store is never deleted, since the global scope and other projects
  may still be using it. Running it against `--global` is rejected (global libraries already
  live in the machine store — there's nothing to localize).

## Adding a library

```bash
# During init:
adept init --from git@github.com:acme/skills.git --name team-skills --ref main

# Or into an existing project:
adept library add team-skills --from git@github.com:acme/skills.git --ref main
```

Either clones the remote into `<project>/.adeptability/libs/team-skills/` and appends the
reference to `config.json`. Stack multiple libraries — on a skill-id collision, **first-wins**,
and any project-canonical skill shadows all of them.

Run the same command with the root `--global` flag to clone into the machine store instead
(`adept --global library add team-skills --from … ` → `~/.adeptability/libs/team-skills/`) —
see [Scopes](scopes.md).

Manage libraries:

```bash
adept library list                    # configured library references + on-disk / update state
adept library update [name]           # fetch newer skills from a library (prompts before applying)
adept library update --yes            # apply without prompting
adept library remove team-skills      # drop the reference (keeps the clone)
adept library remove team-skills --purge   # …and delete the local clone
```

## Teammate onboarding flow

This is why the reference model matters. When a teammate clones a repo that already has a
library reference:

1. **Clone the repo.** They get `.adeptability/config.json` with the `libraries[]` reference —
   but no skill bytes, because those were never committed.
2. **Run `adept`** (`adept sync`, or `adept library add`/`update` if the clone isn't present).
   `adept` reads the reference and **clones the remote into their own project-local libs dir**
   (`<project>/.adeptability/libs/<name>/`) — unless a clone already exists in their machine
   store from an earlier project or `--global` use, in which case it resolves from there
   instead (see [The machine store](#the-machine-store)).
3. **Harnesses materialize.** `adept sync` renders the resolved skills into their harness dirs
   (`.claude/`, `.cursor/`, …) using their configured [materialization mode](#materialization).

```bash
git clone git@github.com:acme/webapp.git && cd webapp
adept status                 # shows: library "team-skills" — no (run `adept library add` or `git pull`)
adept library add team-skills --from git@github.com:acme/skills.git   # clone into .adeptability/libs/
adept sync                   # materialize into this machine's harnesses
```

`adept status` reports each library's on-disk presence and whether an update is available
(`adept status --fetch` refreshes remote tracking refs first).

## Materialization

`adept` writes rendered harness files in one of two modes, set per project
(`config.mode`, default `symlink`):

- **`symlink`** (default) — harness files are symlinks into the resolved skill content. Cheap,
  and edits propagate; the recommended default.
- **`copy`** — harness files are plain copies. Use when a tool or CI environment doesn't follow
  symlinks.

Set it with `adept config set mode symlink|copy` or `adept init --mode copy`.

## Base snapshot

Alongside the resolved library, `adept` keeps a **base snapshot** at
`<root>/.adeptability/base/<id>/` — the last-synced common ancestor. It's the third leg of the
[3-way drift model](drift.md) that lets `adept` tell "you edited it" apart from "upstream
changed it."

## Related

- [Sharing a library with a team (guide)](../guides/sharing-a-library.md)
- [Vendoring a library in-repo (guide)](../guides/vendoring.md)
- [Layouts](layouts.md) — consumer vs. publishable-library layout.
