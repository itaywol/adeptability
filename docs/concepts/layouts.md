# Layouts

A project's **layout** decides *where canonical skills live on disk*. It's set once at
`init` time and recorded in `.adeptability/config.json` (`layout`).

## Consumer layout (default)

The default. Canonical skills live under `.adeptability/`, keeping adept's files out of the way
of your actual project:

```
<root>/
├── .adeptability/
│   ├── config.json
│   ├── skills/<id>/SKILL.md     ← canonical skills
│   └── base/<id>/               ← last-synced base snapshots
├── .claude/                     ← materialized (generated)
└── .cursor/                     ← materialized (generated)
```

Create it with a plain:

```bash
adept init
```

Use this for any repo that *consumes* skills (an app, a service, your day-to-day project).

## Library layout (`--as-library`)

For a repo whose purpose **is** to be a shared library. Canonical skills live at
`<root>/skills/` — the exact place a consumer's `adept library add` / `adept init --from`
reads them — so the repo is directly consumable as a library:

```
<root>/
├── skills/<id>/SKILL.md         ← canonical (published) skills
└── .adeptability/
    ├── config.json              (layout: "library")
    └── base/<id>/
```

Create it with:

```bash
adept init --as-library
```

In library layout, `adept`:

- Places canonical skills at `<root>/skills/` instead of `.adeptability/skills/`.
- **Skips** harness adoption, materialization mode, and the bundled consumer default skills —
  a library curates its own skills and renders to no harnesses of its own.

## Publishing vs. private dev skills in a library

Inside a library project, `adept skill add` writes to the **private dev-canonical** by
default (skills you use while developing the library, not published to consumers). Add
`--publish` to place a skill into the published `skills/` tree that consumers pull:

```bash
adept skill add my-published-skill --publish --edit
```

`adept status` distinguishes them: *"N published, M private, K resolved from libraries."*

## Choosing

| You are… | Layout | Command |
| --- | --- | --- |
| Building an app that uses skills | Consumer | `adept init` |
| Building a skill library others depend on | Library | `adept init --as-library` |
