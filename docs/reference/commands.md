# Command reference

Every command and flag, generated faithfully from the cobra definitions. `adept <cmd> --help`
is always the authoritative, version-matched source — this page mirrors it.

The surface is **five top-level verbs** (`init`, `status`, `sync`, `sync-from`, `diff`) plus
subcommand groups (`harness`, `skill`, `library`, `config`, `hook`, `exchange`).

## Global flags

Available on every command:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--json` | `false` | Emit machine-readable JSON output |
| `--log-level <lvl>` | `info` | `debug` \| `info` \| `warn` \| `error` |
| `--project <path>` | current dir | Project root |
| `--library <path>` | `$ADEPT_LIBRARY` or `~/.adeptability` | Library root |

**Environment variables:** `ADEPT_LIBRARY` (library root), `ADEPT_EXCHANGE_SERVER`,
`ADEPT_EXCHANGE_TOKEN` (exchange defaults), `ANTHROPIC_API_KEY` (LLM key, read at call time).

**Exit codes:** `0` success · `2` drift/dirty state (`status`) or merge conflict · `1` any
other error.

---

## init

```
adept init [flags]
```

Creates `.adeptability/{skills,base}/`, writes `config.json`, optionally clones a remote
library, and adopts any pre-existing harness files (`.claude/`, `.cursor/`, `.opencode/`,
`AGENTS.md`, `.github/instructions/`) it finds on disk.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--from <url>` | — | Remote library URL (git remote or local path) to clone |
| `--ref <branch>` | `main` | Branch or tag in the remote library |
| `--name <local>` | `default` | Local name for the library added via `--from` |
| `--mode symlink\|copy` | `symlink` | Harness materialization mode |
| `--git-hook fail\|fix` | — | Install a pre-commit drift hook (bare `--git-hook` = `fail`) |
| `--no-default-skills` | `false` | Skip seeding the bundled default skills |
| `--as-library` | `false` | Initialize a publishable library (skills at `<root>/skills/`) |

With `--as-library`, harness adoption, mode, and the bundled default skills are skipped — see
[Layouts](../concepts/layouts.md).

---

## status

```
adept status [--fetch]
```

Project state at a glance: init state, configured libraries (with on-disk / update status),
enabled harnesses, and a one-line drift summary. **Exits `2`** when not initialized, a library
is missing, or a harness has drifted — so scripts and CI can branch on it.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--fetch` | `false` | Fetch library remotes to detect available updates (network) |

---

## sync

```
adept sync [flags]
```

Push canonical skills to every enabled harness (the primary "publish" verb).

| Flag | Default | Purpose |
| --- | --- | --- |
| `--harness <id>` | all enabled | Limit to specific harness ids (repeatable / comma-list) |
| `--force` | `false` | Overwrite drifted harness files |
| `--dry-run` | `false` | Print what would change, write nothing |

---

## sync-from

```
adept sync-from [flags]
```

Reverse of `sync`: walk each enabled harness's on-disk state and adopt edits back into
canonical.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--harness <id>` | — | Limit to specific harness ids |
| `--all` | `false` | Non-interactive: adopt from every harness (strategy=first) |
| `--dry-run` | `false` | Report what would be imported, write nothing |
| `--force` | `false` | Overwrite existing project canonical content |

---

## diff

```
adept diff [--harness <id>]
```

Show drift between canonical skills and harness outputs (synced / drifted / missing /
conflict, per harness).

| Flag | Default | Purpose |
| --- | --- | --- |
| `--harness <id>` | all enabled | Limit to specific harness ids |

---

## harness

Manage which harnesses are enabled in this project.

```
adept harness add <id>       # Enable a harness for this project
adept harness remove <id>    # Disable a harness for this project
adept harness list           # List harnesses (registered + enabled state)
```

`add`/`remove` take exactly one harness id. Run `adept harness list` for the live registry of
valid ids (`claude-code`, `cursor`, `codex`, `copilot`, `opencode`, and 50+ generic adapters).

---

## skill

Manage canonical skills (local + skills.sh / GitHub).

### skill add

```
adept skill add <id> [flags]
```

Create a new project skill from scratch, or import an existing directory.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--from <path>` | — | Import an existing skill directory into the project |
| `--template default\|triage` | `default` | Scaffold template — `triage` is a loop-discovery skill (Read → Judge → Write → Hand off → Stop) |
| `--edit` | `false` | Open the new `SKILL.md` in `$EDITOR` after creation |
| `--publish` | `false` | In a library project, add to the published `skills/` (default: private dev-canonical) |

### skill edit

```
adept skill edit <id>
```

Open the project skill's `SKILL.md` in `$EDITOR`.

### skill remove

```
adept skill remove <id>
```

Remove a skill from the project canonical.

### skill list

```
adept skill list [--project-only]
```

List skills resolved for this project (project canonical + libraries).

| Flag | Default | Purpose |
| --- | --- | --- |
| `--project-only` | `false` | Only show skills present in the project canonical |

### skill install

```
adept skill install <owner>/<repo>[#ref]/<skill> [flags]
```

Install a skill from skills.sh / GitHub into the project canonical. Runs a safety scan and
pins upstream provenance (repo, ref, SHA, content hash).

| Flag | Default | Purpose |
| --- | --- | --- |
| `--yes` | `false` | Skip the install preview confirmation |
| `--allow-unsafe` | `false` | Install even when the sandbox sniff flags suspicious content |

### skill update

```
adept skill update [<id>]
```

Re-resolve locked external skills against upstream and bump their SHA. Omit `<id>` to update
all locked external skills.

### skill info

```
adept skill info <owner>/<repo>[#ref]/<skill>
```

Show repo, license, stars, installs, and current SHA for a skill.

### skill search

```
adept skill search <query> [--limit N]
```

Search skills.sh for installable skills.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--limit <N>` | `20` | Max results to display |

### skill check

```
adept skill check <target> [flags]
```

Scan a skill for safety issues without installing. `<target>` is `project`,
`library:<name>:<id>`, or `<owner>/<repo>/<skill>`.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--format table\|markdown\|json` | `table` | Output format |
| `--llm` | `false` | Force the LLM intent pass (errors if no provider configured) |
| `--no-llm` | `false` | Skip the LLM intent pass even when a provider is configured |

---

## agent

Manage canonical agents (subagents). Agents are single files at
`.adeptability/agents/<id>.md`; `adept sync` renders them into every enabled harness that
supports agent definitions (`.claude/agents/`, `.opencode/agents/`, `.cursor/agents/`,
`.github/agents/*.agent.md`, `.codex/agents/*.toml`), and `adept sync-from` adopts
harness-side agent edits back. Harnesses without an agent concept skip them with a warning.

### agent add

```
adept agent add <id> [flags]
```

Create a new project agent from a best-practice template, or import an existing file.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--from <path>` | — | Import an existing agent `.md` file into the project |
| `--template default\|evaluator` | `default` | Scaffold template — `evaluator` encodes the adversarial maker–checker reviewer pattern |
| `--edit` | `false` | Open the new agent file in `$EDITOR` after creation |

### agent edit

```
adept agent edit <id>
```

Open the project agent's file in `$EDITOR`.

### agent remove

```
adept agent remove <id>
```

Remove an agent from the project canonical.

### agent list

```
adept agent list
```

List agents in the project canonical (`ID`, `MODE`, `TARGETS`, `DESCRIPTION`).

### agent check

```
adept agent check <id> [flags]
```

One report over three passes: the static safety scanner (same malicious-instruction rules as
`skill check`), the optional LLM intent pass, and the agent best-practice linter
(`AGENT-LINT-*`: schema, trigger-shaped description, tool hygiene, boundaries, broken file
references). High or critical findings exit `2` so CI can gate on it.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--format table\|markdown\|json` | `table` | Output format |
| `--llm` | `false` | Force the LLM intent pass (errors if no provider configured) |
| `--no-llm` | `false` | Skip the LLM intent pass even when a provider is configured |

---

## loop

Compose a loop — a scheduled system that discovers work, hands it to agents, verifies with
an independent evaluator, persists state, and reschedules. A loop is not a synced resource:
it is a composition of things adept already manages (a discovery skill + an evaluator agent)
plus a schedule that lives in CI or the harness. See the [loops guide](../guides/loops.md).

### loop add

```
adept loop add <id> [flags]
```

Scaffolds the composition in one shot: a `<id>` discovery skill (triage template,
`activation: manual` so only the automation invokes it), a `<id>-reviewer` evaluator agent
(adversarial template), optionally a GitHub Actions cron skeleton — then prints the
first-loop checklist (state file, isolation, token cap, human review).

| Flag | Default | Purpose |
| --- | --- | --- |
| `--workflow` | `false` | Also write `.github/workflows/adept-loop-<id>.yml` (cloud cron skeleton; never overwrites) |
| `--edit` | `false` | Open the discovery skill in `$EDITOR` after creation |

---

## library

Manage the project's library remotes. See [Libraries](../concepts/libraries.md).

### library add

```
adept library add <name> --from <url> [--ref <branch>]
```

Clone a remote library into the project's library store and record the reference.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--from <url>` | — (required) | Remote URL (git remote or local path) |
| `--ref <branch>` | `main` | Branch or tag to track |

### library update

```
adept library update [name] [--yes]
```

Fetch newer skills from configured libraries (prompts before applying). Omit `name` to update
all.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--yes` | `false` | Apply updates without prompting |

### library remove

```
adept library remove <name> [--purge]
```

Drop a library reference from the project.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--purge` | `false` | Also delete the local clone directory |

### library list

```
adept library list
```

List configured libraries.

---

## config

Read or write strict-typed project configuration.

```
adept config list                 # List configurable keys + current values
adept config get <key>            # Print one config value
adept config set <key> <value>    # Set one config value (validated)
adept config unset <key>          # Clear one value back to default
```

Configurable keys:

| Key | Allowed | Meaning |
| --- | --- | --- |
| `mode` | `symlink` \| `copy` | Harness materialization mode |
| `scan.onInstall` | `true` \| `false` | Run the safety scan + LLM pass before `skill install` (default: on when an LLM is configured) |
| `scan.blockSeverity` | `critical` \| `high` \| `medium` | Lowest scan severity that aborts an install (default: `critical`) |

### config llm

```
adept config llm set <provider> [--model <m>] [--endpoint <url>]
adept config llm unset
adept config llm test
```

Configure the LLM provider used by safety scans. The **API key is read from the environment at
call time** and never stored in config.

| Flag | Purpose |
| --- | --- |
| `--model <m>` | Override the provider's default model |
| `--endpoint <url>` | Override the default endpoint (e.g. self-hosted Ollama) |

Providers: `anthropic`, `ollama`. `test` health-pings the configured provider.

---

## hook

Manage the git pre-commit drift hook. See the [Git drift hook guide](../guides/git-hook.md).

```
adept hook install [--mode fail|fix]    # Install a pre-commit hook that blocks commits on drift
adept hook run [--fix]                  # Drift gate invoked by the installed hook
```

| Command / Flag | Default | Purpose |
| --- | --- | --- |
| `install --mode fail\|fix` | `fail` | Hook behavior on drift |
| `run --fix` | `false` | Auto-adopt/re-render drift and re-stage instead of failing |

---

## exchange

The team **expertise billboard** — a request/answer board. One person hosts `serve`; everyone
else registers with the bootstrap token and posts/answers requests. See
[Exchange](../exchange.md). A shared `--server <url>` flag applies to every client subcommand
(default: `$ADEPT_EXCHANGE_SERVER` or the last registered server).

### exchange serve

```
adept exchange serve [flags]
```

Host the billboard server. Mints a bootstrap token on first run and prints it once.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--addr <addr>` | `:4639` | Listen address |
| `--db fs\|memory` | `fs` | Storage driver |
| `--data <dir>` | `<library>/exchange-data` | Data directory for the `fs` driver |
| `--rotate-bootstrap` | `false` | Mint a new bootstrap token (invalidates the old one) |

### exchange register

```
adept exchange register --bootstrap <token> [--handle <name>]
```

Register a handle and store a bearer token.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--bootstrap <token>` | — (required) | Bootstrap token from `serve` |
| `--handle <name>` | current OS user | Your handle |

### exchange submit

```
adept exchange submit --title <t> [flags]
```

Submit a request for expertise.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--title <t>` | — (required) | Short request title |
| `--body <text>` | — | Request detail |
| `--assignee <handle>` | — | Handle whose expertise you want (repeatable) |
| `--tag <tag>` | — | Topic tag (repeatable) |

### exchange list

```
adept exchange list [--mine] [--status <s>]
```

List billboard requests.

| Flag | Default | Purpose |
| --- | --- | --- |
| `--mine` | `false` | Only requests you authored or are assigned to |
| `--status <s>` | — | Filter: `attention-required` \| `in-progress` \| `closed` |

### exchange show / respond / close / reopen

```
adept exchange show <id>                   # Show a request and its responses
adept exchange respond <id> --body <text>  # Post a response (--body required)
adept exchange close <id>                  # Close a request you authored
adept exchange reopen <id>                 # Reopen a request you authored
```

### exchange token

```
adept exchange token rotate    # Rotate your bearer token (old one stops working immediately)
```

### exchange status / recommendation

```
adept exchange status                      # Report local setup state (configured/registered/dismissed)
adept exchange recommendation dismiss      # Stop the exchange setup recommendation (per-user)
adept exchange recommendation undismiss    # Re-enable it
```

---

## completion

```
adept completion zsh > "${fpath[1]}/_adept"
adept completion bash > /etc/bash_completion.d/adept
adept completion fish > ~/.config/fish/completions/adept.fish
```

Standard cobra shell completion for bash, zsh, and fish.
