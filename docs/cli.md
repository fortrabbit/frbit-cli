# frbit CLI

`frbit` is the command-line interface for the [fortrabbit public API](https://api.fortrabbit.com/v1/docs). Use it to work with the apps available to your fortrabbit account from a terminal, script, or agent.

This page is the complete reference for the commands currently available in `frbit`.

## Install

Download the archive for your operating system and CPU architecture from the [GitHub Releases page](https://github.com/fortrabbit/frbit-cli/releases). Extract it and put the `frbit` binary somewhere on your `PATH`.

Confirm the installation:

```sh
frbit version
```

For every command and option available in your installed version, run:

```sh
frbit --help
frbit apps list --help
```

## Authenticate

Create a personal API token in the fortrabbit dashboard, then sign in:

```sh
frbit auth login
```

The command prompts for the token and stores it in your operating system's credential store. It also saves the API host for subsequent commands.

Verify the active credential at any time:

```sh
frbit auth status
```

Remove the stored credential when you no longer need it on this machine:

```sh
frbit auth logout
```

### Non-interactive authentication

Use `FRBIT_TOKEN` in CI, scripts, containers, and agents. It is used only for the current process and takes precedence over a token stored in the system credential store.

```sh
FRBIT_TOKEN=frbit-at-… frbit apps list
```

To save a token without an interactive prompt, pass it on standard input. Avoid placing tokens directly in shell history or command-line arguments.

```sh
printf '%s' "$FRBIT_TOKEN" | frbit auth login --token-stdin
```

## Apps

### List apps

List the apps available to the authenticated person:

```sh
frbit apps list
```

The default output is a readable table with each app's public ID, name, description, trial status, and last update time.

#### Pagination

Request a specific page of results. Pages start at `1`.

```sh
frbit apps list --page 2
```

#### JSON output

Pass `--json` to print the API response unchanged. This is intended for scripts and tools such as `jq`.

```sh
frbit apps list --json
frbit apps list --json | jq '(."hydra:member" // .member // .)[] | .name'
```

## Profiles

Profiles let you keep separate stored credentials for different accounts or environments. The default profile is named `default`.

Pass `--profile` before the command group to select a profile:

```sh
frbit --profile work auth login
frbit --profile work apps list
frbit --profile work auth logout
```

`FRBIT_TOKEN` overrides the stored token for every profile. This makes it suitable for automation, where a token is normally supplied by the job's secret store instead of being saved on disk.

## Command reference

| Command | Description |
| --- | --- |
| `frbit auth login` | Validate and store a public API token. Accepts `--token-stdin`. |
| `frbit auth status` | Validate the credential selected by `--profile` or `FRBIT_TOKEN`. |
| `frbit auth logout` | Remove the stored credential for the selected profile. |
| `frbit apps list` | List available apps. Accepts `--page` and `--json`. |
| `frbit version` | Print the CLI version, source commit, and build date. |

Global options:

| Option | Description |
| --- | --- |
| `--profile <name>` | Stored credential profile to use. Defaults to `default`. |

## Environment variables

| Variable | Description |
| --- | --- |
| `FRBIT_TOKEN` | Public API token for the current command. Overrides a stored credential. |

## Troubleshooting

If a command reports that no API token is available, set `FRBIT_TOKEN` for that command or run `frbit auth login` interactively.

If authentication fails, confirm that the token is active and has access to the expected apps, then run `frbit auth status` to test the exact profile and host in use.
