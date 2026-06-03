# Security Policy

## Supported versions

`adept` is pre-1.0 and ships from a single line of development. Security fixes land on the
latest released minor version. Please test against the latest release before reporting.

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Report privately via GitHub's [private vulnerability reporting](https://github.com/itaywol/adeptability/security/advisories/new)
("Report a vulnerability" under the **Security** tab). If that is unavailable, email the
maintainer listed in [`CODEOWNERS`](.github/CODEOWNERS).

Please include:

- affected version (`adept --version`) and platform,
- a description of the issue and its impact,
- steps to reproduce or a proof of concept.

You can expect an acknowledgement within **72 hours** and a remediation plan once the report
is triaged. Please give us a reasonable window to ship a fix before any public disclosure.
We'll credit reporters who want it.

## Security-relevant surface

`adept` runs locally and touches a few trust boundaries worth knowing about when you report:

- **Skill content from the internet** — `adept skill install` / `library add` fetch skills
  from GitHub and skills.sh. Installs are pinned to a resolved SHA and content-hashed; a
  static safety scanner (and optional LLM intent pass) runs before any write, and **critical
  findings hard-block** the install unless `--allow-unsafe` is passed.
- **Filesystem writes** — sync materializes files into harness directories via symlink or
  copy. Reports about path traversal, symlink escape, or clobbering files outside the
  project root are in scope.
- **`git` invocation** — adept shells out to `git`; injection via crafted refs/URLs is in scope.
- **Secrets** — API keys are read from the environment at call time and are never written to
  `config.json` or any project file. Reports of secret leakage are in scope.
- **Release integrity** — release binaries are checksummed, signed with cosign, and ship SLSA
  provenance. Report any gap in the supply-chain verification path.
