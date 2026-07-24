---
title: "Safety scans & LLM config"
description: "Scan skills for unsafe instructions before running them, and configure the LLM used for scans."
---

Installing a skill from the internet means running someone else's instructions inside your
agent. `adept` scans skills for safety issues before they land, with an optional LLM intent
pass on top of the static checks.

## The scanner

`adept skill install` runs a safety scan first. It mirrors the
[getsentry/skill-scanner](https://github.com/getsentry/skills) categories — prompt-injection,
malicious-code, secret-exposure, supply-chain, and more.

**Critical findings hard-block the install** unless you explicitly override:

```bash
adept skill install vercel-labs/skills/find-skills
adept skill install vercel-labs/skills/find-skills --allow-unsafe   # install despite findings
adept skill install vercel-labs/skills/find-skills --yes            # skip the preview confirmation
```

## Scan any target without installing

```bash
adept skill check <target>
```

`<target>` can be:

- `<id>` — a skill in the project canonical (`.adeptability/skills/<id>/`)
- `library:<name>:<id>` — a specific skill in a configured library
- `<owner>/<repo>/<skill>` — a remote skill on skills.sh / GitHub

Flags:

```bash
adept skill check <target> --format markdown   # table (default) | markdown | json
adept skill check <target> --llm               # force the LLM intent pass (errors if no provider)
adept skill check <target> --no-llm            # skip the LLM pass even when a provider is configured
```

## Adding an LLM intent pass

Static checks catch known patterns; an LLM pass reasons about *intent*. Configure a provider —
API keys are **read from the environment at call time and never stored** in `config.json`:

```bash
export ANTHROPIC_API_KEY=sk-...
adept config llm set anthropic
adept config llm test                  # health-ping the provider
adept skill check <target>             # now runs static + LLM, merged
```

Supported providers: `anthropic` and `ollama`. Override the model or endpoint (e.g. a
self-hosted Ollama):

```bash
adept config llm set anthropic --model claude-3-5-sonnet-latest
adept config llm set ollama --endpoint http://localhost:11434
adept config llm unset                 # forget the provider
```

## Tuning scan behavior

Two config keys control the scan (see the [Command reference](/docs/reference/commands/#config)):

```bash
adept config set scan.onInstall true       # run the scan + LLM pass before every install
adept config set scan.blockSeverity high   # lowest severity that aborts an install
```

`scan.onInstall` defaults to **on when an LLM provider is configured**. `scan.blockSeverity`
sets the threshold at which findings abort the install — one of `critical` (default), `high`, or `medium`.

## Provenance

Every install **pins upstream provenance** — repo, ref, resolved SHA, and content hash — so a
skill is reproducible and you can audit exactly where it came from. Bump locked external skills
to their latest upstream with:

```bash
adept skill update            # all locked external skills
adept skill update find-skills
```
