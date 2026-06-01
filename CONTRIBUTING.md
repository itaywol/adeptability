# Contributing to adeptability

## Development

```bash
git clone https://github.com/itaywol/adeptability
cd adeptability
go build ./...
go test -race ./...
```

## Conventional commits

Commits follow [Conventional Commits](https://www.conventionalcommits.org/). Release-please uses commit types to compute the next semver:

- `feat: …` — minor bump
- `fix: …` — patch bump
- `perf: …` — patch bump
- `feat!: …` or `BREAKING CHANGE:` — major bump
- `refactor:`, `docs:`, `chore:`, `test:`, `ci:` — no bump

## Tests

Every package must ship tests. Coverage gate ≥80% on:

- `internal/render`
- `internal/status`
- `internal/budget`
- `internal/lockfile`
- `internal/canonical`

Run with `-race`:

```bash
go test -race ./...
```

## Adding a built-in harness

1. Implement `adept.HarnessAdapter` in `internal/render/<id>/`.
2. Add golden fixtures under `testdata/`.
3. Register in `cmd/adept/wire.go`.
4. Document in README harness table.

## Adding a config-driven harness

Drop a `<id>.yaml` adapter in `~/.adeptability/adapters/` matching `pkg/adeptschema/adapter.schema.json`. No code changes; no rebuild.

## License

By contributing, you agree your contributions are MIT-licensed.
