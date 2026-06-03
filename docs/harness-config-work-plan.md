# Work Plan: Harness Correctness Fixes + Per-Skill Per-Harness Config

> Output of a harness-adapter audit (one read-only agent per harness: claude-code, cursor,
> codex, copilot, opencode, perskill) plus a UX-judged design pass. Every claim below is
> grounded in the current source. **Part A** lists places a rendered skill diverges from what
> the harness actually expects; **Part B** designs per-skill per-harness configuration;
> **Part C** sequences the work. Emphasis throughout: user experience.
>
> Status: proposal for maintainer decision — not yet implemented. The A-series correctness
> fixes are independent of the feature and should ship first.
>
> ⚠️ One unresolved discrepancy to verify before acting: the per-harness audit flagged the
> OpenCode adapter as writing the wrong output path (`.opencode/skill/` singular) and emitting
> no YAML frontmatter; the synthesis pass, after re-reading the source, judged output paths and
> sidecars "clean across all harnesses." Confirm OpenCode's current skill discovery path and
> frontmatter requirement against `opencode.ai/docs/skills` before changing the renderer.

## Part A — Harness correctness fixes

Prioritized by user-visible impact. "User-visible" = a skill silently fails to activate, lands
in the wrong place, loses content, or corrupts on round-trip.

| # | Harness | Issue (what the user sees) | Sev | Fix | Effort |
|---|---------|----------------------------|-----|-----|--------|
| A1 | cursor | **Glob rule never matches → skill silently dead.** `globs` is emitted as a YAML flow sequence `globs: [cmd/**/*.go, internal/**/*.go]` (`cursor/renderer.go:114` → `[]string` → flow `SequenceNode` in `common/frontmatter.go:91-100`). Cursor `.mdc` expects a bare comma-separated string, no brackets/quotes. Auto-Attach rule is dead. | **High** | Emit `globs` as a single joined string `strings.Join(s.Globs, ", ")` (a `string` Field, no `Quote`), not `[]string`. Update `cursor/testdata/globs.golden`. Make `cursor/import.go` tolerant of a scalar string. | S |
| A2 | claude | **Glob skill does not round-trip; description permanently accretes `(matches: ...)`.** Render appends ` (matches: a, b)` to description (`claude/renderer.go:108-114`); Import (`claude/import.go:43-56`) never strips it, never repopulates `Globs`, hardcodes `Activation=agent`. push→import→push is non-idempotent. | **High** | In `claude/import.go`: regex-match trailing ` (matches: <csv>)`, strip it, split CSV into `skill.Globs`, set `Activation=ActivationGlobs`. Add a round-trip test. | S |
| A3 | claude / canonical | **Valid canonical id renders a malformed Claude skill.** `SkillIDPattern` (`pkg/adept/constants.go:25`, `skill.schema.json:11`) permits `_` and leading `_`; Claude/Agent Skills require `^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`. An id like `my_skill`/`_internal` yields a `name`/dir Claude treats as malformed. | **High** | Decision: (a) tighten `SkillIDPattern` + schema to drop underscore/leading-trailing hyphen (cleanest; rejects existing `_` ids → migrate note), or (b) claude renderer sanitizes `id→name` (`_`→`-`) + `Warning`. Recommend (a). | S–M |
| A4 | claude | **Manual-mode detection misfires on body text.** Import classifies manual via whole-file `strings.Contains(..., "disable-model-invocation: true")` (`claude/import.go:53`); a body documenting the field is misread as manual; `disable-model-invocation:true` (no space) is missed. | Low | Surface `disable-model-invocation`/`user-invocable` as parsed frontmatter booleans; map in Import. Folds into A2. | S |
| A5 | claude | **Long description silently truncated by Claude (can cut the glob hint activation depends on).** No ~1,536-char budget guard in `buildDescription` (`claude/renderer.go:108-114`). | Low | Emit a `Warning` when rendered description exceeds the budget; place `(matches: ...)` before free prose so it survives truncation. | S |
| A6 | claude | **No `name` length/charset guard** beyond the 50-char id cap → silent name/command mismatch on odd names. | Low | Subsumed by A3 + existing cap; add a `<=64` length assertion test. | XS |

Notes:
- **A1 and A2 are the two that make a user's skill silently not work** — ship first. A1 is a pure cursor frontmatter-format bug; A2 is data-loss/corruption on the canonical round-trip.
- A3 is user-visible only for ids containing `_`; grep the dogfood library before choosing (a) vs (b).

## Part B — Per-skill per-harness config feature

### The gap (why this is needed)

Canonical `adept.Skill` has 8 author-facing fields (`id`, `description`, `activation`, `globs`,
`allowed-tools`, `targets`, `tags`, `metadata`) and `skill.schema.json` is closed
(`additionalProperties:false`) — an author literally cannot express anything else. The audit
mapped per-harness knobs and found **19 missing, 8 partial, 11 covered**, in three classes:
1. **Harness-native knobs with no canonical home** — Claude `model`/`effort`/`when_to_use`/
   `user-invocable`/`disallowed-tools`/`argument-hint`/`hooks`; Copilot `excludeAgent`;
   OpenCode `license`/`compatibility`.
2. **Canonical fields silently dropped by some renderers** — `allowed-tools` dropped by
   cursor/codex/opencode; `tags`/`metadata` emitted by no built-in renderer.
3. **Values an author wants to differ by harness** — e.g. tighter `allowed-tools` on Claude
   than the experimental hint perskill emits, or a Claude-only `model` pin.

### Recommended design — *promoted canonical knobs + per-harness escape-hatch map*

Chosen over (1) a raw `harness:` block alone and (3) project-config sidecar overrides. It won
on weighted UX/simplicity/safety **and** fits this codebase:
- Rides the existing typed-validator pattern (`skillToSchemaDoc`) so validation is real.
- Fits the existing `common.Field` value union (`toNode`) for promoted scalars — no rewrite.
- Drift is correct for free: drift runs on full rendered bytes via `adapter.Validate`, not a hash.
- One merge choke point: `renderAll` builds every `RenderInput` — all ~51 renderers benefit at once.
- **No config-schema bump** → avoids the `config.Read` hard-reject hazard (`config/store.go:107`).
- Keeps `alwaysApply`/`applyTo` **derived** from `activation`+`globs` — no dual source of truth.

### Canonical schema change

Add to `pkg/adept/canonical.go`:
1. A minimal promoted scalar where there's broad value — `model` (highest-value per the matrix; `effort` optional).
2. A per-harness escape-hatch map for everything else:

```go
// Harness holds per-harness frontmatter/option overrides keyed by harness id.
// Merged LAST over the derived frontmatter at render time. Round-trips verbatim.
Harness map[string]map[string]any `yaml:"harness,omitempty" json:"harness,omitempty"`
```

`skill.schema.json`: keep outer `additionalProperties:false`, add an **open** `harness`
property that validates unknown knobs but blocks identity-field hijack:

```json
"harness": {
  "type": "object",
  "propertyNames": { "pattern": "^[a-z0-9_][a-z0-9_-]{0,49}$" },
  "additionalProperties": {
    "type": "object",
    "additionalProperties": true,
    "not": { "anyOf": [ {"required":["name"]}, {"required":["description"]}, {"required":["id"]} ] }
  }
}
```

**Touchpoints the audit flagged as "not free":**
- `skillToSchemaDoc` (`validator.go`) must emit `harness`+`model`, or the schema never sees it
  (the parser drops unknown frontmatter silently).
- `RenderCanonical` (`canonical/writer.go`) is a hand-rolled string builder; it must learn to
  emit the `harness` block + `model` deterministically (route the map through a `yaml.Node` encode).
- `adept.Skill` parsing: add `Model string` and the `Harness` map (yaml tags).

### Per-renderer changes

Add one shared helper in `internal/render/common`:

```go
// MergeOverride applies override keys onto derived fields (same-named keys replace,
// new keys append). Errors on a value type toNode cannot serialize.
func MergeOverride(fields []Field, override map[string]any) ([]Field, error)
```

Wire it at the choke point: a flatten step in `renderAll` resolves promoted `model` +
`Skill.Harness[spec.ID]` into the input the renderer sees, so renderers stay ignorant of the
override map. Phase 1 consumers: **claude** and **cursor**. `applyTo` (copilot) and codex stay
derived/out-of-scope (bucket-aggregated output makes per-skill overrides awkward). `toNode` may
need a float passthrough if a numeric knob is added.

### Authoring UX — exactly what the author types

One place to edit — the SKILL.md frontmatter they already own. **Zero added lines in the
common (no-override) case.**

```yaml
---
id: db-migrations
description: Guides safe database migrations and rollbacks.
activation: globs
globs: ["**/migrations/**"]
model: claude-opus-4-8          # promoted: simple, top-level, Claude consumes it
harness:
  claude-code:
    allowed-tools: [Bash, Read, Edit]
    effort: high
    user-invocable: false        # Claude-only knob
  cursor:
    alwaysApply: false           # cursor-native escape-hatch knob
---
(body...)
```

CLI: load-time validation (`canonical.Loader → validator.Validate`) reports unknown harness id
→ schema error; attempt to override `id`/`name`/`description` → schema error. Optional
`adept skill check --strict-harness` cross-checks each override key against the adapter's
declared knob set with a "did you mean" suggestion — opt-in so new harness knobs don't break
older adept.

### Validation, round-trip, back-compat

- Validation is genuine (schema), with identity fields protected by the `not` clause.
- `MergeOverride` errors on unserializable values → clear failure, not silent garbage.
- **Import does not reconstruct the `harness` block in Phase 1** (a harness file can't tell
  canonical vs override apart) — imports canonical fields only, documented. Fix A2/A4 first so
  the canonical round-trip is correct before layering overrides.
- **No config schema bump**; `harness`/`model` are `omitempty` → existing SKILL.md render
  byte-identical (no drift churn). Schema widening is additive.

### Phased rollout

1. **Phase 1** — `Harness` map + `model`; `MergeOverride` via `renderAll` flatten; claude+cursor
   consume; schema + `skillToSchemaDoc` + `RenderCanonical` updated. Tests: override round-trip,
   merge last-wins, unknown-harness schema error.
2. **Phase 2** — `--strict-harness` with per-adapter declared knob sets + did-you-mean.
3. **Phase 3** — opencode frontmatter emission; broaden knobs; copilot/codex likely remain out of scope.

## Part C — Sequencing

1. **A1 (cursor globs string) + A2/A4 (claude glob round-trip + manual detection)** — the bugs
   where a user's skill silently does the wrong thing. Independent, low effort, highest
   trust payoff. Ship as one fix PR.
2. **A3 (id charset)** — decide tighten-vs-sanitize; grep dogfood library for `_` ids first.
   Do before the feature (the `harness` map's `propertyNames` reuses `SkillIDPattern`).
3. **A5/A6** — fold into the claude PR.
4. **Feature Phase 1** — after A2/A4. Critical path: extend `adept.Skill`+parser →
   `skillToSchemaDoc` → `RenderCanonical` → `MergeOverride`+`renderAll` flatten → claude/cursor consume.
5. **Feature Phases 2–3** — after Phase 1 is dogfooded.

**Risks:** silent drop of override fields if `skillToSchemaDoc`/`RenderCanonical` aren't both
updated (gate Phase 1 on a parse→render→parse round-trip test); `toNode` value-type gap on
nested/float overrides (MergeOverride validates + clear error); A3 tightening is breaking for
`_` ids (gate on library grep + migrate note); Import asymmetry is a documented Phase-1 limitation.

### Critical files
`pkg/adept/canonical.go` · `internal/canonical/validator.go` · `internal/canonical/writer.go` ·
`internal/harness/orchestrator.go` · `internal/render/cursor/renderer.go` (A1) ·
`internal/render/claude/import.go` (A2/A4)
