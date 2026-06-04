#!/usr/bin/env sh
# adeptability installer
# Usage: curl -fsSL https://raw.githubusercontent.com/itaywol/adeptability/main/scripts/install.sh | sh
# Env:
#   ADEPT_VERSION   override version tag (default: latest)
#   ADEPT_BIN_DIR   install location (default: /usr/local/bin)
#   ADEPT_NO_VERIFY skip cosign verification (default: 0)

set -eu

REPO="itaywol/adeptability"
BIN_DIR="${ADEPT_BIN_DIR:-/usr/local/bin}"
VERSION="${ADEPT_VERSION:-latest}"

err() { printf 'error: %s\n' "$1" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "missing required tool: $1"; }

need curl
need tar
need uname

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) err "unsupported arch: $ARCH_RAW" ;;
esac

case "$OS" in
  linux|darwin) ;;
  *) err "unsupported OS: $OS (on Windows: 'go install', or grab the release .zip)" ;;
esac

if [ "$VERSION" = "latest" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
  [ -n "$VERSION" ] || err "could not resolve latest version"
fi

VER_NUM="${VERSION#v}"
TARBALL="adeptability_${VER_NUM}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${TARBALL}"
SUMS_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

printf 'downloading %s\n' "$URL"
curl -fsSL "$URL" -o "${TMP}/${TARBALL}"
curl -fsSL "$SUMS_URL" -o "${TMP}/checksums.txt"

# Verify sha256
if command -v sha256sum >/dev/null 2>&1; then
  ( cd "$TMP" && grep " ${TARBALL}\$" checksums.txt | sha256sum -c - ) \
    || err "sha256 verification failed"
elif command -v shasum >/dev/null 2>&1; then
  ( cd "$TMP" && grep " ${TARBALL}\$" checksums.txt | shasum -a 256 -c - ) \
    || err "sha256 verification failed"
fi

# Verify cosign signature when available
if [ "${ADEPT_NO_VERIFY:-0}" != "1" ] && command -v cosign >/dev/null 2>&1; then
  curl -fsSL "${URL}.sig" -o "${TMP}/${TARBALL}.sig"
  curl -fsSL "${URL}.pem" -o "${TMP}/${TARBALL}.pem"
  cosign verify-blob \
    --certificate "${TMP}/${TARBALL}.pem" \
    --signature "${TMP}/${TARBALL}.sig" \
    --certificate-identity-regexp 'https://github\.com/itaywol/adeptability/' \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
    "${TMP}/${TARBALL}" >/dev/null 2>&1 \
    || err "cosign verification failed (set ADEPT_NO_VERIFY=1 to skip)"
else
  printf 'note: cosign not installed, skipping signature verification\n'
fi

tar -xzf "${TMP}/${TARBALL}" -C "$TMP"

if [ ! -w "$BIN_DIR" ]; then
  printf 'installing to %s (requires sudo)\n' "$BIN_DIR"
  sudo install -m 0755 "${TMP}/adept" "${BIN_DIR}/adept"
else
  install -m 0755 "${TMP}/adept" "${BIN_DIR}/adept"
fi

printf 'installed adept %s to %s/adept\n' "$VERSION" "$BIN_DIR"
"${BIN_DIR}/adept" --version || true
