# Canonical agents

Skills make *knowledge* portable; agents make *delegation* portable. An agent (a.k.a.
subagent) is a named specialist a coding harness can hand work to — a reviewer, a test
runner, a migration worker — defined by a description (the delegation trigger), a system
prompt, and a tool policy.

Every major harness grew its own agent format. adept gives them one canonical source.

## The canonical form

One file per agent, in both project layouts:

```
.adeptability/agents/<id>.md
```

```markdown
---
id: pr-reviewer                # filename is authoritative, like skill directory names
description: Adversarially reviews drafted changes. Use proactively before every commit.
mode: subagent                 # subagent | primary | all
tools: [Read, Grep, Bash]      # allowlist; omitted = inherit everything
disallowed-tools: [Write]
model: inherit                 # passed through verbatim
targets: []                    # empty = every enabled harness
harness:                       # per-harness overrides, merged last-wins at render
  cursor: { readonly: true }
  codex: { sandbox_mode: read-only }
---
You are an adversarial reviewer. Assume the work is broken until proven otherwise.
...
```

The body is the agent's **entire system prompt** — subagents do not inherit the main
conversation's system prompt. Identity is `(id, content-hash)`, exactly like skills. Unlike
skills, agents have no sidecars (no harness supports attaching files to an agent definition)
and no published/private library split — they render to the maintainer's harnesses only.

## Where agents render

| Harness | Output | Notes |
| --- | --- | --- |
| Claude Code | `.claude/agents/<id>.md` | Richest mapping: tools, disallowedTools, model |
| OpenCode | `.opencode/agents/<id>.md` | Filename = name; `mode` maps natively; tool lists warn-drop (use a `permission` override) |
| Cursor | `.cursor/agents/<id>.md` | All-optional frontmatter; `readonly` via override |
| Copilot | `.github/agents/<id>.agent.md` | `tools` allowlist maps; per-file even though Copilot skills aggregate |
| Codex | `.codex/agents/<id>.toml` | TOML; body becomes `developer_instructions` (required — empty bodies fail) |

Fields a harness cannot express are **warn-dropped at sync**, never silently. Harnesses with
no agent concept (the generic vercel-matrix targets) skip agents with a warning.

!!! note "Cursor reads `.claude/agents/` too"
    Syncing agents to both `claude-code` and `cursor` makes each agent appear twice inside
    Cursor. Scope such agents with `targets:` — sync warns when this applies.

## Drift, import, and checks

Agents ride the same machinery as skills:

- `adept status` / `adept diff` include rendered agent files in the per-harness drift report.
- `adept sync-from` imports harness-side agent edits back to canonical (foreign,
  harness-only fields are warn-dropped and reported — the `harness:` block is never
  reconstructed, matching skill imports).
- `adept agent check <id>` runs the safety scanner (malicious-instruction patterns + optional
  LLM pass) and a best-practice linter: trigger-shaped descriptions, one job per agent,
  generator/evaluator separation, explicit boundaries, tool hygiene, and body file references
  that actually exist. High/critical findings exit `2`.

There is no 3-way merge for agents in v1: sync overwrites the rendered files and drift warns
first.

See the [command reference](../reference/commands.md#agent) for the full `adept agent`
surface, and the bundled `authoring-adept-agents` skill (seeded by `adept init`) for the
authoring playbook your harness agents read.
