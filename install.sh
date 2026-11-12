#!/bin/sh

set -eu

# Everything runs from main, called on the last line: a download cut off mid-pipe (curl | sh) runs nothing.

repository="fortrabbit/frbit-cli"
docs_url="https://docs.fortrabbit.com/platform/automation/cli"

fail() {
  printf 'frbit installer: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'USAGE'
Usage: install.sh [--setup-agent] [--agent <name>]...

  --setup-agent   After installing, run `frbit setup agent` for the detected coding agents.
  --agent <name>  Set up this agent only, e.g. claude-code or codex. Repeatable; implies --setup-agent.
  -h, --help      Show this help.

Environment:
  FRBIT_VERSION             Install this release, e.g. 1.12.0, instead of the latest.
  FRBIT_INSTALL_DIR         Install into this directory, even when Homebrew or npm manages frbit.
  FRBIT_VERIFY_PROVENANCE   Set to 1 to fail unless a signed-in GitHub CLI verifies the release.
USAGE
}

download() {
  curl --proto "$curl_protocols" --proto-redir "=https" --tlsv1.2 --retry 3 -fsSL "$1" -o "$2"
}

# Prints "Homebrew" or "npm" when that package manager owns the given frbit.
package_manager_of() {
  if command -v brew >/dev/null 2>&1; then
    brew_prefix=$(brew --prefix 2>/dev/null || true)
    case "$1" in
      "$brew_prefix"/*) [ -n "$brew_prefix" ] && printf 'Homebrew' && return ;;
    esac
  fi
  if command -v npm >/dev/null 2>&1; then
    npm_prefix=$(npm prefix --global 2>/dev/null || true)
    case "$1" in
      "$npm_prefix"/*) [ -n "$npm_prefix" ] && printf 'npm' && return ;;
    esac
  fi
  return 0
}

run_setup_agent() {
  # agent_args holds only validated names, so word splitting is safe here.
  # Piped installs (curl | sh) have no stdin; give the agent the terminal when one can be opened.
  if [ -r /dev/tty ] && ( : </dev/tty ) 2>/dev/null; then
    # shellcheck disable=SC2086
    "$1" setup agent $agent_args </dev/tty
  else
    # shellcheck disable=SC2086
    "$1" setup agent $agent_args
  fi
}

# Older releases have no doctor command; a failed check never fails the install.
run_doctor() {
  if "$1" doctor --help >/dev/null 2>&1; then
    printf '\n'
    "$1" doctor || true
  fi
}

path_hint() {
  case "$(basename "${SHELL:-sh}")" in
    zsh) printf "Add it to PATH: echo 'export PATH=\"%s:\$PATH\"' >> ~/.zshrc\n" "$1" ;;
    bash) printf "Add it to PATH: echo 'export PATH=\"%s:\$PATH\"' >> ~/.bashrc\n" "$1" ;;
    fish) printf 'Add it to PATH: fish_add_path %s\n' "$1" ;;
    *) printf "Add it to PATH: echo 'export PATH=\"%s:\$PATH\"' >> ~/.profile\n" "$1" ;;
  esac
  printf 'Then open a new terminal, so coding agents find frbit too.\n'
}

verify_provenance() {
  # gh attestation verify needs a signed-in gh, so a signed-out gh counts as missing.
  if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
    gh attestation verify "$tmp_dir/checksums.txt" \
      --repo "$repository" \
      --signer-workflow "$repository/.github/workflows/release.yml" \
      --predicate-type "https://slsa.dev/provenance/v1" \
      >/dev/null || fail "release provenance verification failed"
    return
  fi
  if [ "${FRBIT_VERIFY_PROVENANCE:-}" = "1" ]; then
    fail "a signed-in GitHub CLI (gh) is required when FRBIT_VERIFY_PROVENANCE=1"
  fi
  printf 'frbit installer: GitHub CLI (gh) not found or not signed in; skipping release provenance verification. Set FRBIT_VERIFY_PROVENANCE=1 to require this check during installation.\n' >&2
}

main() {
  setup_agent=0
  agent_args=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --setup-agent) setup_agent=1 ;;
      --agent)
        [ "$#" -ge 2 ] || fail "--agent needs a name"
        case "$2" in
          *[!a-z0-9-]*|"") fail "invalid agent name: $2" ;;
        esac
        setup_agent=1
        agent_args="$agent_args --agent $2"
        shift
        ;;
      -h|--help) usage; exit 0 ;;
      *) fail "unknown option: $1 (see --help)" ;;
    esac
    shift
  done

  case "$(uname -s)" in
    Darwin) os="macOS" ;;
    Linux) os="linux" ;;
    MINGW*|MSYS*|CYGWIN*) fail "this script does not run on Windows. Download the Windows archive instead: $docs_url" ;;
    *) fail "unsupported operating system: $(uname -s)" ;;
  esac

  command -v curl >/dev/null 2>&1 || fail "curl is required"
  command -v tar >/dev/null 2>&1 || fail "tar is required"

  existing=$(command -v frbit 2>/dev/null || true)
  if [ -z "${FRBIT_INSTALL_DIR:-}" ] && [ -n "$existing" ]; then
    manager=$(package_manager_of "$existing")
    if [ -n "$manager" ]; then
      printf 'frbit is installed with %s at %s; keeping it.\n' "$manager" "$existing"
      if [ -n "${FRBIT_VERSION:-}" ]; then
        printf 'frbit installer: FRBIT_VERSION is ignored, %s manages the version. Set FRBIT_INSTALL_DIR to install a release next to it.\n' "$manager" >&2
      fi
      if [ "$manager" = "Homebrew" ]; then
        printf 'Update it with: brew upgrade frbit\n'
      else
        printf 'Update it with: npm update --global @fortrabbit/cli\n'
      fi
      if [ "$setup_agent" -eq 1 ]; then
        run_setup_agent "$existing"
      fi
      run_doctor "$existing"
      return
    fi
  fi

  case "$(uname -m)" in
    arm64|aarch64) arch="arm64" ;;
    amd64|x86_64) arch="amd64" ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac

  # A shell under Rosetta reports x86_64 on Apple Silicon; install the native binary instead.
  if [ "$os" = "macOS" ] && [ "$arch" = "amd64" ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = "1" ]; then
    arch="arm64"
  fi

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
      *[!0-9A-Za-z.-]*|*..*) fail "invalid FRBIT_VERSION: $FRBIT_VERSION" ;;
      v*) tag=$FRBIT_VERSION ;;
      *) tag="v$FRBIT_VERSION" ;;
    esac
    release_base_url="https://github.com/$repository/releases/download/$tag"
  else
    release_base_url="https://github.com/$repository/releases/latest/download"
  fi

  curl_protocols="=https"
  case "$release_base_url" in
    *..*) fail "FRBIT_RELEASE_BASE_URL must use the official GitHub release host" ;;
    https://github.com/fortrabbit/frbit-cli/releases/download/*|https://github.com/fortrabbit/frbit-cli/releases/latest/download) ;;
    file://*)
      [ "${FRBIT_INSTALL_TESTING:-}" = "1" ] || fail "FRBIT_RELEASE_BASE_URL must use the official GitHub release host"
      curl_protocols="=https,file"
      ;;
    *) fail "FRBIT_RELEASE_BASE_URL must use the official GitHub release host" ;;
  esac

  if [ -n "${FRBIT_INSTALL_DIR:-}" ]; then
    install_dir=${FRBIT_INSTALL_DIR%/}
  elif [ "$(id -u)" -eq 0 ]; then
    install_dir="/usr/local/bin"
  else
    install_dir="${HOME:?HOME is not set}/.local/bin"
  fi

  tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t frbit)
  trap 'rm -rf "$tmp_dir"' EXIT
  # Exit on signals, so the EXIT trap cleans up and the install stops.
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM

  printf 'Downloading frbit for %s/%s...\n' "$os" "$arch"
  download "$release_base_url/$asset" "$tmp_dir/$asset"
  download "$release_base_url/checksums.txt" "$tmp_dir/checksums.txt"

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

  verify_provenance

  tar -xzf "$tmp_dir/$asset" -C "$tmp_dir" frbit
  mkdir -p "$install_dir"
  install -m 0755 "$tmp_dir/frbit" "$install_dir/frbit"

  installed_version=$("$install_dir/frbit" --version 2>/dev/null) || fail "the installed frbit does not run: $install_dir/frbit"
  printf 'Installed %s to %s/frbit\n' "$installed_version" "$install_dir"

  case ":${PATH:-}:" in
    *":$install_dir:"*)
      first_on_path=$(command -v frbit 2>/dev/null || true)
      if [ -n "$first_on_path" ] && [ "$first_on_path" != "$install_dir/frbit" ]; then
        printf 'frbit installer: %s comes first on PATH and shadows the new install. Remove it, or move %s ahead of it.\n' "$first_on_path" "$install_dir" >&2
      fi
      ;;
    *) path_hint "$install_dir" ;;
  esac

  if [ -z "$existing" ] && [ -z "${FRBIT_INSTALL_DIR:-}" ] && command -v brew >/dev/null 2>&1; then
    printf 'Homebrew found. To let brew upgrade keep frbit current: brew install fortrabbit/tap/frbit, then remove %s/frbit\n' "$install_dir"
  fi

  if [ "$setup_agent" -eq 1 ]; then
    run_setup_agent "$install_dir/frbit"
  else
    printf 'For shell completion, run: frbit completion install <bash|fish|powershell|zsh>\n'
  fi
  run_doctor "$install_dir/frbit"
}

main "$@"
