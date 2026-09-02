#!/bin/sh

set -eu

repository="fortrabbit/frbit-cli"

fail() {
  printf 'frbit installer: %s\n' "$*" >&2
  exit 1
}

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

case "$(uname -s)" in
  Darwin) os="macOS" ;;
  Linux) os="linux" ;;
  *) fail "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch="arm64" ;;
  amd64|x86_64) arch="amd64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

if [ "$os" = "macOS" ]; then
  if [ "$arch" = "arm64" ]; then
    platform="macOS_AppleSilicon"
  else
    platform="macOS_Intel"
  fi
else
  platform="linux_$arch"
fi

asset="frbit_${platform}.tar.gz"

if [ -n "${FRBIT_RELEASE_BASE_URL:-}" ]; then
  release_base_url=${FRBIT_RELEASE_BASE_URL%/}
elif [ -n "${FRBIT_VERSION:-}" ]; then
  case "$FRBIT_VERSION" in
    v*) tag=$FRBIT_VERSION ;;
    *) tag="v$FRBIT_VERSION" ;;
  esac
  release_base_url="https://github.com/$repository/releases/download/$tag"
else
  release_base_url="https://github.com/$repository/releases/latest/download"
fi

validate_release_base_url() {
  case "$release_base_url" in
    https://github.com/fortrabbit/frbit-cli/releases/download/*|https://github.com/fortrabbit/frbit-cli/releases/latest/download) ;;
    file://*) [ "${FRBIT_INSTALL_TESTING:-}" = "1" ] || fail "FRBIT_RELEASE_BASE_URL must use the official GitHub release host" ;;
    *) fail "FRBIT_RELEASE_BASE_URL must use the official GitHub release host" ;;
  esac
}

validate_release_base_url

if [ -n "${FRBIT_INSTALL_DIR:-}" ]; then
  install_dir=${FRBIT_INSTALL_DIR%/}
elif [ "$(id -u)" -eq 0 ]; then
  install_dir="/usr/local/bin"
else
  install_dir="${HOME:?HOME is not set}/.local/bin"
fi

tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t frbit)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM

printf 'Downloading frbit for %s/%s...\n' "$os" "$arch"
curl -fsSL "$release_base_url/$asset" -o "$tmp_dir/$asset"
curl -fsSL "$release_base_url/checksums.txt" -o "$tmp_dir/checksums.txt"

expected=$(awk -v asset="$asset" '$2 == asset { print $1; exit }' "$tmp_dir/checksums.txt")
[ -n "$expected" ] || fail "no checksum found for $asset"

if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp_dir/$asset" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$tmp_dir/$asset" | awk '{ print $1 }')
else
  fail "sha256sum or shasum is required to verify the download"
fi

[ "$actual" = "$expected" ] || fail "checksum verification failed for $asset"

verify_provenance() {
  if command -v gh >/dev/null 2>&1; then
    gh attestation verify "$tmp_dir/checksums.txt" \
      --repo "$repository" \
      --signer-workflow "$repository/.github/workflows/release.yml" \
      --predicate-type "https://slsa.dev/provenance/v1" \
      >/dev/null || fail "release provenance verification failed"
    return
  fi
  if [ "${FRBIT_VERIFY_PROVENANCE:-}" = "1" ]; then
    fail "GitHub CLI (gh) is required when FRBIT_VERIFY_PROVENANCE=1"
  fi
  printf 'frbit installer: GitHub CLI (gh) not found; skipping release provenance verification. Set FRBIT_VERIFY_PROVENANCE=1 to require this check during installation.\n' >&2
}

verify_provenance

tar -xzf "$tmp_dir/$asset" -C "$tmp_dir" frbit
mkdir -p "$install_dir"
install -m 0755 "$tmp_dir/frbit" "$install_dir/frbit"

printf 'Installed frbit to %s/frbit\n' "$install_dir"
case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *) printf 'Add %s to PATH to run frbit.\n' "$install_dir" ;;
esac
printf 'For shell completion, run: frbit completion install <bash|fish|powershell|zsh>\n'
