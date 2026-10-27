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

mkdir -p "$test_dir/release" "$test_dir/package" "$test_dir/bin" "$test_dir/strict-bin" "$test_dir/signed-out-bin"
printf '#!/bin/sh\nexit 0\n' > "$test_dir/gh"
chmod 0755 "$test_dir/gh"
printf '#!/bin/sh\n[ "$1" = auth ] && exit 0\nexit 1\n' > "$test_dir/strict-bin/gh"
chmod 0755 "$test_dir/strict-bin/gh"
printf '#!/bin/sh\nexit 1\n' > "$test_dir/signed-out-bin/gh"
chmod 0755 "$test_dir/signed-out-bin/gh"
printf '#!/bin/sh\nprintf "frbit installer fixture%%s\\n" "${*:+ $*}"\n' > "$test_dir/package/frbit"
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

FRBIT_RELEASE_BASE_URL="file://$test_dir/release" FRBIT_INSTALL_TESTING=1 FRBIT_INSTALL_DIR="$test_dir/signed-out-install" PATH="$test_dir/signed-out-bin:$PATH" sh "$root/install.sh" >/dev/null 2>&1 || { printf 'signed-out gh blocked the install\n' >&2; exit 1; }
if FRBIT_RELEASE_BASE_URL="file://$test_dir/release" FRBIT_INSTALL_TESTING=1 FRBIT_INSTALL_DIR="$test_dir/signed-out-strict" PATH="$test_dir/signed-out-bin:$PATH" FRBIT_VERIFY_PROVENANCE=1 sh "$root/install.sh" >/dev/null 2>&1; then
  printf 'strict provenance check accepted a signed-out gh\n' >&2
  exit 1
fi
install_fixture() {
  FRBIT_RELEASE_BASE_URL="file://$test_dir/release" \
    FRBIT_INSTALL_TESTING=1 \
    FRBIT_INSTALL_DIR="$test_dir/agent-bin" \
    PATH="$test_dir:$PATH" \
    sh "$root/install.sh" "$@"
}

install_fixture --setup-agent | grep -qx 'frbit installer fixture setup agent' || { printf -- '--setup-agent did not run setup agent\n' >&2; exit 1; }
install_fixture --agent codex --agent claude-code | grep -qx 'frbit installer fixture setup agent --agent codex --agent claude-code' || { printf -- '--agent was not forwarded\n' >&2; exit 1; }
if install_fixture --agent 'codex;id' >/dev/null 2>&1; then printf 'invalid agent name was accepted\n' >&2; exit 1; fi
if install_fixture --unknown >/dev/null 2>&1; then printf 'unknown option was accepted\n' >&2; exit 1; fi

printf 'installer test passed\n'
