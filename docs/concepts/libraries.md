# Libraries

A **library** is a shared, versioned set of skills. Libraries are how a team authors skills
once and consumes them across many projects — resolved **package-manager style**.

## The package-manager model

This is the single most important mental model in `adept`, and the most common source of
confusion. A consumed library is **not committed into your project**. Instead:

- The library's skills live in a **per-machine shared store**:
  `$ADEPT_LIBRARY/libs/<name>/skills/` — by default `~/.adeptability/libs/<name>/skills/`.
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
it like `~/.m2`, `~/.cargo`, or the Go module cache: the project declares what it depends on;
the actual content is cached once per machine and shared across every project that references
it.

```
project/.adeptability/config.json   →  libraries: [{name, remote, ref}]   (committed)
                                          │ resolves to
                                          ▼
$ADEPT_LIBRARY/libs/<name>/skills/  →  the actual skill content            (per-machine store)
                                          │ rendered by `adept sync`
                                          ▼
project/.claude/  .cursor/  AGENTS.md  …  materialized harness output       (gitignored / generated)
```

## Adding a library

```bash
# During init:
adept init --from git@github.com:acme/skills.git --name team-skills --ref main

# Or into an existing project:
adept library add team-skills --from git@github.com:acme/skills.git --ref main
```

Either clones the remote into `$ADEPT_LIBRARY/libs/team-skills/` and appends the reference to
`config.json`. Stack multiple libraries — on a skill-id collision, **first-wins**, and any
project-canonical skill shadows all of them.

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
   `adept` reads the reference and **clones the remote into their own local store**
   (`~/.adeptability/libs/<name>/`).
3. **Harnesses materialize.** `adept sync` renders the resolved skills into their harness dirs
   (`.claude/`, `.cursor/`, …) using their configured [materialization mode](#materialization).

```bash
git clone git@github.com:acme/webapp.git && cd webapp
adept status                 # shows: library "team-skills" — no (run `adept library add` or `git pull`)
adept library add team-skills --from git@github.com:acme/skills.git   # resolve into local store
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
