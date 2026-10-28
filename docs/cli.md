# frbit CLI

`frbit` is the command-line interface for the [fortrabbit public API](https://api.fortrabbit.com/v1/docs). Use it to create, configure, and inspect apps and related resources from a terminal, script, or agent.

This page is the complete reference for the commands currently available in `frbit`.

## Install

Use the install script, Homebrew, npm, or a GitHub release archive. The
[installation guide](https://docs.fortrabbit.com/platform/automation/cli) covers
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

## Shell completion

Homebrew installs completion automatically. For other installation methods, run
the command for your shell once:

```sh
frbit completion install bash
frbit completion install fish
frbit completion install powershell
frbit completion install zsh
```

The bash and fish installers save to their auto-discovery directories. The
PowerShell installer adds the completion to its profile, and the zsh installer
saves it in `~/.zfunc/_frbit` and adds that directory to zsh's completion path.

## Authenticate

Sign in through the browser:

```sh
frbit auth login
```

The command prints the sign-in URL and a code first, opens the URL in your default browser, and waits up to 15 minutes. Type the code on the consent page: it shows that the sign-in in the browser belongs to this terminal. Without an account, the same page signs you up. It stores the credential in your operating system's credential store and saves the API host for subsequent commands. The credential refreshes itself before it expires and is only sent to the API host it signed in to; for another host, run `frbit --host <origin> auth login`.

| Option | Effect |
|--|--|
| `--no-browser` | Print the URL without opening a browser |
| `--json` | Print `{"url": …, "confirmation_code": …, "status": "waiting"}` at once, then `{"status": "signed_in", "next": …}` |
| `--signup` | Open the sign-up page instead of the login page, for people without an account |
| `--name "<name>"` | Suggest a name for the new account, 1 to 100 characters |

If the person denies the consent request, the browser tab shows "Not connected. Your agent knows; you can close this tab." and the command fails. With `--json` the last line is `{"status": "error", "code": "access_denied", "error": …}`. The consent request is used up, so ask the person before running `frbit auth login` again. Other stable codes: `timeout` and `frbit_token_set`.

Coding agents run `frbit auth login --json` in the background, show the URL to the person, and continue with `next`. For a person without an account they add `--signup`.

Verify the active credential at any time. It shows the profile, host, credential kind, expiry, and the next setup step. With `--json` it prints one line with `"status": "signed_in"` and the same fields, `{"status": "signed_out"}` (exit 0) without a credential, or `{"status": "error", "error": …}` (exit 1):

```sh
frbit auth status
```

Remove the stored credential when you no longer need it on this machine. A browser sign-in is revoked on the server first, so it leaves the Connections list in the dashboard. When the server cannot be reached, the credential is still removed here and the sign-in stays under Connections until you disconnect it there. A stored API token is only removed locally; revoke it under API tokens. While `FRBIT_TOKEN` is set, `logout` and `auth status` warn on stderr that the token is used instead of a stored sign-in (`auth status --json` adds a `warning` field):

```sh
frbit auth logout
```

### Non-interactive authentication

Use `FRBIT_TOKEN` in CI, scripts, containers, and agents. It is used only for the current process and takes precedence over a token stored in the system credential store.

```sh
FRBIT_TOKEN=frbit-at-… frbit apps list
```

Without a browser, store a personal API token from standard input instead of signing in. `frbit auth login` without `--token-stdin` refuses to start while `FRBIT_TOKEN` is set. Avoid placing tokens directly in shell history or command-line arguments.

```sh
printf '%s' "$FRBIT_TOKEN" | frbit auth login --token-stdin
```

## Uninstall

`frbit uninstall` removes everything fortrabbit put on the machine for coding agents: the MCP server and the skill in Claude Code and Codex, the Claude Code plugin, the sign-in of the selected profile, the config and cache folders, and the binary. It lists everything and asks once; `--yes` skips the question. A Homebrew or npm install is removed with `brew uninstall frbit` or `npm uninstall --global @fortrabbit/cli`. On Windows, delete the binary yourself afterwards: a running executable cannot delete itself. Repeat `frbit auth logout --profile <name>` for other profiles before.

By hand, with the counterpart of the install method:

```sh
rm "$(command -v frbit)"                # install script or release archive
brew uninstall frbit                    # Homebrew
npm uninstall --global @fortrabbit/cli  # npm
```

`brew untap fortrabbit/tap` is only needed when no other formula from the fortrabbit tap remains. Generic binary installers remove their own copies, for example `eget --remove` or `mise uninstall`.

Two files can stay behind. `frbit/config.json` below the user config directory holds the saved API host, and `frbit/update-check.json` below the user cache directory holds the last update check. On macOS those are `~/Library/Application Support` and `~/Library/Caches`, on Linux `~/.config` and `~/.cache`, on Windows `%AppData%` and `%LocalAppData%`. Tokens are not stored in either: they sit in the operating system credential store under the service name `frbit-cli`.

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

`--json` is available on resource reads and on commands that return a created or updated resource. It prints the API response unchanged.

API responses can contain account data, personal information, payment metadata,
environment-variable values, and application log output. Treat terminal captures and CI logs as sensitive,
especially when using `--json` or `deployments logs`.

### Create an app

Create an app with its default environment by selecting the required component plans:

```sh
frbit apps create \
  --name acme-shop \
  --region eu-w1a \
  --software laravel \
  --software-version 11 \
  --component php=sm \
  --component storage=xs \
  --component traffic=xs \
  --component backups=xs
```

Configure Git and start the first deployment as part of the same workflow:

```sh
frbit apps create \
  --name acme-shop \
  --region eu-w1a \
  --component php=sm \
  --component storage=xs \
  --component traffic=xs \
  --component backups=xs \
  --repository acme/shop \
  --branch main \
  --build-command 'composer install --no-dev' \
  --post-deploy-command 'php artisan migrate --force' \
  --deploy
```

Use `--team` or `--payment-method` to select an accessible team or payment method. For the complete nested API payload, pass a JSON object with `--file`, or use `--file -` to read it from standard input.

Pass `--deploy` to start the initial deployment as part of creation. It requires Git configuration: `apps create` needs both `--repository` and `--branch`. Without `--deploy`, creating an app does not start a deployment.

### Update an app

```sh
frbit apps update ap-a1b2c3 --name renamed-shop
frbit apps update ap-a1b2c3 --payment-method pm-a1b2c3
```

## Environments

List environments, optionally select a page or specific public IDs, or retrieve one environment:

```sh
frbit environments list
frbit environments list --page 2 --id en-a1b2c3
frbit environments get en-a1b2c3
```

Create an environment with explicit component plans or clone the plans from an existing environment:

```sh
frbit environments create \
  --app ap-a1b2c3 \
  --name staging \
  --component php=sm \
  --component storage=xs \
  --component traffic=xs \
  --component backups=xs

frbit environments create \
  --app ap-a1b2c3 \
  --name preview \
  --source-environment en-a1b2c3
```

Git settings, build commands, environment variables, and the first deployment can be orchestrated during creation:

```sh
frbit environments create \
  --app ap-a1b2c3 \
  --name staging \
  --source-environment en-a1b2c3 \
  --branch main \
  --env APP_ENV=staging \
  --deploy
```

Pass `--deploy` to start the initial deployment as part of creation. It requires `--branch` so the environment has a Git source. Without `--deploy`, creating an environment does not start a deployment.

Update the environment name or replace its deployment configuration:

```sh
frbit environments update en-a1b2c3 --name staging
frbit environments update en-a1b2c3 --branch develop --directory web
frbit environments update en-a1b2c3 --clear-build-commands
```

Read or merge environment variables. `--set` and `--delete` are repeatable:

```sh
frbit environments variables get en-a1b2c3             # values are masked
frbit environments variables get en-a1b2c3 --reveal    # show values explicitly
frbit environments variables update en-a1b2c3 \
  --set APP_ENV=production \
  --delete OLD_FLAG
```

Values supplied directly on the command line can be retained in shell history. For sensitive values, pass the API request object using `--file` or `--file -` and standard input.

Restart an environment or trigger a deployment from its configured Git source:

```sh
frbit environments restart en-a1b2c3
frbit environments deploy en-a1b2c3
```

The `apps create`, `apps update`, `environments create`, `environments update`, and `environments variables update` commands accept `--file`. Request field flags and `--file` cannot be combined.

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

## Deleting resources

Apps, environments, domains, teams, and payment methods can be deleted. The CLI retrieves the resource first, displays the consequences, and requires the exact public ID before sending the irreversible request:

```sh
frbit apps delete ap-a1b2c3
frbit environments delete en-a1b2c3
frbit domains delete do-a1b2c3
frbit teams delete tm-a1b2c3
frbit payment-methods delete pm-a1b2c3
```

For intentional non-interactive use, repeat the public ID with `--confirm`. A missing or mismatched value prevents the deletion:

```sh
frbit apps delete ap-a1b2c3 --confirm ap-a1b2c3
```

Deleting an app or environment permanently erases its files and database contents. Deleting a payment method or team can also delete connected apps and environments.

## Agent setup

Set up the complete fortrabbit agent integration with one command:

```sh
frbit setup agent
```

The command detects Claude Code and Codex, registers the remote fortrabbit MCP
server with each detected agent, and installs the latest fortrabbit skills. It
uses each agent's native CLI to update only the MCP entry named `fortrabbit` and
prints every skills and MCP configuration path it touched. Re-running the
command is safe: current MCP entries are left unchanged and the skills are
refreshed from their independently versioned release.

The install script runs the same step right after installing when given
`--setup-agent`, or `--agent <name>` for one agent:

```sh
curl -fsSL https://github.com/fortrabbit/frbit-cli/releases/latest/download/install.sh | sh -s -- --setup-agent
```

Select one or more agents explicitly to bypass detection:

```sh
frbit setup agent --agent codex
frbit setup agent --agent claude-code --agent codex
```

The setup summary reports whether a public API credential is configured for the
selected `frbit` profile. That credential is separate from the MCP server's
OAuth authorization. Codex authorization can be started with
`codex mcp login fortrabbit`; in Claude Code, use `/mcp`.

`setup agent` is user-wide and supports Claude Code and Codex. To install only
the skills, including project-scoped skills or GitHub Copilot skills, use the
`frbit skills` commands below.

When Claude Code has the fortrabbit plugin enabled, `setup agent` and
`mcp install` skip Claude Code: the plugin brings its own skill and MCP server.

Undo the setup with `--remove`. It lists the MCP entries, skills and the CLI
sign-in it will remove and asks before it does; `--yes` skips the question. The
binary and the plugin stay; `frbit uninstall` removes them as well:

```sh
frbit setup agent --remove
frbit setup agent --remove --agent codex --yes
```

## MCP server

Manage the MCP registration independently when you do not want to change the
skills:

```sh
frbit mcp install
frbit mcp list
frbit mcp remove
```

All three commands accept repeatable `--agent claude-code|codex`. Without that
option, the CLI detects installed agents. `install` creates the named entry or
updates a stale fortrabbit URL while leaving all other MCP servers alone.
`remove` lists the affected agent configuration files and asks for confirmation;
pass `--yes` only for intentional unattended removal.

The remote endpoint is `https://mcp.fortrabbit.com/mcp`. Claude Code stores its
user-scoped entry in `~/.claude.json`; Codex stores it in
`~/.codex/config.toml`.

## Agent skills

Install the latest release of the
[fortrabbit agent skills](https://github.com/fortrabbit/agent-skills):

```sh
frbit skills install
```

A user-wide install detects existing Claude Code (`~/.claude`) and Codex
(`~/.codex`) configuration directories. Codex skills are written to its
user-skill location at `~/.agents/skills`. Select an agent explicitly to bypass
detection. Repeat `--agent` to install for more than one agent. GitHub Copilot
is never detected: select it with `--agent copilot` to write its user skills to
`~/.copilot/skills`.

```sh
frbit skills install --agent claude-code
frbit skills install --agent claude-code --agent codex
frbit skills install --agent copilot
```

Install into the current project with `--project`. With no explicit agent, a
project install writes the skills for Claude Code (`.claude/skills`), Codex
(`.agents/skills`) and GitHub Copilot (`.github/skills`).

Install and update remove fortrabbit skills that the release no longer ships,
and the `.github/instructions/fortrabbit.instructions.md` file written by
earlier releases for Copilot.

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
| `frbit auth login` | Sign in through the browser. Accepts `--no-browser`, `--json`, `--signup`, `--name`, and `--token-stdin` for a personal API token. |
| `frbit auth status` | Validate the credential selected by `--profile` or `FRBIT_TOKEN` and show its kind, expiry, and the next setup step. Accepts `--json`. |
| `frbit auth logout` | Revoke a browser sign-in and remove the stored credential for the selected profile. |
| `frbit apps list` | List apps. Accepts `--page`, repeatable `--id`, and `--json`. |
| `frbit apps get <id>` | Get an app. |
| `frbit apps create` | Create an app and its initial environment. Accepts field flags or `--file`. |
| `frbit apps update <id>` | Update an app name or payment method. Accepts field flags or `--file`. |
| `frbit apps delete <id>` | Permanently delete an app. |
| `frbit environments list` / `get <id>` | List or get environments. `list` accepts `--page` and repeatable `--id`. |
| `frbit environments create` | Create and optionally configure/deploy an environment. Accepts field flags or `--file`. |
| `frbit environments update <id>` | Update an environment and its deployment configuration. |
| `frbit environments delete <id>` | Permanently delete an environment. |
| `frbit environments variables get <id>` | Get custom and platform-injected environment variables. |
| `frbit environments variables update <id>` | Set or delete custom environment variables. |
| `frbit environments restart <id>` | Request an environment restart. |
| `frbit environments deploy <id>` | Create a deployment from the configured Git source. |
| `frbit deployments list` / `get <id>` / `logs <id>` | List deployments, get one, or retrieve its logs. |
| `frbit domains list` / `get <id>` / `delete <id>` | List, get, or delete domains. |
| `frbit people list` / `get <id>` | List or get people. `list` accepts repeatable `--id`. |
| `frbit teams list` / `get <id>` / `delete <id>` | List, get, or delete teams. |
| `frbit payment-methods list` / `get <id>` / `delete <id>` | List, get, or delete payment methods. |
| `frbit uninstall` | Remove the agent setup, the Claude Code plugin, the sign-in, the config and cache folders and the binary after confirmation. Accepts `--yes`. |
| `frbit setup agent` | Register the remote MCP server and install skills for detected agents. Accepts repeatable `--agent`; `--remove` undoes it and signs out. |
| `frbit mcp install` | Register or update the named fortrabbit MCP entry. Accepts repeatable `--agent`. |
| `frbit mcp list` | List the fortrabbit MCP status, URL, and agent configuration paths. |
| `frbit mcp remove` | List and remove the named fortrabbit MCP entry after confirmation. Accepts `--yes`. |
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

If a command reports that no API token is available, set `FRBIT_TOKEN` for that command or run `frbit auth login`.

If authentication fails, confirm that the token is active and has access to the expected apps, then run `frbit auth status` to test the exact profile and host in use.
