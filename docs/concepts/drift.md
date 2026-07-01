# Drift & the 3-way model

`adept` has no lockfile. The **filesystem itself is the source of truth**, and a hash-based
3-way comparison tells `adept` exactly what changed and in which direction.

## The three inputs

For every skill, `adept` compares three things:

1. **Canonical** — `<root>/.adeptability/skills/<id>/` (or `<root>/skills/<id>/` in a library
   layout). This is "ours."
2. **Base** — `<root>/.adeptability/base/<id>/`, the last-synced snapshot. This is the common
   ancestor.
3. **Rendered output** — what's actually on disk in each harness (`.claude/…`, `.cursor/…`, …).

Because skill identity is `(id, content-hash)`, `adept` can hash each side and reason about
divergence without any version numbers or a lockfile.

## States

The comparison resolves to one of:

| State | Meaning |
| --- | --- |
| `synced` | Canonical and rendered output match the base — nothing to do. |
| `ahead` | Canonical changed; harness output is stale. `adept sync` publishes it. |
| `behind` | Harness output changed; canonical is stale. `adept sync-from` adopts it. |
| `diverged` | Both sides changed since base — a real conflict. |

## Inspecting drift

```bash
adept status              # one-line drift summary per harness; exits 2 if anything is out of sync
adept diff                # exactly which files are drifted / missing / conflicting
adept diff --harness cursor
```

`adept diff` output groups per harness:

```
HARNESS      SYNCED  DRIFTED  MISSING  CONFLICT
claude-code  3       1        0        0

[claude-code]
  drift    .claude/skills/pr-review/SKILL.md
```

## Resolving drift

- **Canonical is right → publish:** `adept sync` (add `--force` to overwrite drifted harness
  files, `--dry-run` to preview).
- **Harness edit is right → adopt:** `adept sync-from` (interactive), or `--all` for
  non-interactive adoption from every harness, `--force` to overwrite canonical, `--dry-run`
  to preview.
- **Conflict (`diverged`):** review with `adept diff`, then decide which side wins and run the
  corresponding command with `--force`.

## Enforcing it in git

Install a pre-commit hook that blocks commits when there's drift — see the
[Git drift hook guide](../guides/git-hook.md). In CI, run `adept status` and branch on its
exit code (`2` = drift/dirty).
