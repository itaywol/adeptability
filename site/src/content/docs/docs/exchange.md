---
title: "Exchange — the team expertise billboard"
description: "The exchange is a lightweight request/answer board for sharing expertise across a team."
---

The **exchange** is a lightweight request/answer board for a team. Someone hosts a server;
everyone else registers and posts or answers requests for expertise. The server is passive
storage plus auth — it never runs an agent. Agents participate by calling the commands with
`--json`.

It pairs with the bundled `expertise-exchange` skill, which prompts (once) to set up an
exchange and can post/answer on your behalf.

## Topology

```
        ┌─────────────────────┐
        │  adept exchange serve│   one host, passive storage + auth
        └──────────┬──────────┘
                   │ bootstrap token
   ┌───────────────┼───────────────┐
   ▼               ▼               ▼
register        register        register        (each teammate)
submit / respond / list / show / close / reopen
```

## Host the server

```bash
adept exchange serve
```

On first run it mints a **bootstrap token** and prints it once — teammates need it to
register. Defaults: listens on `:4639`, `fs` storage driver, data under
`<library>/exchange-data`.

```bash
adept exchange serve --addr :8080 --db fs --data ./exchange-data
adept exchange serve --rotate-bootstrap        # mint a fresh bootstrap token
```

:::caution[TLS]
`serve` speaks plain HTTP. Run it on a trusted network, and terminate TLS at a reverse
proxy when exposing it beyond the LAN.
:::

## Register

```bash
adept exchange register --bootstrap <token> --handle alice
```

Stores a bearer token locally (under the library's `exchange/` dir, one file per server host,
mode `0600`). `--handle` defaults to your OS username.

The server URL resolves from `--server`, then `$ADEPT_EXCHANGE_SERVER`, then the last
registered server.

## Post and answer requests

```bash
adept exchange submit --title "How do we rotate DB creds?" \
                      --body "Staging vs prod differ; where's the runbook?" \
                      --assignee bob --tag ops

adept exchange list                         # all requests
adept exchange list --mine                  # yours (authored or assigned)
adept exchange list --status attention-required
adept exchange show 42                      # a request and its responses
adept exchange respond 42 --body "Runbook: infra/docs/creds.md; prod needs the vault role."
adept exchange close 42                     # author closes it
adept exchange reopen 42
```

## Manage your token

```bash
adept exchange token rotate     # old token stops working immediately
```

## Setup recommendation

The `expertise-exchange` skill checks local setup state and may prompt you to configure an
exchange. Control that prompt:

```bash
adept exchange status                       # configured / registered / dismissed
adept exchange recommendation dismiss       # stop the prompt (per-user)
adept exchange recommendation undismiss     # bring it back
```

## Environment overrides

| Variable | Purpose |
| --- | --- |
| `ADEPT_EXCHANGE_SERVER` | Default billboard server URL |
| `ADEPT_EXCHANGE_TOKEN` | Override the on-disk bearer token |
