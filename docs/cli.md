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

`--limit N` returns at most N apps and fetches further pages until it has them. `--all` returns every page. Both work on every `list` command. `--all` cannot be combined with `--page` or `--limit`.

```sh
frbit apps list --limit 50
frbit apps list --all --json
```

With `--limit` or `--all`, `--json` prints one JSON array of the returned resources instead of the API response of a single page.

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

When the API rejects a request made with `--json`, the CLI prints the API's problem document (`title`, `detail`, `violations`) to standard output and exits non-zero. Without `--json` the error goes to standard error, one line per rejected field.

Deprecation notices from the API (`Warning`, `Deprecation` and `Sunset` headers) are printed to standard error as `warning: ...` lines, so they never mix with `--json` output.

Create commands send an `Idempotency-Key` header. When the API answers a create with 429 or a 5xx error, the CLI retries it once with the same key, so the API never creates the resource twice.

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

The API gives the initial environment a cloud domain with a label derived from the app name, and creates none when that label is invalid, reserved or taken. Pass `--no-cloud-domain` to create the app without one, or set `initialEnvironment.cloudDomain` with `--file` to choose the label.

Pass `--root-path public` to set the folder of the repository the web server serves; it defaults to the root path of the software preset.

Pass `--deploy` to start the initial deployment as part of creation. It requires Git configuration: `apps create` needs both `--repository` and `--branch`. Without `--deploy`, creating an app does not start a deployment.

### Billed creates

`apps create` and `environments create` first run the same request as a dry run, print the monthly price and the payment method, and ask `Create and start billing? [y/N]`. The price and the question go to stderr. Trial apps without a payment method cost nothing and are created without a question. Without a terminal the command fails with `billed change needs --yes`; pass `--yes` only after the person has seen the price. The dry run applies to `--file` bodies too and sends no idempotency key.

### Update an app

```sh
frbit apps update ap-a1b2c3 --name renamed-shop
frbit apps update ap-a1b2c3 --payment-method pm-a1b2c3
frbit apps update ap-a1b2c3 --software-preset laravel --git-deploy-comments=false
frbit apps update ap-a1b2c3 --technical-contact pn-a1b2c3 --technical-contact pn-d4e5f6
frbit apps update ap-a1b2c3 --clear-technical-contacts
frbit apps update ap-a1b2c3 --repository acme/shop
frbit apps update ap-a1b2c3 --disconnect-repository --confirm ap-a1b2c3
```

- `--software-preset` resets the software version of every environment to the preset default.
- `--technical-contact` (repeatable) replaces the list with the given person public IDs; `--clear-technical-contacts` empties it.
- `--git-deploy-comments=true|false` toggles deployment comments on GitHub pull requests.
- `--repository OWNER/REPOSITORY` connects a GitHub repository (`PUT /v1/apps/{id}/git-repository`) and resets the branches of all environments; set them again with `environments update`. Combined with other flags, the app is updated first, then the repository connected.
- `--disconnect-repository` removes the connection and stops deployments until a repository is connected again. It asks you to type the app ID; without a terminal pass `--confirm <app-id>`.

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

Pass `--root-path public` to set the folder of the repository the web server serves.

Pass `--deploy` to start the initial deployment as part of creation. It requires `--branch` so the environment has a Git source. Without `--deploy`, creating an environment does not start a deployment.

The API gives the environment a cloud domain with a label derived from the app's cloud-domain label, or the app name when the app has none, plus the environment name, for example `acme-shop-staging`. It creates none when that label is invalid, reserved or taken. Pass `--no-cloud-domain` to create the environment without one, or set `cloudDomain` with `--file` to choose the label.

Update the environment name or replace its deployment configuration:

```sh
frbit environments update en-a1b2c3 --name staging
frbit environments update en-a1b2c3 --branch develop --directory web
frbit environments update en-a1b2c3 --clear-build-commands
frbit environments update en-a1b2c3 --root-path public    # empty value resets it
```

Change the deployment settings. Each flag sends only its own key; repeatable list flags replace the whole list and a `--clear-<list>` flag empties it:

```sh
frbit environments update en-a1b2c3 --strategy merge --replace-pattern /vendor
frbit environments update en-a1b2c3 --exclude-pattern /storage --cached-directory vendor
frbit environments update en-a1b2c3 --clear-exclude-patterns
frbit environments update en-a1b2c3 --trigger manual --permission connectedDevelopers
frbit environments update en-a1b2c3 --node-version 24 --deploy-hook
frbit environments update en-a1b2c3 --software-version 11 --php-version 8.4   # --php-version stops automatic PHP updates
```

| Flag | Values |
| --- | --- |
| `--strategy` | `merge` or `replace` |
| `--exclude-pattern`, `--clear-exclude-patterns` | Path patterns left out of deployments |
| `--replace-pattern`, `--clear-replace-patterns` | Path patterns replaced on deployment, only with strategy `merge` |
| `--cached-directory`, `--clear-cached-directories` | Directories cached between deployments |
| `--trigger` | `push`, `manual` or `hook` |
| `--permission` | `connectedDevelopers` or `repositoryAccess` |
| `--node-version` | A version from `frbit node-versions list`; stops following newer Node.js versions |
| `--deploy-hook[=false]` | Turn the deploy hook on or off |

The strategy, the pattern lists and the cached directories are refused while a deploy file in the repository manages them. `trigger`, `permission`, `nodeVersion` and `deployHook` are always editable. `environments get` lists the settings as `deployment.<field>` rows next to `rootPath`.

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

### PHP settings, access and components

```sh
frbit php-settings list                  # valid directives and values
frbit php-extensions list
frbit environments php-settings get en-a1b2c3
frbit environments php-settings update en-a1b2c3 --set max_execution_time=120 --set upload_max_filesize=256M --extension apcu=on

frbit environments basic-auth get en-a1b2c3
printf '%s\n' "$PASSWORD" | frbit environments basic-auth update en-a1b2c3 --enabled --username preview --password-stdin

frbit environments firewall-rules list en-a1b2c3
frbit environments firewall-rules add en-a1b2c3 --description "Metrics exporter" --port 8080-8090
frbit environments firewall-rules delete fr-a1b2c3 --confirm fr-a1b2c3

frbit environments components get en-a1b2c3
frbit environments components update en-a1b2c3 --component php=md --component database=off --yes
```

- Directives and extensions you do not name stay unchanged. Numeric `--set` values are sent as numbers.
- The basic auth password is read as one line from stdin and is never a flag or part of any output. Pipe it in; a password typed on a terminal is echoed. Omit `--password-stdin` to keep the current password.
- `--port N` opens one port, `--port N-M` a range of at most 1000 ports, within 1-65535.
- `firewall-rules delete` needs the exact rule ID: typed on a terminal, or `--confirm <id>` without one.
- `components update` looks up each size in the catalog first (an unknown size is an error before any request), prints `slug: old (price) -> new (price)` per changed component and the signed `Monthly change` to stderr (priced in the region of the environment's app; `--region` overrides, and the command fails before any change if the region can't be resolved), skips components already at the requested size (nothing changes: `No component plan changes.`), and asks `Change component plans? [y/N]`, also for a downsize. Without a terminal it fails with `billed change needs --yes`. The change is applied asynchronously (202).

### Metrics

```sh
frbit environments metrics en-a1b2c3
frbit environments metrics en-a1b2c3 php-requests --range 7d
frbit environments metrics en-a1b2c3 web-storage --range 30d --step hour
```

Without a slug, `metrics` prints one row per booked component (storage, database, traffic) with current usage, limit and percentage, the last 24 hours (requests, 500/503/504 responses, response times, PHP memory, out-of-memory kills) and the available metric slugs. With a slug it prints the history: totals and one point per step. `--range 1h|24h|7d|30d` (default `24h`) sets `from` and `to` (ISO 8601, UTC) counted back from now; `--step hour|day` defaults to `hour` for 1h and 24h and `day` for 7d and 30d (the API allows hourly steps up to 83 days). `--json` prints the raw API body.

### Logs

```sh
frbit environments logs en-a1b2c3
frbit environments logs en-a1b2c3 --source access --status 5xx --since 1h
frbit environments logs en-a1b2c3 --source php --follow
```

Prints the newest `--limit` entries (default 100, max 1000) of one `--source` (`php` default, `apache`, `access`, `jobs`), oldest first, paging back through the `Link` header. `--since` takes a duration (`15m`, `24h`, `7d`) or an ISO 8601 time with a time zone. `--status` (`500`, `5xx`, `500,503`) and `--method` filter the access log only. `--follow` polls every 2 seconds for new entries until Ctrl-C, which exits 0. Control characters other than tabs are stripped. `--follow` retries after 429, 503 and network errors (waiting `Retry-After` when given). `--json` prints the raw API body when one page satisfies the request, the merged array (newest first) when several pages were needed, and one object per line with `--follow`.

## Deployments

Inspect deployments and retrieve the log entries for one deployment. The list shows the newest 30 first; `--environment` narrows it to one environment:

```sh
frbit deployments list
frbit deployments list --environment en-a1b2c3 --page 2
frbit deployments get dp-a1b2c3
frbit deployments logs dp-a1b2c3
frbit deployments logs dp-a1b2c3 --follow
```

`logs --follow` polls every 2 seconds, prints each line once and stops when the deployment is `succeeded`, `failed`, `canceled` or `unknown` and its logs are complete (`ready: true`). `--follow` retries after 429, 503 and network errors and prints `waiting for logs…` to stderr once while none have arrived. Without `--follow`, logs that are still being collected say so.

## Domains

```sh
frbit domains list
frbit domains list --page 2 --id do-a1b2c3
frbit domains get do-a1b2c3
frbit domains create en-a1b2c3 www.example.com --main
frbit domains create en-a1b2c3 acme-shop --cloud
frbit domains dns do-a1b2c3 --refresh
```

`domains create <environment> <name>` adds a domain you own (`name` is a hostname) and prints the DNS record to set at your DNS provider, the record found in DNS now, and the verification state. A bare apex such as `example.com` also creates `www.example.com`. `--main` makes it the environment's main domain.

With `--cloud`, `name` is a label such as `acme-shop`, not a full hostname; the API appends the cloud domain root. The CLI checks `GET /v1/domains/cloud/availability` first and fails with the reason (`invalid`, `reserved`, `taken`) before creating anything. Cloud domains need no DNS record. An environment can have one cloud domain.

`domains dns <id>` shows the expected and actual record. `--refresh` makes the API check DNS again first. Cloud and default frbit.app domains have no DNS record and answer 404. Domains are not billed. `--json` prints the raw API body (for `create`, the created domain only).

## Jobs

```sh
frbit jobs list en-a1b2c3
frbit jobs get jo-a1b2c3
frbit jobs create en-a1b2c3 --name queue --type worker --command "php artisan queue:work"
frbit jobs create en-a1b2c3 --name nightly --type cron --command "php artisan schedule:run" --interval day --max-runtime 3600
frbit jobs create en-a1b2c3 --name sync --type cron --command "php sync.php" --interval custom --expression "0 */2 * * *" --max-runtime 300
frbit jobs update jo-a1b2c3 --inactive
frbit jobs delete jo-a1b2c3 --confirm jo-a1b2c3
```

`jobs list <environment>` prints the jobs and a footer `Jobs: <count> of <limit>` from the `X-Jobs-Count` and `X-Jobs-Limit` headers; the limit is 0 when no jobs plan is booked (create then answers 403). A worker runs all the time (a Laravel or Craft queue worker); a cron job runs on a schedule.

`create` needs `--name`, `--command` (must start with `php ` or `bash `) and `--type worker|cron`. A cron job also needs `--interval minute|hour|day|custom` and `--max-runtime 300|1800|3600|10800|21600`; `--interval custom` needs `--expression`, which the CLI checks with `GET /v1/jobs/check-cron` before creating. For a worker, `--interval`, `--expression` and `--max-runtime` are ignored with a warning on stderr and not sent; so is `--expression` with an interval other than `custom`. Optional: `--inactive`, `--exit-timeout 1|5|10|30`, `--exit-signal TERM|INT`.

`update <job>` takes the same flags plus `--active`, and sends only the flags you pass. `delete <job>` stops and removes the job; it needs the job ID typed on a terminal or `--confirm <id>` without one. `--json` prints the raw API body.

## MySQL

```sh
frbit mysql get en-a1b2c3
frbit mysql rotate-password en-a1b2c3 --confirm en-a1b2c3
```

`mysql get <environment>` prints host, port, user and database; it never prints a password (`--json` prints the raw API body). `rotate-password` starts a password rotation; the app breaks until it reads the new password (redeploy or restart). It needs the environment ID typed on a terminal or `--confirm <id>` without one. The new password is never printed: the app reads it from its environment variables.

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

## Catalog

Look up the values `apps create` needs instead of guessing them. These lists are not paginated; `--limit` and `--all` are accepted for consistency.

```sh
frbit regions list
frbit plans list --component php --region eu-w1a --currency EUR
frbit presets list
frbit software list
frbit php-versions list
frbit php-extensions list
frbit node-versions list
```

`plans list` accepts `--region` and `--currency` (`EUR` or `USD`) and filters client-side with `--component <slug>`. All catalog lists accept `--json`.

## Git repositories

```sh
frbit git repositories
frbit git branches acme/site
```

## Backups

```sh
frbit backups list en-a1b2c3
frbit backups create en-a1b2c3
frbit backups get ba-a1b2c3
frbit backups restore ba-a1b2c3 --confirm ba-a1b2c3
frbit backups download ba-a1b2c3 --type database --output ./backups/
```

`create` requests a manual backup; it appears in `list` once completed. `list` and `get` take `--json`.

`restore` overwrites the live database and files of the environment with the backup and cannot be undone. It asks you to type the backup ID, or takes `--confirm <backup>` without a TTY; a missing or wrong value fails before any request.

`download` asks the API for a signed link (`--type files|database`, default `files`) and streams it to disk without sending the API token to the storage host. `--output` is a file, a folder, or `-` for stdout; the default is `<backup>-<type>` plus the extension from the storage response, in the current folder. The data goes to a temporary file in the target folder that is renamed on success and removed on failure. The file is written with mode 0600 because a backup holds database dumps. An existing target file is not overwritten unless you pass `--force`. `--output -` may leave partial output on stdout when the transfer fails. The link expires; run the command again for a fresh one.

## Link a local project

```sh
frbit link
frbit link en-a1b2c3
frbit link en-d4e5f6 --label staging
```

Commands that work on the environment of the current checkout find it in this order: the environment argument (an ID, or a label from `.fortrabbit/environments.yml`), `FORTRABBIT_ENVIRONMENT`, the file entry for the current branch, the server lookup, the only entry in the file, else a list to pick from on a terminal.

The server lookup matches the GitHub `origin` remote to the repository connected to an app, and the current branch to the environment that deploys it. On such a connected repository `frbit link` confirms the match and writes nothing.

Without a connection, `frbit link <environment>` writes `.fortrabbit/environments.yml`, a flat map of label to environment ID. The label defaults to the current branch; `--label` sets another one. Other entries stay; commit the file. `.fortrabbit/deploy.yml` is never touched.

```yaml
main: en-a1b2c3
staging: en-d4e5f6
```

An ID outside your account fails with `run frbit link`, for example in a fork of a template. A `.fortrabbit` file with `app-env-id=` from an older setup blocks the folder: on a terminal `frbit link` offers to move the ID into `environments.yml` under the current branch.

## Deleting resources

Apps, environments, domains, jobs, teams, and payment methods can be deleted. The CLI retrieves the resource first, displays the consequences, and requires the exact public ID before sending the irreversible request:

```sh
frbit apps delete ap-a1b2c3
frbit environments delete en-a1b2c3
frbit domains delete do-a1b2c3
frbit jobs delete jo-a1b2c3
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
| `frbit apps list` | List apps. Accepts `--page`, `--limit`, `--all`, repeatable `--id`, and `--json`. |
| `frbit apps get <id>` | Get an app. |
| `frbit apps create` | Create an app and its initial environment. Accepts field flags or `--file`. Shows the monthly price and asks first; `--yes` skips the question. |
| `frbit apps update <id>` | Update an app: name, payment method, software preset, technical contacts, deployment comments, repository connection. Accepts field flags or `--file`. |
| `frbit apps delete <id>` | Permanently delete an app. |
| `frbit environments list` / `get <id>` | List or get environments. `list` accepts `--page`, `--limit`, `--all`, and repeatable `--id`. |
| `frbit environments create` | Create and optionally configure/deploy an environment. Accepts field flags or `--file`. Shows the monthly price and asks first; `--yes` skips the question. |
| `frbit environments update <id>` | Update an environment and its deployment configuration, `--software-version`, `--php-version`. |
| `frbit environments php-settings get <id>` / `update <id>` | Read or change php.ini settings (`--set KEY=VALUE`) and extensions (`--extension NAME=on\|off`). |
| `frbit environments basic-auth get <id>` / `update <id>` | Read or change basic auth; the password only via `--password-stdin`. |
| `frbit environments firewall-rules list <env>` / `add <env>` / `delete <id>` | Manage opened ports; `add` takes `--description` and `--port N[-M]`, `delete` needs `--confirm <id>` without a terminal. |
| `frbit environments components get <id>` / `update <id>` | Read or change component sizes (`--component SLUG=SIZE`); update shows prices and needs `--yes` without a terminal. |
| `frbit environments delete <id>` | Permanently delete an environment. |
| `frbit environments variables get <id>` | Get custom and platform-injected environment variables. |
| `frbit environments variables update <id>` | Set or delete custom environment variables. |
| `frbit environments restart <id>` | Request an environment restart. |
| `frbit environments deploy <id>` | Create a deployment from the configured Git source. |
| `frbit environments logs <id>` | Show environment logs: `--source`, `--limit`, `--since`, `--status`, `--method`, `--follow`. |
| `frbit deployments list` / `get <id>` / `logs <id>` | List deployments, get one, or retrieve its logs (`logs --follow` until the deployment ends). `list` accepts `--page`, `--limit`, `--all`, and `--environment`. |
| `frbit domains list` / `get <id>` / `delete <id>` / `create <environment> <name>` / `dns <id>` | List, get, delete, create domains, or show their DNS record. `list` accepts `--page`, `--limit`, `--all`, and repeatable `--id`. |
| `frbit jobs list <environment>` / `get <id>` / `create <environment>` / `update <id>` / `delete <id>` | Manage workers and cron jobs; `list` shows the jobs-plan limit, `delete` needs `--confirm <id>` without a terminal. |
| `frbit people list` / `get <id>` | List or get people. `list` accepts `--limit`, `--all`, and repeatable `--id`. |
| `frbit teams list` / `get <id>` / `delete <id>` | List, get, or delete teams. `list` accepts `--limit` and `--all`. |
| `frbit payment-methods list` / `get <id>` / `delete <id>` | List, get, or delete payment methods. `list` accepts `--limit` and `--all`. |
| `frbit regions list` / `presets list` / `software list` | List regions, software presets, or software with versions. |
| `frbit plans list` | List component plans with sizes and prices. Accepts `--region`, `--currency`, and `--component`. |
| `frbit php-versions list` / `php-extensions list` / `php-settings list` / `node-versions list` | List the available PHP versions, PHP extensions, php.ini directives with accepted values, or Node.js versions. |
| `frbit git repositories` | List repositories of the connected Git account. |
| `frbit git branches <owner/repo>` | List the branches of a repository. |
| `frbit link [environment]` | Link the checkout to an environment: confirm the connected repository, or write `.fortrabbit/environments.yml`. Accepts `--label`. |
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
| `FORTRABBIT_ENVIRONMENT` | Environment of the current checkout, after an explicit argument. Set by the platform on every environment. |

## Troubleshooting

If a command reports that no API token is available, set `FRBIT_TOKEN` for that command or run `frbit auth login`.

If authentication fails, confirm that the token is active and has access to the expected apps, then run `frbit auth status` to test the exact profile and host in use.
