# Vendoring a library in-repo

By default a consumed library clones into `<project>/.adeptability/libs/<name>/`, which is
auto-gitignored. Sometimes you instead want that clone committed — for air-gapped builds,
hermetic CI, or so a fresh clone of your repo works with zero network access.

## Commit the clone

Delete the `libs/` (and, if you also want staging committed, `staging/`) line from
`.adeptability/.gitignore` and commit the clone directory itself:

```bash
# Remove the auto-added "libs/" line from .adeptability/.gitignore, then:
git add .adeptability/.gitignore .adeptability/libs && git commit -m "chore: vendor team-skills library"
```

Trade-off: the repo now carries the full skill content instead of just the `{name, remote,
ref}` pointer, and updates land as a normal commit (`adept library update` still works — it
edits the clone in place) instead of a re-clone on every machine.

## CI-hermetic use of `ADEPT_LIBRARY`

Point `ADEPT_LIBRARY` at an in-repo path if you additionally want the *machine store* itself
(rather than just the project's `libs/`) to stay inside the checkout — e.g. when a global-scope
library also needs to resolve without touching `~/.adeptability`:

```yaml
# .github/workflows/ci.yml (excerpt)
env:
  ADEPT_LIBRARY: ${{ github.workspace }}/.adept-lib
steps:
  - uses: actions/checkout@v4
  - run: adept status      # exits 2 on drift
  - run: adept sync --dry-run
```

!!! tip "Materialization mode"
    When vendoring for CI, consider `copy` mode
    (`adept config set mode copy`) if your build tools don't follow symlinks.
