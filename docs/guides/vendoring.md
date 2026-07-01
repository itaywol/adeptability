# Vendoring a library in-repo

By default a consumed library lives in the per-machine store (`~/.adeptability/libs/…`) and is
resolved by reference. Sometimes you instead want the library content to live **inside the
repo** — for air-gapped builds, hermetic CI, or so a clone works with zero network access.

You do this by pointing the **library root** at a path inside your repo.

## Point the library root at a repo path

`adept` resolves the library root from, in order:

1. The `--library <path>` global flag.
2. The `ADEPT_LIBRARY` environment variable.
3. The default `~/.adeptability`.

To vendor, point it at an in-repo directory such as `./.adept-lib`:

```bash
# Per-invocation:
adept --library ./.adept-lib sync

# Or for the whole session / CI job:
export ADEPT_LIBRARY="$PWD/.adept-lib"
adept sync
```

Libraries added while this is set clone into `./.adept-lib/libs/<name>/`, which you can then
commit:

```bash
export ADEPT_LIBRARY="$PWD/.adept-lib"
adept library add team-skills --from git@github.com:acme/skills.git --ref main
git add .adept-lib && git commit -m "chore: vendor team-skills library"
```

## CI usage

Set `ADEPT_LIBRARY` to the vendored path so builds never reach the network:

```yaml
# .github/workflows/ci.yml (excerpt)
env:
  ADEPT_LIBRARY: ${{ github.workspace }}/.adept-lib
steps:
  - uses: actions/checkout@v4
  - run: adept status      # exits 2 on drift
  - run: adept sync --dry-run
```

## Trade-offs

| | Referenced (default) | Vendored in-repo |
| --- | --- | --- |
| Repo size | Small (pointer only) | Larger (full skill content) |
| Offline clone works | No (needs `adept library add`) | Yes |
| Update flow | `adept library update` | Commit new content to the repo |
| Best for | Most teams | Air-gapped / hermetic builds |

!!! tip "Materialization mode"
    When vendoring for CI, consider `copy` mode
    (`adept config set mode copy`) if your build tools don't follow symlinks.
