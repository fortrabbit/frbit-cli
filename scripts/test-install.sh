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
if command -v setsid >/dev/null 2>&1; then detach="setsid"; elif command -v perl >/dev/null 2>&1; then detach="perl -MPOSIX -e POSIX::setsid();exec(@ARGV)"; else detach=""; fi
if [ -n "$detach" ]; then
  # shellcheck disable=SC2086
  $detach env FRBIT_RELEASE_BASE_URL="file://$test_dir/release" FRBIT_INSTALL_TESTING=1 FRBIT_INSTALL_DIR="$test_dir/notty-bin" PATH="$test_dir:$PATH" sh "$root/install.sh" --setup-agent </dev/null 2>&1 | grep -qx 'frbit installer fixture setup agent' || { printf 'no-tty fallback did not run setup agent\n' >&2; exit 1; }
fi
if install_fixture --agent 'codex;id' >/dev/null 2>&1; then printf 'invalid agent name was accepted\n' >&2; exit 1; fi
if install_fixture --unknown >/dev/null 2>&1; then printf 'unknown option was accepted\n' >&2; exit 1; fi


# The cases below run on a minimal PATH, so a frbit, brew or npm on the test machine cannot leak in.
sys_path="/usr/bin:/bin:/usr/sbin:/sbin"
mkdir -p "$test_dir/home" "$test_dir/brew/bin" "$test_dir/brew-tool" "$test_dir/npm/bin" "$test_dir/npm-tool" "$test_dir/other/bin" "$test_dir/windows"
printf '#!/bin/sh\nprintf "%%s\\n" "%s"\n' "$test_dir/brew" > "$test_dir/brew-tool/brew"
printf '#!/bin/sh\nprintf "%%s\\n" "%s"\n' "$test_dir/npm" > "$test_dir/npm-tool/npm"
for managed in brew npm other; do
  printf '#!/bin/sh\nprintf "managed frbit%%s\\n" "${*:+ $*}"\n' > "$test_dir/$managed/bin/frbit"
done
printf '#!/bin/sh\nprintf "MINGW64_NT-10.0\\n"\n' > "$test_dir/windows/uname"
chmod 0755 "$test_dir/brew-tool/brew" "$test_dir/npm-tool/npm" "$test_dir/brew/bin/frbit" "$test_dir/npm/bin/frbit" "$test_dir/other/bin/frbit" "$test_dir/windows/uname"

isolated_install() {
  path=$1
  shift
  env -i HOME="$test_dir/home" SHELL=/bin/zsh PATH="$path" \
    FRBIT_RELEASE_BASE_URL="file://$test_dir/release" FRBIT_INSTALL_TESTING=1 \
    sh "$root/install.sh" "$@" 2>&1
}

out=$(isolated_install "$test_dir/brew/bin:$test_dir/brew-tool:$test_dir:$sys_path" --setup-agent)
printf '%s\n' "$out" | grep -q 'installed with Homebrew' || { printf 'Homebrew frbit was not kept\n' >&2; exit 1; }
printf '%s\n' "$out" | grep -qx 'managed frbit setup agent' || { printf 'setup agent did not use the Homebrew frbit\n' >&2; exit 1; }
test ! -e "$test_dir/home/.local/bin/frbit" || { printf 'installed next to a Homebrew frbit\n' >&2; exit 1; }

out=$(isolated_install "$test_dir/npm/bin:$test_dir/npm-tool:$test_dir:$sys_path")
printf '%s\n' "$out" | grep -q 'npm update --global @fortrabbit/cli' || { printf 'npm frbit was not kept\n' >&2; exit 1; }

out=$(isolated_install "$test_dir/other/bin:$test_dir/home/.local/bin:$test_dir:$sys_path")
printf '%s\n' "$out" | grep -q 'shadows the new install' || { printf 'shadowing frbit was not reported\n' >&2; exit 1; }
rm -rf "$test_dir/home/.local"

out=$(isolated_install "$test_dir/brew-tool:$test_dir:$sys_path")
printf '%s\n' "$out" | grep -q 'brew install fortrabbit/tap/frbit' || { printf 'Homebrew hint missing\n' >&2; exit 1; }
printf '%s\n' "$out" | grep -q ">> ~/.zshrc" || { printf 'zsh PATH hint missing\n' >&2; exit 1; }
printf '%s\n' "$out" | grep -q '^Installed frbit installer fixture --version to ' || { printf 'installed version not reported\n' >&2; exit 1; }

if isolated_install "$test_dir/windows:$test_dir:$sys_path" | grep -q 'does not run on Windows'; then :; else printf 'Windows shell not pointed to the docs\n' >&2; exit 1; fi
if env -i HOME="$test_dir/home" PATH="$test_dir:$sys_path" FRBIT_VERSION='v1/../../evil' sh "$root/install.sh" >/dev/null 2>&1; then printf 'FRBIT_VERSION with .. was accepted\n' >&2; exit 1; fi
if env -i HOME="$test_dir/home" PATH="$test_dir:$sys_path" FRBIT_RELEASE_BASE_URL='https://github.com/fortrabbit/frbit-cli/releases/download/../../evil' sh "$root/install.sh" >/dev/null 2>&1; then printf 'FRBIT_RELEASE_BASE_URL with .. was accepted\n' >&2; exit 1; fi
sh "$root/install.sh" --help | grep -q 'FRBIT_INSTALL_DIR' || { printf 'help does not list the environment\n' >&2; exit 1; }

printf 'installer test passed\n'
