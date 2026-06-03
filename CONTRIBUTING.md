# Contributing to adeptability

Thanks for contributing! This guide is intentionally short — for deeper architecture and
invariants, read [AGENTS.md](AGENTS.md).

## Prerequisites

- Go **1.25+**
- `git`
- Optional: [`golangci-lint`](https://golangci-lint.run/) for local linting,
  [`goreleaser`](https://goreleaser.com/) only if you touch the release config.

## Development

```bash
git clone https://github.com/itaywol/adeptability
cd adeptability
go build ./...
go test -race ./...
```

Build and try the binary:

```bash
go build -o /tmp/adept ./cmd/adept
/tmp/adept --help
```

## Before you open a PR

Run the same gates CI runs:

```bash
gofmt -l .            # must print nothing — run `gofmt -w .` to fix
go vet ./...
golangci-lint run     # config in .golangci.yml
go test -race ./...
```

- Keep PRs focused. Update docs and golden fixtures (`testdata/`) in the same PR as the
  change that affects them.
- New behavior needs tests. Bug fixes should include a regression test.
- Wrap errors with context (`fmt.Errorf("...: %w", err)`) and compare sentinels from
  `pkg/adept/errors.go` with `errors.Is` — don't match on error strings.

## Conventional commits

Commits follow [Conventional Commits](https://www.conventionalcommits.org/). `release-please`
uses the type to compute the next semver:

| Type | Bump |
| --- | --- |
| `feat:` | minor |
| `fix:` / `perf:` | patch |
| `feat!:` or `BREAKING CHANGE:` | major |
| `refactor:` `docs:` `chore:` `test:` `ci:` | none |

## Tests

Table-driven tests with `testify/require` are the default. Every package should ship tests;
keep coverage ≥80% on `internal/render`, `internal/status`, `internal/budget`,
`internal/locks`, and `internal/canonical`.

```bash
go test -race ./...            # full suite
go test -run E2E ./cmd/adept   # end-to-end against the built binary
```

## Adding a built-in harness

1. Implement `adept.HarnessAdapter` in `internal/render/<id>/`.
2. Add golden fixtures under that package's `testdata/`.
3. Register it in `internal/cli/deps.go` (`registerBuiltinAdapters`).
4. Document it in the README harness table.

## Adding a config-driven harness

Drop a `<id>.yaml` adapter in `~/.adeptability/adapters/` matching
`pkg/adeptschema/adapter.schema.json`. No code changes, no rebuild.

## Reporting bugs & security issues

- Functional bugs: open a [GitHub issue](https://github.com/itaywol/adeptability/issues).
- Security vulnerabilities: **do not** open a public issue — follow [SECURITY.md](SECURITY.md).

## License

By contributing, you agree your contributions are licensed under the [MIT License](LICENSE).
