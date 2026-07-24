---
title: "Configuration reference"
description: "Every adept config key with its type, default, and effect, plus the LLM provider config and project vs global scope."
---

`adept config` reads and writes project settings. Sets are strict-typed: an invalid value fails
at parse time with the allowed list, so a typo never corrupts the config file.

```bash
adept config list                       # all keys, current values, allowed values
adept config get mode                   # print one value
adept config set mode copy              # set one value (validated)
adept config unset mode                 # clear back to default
```

Add `--json` to `list` for an array of `{key, value, allowed, help}` objects.

## Keys

Only the typed scalar keys below (plus the grouped `llm.*` fields) are exposed through
`config get/set`. Harness enablement lives under [`adept harness`](/docs/reference/commands/#harness)
and library remotes under [`adept library`](/docs/reference/commands/#library); they are stored
in the same file but managed by their own commands.

| Key | Type | Default | Effect |
| --- | --- | --- | --- |
| `mode` | `symlink` \| `copy` | `symlink` | How canonical skills materialize into each harness: a symlink back to the canonical file, or a copied file. See [Layouts](/docs/concepts/layouts/). |
| `scan.onInstall` | `true` \| `false` | on when an LLM provider is configured | Run the safety scan and LLM intent pass before `skill install`. |
| `scan.blockSeverity` | `critical` \| `high` \| `medium` | `critical` | Lowest scan severity that aborts an install. |

The `llm` provider is grouped. `config list` surfaces its three fields for a complete view, but
you set them through the `config llm` subcommand, not `config set`:

| Key | Effect |
| --- | --- |
| `llm.provider` | Current provider name (`anthropic` or `ollama`), or `(unset)`. |
| `llm.model` | Model override; provider default when unset. |
| `llm.endpoint` | Endpoint override, for example a self-hosted Ollama. |

## LLM provider config

Safety scans can add an LLM intent pass on top of the static rules. `config llm` records which
provider to use. API keys are never stored in the config: they are read from the environment at
call time.

```bash
export ANTHROPIC_API_KEY=sk-...
adept config llm set anthropic          # provider: anthropic | ollama
adept config llm test                   # health-ping the configured provider
adept config llm unset                  # forget the provider

# override the model or endpoint
adept config llm set anthropic --model claude-3-5-sonnet-latest
adept config llm set ollama --endpoint http://localhost:11434
```

Supported providers are `anthropic` and `ollama`. Setting a provider flips `scan.onInstall` on
by default. See [Safety scans and LLM config](/docs/guides/safety-scans/) for how the pass is
used.

## Scope: project vs global

By default `config` operates on the current project. Pass `--global` to read or write the
home-level scope instead (the harness config adept applies outside any project):

```bash
adept config list --global
adept config set mode copy --global
```

See [Scopes](/docs/concepts/scopes/) for when each applies.

## Where config lives on disk

Project config is a single JSON file at:

```
<project>/.adeptability/config.json
```

It holds the typed keys above alongside harness state, materialization mode, and adapters.
Writes are atomic. Prefer the `config` commands over hand-editing so values stay validated.
