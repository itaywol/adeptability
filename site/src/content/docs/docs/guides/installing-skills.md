---
title: "Installing skills from skills.sh and GitHub"
description: "Search skills.sh, install a skill into your project canonical, and keep external skills SHA-locked and updatable."
---

`adept skill install` pulls a published skill from GitHub (indexed by
[skills.sh](https://skills.sh)) straight into your project canonical, then locks it to an exact
commit so it never changes under you. Every install is gated by a safety scan first.

## Find a skill

```bash
adept skill search "postgres migration"
adept skill search react --limit 5      # default limit is 20
```

Search queries the skills.sh index and prints matching skills with the slug you pass to
`install`.

## Inspect before installing

```bash
adept skill info owner/repo/skill-name
```

`info` fetches repo metadata without touching your project. It reports the repository, license,
star count, install count (from skills.sh), and the current resolved commit SHA. Add `--json`
for a machine-readable record.

## Install

```bash
adept skill install owner/repo/skill-name
```

The slug is `<owner>/<repo>[#ref]/<skill>`. The optional `#ref` pins a branch, tag, or commit;
without it, the repository's default branch is resolved. Install then:

1. Resolves the ref to an immutable commit SHA and downloads that exact tarball.
2. Extracts the target skill directory and parses its `SKILL.md`.
3. Runs the safety scan (see below).
4. Shows an install preview: slug, repo URL, SHA, path, license, stars, installs, the file
   list, and any scan findings.
5. Prompts `proceed with install? [y/N]`, then writes the skill into the project canonical,
   snapshots a base for drift detection, and records a lock entry.

Skip the confirmation with `--yes`:

```bash
adept skill install owner/repo/skill-name --yes
```

:::note[Reinstalling and name clashes]
Re-installing a locked skill overwrites it. If a skill directory with the same id already
exists in the canonical but has no lock entry (a manual copy), install refuses and asks you to
remove or rename it first.
:::

## How external skills are SHA-locked

Installing records a lock entry for the skill: the source slug, the resolved commit SHA, the
matched skill path, and a content hash of the extracted files. Because the ref is resolved to a
specific commit at install time, the on-disk skill is pinned to immutable content, not to a
moving branch. The base snapshot taken at install is what [drift detection](/docs/concepts/drift/)
compares against, so later edits (yours or a re-render) are visible as drift.

## Update locked skills

```bash
adept skill update              # re-resolve every locked external skill
adept skill update skill-name   # just one
```

`update` re-resolves each locked skill's ref against upstream. If the SHA is unchanged it
prints `up to date`; otherwise it prints `old -> new`, re-downloads the new tarball, rewrites
the skill and its base snapshot, and bumps the SHA and content hash in the lock. The lock is
saved after each skill so a mid-run failure never leaves an entry pointing at stale content.

## How the safety scan gates installs

Every install runs a structured static scan over `SKILL.md` plus tighter script-only rules over
any sidecar files, optionally followed by an LLM intent pass when a provider is configured. If
the worst finding meets or exceeds the blocking severity, the install aborts:

```
install aborted by safety scan; review findings and pass --allow-unsafe to override
```

The blocking severity is `scan.blockSeverity` (default `critical`); tighten it to `high` or
`medium` per project. Whether the scan runs at all is `scan.onInstall` (defaults on once an LLM
provider is set). Override a blocked install with `--allow-unsafe` only after reviewing the
findings. See [Safety scans and LLM config](/docs/guides/safety-scans/) for the full model, and
the [configuration reference](/docs/reference/configuration/) for the keys.

:::caution[--json installs are non-interactive]
In `--json` mode there is no confirmation prompt, so `--yes` is required. A scan block still
aborts unless you also pass `--allow-unsafe`.
:::

## Scan any skill without installing

`adept skill check` runs the same scanner against a target you name three ways:

```bash
adept skill check my-skill                    # a project canonical skill, by id
adept skill check library:default:my-skill    # library:<name>:<id>
adept skill check owner/repo/skill-name       # a remote skill, fetched not installed
```

Flags: `--format table|markdown|json` (default `table`), `--llm` to force the LLM intent pass
(errors when no provider is configured), and `--no-llm` to skip it even when one is. High and
critical findings exit non-zero, so `skill check` works as a CI gate.
