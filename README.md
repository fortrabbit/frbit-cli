# frbit CLI

`frbit` is the command-line interface for the fortrabbit public API.

## Install

Download an archive for your platform from the GitHub Releases page, then place
the `frbit` binary on your `PATH`.

## Authenticate

Create a personal API token in the fortrabbit dashboard, then store it in your
operating-system credential store:

```sh
frbit auth login
```

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
frbit auth status
```

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
just build
just test
```

## Development

Requires Go 1.24 or newer. Go 1.22+ will automatically download the declared
toolchain when `GOTOOLCHAIN` is left at its default `auto` setting.

```sh
go test ./...
go run ./cmd/frbit apps list
just run apps list
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
just run --host http://localhost:8085 auth login
```

Host resolution is: `--host`, `FRBIT_HOST`, saved host, then the production
default.

## Release

```sh
just release --dry-run          # preview the first release as v0.1.0
just release                    # default: increase the minor version
just release --patch
just release --major
```

CI validates formatting, static analysis, tests, builds, and the GoReleaser
configuration. Pushing a `v*` tag creates the GitHub release archives, checksums,
and SBOMs.
