---
title: "Importing existing rules"
description: "Already have .cursor/rules, .claude/skills, an AGENTS.md, or GitHub instructions? Import them into adept."
---

Already have `.cursor/rules`, `.claude/skills`, an `AGENTS.md`, or `.github/instructions`?
`adept` adopts them into the canonical layout so you can manage them from one place.

## Automatic adoption on `init`

The simplest path. In a project that already has harness files, just:

```bash
adept init
```

`init` scans for pre-existing harness trees — `.claude/`, `.cursor/`, `.opencode/`,
`AGENTS.md`, `.github/instructions/` — parses them back into canonical skills under
`.adeptability/skills/`, and records the matching harness ids in `config.json`.

Verify what got adopted and confirm the round-trip is clean:

```bash
adept status      # shows adopted harnesses + skill counts
adept diff        # should be clean if adoption round-tripped correctly
```

## Adopting later with `sync-from`

If you add harness files *after* init (or edit them directly), pull them into canonical with
`sync-from`:

```bash
adept sync-from                    # interactive: adopt harness-side edits
adept sync-from --harness cursor   # limit to one harness
adept sync-from --all              # non-interactive: adopt from every harness (strategy=first)
adept sync-from --dry-run          # preview what would be imported, write nothing
adept sync-from --force            # overwrite existing canonical content
```

Then publish the canonical version back out everywhere:

```bash
adept sync
```

## How each format maps back

Adoption is the inverse of rendering:

- **Claude Code / OpenCode** (`.claude/skills/<id>/`, `.opencode/skill/<id>/`) — per-skill
  directories map straight to canonical skills, sidecars included.
- **Cursor** (`.cursor/rules/<id>.mdc`) — the `mdc` frontmatter (`description`, `alwaysApply`,
  `globs`) is translated back to canonical `activation` + `description`.
- **Codex** (`AGENTS.md`) — the aggregated file is split back into its constituent skills by
  the `adeptability:begin/end` markers.
- **Copilot** (`.github/instructions/*.instructions.md`) — per-glob buckets are de-aggregated.

For **config-driven adapters**, `sync-from` auto-derives the reverse of the adapter's
`frontmatter.rename`. Non-bijective renames need an explicit `import.rename` block — see
[Harnesses & adapters](/docs/concepts/harnesses/#config-driven-adapters-no-recompile).

## After importing

Once adopted, `.adeptability/skills/` is your source of truth. Edit there and `adept sync`,
rather than editing the harness files directly.
