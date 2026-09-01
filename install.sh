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

tar -xzf "$tmp_dir/$asset" -C "$tmp_dir" frbit
mkdir -p "$install_dir"
install -m 0755 "$tmp_dir/frbit" "$install_dir/frbit"

printf 'Installed frbit to %s/frbit\n' "$install_dir"
case ":${PATH:-}:" in
  *":$install_dir:"*) ;;
  *) printf 'Add %s to PATH to run frbit.\n' "$install_dir" ;;
esac
printf 'For shell completion, run: frbit completion install <bash|fish|powershell|zsh>\n'
