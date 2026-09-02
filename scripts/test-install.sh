#!/bin/sh

set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
test_dir=$(mktemp -d 2>/dev/null || mktemp -d -t frbit-install-test)
trap 'rm -rf "$test_dir"' EXIT HUP INT TERM

case "$(uname -s):$(uname -m)" in
  Darwin:arm64) asset="frbit_macOS_AppleSilicon.tar.gz" ;;
  Darwin:x86_64) asset="frbit_macOS_Intel.tar.gz" ;;
  Linux:aarch64|Linux:arm64) asset="frbit_linux_arm64.tar.gz" ;;
  Linux:x86_64|Linux:amd64) asset="frbit_linux_amd64.tar.gz" ;;
  *) printf 'unsupported test platform\n' >&2; exit 1 ;;
esac

mkdir -p "$test_dir/release" "$test_dir/package" "$test_dir/bin" "$test_dir/strict-bin"
printf '#!/bin/sh\nexit 0\n' > "$test_dir/gh"
chmod 0755 "$test_dir/gh"
printf '#!/bin/sh\nexit 1\n' > "$test_dir/strict-bin/gh"
chmod 0755 "$test_dir/strict-bin/gh"
printf '#!/bin/sh\nprintf "frbit installer fixture\\n"\n' > "$test_dir/package/frbit"
chmod 0755 "$test_dir/package/frbit"
tar -czf "$test_dir/release/$asset" -C "$test_dir/package" frbit

if command -v sha256sum >/dev/null 2>&1; then
  checksum=$(sha256sum "$test_dir/release/$asset" | awk '{ print $1 }')
else
  checksum=$(shasum -a 256 "$test_dir/release/$asset" | awk '{ print $1 }')
fi
printf '%s  %s\n' "$checksum" "$asset" > "$test_dir/release/checksums.txt"

FRBIT_RELEASE_BASE_URL="file://$test_dir/release" \
  FRBIT_INSTALL_TESTING=1 \
  FRBIT_INSTALL_DIR="$test_dir/bin" \
  PATH="$test_dir:$PATH" \
  sh "$root/install.sh"

test -x "$test_dir/bin/frbit"
test "$("$test_dir/bin/frbit")" = "frbit installer fixture"

if FRBIT_RELEASE_BASE_URL="file://$test_dir/release" FRBIT_INSTALL_TESTING=1 FRBIT_INSTALL_DIR="$test_dir/strict-install" PATH="$test_dir/strict-bin:$PATH" FRBIT_VERIFY_PROVENANCE=1 sh "$root/install.sh" 2>&1 | grep -q 'release provenance verification failed'; then
  :
else
  printf 'strict provenance check did not fail\n' >&2
  exit 1
fi
printf 'installer test passed\n'
