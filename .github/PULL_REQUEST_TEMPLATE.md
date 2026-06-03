<!--
Title must follow Conventional Commits, e.g. `feat: add jetbrains harness`.
release-please derives the next version from the squash-merge title.
-->

## What & why

<!-- What does this change do, and why is it needed? Link any related issue: Closes #123 -->

## Type of change

- [ ] `fix` — bug fix
- [ ] `feat` — new feature
- [ ] `refactor` / `docs` / `chore` / `test` / `ci`
- [ ] breaking change (`!` / `BREAKING CHANGE:`)

## Checklist

- [ ] `gofmt -l .` prints nothing
- [ ] `go vet ./...` passes
- [ ] `golangci-lint run` passes
- [ ] `go test -race ./...` passes
- [ ] Added/updated tests for the change
- [ ] Updated golden fixtures (`testdata/`) if output changed — and explained why
- [ ] Updated docs (README / AGENTS.md) if behavior or commands changed
