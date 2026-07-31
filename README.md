# frbit CLI

`frbit` is the command-line interface for the fortrabbit public API.

## Install

Download an archive for your platform from the GitHub Releases page, then place
the `frbit` binary on your `PATH`.

## Authenticate

Create a personal API token in the fortrabbit dashboard, then either store it in
your operating-system credential store:

```sh
printf '%s' "$FRBIT_TOKEN" | frbit auth login --token-stdin
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

The default API host is `https://api.fortrabbit.com`. Override it with `--host`
or `FRBIT_HOST`:

```sh
FRBIT_TOKEN=frbit-at-… frbit --host https://api.eu-n1a.frbit.dev apps list
```

## Development

Requires Go 1.22 or newer.

```sh
go test ./...
go run ./cmd/frbit apps list
```

CI validates formatting, static analysis, tests, builds, and the GoReleaser
configuration. Pushing a `v*` tag creates the GitHub release archives, checksums,
and SBOMs.
