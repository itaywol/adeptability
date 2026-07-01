# Install

`adept` is a single self-contained binary. Pick whichever channel fits your platform.

## Go

```bash
go install github.com/itaywol/adeptability/cmd/adept@latest
```

Requires Go 1.25+. Installs to `$(go env GOBIN)` (or `$(go env GOPATH)/bin`) — make sure it's
on your `PATH`.

## Homebrew (macOS / Linux)

```bash
brew install itaywol/tap/adeptability
```

## curl installer

```bash
curl -fsSL https://raw.githubusercontent.com/itaywol/adeptability/main/scripts/install.sh | sh
```

The installer downloads the latest release archive, verifies its cosign signature, and drops
the binary in `/usr/local/bin`. Environment overrides:

| Variable | Default | Purpose |
| --- | --- | --- |
| `ADEPT_VERSION` | `latest` | Install a specific release tag |
| `ADEPT_BIN_DIR` | `/usr/local/bin` | Install location |
| `ADEPT_NO_VERIFY` | `0` | Set to `1` to skip cosign verification |

## Docker (GHCR)

```bash
docker run --rm -v "$PWD:/work" -w /work ghcr.io/itaywol/adeptability:latest --help
```

Mount your project at `/work` so `adept` operates on your repo.

## Pre-built binaries

Every release ships cross-compiled archives for darwin / linux / windows × amd64 / arm64,
plus `checksums.txt`, cosign signatures, and build-provenance attestation. Download from the
[latest release](https://github.com/itaywol/adeptability/releases/latest).

!!! note "Other package managers"
    Scoop, WinGet, and an npm wrapper are prototyped but **not published yet**. If you want
    one of them, add a 👍 to the
    [additional package managers issue](https://github.com/itaywol/adeptability/issues) so we
    know it's worth committing to maintaining.

## Verify the install

```bash
adept --version
adept --help
```

## Shell completion

`adept` ships cobra completion for bash, zsh, and fish:

```bash
adept completion zsh > "${fpath[1]}/_adept"   # zsh
adept completion bash > /etc/bash_completion.d/adept
adept completion fish > ~/.config/fish/completions/adept.fish
```
