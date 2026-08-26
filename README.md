# frbit CLI

`frbit` is the command-line interface for the fortrabbit public API.

For installation, authentication, automation, and the complete command reference,
see the [installation guide](https://docs.fortrabbit.com/platform/automation/cli)
and [CLI documentation](docs/cli.md).

## Install

```sh
curl -fsSL https://github.com/fortrabbit/frbit-cli/releases/latest/download/install.sh | sh
```

You can also install with Homebrew, npm, or a GitHub release archive. See the
[installation guide](https://docs.fortrabbit.com/platform/automation/cli) for every
supported method.

## Authenticate

Create a personal API token in the fortrabbit dashboard, then store it in your
operating-system credential store:

```sh
frbit auth login
```

Interactive login prints the token creation URL and tries to open it in your
default browser. Use `frbit auth login --no-browser` to keep the flow entirely
in the terminal.

Or supply it for one command or an automated job:

```sh
FRBIT_TOKEN=frbit-at-… frbit apps list
```

`FRBIT_TOKEN` takes precedence over the stored credential.

## Usage

```sh
frbit apps list
frbit apps list --page 2
frbit apps list --json
frbit environments list
frbit environments variables get en-a1b2c3
frbit environments restart en-a1b2c3
frbit environments deploy en-a1b2c3
frbit deployments logs dp-a1b2c3
frbit auth status
```

The CLI can also set up fortrabbit for coding agents. See the
[CLI documentation](docs/cli.md#agent-setup) for details.

## Just recipes

[just](https://github.com/casey/just) is an optional command runner. The `run`
recipe forwards arguments to the CLI without keeping a binary in the working
directory:

```sh
just run apps list
just run apps list --json
FRBIT_TOKEN=frbit-at-… just run apps list
```

Other available recipes:

```sh
just                            # list the available recipes
just build
just symlink                    # build, then link ./frbit into /usr/local/bin
just test
```

## Uninstall

Sign out first, then remove the binary with the counterpart of the install
method:

```sh
frbit auth logout                       # repeat with --profile per profile
rm "$(command -v frbit)"                # install script or release archive
brew uninstall frbit                    # Homebrew
npm uninstall --global @fortrabbit/cli  # npm
```

`frbit mcp remove` and `frbit skills remove` undo `frbit setup agent`. The
[uninstall guide](https://docs.fortrabbit.com/platform/automation/cli#uninstall)
covers the config and cache files that stay behind.

## Development

Development and release builds use Go 1.26.6. A compatible Go installation
will automatically download the declared toolchain when `GOTOOLCHAIN` is left
at its default `auto` setting.

```sh
go test ./...
go run ./cmd/frbit apps list
just run apps list
```

To build a reusable binary in the repository root and run it directly:

```sh
just build
./frbit
```

### Target a development API

The default API host is `https://api.fortrabbit.com`. For a single command, use
the root `--host` option or `FRBIT_HOST`:

```sh
FRBIT_TOKEN=frbit-at-… \
  just run --host http://localhost:8085 apps list

FRBIT_HOST=http://localhost:8085 \
  FRBIT_TOKEN=frbit-at-… \
  just run apps list
```

To persist a host and token in your local CLI configuration and operating-system
keychain, log in against that host:

```sh
FRBIT_DASHBOARD_URL=http://localhost:3001 \
  just run --host http://localhost:8085 auth login
```

`FRBIT_DASHBOARD_URL` controls the dashboard origin used for the token creation
link during interactive login.

Host resolution is: `--host`, `FRBIT_HOST`, saved host, then the production
default.

## Release

```sh
just release --dry-run          # preview the next minor release
just release                    # default: increase the minor version
just release --patch
just release --major
```

CI validates formatting, static analysis, tests, builds, and the GoReleaser
configuration. Pushing a `v*` tag creates the GitHub release archives, checksums,
SBOMs, Homebrew formula, and npm packages.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).

Licenses for software included in release binaries are collected in
[THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES).
