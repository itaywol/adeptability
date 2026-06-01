# Release Checklist

How to ship adeptability publicly when you're ready.

## Current state (default)

The release pipeline builds, signs, and attaches multi-arch binaries to the
GitHub Release on every `v*` tag. External publishing channels (Homebrew,
Docker GHCR, Scoop, WinGet, npm) are **disabled by default** so private
releases don't fail on missing tokens or non-existent repos.

What runs today on `git push origin v0.X.Y`:

- [x] Build darwin/linux/windows × amd64/arm64 binaries
- [x] sha256 checksums
- [x] cosign keyless signatures (`.sig` + `.pem`)
- [x] SLSA provenance
- [x] GitHub Release with all artifacts attached

What's skipped until opted in:

- [ ] Homebrew tap formula push
- [ ] Docker images to ghcr.io
- [ ] Scoop bucket
- [ ] WinGet manifest
- [ ] npm wrapper publish

## Going public — minimal launch

1. **Flip repo visibility**:
   ```
   gh repo edit itaywol/adeptability --visibility public --accept-visibility-change-consequences
   ```

2. **Bump version**, push tag:
   ```
   git tag -a v0.X.0 -m "Public launch"
   git push origin v0.X.0
   ```

That's it for a minimal launch — `curl -fsSL https://github.com/itaywol/adeptability/releases/download/v0.X.0/adeptability_..._linux_amd64.tar.gz` works for anyone.

## Enabling Homebrew

1. Flip the tap repo public:
   ```
   gh repo edit itaywol/homebrew-tap --visibility public --accept-visibility-change-consequences
   ```

2. Generate a fine-grained PAT scoped to `itaywol/homebrew-tap` with
   `contents: write`. https://github.com/settings/tokens?type=beta

3. Add it as a repo secret on `itaywol/adeptability`:
   ```
   gh -R itaywol/adeptability secret set HOMEBREW_TAP_GITHUB_TOKEN
   ```

4. Tag a new version. Goreleaser writes the formula to the tap.

5. Users install with:
   ```
   brew install itaywol/tap/adeptability
   ```

## Enabling Docker (GHCR)

1. Flip `vars.DOCKER_PUBLISH` to `1` on the repo:
   ```
   gh -R itaywol/adeptability variable set DOCKER_PUBLISH --body 1
   ```

2. Make the ghcr.io package public after the first push (defaults to private):
   - https://github.com/users/itaywol/packages/container/adeptability/settings → "Change visibility" → Public.

3. Tag a new version.

## Enabling Scoop / WinGet / npm

Each has its own publishers section (currently omitted from `.goreleaser.yaml`). Add when ready:

- **Scoop**: add a `scoops:` block pointing at `itaywol/scoop-bucket` (create the repo first; bucket repos must be public for users to add).
- **WinGet**: add a `winget:` block; needs a separate publish step using `vedantmgoyal2009/winget-releaser`.
- **npm**: publish the `scripts/npm/` wrapper after a release; bump the wrapper version to match.

## Disabling everything for a private dogfood release

Default state. Nothing extra to do — just push the tag.

## Sanity smoke after every release

```
gh -R itaywol/adeptability release view v0.X.Y
gh -R itaywol/adeptability release download v0.X.Y -p '*linux_amd64.tar.gz'
tar -xzf adeptability_*_linux_amd64.tar.gz
./adept --version
cosign verify-blob \
  --certificate adeptability_*_linux_amd64.tar.gz.pem \
  --signature   adeptability_*_linux_amd64.tar.gz.sig \
  --certificate-identity-regexp 'https://github\.com/itaywol/adeptability/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  adeptability_*_linux_amd64.tar.gz
```
