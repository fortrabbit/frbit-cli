# frbit CLI

`frbit` is the command-line interface for the [fortrabbit public API](https://api.fortrabbit.com/v1/docs). Use it to inspect the apps and related resources available to your fortrabbit account from a terminal, script, or agent.

This page is the complete reference for the commands currently available in `frbit`.

## Install

Use the install script, Homebrew, npm, or a GitHub release archive. The
[installation guide](https://docs.fortrabbit.com/platform/concepts/cli) covers
every supported method and platform.

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

### List and inspect apps

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

#### Filter by public ID

Use `--id` once or repeatedly to return a known set of apps:

```sh
frbit apps list --id ap-a1b2c3 --id ap-d4e5f6
```

#### Get one app

```sh
frbit apps get ap-a1b2c3
```

#### JSON output

Pass `--json` to print the API response unchanged. This is intended for scripts and tools such as `jq`.

```sh
frbit apps list --json
frbit apps list --json | jq '(."hydra:member" // .member // .)[] | .name'
```

`--json` is available on every `list`, `get`, and `deployments logs` command. It prints the API response unchanged.

API responses can contain account data, personal information, payment metadata,
and application log output. Treat terminal captures and CI logs as sensitive,
especially when using `--json` or `deployments logs`.

## Environments

List environments, optionally select a page or specific public IDs, or retrieve one environment:

```sh
frbit environments list
frbit environments list --page 2 --id en-a1b2c3
frbit environments get en-a1b2c3
```

## Deployments

Inspect deployments and retrieve the log entries for one deployment:

```sh
frbit deployments list
frbit deployments get dp-a1b2c3
frbit deployments logs dp-a1b2c3
```

## Domains

```sh
frbit domains list
frbit domains list --page 2 --id do-a1b2c3
frbit domains get do-a1b2c3
```

## People

```sh
frbit people list
frbit people list --id pn-a1b2c3
frbit people get pn-a1b2c3
```

## Teams

```sh
frbit teams list
frbit teams get tm-a1b2c3
```

## Payment methods

```sh
frbit payment-methods list
frbit payment-methods get pm-a1b2c3
```

## Agent skills

Install the latest release of the
[fortrabbit agent skills](https://github.com/fortrabbit/agent-skills):

```sh
frbit skills install
```

A user-wide install detects existing Claude Code (`~/.claude`) and Codex
(`~/.codex`) configuration directories. Codex skills are written to its
user-skill location at `~/.agents/skills`. Select an agent explicitly to bypass
detection. Repeat `--agent` to install for more than one agent.

```sh
frbit skills install --agent claude-code
frbit skills install --agent claude-code --agent codex
```

Install into the current project with `--project`. With no explicit agent, a
project install writes the skills for Claude Code and Codex and writes the
repository-scoped GitHub Copilot instructions.

```sh
frbit skills install --project
frbit skills install --project --agent copilot
```

Installed skills retain their version independently of the `frbit` CLI. List
the CLI version together with installed skill versions and exact target paths,
update the skills, or remove them:

```sh
frbit skills list
frbit skills update
frbit skills remove
```

Pass `--project` or `--agent` to those commands to select the same scope and
targets used during installation. `remove` prints every path and asks for
confirmation. Use `--yes` only when an unattended removal is intentional.

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
| `frbit apps list` | List apps. Accepts `--page`, repeatable `--id`, and `--json`. |
| `frbit apps get <id>` | Get an app. |
| `frbit environments list` / `get <id>` | List or get environments. `list` accepts `--page` and repeatable `--id`. |
| `frbit deployments list` / `get <id>` / `logs <id>` | List deployments, get one, or retrieve its logs. |
| `frbit domains list` / `get <id>` | List or get domains. `list` accepts `--page` and repeatable `--id`. |
| `frbit people list` / `get <id>` | List or get people. `list` accepts repeatable `--id`. |
| `frbit teams list` / `get <id>` | List or get teams. |
| `frbit payment-methods list` / `get <id>` | List or get payment methods. |
| `frbit skills install` | Install the latest fortrabbit agent skills. Accepts repeatable `--agent` and `--project`. |
| `frbit skills list` | List installed skills, versions, scopes, and target paths. |
| `frbit skills update` | Update installed skills, or report that the installed version is current. |
| `frbit skills remove` | List and remove installed skills after confirmation. Accepts `--yes`. |
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
