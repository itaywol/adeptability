---
title: "Scripting and CI with --json"
description: "Use the global --json flag, jq pipelines, and drift exit codes to gate CI on skill sync state."
---

Every `adept` command takes a global `--json` flag that swaps human tables for machine-readable
JSON. Combine it with exit codes and `jq` to script adept in CI.

## The --json flag

```bash
adept status --json
adept diff --json
adept skill list --json
adept harness list --json
adept config list --json
```

`--json` is global, so it sits before or after the subcommand. Read and list commands produce
the most useful payloads:

- `status --json` is a single object: `initialized`, `projectRoot`, `mode`, `skillsCanonical`,
  `driftedHarnesses`, `updatableLibraries`, and more.
- `diff --json` is an array, one entry per harness: `{ harness, synced, drifted, missing, conflict }`.
  It is `null` when nothing is enabled.
- `skill list`, `agent list`, `harness list`, and `library list` return arrays of objects.
- `config list` returns `{ key, value, allowed, help }` objects.

## jq pipelines

```bash
# Is anything drifted right now?
adept status --json | jq '.driftedHarnesses'

# Which harnesses are enabled?
adept harness list --json | jq -r '.[] | select(.enabled) | .id'

# List every drifted file across all harnesses.
adept diff --json | jq -r '.[] | .drifted[]?'

# Read one config value.
adept config list --json | jq -r '.[] | select(.key=="mode") | .value'

# Just the project skill ids.
adept skill list --json | jq -r '.[] | select(.source=="project") | .id'
```

## Exit codes for CI gating

`status` and `diff` report drift through their exit code, so a CI job can fail the build when
harness outputs are stale:

| Code | Meaning |
| --- | --- |
| `0` | Clean: no drift. |
| `2` | Drift detected (`status` out of sync, `diff` found differences). |
| `1` | Any other error (bad flags, unreadable project, and so on). |

```yaml
# CI: fail if canonical skills and harness outputs disagree.
- run: adept diff
```

`adept diff` exits `2` when any harness has drifted, writing nothing. That is the drift gate:
non-zero fails the step. `adept sync` (or the fix-mode hook below) is how you resolve it.

:::tip[Pair the gate with --json for reporting]
`adept diff` sets the exit code; `adept diff --json` gives you the detail to log. Run the plain
form to gate and the JSON form to annotate, or read `$?` after the JSON call.
:::

## The pre-commit hook in CI

`adept hook install` writes `.git/hooks/pre-commit`:

```bash
adept hook install                # fail mode (default)
adept hook install --mode fix     # adopt harness edits, re-render, re-stage
```

In `fail` mode a drifted commit is blocked with a suggested fix. In `fix` mode the hook adopts
harness-side edits back into canonical, re-renders, and re-stages so the commit proceeds. For CI
that cannot install git hooks, run `adept diff` directly as the gate instead. See the
[git drift hook guide](/docs/guides/git-hook/) for the full workflow.

## Running outside the repo

Two more global flags matter for scripts:

- `--project <path>`: point adept at a project root other than the current directory, so a CI
  job can run from anywhere.
- `--log-level debug|info|warn|error` (default `info`): raise to `debug` to trace resolution, or
  drop to `error` to keep logs out of captured output. Logs go to stderr; `--json` payloads go
  to stdout, so redirection stays clean.

```bash
adept status --json --project /src/app --log-level error | jq '.driftedHarnesses'
```
