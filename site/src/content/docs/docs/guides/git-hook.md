---
title: "Git drift hook"
description: "Install a git pre-commit hook that keeps committed harness files honest."
---

Keep committed harness files honest by installing a git **pre-commit** hook that runs the
[drift gate](/docs/concepts/drift/) on every commit.

## Install

```bash
adept hook install                 # fail mode (default)
adept hook install --mode fix      # auto-fix mode
```

This writes `.git/hooks/pre-commit`. The installed hook is a thin stub that execs
`adept hook run` — all the logic stays in the CLI.

You can also install it at `init` time:

```bash
adept init --git-hook              # bare flag == fail mode
adept init --git-hook=fix
```

## Modes

| Mode | Behavior on drift |
| --- | --- |
| `fail` (default) | The commit is **blocked**; you fix drift and re-commit. |
| `fix` | `adept` auto-adopts / re-renders the drift and **re-stages** the files, then the commit proceeds. |

Under the hood, `fix` mode runs `adept hook run --fix`.

## Safety

- `adept` only overwrites a pre-commit hook it wrote itself (marked with an internal
  `adept-managed` marker). It **refuses to clobber a hand-written hook** — remove it or install
  manually if you already have one.
- The hook assumes a standard `.git/` directory. In a worktree or submodule (where `.git` is a
  file pointing elsewhere), install the hook manually.
- The installed hook calls `adept` from `$PATH` — that's the install-time contract, so make
  sure `adept` is on the `PATH` of whoever commits.

## Running the gate manually / in CI

The same gate the hook invokes:

```bash
adept hook run           # fail on drift
adept hook run --fix     # auto-adopt/re-render and re-stage
```

For CI, prefer `adept status`, which exits `2` when anything is out of sync:

```bash
adept status || echo "drift detected"
```
