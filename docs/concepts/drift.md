# Drift & the 3-way model

`adept` has no lockfile. The **filesystem itself is the source of truth**, and hash
comparisons tell `adept` exactly what changed and in which direction. Two independent
comparisons run: the **3-way model** between project and library, and a per-harness
comparison between canonical and the rendered output on disk.

## The 3-way model: project vs. library

For every skill, `adept` compares three content hashes:

1. **Project canonical** — `<root>/.adeptability/skills/<id>/` (or `<root>/skills/<id>/` in a
   library layout). This is "ours."
2. **Base** — `<root>/.adeptability/base/<id>/`, the last-synced snapshot. This is the common
   ancestor.
3. **Library canonical** — `$ADEPT_LIBRARY/skills/<id>/`. This is "upstream."

Because skill identity is `(id, content-hash)`, `adept` can hash each side and reason about
divergence without any version numbers or a lockfile.

| State | Meaning |
| --- | --- |
| `synced` | Neither side changed since base — nothing to do. |
| `ahead` | Project canonical changed; the library didn't. Your local edit. |
| `behind` | Library changed; project canonical didn't. Upstream moved. |
| `diverged` | Both sides changed since base — a real conflict. |

This is what lets `adept` tell *"you edited it"* apart from *"upstream changed it"* — see
[Libraries](libraries.md) for how the base snapshot fits in.

## Harness drift: canonical vs. rendered output

Separately, `adept` renders each skill in memory and compares the result against what's
actually on disk in each enabled harness (`.claude/…`, `.cursor/…`, …). Each expected file —
including sidecars — classifies as:

| State | Meaning |
| --- | --- |
| `SYNCED` | On-disk bytes match the expected render. |
| `DRIFTED` | The file exists but its bytes differ — the harness side was edited. |
| `MISSING` | The expected file isn't on disk yet. |
| `CONFLICT` | The path has the wrong shape — a directory, broken symlink, or unreadable target. |

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

## Resolving harness drift

- **Canonical is right → publish:** `adept sync` (add `--force` to overwrite drifted harness
  files, `--dry-run` to preview).
- **Harness edit is right → adopt:** `adept sync-from` (interactive), or `--all` for
  non-interactive adoption from every harness, `--force` to overwrite canonical, `--dry-run`
  to preview.
- **Conflict:** review with `adept diff`, then decide which side wins and run the
  corresponding command with `--force`.

## Enforcing it in git

Install a pre-commit hook that blocks commits when there's drift — see the
[Git drift hook guide](../guides/git-hook.md). In CI, run `adept status` and branch on its
exit code (`2` = drift/dirty).
